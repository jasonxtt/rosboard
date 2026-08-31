package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"rosboard/internal/applicationcatalog"
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

func TestApplicationCatalogStatusRoute(t *testing.T) {
	server := NewServer(config.Config{}, nil, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/application-catalog", nil))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"lastSuccess"`) {
		t.Fatalf("unconfigured catalog status=%d body=%s", response.Code, response.Body.String())
	}

	path := filepath.Join(t.TempDir(), "feature.cfg")
	if err := os.WriteFile(path, []byte(`#version v1
#format v3.0
1101 Example:[tcp;;;example.com;;]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := applicationcatalog.New(path, time.Hour)
	if err := catalog.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.SetApplicationCatalog(catalog)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/application-catalog", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", response.Code, response.Body.String())
	}
	var status applicationcatalog.CatalogStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Source != path || status.LastSuccess == nil || status.Version != "v1" || status.ApplicationCount != 1 || status.DomainCount != 1 {
		t.Fatalf("unexpected catalog status: %+v", status)
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/application-catalog", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST catalog status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestMosDNSRoutesRequireDeviceID(t *testing.T) {
	server := NewServer(config.Config{ProtocolAnalysis: config.ProtocolAnalysisConfig{Enabled: true}}, nil, nil)

	missingResponse := httptest.NewRecorder()
	server.ServeHTTP(missingResponse, httptest.NewRequest(http.MethodGet, "/api/mosdns", nil))
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("MosDNS status without deviceId: status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}

	statusResponse := httptest.NewRecorder()
	server.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/api/mosdns?deviceId=missing", nil))
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("MosDNS status route failed: status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}

	observationsMissing := httptest.NewRecorder()
	server.ServeHTTP(observationsMissing, httptest.NewRequest(http.MethodGet, "/api/mosdns/observations", nil))
	if observationsMissing.Code != http.StatusBadRequest {
		t.Fatalf("MosDNS observations without deviceId: status=%d body=%s", observationsMissing.Code, observationsMissing.Body.String())
	}

	observationsResponse := httptest.NewRecorder()
	server.ServeHTTP(observationsResponse, httptest.NewRequest(http.MethodGet, "/api/mosdns/observations?deviceId=missing", nil))
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
		Devices: []config.DeviceConfig{{
			ID:               config.DefaultDeviceID,
			Name:             "RouterOS",
			Enabled:          true,
			RouterOS:         config.RouterOSConfig{BaseURL: "http://router.test", Username: "admin", Password: "super-secret", TrafficInterfaces: []string{"pppoe-out1"}},
			ProtocolAnalysis: true,
			FeatureLibrary:   config.FeatureLibraryConfig{Enabled: true, SourceURL: "https://example.test/library.yml"},
		}},
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
		ProtocolAnalysis struct {
			Enabled bool `json:"enabled"`
		} `json:"protocolAnalysis"`
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
	if payload.ProtocolAnalysis.Enabled || !strings.Contains(response.Body.String(), `"protocolAnalysis":true`) {
		t.Fatalf("unexpected protocol analysis projection: %+v", payload.ProtocolAnalysis)
	}
	if !strings.Contains(response.Body.String(), `"featureLibrary":{"enabled":true`) {
		t.Fatalf("device projection must preserve stored feature library toggle: %s", response.Body.String())
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

func TestRecognitionSettingsPostSavesPerDeviceRecognition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{
		Path: path, DataDir: t.TempDir(), PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		Devices: []config.DeviceConfig{
			{ID: "edge", Name: "Edge", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://edge.test", Username: "u", Password: "p"}},
			{ID: "core", Name: "Core", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://core.test", Username: "u", Password: "p"}},
		},
	}
	server := NewServer(cfg, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/recognition", strings.NewReader(`{
		"devices":[
			{"id":"edge","protocolAnalysis":true,"mosdns":{"enabled":true,"baseUrl":"10.0.0.4","syncIntervalMinutes":15},"featureLibrary":{"enabled":true,"sourceUrl":"https://example.test/updated.yml","refreshIntervalHours":24,"matchWindowMinutes":45}},
			{"id":"core","protocolAnalysis":true,"mosdns":{"enabled":false,"baseUrl":"","syncIntervalMinutes":30},"featureLibrary":{"enabled":false,"sourceUrl":"","refreshIntervalHours":168,"matchWindowMinutes":30}}
		]
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
	edge, found := loaded.Device("edge")
	if !found || !edge.ProtocolAnalysis {
		t.Fatalf("edge protocol analysis was not saved: %#v", edge)
	}
	if !edge.MosDNS.Enabled || edge.MosDNS.BaseURL != "http://10.0.0.4" || edge.MosDNS.SyncIntervalMinutes != 15 {
		t.Fatalf("edge device MosDNS was not saved: %#v", edge.MosDNS)
	}
	if !edge.FeatureLibrary.Enabled || edge.FeatureLibrary.SourceURL != "https://example.test/updated.yml" || edge.FeatureLibrary.RefreshIntervalHours != 24 || edge.FeatureLibrary.MatchWindowMinutes != 45 {
		t.Fatalf("edge device feature library was not saved: %#v", edge.FeatureLibrary)
	}
	core, found := loaded.Device("core")
	if !found || core.MosDNS.Enabled || core.FeatureLibrary.Enabled {
		t.Fatalf("core device recognition settings were not saved: %#v", core)
	}
}

func TestRecognitionSettingsMasterSwitchDisablesChildren(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{
		Path: path, DataDir: t.TempDir(), PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		Devices: []config.DeviceConfig{
			{ID: "edge", Name: "Edge", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://edge.test", Username: "u", Password: "p"}, ProtocolAnalysis: true},
		},
	}
	server := NewServer(cfg, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/recognition", strings.NewReader(`{
		"devices":[{"id":"edge","protocolAnalysis":false,"mosdns":{"enabled":true,"baseUrl":"10.0.0.3","syncIntervalMinutes":30},"featureLibrary":{"enabled":true,"sourceUrl":"https://example.test/library.yml","refreshIntervalHours":168,"matchWindowMinutes":30}}]
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
	edge, found := loaded.Device("edge")
	if !found {
		t.Fatal("edge device disappeared")
	}
	if edge.ProtocolAnalysis || edge.MosDNS.Enabled || edge.FeatureLibrary.Enabled {
		t.Fatalf("per-device protocol analysis disable must force child toggles off: %#v", edge)
	}
}

func TestProtocolAPIGatesPerDevice(t *testing.T) {
	server := NewServer(config.Config{
		Devices: []config.DeviceConfig{
			{ID: "edge", Name: "Edge", Enabled: true, ProtocolAnalysis: true, RouterOS: config.RouterOSConfig{BaseURL: "http://edge.test", Username: "u", Password: "p"}},
			{ID: "core", Name: "Core", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://core.test", Username: "u", Password: "p"}},
		},
	}, nil, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/protocols?device=core", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("protocol API status=%d body=%s", response.Code, response.Body.String())
	}
	var protocols struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &protocols); err != nil {
		t.Fatal(err)
	}
	if protocols.Enabled {
		t.Fatalf("device without protocol analysis must report enabled=false: %#v", protocols)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/protocols?device=edge", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	// With the per-device switch on, the request proceeds to the monitor path;
	// without a monitor it degrades to setup-required instead of the disabled payload.
	if response.Code == http.StatusOK && strings.Contains(response.Body.String(), `"enabled":false`) {
		t.Fatalf("device with protocol analysis must not receive the disabled payload: %s", response.Body.String())
	}

	recognitionMissing := httptest.NewRecorder()
	server.ServeHTTP(recognitionMissing, httptest.NewRequest(http.MethodGet, "/api/recognition", nil))
	if recognitionMissing.Code != http.StatusBadRequest {
		t.Fatalf("recognition status without deviceId: status=%d", recognitionMissing.Code)
	}
	recognitionResponse := httptest.NewRecorder()
	server.ServeHTTP(recognitionResponse, httptest.NewRequest(http.MethodGet, "/api/recognition?deviceId=edge", nil))
	if recognitionResponse.Code != http.StatusOK || !strings.Contains(recognitionResponse.Body.String(), `"protocolAnalysis":true`) {
		t.Fatalf("recognition status=%d body=%s", recognitionResponse.Code, recognitionResponse.Body.String())
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

func TestSettingsProjectionExcludesRetiredPolicyAccess(t *testing.T) {
	server := NewServer(config.Config{Devices: []config.DeviceConfig{{
		ID: "edge", Name: "Edge", Enabled: true,
		RouterOS: config.RouterOSConfig{BaseURL: "http://router.test", Username: "monitor", Password: "monitor-secret"},
	}}}, nil, nil)

	payload, err := json.Marshal(server.settingsResponse())
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	if strings.Contains(body, "monitor-secret") || strings.Contains(body, "policyAccess") {
		t.Fatalf("settings projection leaked a password: %s", body)
	}
}

func TestDeleteDeviceAccountClearsCredentialsAndDisablesDevice(t *testing.T) {
	dir := t.TempDir()
	restarted := make(chan struct{}, 1)
	cfg := config.Config{
		Path: filepath.Join(dir, "config.yaml"), ListenAddress: ":8080", DataDir: dir,
		PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		Devices: []config.DeviceConfig{{
			ID: "edge", Name: "Edge", Enabled: true,
			RouterOS:       config.RouterOSConfig{BaseURL: "http://10.0.0.1", Username: "rosboard_old", Password: "secret"},
			ManagedAccount: &config.ManagedRouterOSAccount{Username: "rosboard_old", GroupName: "rosboard_g_old"},
		}},
	}
	server := NewServerWithRestart(cfg, nil, nil, func() { restarted <- struct{}{} })
	request := httptest.NewRequest(http.MethodDelete, "/api/devices/edge/account", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	device, found := server.configSnapshot().Device("edge")
	if !found || device.Enabled || device.RouterOS.Username != "" || device.RouterOS.Password != "" || device.ManagedAccount != nil {
		t.Fatalf("device account was not cleared safely: %#v found=%v", device, found)
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("device restart was not scheduled")
	}
	loaded, err := config.Load(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, found := loaded.Device("edge")
	if !found || persisted.Enabled || persisted.RouterOS.Username != "" || persisted.RouterOS.Password != "" || persisted.ManagedAccount != nil {
		t.Fatalf("persisted device account was not cleared: %#v found=%v", persisted, found)
	}
}

func newDeviceOrderTestServer(t *testing.T, devices []config.DeviceConfig) (*Server, string, *int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{
		Path:                        path,
		ListenAddress:               "127.0.0.1:8080",
		DataDir:                     t.TempDir(),
		PollIntervalSeconds:         10,
		RealtimePollIntervalSeconds: 1,
		TerminalPollIntervalSeconds: 3,
		SampleRetentionHours:        48,
		Devices:                     devices,
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	restarts := 0
	server := NewServerWithManager(loaded, nil, nil, nil, func() { restarts++ })
	return server, path, &restarts
}

func orderedDeviceIDs(t *testing.T, body string) []string {
	t.Helper()
	var payload struct {
		Devices []struct {
			ID string `json:"id"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(payload.Devices))
	for _, device := range payload.Devices {
		ids = append(ids, device.ID)
	}
	return ids
}

func settingsDeviceIDs(t *testing.T, body string) []string {
	t.Helper()
	var payload struct {
		Devices []struct {
			ID string `json:"id"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(payload.Devices))
	for _, device := range payload.Devices {
		ids = append(ids, device.ID)
	}
	return ids
}

func TestDeviceReorderPersistsOrderSkipsRestartAndAlignsResponses(t *testing.T) {
	server, path, restarts := newDeviceOrderTestServer(t, []config.DeviceConfig{
		{ID: "alpha", Name: "Alpha", Enabled: true, SortOrder: 1},
		{ID: "bravo", Name: "Bravo", Enabled: true, SortOrder: 2},
		{ID: "charlie", Name: "Charlie", Enabled: true, SortOrder: 3},
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/devices/reorder", strings.NewReader(`{"deviceIds":["charlie","alpha","bravo"]}`))
	request.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Restarting bool `json:"restarting"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Restarting {
		t.Fatal("reorder must not restart the panel")
	}
	if *restarts != 0 {
		t.Fatalf("reorder scheduled %d restarts", *restarts)
	}

	deviceResponse := httptest.NewRecorder()
	deviceRequest := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	deviceRequest.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(deviceResponse, deviceRequest)
	if deviceResponse.Code != http.StatusOK {
		t.Fatalf("devices status=%d body=%s", deviceResponse.Code, deviceResponse.Body.String())
	}
	wantOrder := []string{"charlie", "alpha", "bravo"}
	if got := orderedDeviceIDs(t, deviceResponse.Body.String()); !equalStrings(got, wantOrder) {
		t.Fatalf("/api/devices order = %v, want %v", got, wantOrder)
	}

	settingsResponse := httptest.NewRecorder()
	settingsRequest := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	settingsRequest.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(settingsResponse, settingsRequest)
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", settingsResponse.Code, settingsResponse.Body.String())
	}
	if got := settingsDeviceIDs(t, settingsResponse.Body.String()); !equalStrings(got, wantOrder) {
		t.Fatalf("/api/settings device order = %v, want %v", got, wantOrder)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for index, wantID := range wantOrder {
		device := loaded.Devices[index]
		if device.ID != wantID || device.SortOrder != index+1 {
			t.Fatalf("persisted device[%d] = %q sort %d, want %q sort %d (all: %+v)", index, device.ID, device.SortOrder, wantID, index+1, loaded.Devices)
		}
	}
}

func TestDeviceReorderRejectsInvalidRequestsWithoutChangingConfig(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "duplicate", body: `{"deviceIds":["alpha","alpha","charlie"]}`},
		{name: "unknown", body: `{"deviceIds":["alpha","charlie","ghost"]}`},
		{name: "missing", body: `{"deviceIds":["alpha"]}`},
		{name: "empty", body: `{"deviceIds":[]}`},
		{name: "archived included", body: `{"deviceIds":["alpha","charlie","bravo"]}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server, path, restarts := newDeviceOrderTestServer(t, []config.DeviceConfig{
				{ID: "alpha", Name: "Alpha", Enabled: true, SortOrder: 1},
				{ID: "charlie", Name: "Charlie", Enabled: true, SortOrder: 2},
				{ID: "bravo", Name: "Bravo", Enabled: false, Archived: true, SortOrder: 3},
			})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/api/devices/reorder", strings.NewReader(testCase.body))
			request.RemoteAddr = "127.0.0.1:12345"
			server.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if *restarts != 0 {
				t.Fatalf("rejected reorder scheduled %d restarts", *restarts)
			}
			loaded, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Devices) != 3 || loaded.Devices[0].ID != "alpha" || loaded.Devices[0].SortOrder != 1 {
				t.Fatalf("rejected reorder changed persisted config: %+v", loaded.Devices)
			}
		})
	}
}

func TestDeviceReorderAllowsEmptyListWithoutDevices(t *testing.T) {
	server, _, _ := newDeviceOrderTestServer(t, nil)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/devices/reorder", strings.NewReader(`{"deviceIds":[]}`))
	request.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeviceCreateAppendsTailOrderAndEditPreservesIt(t *testing.T) {
	routerOS := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/rest/interface" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`[{"name":"ether1","running":"true","disabled":"false"}]`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer routerOS.Close()
	endpoint, err := url.Parse(routerOS.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(endpoint.Port())
	if err != nil {
		t.Fatal(err)
	}

	server, path, _ := newDeviceOrderTestServer(t, []config.DeviceConfig{
		{ID: "alpha", Name: "Alpha", Enabled: true, SortOrder: 1},
		{ID: "bravo", Name: "Bravo", Enabled: true, SortOrder: 2},
	})

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/devices", strings.NewReader(fmt.Sprintf(`{"name":"Charlie","scheme":"http","host":"%s","port":%d,"username":"u","password":"p","enabled":true,"deferRestart":true}`, endpoint.Hostname(), port)))
	createRequest.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	charlie, found := loaded.Device(created.ID)
	if !found {
		t.Fatalf("created device missing from config: %+v", loaded.Devices)
	}
	// config.Load normalizes the slice into display order, so the new device
	// must be last with the next sort position.
	last := loaded.Devices[len(loaded.Devices)-1]
	if last.ID != created.ID || charlie.SortOrder != 3 {
		t.Fatalf("created device must be appended at the tail, got %+v", loaded.Devices)
	}

	editResponse := httptest.NewRecorder()
	editRequest := httptest.NewRequest(http.MethodPut, "/api/devices/"+url.PathEscape(created.ID), strings.NewReader(fmt.Sprintf(`{"name":"Charlie Renamed","scheme":"http","host":"%s","port":%d,"username":"u","enabled":true,"deferRestart":true}`, endpoint.Hostname(), port)))
	editRequest.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(editResponse, editRequest)
	if editResponse.Code != http.StatusOK {
		t.Fatalf("edit status=%d body=%s", editResponse.Code, editResponse.Body.String())
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	edited, found := reloaded.Device(created.ID)
	if !found || edited.SortOrder != 3 || edited.Name != "Charlie Renamed" {
		t.Fatalf("editing a device must preserve its sort order, got %+v", reloaded.Devices)
	}
}

func TestDeviceArchiveAndRestoreAffectOrder(t *testing.T) {
	server, path, _ := newDeviceOrderTestServer(t, []config.DeviceConfig{
		{ID: "alpha", Name: "Alpha", Enabled: true, SortOrder: 1},
		{ID: "bravo", Name: "Bravo", Enabled: true, SortOrder: 2},
		{ID: "charlie", Name: "Charlie", Enabled: true, SortOrder: 3},
	})

	archiveResponse := httptest.NewRecorder()
	archiveRequest := httptest.NewRequest(http.MethodDelete, "/api/devices/bravo", nil)
	archiveRequest.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(archiveResponse, archiveRequest)
	if archiveResponse.Code != http.StatusOK {
		t.Fatalf("archive status=%d body=%s", archiveResponse.Code, archiveResponse.Body.String())
	}

	deviceResponse := httptest.NewRecorder()
	deviceRequest := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	deviceRequest.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(deviceResponse, deviceRequest)
	if got := orderedDeviceIDs(t, deviceResponse.Body.String()); !equalStrings(got, []string{"alpha", "charlie"}) {
		t.Fatalf("archived device must vanish from /api/devices, got %v", got)
	}

	restoreResponse := httptest.NewRecorder()
	restoreRequest := httptest.NewRequest(http.MethodPost, "/api/devices/bravo/restore", nil)
	restoreRequest.RemoteAddr = "127.0.0.1:12345"
	server.ServeHTTP(restoreResponse, restoreRequest)
	if restoreResponse.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restoreResponse.Code, restoreResponse.Body.String())
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	bravo, found := loaded.Device("bravo")
	if !found || bravo.Archived {
		t.Fatalf("restored device must be active again: %#v found=%v", bravo, found)
	}
	if bravo.SortOrder != 4 || loaded.Devices[len(loaded.Devices)-1].ID != "bravo" {
		t.Fatalf("restored device must re-join at the tail, got %+v", loaded.Devices)
	}
}

func equalStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
