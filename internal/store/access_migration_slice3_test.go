package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
)

func TestCanonicalAccessMigrationUnknownOpaqueApplicationFailsClosedWithoutTarget(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	accessRepository := storage.AccessRepository()
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{ID: "legacy-app", Name: "legacy", TargetScope: accesscontrol.TargetScopeApplications, ApplicationIDs: []string{"oaf:1001"}, Enabled: true}, []accesscontrol.RuleMember{{RuleID: "legacy-app", TerminalID: "terminal", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := accessRepository.EnsureCanonicalAccessMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	rules, err := accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Enabled || rules[0].TargetScope != accesscontrol.TargetScopeTargets || len(rules[0].TargetListIDs) != 0 || len(rules[0].MigrationIssues) != 1 || !strings.HasPrefix(rules[0].MigrationIssues[0], "legacy_application_unresolved:") {
		t.Fatalf("unknown opaque application was not fail-closed: %#v", rules)
	}
	if _, err := storage.PolicyRepository().GetTargetList(ctx, "preset:youtube:domain"); !errors.Is(err, policyv2.ErrTargetListNotFound) {
		t.Fatalf("unknown application created a fake target list: %v", err)
	}
	var issueCount int
	if err := storage.db.QueryRow(`SELECT count(*) FROM access_rule_migration_issues WHERE rule_id = ?`, "legacy-app").Scan(&issueCount); err != nil {
		t.Fatal(err)
	}
	if issueCount != 1 {
		t.Fatalf("migration issue count = %d, want 1", issueCount)
	}
	state, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := accessRepository.EnsureCanonicalAccessMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	stateAgain, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stateAgain.DesiredRevision != state.DesiredRevision {
		t.Fatalf("migration replay changed desired revision: before=%d after=%d", state.DesiredRevision, stateAgain.DesiredRevision)
	}
	if err := storage.db.QueryRow(`SELECT count(*) FROM access_rule_migration_issues WHERE rule_id = ?`, "legacy-app").Scan(&issueCount); err != nil {
		t.Fatal(err)
	}
	if issueCount != 1 {
		t.Fatalf("migration replay duplicated issues: %d", issueCount)
	}
	var targetCount int
	if err := storage.db.QueryRow(`SELECT count(*) FROM policy_v2_sources WHERE id = ?`, "preset:youtube:domain").Scan(&targetCount); err != nil {
		t.Fatal(err)
	}
	if targetCount != 0 {
		t.Fatalf("migration replay created a fake target list: %d", targetCount)
	}
}

func TestCanonicalAccessMigrationUnresolvedApplicationIsDisabled(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	accessRepository := storage.AccessRepository()
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{ID: "missing-app", Name: "missing", TargetScope: accesscontrol.TargetScopeApplications, ApplicationIDs: []string{"oaf:4040"}, Enabled: true}, []accesscontrol.RuleMember{{RuleID: "missing-app", TerminalID: "terminal", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := accessRepository.EnsureCanonicalAccessMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	rules, err := accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Enabled || rules[0].TargetScope != accesscontrol.TargetScopeTargets || len(rules[0].TargetListIDs) != 0 || len(rules[0].MigrationIssues) != 1 {
		t.Fatalf("unresolved application was not fail-closed: %#v", rules)
	}
}

func TestCanonicalAccessMigrationReinterpretsLegacySourcesWithoutChangingIDs(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	for _, source := range []struct {
		id   string
		kind string
	}{{"source-a", policyv2.KindDomain}, {"source-b", policyv2.KindIP}} {
		if _, err := policyRepository.SaveSource(ctx, policyv2.Source{ID: source.id, Type: policyv2.TargetSourceTypeManual, Kind: source.kind, Name: source.id, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	accessRepository := storage.AccessRepository()
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{
		ID: "legacy-sources", Name: "legacy sources", TargetScope: accesscontrol.TargetScopeSources,
		SourceIDs: []string{"source-b", "source-a"}, Enabled: true,
	}, []accesscontrol.RuleMember{{RuleID: "legacy-sources", TerminalID: "terminal", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := accessRepository.EnsureCanonicalAccessMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	rules, err := accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].TargetScope != accesscontrol.TargetScopeTargets || len(rules[0].TargetListIDs) != 2 || rules[0].TargetListIDs[0] != "source-a" || rules[0].TargetListIDs[1] != "source-b" {
		t.Fatalf("legacy source relation was not reinterpreted as canonical targets: %#v", rules)
	}
	var relationCount int
	if err := storage.db.QueryRow(`SELECT count(*) FROM access_rule_sources WHERE rule_id = ?`, "legacy-sources").Scan(&relationCount); err != nil {
		t.Fatal(err)
	}
	if relationCount != 2 {
		t.Fatalf("source-to-target relation rows were not preserved: %d", relationCount)
	}
}

func TestCanonicalAccessMigrationHandlesMixedLegacyRules(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	if _, err := policyRepository.SaveSource(ctx, policyv2.Source{ID: "source-a", Type: policyv2.TargetSourceTypeManual, Kind: policyv2.KindIP, Name: "source-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	accessRepository := storage.AccessRepository()
	fixtures := []accesscontrol.AccessRule{
		{ID: "source-rule", Name: "source", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: []string{"source-a"}, Enabled: true},
		{ID: "application-rule", Name: "application", TargetScope: accesscontrol.TargetScopeApplications, ApplicationIDs: []string{"oaf:1001"}, Enabled: true},
		{ID: "internet-rule", Name: "internet", TargetScope: accesscontrol.TargetScopeInternet, Enabled: true},
	}
	for _, rule := range fixtures {
		if _, err := accessRepository.SaveRule(ctx, rule, []accesscontrol.RuleMember{{RuleID: rule.ID, TerminalID: "terminal", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := accessRepository.EnsureCanonicalAccessMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	rules, err := accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]accesscontrol.AccessRule, len(rules))
	for _, rule := range rules {
		byID[rule.ID] = rule
	}
	if byID["source-rule"].TargetScope != accesscontrol.TargetScopeTargets || len(byID["source-rule"].TargetListIDs) != 1 || byID["source-rule"].TargetListIDs[0] != "source-a" {
		t.Fatalf("legacy source rule was not migrated: %#v", byID["source-rule"])
	}
	if byID["application-rule"].Enabled || byID["application-rule"].TargetScope != accesscontrol.TargetScopeTargets || len(byID["application-rule"].TargetListIDs) != 0 || len(byID["application-rule"].MigrationIssues) != 1 || !strings.HasPrefix(byID["application-rule"].MigrationIssues[0], "legacy_application_unresolved:") {
		t.Fatalf("legacy application rule was not fail-closed: %#v", byID["application-rule"])
	}
	if byID["internet-rule"].TargetScope != accesscontrol.TargetScopeInternet || len(byID["internet-rule"].TargetListIDs) != 0 {
		t.Fatalf("internet rule changed during migration: %#v", byID["internet-rule"])
	}
}
