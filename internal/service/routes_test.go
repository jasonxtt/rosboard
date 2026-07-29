package service

import (
	"reflect"
	"testing"

	"rosboard/internal/routeros"
)

func TestRouteMatcherUsesRuleAndLongestPrefix(t *testing.T) {
	matcher := newRouteMatcher(
		[]routeros.RoutingRule{{ID: "*1", Comment: "LAN split", SrcAddress: "10.0.0.0/24", Action: "lookup-only-in-table", Table: "wan2"}},
		[]routeros.RoutingRoute{
			{ID: "*A", DstAddress: "0.0.0.0/0", RoutingTable: "wan2", Gateway: "10.1.0.1", Distance: "1", Active: "true"},
			{ID: "*B", DstAddress: "8.8.8.0/24", RoutingTable: "wan2", ImmediateGateway: "10.2.0.1%ether2", Distance: "1", Active: "true"},
		},
	)
	got := matcher.match("ipv4", "10.0.0.20", "8.8.8.8", "lan", "")
	if got.RuleID != "*1" || got.Table != "wan2" || got.RouteID != "*B" || got.Destination != "8.8.8.0/24" || got.State != "inferred" {
		t.Fatalf("unexpected attribution: %#v", got)
	}
}

func TestRouteMatcherReportsECMPAndIPv6(t *testing.T) {
	matcher := newRouteMatcher(nil, []routeros.RoutingRoute{
		{ID: "*1", AFI: "ip6", DstAddress: "::/0", RoutingTable: "main", Gateway: "fe80::1", Distance: "1", Active: "true"},
		{ID: "*2", AFI: "ip6", DstAddress: "::/0", RoutingTable: "main", Gateway: "fe80::2", Distance: "1", Active: "true"},
	})
	got := matcher.match("ipv6", "fd00::20", "2001:4860:4860::8888", "lan", "main")
	if got.State != "ambiguous" || !reflect.DeepEqual(got.Gateways, []string{"fe80::1", "fe80::2"}) {
		t.Fatalf("expected IPv6 ECMP attribution, got %#v", got)
	}
}

func TestRouteMatcherLookupFallsBackToMain(t *testing.T) {
	matcher := newRouteMatcher(
		[]routeros.RoutingRule{{ID: "*1", SrcAddress: "10.0.0.0/24", Action: "lookup", Table: "missing"}},
		[]routeros.RoutingRoute{{ID: "*A", DstAddress: "0.0.0.0/0", RoutingTable: "main", Gateway: "10.0.0.1", Distance: "1", Active: "true"}},
	)
	got := matcher.match("ipv4", "10.0.0.20", "1.1.1.1", "lan", "")
	if got.RouteID != "*A" || got.RuleID != "*1" || got.Basis != "routing rule fallback" {
		t.Fatalf("expected main-table fallback, got %#v", got)
	}
}

func TestRouteMatcherNormalizesGatewayAndResolvesPhysicalEgress(t *testing.T) {
	topology := newInterfaceTopology(
		[]routeros.Interface{{Name: "wan", Type: "ether"}, {Name: "wan-xray", Type: "ether"}, {Name: "pppoe-out1", Type: "pppoe-out"}, {Name: "vlan35", Type: "vlan"}},
		[]routeros.EthernetInterface{{Name: "wan"}, {Name: "wan-xray"}},
		[]routeros.PPPoEClient{{Name: "pppoe-out1", Interface: "wan"}}, []routeros.VLANInterface{{Name: "vlan35", Interface: "wan"}}, nil,
	)

	t.Run("pppoe carrier", func(t *testing.T) {
		matcher := newRouteMatcher(nil, []routeros.RoutingRoute{{ID: "*1", DstAddress: "0.0.0.0/0", RoutingTable: "main", ImmediateGateway: "pppoe-out1", Distance: "1", Active: "true"}}).withTopology(topology)
		got := matcher.match("ipv4", "10.0.0.8", "1.1.1.1", "lan", "")
		if !reflect.DeepEqual(got.Gateways, []string{"pppoe-out1"}) || !reflect.DeepEqual(got.RouteInterfaces, []string{"pppoe-out1"}) || !reflect.DeepEqual(got.EgressInterfaces, []string{"wan"}) {
			t.Fatalf("unexpected PPPoE attribution: %#v", got)
		}
	})

	t.Run("direct gateway fallback", func(t *testing.T) {
		matcher := newRouteMatcher(nil, []routeros.RoutingRoute{{ID: "*1", DstAddress: "0.0.0.0/0", RoutingTable: "main", Gateway: "pppoe-out1", Distance: "1", Active: "true"}}).withTopology(topology)
		got := matcher.match("ipv4", "10.0.0.8", "1.1.1.1", "lan", "")
		if !reflect.DeepEqual(got.RouteInterfaces, []string{"pppoe-out1"}) || !reflect.DeepEqual(got.EgressInterfaces, []string{"wan"}) {
			t.Fatalf("unexpected direct-interface fallback: %#v", got)
		}
	})

	t.Run("gateway percent interface", func(t *testing.T) {
		matcher := newRouteMatcher(nil, []routeros.RoutingRoute{{ID: "*1", DstAddress: "0.0.0.0/0", RoutingTable: "main", ImmediateGateway: "10.0.2.1%wan-xray", Distance: "1", Active: "true"}}).withTopology(topology)
		got := matcher.match("ipv4", "10.0.0.8", "1.1.1.1", "lan", "")
		if !reflect.DeepEqual(got.Gateways, []string{"10.0.2.1"}) || !reflect.DeepEqual(got.RouteInterfaces, []string{"wan-xray"}) || !reflect.DeepEqual(got.EgressInterfaces, []string{"wan-xray"}) {
			t.Fatalf("unexpected scoped-gateway attribution: %#v", got)
		}
	})

	t.Run("vlan parent", func(t *testing.T) {
		matcher := newRouteMatcher(nil, []routeros.RoutingRoute{{ID: "*1", DstAddress: "0.0.0.0/0", RoutingTable: "main", ImmediateGateway: "192.0.2.1%vlan35", Distance: "1", Active: "true"}}).withTopology(topology)
		got := matcher.match("ipv4", "10.0.0.8", "1.1.1.1", "lan", "")
		if !reflect.DeepEqual(got.RouteInterfaces, []string{"vlan35"}) || !reflect.DeepEqual(got.EgressInterfaces, []string{"wan"}) {
			t.Fatalf("unexpected VLAN attribution: %#v", got)
		}
	})
}

func TestRouteMatcherPhysicalEgressHandlesIPv6ECMPAndUnknownCarrier(t *testing.T) {
	topology := newInterfaceTopology(
		[]routeros.Interface{{Name: "wan", Type: "ether"}, {Name: "wan2", Type: "ether"}, {Name: "wireguard1", Type: "wireguard"}},
		[]routeros.EthernetInterface{{Name: "wan"}, {Name: "wan2"}}, nil, nil, nil,
	)
	matcher := newRouteMatcher(nil, []routeros.RoutingRoute{
		{ID: "*1", AFI: "ip6", DstAddress: "::/0", RoutingTable: "main", ImmediateGateway: "fe80::1%wan", Distance: "1", Active: "true"},
		{ID: "*2", AFI: "ip6", DstAddress: "::/0", RoutingTable: "main", ImmediateGateway: "fe80::2%wan2", Distance: "1", Active: "true"},
	}).withTopology(topology)
	got := matcher.match("ipv6", "fd00::8", "2001:4860:4860::8888", "lan", "")
	if got.State != "ambiguous" || !reflect.DeepEqual(got.Gateways, []string{"fe80::1", "fe80::2"}) || !reflect.DeepEqual(got.EgressInterfaces, []string{"wan", "wan2"}) {
		t.Fatalf("unexpected IPv6 ECMP attribution: %#v", got)
	}

	unknown := newRouteMatcher(nil, []routeros.RoutingRoute{{DstAddress: "0.0.0.0/0", RoutingTable: "main", ImmediateGateway: "wireguard1", Distance: "1", Active: "true"}}).withTopology(topology).match("ipv4", "10.0.0.8", "1.1.1.1", "lan", "")
	if len(unknown.EgressInterfaces) != 0 || !reflect.DeepEqual(unknown.RouteInterfaces, []string{"wireguard1"}) {
		t.Fatalf("WireGuard must not get guessed physical egress: %#v", unknown)
	}

	cyclicTopology := interfaceTopology{physical: map[string]bool{}, parents: map[string]string{"vlan-a": "vlan-b", "vlan-b": "vlan-a"}}
	if egress := cyclicTopology.physicalEgress("vlan-a"); egress != "" {
		t.Fatalf("cyclic topology must not resolve an egress, got %q", egress)
	}
}
