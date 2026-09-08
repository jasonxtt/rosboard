package accesscontrol

import "testing"

func TestEvaluateMembersStates(t *testing.T) {
	terminals := []Terminal{
		{ID: "mac:aa", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"10.0.0.20"}},
		{ID: "mac:bb", MACAddress: "BA:BB:BB:BB:BB:BB", IPv4: []string{"10.0.0.21", "10.0.0.20"}},
	}

	evaluations := EvaluateMembers([]RuleMember{autoMember("rule-a", "mac:aa")}, terminals)
	if len(evaluations) != 1 || evaluations[0].State != MemberConflicted {
		t.Fatalf("address shared with another identity must be conflicted: %#v", evaluations)
	}
	if len(evaluations[0].IPv4) != 0 || len(evaluations[0].RemovedIPv4) != 1 {
		t.Fatalf("conflicted address must be withheld, not projected: %#v", evaluations[0])
	}

	evaluations = EvaluateMembers([]RuleMember{RuleMember{
		RuleID: "rule-a", TerminalID: "mac:cc", Binding: BindingAuto, AnchorMAC: "CC:CC:CC:CC:CC:CC", LastIPv4: []string{"10.0.0.30"},
	}}, terminals)
	if len(evaluations) != 1 || evaluations[0].State != MemberUnresolved {
		t.Fatalf("unseen terminal with trusted history must be temporarily unresolved: %#v", evaluations)
	}
	if len(evaluations[0].IPv4) != 1 || evaluations[0].IPv4[0] != "10.0.0.30" {
		t.Fatalf("last trusted address must be kept: %#v", evaluations[0])
	}

	evaluations = EvaluateMembers([]RuleMember{RuleMember{
		RuleID: "rule-a", TerminalID: "mac:cc", Binding: BindingAuto,
	}}, terminals)
	if len(evaluations) != 1 || evaluations[0].State != MemberUnresolved || len(evaluations[0].IPv4) != 0 {
		t.Fatalf("never-resolved member must project nothing: %#v", evaluations)
	}

	evaluations = EvaluateMembers([]RuleMember{fixedMember("rule-a", "mac:aa", "10.0.0.20")}, nil)
	if len(evaluations) != 1 || evaluations[0].State != MemberResolved || len(evaluations[0].IPv4) != 1 {
		t.Fatalf("fixed members always resolve to their pinned addresses: %#v", evaluations)
	}

	evaluations = EvaluateMembers([]RuleMember{autoMember("rule-a", "mac:aa")}, []Terminal{
		{ID: "mac:aa", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"10.0.0.20", "10.0.0.21"}},
		{ID: "mac:bb", MACAddress: "BA:BB:BB:BB:BB:BB", IPv4: []string{"10.0.0.21"}},
	})
	if len(evaluations) != 1 || evaluations[0].State != MemberConflicted {
		t.Fatalf("member address observed on another MAC must degrade: %#v", evaluations)
	}
	if len(evaluations[0].IPv4) != 1 || evaluations[0].IPv4[0] != "10.0.0.20" {
		t.Fatalf("own exclusive address must be kept: %#v", evaluations[0])
	}
}

func TestEvaluateMembersNormalizesIPv6BeforeConflictLookup(t *testing.T) {
	member := RuleMember{RuleID: "rule-a", TerminalID: "mac:aa", Binding: BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv6: []string{"2001:db8::20"}}
	terminals := []Terminal{
		{ID: "mac:aa", MACAddress: "AA:BB:CC:DD:EE:FF", IPv6: []string{"2001:db8::20"}},
		{ID: "mac:bb", MACAddress: "BA:BB:BB:BB:BB:BB", IPv6: []string{"2001:0db8:0:0::20"}},
	}
	evaluations := EvaluateMembers([]RuleMember{member}, terminals)
	if len(evaluations) != 1 || evaluations[0].State != MemberConflicted {
		t.Fatalf("equivalent IPv6 spellings must conflict: %#v", evaluations)
	}
	if len(evaluations[0].IPv6) != 0 || len(evaluations[0].RemovedIPv6) != 1 || evaluations[0].RemovedIPv6[0] != "2001:db8::20" {
		t.Fatalf("conflicting canonical IPv6 address must be withheld: %#v", evaluations[0])
	}
}

func TestNormalizeMACRejectsNonUnicastAndZeroIdentities(t *testing.T) {
	for _, value := range []string{"01:00:5E:00:00:01", "00:00:00:00:00:00"} {
		if _, err := NormalizeMAC(value); err == nil {
			t.Fatalf("NormalizeMAC(%q) accepted an unreliable terminal identity", value)
		}
		if IsReliableMAC(value) {
			t.Fatalf("IsReliableMAC(%q) returned true for an unreliable terminal identity", value)
		}
	}
}
