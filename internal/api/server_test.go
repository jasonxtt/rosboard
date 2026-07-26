package api

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/service"
)

func TestViewerHeartbeatRequiresPostAndReturnsDeadline(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServer(config.Config{}, monitor, nil)

	getResponse := httptest.NewRecorder()
	server.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/viewer-heartbeat", nil))
	if getResponse.Code != http.StatusMethodNotAllowed || getResponse.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET status=%d allow=%q", getResponse.Code, getResponse.Header().Get("Allow"))
	}

	before := time.Now()
	postResponse := httptest.NewRecorder()
	server.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, "/api/viewer-heartbeat", nil))
	if postResponse.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", postResponse.Code, postResponse.Body.String())
	}
	var payload struct {
		ActiveUntil time.Time `json:"activeUntil"`
	}
	if err := json.Unmarshal(postResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.ActiveUntil.After(before.Add(20 * time.Second)) {
		t.Fatalf("heartbeat deadline was not extended: %s", payload.ActiveUntil)
	}
}

func TestSettingsReturnsEffectiveConfig(t *testing.T) {
	cfg := config.Config{
		ListenAddress:               "127.0.0.1:8080",
		PollIntervalSeconds:         10,
		RealtimePollIntervalSeconds: 1,
		TerminalPollIntervalSeconds: 3,
		SampleRetentionHours:        48,
		AllowedCIDRs:                []string{"127.0.0.0/8", "::1/128"},
		RouterOS: config.RouterOSConfig{
			BaseURL:           "http://router.test",
			Username:          "admin",
			Password:          "super-secret",
			TrafficInterfaces: []string{"pppoe-out1"},
		},
	}
	monitor := service.NewMonitor(cfg, nil, nil, log.Default())
	server := NewServer(cfg, monitor, nil)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Connection struct {
			APIBasePath         string   `json:"apiBasePath"`
			Configured          bool     `json:"configured"`
			ListenAddress       string   `json:"listenAddress"`
			AllowedCIDRs        []string `json:"allowedCidrs"`
			RouterOSBaseURL     string   `json:"routerosBaseUrl"`
			RouterOSScheme      string   `json:"routerosScheme"`
			RouterOSHost        string   `json:"routerosHost"`
			RouterOSPort        int      `json:"routerosPort"`
			RouterOSUsername    string   `json:"routerosUsername"`
			RouterOSPassword    string   `json:"routerosPassword"`
			RouterOSPasswordSet bool     `json:"routerosPasswordSet"`
		} `json:"connection"`
		Collection struct {
			PollIntervalSeconds         int      `json:"pollIntervalSeconds"`
			RealtimePollIntervalSeconds int      `json:"realtimePollIntervalSeconds"`
			TerminalPollIntervalSeconds int      `json:"terminalPollIntervalSeconds"`
			SampleRetentionHours        int      `json:"sampleRetentionHours"`
			TrafficInterfaces           []string `json:"trafficInterfaces"`
			TerminalCIDRs               []string `json:"terminalCidrs"`
		} `json:"collection"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Connection.APIBasePath != "/api" ||
		!payload.Connection.Configured ||
		payload.Connection.ListenAddress != cfg.ListenAddress ||
		payload.Connection.RouterOSBaseURL != cfg.RouterOS.BaseURL ||
		payload.Connection.RouterOSScheme != "http" ||
		payload.Connection.RouterOSHost != "router.test" ||
		payload.Connection.RouterOSPort != 80 ||
		payload.Connection.RouterOSUsername != cfg.RouterOS.Username ||
		payload.Connection.RouterOSPassword != cfg.RouterOS.Password ||
		!payload.Connection.RouterOSPasswordSet {
		t.Fatalf("unexpected connection settings: %+v", payload.Connection)
	}
	if len(payload.Connection.AllowedCIDRs) != 2 || payload.Connection.AllowedCIDRs[1] != "::1/128" {
		t.Fatalf("unexpected cidrs: %+v", payload.Connection.AllowedCIDRs)
	}
	if payload.Collection.PollIntervalSeconds != cfg.PollIntervalSeconds ||
		payload.Collection.RealtimePollIntervalSeconds != cfg.RealtimePollIntervalSeconds ||
		payload.Collection.TerminalPollIntervalSeconds != cfg.TerminalPollIntervalSeconds ||
		payload.Collection.SampleRetentionHours != cfg.SampleRetentionHours {
		t.Fatalf("unexpected collection settings: %+v", payload.Collection)
	}
	if len(payload.Collection.TrafficInterfaces) != 1 || payload.Collection.TrafficInterfaces[0] != "pppoe-out1" {
		t.Fatalf("unexpected traffic interfaces: %+v", payload.Collection.TrafficInterfaces)
	}
	if payload.Collection.TerminalCIDRs == nil || len(payload.Collection.TerminalCIDRs) != 0 {
		t.Fatalf("unexpected terminal cidrs: %+v", payload.Collection.TerminalCIDRs)
	}
}

func TestConnectionSettingsPostSavesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{
		Path:                        path,
		ListenAddress:               "127.0.0.1:8080",
		DataDir:                     "./data",
		PollIntervalSeconds:         10,
		RealtimePollIntervalSeconds: 1,
		TerminalPollIntervalSeconds: 3,
		SampleRetentionHours:        48,
	}
	server := NewServer(cfg, nil, nil)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/settings/connection", strings.NewReader(`{"scheme":"https","host":"10.0.0.6","port":443,"username":"admin","password":"secret-key"}`))
	request.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !strings.Contains(text, "base_url: https://10.0.0.6:443") ||
		!strings.Contains(text, "username: admin") ||
		!strings.Contains(text, "password: secret-key") {
		t.Fatalf("saved config missing connection fields:\n%s", text)
	}
}

func TestCollectionSettingsPostSavesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{
		Path:                        path,
		ListenAddress:               "127.0.0.1:8080",
		DataDir:                     "./data",
		PollIntervalSeconds:         10,
		RealtimePollIntervalSeconds: 1,
		TerminalPollIntervalSeconds: 3,
		SampleRetentionHours:        48,
	}
	server := NewServer(cfg, nil, nil)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/settings/collection", strings.NewReader(`{
		"pollIntervalSeconds":15,
		"realtimePollIntervalSeconds":2,
		"terminalPollIntervalSeconds":5,
		"sampleRetentionHours":72,
		"trafficInterfaces":[" pppoe-out1 ","ether1","ether1"],
		"terminalCidrs":["10.0.0.0/24",""]
	}`))
	request.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{
		"poll_interval_seconds: 15",
		"realtime_poll_interval_seconds: 2",
		"terminal_poll_interval_seconds: 5",
		"sample_retention_hours: 72",
		"- pppoe-out1",
		"- ether1",
		"- 10.0.0.0/24",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("saved config missing %q:\n%s", expected, text)
		}
	}
	if strings.Count(text, "- ether1") != 1 {
		t.Fatalf("saved config did not de-duplicate interfaces:\n%s", text)
	}
}

func TestCollectionSettingsRejectsNonPositiveValues(t *testing.T) {
	server := NewServer(config.Config{Path: filepath.Join(t.TempDir(), "config.yaml")}, nil, nil)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/settings/collection", strings.NewReader(`{
		"pollIntervalSeconds":0,
		"realtimePollIntervalSeconds":1,
		"terminalPollIntervalSeconds":3,
		"sampleRetentionHours":48
	}`))
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRestartSettingsSchedulesRestart(t *testing.T) {
	restarted := make(chan struct{}, 1)
	server := NewServerWithRestart(config.Config{}, nil, nil, func() { restarted <- struct{}{} })
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/settings/restart", nil)
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("restart callback was not called")
	}
}
