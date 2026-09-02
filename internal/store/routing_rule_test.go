package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"rosboard/internal/policyv2"
)

func TestRoutingRuleMigrationIsLosslessAndIdempotent(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":["bridge-lan"]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-a", Name: "WAN A", Priority: 20, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-b", Name: "WAN B", Priority: 10, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-empty", Name: "WAN empty", Priority: 30, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-pending", Name: "WAN pending", Priority: 40, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []policyv2.Source{
		{ID: "target-a", EgressID: "wan-a", Type: "manual", Kind: policyv2.KindDomain, Name: "Target A", Enabled: true},
		{ID: "target-b", EgressID: "wan-a", Type: "manual", Kind: policyv2.KindIP, Name: "Target B", Enabled: false},
		{ID: "target-c", EgressID: "wan-b", Type: "manual", Kind: policyv2.KindIP, Name: "Target C", Enabled: true},
		{ID: "library-only", Type: "manual", Kind: policyv2.KindDomain, Name: "Library only", Enabled: true},
		{ID: "pending-target", EgressID: "wan-a", Type: "manual", Kind: policyv2.KindDomain, Name: "Pending target", Enabled: true},
		{ID: "pending-egress-target", EgressID: "wan-pending", Type: "manual", Kind: policyv2.KindDomain, Name: "Pending egress target", Enabled: true},
	} {
		if _, err := repository.SaveSource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := storage.db.Exec(`UPDATE policy_v2_sources SET pending_delete = 1 WHERE id = 'pending-target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`UPDATE policy_v2_egresses SET pending_delete = 1 WHERE id = 'wan-pending'`); err != nil {
		t.Fatal(err)
	}
	before, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureRoutingRulesMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.DesiredRevision != before.DesiredRevision+1 {
		t.Fatalf("migration desired revision = %d, want %d", after.DesiredRevision, before.DesiredRevision+1)
	}
	authority, err := repository.RoutingAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if authority != policyv2.RoutingRuleAuthorityV1 {
		t.Fatalf("routing authority = %q", authority)
	}
	rules, err := repository.ListRoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("migrated rules = %#v, want one per non-pending egress with targets", rules)
	}
	byEgress := make(map[string]policyv2.RoutingRule, len(rules))
	for _, rule := range rules {
		byEgress[rule.EgressID] = rule
		if !rule.Enabled || rule.Subject.Mode != policyv2.SubjectModeAll || rule.Revision != 1 {
			t.Fatalf("migrated rule lost required defaults: %#v", rule)
		}
		if !reflect.DeepEqual(rule.Ingress, policyv2.TrafficIngressScope{InterfaceLists: []string{"LAN"}, Interfaces: []string{"bridge-lan"}}) {
			t.Fatalf("migrated rule did not take ownership of global ingress: %#v", rule.Ingress)
		}
	}
	if got := byEgress["wan-a"].TargetListIDs; !reflect.DeepEqual(got, []string{"target-a", "target-b"}) {
		t.Fatalf("wan-a targets = %#v", got)
	}
	if got := byEgress["wan-b"].TargetListIDs; !reflect.DeepEqual(got, []string{"target-c"}) {
		t.Fatalf("wan-b targets = %#v", got)
	}
	if _, ok := byEgress["wan-empty"]; ok {
		t.Fatal("empty egress received a migrated routing rule")
	}
	if _, ok := byEgress["wan-pending"]; ok {
		t.Fatal("pending egress received a migrated routing rule")
	}
	for _, id := range []string{"target-a", "target-b", "target-c", "pending-target", "pending-egress-target"} {
		var egressID string
		if err := storage.db.QueryRow(`SELECT egress_id FROM policy_v2_sources WHERE id = ?`, id).Scan(&egressID); err != nil {
			t.Fatal(err)
		}
		if egressID != "" {
			t.Fatalf("source %s retained legacy egress authority %q", id, egressID)
		}
	}
	secondBefore, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureRoutingRulesMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	secondAfter, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if secondAfter.DesiredRevision != secondBefore.DesiredRevision {
		t.Fatalf("replayed migration changed desired revision from %d to %d", secondBefore.DesiredRevision, secondAfter.DesiredRevision)
	}
	replayed, err := repository.ListRoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != len(rules) || replayed[0].ID != rules[0].ID || replayed[1].ID != rules[1].ID {
		t.Fatalf("replayed migration changed rule identities: first=%#v replayed=%#v", rules, replayed)
	}
}

func TestGlobalTrafficIngressStopsBumpingRoutingAfterRuleMigration(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-ingress-default", Name: "WAN ingress default"}); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "target-ingress-default", Name: "Ingress target", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "routing-ingress-default", Name: "Ingress default", EgressID: "wan-ingress-default", TargetListIDs: []string{target.ID},
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll}, Ingress: policyv2.TrafficIngressScope{InterfaceLists: []string{"LAN"}}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureRoutingRulesMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["VLAN20"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	after, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.DesiredRevision != before.DesiredRevision {
		t.Fatalf("compatibility global ingress changed routing revision after rule migration: before=%d after=%d", before.DesiredRevision, after.DesiredRevision)
	}
	rules, err := repository.ListRoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || len(rules[0].Ingress.InterfaceLists) != 1 || rules[0].Ingress.InterfaceLists[0] != "LAN" || len(rules[0].Ingress.Interfaces) != 0 {
		t.Fatalf("global compatibility write changed rule ingress: %#v", rules)
	}
}

func TestRoutingRuleMigrationRunsWhenAnExistingDatabaseReopens(t *testing.T) {
	dataDir := t.TempDir()
	storage, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-reopen", Name: "WAN reopen", Priority: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveSource(ctx, policyv2.Source{ID: "target-reopen", EgressID: "wan-reopen", Type: "manual", Kind: policyv2.KindDomain, Name: "Target"}); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedRepository := reopened.PolicyRepository()
	rules, err := reopenedRepository.ListRoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "routing-rule:legacy-egress:wan-reopen" || !reflect.DeepEqual(rules[0].TargetListIDs, []string{"target-reopen"}) {
		t.Fatalf("reopen migration = %#v", rules)
	}
	var egressID string
	if err := reopened.db.QueryRow(`SELECT egress_id FROM policy_v2_sources WHERE id = 'target-reopen'`).Scan(&egressID); err != nil {
		t.Fatal(err)
	}
	if egressID != "" {
		t.Fatalf("reopen migration left legacy egress association %q", egressID)
	}
}

func TestRoutingRuleMigrationRollsBackBeforeWritingAuthorityMarker(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-fail", Name: "WAN fail"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveSource(ctx, policyv2.Source{ID: "target-fail", EgressID: "wan-fail", Type: "manual", Kind: policyv2.KindIP, Name: "Target"}); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`CREATE TRIGGER fail_routing_migration BEFORE INSERT ON policy_v2_routing_rules BEGIN SELECT RAISE(ABORT, 'injected migration failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureRoutingRulesMigrated(ctx); err == nil {
		t.Fatal("injected migration failure was ignored")
	}
	if _, err := storage.db.Exec(`DROP TRIGGER fail_routing_migration`); err != nil {
		t.Fatal(err)
	}
	authority, err := repository.RoutingAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if authority != "" {
		t.Fatalf("failed migration wrote authority marker %q", authority)
	}
	var ruleCount int
	if err := storage.db.QueryRow(`SELECT count(*) FROM policy_v2_routing_rules`).Scan(&ruleCount); err != nil {
		t.Fatal(err)
	}
	if ruleCount != 0 {
		t.Fatalf("failed migration left %d routing rules", ruleCount)
	}
	var egressID string
	if err := storage.db.QueryRow(`SELECT egress_id FROM policy_v2_sources WHERE id = 'target-fail'`).Scan(&egressID); err != nil {
		t.Fatal(err)
	}
	if egressID != "wan-fail" {
		t.Fatalf("failed migration cleared legacy association %q", egressID)
	}
	if err := repository.EnsureRoutingRulesMigrated(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingRuleCRUDUsesSeparateSubjectAndTargetPersistence(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-crud", Name: "WAN CRUD"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"target-a", "target-b"} {
		if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: id, Name: id, Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	rule, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "routing-crud", Name: "CRUD", EgressID: "wan-crud", TargetListIDs: []string{"target-b", "target-a", "target-a"}, Priority: 4, Enabled: true,
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Members: []policyv2.SubjectMember{{TerminalID: "terminal-a", Binding: "fixed", PinnedIPv4: []string{"192.0.2.10"}}}, Prefixes: []string{"198.51.100.129/24"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rule.Revision != 1 || !reflect.DeepEqual(rule.TargetListIDs, []string{"target-a", "target-b"}) || !reflect.DeepEqual(rule.Subject.Prefixes, []string{"198.51.100.0/24"}) {
		t.Fatalf("saved routing rule was not normalized: %#v", rule)
	}
	loaded, err := repository.GetRoutingRule(ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Subject, rule.Subject) || !reflect.DeepEqual(loaded.TargetListIDs, rule.TargetListIDs) {
		t.Fatalf("routing rule round trip changed subject/targets: loaded=%#v saved=%#v", loaded, rule)
	}
	if _, err := repository.SaveSource(ctx, policyv2.Source{ID: "legacy-write", EgressID: "wan-crud", Type: "manual", Kind: policyv2.KindIP, Name: "legacy"}); !errors.Is(err, policyv2.ErrRoutingRuleRequired) {
		t.Fatalf("legacy routing write error = %v, want ErrRoutingRuleRequired", err)
	}
	if err := repository.DeleteEgress(ctx, "wan-crud", rule.Revision); !errors.Is(err, policyv2.ErrEgressInUse) {
		t.Fatalf("referenced egress delete error = %v, want ErrEgressInUse", err)
	}
	if err := repository.DeleteTargetList(ctx, "target-a", 1); !errors.Is(err, policyv2.ErrTargetListInUse) {
		t.Fatalf("referenced target delete error = %v, want ErrTargetListInUse", err)
	}
	rule.Name = "CRUD updated"
	updated, err := repository.SaveRoutingRule(ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 {
		t.Fatalf("updated routing rule revision = %d, want 2", updated.Revision)
	}
	if _, err := repository.SaveRoutingRule(ctx, rule); !errors.Is(err, policyv2.ErrRevisionStale) {
		t.Fatalf("stale routing rule save error = %v, want ErrRevisionStale", err)
	}
	if err := repository.DeleteRoutingRule(ctx, updated.ID, updated.Revision); err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteTargetList(ctx, "target-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteEgress(ctx, "wan-crud", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetRoutingRule(ctx, updated.ID); !errors.Is(err, policyv2.ErrRoutingRuleNotFound) {
		t.Fatalf("deleted routing rule error = %v", err)
	}
}

func TestExcludedRoutingRuleRequiresTrafficIngress(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-excluded", Name: "WAN excluded"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "target-excluded", Name: "Target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	rule := policyv2.RoutingRule{ID: "rule-excluded", Name: "Excluded", EgressID: "wan-excluded", TargetListIDs: []string{"target-excluded"}, Enabled: true, Subject: policyv2.Subject{Mode: policyv2.SubjectModeExcluded, Prefixes: []string{"192.0.2.0/24"}}}
	if _, err := repository.SaveRoutingRule(ctx, rule); !errors.Is(err, policyv2.ErrRoutingExcludedRequiresIngress) {
		t.Fatalf("excluded rule without ingress error=%v, want ErrRoutingExcludedRequiresIngress", err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, rule); err != nil {
		t.Fatalf("excluded rule with ingress was rejected: %v", err)
	}
}
