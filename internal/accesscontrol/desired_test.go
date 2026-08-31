package accesscontrol

import (
	"strings"
	"testing"

	"rosboard/internal/routeros"
)

func sourcesRule(id string, sourceIDs ...string) AccessRule {
	return AccessRule{ID: id, Name: "规则 " + id, TargetScope: TargetScopeSources, SourceIDs: sourceIDs, Enabled: true, Revision: 1}
}

func internetRule(id string) AccessRule {
	return AccessRule{ID: id, Name: "规则 " + id, TargetScope: TargetScopeInternet, Enabled: true, Revision: 1}
}

func fixedMember(ruleID, terminalID, ipv4 string) RuleMember {
	return RuleMember{RuleID: ruleID, TerminalID: terminalID, Binding: BindingFixed, PinnedIPv4: []string{ipv4}}
}

func autoMember(ruleID, terminalID string) RuleMember {
	return RuleMember{RuleID: ruleID, TerminalID: terminalID, Binding: BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF"}
}

func collectByMenu(result DesiredResult, menu routeros.MutationMenu) []DesiredObject {
	objects := make([]DesiredObject, 0)
	for _, object := range result.Objects {
		if object.Menu == menu {
			objects = append(objects, object)
		}
	}
	return objects
}

func TestBuildDesiredExpandsMultiClientMultiSourceWithSharedSubChain(t *testing.T) {
	ruleList := RuleMemberListName("manager", "router-a", "rule-a")
	result := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules:      []AccessRule{sourcesRule("rule-a", "source-a", "source-b")},
		Members:    []RuleMember{fixedMember("rule-a", "t1", "10.0.0.20"), fixedMember("rule-a", "t2", "10.0.0.21")},
		SourceList: map[string]string{"source-a": "rb_src_a", "source-b": "rb_src_b"},
	})
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %#v", result.Blockers)
	}

	filters := collectByMenu(result, routeros.MenuIPFirewallFilter)
	jumps := 0
	for _, object := range filters {
		if object.Fields["chain"] == "forward" && object.Fields["action"] == "jump" {
			jumps++
			if object.Fields["src-address-list"] != ruleList && object.Fields["dst-address-list"] != ruleList {
				t.Fatalf("jump does not reference the per-rule member list: %#v", object)
			}
		}
		if object.Fields["action"] == "accept" {
			t.Fatalf("access control must never accept traffic: %#v", object)
		}
	}
	// 2 sources × (out + in) jumps; both members share one member list and
	// one sub-chain instead of a 2×2 jump matrix.
	if jumps != 4 {
		t.Fatalf("expected 4 bidirectional jumps for 2 sources, got %d: %#v", jumps, filters)
	}

	memberEntries := 0
	for _, object := range collectByMenu(result, routeros.MenuIPFirewallAddressList) {
		if strings.HasPrefix(object.LogicalID, "access-member:") {
			memberEntries++
		}
	}
	if memberEntries != 2 {
		t.Fatalf("expected one member list entry per address, got %d", memberEntries)
	}
}

func TestBuildDesiredDisablesJumpsForDisabledSourcesWithoutDisablingOtherSources(t *testing.T) {
	result := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules:      []AccessRule{sourcesRule("rule-a", "source-a", "source-b")},
		Members:    []RuleMember{fixedMember("rule-a", "t1", "10.0.0.20")},
		SourceList: map[string]string{"source-a": "rb_src_a", "source-b": "rb_src_b"},
		SourceDisabled: map[string]bool{
			"source-a": true,
		},
	})
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %#v", result.Blockers)
	}
	for _, object := range collectByMenu(result, routeros.MenuIPFirewallFilter) {
		if !strings.Contains(object.LogicalID, "jump-") {
			continue
		}
		want := "no"
		if strings.Contains(object.LogicalID, ":source-a") {
			want = "yes"
		}
		if object.Fields["disabled"] != want {
			t.Fatalf("source %s jump disabled=%q, want %q: %#v", object.LogicalID, object.Fields["disabled"], want, object)
		}
	}
}

func TestBuildDesiredInternetScopeUsesDirectEgressRules(t *testing.T) {
	result := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules:     []AccessRule{internetRule("rule-a")},
		Members:   []RuleMember{autoMember("rule-a", "mac:aa")},
		Terminals: []Terminal{{ID: "mac:aa", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"10.0.0.20"}, IPv6: []string{"fd00::20"}}},
		InternetEgresses: map[string][]string{
			FamilyIPv4: {"pppoe-out1"}, FamilyIPv6: {"pppoe-out1"},
		},
	})
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %#v", result.Blockers)
	}

	for _, menu := range []routeros.MutationMenu{routeros.MenuIPFirewallFilter, routeros.MenuIPv6FirewallFilter} {
		filters := collectByMenu(result, menu)
		if len(filters) != 6 {
			t.Fatalf("one egress must create six direct bidirectional deny rules on %s: %#v", menu, filters)
		}
		var tcp, udp, other, out, in bool
		for _, object := range filters {
			if object.Fields["action"] == "accept" {
				t.Fatalf("access control must never accept traffic: %#v", object)
			}
			if object.Fields["chain"] != "forward" || object.Fields["action"] == "jump" || object.Fields["action"] == "return" || object.Fields["jump-target"] != "" {
				t.Fatalf("internet scope must use direct forward rules: %#v", object)
			}
			if object.Fields["out-interface"] != "" {
				out = true
				if object.Fields["src-address-list"] != RuleMemberListName("manager", "router-a", "rule-a") || object.Fields["out-interface"] != "pppoe-out1" {
					t.Fatalf("invalid direct outbound matcher: %#v", object)
				}
			}
			if object.Fields["in-interface"] != "" {
				in = true
				if object.Fields["dst-address-list"] != RuleMemberListName("manager", "router-a", "rule-a") || object.Fields["in-interface"] != "pppoe-out1" {
					t.Fatalf("invalid direct inbound matcher: %#v", object)
				}
			}
			switch {
			case object.Fields["protocol"] == "tcp" && object.Fields["action"] == "reject" && object.Fields["reject-with"] == "tcp-reset":
				tcp = true
			case object.Fields["protocol"] == "udp" && object.Fields["action"] == "drop":
				udp = true
			case object.Fields["protocol"] == "" && object.Fields["action"] == "drop":
				other = true
			}
		}
		if !tcp || !udp || !other || !out || !in {
			t.Fatalf("internet scope must reset TCP and drop UDP/other in both directions on %s: %#v", menu, filters)
		}
	}

	localObjects := 0
	for _, object := range collectByMenu(result, routeros.MenuIPFirewallAddressList) {
		if strings.HasPrefix(object.LogicalID, "access-local:") {
			localObjects++
		}
	}
	if localObjects != 0 {
		t.Fatalf("direct egress rules must not materialize synthetic local-prefix lists: %d", localObjects)
	}
}

func TestBuildDesiredInternetScopeExpandsEveryEgress(t *testing.T) {
	result := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a", Rules: []AccessRule{internetRule("rule-a")},
		Members:          []RuleMember{fixedMember("rule-a", "t1", "10.0.0.20")},
		InternetEgresses: map[string][]string{FamilyIPv4: {"pppoe-out2", "pppoe-out1", "pppoe-out1"}},
	})
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %#v", result.Blockers)
	}
	filters := collectByMenu(result, routeros.MenuIPFirewallFilter)
	if len(filters) != 6 {
		t.Fatalf("two unique egress interfaces must create six interface-list rules, got %d: %#v", len(filters), filters)
	}
	interfaces := map[string]bool{}
	for _, object := range filters {
		if object.Fields["out-interface-list"] != InternetEgressListName("manager", "router-a", FamilyIPv4) && object.Fields["in-interface-list"] != InternetEgressListName("manager", "router-a", FamilyIPv4) {
			t.Fatalf("multiple egress filters must reference one interface list: %#v", object)
		}
	}
	for _, object := range collectByMenu(result, routeros.MenuInterfaceListMember) {
		interfaces[object.Fields["interface"]] = true
	}
	if !interfaces["pppoe-out1"] || !interfaces["pppoe-out2"] {
		t.Fatalf("the interface list must contain all deduplicated egress interfaces: %#v", interfaces)
	}
}

func TestBuildDesiredInternetScopeBlocksWithoutEgress(t *testing.T) {
	result := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules:     []AccessRule{internetRule("rule-a")},
		Members:   []RuleMember{autoMember("rule-a", "mac:aa")},
		Terminals: []Terminal{{ID: "mac:aa", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"10.0.0.20"}}},
	})
	found := false
	for _, blocker := range result.Blockers {
		if blocker.Code == "access_internet_egress_unavailable" && strings.Contains(blocker.Reason, "实际互联网出口接口") {
			found = true
		}
	}
	if !found {
		t.Fatalf("internet rule without a proven egress must be blocked, got: %#v", result.Blockers)
	}
	for _, object := range collectByMenu(result, routeros.MenuIPFirewallFilter) {
		if object.Fields["action"] == "drop" || object.Fields["action"] == "reject" {
			t.Fatalf("missing egress must not produce direct deny rules: %#v", object)
		}
	}
}

func TestBuildDesiredProjectsAllNonLinkLocalIPv6Addresses(t *testing.T) {
	result := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a", Rules: []AccessRule{internetRule("rule-a")},
		Members:          []RuleMember{autoMember("rule-a", "mac:aa")},
		Terminals:        []Terminal{{ID: "mac:aa", MACAddress: "AA:BB:CC:DD:EE:FF", IPv6: []string{"fc00::20", "fc00::be24:11ff:fe64:333a", "fe80::20"}}},
		InternetEgresses: map[string][]string{FamilyIPv6: {"pppoe-out1"}},
	})
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %#v", result.Blockers)
	}
	addresses := make([]string, 0)
	for _, object := range collectByMenu(result, routeros.MenuIPv6FirewallAddressList) {
		if strings.HasPrefix(object.LogicalID, "access-member:") {
			addresses = append(addresses, object.Fields["address"])
		}
	}
	if strings.Join(addresses, ",") != "fc00::20,fc00::be24:11ff:fe64:333a" {
		t.Fatalf("all usable IPv6 addresses must be projected while link-local is ignored: %#v", addresses)
	}
}

func TestBuildDesiredTemporarilyUnresolvedMemberKeepsTrustedProjectionWithoutBlocking(t *testing.T) {
	online := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules:      []AccessRule{sourcesRule("rule-a", "source-a")},
		Members:    []RuleMember{autoMember("rule-a", "mac:aa")},
		Terminals:  []Terminal{{ID: "mac:aa", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"10.0.0.20"}}},
		SourceList: map[string]string{"source-a": "rb_src_a"},
	})
	if len(online.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %#v", online.Blockers)
	}
	if len(online.Resolutions) != 1 || len(online.Resolutions[0].IPv4) != 1 || online.Resolutions[0].IPv4[0] != "10.0.0.20" {
		t.Fatalf("resolved auto-follow member must carry a deferred trusted-resolution update: %#v", online.Resolutions)
	}
	stable := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules:      []AccessRule{sourcesRule("rule-a", "source-a")},
		Members:    []RuleMember{{RuleID: "rule-a", TerminalID: "mac:aa", Binding: BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.20"}}},
		Terminals:  []Terminal{{ID: "mac:aa", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"10.0.0.20"}}},
		SourceList: map[string]string{"source-a": "rb_src_a"},
	})
	if len(stable.Resolutions) != 0 {
		t.Fatalf("unchanged trusted projection must not request another resolution write: %#v", stable.Resolutions)
	}
	offline := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules:      []AccessRule{sourcesRule("rule-a", "source-a")},
		Members:    []RuleMember{{RuleID: "rule-a", TerminalID: "mac:aa", Binding: BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.20"}}},
		Terminals:  []Terminal{},
		SourceList: map[string]string{"source-a": "rb_src_a"},
	})
	if len(offline.Blockers) != 0 {
		t.Fatalf("temporarily unresolved member must not block the device: %#v", offline.Blockers)
	}
	degraded := false
	for _, issue := range offline.Issues {
		if issue.Code == "access_member_temporarily_unresolved" {
			degraded = true
		}
	}
	if !degraded {
		t.Fatalf("expected a member-local degraded issue: %#v", offline.Issues)
	}
	projected := 0
	for _, object := range offline.Objects {
		if object.Menu == routeros.MenuIPFirewallAddressList && object.Fields["address"] == "10.0.0.20" {
			projected++
		}
	}
	if projected != 1 {
		t.Fatalf("last trusted address must keep being projected while the terminal is unseen: %#v", offline.Objects)
	}
	if len(offline.Resolutions) != 0 {
		t.Fatalf("unresolved members must not refresh their trusted projection: %#v", offline.Resolutions)
	}
}

func TestBuildDesiredRemovesReassignedAddressProjection(t *testing.T) {
	result := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules: []AccessRule{sourcesRule("rule-a", "source-a"), sourcesRule("rule-b", "source-a")},
		Members: []RuleMember{
			RuleMember{RuleID: "rule-a", TerminalID: "mac:aa", Binding: BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.20"}},
			fixedMember("rule-b", "mac:bb", "10.0.0.21"),
		},
		// 10.0.0.20 now demonstrably belongs to another terminal identity.
		Terminals:  []Terminal{{ID: "mac:cc", MACAddress: "CC:CC:CC:CC:CC:CC", IPv4: []string{"10.0.0.20"}}},
		SourceList: map[string]string{"source-a": "rb_src_a"},
	})
	if len(result.Blockers) != 0 {
		t.Fatalf("reassignment on one member must not block the device: %#v", result.Blockers)
	}
	conflicted := false
	for _, issue := range result.Issues {
		if issue.Code == "access_member_conflicted" && strings.Contains(issue.Reason, "10.0.0.20") {
			conflicted = true
		}
	}
	if !conflicted {
		t.Fatalf("expected a conflicted member issue naming the lost address: %#v", result.Issues)
	}
	if len(result.Resolutions) != 1 || len(result.Resolutions[0].IPv4) != 0 {
		t.Fatalf("conflict resolution must clear the old trusted address after apply: %#v", result.Resolutions)
	}
	for _, object := range result.Objects {
		if object.Fields["address"] == "10.0.0.20" {
			t.Fatalf("reassigned address must not be projected anymore: %#v", object)
		}
	}
	// rule-b (fixed member) keeps applying even though rule-a degraded.
	ruleBJumps := 0
	for _, object := range result.Objects {
		if strings.Contains(object.LogicalID, "access:rule-b:ipv4:jump-out") {
			ruleBJumps++
		}
	}
	if ruleBJumps != 1 {
		t.Fatalf("other rules must continue to apply: %#v", result.Objects)
	}
}

func TestManagedCommentUsesShortRouterOSIdentity(t *testing.T) {
	comment := ManagedComment("manager", "router-a", "access:rule-a:ipv4:jump-out", "访问控制出站入口")
	identity := strings.SplitN(comment, " | ", 2)[0]
	if !strings.HasPrefix(identity, "rb_") || !hasHex(strings.TrimPrefix(identity, "rb_"), 8) {
		t.Fatalf("unexpected RouterOS comment identity: %q", comment)
	}
	if !IsManagedComment(comment) || strings.Contains(identity, "ra_") {
		t.Fatalf("short comment was not recognized as managed: %q", comment)
	}
}

func TestBuildDesiredStopsOnUnavailableSource(t *testing.T) {
	result := BuildDesired(DesiredInput{
		ManagerID: "manager", DeviceID: "router-a",
		Rules:   []AccessRule{sourcesRule("rule-a", "source-missing")},
		Members: []RuleMember{fixedMember("rule-a", "t1", "10.0.0.20")},
	})
	found := false
	for _, blocker := range result.Blockers {
		if blocker.Code == "access_source_unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unavailable source must block: %#v", result.Blockers)
	}
	if len(result.Objects) != 0 {
		t.Fatalf("blocked rule must not emit partial objects: %#v", result.Objects)
	}
}
