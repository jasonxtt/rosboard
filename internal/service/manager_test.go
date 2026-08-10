package service

import (
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/model"
)

func TestFleetOverviewUsesCachedSnapshotsAndClassifiesDevices(t *testing.T) {
	now := time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC)
	manager := &MonitorManager{
		items: map[string]*managedMonitor{
			"alpha": {
				device:  config.DeviceConfig{ID: "alpha", Name: "Alpha", Enabled: true},
				started: true,
				monitor: &Monitor{snapshot: model.DashboardSnapshot{Overview: model.Overview{RouterName: "alpha-router", CPULoadPercent: 12, MemoryUsedPercent: 48, UploadBps: 1000, DownloadBps: 2000, ConnectedDeviceCount: 5, TerminalStateCounts: model.TerminalStateCounts{Online: 3, Inactive: 1, Offline: 1}, ConnectionCount: 8, ConnectionProtocolCounts: model.ConnectionProtocolCounts{TCP: 5, UDP: 2, Other: 1}, Uptime: "1d", UpdatedAt: now.Add(-time.Second)}}},
			},
			"bravo": {
				device:  config.DeviceConfig{ID: "bravo", Name: "Bravo", Enabled: true},
				started: true,
				monitor: &Monitor{snapshot: model.DashboardSnapshot{Overview: model.Overview{UpdatedAt: now.Add(-time.Second)}, Alerts: []model.AlertEvent{{ID: "policy", Level: "warning"}}}},
			},
			"charlie": {
				device:  config.DeviceConfig{ID: "charlie", Name: "Charlie", Enabled: true},
				started: true,
				monitor: &Monitor{snapshot: model.DashboardSnapshot{Overview: model.Overview{UpdatedAt: now.Add(-fleetSnapshotStaleAfter - time.Second)}}},
			},
			"disabled": {device: config.DeviceConfig{ID: "disabled", Name: "Disabled", Enabled: false}},
		},
		order: []string{"charlie", "disabled", "bravo", "alpha"},
	}

	got := manager.FleetOverview(now)
	if got.TotalDevices != 3 || got.OnlineDevices != 2 || got.OfflineDevices != 1 || got.AlertDevices != 2 {
		t.Fatalf("unexpected fleet summary: %+v", got)
	}
	if len(got.Devices) != 3 || got.Devices[0].ID != "alpha" || got.Devices[1].ID != "bravo" || got.Devices[2].ID != "charlie" {
		t.Fatalf("devices must be name sorted and omit disabled entries: %+v", got.Devices)
	}
	if got.Devices[0].State != "online" || got.Devices[0].CPULoadPercent != 12 || got.Devices[0].TerminalCount != 5 || got.Devices[0].TerminalInactive != 1 || got.Devices[0].ConnectionCount != 8 || got.Devices[0].ConnectionUDP != 2 {
		t.Fatalf("online snapshot fields were not projected: %+v", got.Devices[0])
	}
	if !got.Devices[1].Alerting || got.Devices[1].State != "online" {
		t.Fatalf("current monitor alerts must mark an online device alerting: %+v", got.Devices[1])
	}
	if got.Devices[2].State != "offline" || !got.Devices[2].Alerting || got.Devices[2].Error == "" {
		t.Fatalf("stale snapshots must be offline alerting entries: %+v", got.Devices[2])
	}
}

func TestMonitorRetryDelayBacksOffAndCaps(t *testing.T) {
	if got := nextMonitorRetryDelay(initialMonitorRetryDelay); got != 60*time.Second {
		t.Fatalf("first retry delay = %s, want 1m", got)
	}
	if got := nextMonitorRetryDelay(4 * time.Minute); got != maxMonitorRetryDelay {
		t.Fatalf("capped retry delay = %s, want 5m", got)
	}
	if got := nextMonitorRetryDelay(maxMonitorRetryDelay); got != maxMonitorRetryDelay {
		t.Fatalf("max retry delay = %s, want 5m", got)
	}
}

func TestProtocolAnalysisDisabledSkipsRecognitionServices(t *testing.T) {
	manager, err := NewMonitorManager(config.Config{
		ProtocolAnalysis: config.ProtocolAnalysisConfig{Enabled: false},
		MosDNS:           config.MosDNSConfig{Enabled: true, BaseURL: "http://10.0.0.3", SyncIntervalMinutes: 30},
		FeatureLibrary:   config.FeatureLibraryConfig{Enabled: true, SourceURL: "https://example.test/library.yml", RefreshIntervalHours: 24, MatchWindowMinutes: 30},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if manager.mosdns != nil || manager.feature != nil || manager.resolver != nil {
		t.Fatalf("recognition services must stay nil when protocol analysis is disabled: %+v", manager)
	}
	if manager.MosDNSStatus() != (MosDNSStatus{}) || manager.RecognitionStatus() != (RecognitionStatus{}) {
		t.Fatalf("recognition status must be zero when protocol analysis is disabled: mosdns=%+v recognition=%+v", manager.MosDNSStatus(), manager.RecognitionStatus())
	}
}
