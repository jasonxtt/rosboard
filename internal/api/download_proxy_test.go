package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"rosboard/internal/config"
	"strings"
	"testing"
)

func TestProxySettingsRedactionRevisionAndClear(t *testing.T) {
	cfg := config.Config{Path: filepath.Join(t.TempDir(), "config.yaml"), DownloadProxyURL: "http://user:private-proxy-fixture@proxy.example:7890"}
	s := NewServer(cfg, nil, nil)
	call := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/settings/download-proxy", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		s.ServeHTTP(w, r)
		return w
	}
	w := call("GET", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-proxy-fixture") {
		t.Fatal("proxy secret exposed")
	}
	var status struct {
		DisplayURL, Revision string
		PasswordSet          bool
	}
	json.Unmarshal(w.Body.Bytes(), &status)
	if !status.PasswordSet {
		t.Fatal("missing saved status")
	}
	w = call("PUT", `{"url":"http://user:xxxxx@proxy.example:7890"}`)
	if w.Code != 400 {
		t.Fatal("mask persisted")
	}
	w = call("PUT", `{"useSaved":true,"revision":"stale"}`)
	if w.Code != 409 {
		t.Fatal("stale secret reused")
	}
	body, _ := json.Marshal(map[string]any{"useSaved": true, "revision": status.Revision})
	if call("PUT", string(body)).Code != http.StatusOK {
		t.Fatal("unchanged saved config failed")
	}
	if s.configSnapshot().DownloadProxyURL != cfg.DownloadProxyURL {
		t.Fatal("saved secret changed")
	}
	if call("PUT", `{"url":""}`).Code != 200 || s.configSnapshot().DownloadProxyURL != "" {
		t.Fatal("empty did not restore explicit direct mode")
	}
}
