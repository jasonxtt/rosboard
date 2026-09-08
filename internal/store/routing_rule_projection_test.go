package store

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"rosboard/internal/ownership"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

func TestRoutingRuleProjectionUsesSubjectAndConsumerScopedTargetLists(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-selected", Name: "WAN selected", Enabled: true, RouterOutput: true,
		Families: []policyv2.EgressFamily{
			{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1", RouteTable: "selected4"},
			{Family: policyv2.FamilyIPv6, Enabled: true, Gateway: "2001:db8:1::1", RouteTable: "selected6"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "target-selected", Name: "Selected target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: "target-selected-version", TargetListID: target.ID, SHA256: "selected", CompressedYAML: []byte("selected"), State: "pending"}, []policyv2.TargetListRule{
		{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"},
		{RuleType: "IP-CIDR6", Domain: "2001:db8:2::/48"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "rule-selected", Name: "Selected clients", EgressID: "wan-selected", TargetListIDs: []string{target.ID}, Enabled: true,
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"192.0.2.10", "2001:db8:10::1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "rule-all", Name: "All clients", EgressID: "wan-selected", TargetListIDs: []string{target.ID}, Enabled: true,
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "rule-excluded", Name: "Excluded clients", EgressID: "wan-selected", TargetListIDs: []string{target.ID}, Enabled: true,
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeExcluded, Prefixes: []string{"198.18.0.0/16", "2001:db8:20::/48"}},
	}); err != nil {
		t.Fatal(err)
	}
	desired, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("selected routing desired is blocked: %#v", desired.Blockers)
	}
	targetAddresses := desiredObjectsByLogicalPrefix(desired.Objects, "routing-target-addr:wan-selected:target-selected:")
	if len(targetAddresses) != 2 {
		t.Fatalf("target was not projected once per enabled family: %#v", targetAddresses)
	}
	targetListName := targetAddresses[0].Fields["list"]
	if targetListName == "" || targetListName != targetAddresses[1].Fields["list"] || len(targetListName) < len("rb_rt_") || targetListName[:len("rb_rt_")] != "rb_rt_" {
		t.Fatalf("target list is not one deterministic egress-target projection: %#v", targetAddresses)
	}

	subjectAddresses := desiredObjectsByLogicalPrefix(desired.Objects, "routing-subject:rule-selected:")
	if len(subjectAddresses) != 2 {
		t.Fatalf("selected subject was not projected per family: %#v", subjectAddresses)
	}
	for _, object := range subjectAddresses {
		if !strings.HasPrefix(object.Fields["list"], "rb_sub_") {
			t.Fatalf("unexpected selected subject list: %#v", object)
		}
	}
	selectedConnections := desiredObjectsByLogicalPrefix(desired.Objects, "routing-rule-connection:rule-selected:")
	allConnections := desiredObjectsByLogicalPrefix(desired.Objects, "routing-rule-connection:rule-all:")
	excludedConnections := desiredObjectsByLogicalPrefix(desired.Objects, "routing-rule-connection:rule-excluded:")
	if len(selectedConnections) != 2 || len(allConnections) != 2 || len(excludedConnections) != 2 {
		t.Fatalf("each rule/family did not receive a target matcher: selected=%#v all=%#v excluded=%#v", selectedConnections, allConnections, excludedConnections)
	}
	for _, object := range selectedConnections {
		if object.Fields["src-address-list"] == "" || object.Fields["in-interface-list"] != "" || object.Fields["dst-address-list"] != targetListName || object.Fields["chain"] != "prerouting" {
			t.Fatalf("selected matcher missed subject or target: %#v", object)
		}
		if !strings.Contains(object.Fields["comment"], "入口连接标记 · 策略 WAN selected · Selected target IP") || strings.Contains(object.Fields["comment"], "Domain") {
			t.Fatalf("selected matcher comment is not a clean IP label: %#v", object)
		}
	}
	for _, object := range allConnections {
		if object.Fields["src-address-list"] != "" || object.Fields["in-interface-list"] == "" || object.Fields["dst-address-list"] != targetListName || object.Fields["chain"] != "prerouting" {
			t.Fatalf("all matcher unexpectedly narrowed subject: %#v", object)
		}
	}
	for _, object := range excludedConnections {
		if object.Fields["src-address-list"] == "" || object.Fields["src-address-list"][0] != '!' || object.Fields["in-interface-list"] == "" || object.Fields["dst-address-list"] != targetListName || object.Fields["chain"] != "prerouting" {
			t.Fatalf("excluded matcher did not combine ingress and negated subject: %#v", object)
		}
	}
	selectedExecution := desiredObjectsByLogicalPrefix(desired.Objects, "routing-rule-routing:wan-selected:")
	if len(selectedExecution) != 6 {
		t.Fatalf("expected one physical execution group per family and source boundary: %#v", selectedExecution)
	}
	for _, object := range selectedExecution {
		switch {
		case strings.Contains(object.LogicalID, ":ingress:"):
			if object.Fields["in-interface-list"] == "" || object.Fields["src-address-list"] != "" {
				t.Fatalf("all execution group has the wrong source guard: %#v", object)
			}
		case strings.Contains(object.LogicalID, ":selected:"):
			if object.Fields["in-interface-list"] != "" || object.Fields["src-address-list"] == "" {
				t.Fatalf("selected execution group has the wrong source guard: %#v", object)
			}
		case strings.Contains(object.LogicalID, ":excluded:"):
			if object.Fields["in-interface-list"] == "" || object.Fields["src-address-list"] == "" || object.Fields["src-address-list"][0] != '!' {
				t.Fatalf("excluded execution group has the wrong source guard: %#v", object)
			}
		default:
			t.Fatalf("unexpected execution group identity: %#v", object)
		}
		if !ownership.IsCanonical(object.Fields["comment"]) || !strings.Contains(object.Fields["comment"], "策略 WAN selected") || !strings.Contains(object.Fields["comment"], "IP") {
			t.Fatalf("execution group comment lost the strategy label: %#v", object)
		}
		family := "IPv6"
		if strings.Contains(object.LogicalID, ":ipv4:") {
			family = "IPv4"
		}
		wantComment := "入口路由标记 · 策略 WAN selected · " + family
		if !strings.HasSuffix(object.Fields["comment"], " | "+wantComment) || strings.Contains(object.Fields["comment"], " · 域名 · ") || strings.Contains(object.Fields["comment"], " · IP · ") || strings.HasSuffix(object.Fields["comment"], " · 域名") || strings.HasSuffix(object.Fields["comment"], " · IP") {
			t.Fatalf("execution group comment is not a clean family label: %#v", object)
		}
	}
	routerConnections := desiredObjectsByLogicalPrefix(desired.Objects, "routing-router-connection:wan-selected:")
	if len(routerConnections) != 2 {
		t.Fatalf("router-output union did not cover both families: %#v", routerConnections)
	}
	for _, object := range routerConnections {
		if object.Fields["src-address-list"] != "" || object.Fields["dst-address-list"] != targetListName || object.Fields["chain"] != "output" {
			t.Fatalf("router-output matcher applied end-device subject: %#v", object)
		}
	}
	if len(desiredObjectsByMenu(desired.Objects, routeros.MenuIPv6FirewallAddressList)) == 0 {
		t.Fatal("IPv6 target/subject projection was lost")
	}
}

func TestSelectedRoutingRuleUsesSourceOnlyWithoutTrafficIngress(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-source-only", Name: "WAN source-only", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "source-only-target", Name: "Source-only target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: "source-only-version", TargetListID: target.ID, SHA256: "source-only", CompressedYAML: []byte("ip"), State: "pending"}, []policyv2.TargetListRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "rule-source-only", Name: "Source-only clients", EgressID: "wan-source-only", TargetListIDs: []string{target.ID}, Enabled: true,
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"192.0.2.0/24"}},
	}); err != nil {
		t.Fatal(err)
	}
	desired, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range desired.Blockers {
		if blocker.Code == "traffic_ingress_required" {
			t.Fatalf("source-only routing rule unexpectedly requires TrafficIngress: %#v", desired.Blockers)
		}
	}
	connections := desiredObjectsByLogicalPrefix(desired.Objects, "routing-rule-connection:rule-source-only:")
	if len(connections) != 1 || connections[0].Fields["src-address-list"] == "" || connections[0].Fields["in-interface-list"] != "" {
		t.Fatalf("source-only routing matcher has the wrong boundary: %#v", connections)
	}
}

func TestRoutingRulesWithDifferentIngressUseDifferentExecutionGroups(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-per-rule-ingress", Name: "Per-rule ingress", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1", RouteTable: "table-per-rule"}},
	}); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "per-rule-target", Name: "Per-rule target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: "per-rule-version", TargetListID: target.ID, SHA256: "per-rule", CompressedYAML: []byte("ip"), State: "pending"}, []policyv2.TargetListRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []policyv2.RoutingRule{
		{ID: "rule-lan", Name: "LAN policy", EgressID: "wan-per-rule-ingress", TargetListIDs: []string{target.ID}, Enabled: true, Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll}, Ingress: policyv2.TrafficIngressScope{InterfaceLists: []string{"LAN"}}},
		{ID: "rule-vlan", Name: "VLAN policy", EgressID: "wan-per-rule-ingress", TargetListIDs: []string{target.ID}, Enabled: true, Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll}, Ingress: policyv2.TrafficIngressScope{InterfaceLists: []string{"VLAN20"}}},
	} {
		if _, err := repository.SaveRoutingRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}
	desired, err := policyv2.BuildRoutingDesired(ctx, repository, newPolicyV2FakeRouter(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("per-rule ingress desired is blocked: %#v", desired.Blockers)
	}
	connections := desiredObjectsByLogicalPrefix(desired.Objects, "routing-rule-connection:")
	if len(connections) != 2 {
		t.Fatalf("expected one matcher per rule: %#v", connections)
	}
	ingressLists := map[string]string{}
	for _, object := range desiredObjectsByLogicalPrefix(desired.Objects, "traffic-ingress:list") {
		ingressLists[object.Fields["name"]] = object.Fields["include"]
	}
	if len(ingressLists) != 2 || !containsStringValue(ingressLists, "LAN") || !containsStringValue(ingressLists, "VLAN20") {
		t.Fatalf("per-rule ingress projections were not materialized independently: %#v", ingressLists)
	}
	for _, object := range connections {
		if ingressLists[object.Fields["in-interface-list"]] == "" {
			t.Fatalf("matcher lost its rule ingress projection: %#v", object)
		}
	}
	executions := desiredObjectsByLogicalPrefix(desired.Objects, "routing-rule-routing:wan-per-rule-ingress:ipv4:ingress:")
	if len(executions) != 2 {
		t.Fatalf("different ingress scopes incorrectly shared an execution group: %#v", executions)
	}
	marks := map[string]bool{}
	for _, object := range executions {
		marks[object.Fields["connection-mark"]] = true
	}
	if len(marks) != 2 {
		t.Fatalf("different ingress scopes reused one final routing mark: %#v", executions)
	}
}

func containsStringValue(values map[string]string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSourceOnlyRoutingRuleIgnoresStaleTrafficIngress(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-source-only-stale", Name: "WAN source-only stale", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["lan"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "source-only-stale-target", Name: "Source-only stale target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: "source-only-stale-version", TargetListID: target.ID, SHA256: "source-only-stale", CompressedYAML: []byte("ip"), State: "pending"}, []policyv2.TargetListRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "rule-source-only-stale", Name: "Source-only stale ingress", EgressID: "wan-source-only-stale", TargetListIDs: []string{target.ID}, Enabled: true,
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"192.0.2.10"}},
	}); err != nil {
		t.Fatal(err)
	}

	issues, err := policyv2.ValidateTrafficIngress(ctx, newPolicyV2FakeRouter(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("source-only routing rule was blocked by stale unused ingress: %#v", issues)
	}

	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "rule-all-stale", Name: "All stale ingress", EgressID: "wan-source-only-stale", TargetListIDs: []string{target.ID}, Enabled: true,
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll},
	}); err != nil {
		t.Fatal(err)
	}
	issues, err = policyv2.ValidateTrafficIngress(ctx, newPolicyV2FakeRouter(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStorePlanIssue(issues, "traffic_ingress_list_not_found") {
		t.Fatalf("all-device routing rule did not validate stale ingress: %#v", issues)
	}
}

func TestRoutingRuleDesiredBlocksOnlyProvenOverlappingIPPolicies(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	for _, id := range []string{"wan-a", "wan-b"} {
		if _, err := repository.SaveEgress(ctx, policyv2.Egress{
			ID: id, Name: id, Enabled: true,
			Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "shared-ip", Name: "Shared IP", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: "shared-ip-version", TargetListID: target.ID, SHA256: "shared-ip", CompressedYAML: []byte("ip"), State: "pending"}, []policyv2.TargetListRule{{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}}); err != nil {
		t.Fatal(err)
	}
	for index, egressID := range []string{"wan-a", "wan-b"} {
		if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
			ID: "rule-" + egressID, Name: egressID, EgressID: egressID, TargetListIDs: []string{target.ID}, Enabled: true, Priority: index,
			Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll},
		}); err != nil {
			t.Fatal(err)
		}
	}
	desired, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if !hasStorePlanIssue(desired.Blockers, "routing_rule_conflict") {
		t.Fatalf("proven overlapping policies were not blocked: %#v", desired.Blockers)
	}

	rules, err := repository.ListRoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rules[0].Subject = policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"198.51.100.0/24"}}
	if _, err := repository.SaveRoutingRule(ctx, rules[0]); err != nil {
		t.Fatal(err)
	}
	rules[1].Subject = policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"203.0.113.0/24"}}
	if _, err := repository.SaveRoutingRule(ctx, rules[1]); err != nil {
		t.Fatal(err)
	}
	desired, err = policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if hasStorePlanIssue(desired.Blockers, "routing_rule_conflict") {
		t.Fatalf("disjoint subjects were blocked: %#v", desired.Blockers)
	}
}

func hasStorePlanIssue(issues []policyv2.PlanIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func saveStoreDomainTarget(t *testing.T, repository *PolicyRepository, ctx context.Context, id string, rules ...policyv2.TargetListRule) {
	t.Helper()
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: id, Name: id, Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: id + "-version", TargetListID: target.ID, SHA256: id, CompressedYAML: []byte("domain"), State: "pending"}, rules); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingDNSStaticStagedDisabledLeftoverIsActivatedNotDuplicated(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-a", Name: "wan-a", Enabled: true, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53",
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	saveStoreDomainTarget(t, repository, ctx, "domain-left", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "bdn.dev"})
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "rule-left", Name: "Left", EgressID: "wan-a", TargetListIDs: []string{"domain-left"}, Enabled: true, Priority: 10,
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll},
	}); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: repository}); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.GeneratePlan(ctx, "default", "routing-leftover-seed")
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		t.Fatalf("seed apply failed: %#v", job)
	}
	// Simulate the exact field state after a failed apply interrupted between
	// staging and activation: the owned DNS Static exists but stays disabled.
	logicalID := "routing-dns:wan-a:domain-left:DOMAIN-SUFFIX:bdn.dev"
	router.mu.Lock()
	dnsStatics := router.objects[routeros.MenuIPDNSStatic]
	present := 0
	for _, object := range dnsStatics {
		if object["name"] == "bdn.dev" {
			object["disabled"] = "true"
			present++
		}
	}
	if present != 1 {
		router.mu.Unlock()
		t.Fatalf("seed must create exactly one bdn.dev DNS static, got %d", present)
	}
	router.mu.Unlock()

	// The next plan must reuse the owned object: no duplicate create, and the
	// only DNS-static mutation is the activation enable patch.
	plan, err = manager.GeneratePlan(ctx, "default", "routing-leftover-recover")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Blockers) != 0 {
		t.Fatalf("leftover recovery plan blocked: %#v", plan.Plan.Blockers)
	}
	staticCreates, staticEnablePatches := 0, 0
	for _, operation := range plan.Plan.Operations {
		if operation.Menu != string(routeros.MenuIPDNSStatic) || operation.LogicalID != logicalID {
			continue
		}
		switch {
		case operation.Action == "create":
			staticCreates++
		case operation.Action == "patch" && strings.EqualFold(operation.After["disabled"], "no"):
			staticEnablePatches++
		}
	}
	if staticCreates != 0 || staticEnablePatches != 1 {
		t.Fatalf("leftover DNS static must activate once without re-creation: creates=%d enable-patches=%d ops=%#v", staticCreates, staticEnablePatches, plan.Plan.Operations)
	}
	job, err = manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		t.Fatalf("leftover recovery apply failed: %#v", job)
	}
	router.mu.Lock()
	defer router.mu.Unlock()
	statics := 0
	for _, object := range router.objects[routeros.MenuIPDNSStatic] {
		if object["name"] == "bdn.dev" {
			statics++
			if object["disabled"] != "false" {
				t.Fatalf("recovered DNS static did not activate: %#v", object)
			}
		}
	}
	if statics != 1 {
		t.Fatalf("recovery left %d bdn.dev statics, want 1", statics)
	}
}

func TestRoutingDNSStaticOrderConvergesThroughRealApply(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	for index, id := range []string{"wan-a", "wan-b"} {
		if _, err := repository.SaveEgress(ctx, policyv2.Egress{
			ID: id, Name: id, Enabled: true, DNSUpstream: "1.1.1.1", FakeAlias: fmt.Sprintf("192.0.2.5%d", 3+index),
			Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	saveStoreDomainTarget(t, repository, ctx, "domain-exact", policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "api.example.com"})
	saveStoreDomainTarget(t, repository, ctx, "domain-suffix", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"})
	// Case 4 layout: p10 exact → wan-a, p20 suffix → wan-b. The exact static
	// must lead the suffix static on RouterOS so api.example.com resolves via
	// wan-a while the rest of example.com still resolves via wan-b.
	for _, rule := range []policyv2.RoutingRule{
		{ID: "rule-exact", Name: "Exact high", EgressID: "wan-a", TargetListIDs: []string{"domain-exact"}, Enabled: true, Priority: 10, Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll}},
		{ID: "rule-suffix", Name: "Suffix low", EgressID: "wan-b", TargetListIDs: []string{"domain-suffix"}, Enabled: true, Priority: 20, Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll}},
	} {
		if _, err := repository.SaveRoutingRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: repository}); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.GeneratePlan(ctx, "default", "routing-domain-priority")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Blockers) != 0 {
		t.Fatalf("priority-resolved domain overlap must apply: %#v", plan.Plan.Blockers)
	}
	job, err := manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		t.Fatalf("initial apply failed: %#v", job)
	}
	routingStaticOrder := func() []string {
		router.mu.Lock()
		defer router.mu.Unlock()
		names := make([]string, 0, 2)
		for _, id := range router.order[routeros.MenuIPDNSStatic] {
			object := router.objects[routeros.MenuIPDNSStatic][id]
			if strings.HasPrefix(object["comment"], "rbs_") {
				names = append(names, object["name"])
			}
		}
		return names
	}
	if order := routingStaticOrder(); len(order) != 2 || order[0] != "api.example.com" || order[1] != "example.com" {
		t.Fatalf("creates must land in priority order: %#v", order)
	}

	// Simulate RouterOS physical-order drift (for example a manual reordering)
	// and prove the reconciler converges it back with a real move through the
	// full GeneratePlan → ApplyPlan executor, not just a helper sort.
	router.mu.Lock()
	staticIDs := make([]string, 0, 2)
	for _, id := range router.order[routeros.MenuIPDNSStatic] {
		if router.objects[routeros.MenuIPDNSStatic][id]["type"] == "FWD" {
			staticIDs = append(staticIDs, id)
		}
	}
	for left, right := 0, len(staticIDs)-1; left < right; left, right = left+1, right-1 {
		indexLeft, indexRight := -1, -1
		for index, id := range router.order[routeros.MenuIPDNSStatic] {
			if id == staticIDs[left] {
				indexLeft = index
			}
			if id == staticIDs[right] {
				indexRight = index
			}
		}
		router.order[routeros.MenuIPDNSStatic][indexLeft], router.order[routeros.MenuIPDNSStatic][indexRight] = router.order[routeros.MenuIPDNSStatic][indexRight], router.order[routeros.MenuIPDNSStatic][indexLeft]
	}
	router.mu.Unlock()
	plan, err = manager.GeneratePlan(ctx, "default", "routing-domain-priority-reorder")
	if err != nil {
		t.Fatal(err)
	}
	moves := 0
	for _, operation := range plan.Plan.Operations {
		if operation.Action == "move" && operation.Menu == string(routeros.MenuIPDNSStatic) {
			moves++
		}
	}
	if moves == 0 {
		t.Fatalf("drifted DNS static order must plan moves: %#v", plan.Plan.Operations)
	}
	job, err = manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		t.Fatalf("reorder apply failed: %#v", job)
	}
	if order := routingStaticOrder(); len(order) != 2 || order[0] != "api.example.com" || order[1] != "example.com" {
		t.Fatalf("RouterOS DNS static order did not converge to priority order: %#v", order)
	}
}

func TestRoutingRuleDomainOverlapResolvesByPriorityAndOrdersDNSStatics(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	for _, id := range []string{"wan-a", "wan-b"} {
		if _, err := repository.SaveEgress(ctx, policyv2.Egress{
			ID: id, Name: id, Enabled: true, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53",
			Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "shared-domain", Name: "Shared Domain", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: "shared-domain-version", TargetListID: target.ID, SHA256: "shared-domain", CompressedYAML: []byte("domain"), State: "pending"}, []policyv2.TargetListRule{{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"}}); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []policyv2.RoutingRule{
		{ID: "rule-high", Name: "香港线路", EgressID: "wan-a", TargetListIDs: []string{target.ID}, Enabled: true, Priority: 10, Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll}},
		{ID: "rule-low", Name: "美国线路", EgressID: "wan-b", TargetListIDs: []string{target.ID}, Enabled: true, Priority: 20, Subject: policyv2.Subject{Mode: policyv2.SubjectModeAll}},
	} {
		if _, err := repository.SaveRoutingRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}
	desired, err := policyv2.BuildDesired(ctx, repository, newPolicyV2FakeRouter())
	if err != nil {
		t.Fatal(err)
	}
	if hasStorePlanIssue(desired.Blockers, "routing_rule_conflict") || hasStorePlanIssue(desired.Blockers, "domain_projection_context_ambiguous") {
		t.Fatalf("different-priority domain overlap must not block: %#v", desired.Blockers)
	}
	warning := false
	for _, issue := range desired.Warnings {
		if issue.Code == "domain_projection_priority_shadowed" {
			warning = true
			if !strings.Contains(issue.Reason, "香港线路") || !strings.Contains(issue.Reason, "美国线路") || !strings.Contains(issue.Reason, "youtube.com") {
				t.Fatalf("warning must name both rules and the overlapped matcher: %q", issue.Reason)
			}
		}
	}
	if !warning {
		t.Fatalf("priority-resolved domain overlap must warn: %#v", desired.Warnings)
	}

	// The desired DNS Static sequence is the device-global priority order:
	// every routing static of the p10 egress precedes the p20 egress.
	staticOrder := make([]string, 0, 2)
	staticObjects := make(map[string]policyv2.DesiredObject, 2)
	for _, object := range desired.Objects {
		if object.Menu == string(routeros.MenuIPDNSStatic) && strings.HasPrefix(object.LogicalID, "routing-dns:") {
			staticOrder = append(staticOrder, object.LogicalID)
			staticObjects[object.LogicalID] = object
		}
	}
	if len(staticOrder) != 2 || !strings.HasPrefix(staticOrder[0], "routing-dns:wan-a:") || !strings.HasPrefix(staticOrder[1], "routing-dns:wan-b:") {
		t.Fatalf("routing DNS statics must follow effective projection priority: %#v", staticOrder)
	}
	if staticObjects[staticOrder[0]].Order >= staticObjects[staticOrder[1]].Order {
		t.Fatalf("desired Order does not carry the priority sequence: %#v", staticObjects)
	}

	// Real DesiredObject.Order → PlanOperation move chain: seed RouterOS with
	// the same managed statics in reversed physical order and prove the
	// reconciler plans a converging move, then converges after it is applied.
	router := newPolicyV2FakeRouter()
	for _, logicalID := range []string{staticOrder[1], staticOrder[0]} {
		object := staticObjects[logicalID]
		router.nextID++
		id := fmt.Sprintf("*%d", router.nextID)
		seeded := routeros.RouterOSObject{".id": id}
		for key, value := range object.Fields {
			seeded[key] = value
		}
		if router.objects[routeros.MenuIPDNSStatic] == nil {
			router.objects[routeros.MenuIPDNSStatic] = make(map[string]routeros.RouterOSObject)
		}
		router.objects[routeros.MenuIPDNSStatic][id] = seeded
		router.order[routeros.MenuIPDNSStatic] = append(router.order[routeros.MenuIPDNSStatic], id)
	}
	actual, _, err := policyv2.ScanManaged(ctx, router, repository, desired.Objects)
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers := policyv2.DiffDesired(desired.Objects, actual)
	if len(blockers) != 0 {
		t.Fatalf("unexpected diff blockers: %#v", blockers)
	}
	moves := 0
	for _, operation := range operations {
		if operation.Action == "move" && operation.Menu == string(routeros.MenuIPDNSStatic) {
			moves++
		}
	}
	if moves != 1 {
		t.Fatalf("reversed RouterOS DNS static order must plan exactly one priority move: operations=%#v", operations)
	}
	for _, operation := range operations {
		if operation.Action == "move" {
			if _, err := router.Move(ctx, routeros.MutationMenu(operation.Menu), routeros.MoveRequest{ID: operation.RouterID, BeforeID: operation.Anchor.RouterID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	actual, _, err = policyv2.ScanManaged(ctx, router, repository, desired.Objects)
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers = policyv2.DiffDesired(desired.Objects, actual)
	if len(blockers) != 0 {
		t.Fatalf("unexpected post-move blockers: %#v", blockers)
	}
	for _, operation := range operations {
		if operation.Action == "move" && operation.Menu == string(routeros.MenuIPDNSStatic) {
			t.Fatalf("DNS static ordering did not converge after move: %#v", operations)
		}
	}
}
