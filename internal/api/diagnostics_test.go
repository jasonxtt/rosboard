package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rosboard/internal/config"
	"rosboard/internal/diagnostics"
	"rosboard/internal/store"
)

func TestDiagnosticsQuickReportIsReadOnlyAPI(t *testing.T) {
	cfg := config.Config{
		DataDir: t.TempDir(),
		Devices: []config.DeviceConfig{{
			ID:      "router-a",
			Name:    "Router A",
			Enabled: true,
			RouterOS: config.RouterOSConfig{
				BaseURL:  "http://router-a.test",
				Username: "reader",
				Password: "fixture",
			},
		}},
	}
	storage, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer storage.Close()
	server := NewServerWithManager(cfg, nil, nil, fsys{}, nil)
	server.store = storage
	request := httptest.NewRequest(http.MethodGet, "/api/diagnostics?device=router-a", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload struct {
		Mode     string `json:"mode"`
		DeviceID string `json:"deviceId"`
		Findings []struct {
			ID string `json:"id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if payload.Mode != "quick" || payload.DeviceID != "router-a" || len(payload.Findings) == 0 {
		t.Fatalf("unexpected diagnostics report: %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "devices")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("diagnostics created device directory, stat error = %v", err)
	}
}

func TestDiagnosticsRequiresDeviceAndGet(t *testing.T) {
	server := NewServerWithManager(config.Config{}, nil, nil, fsys{}, nil)
	for _, test := range []struct {
		name string
		path string
		want int
	}{
		{name: "missing device", path: "/api/diagnostics", want: http.StatusBadRequest},
		{name: "wrong method", path: "/api/diagnostics?device=x", want: http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			method := http.MethodGet
			if test.name == "wrong method" {
				method = http.MethodPost
			}
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(method, test.path, nil))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
		})
	}
}

func TestDiagnosticsDeepEndpointReturnsSnapshotAndTrace(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()

	request := httptest.NewRequest(http.MethodPost, "/api/diagnostics/deep?device=edge", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload struct {
		Mode     string `json:"mode"`
		DeviceID string `json:"deviceId"`
		Snapshot struct {
			Endpoints []struct {
				Endpoint  string `json:"endpoint"`
				ReadCount int    `json:"readCount"`
			} `json:"endpoints"`
		} `json:"snapshot"`
		IngressTrace []struct {
			ReasonCode string `json:"reasonCode"`
		} `json:"ingressTrace"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode deep report: %v", err)
	}
	if payload.Mode != "deep" || payload.DeviceID != "edge" {
		t.Fatalf("unexpected deep report identity: %#v", payload)
	}
	if len(payload.Snapshot.Endpoints) != 12 || len(payload.IngressTrace) == 0 {
		t.Fatalf("deep report lacks snapshot/trace: %#v", payload)
	}
	for _, endpoint := range payload.Snapshot.Endpoints {
		if endpoint.ReadCount != 1 {
			t.Fatalf("endpoint %s read count = %d, want 1", endpoint.Endpoint, endpoint.ReadCount)
		}
	}
}

func TestDiagnosticsDeepRequiresPost(t *testing.T) {
	server := NewServerWithManager(config.Config{}, nil, nil, fsys{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/diagnostics/deep?device=edge", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("status = %d allow = %q body = %s", recorder.Code, recorder.Header().Get("Allow"), recorder.Body.String())
	}
}

func TestDiagnosticsExportReturnsSanitizedZipFromCachedDeepReport(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	logs := diagnostics.NewLogBuffer(10, 1024)
	_, _ = logs.Write([]byte("password=abc123 Authorization: Bearer xxx\n"))
	server.SetDiagnosticLogSource(logs)

	deepResponse := httptest.NewRecorder()
	server.ServeHTTP(deepResponse, httptest.NewRequest(http.MethodPost, "/api/diagnostics/deep?device=edge", nil))
	if deepResponse.Code != http.StatusOK {
		t.Fatalf("deep status = %d: %s", deepResponse.Code, deepResponse.Body.String())
	}

	exportResponse := httptest.NewRecorder()
	server.ServeHTTP(exportResponse, httptest.NewRequest(http.MethodPost, "/api/diagnostics/export?device=edge", nil))
	if exportResponse.Code != http.StatusOK {
		t.Fatalf("export status = %d: %s", exportResponse.Code, exportResponse.Body.String())
	}
	if exportResponse.Header().Get("Content-Type") != "application/zip" || !strings.Contains(exportResponse.Header().Get("Content-Disposition"), "rosboard-diagnostics-") {
		t.Fatalf("unexpected export headers: %#v", exportResponse.Header())
	}
	archive, err := zip.NewReader(bytes.NewReader(exportResponse.Body.Bytes()), int64(exportResponse.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	paths := make(map[string]bool)
	for _, file := range archive.File {
		paths[file.Name] = true
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatalf("open %s: %v", file.Name, openErr)
		}
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", file.Name, readErr)
		}
		if strings.Contains(string(data), "abc123") || strings.Contains(string(data), "xxx") || strings.Contains(string(data), "secret") {
			t.Fatalf("export entry %s contains a forbidden fixture value", file.Name)
		}
	}
	for _, path := range []string{"manifest.json", "health-report.json", "routeros/snapshot.json", "routeros/ingress-decision-trace.json", "monitor/status.json", "policy/status.json", "access/status.json", "recognition/mosdns.json", "update/status.json", "logs/recent.log"} {
		if !paths[path] {
			t.Fatalf("export missing %s: %#v", path, paths)
		}
	}
}

// fsys is an empty asset filesystem; diagnostics tests never serve the app.
type fsys struct{}

func (fsys) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
