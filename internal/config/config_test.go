package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsToTieredPollingIntervals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	payload := []byte("routeros:\n  base_url: http://router.test\n  username: test\n  password: test\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RealtimePollIntervalSeconds != 1 || cfg.TerminalPollIntervalSeconds != 3 || cfg.PollIntervalSeconds != 10 {
		t.Fatalf("unexpected polling defaults: realtime=%d terminal=%d full=%d", cfg.RealtimePollIntervalSeconds, cfg.TerminalPollIntervalSeconds, cfg.PollIntervalSeconds)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].ID != DefaultDeviceID || !cfg.Devices[0].Enabled {
		t.Fatalf("legacy routeros config was not normalized: %#v", cfg.Devices)
	}
}

func TestLoadMissingConfigStartsSetupDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != path {
		t.Fatalf("expected config path to be retained, got %q", cfg.Path)
	}
	if cfg.RouterOSConfigured() {
		t.Fatalf("missing config should not be treated as routeros configured")
	}
	if cfg.RouterOS.BaseURL != "http://10.0.0.1" {
		t.Fatalf("unexpected default routeros url: %q", cfg.RouterOS.BaseURL)
	}
}

func TestLoadDeviceListAndSaveWithoutLegacyRouterOS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	payload := []byte("devices:\n  - id: edge\n    name: Edge router\n    enabled: true\n    routeros:\n      base_url: http://edge.test\n      username: test\n      password: secret\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RouterOSConfigured() || cfg.RouterOS.BaseURL != "http://edge.test" {
		t.Fatalf("unexpected effective device config: %#v", cfg)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "\nrouteros:") {
		t.Fatalf("saved device config retained legacy routeros block:\n%s", saved)
	}
}

func TestSaveUsesPrivatePermissionsAndLeavesNoTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	cfg := Config{PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions=%o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("unexpected files after atomic save: %+v", entries)
	}
}

func TestValidateRejectsDuplicateDeviceIDs(t *testing.T) {
	cfg := Config{
		PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		Devices: []DeviceConfig{{ID: "same", Name: "One"}, {ID: "same", Name: "Two"}},
	}
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate device validation error, got %v", err)
	}
}

func TestValidateRejectsNonPositiveTieredPollingIntervals(t *testing.T) {
	cfg := Config{
		PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 0, TerminalPollIntervalSeconds: 3,
		SampleRetentionHours: 48, RouterOS: RouterOSConfig{BaseURL: "http://router.test", Username: "test", Password: "test"},
	}
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "realtime_poll_interval_seconds") {
		t.Fatalf("expected realtime interval validation error, got %v", err)
	}
	cfg.RealtimePollIntervalSeconds = 1
	cfg.TerminalPollIntervalSeconds = 0
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "terminal_poll_interval_seconds") {
		t.Fatalf("expected terminal interval validation error, got %v", err)
	}
}
