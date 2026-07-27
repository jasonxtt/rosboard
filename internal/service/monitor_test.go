package service

import (
	"context"
	"net"
	"reflect"
	"sort"
	"testing"
	"time"

	"rosboard/internal/model"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

func TestSelectTrafficInterfacesPrefersPPPoE(t *testing.T) {
	interfaces := []routeros.Interface{
		{Name: "lan", Type: "ether", Running: "true"},
		{Name: "wan", Type: "ether", Running: "true"},
		{Name: "pppoe-out1", Type: "pppoe-out", Running: "true"},
	}

	selected := selectTrafficInterfaces(nil, interfaces)
	if len(selected) != 1 || selected[0] != "pppoe-out1" {
		t.Fatalf("expected pppoe-out1, got %#v", selected)
	}
}

func TestEmptySnapshotUsesJSONArrays(t *testing.T) {
	snapshot := (&Monitor{}).Snapshot()

	value := reflect.ValueOf(snapshot)
	for _, field := range []string{"Interfaces", "Terminals", "Capabilities", "Protocols", "Policies", "Routes", "Alerts", "Warnings"} {
		if value.FieldByName(field).IsNil() {
			t.Fatalf("%s must be an empty slice, not nil", field)
		}
	}
	if snapshot.Overview.TrafficInterfaces == nil || snapshot.Overview.ChartSamples == nil {
		t.Fatal("overview traffic collections must be empty slices, not nil")
	}
	if snapshot.TerminalScopeSummaries == nil {
		t.Fatal("terminal scope summaries must be an empty map, not nil")
	}
}

func TestSelectedTrafficRatesMapTXToUploadAndRXToDownload(t *testing.T) {
	rates := map[string]routeros.MonitorTrafficEntry{
		"pppoe-out1": {RXBitsPerSecond: "125000", TXBitsPerSecond: "750000"},
		"lan":        {RXBitsPerSecond: "999999", TXBitsPerSecond: "999999"},
	}
	selected := []string{"pppoe-out1"}

	if got := totalSelectedTXBps(rates, selected); got != 750000 {
		t.Fatalf("expected selected TX to be WAN upload, got %.0f", got)
	}
	if got := totalSelectedRXBps(rates, selected); got != 125000 {
		t.Fatalf("expected selected RX to be WAN download, got %.0f", got)
	}
}

func TestConnectedLANDeviceCountIncludesOnlyOnlineLANDevices(t *testing.T) {
	terminals := []model.Terminal{
		{ID: routerTerminalID, State: "online", PrimaryInterface: "lan"},
		{ID: "lan-online", State: "online", PrimaryInterface: "lan"},
		{ID: "wan-online", State: "online", PrimaryInterface: "wan"},
		{ID: "pppoe-online", State: "online", PrimaryInterface: "pppoe-out1"},
		{ID: "lan-offline", State: "offline", PrimaryInterface: "lan"},
		{ID: "online-unknown-interface", State: "online"},
	}

	if got := connectedLANDeviceCount(terminals, []string{"pppoe-out1"}); got != 2 {
		t.Fatalf("expected 2 online LAN devices, got %d", got)
	}
}

func TestTerminalScopeSummariesAggregateEligibleOnlineLANDevices(t *testing.T) {
	terminals := []model.Terminal{
		{
			ID: "dual-stack", State: "online", PrimaryInterface: "lan",
			IPv4: []string{"10.0.0.2"}, IPv6: []string{"fd00::2"},
			FamilyStats: map[string]model.TerminalFamilyStats{
				"ipv4": {ConnectionCount: 2, CurrentUploadBps: 80, CurrentDownloadBps: 160, ActiveUploadBytes: 1000, ActiveDownloadBytes: 2000},
				"ipv6": {ConnectionCount: 3, CurrentUploadBps: 40, CurrentDownloadBps: 120, ActiveUploadBytes: 3000, ActiveDownloadBytes: 4000},
			},
		},
		{
			ID: "ipv4-only", State: "online", PrimaryInterface: "lan", IPv4: []string{"10.0.0.3"},
			FamilyStats: map[string]model.TerminalFamilyStats{
				"ipv4": {ConnectionCount: 5, CurrentUploadBps: 800, CurrentDownloadBps: 1600, ActiveUploadBytes: 5000, ActiveDownloadBytes: 6000},
			},
		},
		{
			ID: "ipv6-only", State: "online", IPv6: []string{"fd00::4"},
			FamilyStats: map[string]model.TerminalFamilyStats{
				"ipv6": {ConnectionCount: 7, CurrentUploadBps: 400, CurrentDownloadBps: 1200, ActiveUploadBytes: 7000, ActiveDownloadBytes: 8000},
			},
		},
		{ID: routerTerminalID, State: "online", PrimaryInterface: "lan", IPv4: []string{"10.0.0.1"}, FamilyStats: map[string]model.TerminalFamilyStats{"ipv4": {ConnectionCount: 100}}},
		{ID: "inactive", State: "inactive", PrimaryInterface: "lan", IPv4: []string{"10.0.0.5"}, FamilyStats: map[string]model.TerminalFamilyStats{"ipv4": {ConnectionCount: 100}}},
		{ID: "offline", State: "offline", PrimaryInterface: "lan", IPv6: []string{"fd00::6"}, FamilyStats: map[string]model.TerminalFamilyStats{"ipv6": {ConnectionCount: 100}}},
		{ID: "wan", State: "online", PrimaryInterface: "wan", IPv4: []string{"198.51.100.7"}, FamilyStats: map[string]model.TerminalFamilyStats{"ipv4": {ConnectionCount: 100}}},
		{ID: "loopback", State: "online", PrimaryInterface: "lo", IPv4: []string{"127.0.0.1"}, FamilyStats: map[string]model.TerminalFamilyStats{"ipv4": {ConnectionCount: 100}}},
		{ID: "selected-traffic", State: "online", PrimaryInterface: "pppoe-out1", IPv6: []string{"2001:db8::8"}, FamilyStats: map[string]model.TerminalFamilyStats{"ipv6": {ConnectionCount: 100}}},
	}

	got := terminalScopeSummaries(terminals, []string{"pppoe-out1"})
	want := map[string]model.TerminalScopeSummary{
		"all":  {DeviceCount: 3, ConnectionCount: 17, CurrentUploadBps: 1320, CurrentDownloadBps: 3080, ActiveUploadBytes: 16000, ActiveDownloadBytes: 20000},
		"ipv4": {DeviceCount: 2, ConnectionCount: 7, CurrentUploadBps: 880, CurrentDownloadBps: 1760, ActiveUploadBytes: 6000, ActiveDownloadBytes: 8000},
		"ipv6": {DeviceCount: 2, ConnectionCount: 10, CurrentUploadBps: 440, CurrentDownloadBps: 1320, ActiveUploadBytes: 10000, ActiveDownloadBytes: 12000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected terminal scope summaries:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestTerminalScopeSummariesEmpty(t *testing.T) {
	got := terminalScopeSummaries(nil, nil)
	want := map[string]model.TerminalScopeSummary{
		"all": {}, "ipv4": {}, "ipv6": {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected empty terminal scope summaries: %#v", got)
	}
}

func TestBuildTerminalsDistinguishesStrongAndWeakPresenceEvidence(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	monitor := &Monitor{store: storage}
	now := time.Unix(1000, 0).UTC()
	terminals, _, err := monitor.buildTerminals(
		context.Background(),
		now,
		nil,
		map[string]routerAssignedAddress{"10.0.0.1": {Family: "ipv4", Interface: "lan"}},
		[]routeros.DHCPLease{{Address: "10.0.0.2", MACAddress: "00:11:22:33:44:02", Status: "bound"}},
		[]routeros.ARPEntry{{Address: "10.0.0.3", MACAddress: "00:11:22:33:44:03", Interface: "lan", Complete: "true", Status: "stale"}},
		[]routeros.IPv6Neighbor{{Address: "fc00::4", MACAddress: "00:11:22:33:44:04", Interface: "lan", Status: "reachable"}},
		nil,
		nil,
		routeMatcher{},
	)
	if err != nil {
		t.Fatalf("build terminals: %v", err)
	}
	if len(terminals) != 4 {
		t.Fatalf("expected four terminals, got %d", len(terminals))
	}
	states := map[string]string{}
	for _, terminal := range terminals {
		states[terminal.PrimaryIPv4+terminal.PrimaryIPv6] = terminal.State
	}
	if states["10.0.0.1"] != "online" || states["fc00::4"] != "online" {
		t.Fatalf("strong evidence should be online: %#v", states)
	}
	if states["10.0.0.2"] != "offline" || states["10.0.0.3"] != "offline" {
		t.Fatalf("lease and stale ARP must not be online: %#v", states)
	}
}

func TestBuildTerminalsFiltersDiscoveryOutsideTerminalCIDRs(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	monitor := &Monitor{store: storage}
	now := time.Unix(1500, 0).UTC()
	terminals, _, err := monitor.buildTerminals(
		context.Background(),
		now,
		parseCIDRs([]string{"10.0.0.1/24"}),
		map[string]routerAssignedAddress{"10.0.2.2": {Family: "ipv4", Interface: "wan-xray"}},
		[]routeros.DHCPLease{
			{Address: "10.0.0.8", MACAddress: "00:11:22:33:44:08", Status: "bound"},
			{Address: "10.0.2.1", MACAddress: "00:11:22:33:44:21", Status: "bound"},
		},
		[]routeros.ARPEntry{
			{Address: "10.0.0.8", MACAddress: "00:11:22:33:44:08", Interface: "lan", Complete: "true", Status: "reachable"},
			{Address: "10.0.2.1", MACAddress: "00:11:22:33:44:21", Interface: "wan-xray", Complete: "true", Status: "reachable"},
		},
		nil,
		nil,
		nil,
		routeMatcher{},
	)
	if err != nil {
		t.Fatalf("build terminals: %v", err)
	}

	byAddress := map[string]model.Terminal{}
	for _, terminal := range terminals {
		byAddress[terminal.PrimaryIPv4] = terminal
	}
	if _, exists := byAddress["10.0.0.8"]; !exists {
		t.Fatalf("expected in-scope terminal, got %#v", terminals)
	}
	if _, exists := byAddress["10.0.2.1"]; exists {
		t.Fatalf("out-of-scope terminal should be hidden: %#v", terminals)
	}
	if terminal := byAddress["10.0.2.2"]; terminal.ID != routerTerminalID {
		t.Fatalf("router self address must remain visible, got %#v", terminals)
	}
}

func TestBuildTerminalsUsesInactiveGracePeriodWithoutAdvancingLastSeen(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	monitor := &Monitor{store: storage}
	ctx := context.Background()
	firstSeen := time.Unix(2000, 0).UTC()
	strongARP := []routeros.ARPEntry{{Address: "10.0.0.5", MACAddress: "BC:24:11:94:45:CA", Interface: "lan", Complete: "true", Status: "reachable"}}
	staleARP := []routeros.ARPEntry{{Address: "10.0.0.5", MACAddress: "BC:24:11:94:45:CA", Interface: "lan", Complete: "true", Status: "stale"}}

	terminals, _, err := monitor.buildTerminals(ctx, firstSeen, nil, nil, nil, strongARP, nil, nil, nil, routeMatcher{})
	if err != nil || len(terminals) != 1 || terminals[0].State != "online" {
		t.Fatalf("expected initial strong evidence online, terminals=%#v err=%v", terminals, err)
	}

	terminals, _, err = monitor.buildTerminals(ctx, firstSeen.Add(time.Minute), nil, nil, nil, staleARP, nil, nil, nil, routeMatcher{})
	if err != nil || terminals[0].State != "inactive" {
		t.Fatalf("expected stale evidence inside grace period inactive, terminals=%#v err=%v", terminals, err)
	}
	if !terminals[0].LastSeen.Equal(firstSeen) {
		t.Fatalf("stale evidence advanced lastSeen: got %v want %v", terminals[0].LastSeen, firstSeen)
	}

	terminals, _, err = monitor.buildTerminals(ctx, firstSeen.Add(6*time.Minute), nil, nil, nil, staleARP, nil, nil, nil, routeMatcher{})
	if err != nil || terminals[0].State != "offline" {
		t.Fatalf("expected stale evidence after grace period offline, terminals=%#v err=%v", terminals, err)
	}
	if !terminals[0].LastSeen.Equal(firstSeen) {
		t.Fatalf("offline stale evidence advanced lastSeen: got %v want %v", terminals[0].LastSeen, firstSeen)
	}
}

func TestRecordRefreshErrorDeduplicatesCurrentAlert(t *testing.T) {
	monitor := &Monitor{snapshot: model.DashboardSnapshot{Alerts: []model.AlertEvent{{ID: "policy", Level: "warning"}}}}
	monitor.recordRefreshError(context.DeadlineExceeded)
	monitor.recordRefreshError(context.Canceled)

	snapshot := monitor.Snapshot()
	if len(snapshot.Alerts) != 2 {
		t.Fatalf("expected one refresh error plus existing alert, got %#v", snapshot.Alerts)
	}
	if snapshot.Alerts[0].ID != "dashboard-refresh" || snapshot.Alerts[0].Level != "error" || snapshot.Alerts[0].Message != context.Canceled.Error() {
		t.Fatalf("unexpected refresh alert: %#v", snapshot.Alerts[0])
	}
}

func TestEffectiveTerminalNamePrefersCustomThenAutomaticThenAddress(t *testing.T) {
	tests := []struct {
		terminal model.Terminal
		want     string
	}{
		{terminal: model.Terminal{CustomName: "iPhone 13 PM", AutoName: "iphone", PrimaryIPv4: "10.0.0.8"}, want: "iPhone 13 PM"},
		{terminal: model.Terminal{AutoName: "iphone", PrimaryIPv4: "10.0.0.8"}, want: "iphone"},
		{terminal: model.Terminal{PrimaryIPv4: "10.0.0.8", MACAddress: "00:11:22:33:44:55"}, want: "10.0.0.8"},
		{terminal: model.Terminal{MACAddress: "00:11:22:33:44:55"}, want: "00:11:22:33:44:55"},
	}
	for _, test := range tests {
		if got := effectiveTerminalName(test.terminal); got != test.want {
			t.Errorf("effectiveTerminalName(%#v) = %q, want %q", test.terminal, got, test.want)
		}
	}
}

func TestRecognizedAutoNameRejectsAddressFallbacks(t *testing.T) {
	if got := recognizedAutoName("00:11:22:33:44:55", "00:11:22:33:44:55"); got != "" {
		t.Fatalf("MAC fallback must not be an automatic name: %q", got)
	}
	if got := recognizedAutoName("10.0.0.8", "00:11:22:33:44:55", []string{"10.0.0.8"}); got != "" {
		t.Fatalf("IP fallback must not be an automatic name: %q", got)
	}
	if got := recognizedAutoName("iphone", "00:11:22:33:44:55", []string{"10.0.0.8"}); got != "iphone" {
		t.Fatalf("expected DHCP hostname to remain, got %q", got)
	}
}

func TestUpdateTerminalMetadataUpdatesSnapshotAndDetailsWithoutRefresh(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	ctx := context.Background()
	id := "mac:00:11:22:33:44:55"
	if err := storage.UpsertTerminal(ctx, id, "00:11:22:33:44:55", "iphone", time.Now().UTC()); err != nil {
		t.Fatalf("upsert terminal: %v", err)
	}
	terminal := model.Terminal{ID: id, AutoName: "iphone", DisplayName: "iphone", PrimaryIPv4: "10.0.0.8"}
	monitor := &Monitor{store: storage, snapshot: model.DashboardSnapshot{Terminals: []model.Terminal{terminal}}, terminalDetails: map[string]model.TerminalDetail{id: {Terminal: terminal, FamilySummaries: map[string]model.Terminal{"ipv4": terminal}}}}

	detail, err := monitor.UpdateTerminalMetadata(ctx, id, "iPhone 13 PM", "Tom 的手机")
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if detail.Terminal.DisplayName != "iPhone 13 PM" || detail.Terminal.Remark != "Tom 的手机" || detail.FamilySummaries["ipv4"].CustomName != "iPhone 13 PM" {
		t.Fatalf("detail not updated: %#v", detail)
	}
	snapshot := monitor.Snapshot()
	if snapshot.Terminals[0].DisplayName != "iPhone 13 PM" || snapshot.Terminals[0].Remark != "Tom 的手机" {
		t.Fatalf("snapshot not updated: %#v", snapshot.Terminals[0])
	}
}

func TestOrientConnectionMapsUploadAndDownloadForLocalSource(t *testing.T) {
	_, network, _ := net.ParseCIDR("10.0.0.0/24")

	view, ok := orientConnection("ipv4", routeros.FirewallConnection{
		Protocol:        "tcp",
		SrcAddress:      "10.0.0.31",
		SrcPort:         "51998",
		DstAddress:      "8.8.8.8",
		DstPort:         "443",
		ReplySrcAddress: "8.8.8.8",
		ReplySrcPort:    "443",
		ReplyDstAddress: "10.0.0.31",
		ReplyDstPort:    "51998",
		OrigBytes:       "2048",
		ReplBytes:       "8192",
		OrigRate:        "128000",
		ReplRate:        "512000",
	}, []*net.IPNet{network}, nil)
	if !ok {
		t.Fatal("expected connection to be oriented")
	}
	if view.LocalAddress != "10.0.0.31" {
		t.Fatalf("unexpected local address: %s", view.LocalAddress)
	}
	if view.CurrentUploadBytes != 2048 || view.CurrentDownloadBytes != 8192 {
		t.Fatalf("unexpected byte mapping: %#v", view)
	}
	if view.UploadBps != 128000 || view.DownloadBps != 512000 {
		t.Fatalf("unexpected rate mapping: %#v", view)
	}
	if view.PublicAddress != "" {
		t.Fatalf("expected unchanged local reply address to be omitted, got %q", view.PublicAddress)
	}
}

func TestCompareTerminalAddressUsesNumericIPv4Order(t *testing.T) {
	terminals := []model.Terminal{
		{ID: "86", PrimaryIPv4: "10.0.0.86"},
		{ID: "115", PrimaryIPv4: "10.0.0.115"},
		{ID: "6", PrimaryIPv4: "10.0.0.6"},
		{ID: "v6", PrimaryIPv6: "fc00::1"},
	}
	sort.Slice(terminals, func(left, right int) bool { return compareTerminalAddress(terminals[left], terminals[right]) < 0 })
	got := []string{terminals[0].ID, terminals[1].ID, terminals[2].ID, terminals[3].ID}
	want := []string{"6", "86", "115", "v6"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected order: got %v want %v", got, want)
	}
}

func TestOrientConnectionMapsReplySideWhenReplySourceIsLocal(t *testing.T) {
	_, network, _ := net.ParseCIDR("10.0.0.0/24")

	view, ok := orientConnection("ipv4", routeros.FirewallConnection{
		Protocol:        "tcp",
		SrcAddress:      "8.8.8.8",
		SrcPort:         "443",
		DstAddress:      "203.0.113.9",
		DstPort:         "55000",
		ReplySrcAddress: "10.0.0.50",
		ReplySrcPort:    "55000",
		ReplyDstAddress: "8.8.8.8",
		ReplyDstPort:    "443",
		OrigBytes:       "12000",
		ReplBytes:       "4000",
		OrigRate:        "640000",
		ReplRate:        "128000",
	}, []*net.IPNet{network}, nil)
	if !ok {
		t.Fatal("expected connection to be oriented")
	}
	if view.LocalAddress != "10.0.0.50" {
		t.Fatalf("unexpected local address: %s", view.LocalAddress)
	}
	if view.CurrentUploadBytes != 4000 || view.CurrentDownloadBytes != 12000 {
		t.Fatalf("unexpected byte mapping: %#v", view)
	}
	if view.UploadBps != 128000 || view.DownloadBps != 640000 {
		t.Fatalf("unexpected rate mapping: %#v", view)
	}
}

func TestDeriveLocalCIDRsIncludesLANIPv6AndExactNeighbors(t *testing.T) {
	networks := deriveLocalCIDRs(
		nil,
		[]string{"pppoe-out1"},
		[]routeros.IPAddress{
			{Address: "10.0.0.1/24", Interface: "lan"},
			{Address: "198.51.100.2/24", Interface: "wan"},
		},
		[]routeros.IPv6Address{
			{Address: "fc00::1001/64", Interface: "lan"},
			{Address: "2408:826c:6912:4516::1/64", Interface: "pppoe-out1"},
			{Address: "fd00::1/64", Interface: "lo"},
		},
		[]routeros.IPv6Neighbor{
			{Address: "240e:1234::88", Interface: "lan"},
			{Address: "240e:9999::88", Interface: "wan"},
		},
	)

	for _, address := range []string{"10.0.0.42", "fc00::2222", "240e:1234::88"} {
		if !containsIP(networks, address) {
			t.Errorf("expected %s to be recognized as local", address)
		}
	}
	for _, address := range []string{"198.51.100.42", "2408:826c:6912:4516::99", "240e:1234::89", "240e:9999::88", "fd00::2"} {
		if containsIP(networks, address) {
			t.Errorf("expected %s to be excluded", address)
		}
	}
}

func TestTerminalFamilySummaryOnlyIncludesSelectedFamily(t *testing.T) {
	terminal := model.Terminal{
		IPv4:        []string{"10.0.0.2"},
		IPv6:        []string{"fc00::2"},
		PrimaryIPv4: "10.0.0.2",
		PrimaryIPv6: "fc00::2",
	}
	connections := []model.TerminalConnection{
		{Family: "ipv4", UploadBps: 10, DownloadBps: 20, UploadBytes: 100, DownloadBytes: 200},
		{Family: "ipv6", UploadBps: 30, DownloadBps: 40, UploadBytes: 300, DownloadBytes: 400},
		{Family: "ipv6", UploadBps: 50, DownloadBps: 60, UploadBytes: 500, DownloadBytes: 600},
	}

	summary := terminalFamilySummary(terminal, connections, "ipv6")
	if summary.ConnectionCount != 2 || summary.CurrentUploadBps != 80 || summary.CurrentDownloadBps != 100 {
		t.Fatalf("unexpected IPv6 current summary: %#v", summary)
	}
	if summary.TotalUploadBytes != 800 || summary.TotalDownloadBytes != 1000 {
		t.Fatalf("unexpected IPv6 byte summary: %#v", summary)
	}
	if len(summary.IPv4) != 0 || summary.PrimaryIPv4 != "" || summary.PrimaryIPv6 != "fc00::2" {
		t.Fatalf("unexpected IPv6 address projection: %#v", summary)
	}
}

func TestOrientConnectionPrefersExactRouterAddressOutsideTerminalCIDRs(t *testing.T) {
	_, lan, _ := net.ParseCIDR("fc00::/64")
	routerAddresses := map[string]routerAssignedAddress{
		"2408:826c:6912:4516::1": {Family: "ipv6", Interface: "pppoe-out1"},
	}

	view, ok := orientConnection("ipv6", routeros.FirewallConnection{
		Protocol:        "tcp",
		SrcAddress:      "2001:db8::20",
		DstAddress:      "2408:826c:6912:4516::1",
		DstPort:         "8291",
		ReplySrcAddress: "2408:826c:6912:4516::1",
		ReplyDstAddress: "2001:db8::20",
		OrigBytes:       "1000",
		ReplBytes:       "250",
	}, []*net.IPNet{lan}, routerAddresses)
	if !ok || !view.RouterSelf {
		t.Fatalf("expected exact WAN address to identify RouterOS self: %#v", view)
	}
	if view.LocalAddress != "2408:826c:6912:4516::1" || view.CurrentUploadBytes != 250 || view.CurrentDownloadBytes != 1000 {
		t.Fatalf("unexpected RouterOS reply-side orientation: %#v", view)
	}

	_, ok = orientConnection("ipv6", routeros.FirewallConnection{
		SrcAddress:      "2408:826c:6912:4516::99",
		ReplySrcAddress: "2001:db8::20",
	}, []*net.IPNet{lan}, routerAddresses)
	if ok {
		t.Fatal("expected another address in the WAN prefix to remain external")
	}

	view, ok = orientConnection("ipv6", routeros.FirewallConnection{
		SrcAddress:      "fc00::20",
		ReplySrcAddress: "2408:826c:6912:4516::1",
	}, []*net.IPNet{lan}, routerAddresses)
	if !ok || view.RouterSelf || view.LocalAddress != "fc00::20" {
		t.Fatalf("expected original-source LAN terminal ownership to remain intact: %#v", view)
	}
}

func TestRouterAddressesShareStableTerminalIdentity(t *testing.T) {
	addresses := deriveRouterAddresses(
		[]routeros.IPAddress{
			{Address: "10.0.0.1/24", Interface: "lan"},
			{Address: "198.51.100.2/32", Interface: "pppoe-out1"},
			{Address: "192.0.2.1/24", Interface: "disabled", Disabled: "true"},
		},
		[]routeros.IPv6Address{
			{Address: "fc00::1001/64", Interface: "lan"},
			{Address: "::1/128", Interface: "lo"},
		},
	)
	for _, address := range []string{"10.0.0.1", "198.51.100.2", "fc00::1001", "0:0:0:0:0:0:0:1"} {
		if got := terminalIdentity("", address, addresses); got != routerTerminalID {
			t.Errorf("expected %s to use %s, got %s", address, routerTerminalID, got)
		}
	}
	if got := terminalIdentity("", "192.0.2.1", addresses); got == routerTerminalID {
		t.Fatal("disabled address must not identify RouterOS self")
	}
	if got := preferredRouterAddress(addresses, "ipv4"); got != "10.0.0.1" {
		t.Fatalf("expected LAN IPv4 to be preferred, got %s", got)
	}
	if got := preferredRouterAddress(addresses, "ipv6"); got != "fc00::1001" {
		t.Fatalf("expected LAN IPv6 to be preferred, got %s", got)
	}
}

func TestConnectionStatusUsesWinboxReplyAndAssuredFlags(t *testing.T) {
	tests := []struct {
		seenReply string
		assured   string
		want      string
	}{
		{seenReply: "false", assured: "false", want: "未见回包"},
		{seenReply: "true", assured: "false", want: "已见回包"},
		{seenReply: "true", assured: "true", want: "已见回包 · Assured"},
	}
	for _, test := range tests {
		if got := connectionStatus(test.seenReply, test.assured); got != test.want {
			t.Errorf("connectionStatus(%q, %q) = %q, want %q", test.seenReply, test.assured, got, test.want)
		}
	}
}

func TestFillRateSampleGapsCarriesLastMeasuredRate(t *testing.T) {
	start := time.Date(2026, time.July, 13, 0, 0, 11, 0, time.UTC)
	samples := []model.RateSample{
		{Timestamp: start, UploadBps: 10, DownloadBps: 20},
		{Timestamp: start.Add(3 * time.Second), UploadBps: 40, DownloadBps: 50},
	}

	got := fillRateSampleGaps(samples)
	if len(got) != 4 {
		t.Fatalf("expected four continuous samples, got %d", len(got))
	}
	for index := 0; index < 3; index++ {
		if !got[index].Timestamp.Equal(start.Add(time.Duration(index) * time.Second)) {
			t.Fatalf("sample %d timestamp = %s", index, got[index].Timestamp)
		}
	}
	if got[1].UploadBps != 10 || got[2].DownloadBps != 20 {
		t.Fatalf("gap samples must retain the last measured rate: %#v", got)
	}
	if got[3].UploadBps != 40 || got[3].DownloadBps != 50 {
		t.Fatalf("actual sample must remain unchanged: %#v", got[3])
	}
}

func TestDownsampleRateSamplesAveragesBuckets(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC().Truncate(time.Minute)
	samples := []model.RateSample{
		{Timestamp: start, UploadBps: 10, DownloadBps: 20},
		{Timestamp: start.Add(20 * time.Second), UploadBps: 30, DownloadBps: 40},
		{Timestamp: start.Add(time.Minute), UploadBps: 50, DownloadBps: 60},
	}
	got := downsampleRateSamples(samples, time.Minute)
	if len(got) != 2 {
		t.Fatalf("expected two buckets, got %#v", got)
	}
	if got[0].UploadBps != 20 || got[0].DownloadBps != 30 || got[1].UploadBps != 50 {
		t.Fatalf("unexpected bucket averages: %#v", got)
	}
}

func TestDownsampleRateSamplesCapsNewestBuckets(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC().Truncate(time.Minute)
	samples := make([]model.RateSample, 361)
	for index := range samples {
		samples[index] = model.RateSample{
			Timestamp: start.Add(time.Duration(index) * time.Minute),
			UploadBps: float64(index),
		}
	}

	got := downsampleRateSamples(samples, time.Minute)
	if len(got) != 360 {
		t.Fatalf("expected 360 newest buckets, got %d", len(got))
	}
	if !got[0].Timestamp.Equal(start.Add(time.Minute)) || got[len(got)-1].UploadBps != 360 {
		t.Fatalf("unexpected retained bucket range: first=%s last=%#v", got[0].Timestamp, got[len(got)-1])
	}
}

func TestDownsampleLoadSamplesAveragesAndIgnoresUnknownConnections(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC().Truncate(4 * time.Minute)
	samples := []model.LoadSample{
		{Timestamp: start, CPULoadPercent: 10, OnlineTerminalCount: 2, ConnectionCount: -1},
		{Timestamp: start.Add(time.Minute), CPULoadPercent: 30, OnlineTerminalCount: 4, ConnectionCount: 100},
		{Timestamp: start.Add(4 * time.Minute), CPULoadPercent: 50, OnlineTerminalCount: 6, ConnectionCount: 300},
	}

	got := downsampleLoadSamples(samples, 4*time.Minute)
	if len(got) != 2 {
		t.Fatalf("expected two buckets, got %#v", got)
	}
	if got[0].CPULoadPercent != 20 || got[0].OnlineTerminalCount != 3 || got[0].ConnectionCount != 100 {
		t.Fatalf("unexpected first bucket: %#v", got[0])
	}
	if got[1].ConnectionCount != 300 {
		t.Fatalf("unexpected second bucket: %#v", got[1])
	}
}

func TestViewerActivityExtendsWithoutRepeatedTransition(t *testing.T) {
	monitor := &Monitor{}
	start := time.Date(2026, time.July, 13, 1, 0, 0, 0, time.UTC)

	activeUntil, becameActive := monitor.markViewerActive(start)
	if !becameActive || !activeUntil.Equal(start.Add(viewerHeartbeatTTL)) {
		t.Fatalf("first heartbeat must activate until %s: until=%s active=%v", start.Add(viewerHeartbeatTTL), activeUntil, becameActive)
	}
	activeUntil, becameActive = monitor.markViewerActive(start.Add(10 * time.Second))
	if becameActive || !activeUntil.Equal(start.Add(40*time.Second)) {
		t.Fatalf("renewal must only extend activity: until=%s active=%v", activeUntil, becameActive)
	}
	if !monitor.viewerActive(start.Add(39*time.Second)) || monitor.viewerActive(start.Add(40*time.Second)) {
		t.Fatal("activity must expire exactly at activeUntil")
	}
}
