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
