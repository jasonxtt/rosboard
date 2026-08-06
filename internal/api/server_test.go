package api

import (
	"context"
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
	"rosboard/internal/store"
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

func TestFleetOverviewRouteIsReadOnlyAndAvailableWithoutDevices(t *testing.T) {
	server := NewServer(config.Config{}, nil, nil)

	getResponse := httptest.NewRecorder()
	server.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/fleet-overview", nil))
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"devices":[]`) {
		t.Fatalf("GET status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}

	postResponse := httptest.NewRecorder()
	server.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, "/api/fleet-overview", nil))
	if postResponse.Code != http.StatusMethodNotAllowed || postResponse.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST status=%d allow=%q", postResponse.Code, postResponse.Header().Get("Allow"))
	}
}

func TestMosDNSRoutesAreAvailableWithoutRouterOSMonitor(t *testing.T) {
	cfg := config.Config{MosDNS: config.MosDNSConfig{Enabled: true, BaseURL: "http://10.0.0.3", SyncIntervalMinutes: 30}}
	server := NewServer(cfg, nil, nil)

	statusResponse := httptest.NewRecorder()
	server.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/api/mosdns", nil))
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"enabled":true`) {
		t.Fatalf("MosDNS status route failed: status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}

	observationsResponse := httptest.NewRecorder()
	server.ServeHTTP(observationsResponse, httptest.NewRequest(http.MethodGet, "/api/mosdns/observations", nil))
	if observationsResponse.Code != http.StatusOK || !strings.Contains(observationsResponse.Body.String(), `"observations":[]`) {
		t.Fatalf("MosDNS observations route failed: status=%d body=%s", observationsResponse.Code, observationsResponse.Body.String())
	}
}

func TestTerminalViewerHeartbeatRequiresPostAndReturnsDeadline(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServer(config.Config{}, monitor, nil)

	getResponse := httptest.NewRecorder()
	server.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/terminal-viewer-heartbeat", nil))
	if getResponse.Code != http.StatusMethodNotAllowed || getResponse.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET status=%d allow=%q", getResponse.Code, getResponse.Header().Get("Allow"))
	}

	before := time.Now()
	postResponse := httptest.NewRecorder()
	server.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, "/api/terminal-viewer-heartbeat", nil))
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
		t.Fatalf("terminal heartbeat deadline was not extended: %s", payload.ActiveUntil)
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
			RouterOSPasswordSet bool     `json:"routerosPasswordSet"`
		} `json:"connection"`
		Collection struct {
			PollIntervalSeconds         int `json:"pollIntervalSeconds"`
			RealtimePollIntervalSeconds int `json:"realtimePollIntervalSeconds"`
			TerminalPollIntervalSeconds int `json:"terminalPollIntervalSeconds"`
			SampleRetentionHours        int `json:"sampleRetentionHours"`
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
		!payload.Connection.RouterOSPasswordSet {
		t.Fatalf("unexpected connection settings: %+v", payload.Connection)
	}
	if strings.Contains(response.Body.String(), "super-secret") || strings.Contains(response.Body.String(), "routerosPassword\"") {
		t.Fatalf("settings response exposed RouterOS password: %s", response.Body.String())
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
}

func TestConnectionSettingsPostRequiresVerifiedDeviceAPI(t *testing.T) {
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
	if response.Code != http.StatusGone {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy endpoint unexpectedly saved config: %v", err)
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
		Devices: []config.DeviceConfig{{
			ID:      "edge",
			Name:    "Edge",
			Enabled: true,
			RouterOS: config.RouterOSConfig{
				BaseURL:           "http://10.0.0.1:80",
				Username:          "admin",
				Password:          "secret",
				TrafficInterfaces: []string{"pppoe-out1"},
				TerminalCIDRs:     []string{"10.0.0.0/24"},
			},
		}},
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
		"- 10.0.0.0/24",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("saved config missing %q:\n%s", expected, text)
		}
	}
	if strings.Count(text, "- ether1") != 0 {
		t.Fatalf("collection save should not persist submitted per-device interface values:\n%s", text)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	device, ok := loaded.Device("edge")
	if !ok {
		t.Fatal("device was not preserved")
	}
	if strings.Join(device.RouterOS.TrafficInterfaces, ",") != "pppoe-out1" || strings.Join(device.RouterOS.TerminalCIDRs, ",") != "10.0.0.0/24" {
		t.Fatalf("collection save mutated device scopes: %#v", device.RouterOS)
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

func TestRecognitionSettingsPostSavesIndependentToggles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{
		Path: path, DataDir: t.TempDir(), PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		MosDNS:         config.MosDNSConfig{Enabled: true, BaseURL: "http://10.0.0.3", SyncIntervalMinutes: 30},
		FeatureLibrary: config.FeatureLibraryConfig{Enabled: true, SourceURL: "https://example.test/library.yml", RefreshIntervalHours: 168, MatchWindowMinutes: 30},
	}
	server := NewServer(cfg, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/recognition", strings.NewReader(`{
		"mosdns":{"enabled":false,"baseUrl":"http://10.0.0.3","syncIntervalMinutes":60},
		"featureLibrary":{"enabled":true,"sourceUrl":"https://example.test/updated.yml","refreshIntervalHours":24,"matchWindowMinutes":45}
	}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MosDNS.Enabled || loaded.MosDNS.SyncIntervalMinutes != 60 {
		t.Fatalf("MosDNS settings were not saved: %#v", loaded.MosDNS)
	}
	if !loaded.FeatureLibrary.Enabled || loaded.FeatureLibrary.SourceURL != "https://example.test/updated.yml" || loaded.FeatureLibrary.RefreshIntervalHours != 24 || loaded.FeatureLibrary.MatchWindowMinutes != 45 {
		t.Fatalf("feature library settings were not saved: %#v", loaded.FeatureLibrary)
	}
}

func TestRecognitionSettingsAcceptsPlainMosDNSAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{
		Path: path, DataDir: t.TempDir(), PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		MosDNS:         config.MosDNSConfig{Enabled: false, SyncIntervalMinutes: 30},
		FeatureLibrary: config.FeatureLibraryConfig{Enabled: false, SourceURL: "https://example.test/library.yml", RefreshIntervalHours: 168, MatchWindowMinutes: 30},
	}
	server := NewServer(cfg, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/recognition", strings.NewReader(`{
		"mosdns":{"enabled":true,"baseUrl":"10.0.0.3","syncIntervalMinutes":30},
		"featureLibrary":{"enabled":false,"sourceUrl":"https://example.test/library.yml","refreshIntervalHours":168,"matchWindowMinutes":30}
	}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.MosDNS.Enabled || loaded.MosDNS.BaseURL != "http://10.0.0.3" {
		t.Fatalf("plain MosDNS address was not saved as a URL: %#v", loaded.MosDNS)
	}
}

func TestParseWindowSupportsOverviewRanges(t *testing.T) {
	tests := map[string]time.Duration{
		"5m":  5 * time.Minute,
		"1h":  time.Hour,
		"6h":  6 * time.Hour,
		"24h": 24 * time.Hour,
	}
	for value, expected := range tests {
		if got := parseWindow(value); got != expected {
			t.Errorf("parseWindow(%q)=%s, want %s", value, got, expected)
		}
	}
}

func TestLoadWindowBucketBoundsLongRanges(t *testing.T) {
	if got := loadWindowBucket("5m"); got != time.Minute {
		t.Fatalf("5m bucket=%s", got)
	}
	if got := loadWindowBucket("24h"); got != 4*time.Minute {
		t.Fatalf("24h bucket=%s", got)
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

func TestDHCPEndpointReturnsEmptyCollections(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServer(config.Config{}, monitor, nil)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/dhcp", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Servers []map[string]any `json:"servers"`
		Pools   []map[string]any `json:"pools"`
		Leases  []map[string]any `json:"leases"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Servers == nil || payload.Pools == nil || payload.Leases == nil {
		t.Fatalf("dhcp collections must be arrays, got %s", response.Body.String())
	}
}

func TestDeviceLifecycleArchivesBeforePurging(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	storage, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	cfg := config.Config{
		Path: filepath.Join(dir, "config.yaml"), ListenAddress: ":8080", DataDir: dir,
		PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		Devices: []config.DeviceConfig{{ID: "edge", Name: "Edge", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://10.0.0.1", Username: "admin", Password: "secret"}}},
	}
	if err := storage.ForDevice("edge").UpsertTerminal(ctx, "mac:test", "AA:BB:CC:DD:EE:FF", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithAuth(cfg, nil, storage, nil, nil, nil)

	archiveRecorder := httptest.NewRecorder()
	archive := httptest.NewRequest(http.MethodDelete, "/api/devices/edge", nil)
	archive.RemoteAddr = "127.0.0.1:1234"
	server.ServeHTTP(archiveRecorder, archive)
	if archiveRecorder.Code != http.StatusOK {
		t.Fatalf("archive status %d: %s", archiveRecorder.Code, archiveRecorder.Body.String())
	}
	if totals, err := storage.ForDevice("edge").TerminalTotals(ctx, []string{"mac:test"}); err != nil || len(totals) != 1 {
		t.Fatalf("archive removed history: totals=%#v err=%v", totals, err)
	}

	purgeRecorder := httptest.NewRecorder()
	purge := httptest.NewRequest(http.MethodDelete, "/api/devices/edge/data", strings.NewReader(`{"confirmation":"Edge"}`))
	purge.Header.Set("Content-Type", "application/json")
	purge.RemoteAddr = "127.0.0.1:1234"
	server.ServeHTTP(purgeRecorder, purge)
	if purgeRecorder.Code != http.StatusOK {
		t.Fatalf("purge status %d: %s", purgeRecorder.Code, purgeRecorder.Body.String())
	}
	if totals, err := storage.ForDevice("edge").TerminalTotals(ctx, []string{"mac:test"}); err != nil || len(totals) != 0 {
		t.Fatalf("purge retained history: totals=%#v err=%v", totals, err)
	}
	loaded, err := config.Load(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Devices) != 0 || loaded.RouterOSConfigured() {
		t.Fatalf("purged device remained configured: %#v", loaded.Devices)
	}
}
