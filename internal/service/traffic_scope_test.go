package service

import (
	"reflect"
	"strings"
	"testing"

	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/routeros"
)

func trafficTestTerminalScope(lan ...string) terminalScope {
	interfaces := make(map[string]InterfaceEvidence, len(lan))
	for _, name := range lan {
		interfaces[name] = InterfaceEvidence{Interface: name, Role: InterfaceRoleLAN}
	}
	return terminalScope{Interfaces: interfaces}
}

func selectedTrafficNames(scope trafficScope) []string { return scope.selectedNames() }

func TestDeriveTrafficScopePPPoEIncludesStandbyAndExcludesParent(t *testing.T) {
	interfaces := []routeros.Interface{
		{Name: "ether1", Type: "ether", Running: "true"},
		{Name: "vlan35", Type: "vlan", Running: "true"},
		{Name: "pppoe-out1", Type: "pppoe-out", Running: "true"},
		{Name: "pppoe-out2", Type: "pppoe-out", Running: "false"},
	}
	scope := deriveTrafficScope(config.RouterOSConfig{}, trafficTestTerminalScope(), interfaces,
		[]routeros.PPPoEClient{{Name: "pppoe-out1", Interface: "ether1", Running: "true"}, {Name: "pppoe-out2", Interface: "vlan35", Running: "false"}}, nil, nil, nil, nil)
	if got, want := selectedTrafficNames(scope), []string{"pppoe-out1", "pppoe-out2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected=%v want=%v", got, want)
	}
	if scope.Interfaces[1].Running {
		t.Fatal("standby PPPoE must remain selected while down")
	}
}

func TestDeriveTrafficScopeDHCPStaticCellularAndInternalExclusions(t *testing.T) {
	interfaces := []routeros.Interface{
		{Name: "lan", Type: "bridge", Running: "true"},
		{Name: "ether1", Type: "ether", Running: "true"},
		{Name: "ether2", Type: "ether", Running: "true"},
		{Name: "lte1", Type: "lte", Running: "true"},
		{Name: "wireguard1", Type: "wireguard", Running: "true"},
		{Name: "wan-xray", Type: "ether", Running: "true"},
	}
	lists := []routeros.InterfaceList{{Name: "WAN"}}
	members := []routeros.InterfaceListMember{{List: "WAN", Interface: "ether2"}, {List: "WAN", Interface: "wireguard1"}, {List: "WAN", Interface: "wan-xray"}}
	routes := []routeros.RoutingRoute{{DstAddress: "0.0.0.0/0", ImmediateGateway: "ether2", Active: "true"}, {DstAddress: "::/0", ImmediateGateway: "wireguard1", Active: "true"}}
	scope := deriveTrafficScope(config.RouterOSConfig{}, trafficTestTerminalScope("lan"), interfaces, nil,
		[]routeros.DHCPClient{{Interface: "ether1", Status: "bound"}, {Interface: "lan", Status: "bound"}, {Interface: "internal", Status: "stopped"}}, lists, members, routes)
	if got, want := selectedTrafficNames(scope), []string{"ether1", "lte1", "ether2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected=%v want=%v warnings=%v", got, want, scope.Warnings)
	}
	for _, name := range selectedTrafficNames(scope) {
		if name == "lan" || name == "wireguard1" || name == "wan-xray" {
			t.Fatalf("internal interface %s selected", name)
		}
	}
}

func TestDeriveTrafficScopeDHCPBackupOverridesAndEmpty(t *testing.T) {
	interfaces := []routeros.Interface{{Name: "ether1", Type: "ether"}, {Name: "custom", Type: "ether"}, {Name: "wan-xray", Type: "ether"}}
	scope := deriveTrafficScope(config.RouterOSConfig{TrafficScope: config.TrafficScopeConfig{IncludeInterfaces: []string{"custom"}, ExcludeInterfaces: []string{"ether1"}}}, trafficTestTerminalScope(), interfaces, nil,
		[]routeros.DHCPClient{{Interface: "ether1", AddDefaultRoute: "true"}}, nil, nil, nil)
	if got, want := selectedTrafficNames(scope), []string{"custom"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected=%v want=%v", got, want)
	}
	if !scope.OverridesApplied || scope.Interfaces[0].Automatic {
		t.Fatalf("manual override lost: %#v", scope)
	}

	empty := deriveTrafficScope(config.RouterOSConfig{}, trafficTestTerminalScope(), []routeros.Interface{{Name: "lan", Type: "bridge", Running: "true"}}, nil, nil, nil, nil, nil)
	if len(empty.Interfaces) != 0 || !strings.Contains(strings.Join(empty.Warnings, " "), "未能自动识别") {
		t.Fatalf("empty scope fell back or omitted warning: %#v", empty)
	}
}

func TestDeriveTrafficScopeLegacyPreservesExactNames(t *testing.T) {
	scope := deriveTrafficScope(config.RouterOSConfig{TrafficInterfaces: []string{"missing", "pppoe-out1"}}, trafficTestTerminalScope(), []routeros.Interface{{Name: "pppoe-out1", Type: "pppoe-out", Disabled: "true"}}, nil, nil, nil, nil, nil)
	if !scope.Legacy || scope.Mode != "legacy" || !reflect.DeepEqual(selectedTrafficNames(scope), []string{"missing", "pppoe-out1"}) {
		t.Fatalf("legacy selection changed: %#v", scope)
	}
	if len(scope.Warnings) != 2 {
		t.Fatalf("expected missing and disabled warnings, got %v", scope.Warnings)
	}
}

func TestTrafficScopeDoesNotChangeTerminalEligibility(t *testing.T) {
	terminals := []model.Terminal{{ID: "lan", State: "online", PrimaryInterface: "lan"}, {ID: "wan", State: "online", PrimaryInterface: "ether1"}}
	scope := trafficTestTerminalScope("lan")
	scope.Interfaces["ether1"] = InterfaceEvidence{Interface: "ether1", Role: InterfaceRoleWAN}
	if got := connectedLANDeviceCount(terminals, scope); got != 1 {
		t.Fatalf("traffic-independent terminal count=%d", got)
	}
}
