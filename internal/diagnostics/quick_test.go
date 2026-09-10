package diagnostics

import (
	"testing"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/policyv2"
	"rosboard/internal/store"
	"rosboard/internal/subject"
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

func TestAccessRevisionStaysVisibleWhenMonitorUnavailable(t *testing.T) {
	dataDir := t.TempDir()
	storage, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer storage.Close()
	ctx := t.Context()
	_, err = storage.AccessRepository().SaveRule(ctx, accesscontrol.AccessRule{
		ID:          "rule-a",
		Name:        "访问控制规则",
		TargetScope: accesscontrol.TargetScopeInternet,
		Subject:     subject.Subject{Mode: subject.ModeAll},
		Enabled:     true,
	}, nil, "test")
	if err != nil {
		t.Fatalf("SaveRule() error = %v", err)
	}

	report := (Runner{
		Config: config.Config{DataDir: dataDir},
		Device: config.DeviceConfig{
			ID:      "router-a",
			Name:    "Router A",
			Enabled: true,
			RouterOS: config.RouterOSConfig{
				BaseURL: "http://router-a.test", Username: "reader", Password: "fixture",
			},
		},
		Store: storage,
	}).Quick(ctx)

	for _, finding := range report.Findings {
		if finding.ID == "access.state" {
			if finding.Status != StatusWarning {
				t.Fatalf("access status = %q, want %q: %#v", finding.Status, StatusWarning, finding)
			}
			return
		}
	}
	t.Fatal("access.state finding not found")
}

func TestAccessJobFailureStaysVisibleWhenMonitorUnavailable(t *testing.T) {
	dataDir := t.TempDir()
	storage, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer storage.Close()
	ctx := t.Context()
	_, err = storage.AccessRepository().SaveRule(ctx, accesscontrol.AccessRule{
		ID:          "rule-a",
		Name:        "访问控制规则",
		TargetScope: accesscontrol.TargetScopeInternet,
		Subject:     subject.Subject{Mode: subject.ModeAll},
		Enabled:     true,
	}, nil, "test")
	if err != nil {
		t.Fatalf("SaveRule() error = %v", err)
	}
	if err := storage.PolicyRepository().SaveApplyJob(ctx, policyv2.ApplyJob{
		ID: "job-a", State: "failed", Phase: "verify", Error: "verification failed",
	}); err != nil {
		t.Fatalf("SaveApplyJob() error = %v", err)
	}

	report := (Runner{
		Config: config.Config{DataDir: dataDir},
		Device: config.DeviceConfig{
			ID:      "router-a",
			Name:    "Router A",
			Enabled: true,
			RouterOS: config.RouterOSConfig{
				BaseURL: "http://router-a.test", Username: "reader", Password: "fixture",
			},
		},
		Store: storage,
	}).Quick(ctx)

	for _, finding := range report.Findings {
		if finding.ID == "access.state" {
			if finding.Status != StatusError {
				t.Fatalf("access status = %q, want %q: %#v", finding.Status, StatusError, finding)
			}
			return
		}
	}
	t.Fatal("access.state finding not found")
}
