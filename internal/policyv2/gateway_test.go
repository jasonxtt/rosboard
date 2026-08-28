package policyv2

import (
	"context"
	"testing"

	"rosboard/internal/routeros"
)

type gatewayTestReader map[routeros.ReadMenu][]routeros.RouterOSObject

func (r gatewayTestReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	return r[menu], nil
}

func TestResolveGatewayIgnoresUnselectedPolicyTables(t *testing.T) {
	reader := gatewayTestReader{
		routeros.ReadMenuInterface: {
			{"name": "wan-xray", "type": "ether"},
		},
		routeros.ReadMenuIPRoute: {
			{"dst-address": "0.0.0.0/0", "gateway": "10.0.2.99", "immediate-gw": "10.0.2.99%wan-xray", "routing-table": "sing-box-v4", "active": "true"},
			{"dst-address": "0.0.0.0/0", "gateway": "10.0.2.1", "immediate-gw": "10.0.2.1%wan-xray", "routing-table": "main", "active": "true"},
		},
	}
	resolution, err := ResolveGateway(context.Background(), reader, EgressFamily{Family: FamilyIPv4, WANInterface: "wan-xray"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.PointToPoint || resolution.Gateway != "10.0.2.1" || len(resolution.Candidates) != 1 {
		t.Fatalf("unexpected gateway resolution: %#v", resolution)
	}
}

func TestResolveGatewayAllowsPointToPointWithoutIPGateway(t *testing.T) {
	reader := gatewayTestReader{
		routeros.ReadMenuInterface: {{"name": "pppoe-out1", "type": "pppoe-out"}},
	}
	resolution, err := ResolveGateway(context.Background(), reader, EgressFamily{Family: FamilyIPv4, WANInterface: "pppoe-out1"})
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.PointToPoint || resolution.Gateway != "" {
		t.Fatalf("unexpected point-to-point resolution: %#v", resolution)
	}
}

func TestResolveGatewayUsesBoundDHCPGateway(t *testing.T) {
	reader := gatewayTestReader{
		routeros.ReadMenuInterface:    {{"name": "ether1", "type": "ether"}},
		routeros.ReadMenuIPDHCPClient: {{"interface": "ether1", "status": "bound", "gateway": "192.0.2.1"}},
		routeros.ReadMenuIPRoute:      {},
	}
	resolution, err := ResolveGateway(context.Background(), reader, EgressFamily{Family: FamilyIPv4, WANInterface: "ether1"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Gateway != "192.0.2.1" || len(resolution.Candidates) != 1 {
		t.Fatalf("unexpected DHCP gateway resolution: %#v", resolution)
	}
}

func TestResolveGatewayDoesNotGuessFromARPOnly(t *testing.T) {
	reader := gatewayTestReader{
		routeros.ReadMenuInterface: {
			{"name": "ether1", "type": "ether"},
		},
		routeros.ReadMenuIPRoute: {
			{"dst-address": "0.0.0.0/0", "gateway": "10.0.2.1", "immediate-gw": "10.0.2.1%wan-xray", "routing-table": "sing-box-v4", "active": "true"},
		},
	}
	resolution, err := ResolveGateway(context.Background(), reader, EgressFamily{Family: FamilyIPv4, WANInterface: "ether1"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Gateway != "" || len(resolution.Candidates) != 0 {
		t.Fatalf("ARP-only or unrelated route was guessed: %#v", resolution)
	}
}

func TestResolveGatewayLeavesAmbiguousCandidatesUnselected(t *testing.T) {
	reader := gatewayTestReader{
		routeros.ReadMenuInterface: {{"name": "ether1", "type": "ether"}},
		routeros.ReadMenuIPRoute: {
			{"dst-address": "0.0.0.0/0", "gateway": "192.0.2.1", "immediate-gw": "192.0.2.1%ether1", "routing-table": "main", "active": "true"},
			{"dst-address": "0.0.0.0/0", "gateway": "192.0.2.2", "immediate-gw": "192.0.2.2%ether1", "routing-table": "main", "active": "true"},
		},
	}
	resolution, err := ResolveGateway(context.Background(), reader, EgressFamily{Family: FamilyIPv4, WANInterface: "ether1"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Gateway != "" || len(resolution.Candidates) != 2 {
		t.Fatalf("ambiguous candidates were selected: %#v", resolution)
	}
}
