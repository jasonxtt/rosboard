package policyv2

import (
	"strconv"
	"strings"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/routeros"
)

func TestAppendDNSCacheWarningOnlyAboveRuleThreshold(t *testing.T) {
	for _, test := range []struct {
		name      string
		ruleCount int
		warnings  int
	}{
		{name: "threshold", ruleCount: 1000, warnings: 0},
		{name: "above threshold", ruleCount: 1001, warnings: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := DesiredResult{Objects: make([]DesiredObject, 0, test.ruleCount)}
			for index := 0; index < test.ruleCount; index++ {
				result.Objects = append(result.Objects, DesiredObject{
					Menu:   string(routeros.MenuIPDNSStatic),
					Fields: map[string]string{"name": "domain-" + strconv.Itoa(index) + ".example"},
				})
			}

			appendDNSCacheWarning(&result)
			if len(result.Warnings) != test.warnings {
				t.Fatalf("warnings=%d, want %d: %#v", len(result.Warnings), test.warnings, result.Warnings)
			}
			if test.warnings == 1 && !strings.Contains(result.Warnings[0].Reason, "32MiB") {
				t.Fatalf("warning does not mention the default cache size: %#v", result.Warnings[0])
			}
		})
	}
}

func TestSharedListNameUsesEgressName(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "Google Exit", want: "manual_google_exit_lab"},
		{name: "国际 出口", want: "manual_国际_出口_lab"},
		{name: "", want: "manual_policy_lab"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := SharedListName(test.name); got != test.want {
				t.Fatalf("SharedListName(%q)=%q, want %q", test.name, got, test.want)
			}
		})
	}
}

func TestSourceListNameIsStableAcrossRenameAndScopedToDevice(t *testing.T) {
	first := SourceListName("manager-a", "router-a", Source{ID: "source-a", Name: "Bilibili"})
	renamed := SourceListName("manager-a", "router-a", Source{ID: "source-a", Name: "哔哩哔哩"})
	otherSource := SourceListName("manager-a", "router-a", Source{ID: "source-b", Name: "Bilibili"})
	otherDevice := SourceListName("manager-a", "router-b", Source{ID: "source-a", Name: "Bilibili"})

	if first != renamed {
		t.Fatalf("source rename changed stable list name: %q != %q", first, renamed)
	}
	if first == otherSource || first == otherDevice {
		t.Fatalf("source list name is not scoped to source/device: first=%q source=%q device=%q", first, otherSource, otherDevice)
	}
	if !strings.HasPrefix(first, "rb_src_") {
		t.Fatalf("unexpected source list prefix: %q", first)
	}
}

func TestNeedsAccessForwarderForDisabledDomainSource(t *testing.T) {
	rules := []accesscontrol.AccessRule{{
		ID: "rule-a", Enabled: true, TargetScope: accesscontrol.TargetScopeTargets, TargetListIDs: []string{"source-a"},
	}}
	sources := map[string]Source{
		"rule-a\x00source-a": {ID: "source-a", Kind: KindDomain, Enabled: false, ActiveVersionID: "version-a"},
	}
	if !needsAccessForwarder(rules, sources) {
		t.Fatal("a disabled domain source referenced by access control still needs the device-level access DNS forwarder")
	}
}

func TestSharedBuildFamilyUsesOneConnectionMarkAndOneRoutingRule(t *testing.T) {
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}

	buildFamily(&result, add, Egress{ID: "wan-a", ListMode: ListModeShared, ListName: "legacy-shared", FailureMode: "strict"}, EgressFamily{
		Family: FamilyIPv4, Enabled: true, WANInterface: "ether1", RouteTable: "wan-a-table",
	}, "198.51.100.1", map[string]string{"source-a": "rb_src_a", "source-b": "rb_src_b"}, "rosboard-lan", "manager", "device", "no")

	connectionMarks := map[string]bool{}
	routingRules := 0
	connectionRules := 0
	for _, object := range result.Objects {
		if object.Menu != string(routeros.MenuIPFirewallMangle) || object.Fields["chain"] != "prerouting" {
			continue
		}
		switch object.Fields["action"] {
		case "mark-connection":
			connectionRules++
			connectionMarks[object.Fields["new-connection-mark"]] = true
		case "mark-routing":
			routingRules++
		}
	}
	if connectionRules != 2 || len(connectionMarks) != 1 || routingRules != 1 {
		t.Fatalf("shared egress must use two source matchers, one connection mark and one routing rule: rules=%d marks=%v routing=%d objects=%#v", connectionRules, connectionMarks, routingRules, result.Objects)
	}
}

func TestSharedBuildFamilySkipsEmptySourceSet(t *testing.T) {
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}

	buildFamily(&result, add, Egress{ID: "wan-a", ListMode: ListModeShared, FailureMode: "strict"}, EgressFamily{
		Family: FamilyIPv4, Enabled: true, WANInterface: "ether1", RouteTable: "wan-a-table",
	}, "198.51.100.1", nil, "rosboard-lan", "manager", "device", "no")

	for _, object := range result.Objects {
		if object.Menu == string(routeros.MenuIPFirewallMangle) {
			t.Fatalf("shared egress without source lists must not create dangling mangle rules: %#v", result.Objects)
		}
	}
}

func TestBuildFamilyDoesNotCreateMasquerade(t *testing.T) {
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}

	buildFamily(&result, add, Egress{ID: "wan-a", FailureMode: "strict"}, EgressFamily{
		Family: FamilyIPv4, Enabled: true, WANInterface: "ether1", RouteTable: "wan-a-table", NATMode: "masquerade",
	}, "198.51.100.1", map[string]string{"source-a": "policy-list"}, "rosboard-lan", "manager", "device", "no")

	for _, object := range result.Objects {
		if object.Menu == string(routeros.MenuIPFirewallNAT) && object.Fields["chain"] == "srcnat" && object.Fields["action"] == "masquerade" {
			t.Fatalf("buildFamily created an unmanaged masquerade rule: %#v", object)
		}
	}

	result = DesiredResult{}
	addDNSTransport(add, "wan-a", "192.0.2.53", "198.51.100.53", "wan-a-table", true, "no")
	foundDNSRedirect := false
	for _, object := range result.Objects {
		if object.Menu == string(routeros.MenuIPFirewallNAT) && object.Fields["chain"] == "output" && object.Fields["action"] == "dst-nat" {
			foundDNSRedirect = true
		}
	}
	if !foundDNSRedirect {
		t.Fatal("Fake DNS transport lost its policy-owned dst-nat redirect")
	}
}

func TestRoutingIPv4SubjectDoesNotBlockOtherEgressFamily(t *testing.T) {
	target := &routingTargetProjection{
		id: "domain-target", list: "domain-list", source: Source{ID: "domain-target", Kind: KindDomain},
		rules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.com"}},
	}
	rule := RoutingRule{
		ID: "rule-ipv4-only", EgressID: "wan-a", TargetListIDs: []string{"domain-target"}, Enabled: true,
		Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{
			TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF",
		}}},
	}
	terminals := []accesscontrol.Terminal{{ID: "terminal-a", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"192.0.2.20"}}}

	for _, test := range []struct {
		name        string
		family      AddressFamily
		terminals   []accesscontrol.Terminal
		wantID      string
		wantWarning string
	}{
		{name: "IPv4 only", family: FamilyIPv4, terminals: terminals, wantID: "routing-rule-connection:rule-ipv4-only:ipv4:domain-target"},
		{name: "IPv4-only source with IPv6 egress", family: FamilyIPv6, terminals: terminals, wantWarning: "IPv6"},
		{name: "IPv6 only", family: FamilyIPv6, terminals: []accesscontrol.Terminal{{ID: "terminal-a", MACAddress: "AA:BB:CC:DD:EE:FF", IPv6: []string{"2001:db8::20"}}}, wantID: "routing-rule-connection:rule-ipv4-only:ipv6:domain-target"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := DesiredResult{}
			add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
				result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
			}
			buildRoutingMangleFamily(&result, add, Egress{ID: "wan-a", Enabled: true}, EgressFamily{Family: test.family}, map[string]string{}, map[string]bool{}, "wan-a-table", []*routingTargetProjection{target}, []RoutingRule{rule}, test.terminals, "no", "manager", "device")
			if len(result.Blockers) != 0 {
				t.Fatalf("family %s produced blockers: %#v", test.family, result.Blockers)
			}
			if test.wantID != "" {
				for _, object := range result.Objects {
					if object.LogicalID == test.wantID {
						return
					}
				}
				t.Fatalf("IPv4 mangle was not created: %#v", result.Objects)
			}
			if test.wantWarning != "" {
				for _, warning := range result.Warnings {
					if strings.Contains(warning.Reason, test.wantWarning) && !strings.Contains(warning.Reason, "selected routing subject") {
						return
					}
				}
				t.Fatalf("IPv6 family warning was not human-readable: %#v", result.Warnings)
			}
		})
	}
}

func TestRoutingAutoSubjectProjectsOnlyUsableIPv6(t *testing.T) {
	terminals := RoutingUsableTerminals([]accesscontrol.Terminal{{
		ID: "terminal-a", MACAddress: "AA:BB:CC:DD:EE:FF", IPv6: []string{"2001:db8::20", "fe80::20"},
	}})
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	rule := RoutingRule{
		ID: "rule-ipv6-auto", EgressID: "wan-a", TargetListIDs: []string{"domain-target"}, Enabled: true,
		Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{
			TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF",
		}}},
	}
	if !buildRoutingSubjectList(&result, add, rule, FamilyIPv6, terminals, "routing-subject-list", true) {
		t.Fatal("usable IPv6 address should produce a subject list")
	}
	addresses := []string{}
	for _, object := range result.Objects {
		if object.Menu == string(routeros.MenuIPv6FirewallAddressList) {
			addresses = append(addresses, object.Fields["address"])
		}
	}
	if strings.Join(addresses, ",") != "2001:db8::20" {
		t.Fatalf("routing subject projected non-usable IPv6 addresses: %#v", addresses)
	}
}

func TestRoutingSelectedIPv6SubjectPartiallyResolves(t *testing.T) {
	target := &routingTargetProjection{
		id: "domain-target", list: "domain-list", source: Source{ID: "domain-target", Kind: KindDomain},
		rules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.com"}},
	}
	rule := RoutingRule{
		ID: "rule-selected-partial", EgressID: "wan-a", TargetListIDs: []string{"domain-target"}, Enabled: true,
		Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{
			{TerminalID: "terminal-30", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:30"},
			{TerminalID: "terminal-98", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:98"},
		}},
	}
	terminals := []accesscontrol.Terminal{
		{ID: "terminal-30", DisplayName: "10.0.0.30", MACAddress: "AA:BB:CC:DD:EE:30", IPv4: []string{"10.0.0.30"}},
		{ID: "terminal-98", DisplayName: "10.0.0.98", MACAddress: "AA:BB:CC:DD:EE:98", IPv4: []string{"10.0.0.98"}, IPv6: []string{"2001:db8::98"}},
	}
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	buildRoutingMangleFamily(&result, add, Egress{ID: "wan-a", Enabled: true}, EgressFamily{Family: FamilyIPv6}, map[string]string{}, map[string]bool{}, "wan-a-table", []*routingTargetProjection{target}, []RoutingRule{rule}, terminals, "no", "manager", "device")

	if !hasDesiredObject(result.Objects, "routing-rule-connection:rule-selected-partial:ipv6:domain-target") {
		t.Fatalf("selected IPv6 rule with one known member did not activate: %#v", result.Objects)
	}
	addresses := desiredObjectFieldValues(result.Objects, string(routeros.MenuIPv6FirewallAddressList), "address")
	if strings.Join(addresses, ",") != "2001:db8::98" {
		t.Fatalf("selected IPv6 rule projected unexpected addresses: %#v", addresses)
	}
	if !hasRoutingWarning(result.Warnings, "routing_subject_family_partial", "10.0.0.30") {
		t.Fatalf("selected partial IPv6 resolution warning missing: %#v", result.Warnings)
	}
}

func TestRoutingExcludedIPv6SubjectFailsClosedUntilAllMembersResolve(t *testing.T) {
	target := &routingTargetProjection{
		id: "domain-target", list: "domain-list", source: Source{ID: "domain-target", Kind: KindDomain},
		rules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.com"}},
	}
	baseRule := RoutingRule{
		ID: "rule-excluded-family", EgressID: "wan-a", TargetListIDs: []string{"domain-target"}, Enabled: true,
		Subject: Subject{Mode: SubjectModeExcluded, Members: []SubjectMember{
			{TerminalID: "terminal-30", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:30"},
		}},
		Ingress: TrafficIngressScope{Interfaces: []string{"lan"}},
	}
	terminals := []accesscontrol.Terminal{
		{ID: "terminal-30", DisplayName: "10.0.0.30", MACAddress: "AA:BB:CC:DD:EE:30", IPv4: []string{"10.0.0.30"}},
		{ID: "terminal-98", DisplayName: "10.0.0.98", MACAddress: "AA:BB:CC:DD:EE:98", IPv4: []string{"10.0.0.98"}, IPv6: []string{"2001:db8::98"}},
	}
	ingressLists := map[string]string{baseRule.ID: "ingress-list"}
	ingressReady := map[string]bool{baseRule.ID: true}

	for _, test := range []struct {
		name       string
		rule       RoutingRule
		wantActive bool
		wantWarn   bool
	}{
		{name: "one IPv4-only member", rule: baseRule, wantWarn: true},
		{name: "one IPv4-only plus one IPv6 member", rule: withRoutingSubjectMember(baseRule, "terminal-98", "AA:BB:CC:DD:EE:98"), wantWarn: true},
		{name: "all members have IPv6", rule: withRoutingSubjectMember(baseRule, "terminal-98", "AA:BB:CC:DD:EE:98"), wantActive: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			currentTerminals := terminals
			if test.name == "all members have IPv6" {
				currentTerminals = []accesscontrol.Terminal{
					{ID: "terminal-30", DisplayName: "10.0.0.30", MACAddress: "AA:BB:CC:DD:EE:30", IPv6: []string{"2001:db8::30"}},
					terminals[1],
				}
			}
			result := DesiredResult{}
			add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
				result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
			}
			buildRoutingMangleFamily(&result, add, Egress{ID: "wan-a", Enabled: true}, EgressFamily{Family: FamilyIPv6}, ingressLists, ingressReady, "wan-a-table", []*routingTargetProjection{target}, []RoutingRule{test.rule}, currentTerminals, "no", "manager", "device")
			active := hasDesiredObject(result.Objects, "routing-rule-connection:"+test.rule.ID+":ipv6:domain-target")
			if active != test.wantActive {
				t.Fatalf("excluded IPv6 activation=%v, want %v; objects=%#v", active, test.wantActive, result.Objects)
			}
			if hasRoutingWarning(result.Warnings, "routing_exclusion_family_unresolved", "10.0.0.30") != test.wantWarn {
				t.Fatalf("excluded IPv6 warning mismatch: %#v", result.Warnings)
			}
		})
	}
}

func TestRoutingAllIPv6SubjectDoesNotNeedTerminalAddresses(t *testing.T) {
	target := &routingTargetProjection{
		id: "domain-target", list: "domain-list", source: Source{ID: "domain-target", Kind: KindDomain},
		rules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.com"}},
	}
	rule := RoutingRule{ID: "rule-all-family", EgressID: "wan-a", TargetListIDs: []string{"domain-target"}, Enabled: true, Subject: Subject{Mode: SubjectModeAll}, Ingress: TrafficIngressScope{Interfaces: []string{"lan"}}}
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	buildRoutingMangleFamily(&result, add, Egress{ID: "wan-a", Enabled: true}, EgressFamily{Family: FamilyIPv6}, map[string]string{rule.ID: "ingress-list"}, map[string]bool{rule.ID: true}, "wan-a-table", []*routingTargetProjection{target}, []RoutingRule{rule}, nil, "no", "manager", "device")
	if !hasDesiredObject(result.Objects, "routing-rule-connection:rule-all-family:ipv6:domain-target") {
		t.Fatalf("all IPv6 rule should not depend on terminal IPv6 observations: %#v", result.Objects)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("all IPv6 rule unexpectedly warned about missing terminals: %#v", result.Warnings)
	}
}

func TestRoutingSelectedIPv6PrefixCanActivateWithUnresolvedMember(t *testing.T) {
	target := &routingTargetProjection{
		id: "domain-target", list: "domain-list", source: Source{ID: "domain-target", Kind: KindDomain},
		rules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.com"}},
	}
	rule := RoutingRule{
		ID: "rule-prefix-family", EgressID: "wan-a", TargetListIDs: []string{"domain-target"}, Enabled: true,
		Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{TerminalID: "terminal-30", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:30"}}, Prefixes: []string{"2001:db8:30::/64"}},
	}
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	buildRoutingMangleFamily(&result, add, Egress{ID: "wan-a", Enabled: true}, EgressFamily{Family: FamilyIPv6}, map[string]string{}, map[string]bool{}, "wan-a-table", []*routingTargetProjection{target}, []RoutingRule{rule}, []accesscontrol.Terminal{{ID: "terminal-30", DisplayName: "10.0.0.30", MACAddress: "AA:BB:CC:DD:EE:30", IPv4: []string{"10.0.0.30"}}}, "no", "manager", "device")
	if !hasDesiredObject(result.Objects, "routing-rule-connection:rule-prefix-family:ipv6:domain-target") {
		t.Fatalf("manual IPv6 prefix should activate selected rule: %#v", result.Objects)
	}
	if !hasRoutingWarning(result.Warnings, "routing_subject_family_partial", "10.0.0.30") {
		t.Fatalf("manual-prefix partial warning missing: %#v", result.Warnings)
	}
}

func TestRoutingExcludedIPv6PrefixDoesNotMaskUnresolvedMember(t *testing.T) {
	target := &routingTargetProjection{
		id: "domain-target", list: "domain-list", source: Source{ID: "domain-target", Kind: KindDomain},
		rules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.com"}},
	}
	rule := RoutingRule{
		ID: "rule-excluded-prefix", EgressID: "wan-a", TargetListIDs: []string{"domain-target"}, Enabled: true,
		Subject: Subject{Mode: SubjectModeExcluded, Members: []SubjectMember{{TerminalID: "terminal-30", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:30"}}, Prefixes: []string{"2001:db8:30::/64"}},
		Ingress: TrafficIngressScope{Interfaces: []string{"lan"}},
	}
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	buildRoutingMangleFamily(&result, add, Egress{ID: "wan-a", Enabled: true}, EgressFamily{Family: FamilyIPv6}, map[string]string{rule.ID: "ingress-list"}, map[string]bool{rule.ID: true}, "wan-a-table", []*routingTargetProjection{target}, []RoutingRule{rule}, []accesscontrol.Terminal{{ID: "terminal-30", DisplayName: "10.0.0.30", MACAddress: "AA:BB:CC:DD:EE:30", IPv4: []string{"10.0.0.30"}}}, "no", "manager", "device")
	if hasDesiredObject(result.Objects, "routing-rule-connection:rule-excluded-prefix:ipv6:domain-target") {
		t.Fatalf("manual IPv6 prefix must not mask an unresolved excluded member: %#v", result.Objects)
	}
	if !hasRoutingWarning(result.Warnings, "routing_exclusion_family_unresolved", "10.0.0.30") {
		t.Fatalf("excluded manual-prefix warning missing: %#v", result.Warnings)
	}
}

func TestRoutingIPv6FoundationIsBuiltWithoutSourceIPv6(t *testing.T) {
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	_, ok := buildRoutingFamilyFoundation(&result, add, Egress{ID: "wan-a", Enabled: true}, EgressFamily{Family: FamilyIPv6, RouteTable: "wan-a-table", RouteMode: "strict"}, "2001:db8::1", "manager", "device", "no")
	if !ok {
		t.Fatalf("IPv6 egress foundation unexpectedly failed: %#v", result.Blockers)
	}
	if !hasDesiredObject(result.Objects, "route:wan-a:ipv6") {
		t.Fatalf("IPv6 egress default route was not built: %#v", result.Objects)
	}
}

func hasDesiredObject(objects []DesiredObject, logicalID string) bool {
	for _, object := range objects {
		if object.LogicalID == logicalID {
			return true
		}
	}
	return false
}

func desiredObjectFieldValues(objects []DesiredObject, menu, field string) []string {
	values := []string{}
	for _, object := range objects {
		if object.Menu == menu {
			values = append(values, object.Fields[field])
		}
	}
	return values
}

func hasRoutingWarning(warnings []PlanIssue, code, fragment string) bool {
	for _, warning := range warnings {
		if warning.Code == code && strings.Contains(warning.Reason, fragment) {
			return true
		}
	}
	return false
}

func withRoutingSubjectMember(rule RoutingRule, terminalID, anchorMAC string) RoutingRule {
	rule.Subject.Members = append(rule.Subject.Members, SubjectMember{TerminalID: terminalID, Binding: "auto", AnchorMAC: anchorMAC})
	return rule
}
