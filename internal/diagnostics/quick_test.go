package diagnostics

import (
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/store"
)

func TestLatestRefreshFailureIgnoresUnrelatedAlerts(t *testing.T) {
	now := time.Now().UTC()
	alerts := []model.AlertEvent{
		{ID: "terminal-warning", Level: "warning", Timestamp: now.Add(time.Minute), Message: "not a refresh failure"},
		{ID: "dashboard-refresh", Level: "error", Timestamp: now.Add(-time.Minute), Message: "refresh failed"},
	}
	got := latestRefreshFailure(alerts)
	if got == nil || got.Message != "refresh failed" {
		t.Fatalf("latestRefreshFailure() = %#v, want dashboard-refresh alert", got)
	}
}

func TestQuickReadsExistingSQLiteStore(t *testing.T) {
	dataDir := t.TempDir()
	storage, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer storage.Close()

	report := (Runner{
		Config: config.Config{DataDir: dataDir},
		Device: config.DeviceConfig{ID: "disabled", Name: "Disabled", Enabled: false},
		Store:  storage,
	}).Quick(t.Context())
	if report.Overall != OverallHealthy {
		t.Fatalf("disabled device overall = %q, want %q", report.Overall, OverallHealthy)
	}
	for _, finding := range report.Findings {
		if (finding.ID == "routeros.connection" || finding.ID == "recognition.mosdns") && finding.AffectsOverall {
			t.Fatalf("optional/disabled finding %q unexpectedly affects overall", finding.ID)
		}
		if finding.ID == "system.storage" {
			if finding.Status != StatusOK {
				t.Fatalf("storage finding status = %q, want %q: %#v", finding.Status, StatusOK, finding)
			}
			return
		}
	}
	t.Fatal("system.storage finding not found")
}
