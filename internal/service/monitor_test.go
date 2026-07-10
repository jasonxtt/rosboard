package service

import (
	"net"
	"reflect"
	"sort"
	"testing"

	"rosboard/internal/model"
	"rosboard/internal/routeros"
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
