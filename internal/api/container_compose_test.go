package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"rosboard/internal/config"
	"rosboard/internal/containers"
)

func TestComposeParseAPIHasNoRouterOrWriterIO(t *testing.T) {
	cfg := config.Config{Devices: []config.DeviceConfig{{ID: "a", Enabled: true}, {ID: "b", Enabled: true}, {ID: "disabled"}, {ID: "archived", Enabled: true, Archived: true}}}
	s := NewServer(cfg, nil, fstest.MapFS{})
	s.containers.ReaderFor = func(config.DeviceConfig) containers.Reader {
		t.Fatal("pure parse attempted RouterOS I/O")
		return nil
	}
	for _, tc := range []struct {
		method, device, source string
		status                 int
	}{
		{"POST", "a", "services: {app: {image: nginx}}", 200},
		{"POST", "b", "services: {app: {image: nginx}}", 200},
		{"POST", "", "", 400}, {"POST", "missing", "", 404},
		{"POST", "disabled", "", 404}, {"POST", "archived", "", 404},
		{"GET", "a", "", 405}, {"PUT", "a", "", 405},
		{"POST", "a", "services: [", 400},
		{"POST", "a", strings.Repeat("x", 70<<10), 400},
	} {
		body, _ := json.Marshal(map[string]string{"source": tc.source})
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(tc.method, "/api/containers/compose/parse?device="+tc.device, bytes.NewReader(body)))
		if w.Code != tc.status {
			t.Fatalf("%s device=%s => %d", tc.method, tc.device, w.Code)
		}
		if tc.status == 200 && !strings.Contains(w.Body.String(), `"image":"nginx"`) {
			t.Fatal("missing parsed fields")
		}
		if tc.status == 405 && w.Header().Get("Allow") != "POST" {
			t.Fatal("missing allowed method")
		}
	}
}

func TestComposeParseRetainsAuthenticationBoundary(t *testing.T) {
	s, _ := newAuthServer(t, []string{"127.0.0.0/8"})
	w := authRequest(t, s, http.MethodPost, "/api/containers/compose/parse?device=a", `{"source":"services: {app: {image: nginx}}"}`, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated parser returned %d", w.Code)
	}
}
