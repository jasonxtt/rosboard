package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
	"rosboard/internal/subject"
)

func TestPolicyV2DomainApplyStateIsolated(t *testing.T) {
	t.Run("access apply leaves routing applied state unchanged", func(t *testing.T) {
		storage, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer storage.Close()
		ctx := context.Background()
		policyRepository := storage.PolicyRepository()
		accessRepository := storage.AccessRepository()
		if _, err := policyRepository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
			t.Fatal(err)
		}
		policyBefore, err := policyRepository.GetDeviceState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := policyRepository.CommitRoutingApply(ctx, policyBefore.DesiredRevision, "routing-hash", policyv2.ApplyJob{ID: "routing-job", PlanID: "routing-plan"}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := policyRepository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["VLAN20"],"interfaces":[]}`)); err != nil {
			t.Fatal(err)
		}
		policyBefore, err = policyRepository.GetDeviceState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{
			ID: "access-state-rule", Name: "Access state", Subject: subject.Subject{Mode: subject.ModeSelected}, TargetScope: accesscontrol.TargetScopeInternet, Enabled: true,
		}, []accesscontrol.RuleMember{{RuleID: "access-state-rule", TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
			t.Fatal(err)
		}
		accessState, err := accessRepository.GetState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := policyRepository.CommitAccessApply(ctx, accessState.DesiredRevision, "access-hash", policyv2.ApplyJob{ID: "access-job", PlanID: "access-plan"}, nil, nil); err != nil {
			t.Fatal(err)
		}
		policyAfter, err := policyRepository.GetDeviceState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if policyAfter.DesiredRevision != policyBefore.DesiredRevision || policyAfter.AppliedRevision != policyBefore.AppliedRevision || policyAfter.AppliedHash != policyBefore.AppliedHash || !policyAfter.AppliedAt.Equal(policyBefore.AppliedAt) {
			t.Fatalf("access apply changed routing applied state: before=%#v after=%#v", policyBefore, policyAfter)
		}
		accessAfter, err := accessRepository.GetState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if accessAfter.AppliedRevision != accessState.DesiredRevision || !accessAfter.Applied() {
			t.Fatalf("access apply did not update access applied state: before=%#v after=%#v", accessState, accessAfter)
		}
	})

	t.Run("routing apply leaves access applied state unchanged", func(t *testing.T) {
		storage, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer storage.Close()
		ctx := context.Background()
		policyRepository := storage.PolicyRepository()
		accessRepository := storage.AccessRepository()
		if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{
			ID: "routing-state-rule", Name: "Routing state", Subject: subject.Subject{Mode: subject.ModeSelected}, TargetScope: accesscontrol.TargetScopeInternet, Enabled: true,
		}, []accesscontrol.RuleMember{{RuleID: "routing-state-rule", TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
			t.Fatal(err)
		}
		accessState, err := accessRepository.GetState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := policyRepository.CommitAccessApply(ctx, accessState.DesiredRevision, "access-hash", policyv2.ApplyJob{ID: "access-job", PlanID: "access-plan"}, nil, nil); err != nil {
			t.Fatal(err)
		}
		accessBefore, err := accessRepository.GetState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := policyRepository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
			t.Fatal(err)
		}
		policyState, err := policyRepository.GetDeviceState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := policyRepository.CommitRoutingApply(ctx, policyState.DesiredRevision, "routing-hash", policyv2.ApplyJob{ID: "routing-job", PlanID: "routing-plan"}, nil); err != nil {
			t.Fatal(err)
		}
		accessAfter, err := accessRepository.GetState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if accessAfter.DesiredRevision != accessBefore.DesiredRevision || accessAfter.AppliedRevision != accessBefore.AppliedRevision || !accessAfter.AppliedAt.Equal(accessBefore.AppliedAt) {
			t.Fatalf("routing apply changed access applied state: before=%#v after=%#v", accessBefore, accessAfter)
		}
	})
}

func TestPolicyV2AtomicProposalInvalidatesSharedTargetConsumers(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "shared-proposal-target", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"})
	if _, err := policyRepository.SaveEgress(ctx, policyv2.Egress{
		ID: "shared-proposal-egress", Name: "Shared proposal egress", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan1", Gateway: "192.0.2.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := policyRepository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "shared-proposal-routing-rule", Name: "Shared proposal routing", Subject: subject.Subject{Mode: subject.ModeSelected, Prefixes: []string{"10.0.0.20/32"}},
		TargetListIDs: []string{target.ID}, EgressID: "shared-proposal-egress", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := accessRepository.SaveRule(ctx, canonicalRule("shared-proposal-access-rule", target.ID), []accesscontrol.RuleMember{canonicalRuleMember("shared-proposal-access-rule")}, "test"); err != nil {
		t.Fatal(err)
	}
	policyBefore, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accessBefore, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}

	accessVersionID := target.ID + ":v2"
	accessRule := canonicalRule("shared-proposal-access-rule", target.ID)
	accessRule.Revision = 1
	accessProposal := policyv2.AccessProposal{
		Rule:    accessRule,
		Members: []accesscontrol.RuleMember{canonicalRuleMember("shared-proposal-access-rule")},
		TargetLists: []policyv2.ProposedTargetList{{
			Target:  target,
			Version: policyv2.TargetListVersion{ID: accessVersionID, TargetListID: target.ID, SHA256: accessVersionID, State: "pending", CompressedYAML: []byte(accessVersionID)},
			Rules:   []policyv2.TargetListRule{{VersionID: accessVersionID, RuleType: "DOMAIN-SUFFIX", Domain: "changed.example"}},
		}},
		TargetListRevisions: map[string]int64{target.ID: target.Revision},
	}
	committedAccessRevision, err := policyRepository.CommitAccessProposal(ctx, accessProposal, accessBefore.DesiredRevision, "test")
	if err != nil {
		t.Fatal(err)
	}
	if committedAccessRevision != accessBefore.DesiredRevision+1 {
		t.Fatalf("access proposal did not advance only its own revision: before=%d after=%d", accessBefore.DesiredRevision, committedAccessRevision)
	}
	policyAfterAccess, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if policyAfterAccess.DesiredRevision != policyBefore.DesiredRevision+1 {
		t.Fatalf("access proposal did not invalidate shared routing target: before=%d after=%d", policyBefore.DesiredRevision, policyAfterAccess.DesiredRevision)
	}

	target, err = policyRepository.GetTargetList(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	routingVersionID := target.ID + ":v3"
	routingProposal := policyv2.PolicyProposal{
		TargetLists: []policyv2.ProposedTargetList{{
			Target:  target,
			Version: policyv2.TargetListVersion{ID: routingVersionID, TargetListID: target.ID, SHA256: routingVersionID, State: "pending", CompressedYAML: []byte(routingVersionID)},
			Rules:   []policyv2.TargetListRule{{VersionID: routingVersionID, RuleType: "DOMAIN-SUFFIX", Domain: "routing.example"}},
		}},
		TargetListRevisions: map[string]int64{target.ID: target.Revision},
	}
	committedRoutingRevision, err := policyRepository.CommitPolicyProposal(ctx, routingProposal, policyAfterAccess.DesiredRevision)
	if err != nil {
		t.Fatal(err)
	}
	if committedRoutingRevision != policyAfterAccess.DesiredRevision+1 {
		t.Fatalf("routing proposal did not advance only its own revision: before=%d after=%d", policyAfterAccess.DesiredRevision, committedRoutingRevision)
	}
	accessAfterRouting, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if accessAfterRouting.DesiredRevision != committedAccessRevision+1 {
		t.Fatalf("routing proposal did not invalidate shared access target: before=%d after=%d", committedAccessRevision, accessAfterRouting.DesiredRevision)
	}
}

func TestPolicyV2AccessPresetProposalCommitsAtomically(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	targetID := "preset:youtube:domain"
	versionID := targetID + ":v1"
	proposal := policyv2.AccessProposal{
		Rule: accesscontrol.AccessRule{
			ID: "access-preset-rule", Name: "YouTube", Subject: subject.Subject{Mode: subject.ModeSelected}, TargetScope: accesscontrol.TargetScopeTargets, TargetListIDs: []string{targetID}, Enabled: true,
		},
		Members: []accesscontrol.RuleMember{{RuleID: "access-preset-rule", TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}},
		TargetLists: []policyv2.ProposedTargetList{{
			Target:  policyv2.TargetList{ID: targetID, Name: "YouTube · Domain", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypePreset, PresetID: "youtube", Enabled: true},
			Version: policyv2.TargetListVersion{ID: versionID, TargetListID: targetID, SHA256: "youtube-v1", State: "pending", CompressedYAML: []byte("youtube-v1")},
			Rules:   []policyv2.TargetListRule{{VersionID: versionID, RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"}},
		}},
	}
	if _, err := policyRepository.GetTargetList(ctx, targetID); !errors.Is(err, policyv2.ErrTargetListNotFound) {
		t.Fatalf("preset target already exists before proposal apply: %v", err)
	}
	if rules, err := accessRepository.ListRules(ctx); err != nil {
		t.Fatal(err)
	} else if len(rules) != 0 {
		t.Fatalf("access rule was materialized before proposal apply: %#v", rules)
	}
	policyBefore, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.GeneratePlanWithOptions(ctx, "default", "access-rule-save", policyv2.PlanOptions{AccessProposal: &proposal})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Blockers) != 0 || plan.Plan.Domain != policyv2.PolicyDomainAccess || len(plan.Plan.TargetPromotions) != 1 || plan.Plan.TargetPromotions[0].TargetListID != targetID || plan.Plan.TargetPromotions[0].VersionID != versionID {
		t.Fatalf("access preset proposal plan is not isolated and ready: %#v", plan.Plan)
	}
	if _, err := policyRepository.GetTargetList(ctx, targetID); !errors.Is(err, policyv2.ErrTargetListNotFound) {
		t.Fatalf("preview materialized preset target: %v", err)
	}
	job, err := manager.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("access preset proposal apply failed: %#v", job)
	}
	target, err := policyRepository.GetTargetList(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.ActiveVersionID != versionID || target.PendingVersionID != "" {
		t.Fatalf("preset target version was not atomically committed and promoted: %#v", target)
	}
	rules, err := accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != proposal.Rule.ID {
		t.Fatalf("access rule was not atomically committed: %#v", rules)
	}
	policyAfter, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if policyAfter.DesiredRevision != policyBefore.DesiredRevision || policyAfter.AppliedRevision != policyBefore.AppliedRevision || policyAfter.AppliedHash != policyBefore.AppliedHash {
		t.Fatalf("access preset proposal changed routing state: before=%#v after=%#v", policyBefore, policyAfter)
	}
	accessState, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !accessState.Applied() {
		t.Fatalf("access preset proposal did not commit access state: %#v", accessState)
	}
}

func TestPolicyV2AccessProposalCanEditRuleAndAddPresetTarget(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	existing := seedCanonicalTarget(t, policyRepository, "access-edit-domain", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "existing.example"})
	if _, err := accessRepository.SaveRule(ctx, canonicalRule("access-edit-rule", existing.ID), []accesscontrol.RuleMember{canonicalRuleMember("access-edit-rule")}, "test"); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	initialPlan, err := manager.GeneratePlan(ctx, "default", "access-rule-save")
	if err != nil {
		t.Fatal(err)
	}
	initialJob, err := manager.ApplyPlan(ctx, "default", initialPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if initialJob = waitPolicyV2Job(t, policyRepository, initialJob.ID); initialJob.State != "committed" {
		t.Fatalf("initial access proposal apply failed: %#v", initialJob)
	}

	rules, err := accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("unexpected access rules after initial apply: %#v", rules)
	}
	rule := rules[0]
	presetID := "preset:fixture:domain"
	presetVersionID := presetID + ":v1"
	proposal := policyv2.AccessProposal{
		Rule: accesscontrol.AccessRule{
			ID: rule.ID, Name: rule.Name, Subject: rule.Subject, TargetScope: accesscontrol.TargetScopeTargets,
			TargetListIDs: []string{existing.ID, presetID}, Enabled: true, Revision: rule.Revision,
		},
		Members: []accesscontrol.RuleMember{canonicalRuleMember(rule.ID)},
		TargetLists: []policyv2.ProposedTargetList{{
			Target:  policyv2.TargetList{ID: presetID, Name: "Fixture · Domain", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypePreset, PresetID: "fixture", Enabled: true},
			Version: policyv2.TargetListVersion{ID: presetVersionID, TargetListID: presetID, SHA256: presetVersionID, State: "pending", CompressedYAML: []byte(presetVersionID)},
			Rules:   []policyv2.TargetListRule{{VersionID: presetVersionID, RuleType: "DOMAIN-SUFFIX", Domain: "preset.example"}},
		}},
	}
	plan, err := manager.GeneratePlanWithOptions(ctx, "default", "access-rule-save", policyv2.PlanOptions{AccessProposal: &proposal})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Blockers) != 0 {
		t.Fatalf("edited access rule with a new preset target was blocked: %#v", plan.Plan.Blockers)
	}
	job, err := manager.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatalf("edited access rule with a new preset target returned an apply error: %v", err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("edited access rule with a new preset target failed: %#v", job)
	}
	rules, err = accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("unexpected access rules after edit: %#v", rules)
	}
	finalRule := rules[0]
	if len(finalRule.TargetListIDs) != 2 || finalRule.TargetListIDs[1] != presetID {
		t.Fatalf("edited access rule did not retain the preset target: %#v", finalRule)
	}
}

func TestPolicyV2AccessProposalReusesExistingPresetTargetList(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	targetID := "preset:existing:domain"
	versionID := targetID + ":v1"
	if _, err := policyRepository.SaveTargetList(ctx, policyv2.TargetList{
		ID: targetID, Name: "Existing preset", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypePreset, PresetID: "existing", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{
		ID: versionID, TargetListID: targetID, SHA256: "existing-v1", State: "pending", CompressedYAML: []byte("existing-v1"),
	}, []policyv2.TargetListRule{{VersionID: versionID, RuleType: "DOMAIN-SUFFIX", Domain: "example.com"}}); err != nil {
		t.Fatal(err)
	}
	policyState, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.CommitRoutingApply(ctx, policyState.DesiredRevision, "seed-existing-preset", policyv2.ApplyJob{ID: "seed-job", PlanID: "seed-plan"}, []policyv2.TargetVersionPromotion{{TargetListID: targetID, VersionID: versionID}}); err != nil {
		t.Fatal(err)
	}
	before, err := policyRepository.GetTargetList(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	proposal := policyv2.AccessProposal{
		Rule:    policyv2AccessRule("access-existing-preset", targetID),
		Members: []accesscontrol.RuleMember{{RuleID: "access-existing-preset", TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}},
	}
	plan, err := manager.GeneratePlanWithOptions(ctx, "default", "access-rule-save", policyv2.PlanOptions{AccessProposal: &proposal})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Plan.Domain != policyv2.PolicyDomainAccess || len(plan.Plan.TargetPromotions) != 0 {
		t.Fatalf("existing preset was not reused directly: %#v", plan.Plan)
	}
	if len(plan.Plan.Blockers) != 0 {
		t.Fatalf("existing preset proposal was blocked: %#v", plan.Plan.Blockers)
	}
	job, err := manager.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("access proposal using existing preset failed: %#v", job)
	}
	after, err := policyRepository.GetTargetList(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.ActiveVersionID != before.ActiveVersionID || after.PendingVersionID != before.PendingVersionID {
		t.Fatalf("existing preset changed during direct reuse: before=%#v after=%#v", before, after)
	}
}

func policyv2AccessRule(id, targetID string) accesscontrol.AccessRule {
	return accesscontrol.AccessRule{
		ID: id, Name: "Existing preset", Subject: subject.Subject{Mode: subject.ModeSelected}, TargetScope: accesscontrol.TargetScopeTargets, TargetListIDs: []string{targetID}, Enabled: true,
	}
}

func TestPolicyV2PlansIgnoreOtherDomainDrift(t *testing.T) {
	t.Run("access ignores routing drift", func(t *testing.T) {
		storage, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer storage.Close()
		ctx := context.Background()
		policyRepository := storage.PolicyRepository()
		accessRepository := storage.AccessRepository()
		target := seedCanonicalTarget(t, policyRepository, "shared-routing-access-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
		if _, err := policyRepository.SaveEgress(ctx, policyv2.Egress{ID: "routing-drift-egress", Name: "Routing drift", Enabled: true, Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan1", Gateway: "192.0.2.1"}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := policyRepository.SaveRoutingRule(ctx, policyv2.RoutingRule{ID: "routing-drift-rule", Name: "Routing drift", Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"10.0.0.20/32"}}, TargetListIDs: []string{target.ID}, EgressID: "routing-drift-egress", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		router := newPolicyV2FakeRouter()
		manager := policyv2.NewManager(nil)
		if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
			t.Fatal(err)
		}
		routingPlan, err := manager.GeneratePlan(ctx, "default", "routing-policy")
		if err != nil {
			t.Fatal(err)
		}
		if len(routingPlan.Plan.Blockers) != 0 {
			t.Fatalf("routing plan blocked before drift: %#v", routingPlan.Plan.Blockers)
		}
		job, err := manager.ApplyPlan(ctx, "default", routingPlan.PlanID)
		if err != nil {
			t.Fatal(err)
		}
		if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
			t.Fatalf("routing setup apply failed: %#v", job)
		}
		router.mu.Lock()
		routingDriftID := ""
		for id, object := range router.objects[routeros.MenuIPFirewallMangle] {
			routingDriftID = id
			object["action"] = "accept"
			break
		}
		router.mu.Unlock()
		if _, err := accessRepository.SaveRule(ctx, canonicalRule("access-after-routing-drift", target.ID), []accesscontrol.RuleMember{canonicalRuleMember("access-after-routing-drift")}, "test"); err != nil {
			t.Fatal(err)
		}
		policyBefore, err := policyRepository.GetDeviceState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		accessPlan, err := manager.GeneratePlan(ctx, "default", "access-rule-save")
		if err != nil {
			t.Fatal(err)
		}
		if len(accessPlan.Plan.Blockers) != 0 {
			t.Fatalf("access plan was blocked by routing drift: %#v", accessPlan.Plan.Blockers)
		}
		assertPlanDoesNotTouchRouting(t, accessPlan.Plan.Operations)
		job, err = manager.ApplyPlan(ctx, "default", accessPlan.PlanID)
		if err != nil {
			t.Fatal(err)
		}
		if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
			t.Fatalf("access apply after routing drift failed: %#v", job)
		}
		policyAfter, err := policyRepository.GetDeviceState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if policyAfter.DesiredRevision != policyBefore.DesiredRevision || policyAfter.AppliedRevision != policyBefore.AppliedRevision || policyAfter.AppliedHash != policyBefore.AppliedHash {
			t.Fatalf("access apply changed routing state after drift: before=%#v after=%#v", policyBefore, policyAfter)
		}
		router.mu.Lock()
		routingDriftObject := router.objects[routeros.MenuIPFirewallMangle][routingDriftID]
		router.mu.Unlock()
		if routingDriftObject == nil || routingDriftObject["action"] != "accept" {
			t.Fatalf("access apply changed or removed the routing mangle domain: %#v", routingDriftObject)
		}
	})

	t.Run("routing ignores access drift", func(t *testing.T) {
		storage, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer storage.Close()
		ctx := context.Background()
		policyRepository := storage.PolicyRepository()
		accessRepository := storage.AccessRepository()
		accessTarget := seedCanonicalTarget(t, policyRepository, "access-drift-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "198.51.100.0/24"})
		if _, err := accessRepository.SaveRule(ctx, canonicalRule("access-drift-rule", accessTarget.ID), []accesscontrol.RuleMember{canonicalRuleMember("access-drift-rule")}, "test"); err != nil {
			t.Fatal(err)
		}
		router := newPolicyV2FakeRouter()
		manager := policyv2.NewManager(nil)
		if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
			t.Fatal(err)
		}
		accessPlan, err := manager.GeneratePlan(ctx, "default", "access-rule-save")
		if err != nil {
			t.Fatal(err)
		}
		if len(accessPlan.Plan.Blockers) != 0 {
			t.Fatalf("access plan blocked before drift: %#v", accessPlan.Plan.Blockers)
		}
		job, err := manager.ApplyPlan(ctx, "default", accessPlan.PlanID)
		if err != nil {
			t.Fatal(err)
		}
		if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
			t.Fatalf("access setup apply failed: %#v", job)
		}
		router.mu.Lock()
		accessDriftID := ""
		for id, object := range router.objects[routeros.MenuIPFirewallFilter] {
			accessDriftID = id
			object["action"] = "accept"
			break
		}
		router.mu.Unlock()
		routingTarget := seedCanonicalTarget(t, policyRepository, "routing-after-access-drift", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
		if _, err := policyRepository.SaveEgress(ctx, policyv2.Egress{ID: "routing-after-access-egress", Name: "Routing after access", Enabled: true, Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan2", Gateway: "192.0.2.2"}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := policyRepository.SaveRoutingRule(ctx, policyv2.RoutingRule{ID: "routing-after-access-rule", Name: "Routing after access", Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"10.0.0.20/32"}}, TargetListIDs: []string{routingTarget.ID}, EgressID: "routing-after-access-egress", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		routingPlan, err := manager.GeneratePlan(ctx, "default", "routing-policy")
		if err != nil {
			t.Fatal(err)
		}
		if len(routingPlan.Plan.Blockers) != 0 {
			t.Fatalf("routing plan was blocked by access drift: %#v", routingPlan.Plan.Blockers)
		}
		assertPlanDoesNotTouchAccess(t, routingPlan.Plan.Operations)
		job, err = manager.ApplyPlan(ctx, "default", routingPlan.PlanID)
		if err != nil {
			t.Fatal(err)
		}
		if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
			t.Fatalf("routing apply after access drift failed: %#v", job)
		}
		router.mu.Lock()
		accessDriftObject := router.objects[routeros.MenuIPFirewallFilter][accessDriftID]
		router.mu.Unlock()
		if accessDriftObject == nil || accessDriftObject["action"] != "accept" {
			t.Fatalf("routing apply changed or removed the access filter domain: %#v", accessDriftObject)
		}
	})
}

func assertPlanDoesNotTouchRouting(t *testing.T, operations []policyv2.PlanOperation) {
	t.Helper()
	for _, operation := range operations {
		switch operation.Menu {
		case string(routeros.MenuRoutingTable), string(routeros.MenuIPRoute), string(routeros.MenuIPv6Route), string(routeros.MenuRoutingRule), string(routeros.MenuIPFirewallMangle), string(routeros.MenuIPv6FirewallMangle), string(routeros.MenuIPFirewallNAT), string(routeros.MenuIPv6FirewallNAT):
			t.Fatalf("access plan touched routing operation: %#v", operation)
		}
	}
}

func assertPlanDoesNotTouchAccess(t *testing.T, operations []policyv2.PlanOperation) {
	t.Helper()
	for _, operation := range operations {
		switch operation.Menu {
		case string(routeros.MenuIPFirewallFilter), string(routeros.MenuIPv6FirewallFilter):
			t.Fatalf("routing plan touched access operation: %#v", operation)
		case string(routeros.MenuIPFirewallAddressList), string(routeros.MenuIPv6FirewallAddressList):
			if strings.HasPrefix(operation.LogicalID, "access") || strings.HasPrefix(operation.After["list"], "rb_ac_") || strings.HasPrefix(operation.After["address-list"], "rb_ac_") {
				t.Fatalf("routing plan touched access address-list operation: %#v", operation)
			}
		case string(routeros.MenuIPDNSForwarders), string(routeros.MenuIPDNSStatic):
			if strings.HasPrefix(operation.LogicalID, "access") {
				t.Fatalf("routing plan touched access DNS operation: %#v", operation)
			}
		}
	}
}
