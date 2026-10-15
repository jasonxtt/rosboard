package imagepull

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"rosboard/internal/downloadnet"
)

type mappedTransport struct {
	target *url.URL
	rt     http.RoundTripper
}

func (m mappedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = m.target.Scheme
	u.Host = m.target.Host
	copy.URL = &u
	return m.rt.RoundTrip(copy)
}
func testImage(t *testing.T, arch, content string) v1.Image {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	tw.WriteHeader(&tar.Header{Name: "message.txt", Mode: 0644, Size: int64(len(content))})
	tw.Write([]byte(content))
	tw.Close()
	layer, e := tarball.LayerFromReader(bytes.NewReader(b.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	image, e := mutate.AppendLayers(empty.Image, layer)
	if e != nil {
		t.Fatal(e)
	}
	cfg, _ := image.ConfigFile()
	cfg.OS = "linux"
	cfg.Architecture = arch
	cfg.Config.Cmd = []string{"echo", content}
	cfg.Config.Env = []string{"FIXTURE=preserved"}
	image, e = mutate.ConfigFile(image, cfg)
	if e != nil {
		t.Fatal(e)
	}
	return image
}
func TestPreparePlatformPinAndDockerSave(t *testing.T) {
	server := httptest.NewTLSServer(registry.New())
	defer server.Close()
	u, _ := url.Parse(server.URL)
	transport := mappedTransport{target: u, rt: server.Client().Transport}
	amd := testImage(t, "amd64", "amd-content")
	arm := testImage(t, "arm64", "arm-content")
	index := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: arm, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}}, mutate.IndexAddendum{Add: amd, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}})
	tag, _ := name.NewTag("registry.test/library/fixture:latest")
	if e := remote.WriteIndex(tag, index, remote.WithTransport(transport)); e != nil {
		t.Fatal(e)
	}
	base := t.TempDir()
	dir := filepath.Join(base, "device", "job")
	pinned := ""
	result, e := Prepare(context.Background(), base, dir, tag.Name(), "amd64", "", transport, func(a Artifact) error { pinned = a.Digest; return nil }, func(string) {})
	if e != nil {
		t.Fatal(e)
	}
	want, _ := amd.Digest()
	if pinned != want.String() || result.Digest != pinned || result.Platform != "linux/amd64" {
		t.Fatal("wrong selected digest/platform")
	}
	f, e := os.Open(filepath.Join(dir, "image.tar"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	reader := tar.NewReader(f)
	foundConfig, foundLayer := false, false
	for {
		h, e := reader.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		data, _ := io.ReadAll(reader)
		if strings.HasSuffix(h.Name, ".json") && h.Name != "manifest.json" {
			var cfg v1.ConfigFile
			if json.Unmarshal(data, &cfg) != nil || cfg.Config.Env[0] != "FIXTURE=preserved" {
				t.Fatal("config lost")
			}
			foundConfig = true
		}
		if strings.HasSuffix(h.Name, ".tar") {
			lr := tar.NewReader(bytes.NewReader(data))
			entry, e := lr.Next()
			if e != nil || entry.Name != "message.txt" {
				t.Fatal("layer not traditional uncompressed tar")
			}
			foundLayer = true
		}
	}
	if !foundConfig || !foundLayer {
		t.Fatal("missing image metadata")
	}
	if e = remote.Write(tag, arm, remote.WithTransport(transport)); e != nil {
		t.Fatal(e)
	}
	recovered, e := Prepare(context.Background(), base, dir, tag.Name(), "amd64", pinned, transport, func(Artifact) error { return nil }, func(string) {})
	if e != nil || recovered.Digest != pinned {
		t.Fatal("recovery changed tag or lost verified files")
	}
}
func TestPrepareBoundariesAndQuota(t *testing.T) {
	for _, raw := range []string{"http://registry.test/a", "localhost:5000/a", "bad reference"} {
		if _, e := NormalizeReference(raw); e == nil {
			t.Fatal("unsafe reference accepted")
		}
	}
	if _, e := Platform("arm"); e == nil {
		t.Fatal("guessed ARM32 variant")
	}
	base := t.TempDir()
	f, e := os.Create(filepath.Join(base, "retained"))
	if e != nil {
		t.Fatal(e)
	}
	f.Truncate(Quota + 1)
	f.Close()
	if e := (&diskBudget{ctx: context.Background(), base: base}).check(); e == nil {
		t.Fatal("retained quota ignored")
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(base, "link"))
	if SafeDirectory(base, filepath.Join(base, "link", "escape")) == nil {
		t.Fatal("symlink escape accepted")
	}
	if _, e := os.Stat(filepath.Join(outside, "escape")); !os.IsNotExist(e) {
		t.Fatal("created outside staging")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Prepare(ctx, base, filepath.Join(base, "cancel"), "alpine:latest", "amd64", "", http.DefaultTransport, func(Artifact) error { return nil }, func(string) {}); e == nil {
		t.Fatal("cancel ignored")
	}
}
func TestPublicRegistryLab(t *testing.T) {
	if os.Getenv("ROSBOARD_IMAGE_PULL_LAB") != "1" {
		t.Skip("public download opt-in")
	}
	snap, e := downloadnet.New(os.Getenv("ROSBOARD_IMAGE_PROXY"))
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Transport.CloseIdleConnections()
	base := os.Getenv("ROSBOARD_IMAGE_LAB_DIR")
	if base == "" {
		base = t.TempDir()
	}
	refs := []string{"alpine:3.22", "ghcr.io/nginx/nginx-unprivileged:stable-alpine"}
	for i, ref := range refs {
		dir := filepath.Join(base, "image-"+string(rune('a'+i)))
		a, e := Prepare(context.Background(), base, dir, ref, "amd64", "", snap.Transport, func(Artifact) error { return nil }, func(phase string) { t.Log(phase) })
		if e != nil {
			t.Fatal(e)
		}
		data, _ := json.Marshal(a)
		t.Log(string(data))
	}
}
