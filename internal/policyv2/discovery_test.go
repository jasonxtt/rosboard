package policyv2

import (
	"context"
	"testing"

	"rosboard/internal/routeros"
)

type discoveryReader map[routeros.ReadMenu][]routeros.RouterOSObject

func (r discoveryReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	return r[menu], nil
}

func TestDiscoveryBuildsWANAndTrafficIngressCandidates(t *testing.T) {
	reader := discoveryReader{
		routeros.ReadMenuSystemResource: {{"board-name": "router", "version": "7.22.3"}},
		routeros.ReadMenuInterface: {
			{"name": "ether1", "type": "ether", "running": "true", "disabled": "false"},
			{"name": "ether2", "type": "ether", "running": "true"},
			{"name": "ether3", "type": "ether", "running": "true"},
			{"name": "bridge", "type": "bridge", "running": "true"},
			{"name": "vlan10", "type": "vlan", "running": "true"},
			{"name": "wireguard1", "type": "wg", "running": "true"},
			{"name": "l2tp-in1", "type": "l2tp-in", "running": "true", "dynamic": "true"},
		},
		routeros.ReadMenuIPRoute:             {{".id": "*1", "dst-address": "0.0.0.0/0", "gateway": "192.0.2.1", "immediate-gw": "192.0.2.1%ether1", "routing-table": "main", "distance": "1", "active": "true"}},
		routeros.ReadMenuInterfaceList:       {{"name": "all"}, {"name": "static"}, {"name": "WAN"}, {"name": "LAN"}, {"name": "VPN-LAN"}},
		routeros.ReadMenuInterfaceListMember: {{"list": "LAN", "interface": "bridge"}, {"list": "VPN-LAN", "interface": "wireguard1"}},
		routeros.ReadMenuBridgePort:          {{"bridge": "bridge", "interface": "ether2"}},
		routeros.ReadMenuIPAddress:           {{"interface": "bridge", "address": "10.0.0.1/24"}, {"interface": "vlan10", "address": "10.10.0.1/24"}, {"interface": "wireguard1", "address": "10.20.0.1/24"}},
	}
	discovery, err := NewScanner(reader).Scan(context.Background(), "edge")
	if err != nil {
		t.Fatal(err)
	}
	if !discovery.Available || discovery.Snapshot.Fingerprint == "" {
		t.Fatalf("unexpected discovery state: %#v", discovery)
	}
	if len(discovery.WANs) != 1 || discovery.WANs[0].Interface != "ether1" || !discovery.WANs[0].Proven {
		t.Fatalf("unexpected WAN candidates: %#v", discovery.WANs)
	}
	if len(discovery.TrafficIngress) != 6 || discovery.TrafficIngress[0].Name != "LAN" || len(discovery.TrafficIngress[0].Addresses) != 1 {
		t.Fatalf("unexpected traffic ingress candidates: %#v", discovery.TrafficIngress)
	}
	byName := make(map[string]TrafficIngressCandidate)
	for _, candidate := range discovery.TrafficIngress {
		byName[candidate.Name] = candidate
	}
	for _, hidden := range []string{"all", "static", "WAN", "ether1", "ether2", "l2tp-in1"} {
		if _, ok := byName[hidden]; ok {
			t.Fatalf("unsafe or unstable candidate was exposed: %s", hidden)
		}
	}
	if !containsString(byName["wireguard1"].CoveredBy, "VPN-LAN") || !containsString(byName["bridge"].CoveredBy, "LAN") {
		t.Fatalf("interface-list coverage missing: %#v", byName)
	}
}
