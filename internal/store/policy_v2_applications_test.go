package store

import (
	"context"
	"strings"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/ownership"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
	"rosboard/internal/subject"
)

func seedCanonicalTarget(t *testing.T, repository *PolicyRepository, id, kind string, rules ...policyv2.TargetListRule) policyv2.TargetList {
	t.Helper()
	target, err := repository.SaveTargetList(context.Background(), policyv2.TargetList{
		ID: id, Name: id, Kind: kind, SourceType: policyv2.TargetSourceTypeManual, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	versionID := id + ":v1"
	for i := range rules {
		rules[i].VersionID = versionID
	}
	if err := repository.SavePendingTargetListVersion(context.Background(), policyv2.TargetListVersion{
		ID: versionID, TargetListID: id, SHA256: versionID, State: "pending", CompressedYAML: []byte(versionID),
	}, rules); err != nil {
		t.Fatal(err)
	}
	target, err = repository.GetTargetList(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func canonicalRule(id string, targets ...string) accesscontrol.AccessRule {
	return accesscontrol.AccessRule{
		ID: id, Name: "目标规则 " + id, Subject: subjectForTest(), TargetScope: accesscontrol.TargetScopeTargets,
		TargetListIDs: targets, Enabled: true,
	}
}

func subjectForTest() (result subject.Subject) {
	return subject.Subject{Mode: subject.ModeSelected}
}

func canonicalRuleMember(ruleID string) accesscontrol.RuleMember {
	return accesscontrol.RuleMember{RuleID: ruleID, TerminalID: "terminal", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}
}

func TestPolicyV2DesiredMaterializesCanonicalTargetListsForAccessOnly(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	seedCanonicalTarget(t, policyRepository, "video-domains", policyv2.KindDomain,
		policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "video.example"},
		policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "cdn.example"})
	seedCanonicalTarget(t, policyRepository, "video-addresses", policyv2.KindIP,
		policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	if _, err := accessRepository.SaveRule(ctx, canonicalRule("rule-a", "video-domains", "video-addresses"), []accesscontrol.RuleMember{canonicalRuleMember("rule-a")}, "test"); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	desired, err := policyv2.BuildDesiredWithAccessOptions(ctx, policyRepository, router, accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("canonical target desired is blocked: %#v", desired.Blockers)
	}
	if len(desired.AccessTargetIDs) != 2 {
		t.Fatalf("canonical target desired did not retain access targets: %#v", desired.AccessTargetIDs)
	}
	if len(desiredObjectsByMenu(desired.Objects, routeros.MenuIPDNSForwarders)) != 1 {
		t.Fatalf("domain target must reuse one access DNS forwarder: %#v", desired.Objects)
	}
	for _, object := range desired.Objects {
		if strings.HasPrefix(object.LogicalID, "dns:application:") || strings.HasPrefix(object.LogicalID, "rb_app_") {
			t.Fatalf("canonical access desired emitted OAF projection: %#v", object)
		}
		if strings.HasPrefix(object.LogicalID, "access-target") {
			list := object.Fields["list"]
			if list == "" {
				list = object.Fields["address-list"]
			}
			if !ownership.IsNamespace(list) {
				t.Fatalf("target projection must use an access-consumer list: %#v", object)
			}
		}
	}
	if len(desiredObjectsByLogicalPrefix(desired.Objects, "access-target-dns:")) != 2 || len(desiredObjectsByLogicalPrefix(desired.Objects, "access-target:")) != 1 {
		t.Fatalf("canonical target contents were not materialized: %#v", desired.Objects)
	}
	for _, object := range desired.Objects {
		if strings.HasPrefix(object.LogicalID, "access:") && strings.Contains(object.LogicalID, "jump-") && strings.Contains(object.LogicalID, ":target:") {
			if object.Fields["dst-address-list"] == "" && object.Fields["src-address-list"] == "" {
				t.Fatalf("target jump has no target address list: %#v", object)
			}
		}
	}
}

func TestPolicyV2DomainBuildersDoNotCrossEmitManagedObjects(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	routingTarget := seedCanonicalTarget(t, policyRepository, "routing-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	accessTarget := seedCanonicalTarget(t, policyRepository, "access-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "198.51.100.0/24"})
	if _, err := policyRepository.SaveEgress(ctx, policyv2.Egress{
		ID: "routing-egress", Name: "Routing egress", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "192.0.2.1", RouteTable: "routing-table"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := policyRepository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "routing-rule", Name: "Routing rule", Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"10.0.0.20/32"}},
		TargetListIDs: []string{routingTarget.ID}, EgressID: "routing-egress", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := accessRepository.SaveRule(ctx, canonicalRule("access-rule", accessTarget.ID), []accesscontrol.RuleMember{canonicalRuleMember("access-rule")}, "test"); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	routingDesired, err := policyv2.BuildRoutingDesired(ctx, policyRepository, router, nil)
	if err != nil {
		t.Fatal(err)
	}
	accessDesired, err := policyv2.BuildAccessDesired(ctx, policyRepository, router, accessRepository, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(routingDesired.TargetPromotions) != 1 || routingDesired.TargetPromotions[0].TargetListID != routingTarget.ID || routingDesired.TargetPromotions[0].VersionID != routingTarget.PendingVersionID {
		t.Fatalf("routing desired promoted the wrong target versions: %#v", routingDesired.TargetPromotions)
	}
	if len(accessDesired.TargetPromotions) != 1 || accessDesired.TargetPromotions[0].TargetListID != accessTarget.ID || accessDesired.TargetPromotions[0].VersionID != accessTarget.PendingVersionID {
		t.Fatalf("access desired promoted the wrong target versions: %#v", accessDesired.TargetPromotions)
	}
	if err := policyRepository.CommitAccessApply(ctx, accessDesired.AccessRevision, "access-domain-hash", policyv2.ApplyJob{ID: "access-domain-job", PlanID: "access-domain-plan"}, nil, accessDesired.TargetPromotions); err != nil {
		t.Fatal(err)
	}
	afterAccessRoutingTarget, err := policyRepository.GetTargetList(ctx, routingTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterAccessTarget, err := policyRepository.GetTargetList(ctx, accessTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterAccessRoutingTarget.ActiveVersionID != "" || afterAccessRoutingTarget.PendingVersionID != routingTarget.PendingVersionID || afterAccessTarget.ActiveVersionID != accessTarget.PendingVersionID || afterAccessTarget.PendingVersionID != "" {
		t.Fatalf("access apply promoted a routing target or missed its access target: routing=%#v access=%#v", afterAccessRoutingTarget, afterAccessTarget)
	}
	if err := policyRepository.CommitRoutingApply(ctx, routingDesired.Revision, "routing-domain-hash", policyv2.ApplyJob{ID: "routing-domain-job", PlanID: "routing-domain-plan"}, routingDesired.TargetPromotions); err != nil {
		t.Fatal(err)
	}
	afterRoutingTarget, err := policyRepository.GetTargetList(ctx, routingTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterRoutingAccessTarget, err := policyRepository.GetTargetList(ctx, accessTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRoutingTarget.ActiveVersionID != routingTarget.PendingVersionID || afterRoutingTarget.PendingVersionID != "" || afterRoutingAccessTarget.ActiveVersionID != accessTarget.PendingVersionID || afterRoutingAccessTarget.PendingVersionID != "" {
		t.Fatalf("routing apply did not isolate promotion to the routing target: routing=%#v access=%#v", afterRoutingTarget, afterRoutingAccessTarget)
	}
	for _, object := range routingDesired.Objects {
		if object.Domain != policyv2.PolicyDomainRouting || strings.HasPrefix(object.LogicalID, "access") || strings.HasPrefix(object.Fields["list"], "rb_ac_") || strings.HasPrefix(object.Fields["address-list"], "rb_ac_") {
			t.Fatalf("routing desired crossed into access ownership: %#v", object)
		}
	}
	for _, object := range accessDesired.Objects {
		if object.Domain != policyv2.PolicyDomainAccess {
			t.Fatalf("access desired has the wrong domain: %#v", object)
		}
		switch object.Menu {
		case string(routeros.MenuRoutingTable), string(routeros.MenuIPRoute), string(routeros.MenuIPv6Route), string(routeros.MenuRoutingRule), string(routeros.MenuIPFirewallMangle), string(routeros.MenuIPv6FirewallMangle), string(routeros.MenuIPFirewallNAT), string(routeros.MenuIPv6FirewallNAT):
			t.Fatalf("access desired emitted routing infrastructure: %#v", object)
		}
		if strings.HasPrefix(object.Fields["list"], "rb_rt_") || strings.HasPrefix(object.Fields["address-list"], "rb_rt_") {
			t.Fatalf("access desired used a routing-owned target list: %#v", object)
		}
	}
}

func TestPolicyV2CanonicalTargetProjectionDoesNotGuessMissingContent(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	seedCanonicalTarget(t, policyRepository, "empty-target", policyv2.KindDomain)
	if _, err := accessRepository.SaveRule(ctx, canonicalRule("blocked-rule", "empty-target"), []accesscontrol.RuleMember{canonicalRuleMember("blocked-rule")}, "test"); err != nil {
		t.Fatal(err)
	}
	desired, err := policyv2.BuildDesiredWithAccessOptions(ctx, policyRepository, newPolicyV2FakeRouter(), accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, blocker := range desired.Blockers {
		if blocker.Code == "access_target_unavailable" && blocker.LogicalID == "blocked-rule" {
			found = true
		}
	}
	if !found {
		t.Fatalf("target without supported materialized rules must fail closed: %#v", desired.Blockers)
	}
	for _, object := range desired.Objects {
		if strings.Contains(object.LogicalID, "blocked-rule") {
			t.Fatalf("missing target content emitted a partial access graph: %#v", object)
		}
	}
}

func TestPolicyV2CanonicalTargetRemovalCleansManagedProjectionAndPreservesForeign(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	seedCanonicalTarget(t, policyRepository, "one-target", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "one.example"})
	seedCanonicalTarget(t, policyRepository, "two-target", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "two.example"})
	rule := canonicalRule("remove-target", "one-target", "two-target")
	savedRule, err := accessRepository.SaveRule(ctx, rule, []accesscontrol.RuleMember{canonicalRuleMember(rule.ID)}, "test")
	if err != nil {
		t.Fatal(err)
	}
	rule = savedRule
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	initial, err := manager.GeneratePlan(ctx, "default", "canonical-target-initial")
	if err != nil || len(initial.Plan.Blockers) != 0 {
		t.Fatalf("initial canonical target plan blocked: plan=%#v err=%v", initial.Plan, err)
	}
	job, err := manager.ApplyPlan(ctx, "default", initial.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("initial canonical target apply failed: %#v", job)
	}

	foreign := routeros.RouterOSObject{".id": "*foreign", "name": "foreign.example", "type": "FWD", "forward-to": "foreign", "address-list": "rb_app_foreign", "match-subdomain": "yes"}
	managerID, err := policyRepository.ManagerInstanceID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	legacy := routeros.RouterOSObject{".id": "*legacy", "name": "legacy.example", "type": "FWD", "forward-to": "legacy", "address-list": "rb_app_old", "match-subdomain": "yes", "comment": accesscontrol.ManagedComment(managerID, policyRepository.DeviceID(), "dns:application:oaf:1001:DOMAIN-SUFFIX:legacy.example", "legacy OAF")}
	router.mu.Lock()
	router.objects[routeros.MenuIPDNSStatic][foreign.ID()] = foreign
	router.order[routeros.MenuIPDNSStatic] = append(router.order[routeros.MenuIPDNSStatic], foreign.ID())
	router.objects[routeros.MenuIPDNSStatic][legacy.ID()] = legacy
	router.order[routeros.MenuIPDNSStatic] = append(router.order[routeros.MenuIPDNSStatic], legacy.ID())
	router.mu.Unlock()
	rule.TargetListIDs = []string{"one-target"}
	if _, err := accessRepository.SaveRule(ctx, rule, []accesscontrol.RuleMember{canonicalRuleMember(rule.ID)}, "test"); err != nil {
		t.Fatal(err)
	}
	remove, err := manager.GeneratePlan(ctx, "default", "canonical-target-remove")
	if err != nil || len(remove.Plan.Blockers) != 0 {
		t.Fatalf("target removal plan blocked: plan=%#v err=%v", remove.Plan, err)
	}
	deleted, legacyDeleted := false, false
	for _, operation := range remove.Plan.Operations {
		if operation.Action == "delete" && operation.Before["name"] == "two.example" {
			deleted = true
		}
		if operation.Action == "delete" && operation.Before["name"] == "legacy.example" {
			legacyDeleted = true
		}
		if strings.Contains(operation.LogicalID, "two-target") && operation.Action != "delete" {
			t.Fatalf("removed target projection must only be deleted: %#v", operation)
		}
	}
	if !deleted || !legacyDeleted {
		t.Fatalf("removed target and stale OAF projections were not scheduled for deletion: %#v", remove.Plan.Operations)
	}
	job, err = manager.ApplyPlan(ctx, "default", remove.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("target removal apply failed: %#v", job)
	}
	foreignObject := findPolicyV2ObjectByName(router, routeros.MenuIPDNSStatic, "foreign.example")
	if foreignObject == nil || foreignObject.ID() != foreign.ID() || foreignObject["address-list"] != "rb_app_foreign" {
		t.Fatalf("foreign object was modified during canonical cleanup: %#v", foreignObject)
	}
}
