package policyv2

import (
	"strings"
	"testing"

	"rosboard/internal/routeros"
)

// Two target lists of one egress that both contain the same domain must be
// merged into a single DNS Static entry, because RouterOS refuses to enable a
// second entry with the same identity ("failure: entry already exists").
func TestRoutingDNSStaticMergeAcrossTargetLists(t *testing.T) {
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	egress := Egress{ID: "wan-a", Name: "WAN A", Enabled: true, FakeAlias: "192.0.2.53", DNSUpstream: "1.1.1.1"}
	families := []EgressFamily{{Family: FamilyIPv4, Enabled: true, RouteTable: "wan-a-table"}}
	targets := []*routingTargetProjection{
		{
			id: "anthropic", source: Source{ID: "anthropic", Kind: KindDomain, Name: "Anthropic 域名"},
			rules: []SourceRule{
				{RuleType: "DOMAIN-SUFFIX", Domain: "anthropic.com"},
				{RuleType: "DOMAIN-SUFFIX", Domain: "claude.ai"},
			},
			list: "rb_anthropic", active: true, plainActive: true,
		},
		{
			id: "claude", source: Source{ID: "claude", Kind: KindDomain, Name: "Claude 域名"},
			rules: []SourceRule{
				{RuleType: "DOMAIN-SUFFIX", Domain: "anthropic.com"},
				{RuleType: "DOMAIN-SUFFIX", Domain: "cdn.usefathom.com"},
			},
			list: "rb_claude", active: true, plainActive: true,
		},
	}

	dnsStatics := []routingDNSStaticEntry{}
	buildRoutingDomainObjects(&result, add, egress, families, targets, "no", "manager", "device", map[string]int{}, &dnsStatics)
	sortRoutingDNSStaticEntries(dnsStatics)
	dnsStatics = dedupeRoutingDNSStaticEntries(&result, dnsStatics)
	for _, entry := range dnsStatics {
		add(entry.logicalID, routeros.MenuIPDNSStatic, "dns", entry.fields)
	}

	counts := map[string]int{}
	addressLists := map[string]string{}
	for _, object := range result.Objects {
		if object.Menu != string(routeros.MenuIPDNSStatic) {
			continue
		}
		counts[object.Fields["name"]]++
		addressLists[object.Fields["name"]] = object.Fields["address-list"]
	}
	if counts["anthropic.com"] != 1 {
		t.Fatalf("overlapping domain must merge into exactly one DNS Static entry, got %d", counts["anthropic.com"])
	}
	if counts["claude.ai"] != 1 || counts["cdn.usefathom.com"] != 1 {
		t.Fatalf("non-overlapping domains must survive the merge: claude.ai=%d cdn.usefathom.com=%d", counts["claude.ai"], counts["cdn.usefathom.com"])
	}
	if addressLists["anthropic.com"] != "rb_anthropic" {
		t.Fatalf("merged entry must keep the first (winning) projection address-list, got %q", addressLists["anthropic.com"])
	}
	foundWarning := false
	for _, warning := range result.Warnings {
		if warning.Code != "routing_dns_projection_merged" {
			continue
		}
		foundWarning = true
		for _, want := range []string{"Anthropic 域名", "Claude 域名", "anthropic.com"} {
			if !strings.Contains(warning.Reason, want) {
				t.Fatalf("merge warning must mention %q, got %q", want, warning.Reason)
			}
		}
	}
	if !foundWarning {
		t.Fatalf("overlapping target lists must emit a routing_dns_projection_merged warning, got %#v", result.Warnings)
	}
}

// A disabled projection of an inactive consumer must lose to an enabled entry
// of an active consumer even when the disabled one sorts first.
func TestDedupeRoutingDNSStaticPrefersEnabledProjection(t *testing.T) {
	fields := func(disabled string, addressList string) map[string]string {
		return map[string]string{"name": "example.com", "type": "FWD", "forward-to": "rosboard_fwd", "address-list": addressList, "disabled": disabled, "match-subdomain": "yes"}
	}
	entries := []routingDNSStaticEntry{
		{priority: 10, egressID: "wan-a", targetID: "list-a", domain: "example.com", logicalID: "routing-dns:wan-a:list-a:DOMAIN-SUFFIX:example.com", fields: fields("yes", "rb_a"), egressName: "WAN A", targetName: "A 域名"},
		{priority: 10, egressID: "wan-a", targetID: "list-b", domain: "example.com", logicalID: "routing-dns:wan-a:list-b:DOMAIN-SUFFIX:example.com", fields: fields("no", "rb_b"), egressName: "WAN A", targetName: "B 域名"},
	}

	result := DesiredResult{}
	kept := dedupeRoutingDNSStaticEntries(&result, entries)
	if len(kept) != 1 {
		t.Fatalf("duplicate identity must collapse to one entry, got %d", len(kept))
	}
	if kept[0].fields["disabled"] != "no" || kept[0].targetID != "list-b" {
		t.Fatalf("enabled projection must win over the disabled one: disabled=%s target=%s", kept[0].fields["disabled"], kept[0].targetID)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("merge must emit exactly one warning, got %#v", result.Warnings)
	}
	if !strings.Contains(result.Warnings[0].Reason, "B 域名") || !strings.Contains(result.Warnings[0].Reason, "A 域名") {
		t.Fatalf("warning must name both target lists, got %q", result.Warnings[0].Reason)
	}
}

func TestDedupeRoutingDNSStaticKeepsDistinctIdentities(t *testing.T) {
	entries := []routingDNSStaticEntry{
		{egressID: "wan-a", targetID: "list-a", domain: "example.com", fields: map[string]string{"name": "example.com", "type": "FWD", "forward-to": "rosboard_fwd", "address-list": "rb_a", "disabled": "no", "match-subdomain": "yes"}},
		{egressID: "wan-a", targetID: "list-b", domain: "example.com", fields: map[string]string{"name": "example.com", "type": "FWD", "forward-to": "rosboard_fwd", "address-list": "rb_b", "disabled": "no", "match-subdomain": "no"}},
		{egressID: "wan-b", targetID: "list-a", domain: "example.com", fields: map[string]string{"name": "example.com", "type": "FWD", "forward-to": "rosboard_fwd2", "address-list": "rb_c", "disabled": "no", "match-subdomain": "yes"}},
	}

	result := DesiredResult{}
	kept := dedupeRoutingDNSStaticEntries(&result, entries)
	if len(kept) != 3 {
		t.Fatalf("entries differing in match-subdomain or forwarder are not duplicates, got %d: %#v", len(kept), kept)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("distinct identities must not warn, got %#v", result.Warnings)
	}
}
