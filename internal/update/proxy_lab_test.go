package update

import (
	"context"
	"os"
	"testing"
	"time"

	"rosboard/internal/downloadnet"
)

// Downloads and verifies official release/checksum/CDN content into an isolated
// test directory. Never installs, replaces a binary or contacts RouterOS.
func TestPublicUpdaterProxyLab(t *testing.T) {
	if os.Getenv("ROSBOARD_DOWNLOAD_PROXY_LAB") != "1" {
		t.Skip("explicit public download lab")
	}
	snapshot, e := downloadnet.New(os.Getenv("ROSBOARD_IMAGE_PROXY"))
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Transport.CloseIdleConnections()
	client := newReleaseClient()
	client.http.Transport = snapshot.Transport
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	release, e := client.discover(ctx, "amd64")
	if e != nil {
		t.Fatal(e)
	}
	if release.Asset.Name == "" || release.Checksums.Name == "" {
		t.Fatal("official architecture asset/checksum missing")
	}
	paths := fixturePaths(t)
	job := Job{ID: "proxy-download-lab", From: "0.0.0", To: release.Version, Stage: "downloading", Total: release.Asset.Size}
	if e = client.download(ctx, paths, *release, &job); e != nil {
		t.Fatal(e)
	}
	t.Log("official release metadata, checksum, redirected package, ELF and version validation passed:", release.Version)
}
