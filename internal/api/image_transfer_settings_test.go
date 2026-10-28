package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rosboard/internal/config"
)

func TestImageTransferSettingsRemainPrivateAndIdentityBound(t *testing.T) {
	d := config.DeviceConfig{ID: "lab", Enabled: true, ContainerSSHHostKey: "SHA256:fixture", ContainerTransfer: &config.ContainerTransferConfig{Username: "transfer", Password: "private-transfer-fixture", HostKey: "SHA256:fixture", Port: 2222}, RouterOS: config.RouterOSConfig{BaseURL: "http://127.0.0.1", Username: "monitor", Password: "private-monitor-fixture"}}
	s := NewServer(config.Config{Path: filepath.Join(t.TempDir(), "config.yaml"), Devices: []config.DeviceConfig{d}}, nil, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/devices/lab/image-transfer", nil)
	r.RemoteAddr = "127.0.0.1:1"
	s.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-transfer-fixture") || strings.Contains(w.Body.String(), "private-monitor-fixture") {
		t.Fatal("private transfer credentials exposed")
	}
	call := func(body string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/devices/lab/image-transfer", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1"
		s.ServeHTTP(w, r)
		return w.Code
	}
	if call(`{"clear":true,"revision":"stale"}`) != 409 {
		t.Fatal("stale identity accepted")
	}
	body, _ := json.Marshal(map[string]any{"revision": transferSettingsRevision(d), "clear": true})
	if call(string(body)) != 200 {
		t.Fatal("clear failed")
	}
	saved, _ := s.configSnapshot().Device("lab")
	if saved.ContainerSSHHostKey != "" || saved.ContainerTransfer != nil {
		t.Fatal("clearing transfer retained or granted authority")
	}
}
func TestImageTransferIndependentLab(t *testing.T) {
	path := os.Getenv("ROSBOARD_HOST_IMAGE_LAB_CONFIG")
	if path == "" {
		t.Skip("lab read-only verification opt-in")
	}
	var cfg struct{ BaseURL, Username, Password, HostKey string }
	data, e := os.ReadFile(path)
	if e != nil || json.Unmarshal(data, &cfg) != nil || cfg.BaseURL != "http://10.0.2.5" {
		t.Fatal("private independent lab required")
	}
	d := config.DeviceConfig{ID: "lab", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: cfg.BaseURL, Username: cfg.Username, Password: cfg.Password}}
	s := NewServer(config.Config{Path: filepath.Join(t.TempDir(), "config.yaml"), Devices: []config.DeviceConfig{d, {ID: "other", Enabled: true}}}, nil, nil)
	for _, mode := range []string{"connection", "dedicated", "connection"} {
		current, _ := s.configSnapshot().Device("lab")
		body, _ := json.Marshal(map[string]any{"mode": mode, "username": cfg.Username, "password": cfg.Password, "port": 22, "hostKey": cfg.HostKey, "revision": transferSettingsRevision(current)})
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/devices/lab/image-transfer", strings.NewReader(string(body)))
		r.RemoteAddr = "127.0.0.1:1"
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("source switch %s status=%d", mode, w.Code)
		}
		if strings.Contains(w.Body.String(), cfg.Password) {
			t.Fatal("password exposed")
		}
		saved, _ := s.configSnapshot().Device("lab")
		if (saved.ContainerTransfer != nil) != (mode == "dedicated") {
			t.Fatal("credential source not saved")
		}
		other, _ := s.configSnapshot().Device("other")
		if other.ContainerTransfer != nil || other.ContainerSSHHostKey != "" {
			t.Fatal("cross-device authority leak")
		}
	}
}
