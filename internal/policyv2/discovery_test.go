package policyv2

import (
	"context"
	"fmt"
	"testing"

	"rosboard/internal/routeros"
)

type discoveryReader map[routeros.ReadMenu][]routeros.RouterOSObject

func (r discoveryReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	return r[menu], nil
}

type discoveryErrorReader struct {
	objects map[routeros.ReadMenu][]routeros.RouterOSObject
	errors  map[routeros.ReadMenu]error
}

func (r discoveryErrorReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	if err := r.errors[menu]; err != nil {
		return nil, err
	}
	return r.objects[menu], nil
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
	if len(discovery.WANs) != 2 || discovery.WANs[0].Interface != "ether1" || !discovery.WANs[0].Proven || discovery.WANs[1].Interface != "wireguard1" || discovery.WANs[1].Proven {
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
	if _, ok := byName["wireguard1"]; !ok {
		t.Fatalf("unproven WireGuard candidate was incorrectly hidden from ingress discovery: %#v", byName)
	}
	if !containsString(byName["wireguard1"].CoveredBy, "VPN-LAN") || !containsString(byName["bridge"].CoveredBy, "LAN") {
		t.Fatalf("interface-list coverage missing: %#v", byName)
	}
}

func TestDiscoveryExcludesExplicitLANFromWANAndAddsFixedVPNCandidates(t *testing.T) {
	reader := discoveryReader{
		routeros.ReadMenuSystemResource: {{"board-name": "router", "version": "7.22.3"}},
		routeros.ReadMenuInterface: {
			{"name": "lan", "type": "ether", "running": "true", "disabled": "false"},
			{"name": "pppoe-out1", "type": "pppoe-out", "running": "true", "disabled": "false"},
			{"name": "wg-out", "type": "wg", "running": "true", "disabled": "false"},
			{"name": "l2tp-out1", "type": "l2tp-out", "running": "true", "disabled": "false"},
			{"name": "gre-out1", "type": "gre", "running": "true", "disabled": "false"},
			{"name": "l2tp-in1", "type": "l2tp-in", "running": "true", "dynamic": "true"},
			{"name": "ovpn-in", "type": "ovpn-in", "running": "true", "disabled": "false"},
		},
		routeros.ReadMenuIPRoute: {
			// RouterOS omits active when this static main-table route is inactive.
			{"dst-address": "0.0.0.0/0", "gateway": "10.0.0.99", "immediate-gw": "10.0.0.99%lan", "routing-table": "main", "distance": "10", "disabled": "false"},
			{"dst-address": "0.0.0.0/0", "gateway": "10.0.0.99", "immediate-gw": "10.0.0.99%lan", "routing-table": "cmcc", "distance": "2", "active": "true", "disabled": "false"},
			{"dst-address": "0.0.0.0/0", "gateway": "pppoe-out1", "immediate-gw": "pppoe-out1", "routing-table": "main", "distance": "1", "active": "true", "disabled": "false"},
		},
		routeros.ReadMenuInterfaceList:       {{"name": "LAN"}},
		routeros.ReadMenuInterfaceListMember: {{"list": "LAN", "interface": "lan", "dynamic": "false", "disabled": "false"}},
		routeros.ReadMenuBridgePort:          {},
		routeros.ReadMenuIPAddress:           {{"interface": "lan", "address": "192.0.2.100/24"}},
	}
	discovery, err := NewScanner(reader).Scan(context.Background(), "edge")
	if err != nil {
		t.Fatal(err)
	}

	wans := make(map[string]WANCandidate)
	for _, candidate := range discovery.WANs {
		wans[candidate.Interface] = candidate
	}
	if _, ok := wans["lan"]; ok {
		t.Fatalf("explicit LAN member was exposed as WAN: %#v", wans["lan"])
	}
	if !wans["pppoe-out1"].Proven {
		t.Fatalf("active PPPoE default route was not proven: %#v", wans["pppoe-out1"])
	}
	for _, name := range []string{"wg-out", "l2tp-out1", "gre-out1"} {
		candidate, ok := wans[name]
		if !ok || candidate.Proven || !candidate.PointToPoint {
			t.Fatalf("fixed VPN/tunnel candidate = %#v, want unproven point-to-point: %s", candidate, name)
		}
	}
	for _, name := range []string{"l2tp-in1", "ovpn-in"} {
		if _, ok := wans[name]; ok {
			t.Fatalf("inbound VPN interface was exposed as WAN: %s", name)
		}
	}

	byName := make(map[string]TrafficIngressCandidate)
	for _, candidate := range discovery.TrafficIngress {
		byName[candidate.Name] = candidate
	}
	if !containsString(byName["lan"].CoveredBy, "LAN") {
		t.Fatalf("LAN member was not retained as covered ingress candidate: %#v", byName["lan"])
	}
}

func TestDefaultRoutesTreatMissingOrDisabledActiveAsInactive(t *testing.T) {
	routes := defaultRoutes([]routeros.RouterOSObject{
		{"dst-address": "0.0.0.0/0", "gateway": "192.0.2.1", "immediate-gw": "192.0.2.1%ether1"},
		{"dst-address": "0.0.0.0/0", "gateway": "192.0.2.2", "immediate-gw": "192.0.2.2%ether1", "active": "true", "disabled": "true"},
	}, "ipv4")
	if len(routes) != 2 || routes[0].Active || routes[0].Proven || routes[1].Active || routes[1].Proven {
		t.Fatalf("missing/disabled route activity was treated as active: %#v", routes)
	}
}

func TestDiscoveryFailsWhenIPv4RoutesCannotBeRead(t *testing.T) {
	reader := discoveryErrorReader{
		objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
			routeros.ReadMenuInterface:      {{"name": "ether1", "type": "ether"}},
			routeros.ReadMenuSystemResource: {{"board-name": "router"}},
		},
		errors: map[routeros.ReadMenu]error{
			routeros.ReadMenuIPRoute: fmt.Errorf("ipv4 unavailable"),
		},
	}

	discovery, err := NewScanner(reader).Scan(context.Background(), "edge")
	if err == nil || discovery.Available {
		t.Fatalf("route discovery failure was not propagated: discovery=%#v err=%v", discovery, err)
	}
}

func TestDiscoveryReturnsOptionalReadWarningsWithPartialResults(t *testing.T) {
	reader := discoveryErrorReader{
		objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
			routeros.ReadMenuInterface:      {{"name": "ether1", "type": "ether", "running": "true"}},
			routeros.ReadMenuSystemResource: {{"board-name": "router"}},
			routeros.ReadMenuIPRoute:        {{"dst-address": "0.0.0.0/0", "gateway": "192.0.2.1", "immediate-gw": "192.0.2.1%ether1", "active": "true"}},
		},
		errors: map[routeros.ReadMenu]error{
			routeros.ReadMenuIPv6Route:           fmt.Errorf("ipv6 unavailable"),
			routeros.ReadMenuIPDHCPClient:        fmt.Errorf("dhcp unavailable"),
			routeros.ReadMenuInterfaceList:       fmt.Errorf("interface lists unavailable"),
			routeros.ReadMenuInterfaceListMember: fmt.Errorf("interface list members unavailable"),
			routeros.ReadMenuBridgePort:          fmt.Errorf("bridge ports unavailable"),
			routeros.ReadMenuIPAddress:           fmt.Errorf("ipv4 addresses unavailable"),
			routeros.ReadMenuIPv6Address:         fmt.Errorf("ipv6 addresses unavailable"),
			routeros.ReadMenuIPv6DHCPClient:      fmt.Errorf("ipv6 dhcp unavailable"),
		},
	}

	discovery, err := NewScanner(reader).Scan(context.Background(), "edge")
	if err != nil || !discovery.Available || len(discovery.WANs) != 1 || len(discovery.Warnings) != 8 {
		t.Fatalf("optional discovery failures were not preserved as warnings: discovery=%#v err=%v", discovery, err)
	}
}
