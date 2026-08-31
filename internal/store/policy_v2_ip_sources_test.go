package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

func saveIPSource(t *testing.T, repository *PolicyRepository, id, egressID, name string, rules []policyv2.SourceRule) policyv2.Source {
	t.Helper()
	source, err := repository.SaveSource(context.Background(), policyv2.Source{ID: id, EgressID: egressID, Type: "manual", Kind: policyv2.KindIP, Name: name, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingSourceVersion(context.Background(), policyv2.SourceVersion{ID: "version-" + id, SourceID: id, SHA256: id, CompressedYAML: []byte("gzip")}, rules); err != nil {
		t.Fatal(err)
	}
	return source
}

func saveDomainSource(t *testing.T, repository *PolicyRepository, id, egressID, name string, rules []policyv2.SourceRule) policyv2.Source {
	t.Helper()
	source, err := repository.SaveSource(context.Background(), policyv2.Source{ID: id, EgressID: egressID, Type: "manual", Kind: policyv2.KindDomain, Name: name, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingSourceVersion(context.Background(), policyv2.SourceVersion{ID: "version-" + id, SourceID: id, SHA256: id, CompressedYAML: []byte("gzip")}, rules); err != nil {
		t.Fatal(err)
	}
	return source
}

func desiredObjectsByMenu(objects []policyv2.DesiredObject, menu routeros.MutationMenu) []policyv2.DesiredObject {
	result := make([]policyv2.DesiredObject, 0)
	for _, object := range objects {
		if object.Menu == string(menu) {
			result = append(result, object)
		}
	}
	return result
}

func desiredObjectsByLogicalPrefix(objects []policyv2.DesiredObject, prefix string) []policyv2.DesiredObject {
	result := make([]policyv2.DesiredObject, 0)
	for _, object := range objects {
		if len(object.LogicalID) >= len(prefix) && object.LogicalID[:len(prefix)] == prefix {
			result = append(result, object)
		}
	}
	return result
}

func hasDesiredField(objects []policyv2.DesiredObject, key, value string) bool {
	for _, object := range objects {
		if object.Fields[key] == value {
			return true
		}
	}
	return false
}

func TestPolicyV2DesiredIPOnlySharedHasNoDNSObjectsAndFiltersFamily(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	// DNS transport configuration is deliberately invalid: an IP-only egress
	// must not be blocked by it.
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-ip", Name: "WAN IP", ListMode: policyv2.ListModeShared, ListName: "route_wan_ip",
		DNSUpstream: "1.1.1.1", FakeAlias: "not-an-ip", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1", RouteTable: "ip4"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	saveIPSource(t, repository, "ip-a", "wan-ip", "IP A", []policyv2.SourceRule{
		{RuleType: "IP-CIDR", Domain: "91.108.0.0/16"},
		{RuleType: "IP-CIDR6", Domain: "2001:67c:4e8::/48"},
	})

	desired, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("IP-only desired is blocked: %#v", desired.Blockers)
	}
	for _, menu := range []routeros.MutationMenu{routeros.MenuIPDNSForwarders, routeros.MenuIPDNSStatic, routeros.MenuIPFirewallMangle, routeros.MenuIPFirewallNAT} {
		// DNS transport (mark/dnat) and forwarder/static must be absent; only
		// the business mangle may appear for this shared list.
		if menu == routeros.MenuIPFirewallMangle {
			continue
		}
		if len(desiredObjectsByMenu(desired.Objects, menu)) != 0 {
			t.Fatalf("IP-only desired created DNS objects on %s", menu)
		}
	}
	addr := desiredObjectsByLogicalPrefix(desired.Objects, "source-addr:ip-a:")
	if len(addr) != 1 {
		t.Fatalf("expected exactly one IPv4 address entry, got %#v", addr)
	}
	if addr[0].Menu != string(routeros.MenuIPFirewallAddressList) || addr[0].Fields["address"] != "91.108.0.0/16" || !strings.HasPrefix(addr[0].Fields["list"], "rb_src_") {
		t.Fatalf("unexpected IPv4 address entry: %#v", addr[0])
	}
	if len(desiredObjectsByMenu(desired.Objects, routeros.MenuIPv6FirewallAddressList)) != 0 {
		t.Fatal("disabled IPv6 family must not materialize address entries")
	}
	mangle := desiredObjectsByMenu(desired.Objects, routeros.MenuIPFirewallMangle)
	if !hasDesiredField(mangle, "dst-address-list", addr[0].Fields["list"]) {
		t.Fatalf("business mangle missing for shared IP list: %#v", mangle)
	}
	routes := desiredObjectsByLogicalPrefix(desired.Objects, "route:wan-ip:")
	if len(routes) != 1 || routes[0].Fields["routing-table"] != "ip4" {
		t.Fatalf("existing route pipeline broken: %#v", routes)
	}
}

func TestPolicyV2DesiredDomainAndIPSharedUseOneListAndMangleSet(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-both", Name: "WAN Both", ListMode: policyv2.ListModeShared, ListName: "route_both",
		DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53", Enabled: true,
		Families: []policyv2.EgressFamily{
			{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1", RouteTable: "both4"},
			{Family: policyv2.FamilyIPv6, Enabled: true, WANInterface: "wan6", Gateway: "2001:db8:1::1", RouteTable: "both6"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	saveDomainSource(t, repository, "dom-a", "wan-both", "Dom A", []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "api.example.com"}})
	saveIPSource(t, repository, "ip-a", "wan-both", "IP A", []policyv2.SourceRule{
		{RuleType: "IP-CIDR", Domain: "91.108.0.0/16"},
		{RuleType: "IP-CIDR6", Domain: "2001:67c:4e8::/48"},
	})

	desired, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("domain+ip desired is blocked: %#v", desired.Blockers)
	}
	if len(desiredObjectsByMenu(desired.Objects, routeros.MenuIPDNSForwarders)) != 1 {
		t.Fatal("domain+ip egress lost its DNS forwarder")
	}
	statics := desiredObjectsByMenu(desired.Objects, routeros.MenuIPDNSStatic)
	if len(statics) != 1 || statics[0].Fields["name"] != "api.example.com" {
		t.Fatalf("unexpected DNS static rules: %#v", statics)
	}
	addr := desiredObjectsByLogicalPrefix(desired.Objects, "source-addr:")
	if len(addr) != 2 {
		t.Fatalf("expected IPv4+IPv6 entries, got %#v", addr)
	}
	if !hasDesiredField(addr, "address", "91.108.0.0/16") || !hasDesiredField(addr, "address", "2001:67c:4e8::/48") {
		t.Fatalf("IP entries must materialize in the source list: %#v", addr)
	}
	if hasDesiredField(desiredObjectsByMenu(desired.Objects, routeros.MenuIPv6FirewallAddressList), "address", "91.108.0.0/16") {
		t.Fatal("IPv4 entry leaked into the IPv6 address-list menu")
	}
	for _, menu := range []routeros.MutationMenu{routeros.MenuIPFirewallMangle, routeros.MenuIPv6FirewallMangle} {
		markConnection := 0
		lanRouting := 0
		connectionMarks := map[string]bool{}
		for _, object := range desiredObjectsByMenu(desired.Objects, menu) {
			if strings.HasPrefix(object.Fields["dst-address-list"], "rb_src_") && object.Fields["action"] == "mark-connection" {
				markConnection++
				connectionMarks[object.Fields["new-connection-mark"]] = true
			}
			if len(object.LogicalID) >= len("lan-routing:") && object.LogicalID[:len("lan-routing:")] == "lan-routing:" {
				lanRouting++
			}
		}
		if markConnection != 2 || len(connectionMarks) != 1 || lanRouting != 1 {
			t.Fatalf("shared domain+IP must use source matchers with one shared mark and routing rule per family (%s): connection=%d marks=%v routing=%d", menu, markConnection, connectionMarks, lanRouting)
		}
	}
}

func TestPolicyV2DesiredDomainAndIPDedicatedKeepSharedRouteTable(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-ded", Name: "WAN Ded", ListMode: policyv2.ListModeDedicated,
		DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.54", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1", RouteTable: "ded4"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	saveDomainSource(t, repository, "dom-a", "wan-ded", "Dom A", []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "api.example.com"}})
	saveIPSource(t, repository, "ip-a", "wan-ded", "IP A", []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "91.108.0.0/16"}})

	desired, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("dedicated desired is blocked: %#v", desired.Blockers)
	}
	addr := desiredObjectsByLogicalPrefix(desired.Objects, "source-addr:ip-a:")
	if len(addr) != 1 {
		t.Fatalf("missing dedicated IP entry: %#v", addr)
	}
	ipList := addr[0].Fields["list"]
	if ipList == "" || ipList == "route_both" {
		t.Fatalf("dedicated IP list name not generated: %q", ipList)
	}
	statics := desiredObjectsByMenu(desired.Objects, routeros.MenuIPDNSStatic)
	if len(statics) != 1 || statics[0].Fields["address-list"] == ipList {
		t.Fatalf("dedicated lists must stay per source: %#v", statics)
	}
	routes := desiredObjectsByLogicalPrefix(desired.Objects, "route:wan-ded:")
	if len(routes) != 1 || routes[0].Fields["routing-table"] != "ded4" {
		t.Fatalf("dedicated mode must reuse the egress route table: %#v", routes)
	}
}

func TestPolicyV2DesiredRebuildsDNSWhenDomainSourcesComeAndGo(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-mix", Name: "WAN Mix", ListMode: policyv2.ListModeShared, ListName: "route_mix",
		DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.55", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1", RouteTable: "mix4"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	domain := saveDomainSource(t, repository, "dom-a", "wan-mix", "Dom A", []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "api.example.com"}})
	saveIPSource(t, repository, "ip-a", "wan-mix", "IP A", []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "91.108.0.0/16"}})

	withDomain, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(withDomain.Objects) == 0 || len(desiredObjectsByMenu(withDomain.Objects, routeros.MenuIPDNSForwarders)) != 1 {
		t.Fatal("domain+ip start state must contain DNS objects")
	}
	addrBefore := desiredObjectsByLogicalPrefix(withDomain.Objects, "source-addr:ip-a:")

	stale, err := repository.GetSource(ctx, domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteSource(ctx, stale.ID, stale.Revision); err != nil {
		t.Fatal(err)
	}
	withoutDomain, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(desiredObjectsByMenu(withoutDomain.Objects, routeros.MenuIPDNSForwarders)) != 0 ||
		len(desiredObjectsByMenu(withoutDomain.Objects, routeros.MenuIPDNSStatic)) != 0 ||
		len(desiredObjectsByLogicalPrefix(withoutDomain.Objects, "dns-mark:")) != 0 ||
		len(desiredObjectsByLogicalPrefix(withoutDomain.Objects, "dns-dnat:")) != 0 {
		t.Fatal("removing the last domain source must clean up all DNS objects")
	}
	addrAfter := desiredObjectsByLogicalPrefix(withoutDomain.Objects, "source-addr:ip-a:")
	if len(addrAfter) != len(addrBefore) {
		t.Fatalf("IP routing chain must survive domain removal: before=%d after=%d", len(addrBefore), len(addrAfter))
	}
	if len(addrAfter) != 1 || !hasDesiredField(desiredObjectsByMenu(withoutDomain.Objects, routeros.MenuIPFirewallMangle), "dst-address-list", addrAfter[0].Fields["list"]) {
		t.Fatal("business mangle must survive domain removal")
	}

	saveDomainSource(t, repository, "dom-b", "wan-mix", "Dom B", []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "back.example.com"}})
	withDomainAgain, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(desiredObjectsByMenu(withDomainAgain.Objects, routeros.MenuIPDNSForwarders)) != 1 {
		t.Fatal("adding a domain source must rebuild DNS objects")
	}
	addrAgain := desiredObjectsByLogicalPrefix(withDomainAgain.Objects, "source-addr:ip-a:")
	if len(addrAgain) != len(addrBefore) {
		t.Fatalf("adding a domain source must keep IP entries stable: before=%d after=%d", len(addrBefore), len(addrAgain))
	}
}

func TestPolicyV2DesiredSharedIPSourceDeleteKeepsOtherSourcesAndMangle(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-del", Name: "WAN Del", ListMode: policyv2.ListModeShared, ListName: "route_del",
		DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.56", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1", RouteTable: "del4"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	saveIPSource(t, repository, "ip-a", "wan-del", "IP A", []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "91.108.0.0/16"}})
	ipB := saveIPSource(t, repository, "ip-b", "wan-del", "IP B", []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "8.8.8.8/32"}})

	before, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(desiredObjectsByLogicalPrefix(before.Objects, "source-addr:")) != 2 {
		t.Fatalf("expected entries from both IP sources: %#v", before.Objects)
	}
	staleB, err := repository.GetSource(ctx, ipB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteSource(ctx, staleB.ID, staleB.Revision); err != nil {
		t.Fatal(err)
	}
	after, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	remaining := desiredObjectsByLogicalPrefix(after.Objects, "source-addr:")
	if len(remaining) != 1 || remaining[0].Fields["address"] != "91.108.0.0/16" {
		t.Fatalf("deleting one IP source must keep the other source's entries: %#v", remaining)
	}
	if !hasDesiredField(desiredObjectsByMenu(after.Objects, routeros.MenuIPFirewallMangle), "dst-address-list", remaining[0].Fields["list"]) {
		t.Fatal("deleting one IP source must not remove the shared business mangle")
	}
}

func TestPolicyV2LegacySourceSchemaDefaultsToDomainKind(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rosboard.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := `CREATE TABLE policy_v2_sources (
		id TEXT PRIMARY KEY,
		egress_id TEXT NOT NULL,
		type TEXT NOT NULL,
		name TEXT NOT NULL,
		url TEXT NOT NULL,
		schedule TEXT NOT NULL,
		enabled INTEGER NOT NULL,
		active_version_id TEXT NOT NULL,
		pending_version_id TEXT NOT NULL,
		last_good_version_id TEXT NOT NULL,
		etag TEXT NOT NULL,
		last_modified TEXT NOT NULL,
		next_run_at INTEGER NOT NULL,
		revision INTEGER NOT NULL,
		pending_delete INTEGER NOT NULL,
		applied INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO policy_v2_sources (id, egress_id, type, name, url, schedule, enabled, active_version_id, pending_version_id, last_good_version_id, etag, last_modified, next_run_at, revision, pending_delete, applied, created_at, updated_at) VALUES ('legacy-a', '', 'manual', 'Legacy', '', 'manual', 1, '', '', '', '', '', 0, 1, 0, 0, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	storage, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	sources, err := repository.ListSources(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Kind != policyv2.KindDomain {
		t.Fatalf("legacy source must read as domain: %#v", sources)
	}
	// Metadata edits on the legacy row must keep working and stay domain.
	sources[0].Name = "Legacy Renamed"
	updated, err := repository.SaveSource(context.Background(), sources[0])
	if err != nil {
		t.Fatal(err)
	}
	if updated.Kind != policyv2.KindDomain || updated.Revision != 2 {
		t.Fatalf("legacy source update failed: %#v", updated)
	}
}
