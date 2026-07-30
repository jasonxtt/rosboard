package service

import (
	"testing"

	"rosboard/internal/routeros"
)

func TestParseRouterOSDurationSeconds(t *testing.T) {
	cases := map[string]int64{
		"1d2h3m4s": 93784,
		"5m10s":    310,
		"1w":       604800,
		"45s":      45,
		"":         0,
		"never":    0,
	}
	for input, expected := range cases {
		if got := parseRouterOSDurationSeconds(input); got != expected {
			t.Fatalf("parseRouterOSDurationSeconds(%q) = %d, want %d", input, got, expected)
		}
	}
}

func TestPoolRangeTotal(t *testing.T) {
	cases := map[string]int{
		"10.0.0.10-10.0.0.254":                  245,
		"10.0.0.10-10.0.0.19,10.0.1.0-10.0.1.9": 20,
		"192.168.1.100":                         1,
		"10.0.0.10-10.0.0.19,bogus,10.0.1.5":    11,
		"":                                      0,
		"10.0.0.20-10.0.0.10":                   0,
	}
	for input, expected := range cases {
		if got := poolRangeTotal(input); got != expected {
			t.Fatalf("poolRangeTotal(%q) = %d, want %d", input, got, expected)
		}
	}
}

func TestBuildDHCPComputesPoolUsageAndSortsLeases(t *testing.T) {
	servers := []routeros.DHCPServer{
		{Name: "dhcp1", Interface: "bridge-lan", AddressPool: "pool0", LeaseTime: "30m"},
		{Name: "dhcp2", Interface: "guest", AddressPool: "pool0", Disabled: "true"},
	}
	leases := []routeros.DHCPLease{
		{ID: "*2", Address: "10.0.0.30", Server: "dhcp1", Status: "bound", ExpiresAfter: "10m", Dynamic: "true", MACAddress: "aa:aa:aa:aa:aa:02"},
		{ID: "*1", Address: "10.0.0.9", ActiveAddress: "10.0.0.20", Server: "dhcp1", Status: "bound", HostName: "nas", MACAddress: "aa:aa:aa:aa:aa:01"},
		{ID: "*3", Address: "10.0.0.40", Server: "dhcp1", Status: "waiting"},
		{ID: "*4", Address: "10.0.0.50", Server: "unknown-server", Status: "bound"},
	}
	pools := []routeros.IPPool{
		{Name: "pool0", Ranges: "10.0.0.10-10.0.0.254"},
		{Name: "orphan", Ranges: "10.0.9.1-10.0.9.10"},
	}

	stat := buildDHCP(servers, leases, pools)

	if len(stat.Servers) != 2 || stat.Servers[0].Name != "dhcp1" || stat.Servers[0].AddressPool != "pool0" || !stat.Servers[1].Disabled {
		t.Fatalf("unexpected servers: %#v", stat.Servers)
	}
	if len(stat.Pools) != 2 {
		t.Fatalf("unexpected pools: %#v", stat.Pools)
	}
	pool0 := stat.Pools[0]
	// bound leases on dhcp1/dhcp2 (pool0): *1 and *2; waiting and unknown-server leases excluded.
	if pool0.Total != 245 || pool0.Used != 2 || pool0.Free != 243 || len(pool0.Servers) != 2 {
		t.Fatalf("unexpected pool0: %#v", pool0)
	}
	if pool0.UsedPercent < 0.8 || pool0.UsedPercent > 0.9 {
		t.Fatalf("unexpected pool0 percent: %f", pool0.UsedPercent)
	}
	orphan := stat.Pools[1]
	if orphan.Used != 0 || orphan.Total != 10 || len(orphan.Servers) != 0 {
		t.Fatalf("unexpected orphan pool: %#v", orphan)
	}
	// Sorted numerically by display address: 10.0.0.20(*1) < 10.0.0.30(*2) < 10.0.0.40(*3) < 10.0.0.50(*4).
	if stat.Leases[0].ID != "*1" || stat.Leases[1].ID != "*2" || stat.Leases[2].ID != "*3" || stat.Leases[3].ID != "*4" {
		t.Fatalf("unexpected lease order: %#v", stat.Leases)
	}
	if stat.Leases[0].Address != "10.0.0.20" {
		t.Fatalf("active-address should win: %#v", stat.Leases[0])
	}
	if stat.Leases[1].ExpiresAfter != 600 || !stat.Leases[1].Dynamic {
		t.Fatalf("lease fields not projected: %#v", stat.Leases[1])
	}
}
