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
	if cfg.RealtimePollIntervalSeconds != 1 || cfg.TerminalPollIntervalSeconds != 5 || cfg.PollIntervalSeconds != 10 {
		t.Fatalf("unexpected polling defaults: realtime=%d terminal=%d full=%d", cfg.RealtimePollIntervalSeconds, cfg.TerminalPollIntervalSeconds, cfg.PollIntervalSeconds)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].ID != DefaultDeviceID || !cfg.Devices[0].Enabled {
		t.Fatalf("legacy routeros config was not normalized: %#v", cfg.Devices)
	}
	if cfg.Devices[0].MosDNS.Enabled || cfg.Devices[0].MosDNS.BaseURL != "" || cfg.Devices[0].MosDNS.SyncIntervalMinutes != 30 || cfg.Devices[0].MosDNS.MatchWindowMinutes != 30 {
		t.Fatalf("unexpected per-device MosDNS defaults: %#v", cfg.Devices[0].MosDNS)
	}
	if cfg.Devices[0].ProtocolAnalysis || cfg.Devices[0].FeatureLibrary.Enabled || cfg.Devices[0].FeatureLibrary.SourceURL != "" || cfg.Devices[0].FeatureLibrary.RefreshIntervalHours != 0 || cfg.Devices[0].FeatureLibrary.MatchWindowMinutes != 0 {
		t.Fatalf("unexpected per-device recognition defaults: %#v", cfg.Devices[0])
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].ID != DefaultDeviceID || !cfg.Devices[0].Enabled {
		t.Fatalf("legacy routeros config was not normalized: %#v", cfg.Devices)
	}
}

func TestLegacyPolicyAccessIsIgnoredAndRemovedOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	payload := []byte("devices:\n  - id: edge\n    name: Edge\n    enabled: true\n    routeros:\n      base_url: http://router.test\n      username: monitor\n      password: monitor-secret\n    policy_access:\n      enabled: true\n      username: old-policy\n      password: old-secret\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].RouterOS.Username != "monitor" {
		t.Fatalf("legacy device did not load: %#v", cfg.Devices)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "policy_access") || strings.Contains(string(saved), "old-secret") {
		t.Fatalf("retired policy access remained in saved config: %s", saved)
	}
}

func TestLegacyGlobalRecognitionSectionsAreIgnoredAndRemovedOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	payload := []byte("protocol_analysis:\n  enabled: true\nprotocol_analysis_migrated: true\nfeature_library:\n  enabled: true\n  source_url: https://example.test/library.yml\ndevices:\n  - id: edge\n    name: Edge\n    enabled: true\n    routeros:\n      base_url: http://edge.test\n      username: test\n      password: secret\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].ProtocolAnalysis || cfg.Devices[0].FeatureLibrary.Enabled {
		t.Fatalf("legacy global recognition sections should be ignored: %#v", cfg)
	}

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "\nprotocol_analysis:") || strings.Contains(string(saved), "\nfeature_library:") {
		t.Fatalf("legacy global recognition sections survived save:\n%s", saved)
	}
}

func TestLoadNormalizesPlainMosDNSAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	payload := []byte("devices:\n  - id: edge\n    name: Edge\n    enabled: true\n    routeros:\n      base_url: http://edge.test\n      username: test\n      password: secret\n    mosdns:\n      enabled: true\n      base_url: 10.0.0.3\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 1 {
		t.Fatalf("expected one device, got %#v", cfg.Devices)
	}
	if cfg.Devices[0].MosDNS.BaseURL != "http://10.0.0.3" || !cfg.Devices[0].MosDNS.Configured() {
		t.Fatalf("plain MosDNS address was not normalized: %#v", cfg.Devices[0].MosDNS)
	}
}

func TestLoadMigratesLegacyFeatureLibraryMatchWindowToMosDNS(t *testing.T) {
	cases := []struct {
		name   string
		mosdns string
		want   int
	}{
		{name: "legacy", mosdns: "", want: 20},
		{name: "new value wins", mosdns: "    mosdns:\n      match_window_minutes: 15\n", want: 15},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			payload := []byte("devices:\n  - id: edge\n    name: Edge\n    enabled: true\n    routeros:\n      base_url: http://edge.test\n      username: test\n      password: secret\n    feature_library:\n      enabled: true\n      source_url: https://example.test/library.yml\n      refresh_interval_hours: 24\n      match_window_minutes: 20\n" + testCase.mosdns)
			if err := os.WriteFile(path, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			device := cfg.Devices[0]
			if device.MosDNS.MatchWindowMinutes != testCase.want {
				t.Fatalf("evidence window=%d, want %d; device=%+v", device.MosDNS.MatchWindowMinutes, testCase.want, device)
			}
			if device.FeatureLibrary.SourceURL != "https://example.test/library.yml" {
				t.Fatalf("legacy feature library fields were not decoded: %+v", device.FeatureLibrary)
			}
		})
	}
}

func TestLegacyGlobalMosDNSSectionIsIgnoredAndRemovedOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	payload := []byte("mosdns:\n  enabled: true\n  base_url: http://10.0.0.3\n  sync_interval_minutes: 30\ndevices:\n  - id: edge\n    name: Edge\n    enabled: true\n    routeros:\n      base_url: http://edge.test\n      username: test\n      password: secret\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].MosDNS.Enabled {
		t.Fatalf("legacy global MosDNS section should be ignored: %#v", cfg)
	}

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "\nmosdns:") {
		t.Fatalf("legacy global mosdns section survived save:\n%s", saved)
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

func TestLoadWithoutPathUsesDefaultConfigFile(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != "config.yaml" {
		t.Fatalf("unexpected default config path: %q", cfg.Path)
	}
	if err := Save(cfg.Path, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "config.yaml")); err != nil {
		t.Fatalf("default config file was not created: %v", err)
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

func TestLoadCanDisableMosDNSAndRejectsInvalidEnabledInterval(t *testing.T) {
	directory := t.TempDir()
	disabledPath := filepath.Join(directory, "disabled.yaml")
	if err := os.WriteFile(disabledPath, []byte("devices:\n  - id: edge\n    name: Edge\n    enabled: true\n    routeros:\n      base_url: http://edge.test\n      username: test\n      password: secret\n    mosdns:\n      enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(disabledPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Devices[0].MosDNS.Configured() {
		t.Fatalf("MosDNS should be disabled: %#v", cfg.Devices[0].MosDNS)
	}

	invalidPath := filepath.Join(directory, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte("devices:\n  - id: edge\n    name: Edge\n    enabled: true\n    routeros:\n      base_url: http://edge.test\n      username: test\n      password: secret\n    mosdns:\n      enabled: true\n      base_url: 10.0.0.3\n      sync_interval_minutes: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(invalidPath); err == nil || !strings.Contains(err.Error(), "mosdns.sync_interval_minutes") {
		t.Fatalf("expected invalid MosDNS interval error, got %v", err)
	}

	missingAddressPath := filepath.Join(directory, "missing-address.yaml")
	if err := os.WriteFile(missingAddressPath, []byte("devices:\n  - id: edge\n    name: Edge\n    enabled: true\n    routeros:\n      base_url: http://edge.test\n      username: test\n      password: secret\n    mosdns:\n      enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(missingAddressPath); err == nil || !strings.Contains(err.Error(), "mosdns.base_url") {
		t.Fatalf("expected missing MosDNS address error, got %v", err)
	}
}

func TestLoadMigratesLegacyDevicesToNameSortedSortOrder(t *testing.T) {
	directory := t.TempDir()
	legacyPath := filepath.Join(directory, "legacy.yaml")
	payload := []byte("devices:\n  - id: zulu\n    name: Zulu\n    enabled: true\n    routeros:\n      base_url: http://zulu.test\n      username: test\n      password: secret\n  - id: alpha\n    name: Alpha\n    enabled: true\n    routeros:\n      base_url: http://alpha.test\n      username: test\n      password: secret\n")
	if err := os.WriteFile(legacyPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 2 {
		t.Fatalf("unexpected device count: %+v", cfg.Devices)
	}
	if cfg.Devices[0].ID != "alpha" || cfg.Devices[0].SortOrder != 1 {
		t.Fatalf("legacy devices must be name sorted with sort order 1..n, got %+v", cfg.Devices)
	}
	if cfg.Devices[1].ID != "zulu" || cfg.Devices[1].SortOrder != 2 {
		t.Fatalf("legacy devices must be name sorted with sort order 1..n, got %+v", cfg.Devices)
	}
}

func TestLoadNormalizesNegativeSortOrderViaMigration(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "negative.yaml")
	payload := []byte("devices:\n  - id: zulu\n    name: Zulu\n    enabled: true\n    sort_order: -5\n    routeros:\n      base_url: http://zulu.test\n      username: test\n      password: secret\n  - id: alpha\n    name: Alpha\n    enabled: true\n    routeros:\n      base_url: http://alpha.test\n      username: test\n      password: secret\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Negative values are unconfigured, so the legacy migration fills 1..N by
	// name and the config slice ends up in display order.
	if cfg.Devices[0].ID != "alpha" || cfg.Devices[0].SortOrder != 1 {
		t.Fatalf("negative sort order must be treated as unconfigured, got %+v", cfg.Devices)
	}
	if cfg.Devices[1].ID != "zulu" || cfg.Devices[1].SortOrder != 2 {
		t.Fatalf("negative sort order must be treated as unconfigured, got %+v", cfg.Devices)
	}
}

func TestLoadLegacySingleDeviceGetsSortOrderOne(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "legacy-single.yaml")
	payload := []byte("routeros:\n  base_url: http://router.test\n  username: test\n  password: secret\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].ID != DefaultDeviceID || cfg.Devices[0].SortOrder != 1 {
		t.Fatalf("legacy single-device migration must set sort_order=1, got %+v", cfg.Devices)
	}
}

func TestLoadPreservesExplicitSortOrder(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "sorted.yaml")
	payload := []byte("devices:\n  - id: zulu\n    name: Zulu\n    enabled: true\n    sort_order: 10\n    routeros:\n      base_url: http://zulu.test\n      username: test\n      password: secret\n  - id: alpha\n    name: Alpha\n    enabled: true\n    sort_order: 5\n    routeros:\n      base_url: http://alpha.test\n      username: test\n      password: secret\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// The config slice is the single source of truth for display order, so
	// Load normalizes it to match the effective sort order.
	if cfg.Devices[0].ID != "alpha" || cfg.Devices[0].SortOrder != 5 {
		t.Fatalf("config slice must match display order, got %+v", cfg.Devices)
	}
	if cfg.Devices[1].ID != "zulu" || cfg.Devices[1].SortOrder != 10 {
		t.Fatalf("config slice must match display order, got %+v", cfg.Devices)
	}
	if cfg.Devices[0].RouterOS.BaseURL != "http://alpha.test" || cfg.Devices[1].RouterOS.BaseURL != "http://zulu.test" {
		t.Fatalf("reordering must not mix up device payloads, got %+v", cfg.Devices)
	}
}
