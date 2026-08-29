package service

import (
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/store"
)

func TestStatusesSortBySortOrderThenName(t *testing.T) {
	manager := &MonitorManager{items: map[string]*managedMonitor{}}
	// Input order is deliberately scrambled so a stable sort cannot pass by
	// accident: expected output is Alpha(1), Bravo(2), Charlie(2 — name tie
	// break), then unconfigured entries (<=0) name-sorted: Adam, Bob, Zulu.
	configured := []config.DeviceConfig{
		{ID: "t3", Name: "Charlie", Enabled: true, SortOrder: 2},
		{ID: "t6", Name: "Adam", Enabled: true, SortOrder: 0},
		{ID: "t2", Name: "Bravo", Enabled: true, SortOrder: 2},
		{ID: "t4", Name: "Zulu", Enabled: true, SortOrder: -3},
		{ID: "t1", Name: "Alpha", Enabled: true, SortOrder: 1},
		{ID: "t5", Name: "Bob", Enabled: true, SortOrder: 0},
		{ID: "t7", Name: "Archived", Enabled: true, Archived: true},
	}
	got := manager.Statuses(false, configured)
	want := []string{"t1", "t2", "t3", "t6", "t5", "t4"}
	if len(got) != len(want) {
		t.Fatalf("unexpected status count: %+v", got)
	}
	for index, id := range want {
		if got[index].ID != id {
			t.Fatalf("statuses[%d].ID = %q, want %q (full: %+v)", index, got[index].ID, id, got)
		}
	}
}

func TestMonitorForDevicesFollowsCurrentConfigOrder(t *testing.T) {
	monitors := map[string]*Monitor{
		"first":  {},
		"second": {},
	}
	manager := &MonitorManager{
		items: map[string]*managedMonitor{
			"first":  {device: config.DeviceConfig{ID: "first", Name: "First", Enabled: true}, monitor: monitors["first"]},
			"second": {device: config.DeviceConfig{ID: "second", Name: "Second", Enabled: true}, monitor: monitors["second"]},
		},
		order: []string{"first", "second"},
	}
	// The current config lists "second" first; the default device must follow
	// that order instead of the manager's startup order.
	configured := []config.DeviceConfig{
		{ID: "second", Name: "Second", Enabled: true},
		{ID: "first", Name: "First", Enabled: true},
	}
	monitor, err := manager.MonitorForDevices("", configured)
	if err != nil || monitor != monitors["second"] {
		t.Fatalf("default device must follow current config order, got %v err=%v", monitor, err)
	}
	explicit, err := manager.MonitorForDevices("first", configured)
	if err != nil || explicit != monitors["first"] {
		t.Fatalf("explicit device id must still resolve, got %v err=%v", explicit, err)
	}
}

func TestFleetOverviewUsesCurrentConfigOrderWithoutRestart(t *testing.T) {
	now := time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC)
	// Cached device configs carry stale sort orders; the fleet grid must use
	// the caller's current configuration instead.
	manager := &MonitorManager{
		items: map[string]*managedMonitor{
			"alpha": {
				device:  config.DeviceConfig{ID: "alpha", Name: "Alpha", Enabled: true, SortOrder: 99},
				started: true,
				monitor: &Monitor{snapshot: model.DashboardSnapshot{Overview: model.Overview{RouterName: "alpha-router", CPULoadPercent: 12, MemoryUsedPercent: 48, UploadBps: 1000, DownloadBps: 2000, ConnectedDeviceCount: 5, TerminalStateCounts: model.TerminalStateCounts{Online: 3, Inactive: 1, Offline: 1}, ConnectionCount: 8, ConnectionProtocolCounts: model.ConnectionProtocolCounts{TCP: 5, UDP: 2, Other: 1}, Uptime: "1d", UpdatedAt: now.Add(-time.Second)}}},
			},
			"bravo": {
				device:  config.DeviceConfig{ID: "bravo", Name: "Bravo", Enabled: true, SortOrder: 99},
				started: true,
				monitor: &Monitor{snapshot: model.DashboardSnapshot{Overview: model.Overview{UpdatedAt: now.Add(-time.Second)}, Alerts: []model.AlertEvent{{ID: "policy", Level: "warning"}}}},
			},
			"charlie": {
				device:  config.DeviceConfig{ID: "charlie", Name: "Charlie", Enabled: true, SortOrder: 99},
				started: true,
				monitor: &Monitor{snapshot: model.DashboardSnapshot{Overview: model.Overview{UpdatedAt: now.Add(-fleetSnapshotStaleAfter - time.Second)}}},
			},
			"disabled": {device: config.DeviceConfig{ID: "disabled", Name: "Disabled", Enabled: false}},
		},
		order: []string{"charlie", "disabled", "bravo", "alpha"},
	}
	// Post-reorder config: charlie(1), bravo(2), alpha(3); input order is
	// scrambled to prove the output follows sort order, not slice order.
	configured := []config.DeviceConfig{
		{ID: "charlie", Name: "Charlie", Enabled: true, SortOrder: 1},
		{ID: "bravo", Name: "Bravo", Enabled: true, SortOrder: 2},
		{ID: "alpha", Name: "Alpha", Enabled: true, SortOrder: 3},
		{ID: "disabled", Name: "Disabled", Enabled: false, SortOrder: 4},
		{ID: "archived", Name: "Archived", Enabled: true, Archived: true, SortOrder: 5},
	}

	got := manager.FleetOverview(now, configured)
	if got.TotalDevices != 3 || got.OnlineDevices != 2 || got.OfflineDevices != 1 || got.AlertDevices != 2 {
		t.Fatalf("unexpected fleet summary: %+v", got)
	}
	if len(got.Devices) != 3 || got.Devices[0].ID != "charlie" || got.Devices[1].ID != "bravo" || got.Devices[2].ID != "alpha" {
		t.Fatalf("fleet devices must follow current config sort order without restart: %+v", got.Devices)
	}
	if got.Devices[1].State != "online" || !got.Devices[1].Alerting {
		t.Fatalf("current monitor alerts must mark an online device alerting: %+v", got.Devices[1])
	}
	if got.Devices[0].State != "offline" || !got.Devices[0].Alerting || got.Devices[0].Error == "" {
		t.Fatalf("stale snapshots must be offline alerting entries: %+v", got.Devices[0])
	}
	if got.Devices[2].State != "online" || got.Devices[2].CPULoadPercent != 12 || got.Devices[2].TerminalCount != 5 || got.Devices[2].TerminalInactive != 1 || got.Devices[2].ConnectionCount != 8 || got.Devices[2].ConnectionUDP != 2 {
		t.Fatalf("online snapshot fields were not projected: %+v", got.Devices[2])
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

func TestPerDeviceRecognitionServicesFollowDeviceSwitch(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	cfg := config.Config{
		DataDir: t.TempDir(),
		Devices: []config.DeviceConfig{
			{
				ID: "on", Name: "On", Enabled: true,
				RouterOS:         config.RouterOSConfig{BaseURL: "http://on.test", Username: "u", Password: "p"},
				ProtocolAnalysis: true,
				FeatureLibrary:   config.FeatureLibraryConfig{Enabled: true, SourceURL: "https://example.test/library.yml", RefreshIntervalHours: 24, MatchWindowMinutes: 30},
				MosDNS:           config.MosDNSConfig{Enabled: true, BaseURL: "http://10.0.0.3", SyncIntervalMinutes: 30},
			},
			{
				ID: "off", Name: "Off", Enabled: true,
				RouterOS: config.RouterOSConfig{BaseURL: "http://off.test", Username: "u", Password: "p"},
			},
		},
	}
	manager, err := NewMonitorManager(cfg, storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	if manager.items["on"].feature == nil || manager.items["on"].mosdns == nil {
		t.Fatalf("enabled device must own its recognition services: %+v", manager.items["on"])
	}
	if manager.items["off"].feature != nil || manager.items["off"].mosdns != nil {
		t.Fatalf("disabled device must not own recognition services: %+v", manager.items["off"])
	}
	onStatus := manager.RecognitionStatus("on")
	if !onStatus.ProtocolAnalysis || !onStatus.MosDNS.Enabled || !onStatus.FeatureLibrary.Enabled {
		t.Fatalf("enabled device recognition status incomplete: %+v", onStatus)
	}
	offStatus := manager.RecognitionStatus("off")
	if offStatus.ProtocolAnalysis || offStatus.MosDNS.Enabled || offStatus.FeatureLibrary.Enabled {
		t.Fatalf("disabled device recognition status must be off: %+v", offStatus)
	}
}
