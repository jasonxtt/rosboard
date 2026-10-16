// Package imagepull fetches public registry content without a Docker daemon.
// Layers are verified opaque streams: never executed, mounted or extracted.
package imagepull

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/sys/unix"
)

const MaxArchive int64 = 1 << 30
const Quota int64 = 5 << 30
const Reserve int64 = 256 << 20

var downloadGate = make(chan struct{}, 1)
var ErrPrepare = errors.New("镜像下载或校验失败；请检查公开镜像名称、代理、磁盘空间和目标架构")

type Artifact struct {
	Files          []string `json:"files,omitempty"`
	HasHealthcheck bool     `json:"hasHealthcheck,omitempty"`
	Reference      string   `json:"reference"`
	Digest         string   `json:"digest"`
	Platform       string   `json:"platform"`
	SHA256         string   `json:"sha256"`
	Bytes          int64    `json:"bytes"`
	Expanded       int64    `json:"expanded"`
}

func NormalizeReference(value string) (string, error) {
	if strings.ContainsAny(value, " \t\r\n\\") || strings.Contains(value, "://") || len(value) > 512 {
		return "", ErrPrepare
	}
	ref, e := name.ParseReference(value, name.WeakValidation)
	if e != nil {
		return "", ErrPrepare
	}
	if ref.Context().Registry.Scheme() != "https" {
		return "", ErrPrepare
	}
	return ref.Name(), nil
}
func Platform(arch string) (v1.Platform, error) {
	p := v1.Platform{OS: "linux", Architecture: arch}
	switch arch {
	case "x86_64":
		p.Architecture = "amd64"
	case "amd64", "arm64":
	default:
		return p, errors.New("目标架构暂不支持；ARM32 需要明确 CPU variant")
	}
	return p, nil
}

// Prepare pins the selected manifest before downloading blobs. Recovery passes
// the saved digest; it can never resolve a changed tag. The caller journals pin.
func Prepare(ctx context.Context, base, jobDir, reference, arch, fixed string, rt http.RoundTripper, pin func(Artifact) error, phase func(string)) (Artifact, error) {
	select {
	case downloadGate <- struct{}{}:
		defer func() { <-downloadGate }()
	case <-ctx.Done():
		return Artifact{}, ctx.Err()
	}
	p, e := Platform(arch)
	if e != nil {
		return Artifact{}, e
	}
	normalized, e := NormalizeReference(reference)
	if e != nil {
		return Artifact{}, e
	}
	ref, _ := name.ParseReference(normalized)
	if fixed != "" {
		ref, e = name.NewDigest(ref.Context().Name() + "@" + fixed)
		if e != nil {
			return Artifact{}, ErrPrepare
		}
	}
	opts := []remote.Option{remote.WithContext(ctx), remote.WithAuth(authn.Anonymous), remote.WithTransport(readTransport{base: rt}), remote.WithPlatform(p), remote.WithJobs(1), remote.WithRetryStatusCodes(429, 500, 502, 503, 504)}
	phase("解析并固定镜像 digest")
	img, e := remote.Image(ref, opts...)
	if e != nil {
		return Artifact{}, ErrPrepare
	}
	digest, e := img.Digest()
	if e != nil || digest.Algorithm != "sha256" || fixed != "" && digest.String() != fixed {
		return Artifact{}, ErrPrepare
	}
	raw, e := img.RawManifest()
	if e != nil || len(raw) > 4<<20 || !hashMatches(raw, digest) {
		return Artifact{}, ErrPrepare
	}
	manifest, e := img.Manifest()
	if e != nil || manifest.SchemaVersion != 2 || len(manifest.Layers) > 256 {
		return Artifact{}, ErrPrepare
	}
	if manifest.Config.Size <= 0 || manifest.Config.Size > 1<<20 || manifest.Config.Digest.Algorithm != "sha256" {
		return Artifact{}, ErrPrepare
	}
	configLayer, e := remote.Layer(ref.Context().Digest(manifest.Config.Digest.String()), opts...)
	if e != nil {
		return Artifact{}, ErrPrepare
	}
	configReader, e := configLayer.Compressed()
	if e != nil {
		return Artifact{}, ErrPrepare
	}
	config, e := io.ReadAll(io.LimitReader(configReader, (1<<20)+1))
	configReader.Close()
	if e != nil || len(config) > 1<<20 || int64(len(config)) != manifest.Config.Size || !hashMatches(config, manifest.Config.Digest) {
		return Artifact{}, ErrPrepare
	}
	var cfg v1.ConfigFile
	if json.Unmarshal(config, &cfg) != nil || cfg.OS != p.OS || cfg.Architecture != p.Architecture || len(cfg.RootFS.DiffIDs) != len(manifest.Layers) {
		return Artifact{}, ErrPrepare
	}
	result := Artifact{Reference: normalized, Digest: digest.String(), Platform: p.OS + "/" + p.Architecture}
	result.Files = []string{manifest.Config.Digest.Hex + ".json", "manifest.json", "image.tar"}
	for i, layer := range manifest.Layers {
		if layer.Digest.Algorithm != "sha256" || cfg.RootFS.DiffIDs[i].Algorithm != "sha256" {
			return Artifact{}, ErrPrepare
		}
		result.Files = append(result.Files, layer.Digest.Hex+".blob", cfg.RootFS.DiffIDs[i].Hex+".tar")
	}
	result.HasHealthcheck = cfg.Config.Healthcheck != nil && len(cfg.Config.Healthcheck.Test) > 0 && cfg.Config.Healthcheck.Test[0] != "NONE"

	if e = pin(result); e != nil {
		return Artifact{}, e
	}
	if e = SafeDirectory(base, jobDir); e != nil {
		return Artifact{}, e
	}
	root, e := os.OpenRoot(jobDir)
	if e != nil {
		return Artifact{}, ErrPrepare
	}
	defer root.Close()
	budget := &diskBudget{ctx: ctx, base: base}
	if e = budget.check(); e != nil {
		return Artifact{}, e
	}
	configName := manifest.Config.Digest.Hex + ".json"
	if e = writeSmall(root, configName, config); e != nil {
		return Artifact{}, ErrPrepare
	}
	paths := []string{}
	for i := range manifest.Layers {
		desc := manifest.Layers[i]
		diff := cfg.RootFS.DiffIDs[i]
		if desc.Digest.Algorithm != "sha256" || diff.Algorithm != "sha256" || desc.Size <= 0 || desc.Size > MaxArchive || !desc.MediaType.IsDistributable() {
			return Artifact{}, ErrPrepare
		}
		phase(fmt.Sprintf("下载及校验镜像层 %d/%d", i+1, len(manifest.Layers)))
		compressed := desc.Digest.Hex + ".blob"
		if !validFile(root, compressed, desc.Digest, desc.Size) {
			root.Remove(compressed + ".partial")
			layer, err := remote.Layer(ref.Context().Digest(desc.Digest.String()), opts...)
			if err != nil {
				return Artifact{}, ErrPrepare
			}
			source, err := layer.Compressed()
			if err != nil {
				return Artifact{}, ErrPrepare
			}
			err = writeVerified(root, compressed, source, desc.Digest, desc.Size, budget)
			source.Close()
			if err != nil {
				return Artifact{}, err
			}
		}
		target := diff.Hex + ".tar"
		if !validFile(root, target, diff, -1) {
			root.Remove(target + ".partial")
			src, err := root.Open(compressed)
			if err != nil {
				return Artifact{}, ErrPrepare
			}
			var reader io.Reader = src
			var closeCodec func()
			switch desc.MediaType {
			case types.DockerLayer, types.OCILayer:
				codec, err := gzip.NewReader(src)
				if err != nil {
					src.Close()
					return Artifact{}, ErrPrepare
				}
				reader = codec
				closeCodec = func() { codec.Close() }
			case types.OCILayerZStd:
				codec, err := zstd.NewReader(src, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20))
				if err != nil {
					src.Close()
					return Artifact{}, ErrPrepare
				}
				reader = codec
				closeCodec = codec.Close
			case types.DockerUncompressedLayer, types.OCIUncompressedLayer:
			default:
				src.Close()
				return Artifact{}, errors.New("镜像层格式不支持")
			}
			err = writeVerified(root, target, reader, diff, -1, budget)
			if closeCodec != nil {
				closeCodec()
			}
			src.Close()
			if err != nil {
				return Artifact{}, err
			}
		}
		info, err := root.Stat(target)
		if err != nil {
			return Artifact{}, ErrPrepare
		}
		result.Expanded += info.Size()
		if result.Expanded > MaxArchive-(4<<20) {
			return Artifact{}, errors.New("展开后的镜像超过 1 GiB 归档限制")
		}
		paths = append(paths, target)
	}
	phase("打包 Docker-save 镜像")
	tags := []string{normalized}
	if strings.Contains(normalized, "@") {
		tags = nil
	}
	archiveManifest, _ := json.Marshal([]map[string]any{{"Config": configName, "RepoTags": tags, "Layers": paths}})
	if e = writeSmall(root, "manifest.json", archiveManifest); e != nil {
		return Artifact{}, ErrPrepare
	}
	root.Remove("image.tar.partial")
	out, e := root.OpenFile("image.tar.partial", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return Artifact{}, ErrPrepare
	}
	hash := sha256.New()
	counter := &boundedWriter{w: io.MultiWriter(out, hash), budget: budget, limit: MaxArchive}
	tarWriter := tar.NewWriter(counter)
	seenArchive := map[string]bool{}
	for _, file := range append([]string{configName, "manifest.json"}, paths...) {
		if seenArchive[file] {
			continue
		}
		seenArchive[file] = true
		info, err := root.Stat(file)
		if err != nil {
			e = err
			break
		}
		if e = tarWriter.WriteHeader(&tar.Header{Name: file, Mode: 0600, Size: info.Size(), Typeflag: tar.TypeReg}); e != nil {
			break
		}
		src, err := root.Open(file)
		if err != nil {
			e = err
			break
		}
		_, e = io.Copy(tarWriter, src)
		src.Close()
		if e != nil {
			break
		}
	}
	if err := tarWriter.Close(); e == nil {
		e = err
	}
	if err := out.Sync(); e == nil {
		e = err
	}
	if err := out.Close(); e == nil {
		e = err
	}
	if e != nil {
		return Artifact{}, ErrPrepare
	}
	if e = root.Rename("image.tar.partial", "image.tar"); e != nil {
		return Artifact{}, ErrPrepare
	}
	result.Bytes = counter.n
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}
func hashMatches(data []byte, want v1.Hash) bool {
	h := sha256.Sum256(data)
	return want.Algorithm == "sha256" && hex.EncodeToString(h[:]) == want.Hex
}
func validFile(root *os.Root, path string, hash v1.Hash, size int64) bool {
	info, e := root.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > MaxArchive || size >= 0 && info.Size() != size {
		return false
	}
	f, e := root.Open(path)
	if e != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, MaxArchive+1))
	return e == nil && n <= MaxArchive && hex.EncodeToString(h.Sum(nil)) == hash.Hex
}
func writeSmall(root *os.Root, path string, data []byte) error {
	f, e := root.OpenFile(path+".partial", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = root.Rename(path+".partial", path)
	}
	return e
}
func writeVerified(root *os.Root, path string, reader io.Reader, hash v1.Hash, size int64, budget *diskBudget) error {
	f, e := root.OpenFile(path+".partial", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return ErrPrepare
	}
	h := sha256.New()
	w := &boundedWriter{w: io.MultiWriter(f, h), budget: budget, limit: MaxArchive}
	n, e := io.Copy(w, io.LimitReader(reader, MaxArchive+1))
	if e == nil && (size >= 0 && n != size || hex.EncodeToString(h.Sum(nil)) != hash.Hex) {
		e = ErrPrepare
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return ErrPrepare
	}
	return root.Rename(path+".partial", path)
}
func SafeDirectory(base, dir string) error {
	rel, e := filepath.Rel(base, dir)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ErrPrepare
	}
	for p := dir; ; p = filepath.Dir(p) {
		if i, e := os.Lstat(p); e == nil && (!i.IsDir() || i.Mode()&os.ModeSymlink != 0) {
			return ErrPrepare
		} else if e != nil && !os.IsNotExist(e) {
			return ErrPrepare
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return ErrPrepare
	}
	for p := dir; ; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if e != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return ErrPrepare
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}

type diskBudget struct {
	ctx  context.Context
	base string
}

func (b *diskBudget) check() error {
	if e := b.ctx.Err(); e != nil {
		return e
	}
	var used int64
	e := filepath.WalkDir(b.base, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrPrepare
		}
		if !d.IsDir() {
			i, e := d.Info()
			if e != nil {
				return e
			}
			used += i.Size()
		}
		return nil
	})
	if e != nil {
		return ErrPrepare
	}
	var stat unix.Statfs_t
	if unix.Statfs(b.base, &stat) != nil || used > Quota || stat.Bavail*uint64(stat.Bsize) < uint64(Reserve+1<<20) {
		return errors.New("镜像暂存空间不足；尚未成功运行的镜像将继续保留，请先处理保留任务")
	}
	return nil
}

type boundedWriter struct {
	w                 io.Writer
	budget            *diskBudget
	n, limit, checked int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if w.n+int64(len(p)) > w.limit {
		return 0, ErrPrepare
	}
	if w.n-w.checked >= 1<<20 || w.n == 0 {
		if e := w.budget.check(); e != nil {
			return 0, e
		}
		w.checked = w.n
	}
	if e := w.budget.ctx.Err(); e != nil {
		return 0, e
	}
	n, e := w.w.Write(p)
	w.n += int64(n)
	return n, e
}
