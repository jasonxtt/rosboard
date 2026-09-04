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
