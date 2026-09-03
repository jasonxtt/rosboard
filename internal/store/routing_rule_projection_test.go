package store

import (
	"context"
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
