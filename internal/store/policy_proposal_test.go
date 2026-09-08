package store

import (
	"context"
	"errors"
	"testing"

	"rosboard/internal/policyv2"
)

func TestPolicyV2ProposalPreviewDoesNotMutateCanonicalUntilApproval(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	repository := storage.PolicyRepository()
	egress, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-old", Name: "WAN old", Priority: 10, ListMode: policyv2.ListModeShared,
		ListName: "wan_old", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether2", Gateway: "198.51.100.1", RouteMode: "strict", NATMode: "masquerade"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	target := seedCanonicalTarget(t, repository, "target-old", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	rule, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "rule-old", Name: "Old rule", EgressID: egress.ID, TargetListIDs: []string{target.ID},
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"192.0.2.10"}}, Priority: 10, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}

	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: repository}); err != nil {
		t.Fatal(err)
	}
	initial, err := manager.GenerateAndApply(ctx, "default", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if initial = waitPolicyV2Job(t, repository, initial.ID); initial.State != "committed" {
		t.Fatalf("initial apply failed: %#v", initial)
	}
	before, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldEgress, err := repository.GetEgress(ctx, egress.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldRule, err := repository.GetRoutingRule(ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}

	draftEgress := oldEgress
	draftEgress.Name = "WAN draft"
	draftRule := oldRule
	draftRule.Name = "Draft rule"
	proposal := policyv2.PolicyProposal{
		Egress: &draftEgress, TrafficIngress: &policyv2.TrafficIngressScope{}, RoutingRule: &draftRule,
	}
	preview, err := manager.GeneratePlanWithOptions(ctx, "default", "structural", policyv2.PlanOptions{Proposal: &proposal})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.DesiredRevision != before.DesiredRevision || unchanged.AppliedRevision != before.AppliedRevision {
		t.Fatalf("preview changed device revisions: before=%#v after=%#v", before, unchanged)
	}
	currentEgress, err := repository.GetEgress(ctx, egress.ID)
	if err != nil {
		t.Fatal(err)
	}
	currentRule, err := repository.GetRoutingRule(ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentEgress.Name != oldEgress.Name || currentRule.Name != oldRule.Name {
		t.Fatalf("preview leaked draft into canonical graph: egress=%#v rule=%#v", currentEgress, currentRule)
	}

	// A second normal GenerateAndApply represents a source refresh or another
	// entry point racing with the unapproved wizard. It can only see the old
	// canonical graph, never the in-memory proposal.
	refresh, err := manager.GenerateAndApply(ctx, "default", "source-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if refresh = waitPolicyV2Job(t, repository, refresh.ID); refresh.State != "committed" {
		t.Fatalf("unrelated apply failed: %#v", refresh)
	}
	currentEgress, err = repository.GetEgress(ctx, egress.ID)
	if err != nil {
		t.Fatal(err)
	}
	currentRule, err = repository.GetRoutingRule(ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err = repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if currentEgress.Name != oldEgress.Name || currentRule.Name != oldRule.Name || unchanged.DesiredRevision != before.DesiredRevision {
		t.Fatalf("unapproved proposal was consumed by another apply: egress=%#v rule=%#v state=%#v", currentEgress, currentRule, unchanged)
	}

	if _, err := manager.ApplyPlan(ctx, "default", preview.PlanID); !errors.Is(err, policyv2.ErrPlanStale) {
		t.Fatalf("apply without the reviewed plan hash error = %v, want ErrPlanStale", err)
	}
	if _, err := manager.ApplyPlanWithHash(ctx, "default", preview.PlanID, "wrong-plan-hash"); !errors.Is(err, policyv2.ErrPlanStale) {
		t.Fatalf("wrong reviewed plan hash error = %v, want ErrPlanStale", err)
	}
	approved, err := manager.ApplyPlanWithHash(ctx, "default", preview.PlanID, preview.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	if approved = waitPolicyV2Job(t, repository, approved.ID); approved.State != "committed" {
		t.Fatalf("approved proposal apply failed: %#v", approved)
	}
	finalEgress, err := repository.GetEgress(ctx, egress.ID)
	if err != nil {
		t.Fatal(err)
	}
	finalRule, err := repository.GetRoutingRule(ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	finalState, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if finalEgress.Name != draftEgress.Name || finalRule.Name != draftRule.Name || finalState.DesiredRevision != before.DesiredRevision+1 || !finalState.Applied() {
		t.Fatalf("approved proposal was not atomically committed and applied: egress=%#v rule=%#v state=%#v", finalEgress, finalRule, finalState)
	}
	scope, err := policyv2.ParseTrafficIngressScope(finalState.TrafficIngress)
	if err != nil || len(scope.InterfaceLists) != 1 || scope.InterfaceLists[0] != "LAN" {
		t.Fatalf("canonical routing edit rewrote the compatibility ingress default: scope=%#v err=%v", scope, err)
	}
}

func TestPolicyV2ProposalCanRestorePendingPresetTarget(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	repository := storage.PolicyRepository()
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "preset:fixture:domain", Name: "Fixture · 域名", Kind: policyv2.KindDomain,
		SourceType: policyv2.TargetSourceTypePreset, PresetID: "fixture", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`UPDATE policy_v2_sources SET pending_delete = 1, enabled = 0, revision = revision + 1 WHERE id = ?`, target.ID); err != nil {
		t.Fatal(err)
	}
	target, err = repository.GetTargetList(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	versionID := "preset:fixture:domain:v1"
	target.PendingDeletion = false
	target.Enabled = true
	state, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CommitPolicyProposal(ctx, policyv2.PolicyProposal{TargetLists: []policyv2.ProposedTargetList{{
		Target:  target,
		Version: policyv2.TargetListVersion{ID: versionID, TargetListID: target.ID, SHA256: "preset-sha", CompressedYAML: []byte("preset-content"), State: "pending"},
		Rules:   []policyv2.TargetListRule{{VersionID: versionID, RuleType: "DOMAIN", Domain: "example.com"}},
	}}}, state.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	restored, err := repository.GetTargetList(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.PendingDeletion || restored.PendingVersionID != versionID {
		t.Fatalf("preset target was not restored: %#v", restored)
	}
}
