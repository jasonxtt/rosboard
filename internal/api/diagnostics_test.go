package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"rosboard/internal/config"
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
	server := NewServerWithManager(cfg, nil, nil, fsys{}, nil)
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

// fsys is an empty asset filesystem; diagnostics tests never serve the app.
type fsys struct{}

func (fsys) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
