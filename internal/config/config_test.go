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
