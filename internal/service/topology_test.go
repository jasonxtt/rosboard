package service

import (
	"testing"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

func TestResolveInterfaceListsAppliesIncludeExcludeThenStatic(t *testing.T) {
	lists := []routeros.InterfaceList{{Name: "base"}, {Name: "LAN", Include: "base", Exclude: "blocked"}, {Name: "blocked"}}
	members := []routeros.InterfaceListMember{{List: "base", Interface: "bridge"}, {List: "blocked", Interface: "ether1"}, {List: "LAN", Interface: "vlan20"}, {List: "LAN", Interface: "ignored", Disabled: "true"}}
	resolved, warnings := resolveInterfaceLists(lists, members)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if _, ok := resolved["lan"]["bridge"]; !ok {
		t.Fatal("included bridge missing")
	}
	if _, ok := resolved["lan"]["vlan20"]; !ok {
		t.Fatal("static member missing")
	}
	if _, ok := resolved["lan"]["ether1"]; ok {
		t.Fatal("excluded member remained")
	}
}

func TestResolveInterfaceListsDetectsCycle(t *testing.T) {
	_, warnings := resolveInterfaceLists([]routeros.InterfaceList{{Name: "a", Include: "b"}, {Name: "b", Include: "a"}}, nil)
	if len(warnings) == 0 {
		t.Fatal("expected include cycle warning")
	}
}

func TestDeriveTerminalScopeRejectsPrivateWANAndNeighborPrefixes(t *testing.T) {
	scope := deriveTerminalScope(config.RouterOSConfig{}, []routeros.Interface{{Name: "bridge", Type: "bridge"}, {Name: "wan", Type: "ether"}, {Name: "wireguard1", Type: "wireguard"}}, []routeros.IPAddress{{Address: "10.0.0.1/24", Interface: "bridge"}, {Address: "192.168.100.2/24", Interface: "wan"}, {Address: "10.8.0.1/24", Interface: "wireguard1"}}, nil, []routeros.InterfaceList{{Name: "LAN"}, {Name: "WAN"}}, []routeros.InterfaceListMember{{List: "LAN", Interface: "bridge"}, {List: "WAN", Interface: "wan"}}, nil, nil, nil, nil, []routeros.RoutingRoute{{DstAddress: "0.0.0.0/0", ImmediateGateway: "wan", Active: "true"}})
	if !scope.addressInScope("10.0.0.88") {
		t.Fatal("LAN address missing")
	}
	if scope.addressInScope("192.168.100.88") || scope.addressInScope("10.8.0.88") {
		t.Fatalf("WAN/tunnel prefix leaked: %#v", scope.Prefixes)
	}
}

func TestDefaultRouteViaLANNextHopDoesNotOverrideStrongLANEvidence(t *testing.T) {
	scope := deriveTerminalScope(config.RouterOSConfig{}, []routeros.Interface{{Name: "lan", Type: "bridge"}}, []routeros.IPAddress{{Address: "10.0.0.1/24", Interface: "lan"}}, nil, []routeros.InterfaceList{{Name: "LAN"}}, []routeros.InterfaceListMember{{List: "LAN", Interface: "lan"}}, []routeros.DHCPServer{{Name: "dhcp", Interface: "lan"}}, nil, nil, nil, []routeros.RoutingRoute{{DstAddress: "0.0.0.0/0", ImmediateGateway: "10.0.0.99%lan", Active: "true"}})
	if scope.Interfaces["lan"].Role != InterfaceRoleLAN || !scope.addressInScope("10.0.0.88") {
		t.Fatalf("LAN next-hop route must not demote LAN: %#v", scope)
	}
}

func TestDeriveTerminalScopeIPv6NDPrefixAndManualTunnel(t *testing.T) {
	scope := deriveTerminalScope(config.RouterOSConfig{TerminalScope: config.TerminalScopeConfig{IncludeInterfaces: []string{"wireguard1"}}}, []routeros.Interface{{Name: "lan", Type: "bridge"}, {Name: "wireguard1", Type: "wireguard"}}, []routeros.IPAddress{{Address: "10.0.0.1/24", Interface: "lan"}, {Address: "10.8.0.1/24", Interface: "wireguard1"}}, []routeros.IPv6Address{{Address: "fd86::1/64", Interface: "lan", Advertise: "true"}, {Address: "fe80::1/64", Interface: "lan"}}, []routeros.InterfaceList{{Name: "LAN"}}, []routeros.InterfaceListMember{{List: "LAN", Interface: "lan"}}, nil, nil, nil, []routeros.IPv6NDPrefix{{Interface: "lan", Prefix: "fd86::/64"}}, nil)
	for _, address := range []string{"fd86::8", "10.8.0.7"} {
		if !scope.addressInScope(address) {
			t.Fatalf("expected %s in scope: %#v", address, scope.Prefixes)
		}
	}
	if scope.addressInScope("fe80::2") {
		t.Fatal("link-local prefix must not be terminal scope")
	}
}

func TestLegacyTerminalCIDRsRemainManual(t *testing.T) {
	scope := deriveTerminalScope(config.RouterOSConfig{TerminalCIDRs: []string{"10.0.0.7/24"}}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if !scope.Legacy || scope.Mode != "legacy" || !scope.addressInScope("10.0.0.99") {
		t.Fatalf("legacy scope lost: %#v", scope)
	}
}
