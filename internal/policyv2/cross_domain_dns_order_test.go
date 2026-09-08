package policyv2

import (
	"testing"

	"rosboard/internal/routeros"
)

func crossDomainTestDNSObject(logicalID, name, matchSubdomain string, order int) DesiredObject {
	return DesiredObject{
		Domain: PolicyDomainAccess, LogicalID: logicalID, Menu: string(routeros.MenuIPDNSStatic), Order: order, Phase: "dns",
		Fields: map[string]string{"name": name, "type": "FWD", "disabled": "no", "match-subdomain": matchSubdomain, "address-list": "rb_ac_target", "forward-to": "rosboard_access_dns"},
	}
}

func crossDomainTestConstraint() crossDomainDNSConstraint {
	return crossDomainDNSConstraint{
		AccessLogicalID:  "access-target-dns:access-target:DOMAIN:youtube.com",
		RoutingLogicalID: "routing-dns:wan-a:routing-target:DOMAIN-SUFFIX:youtube.com",
		AccessRuleID:     "access-rule", AccessTargetID: "access-target",
		RoutingRuleID: "routing-rule", RoutingEgressID: "wan-a", RoutingTargetID: "routing-target",
		AccessMatcher:  SourceRule{RuleType: "DOMAIN", Domain: "youtube.com"},
		RoutingMatcher: SourceRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"},
	}
}

func TestPlanCrossDomainDNSMovesAccessBeforeExistingRouting(t *testing.T) {
	constraint := crossDomainTestConstraint()
	access := crossDomainTestDNSObject(constraint.AccessLogicalID, "youtube.com", "no", 1)
	routing := crossDomainTestDNSObject(constraint.RoutingLogicalID, "youtube.com", "yes", 2)
	routing.Domain = PolicyDomainRouting
	actual := []ActualObject{
		{LogicalID: constraint.RoutingLogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*routing", Position: 4, Ownership: "owned", Fields: map[string]string{"disabled": "no"}},
	}
	operations := planCrossDomainDNSMoves(PolicyDomainAccess, []DesiredObject{access}, []DesiredObject{routing}, []crossDomainDNSConstraint{constraint}, actual)
	if len(operations) != 1 {
		t.Fatalf("new Access projection needs one move before existing Routing projection: %#v", operations)
	}
	operation := operations[0]
	if operation.LogicalID != constraint.AccessLogicalID || operation.RouterID != "" || operation.Anchor == nil || operation.Anchor.RouterID != "*routing" {
		t.Fatalf("Access-first move did not preserve create-time source and existing anchor: %#v", operation)
	}

	actual = append(actual, ActualObject{LogicalID: constraint.AccessLogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*access", Position: 1, Ownership: "owned", Fields: map[string]string{"disabled": "no"}})
	if operations = planCrossDomainDNSMoves(PolicyDomainAccess, []DesiredObject{access}, []DesiredObject{routing}, []crossDomainDNSConstraint{constraint}, actual); len(operations) != 0 {
		t.Fatalf("already-converged Access-first order must be idempotent: %#v", operations)
	}
}

func TestPlanCrossDomainDNSMovesBeforeEarliestOverlappingRoutingStatic(t *testing.T) {
	first := crossDomainTestConstraint()
	second := first
	second.RoutingLogicalID = "routing-dns:wan-b:routing-target-b:DOMAIN:youtube.com"
	second.RoutingEgressID = "wan-b"
	second.RoutingTargetID = "routing-target-b"
	second.RoutingMatcher = SourceRule{RuleType: "DOMAIN", Domain: "youtube.com"}
	access := crossDomainTestDNSObject(first.AccessLogicalID, "youtube.com", "no", 1)
	routingA := crossDomainTestDNSObject(first.RoutingLogicalID, "youtube.com", "yes", 2)
	routingA.Domain = PolicyDomainRouting
	routingB := crossDomainTestDNSObject(second.RoutingLogicalID, "youtube.com", "no", 3)
	routingB.Domain = PolicyDomainRouting
	actual := []ActualObject{
		{LogicalID: first.RoutingLogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*routing-a", Position: 6, Ownership: "owned", Fields: map[string]string{"disabled": "no"}},
		{LogicalID: second.RoutingLogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*routing-b", Position: 2, Ownership: "owned", Fields: map[string]string{"disabled": "no"}},
		{LogicalID: first.AccessLogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*access", Position: 8, Ownership: "owned", Fields: map[string]string{"disabled": "no"}},
	}
	operations := planCrossDomainDNSMoves(PolicyDomainAccess, []DesiredObject{access}, []DesiredObject{routingA, routingB}, []crossDomainDNSConstraint{first, second}, actual)
	if len(operations) != 1 || operations[0].Anchor == nil || operations[0].Anchor.LogicalID != second.RoutingLogicalID {
		t.Fatalf("Access must move before the earliest overlapping Routing static: %#v", operations)
	}
}

func TestPlanCrossDomainDNSMovesNeverMovesForeignObjects(t *testing.T) {
	constraint := crossDomainTestConstraint()
	access := crossDomainTestDNSObject(constraint.AccessLogicalID, "youtube.com", "no", 1)
	routing := crossDomainTestDNSObject(constraint.RoutingLogicalID, "youtube.com", "yes", 2)
	routing.Domain = PolicyDomainRouting
	actual := []ActualObject{
		{LogicalID: "foreign-scoped:dns", Menu: string(routeros.MenuIPDNSStatic), RouterID: "*foreign", Position: 0, Ownership: "foreign", Fields: map[string]string{"disabled": "no"}},
		{LogicalID: constraint.RoutingLogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*routing", Position: 1, Ownership: "owned", Fields: map[string]string{"disabled": "no"}},
		{LogicalID: constraint.AccessLogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*access", Position: 2, Ownership: "owned", Fields: map[string]string{"disabled": "no"}},
	}
	operations := planCrossDomainDNSMoves(PolicyDomainRouting, []DesiredObject{routing}, []DesiredObject{access}, []crossDomainDNSConstraint{constraint}, actual)
	if len(operations) != 1 || operations[0].RouterID != "*access" || operations[0].Anchor.RouterID != "*routing" {
		t.Fatalf("only the owned Access object may be moved before Routing: %#v", operations)
	}
}

func TestCrossDomainPrecedenceBlocksRoutingWhenAccessIsUnavailable(t *testing.T) {
	constraint := crossDomainTestConstraint()
	access := crossDomainTestDNSObject(constraint.AccessLogicalID, "youtube.com", "no", 1)
	result := DesiredResult{}
	appendCrossDomainPrecedenceBlockers(PolicyDomainRouting, []DesiredObject{access}, nil, []crossDomainDNSConstraint{constraint}, nil, &result)
	if len(result.Blockers) != 1 || result.Blockers[0].Code != "cross_domain_access_precedence_unavailable" {
		t.Fatalf("missing Access projection must block Routing: %#v", result.Blockers)
	}
	result = DesiredResult{}
	appendCrossDomainPrecedenceBlockers(PolicyDomainAccess, []DesiredObject{access}, nil, []crossDomainDNSConstraint{constraint}, nil, &result)
	if len(result.Blockers) != 0 {
		t.Fatalf("Access plan must be allowed to create the missing precedence projection: %#v", result.Blockers)
	}
}

func TestCrossDomainPrecedenceBlocksRoutingWhenAccessProjectionDrifts(t *testing.T) {
	constraint := crossDomainTestConstraint()
	expected := crossDomainTestDNSObject(constraint.AccessLogicalID, "youtube.com", "no", 1)
	base := ActualObject{
		LogicalID: constraint.AccessLogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*access", Position: 0, Ownership: "owned",
		Fields: map[string]string{"name": "youtube.com", "type": "FWD", "match-subdomain": "no", "address-list": "rb_ac_target", "forward-to": "rosboard_access_dns", "disabled": "no"},
	}
	for _, test := range []struct {
		field string
		value string
	}{
		{field: "name", value: "other.example"},
		{field: "type", value: "A"},
		{field: "match-subdomain", value: "yes"},
		{field: "address-list", value: "other-list"},
		{field: "forward-to", value: "other-forwarder"},
	} {
		t.Run(test.field, func(t *testing.T) {
			actual := base
			actual.Fields = cloneStrings(base.Fields)
			actual.Fields[test.field] = test.value
			result := DesiredResult{}
			appendCrossDomainPrecedenceBlockers(PolicyDomainRouting, []DesiredObject{expected}, nil, []crossDomainDNSConstraint{constraint}, []ActualObject{actual}, &result)
			if len(result.Blockers) != 1 || result.Blockers[0].Code != "cross_domain_access_precedence_unavailable" {
				t.Fatalf("Access projection field drift %s must block Routing: %#v", test.field, result.Blockers)
			}
		})
	}
}

func TestPlanCrossDomainDNSMovesIncludesStaleAndDuplicateRoutingMatchers(t *testing.T) {
	access := crossDomainTestDNSObject("access-target-dns:access-target:DOMAIN:youtube.com", "youtube.com", "no", 1)
	routing := crossDomainTestDNSObject("routing-dns:wan-a:routing-target:DOMAIN-SUFFIX:youtube.com", "youtube.com", "yes", 2)
	routing.Domain = PolicyDomainRouting
	actual := []ActualObject{
		{LogicalID: "stale:old-routing-suffix", Menu: string(routeros.MenuIPDNSStatic), RouterID: "*old", Position: 4, Ownership: "owned", Fields: map[string]string{"name": "youtube.com", "match-subdomain": "yes", "disabled": "no"}},
		{LogicalID: "stale:duplicate-routing-suffix", Menu: string(routeros.MenuIPDNSStatic), RouterID: "*duplicate", Position: 6, Ownership: "owned", Fields: map[string]string{"name": "youtube.com", "match-subdomain": "yes", "disabled": "no"}},
		{LogicalID: access.LogicalID, Menu: string(routeros.MenuIPDNSStatic), RouterID: "*access", Position: 8, Ownership: "owned", Fields: cloneStrings(access.Fields)},
	}
	operations := planCrossDomainDNSMoves(PolicyDomainAccess, []DesiredObject{access}, []DesiredObject{routing}, nil, actual)
	if len(operations) != 1 || operations[0].RouterID != "*access" || operations[0].Anchor == nil || operations[0].Anchor.RouterID != "*old" {
		t.Fatalf("Access must move before the earliest stale/duplicate Routing matcher: %#v", operations)
	}
}

func TestCrossDomainActualFingerprintIncludesOwnedDNSOrder(t *testing.T) {
	first := []ActualObject{
		{LogicalID: "access", Menu: string(routeros.MenuIPDNSStatic), RouterID: "*a", Position: 0, Ownership: "owned", Fields: map[string]string{"disabled": "no"}},
		{LogicalID: "routing", Menu: string(routeros.MenuIPDNSStatic), RouterID: "*r", Position: 1, Ownership: "owned", Fields: map[string]string{"disabled": "no"}},
		{LogicalID: "foreign", Menu: string(routeros.MenuIPDNSStatic), RouterID: "*f", Position: 2, Ownership: "foreign", Fields: map[string]string{"disabled": "no"}},
	}
	second := append([]ActualObject(nil), first...)
	second[0].Position, second[1].Position = 1, 0
	left, err := fingerprintCrossDomainActual(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := fingerprintCrossDomainActual(second)
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("owned DNS order change must change the cross-domain actual fingerprint")
	}
	foreignOnly := append([]ActualObject(nil), first...)
	foreignOnly[2].Position = 99
	third, err := fingerprintCrossDomainActual(foreignOnly)
	if err != nil {
		t.Fatal(err)
	}
	if left != third {
		t.Fatal("foreign DNS position must not affect the owned cross-domain fingerprint")
	}
}

func TestBuildCrossDomainDNSConstraintsUsesMatcherOverlapPairs(t *testing.T) {
	resolution := CrossDomainProjectionResolution{
		AccessRuleID: "access-rule", AccessTargetID: "access-target",
		RoutingRuleID: "routing-rule", RoutingEgressID: "wan-a", RoutingTargetID: "routing-target",
		Overlaps: [][2]SourceRule{{
			{RuleType: "DOMAIN", Domain: "youtube.com"},
			{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"},
		}},
	}
	constraints := buildCrossDomainDNSConstraints([]CrossDomainProjectionResolution{resolution})
	if len(constraints) != 1 || constraints[0].AccessLogicalID != "access-target-dns:access-target:DOMAIN:youtube.com" || constraints[0].RoutingLogicalID != "routing-dns:wan-a:routing-target:DOMAIN-SUFFIX:youtube.com" {
		t.Fatalf("unexpected cross-domain DNS constraint: %#v", constraints)
	}
}
