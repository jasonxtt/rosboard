package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadServicePreviewGzipHashAndPendingVersion(t *testing.T) {
	tempDir := t.TempDir()
	service := NewUploadService(tempDir)
	preview, err := service.Preview(context.Background(), `../../outside.yaml`, strings.NewReader("payload:\n  - DOMAIN,Example.com\n"))
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Filename != "outside.yaml" || preview.Size == 0 || preview.SHA256 == "" {
		t.Fatalf("unexpected preview metadata: %#v", preview)
	}
	if len(preview.CompressedYAML) == 0 || !bytes.HasPrefix(preview.CompressedYAML, []byte{0x1f, 0x8b}) {
		t.Fatalf("preview did not contain gzip YAML")
	}
	if got := len(preview.Parse.Rules); got != 1 {
		t.Fatalf("parsed rule count = %d, want 1", got)
	}
	version, rules, err := preview.PendingVersion("router-a", "source-a", "version-a")
	if err != nil {
		t.Fatalf("PendingVersion() error = %v", err)
	}
	if version.State != "pending" || version.DeviceID != "router-a" || version.SourceID != "source-a" || version.ID != "version-a" {
		t.Fatalf("unexpected pending version: %#v", version)
	}
	if version.SHA256 != preview.SHA256 || len(version.CompressedYAML) == 0 {
		t.Fatalf("version did not preserve upload metadata: %#v", version)
	}
	if len(rules) != 1 || rules[0].DeviceID != "router-a" || rules[0].VersionID != "version-a" {
		t.Fatalf("unexpected version rules: %#v", rules)
	}
	if entries, err := os.ReadDir(tempDir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("temporary upload files were not cleaned up: %v", entries)
	}

	if _, err := json.Marshal(version); err != nil {
		t.Fatalf("version should remain serializable: %v", err)
	}
}

func TestUploadServiceRejectsSizeAndCleansTemporaryFiles(t *testing.T) {
	tempDir := t.TempDir()
	service := NewUploadService(tempDir)
	if _, err := service.Preview(context.Background(), "rules.yaml", bytes.NewReader(make([]byte, MaxSourceBytes+1))); err == nil {
		t.Fatal("oversize upload was accepted")
	}
	if _, err := service.Preview(context.Background(), "rules.txt", strings.NewReader("payload: []\n")); err == nil {
		t.Fatal("non-YAML upload was accepted")
	}
	if entries, err := os.ReadDir(tempDir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("temporary files remain after failure: %v", entries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Preview(ctx, "rules.yaml", strings.NewReader("payload:\n  - DOMAIN,example.com\n")); err == nil {
		t.Fatal("cancelled upload was accepted")
	}
	if entries, err := os.ReadDir(tempDir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("temporary files remain after cancellation: %v", entries)
	}
}

func TestUploadServicePreviewMultipartLimitsAndFilenameIsolation(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="../../etc/passwd.yaml"`)
	header.Set("Content-Type", "application/x-yaml")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("payload:\n  - DOMAIN,upload.example\n"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	tempDir := t.TempDir()
	service := NewUploadService(tempDir)
	preview, err := service.PreviewMultipart(context.Background(), writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("PreviewMultipart() error = %v", err)
	}
	if preview.Filename != "passwd.yaml" {
		t.Fatalf("filename was not isolated and sanitized: %q", preview.Filename)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "..", "etc", "passwd.yaml")); !os.IsNotExist(err) {
		t.Fatalf("upload filename escaped temp directory: %v", err)
	}
	if entries, err := os.ReadDir(tempDir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("temporary multipart files were not cleaned up: %v", entries)
	}

	if _, err := service.PreviewMultipart(context.Background(), writer.FormDataContentType(), bytes.NewReader(make([]byte, maxMultipartBytes+1))); err == nil {
		t.Fatal("oversize multipart body was accepted")
	}
}
