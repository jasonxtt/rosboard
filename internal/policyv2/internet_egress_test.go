package policyv2

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/routeros"
)

type internetEgressReader struct {
	objects map[routeros.ReadMenu][]routeros.RouterOSObject
	errors  map[routeros.ReadMenu]error
}

func (r internetEgressReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	if err := r.errors[menu]; err != nil {
		return nil, err
	}
	return r.objects[menu], nil
}

func TestDiscoverInternetEgressesIncludesStandbyRoutesAndSkipsLocalInterfaces(t *testing.T) {
	reader := internetEgressReader{objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
		routeros.ReadMenuInterface: {
			{"name": "lan", "type": "bridge", "running": "true"},
			{"name": "pppoe-out1", "type": "pppoe-out", "running": "true"},
			{"name": "ether2", "type": "ether", "running": "false"},
			{"name": "wg-out", "type": "wireguard", "running": "true"},
			{"name": "l2tp-in1", "type": "l2tp-in", "running": "true"},
			{"name": "vlan-wan", "type": "vlan", "running": "true"},
		},
		routeros.ReadMenuIPRoute: {
			{".id": "*1", "dst-address": "0.0.0.0/0", "gateway": "pppoe-out1", "immediate-gw": "pppoe-out1", "routing-table": "main", "active": "true"},
			{".id": "*2", "dst-address": "0.0.0.0/0", "gateway": "192.0.2.1", "immediate-gw": "192.0.2.1%ether2", "routing-table": "main", "active": "false"},
			{".id": "*3", "dst-address": "0.0.0.0/0", "gateway": "10.0.0.99", "immediate-gw": "10.0.0.99%lan", "routing-table": "main", "active": "true"},
			{".id": "*4", "dst-address": "0.0.0.0/0", "gateway": "wg-out", "immediate-gw": "wg-out", "routing-table": "vpn", "active": "true"},
			{".id": "*5", "dst-address": "0.0.0.0/0", "gateway": "l2tp-in1", "immediate-gw": "l2tp-in1", "routing-table": "vpn", "active": "true"},
		},
		routeros.ReadMenuIPv6Route: {
			{".id": "*6", "dst-address": "::/0", "gateway": "vlan-wan", "immediate-interface": "vlan-wan", "routing-table": "main", "active": "true"},
			{".id": "*7", "dst-address": "::/0", "gateway": "2001:db8::1", "immediate-gw": "2001:db8::1%ether2", "routing-table": "main", "active": "false"},
		},
		routeros.ReadMenuInterfaceList:       {{"name": "LAN"}},
		routeros.ReadMenuInterfaceListMember: {{"list": "LAN", "interface": "lan"}},
	}}
	egresses, issues := discoverInternetEgresses(context.Background(), reader, accesscontrol.Scope{})
	if got := strings.Join(egresses[accesscontrol.FamilyIPv4], ","); got != "ether2,pppoe-out1,wg-out" {
		t.Fatalf("IPv4 egresses=%q, want all configured WAN routes except local/inbound VPN", got)
	}
	if got := strings.Join(egresses[accesscontrol.FamilyIPv6], ","); got != "ether2,vlan-wan" {
		t.Fatalf("IPv6 egresses=%q, want immediate-interface and standby route outputs", got)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected egress discovery issues: %#v", issues)
	}
}

func TestDiscoverInternetEgressesFallsBackToCombinedRoutingTable(t *testing.T) {
	reader := internetEgressReader{
		objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
			routeros.ReadMenuInterface: {
				{"name": "wan4", "type": "ether"},
				{"name": "wan6", "type": "vlan"},
			},
			routeros.ReadMenuRoutingRoute: {
				{"afi": "ip", "dst-address": "0.0.0.0/0", "gateway": "wan4", "immediate-interface": "wan4", "routing-table": "main", "active": "true"},
				{"afi": "ip6", "dst-address": "::/0", "gateway": "wan6", "immediate-interface": "wan6", "routing-table": "main", "active": "true"},
			},
		},
		errors: map[routeros.ReadMenu]error{
			routeros.ReadMenuIPRoute:   fmt.Errorf("legacy IPv4 route unavailable"),
			routeros.ReadMenuIPv6Route: fmt.Errorf("legacy IPv6 route unavailable"),
		},
	}
	egresses, issues := discoverInternetEgresses(context.Background(), reader, accesscontrol.Scope{})
	if len(issues) != 0 || strings.Join(egresses[accesscontrol.FamilyIPv4], ",") != "wan4" || strings.Join(egresses[accesscontrol.FamilyIPv6], ",") != "wan6" {
		t.Fatalf("combined route fallback failed: egresses=%#v issues=%#v", egresses, issues)
	}
}

func TestDiscoverInternetEgressesBlocksWhenOnlyLocalDefaultRouteExists(t *testing.T) {
	reader := internetEgressReader{objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
		routeros.ReadMenuInterface:           {{"name": "lan", "type": "bridge"}},
		routeros.ReadMenuIPRoute:             {{"dst-address": "0.0.0.0/0", "gateway": "10.0.0.99", "immediate-gw": "10.0.0.99%lan", "active": "true"}},
		routeros.ReadMenuInterfaceList:       {{"name": "LAN"}},
		routeros.ReadMenuInterfaceListMember: {{"list": "LAN", "interface": "lan"}},
	}}
	egresses, issues := discoverInternetEgresses(context.Background(), reader, accesscontrol.Scope{})
	if len(egresses[accesscontrol.FamilyIPv4]) != 0 || !strings.Contains(issues[accesscontrol.FamilyIPv4], "本地接口") {
		t.Fatalf("local-only default route must be blocked: egresses=%#v issues=%#v", egresses, issues)
	}
}

func TestDiscoverInternetEgressesKeepsPPPoEWhenStaleScopeCallsItLocal(t *testing.T) {
	reader := internetEgressReader{objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
		routeros.ReadMenuInterface: {
			{"name": "pppoe-out1", "type": "pppoe-out", "running": "true"},
		},
		routeros.ReadMenuIPRoute: {
			{"dst-address": "0.0.0.0/0", "gateway": "pppoe-out1", "immediate-gw": "pppoe-out1", "active": "true"},
		},
	}}
	discovery := discoverInternetEgressesDetailed(context.Background(), reader, accesscontrol.Scope{LocalInterfaces: []string{"pppoe-out1"}})
	if got := strings.Join(discovery.Egresses[accesscontrol.FamilyIPv4], ","); got != "pppoe-out1" {
		t.Fatalf("known PPPoE WAN must not be rejected by stale local scope evidence: %q, issues=%#v", got, discovery.Issues)
	}
}

func TestDiscoverInternetEgressesOffersLocallyClassifiedInterfaceForManualChoice(t *testing.T) {
	reader := internetEgressReader{objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
		routeros.ReadMenuInterface: {
			{"name": "ether-wan", "type": "ether", "running": "true"},
		},
		routeros.ReadMenuIPRoute: {
			{"dst-address": "0.0.0.0/0", "gateway": "192.0.2.1", "immediate-gw": "192.0.2.1%ether-wan", "active": "true"},
		},
	}}
	discovery := discoverInternetEgressesDetailed(context.Background(), reader, accesscontrol.Scope{LocalInterfaces: []string{"ether-wan"}})
	if len(discovery.Egresses[accesscontrol.FamilyIPv4]) != 0 {
		t.Fatalf("a locally classified generic interface must not be auto-selected: %#v", discovery.Egresses)
	}
	if len(discovery.Candidates[accesscontrol.FamilyIPv4]) != 1 || discovery.Candidates[accesscontrol.FamilyIPv4][0].Interface != "ether-wan" {
		t.Fatalf("a locally classified interface must remain manually selectable: %#v", discovery.Candidates)
	}
}

func TestSelectInternetEgressesAcceptsOnlyScannedCandidates(t *testing.T) {
	discovery := internetEgressDiscovery{
		Egresses: map[string][]string{accesscontrol.FamilyIPv4: {}, accesscontrol.FamilyIPv6: {}},
		Issues:   map[string]string{accesscontrol.FamilyIPv4: "未能自动确认 IPv4 出口"},
		Candidates: map[string][]accesscontrol.InternetEgressCandidate{
			accesscontrol.FamilyIPv4: {{Interface: "pppoe-out1"}},
			accesscontrol.FamilyIPv6: {},
		},
	}
	egresses, issues := selectInternetEgresses(discovery, map[string][]string{accesscontrol.FamilyIPv4: {"pppoe-out1"}})
	if strings.Join(egresses[accesscontrol.FamilyIPv4], ",") != "pppoe-out1" || len(issues) != 0 {
		t.Fatalf("valid manual egress selection was not accepted: egresses=%#v issues=%#v", egresses, issues)
	}
	_, issues = selectInternetEgresses(discovery, map[string][]string{accesscontrol.FamilyIPv4: {"ether-lan"}})
	if !strings.Contains(issues[accesscontrol.FamilyIPv4], "不在本次扫描候选") {
		t.Fatalf("unscanned manual interface must be rejected: %#v", issues)
	}
}
