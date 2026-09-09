package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rosboard/internal/buildinfo"
)

func TestStableVersionComparison(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{{"0.10.0", "0.9.9", 1}, {"v0.2.0", "0.2.0", 0}, {"1.0.0", "2.0.0", -1}, {"999999999999999999999.0.0", "99.0.0", 1}} {
		got, e := CompareStable(tt.a, tt.b)
		if e != nil || got != tt.want {
			t.Fatalf("%+v: %d %v", tt, got, e)
		}
	}
	for _, s := range []string{"01.2.3", "dev", "0.2.0-rc.1", "1.2", "1.2.3+foo", "1.2.-3"} {
		if _, e := CompareStable(s, "0.1.0"); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func fixtureRelease(arch string) githubRelease {
	name := "rosboard_0.2.0_linux_" + arch + ".tar.gz"
	return githubRelease{Tag: "v0.2.0", Assets: []Asset{{Name: name, URL: "https://github.com/" + Repository + "/releases/download/v0.2.0/" + name, Size: 12}, {Name: "sha256sums.txt", URL: "https://github.com/" + Repository + "/releases/download/v0.2.0/sha256sums.txt", Size: 80}}}
}
func TestReleaseExactArchitectureAndTrust(t *testing.T) {
	for _, arch := range []string{"amd64", "amd64-v3", "arm64", "armv7"} {
		r, e := selectRelease(fixtureRelease(arch), arch)
		if e != nil || r.Asset.Name == "" {
			t.Fatal(arch, r, e)
		}
	}
	r, e := selectRelease(fixtureRelease("amd64-v3"), "amd64")
	if e != nil || r.Asset.Name != "" {
		t.Fatal("v3 must not be selected for baseline", r, e)
	}
	for _, change := range []func(*githubRelease){func(r *githubRelease) { r.Draft = true }, func(r *githubRelease) { r.Prerelease = true }, func(r *githubRelease) { r.Tag = "v0.2.0-rc.1" }, func(r *githubRelease) { r.Assets[0].URL = "https://evil.example/payload" }, func(r *githubRelease) { r.Assets = append(r.Assets, r.Assets[0]) }} {
		r := fixtureRelease("arm64")
		change(&r)
		if _, e := selectRelease(r, "arm64"); e == nil {
			t.Fatal("invalid release accepted")
		}
	}
	for _, s := range []string{"http://github.com/a", "https://github.com.evil.test/a", "https://user:pass@github.com/a", "https://github.com:8443/a"} {
		u, _ := url.Parse(s)
		if allowedDownloadURL(u) {
			t.Fatal(s)
		}
	}
}
func TestChecksumManifest(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, name := range []string{"package.tar.gz", "dist/package.tar.gz", "*package.tar.gz"} {
		got, e := checksum(hash+"  "+name, "package.tar.gz")
		if e != nil || got != hash {
			t.Fatal(got, e)
		}
	}
	for _, text := range []string{"garbage package.tar.gz", hash + " other.tar.gz", hash + " package.tar.gz\n" + hash + " package.tar.gz"} {
		if _, e := checksum(text, "package.tar.gz"); e == nil {
			t.Fatal("invalid checksum accepted")
		}
	}
}
func archive(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, h := range headers {
		if e := tw.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write(bytes.Repeat([]byte{'x'}, int(h.Size)))
		}
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}
func TestArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, h := range []*tar.Header{{Name: "../rosboard", Typeflag: tar.TypeReg, Size: 5}, {Name: "rosboard", Typeflag: tar.TypeSymlink, Linkname: "/tmp/evil"}, {Name: "unexpected", Typeflag: tar.TypeReg, Size: 5}} {
		if e := extractBinary(bytes.NewReader(archive(t, h)), filepath.Join(t.TempDir(), "candidate")); e == nil {
			t.Fatal("unsafe archive accepted", h)
		}
	}
	valid := &tar.Header{Name: "rosboard", Typeflag: tar.TypeReg, Size: 5}
	if e := extractBinary(bytes.NewReader(archive(t, valid, valid)), filepath.Join(t.TempDir(), "candidate")); e == nil {
		t.Fatal("duplicate executable accepted")
	}
	b := archive(t, valid)
	if e := extractBinary(bytes.NewReader(b), filepath.Join(t.TempDir(), "candidate")); e != nil {
		t.Fatal(e)
	}
	b[len(b)-5] ^= 0xff
	if e := extractBinary(bytes.NewReader(b), filepath.Join(t.TempDir(), "candidate")); e == nil {
		t.Fatal("gzip corruption accepted")
	}
}
func fixturePaths(t *testing.T) Paths {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("ROSBOARD_UPDATE_BACKUP_DIR", "")
	p, e := NewPaths(filepath.Join(root, "rosboard"), filepath.Join(root, "config.yaml"), filepath.Join(root, "data"))
	if e != nil {
		t.Fatal(e)
	}
	os.MkdirAll(p.Data, 0700)
	os.MkdirAll(p.State, 0700)
	os.WriteFile(p.Binary, []byte("old binary"), 0700)
	os.WriteFile(p.Config, []byte("private config"), 0600)
	os.WriteFile(filepath.Join(p.Data, "rosboard.db"), []byte("db bytes"), 0600)
	os.WriteFile(filepath.Join(p.Data, "rosboard.db-wal"), []byte("wal bytes"), 0600)
	os.Mkdir(filepath.Join(p.Data, "devices"), 0700)
	os.WriteFile(filepath.Join(p.Data, "devices", "second.db"), []byte("second device"), 0600)
	return p
}
func TestBackupAndIdempotentRecovery(t *testing.T) {
	p := fixturePaths(t)
	if e := p.snapshot(); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(p.Binary, []byte("bad binary"), 0700)
	os.WriteFile(p.Config, []byte("bad config"), 0600)
	os.RemoveAll(p.Data)
	os.Mkdir(p.Data, 0700)
	os.WriteFile(filepath.Join(p.Data, "new.db"), []byte("new schema"), 0600)
	for range 2 {
		if e := p.restore(); e != nil {
			t.Fatal(e)
		}
	}
	for path, want := range map[string]string{p.Binary: "old binary", p.Config: "private config", filepath.Join(p.Data, "rosboard.db"): "db bytes", filepath.Join(p.Data, "rosboard.db-wal"): "wal bytes", filepath.Join(p.Data, "devices", "second.db"): "second device"} {
		b, e := os.ReadFile(path)
		if e != nil || string(b) != want {
			t.Fatal(path, string(b), e)
		}
	}
	if _, e := os.Stat(filepath.Join(p.Data, "new.db")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("candidate data survived rollback")
	}
}
func TestBackupRejectsUnsafePathsAndSymlinks(t *testing.T) {
	p := fixturePaths(t)
	unsafe := p
	unsafe.Data = filepath.Dir(p.Binary)
	if e := unsafe.validate(); e == nil {
		t.Fatal("overlapping data accepted")
	}
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.Symlink(outside, filepath.Join(p.Data, "link"))
	if e := p.snapshot(); e == nil {
		t.Fatal("symlink accepted")
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "secret" {
		t.Fatal("outside data changed")
	}
}
func TestFailedCandidateRestoresBeforeRestart(t *testing.T) {
	p := fixturePaths(t)
	os.WriteFile(filepath.Join(p.State, "candidate"), []byte("not executable"), 0700)
	j := &Job{ID: "failure", From: "0.1.0", To: "0.2.0", Stage: "pending"}
	if _, e := apply(context.Background(), p, j, log.New(io.Discard, "", 0)); e != nil {
		t.Fatal(e)
	}
	saved, e := readJob(p)
	if e != nil || saved.Stage != "rolled_back" {
		t.Fatal(saved, e)
	}
	b, _ := os.ReadFile(p.Binary)
	if string(b) != "old binary" {
		t.Fatal("old executable not restored")
	}
}
func TestInterruptedInstallRecovery(t *testing.T) {
	for _, stage := range []string{"installing", "verifying_startup"} {
		t.Run(stage, func(t *testing.T) {
			p := fixturePaths(t)
			if e := p.snapshot(); e != nil {
				t.Fatal(e)
			}
			os.WriteFile(p.Binary, []byte("bad"), 0700)
			saveJob(p, &Job{ID: "power-loss", Stage: stage})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if e := Supervise(ctx, p, log.New(io.Discard, "", 0)); e != nil {
				t.Fatal(e)
			}
			j, _ := readJob(p)
			if j.Stage != "rolled_back" {
				t.Fatal(j)
			}
			b, _ := os.ReadFile(p.Binary)
			if string(b) != "old binary" {
				t.Fatal("wrong binary")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCheckFailureRetainsCacheAndPreventsInstall(t *testing.T) {
	p := fixturePaths(t)
	m := NewManager(context.Background(), p, buildinfo.Info{Version: "0.1.0", OS: "linux", Arch: "amd64"}, func() {}, log.New(io.Discard, "", 0))
	m.supported = true
	m.latest, _ = selectRelease(fixtureRelease("amd64"), "amd64")
	now := time.Now()
	m.checkedAt = &now
	m.client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network down") })
	s, e := m.Check(context.Background())
	if e == nil || s.Latest == nil || s.CheckError == "" || s.CanInstall {
		t.Fatal(s, e)
	}
	if _, e = m.Install("0.2.0"); e == nil {
		t.Fatal("install allowed after failed check")
	}
}
func TestInstallRejectsStaleAndDuplicateJobs(t *testing.T) {
	p := fixturePaths(t)
	m := NewManager(context.Background(), p, buildinfo.Info{Version: "0.1.0", OS: "linux", Arch: "amd64"}, func() {}, log.New(io.Discard, "", 0))
	m.supported = true
	m.latest, _ = selectRelease(fixtureRelease("amd64"), "amd64")
	now := time.Now().Add(-2 * time.Hour)
	m.checkedAt = &now
	if _, e := m.Install("0.2.0"); e == nil {
		t.Fatal("stale install accepted")
	}
	saveJob(p, &Job{ID: "active", Stage: "pending"})
	if m.Status().CanInstall || !m.Active() {
		t.Fatal("duplicate install allowed")
	}
}

func TestCorruptDownloadNeverStopsOrReplacesPanel(t *testing.T) {
	p := fixturePaths(t)
	m := NewManager(context.Background(), p, buildinfo.Info{Version: "0.1.0", OS: "linux", Arch: "amd64"}, func() { t.Error("corrupt download stopped panel") }, log.New(io.Discard, "", 0))
	r, _ := selectRelease(fixtureRelease("amd64"), "amd64")
	r.Asset.Size = 5
	r.Checksums.Size = 100
	m.client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := "wrong"
		if strings.HasSuffix(req.URL.Path, "sha256sums.txt") {
			body = strings.Repeat("a", 64) + "  " + r.Asset.Name
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	m.download(*r, &Job{ID: "corrupt", Stage: "downloading", From: "0.1.0", To: "0.2.0", Total: 5})
	j, e := readJob(p)
	if e != nil || j.Stage != "failed" {
		t.Fatal(j, e)
	}
	b, _ := os.ReadFile(p.Binary)
	if string(b) != "old binary" {
		t.Fatal("running executable changed")
	}
	if _, e = os.Stat(filepath.Join(p.State, "candidate")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("corrupt candidate retained")
	}
}
func TestUnreadableJournalFailsClosed(t *testing.T) {
	p := fixturePaths(t)
	os.WriteFile(filepath.Join(p.State, "job.json"), []byte("broken"), 0600)
	m := NewManager(context.Background(), p, buildinfo.Info{}, func() {}, log.New(io.Discard, "", 0))
	if m.Status().CanInstall || !m.Active() {
		t.Fatal("corrupt journal did not block mutations")
	}
}

func TestFullResetClearsPrivateRecoveryCopies(t *testing.T) {
	p := fixturePaths(t)
	if e := p.snapshot(); e != nil {
		t.Fatal(e)
	}
	saveJob(p, &Job{ID: "old", Stage: "succeeded"})
	m := NewManager(context.Background(), p, buildinfo.Info{}, func() {}, log.New(io.Discard, "", 0))
	if e := m.ClearRecovery(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(p.Backup, "previous")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("private backup retained")
	}
	if j, e := readJob(p); e != nil || j != nil {
		t.Fatal(j, e)
	}
	// This helper only clears updater-owned copies, not the live data; the store
	// owns live reset and its existing transaction/rollback behavior.
	if _, e := os.Stat(p.Config); e != nil {
		t.Fatal("live config removed prematurely")
	}
}

// Optional end-to-end download probe for Linux CI/test machines. The fixture is
// a real release-format rosboard executable, never a production installation.
func TestReleaseDownloadWithRealExecutable(t *testing.T) {
	source := os.Getenv("ROSBOARD_UPDATE_TEST_BINARY")
	if source == "" {
		t.Skip("set ROSBOARD_UPDATE_TEST_BINARY to a Linux release executable")
	}
	output, e := exec.Command(source, "version").Output()
	if e != nil {
		t.Fatal(e)
	}
	var info buildinfo.Info
	if e = json.Unmarshal(output, &info); e != nil {
		t.Fatal(e)
	}
	binary, e := os.ReadFile(source)
	if e != nil {
		t.Fatal(e)
	}
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	tw := tar.NewWriter(gz)
	if e = tw.WriteHeader(&tar.Header{Name: "rosboard", Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(binary))}); e != nil {
		t.Fatal(e)
	}
	if _, e = tw.Write(binary); e != nil {
		t.Fatal(e)
	}
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(packed.Bytes())
	name := "rosboard_" + info.Version + "_linux_" + info.Arch + ".tar.gz"
	manifest := fmt.Sprintf("%x  dist/%s\n", sum, name)
	prefix := "https://github.com/" + Repository + "/releases/download/v" + info.Version + "/"
	rel := githubRelease{Tag: "v" + info.Version, Assets: []Asset{{Name: name, URL: prefix + name, Size: int64(packed.Len())}, {Name: "sha256sums.txt", URL: prefix + "sha256sums.txt", Size: int64(len(manifest))}}}
	metadata, _ := json.Marshal(rel)
	p := fixturePaths(t)
	stopped := make(chan struct{}, 1)
	m := NewManager(context.Background(), p, buildinfo.Info{Version: "0.0.0", OS: "linux", Arch: info.Arch}, func() { stopped <- struct{}{} }, log.New(io.Discard, "", 0))
	m.supported = true
	m.client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var data []byte
		switch {
		case strings.HasSuffix(req.URL.Path, "/latest"):
			data = metadata
		case strings.HasSuffix(req.URL.Path, "sha256sums.txt"):
			data = []byte(manifest)
		case strings.HasSuffix(req.URL.Path, name):
			data = packed.Bytes()
		default:
			t.Errorf("unexpected URL %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})
	status, e := m.Check(context.Background())
	if e != nil || !status.CanInstall {
		t.Fatal(status, e)
	}
	if _, e = m.Install(info.Version); e != nil {
		t.Fatal(e)
	}
	select {
	case <-stopped:
	case <-time.After(30 * time.Second):
		t.Fatal("download failed", m.Status())
	}
	j, e := readJob(p)
	if e != nil || j.Stage != "pending" {
		t.Fatal(j, e)
	}
	candidate, e := os.ReadFile(filepath.Join(p.State, "candidate"))
	if e != nil || !bytes.Equal(candidate, binary) {
		t.Fatal("candidate differs from release")
	}
	old, _ := os.ReadFile(p.Binary)
	if string(old) != "old binary" {
		t.Fatal("download replaced program before supervisor backup")
	}
}
