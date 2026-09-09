package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"rosboard/internal/buildinfo"
)

func checksum(text, name string) (string, error) {
	found := ""
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		file := strings.TrimPrefix(strings.TrimPrefix(f[1], "*"), "dist/")
		if file != name {
			continue
		}
		b, e := hex.DecodeString(f[0])
		if e != nil || len(b) != 32 || found != "" {
			return "", errors.New("invalid or duplicate checksum")
		}
		found = strings.ToLower(f[0])
	}
	if found == "" {
		return "", errors.New("missing archive checksum")
	}
	return found, nil
}
func (c *releaseClient) download(ctx context.Context, p Paths, r Release, j *Job) error {
	cleanupDownload(p)
	verified := false
	defer func() {
		if !verified {
			os.Remove(filepath.Join(p.State, "candidate"))
		}
	}()
	defer os.Remove(filepath.Join(p.State, "download.tar.gz"))
	if r.Asset.Size <= 0 || r.Asset.Size > maxArchive || r.Checksums.Size <= 0 || r.Checksums.Size > 64<<10 {
		return errors.New("invalid release asset size")
	}
	resp, e := c.get(ctx, r.Checksums.URL)
	if e != nil {
		return e
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	resp.Body.Close()
	if e != nil {
		return e
	}
	if len(b) > 64<<10 {
		return errors.New("checksum manifest too large")
	}
	want, e := checksum(string(b), r.Asset.Name)
	if e != nil {
		return e
	}
	resp, e = c.get(ctx, r.Asset.URL)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	f, e := os.OpenFile(filepath.Join(p.State, "download.tar.gz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	hash := sha256.New()
	last := time.Now()
	reader := io.LimitReader(resp.Body, r.Asset.Size+1)
	buf := make([]byte, 64<<10)
	for {
		n, re := reader.Read(buf)
		if n > 0 {
			_, e = io.MultiWriter(f, hash).Write(buf[:n])
			if e != nil {
				break
			}
			j.Downloaded += int64(n)
			if time.Since(last) > time.Second {
				if e = saveJob(p, j); e != nil {
					break
				}
				last = time.Now()
			}
		}
		if re != nil {
			if re != io.EOF {
				e = re
			}
			break
		}
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if j.Downloaded != r.Asset.Size || hex.EncodeToString(hash.Sum(nil)) != want {
		return errors.New("release checksum or size mismatch")
	}
	j.Stage = "verifying"
	if e = saveJob(p, j); e != nil {
		return e
	}
	f, e = os.Open(filepath.Join(p.State, "download.tar.gz"))
	if e != nil {
		return e
	}
	defer f.Close()
	candidate := filepath.Join(p.State, "candidate")
	if e = extractBinary(f, candidate); e != nil {
		return e
	}
	if e = verifyELF(candidate, buildinfo.Current().Arch); e != nil {
		return e
	}
	// No execution happens before the official package hash and archive structure match.
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	probeDir, e := os.MkdirTemp(p.State, "probe-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(probeDir)
	cmd := exec.CommandContext(probeCtx, candidate, "version")
	// Older or incorrectly packaged programs may treat "version" as startup.
	// They must never see the live working directory or ROSBOARD_* overrides.
	cmd.Dir = probeDir
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	var output limitedOutput
	cmd.Stdout = &output
	e = cmd.Run()
	if e != nil {
		return fmt.Errorf("candidate version probe: %w", e)
	}
	var info buildinfo.Info
	if output.Len() > 4096 || json.Unmarshal(output.Bytes(), &info) != nil || info.Version != r.Version || info.OS != "linux" || info.Arch != buildinfo.Current().Arch {
		return errors.New("candidate build metadata mismatch")
	}
	if e = syncDir(p.State); e != nil {
		return e
	}
	verified = true
	return nil
}
func extractBinary(reader io.Reader, dest string) error {
	gz, e := gzip.NewReader(reader)
	if e != nil {
		return e
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, maxArchive+1))
	found := false
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if found || h.Name != "rosboard" || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > maxArchive {
			return errors.New("unexpected release archive entry")
		}
		f, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if e != nil {
			return e
		}
		n, e := io.Copy(f, tr)
		if e == nil && n != h.Size {
			e = io.ErrUnexpectedEOF
		}
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		found = true
	}
	if !found {
		return errors.New("release archive has no executable")
	}
	// Drain to validate the gzip checksum and bound padding/concatenated streams.
	n, e := io.Copy(io.Discard, io.LimitReader(gz, maxArchive+1))
	if e != nil {
		return e
	}
	if n > maxArchive {
		return errors.New("archive padding too large")
	}
	return nil
}

func verifyELF(path, arch string) error {
	f, e := elf.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	machines := map[string]elf.Machine{"amd64": elf.EM_X86_64, "amd64-v3": elf.EM_X86_64, "arm64": elf.EM_AARCH64, "armv7": elf.EM_ARM}
	machine, ok := machines[arch]
	if !ok || f.Machine != machine || f.Data != elf.ELFDATA2LSB || (f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN) {
		return errors.New("candidate executable architecture mismatch")
	}
	if arch == "armv7" && f.Class != elf.ELFCLASS32 || arch != "armv7" && f.Class != elf.ELFCLASS64 {
		return errors.New("candidate executable class mismatch")
	}
	return nil
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4096 {
		return 0, errors.New("candidate metadata output too large")
	}
	return b.Buffer.Write(p)
}
