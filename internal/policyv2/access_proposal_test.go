package policyv2

import (
	"testing"

	"rosboard/internal/accesscontrol"
)

func TestReplaceAccessProposalMembersKeepsTrustedAutoResolution(t *testing.T) {
	current := []accesscontrol.RuleMember{{
		RuleID: "rule-a", TerminalID: "terminal-a", Binding: accesscontrol.BindingAuto,
		AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.20"}, LastIPv6: []string{"2001:db8::20"},
	}}
	replacement := []accesscontrol.RuleMember{{
		RuleID: "rule-a", TerminalID: "terminal-a", Binding: accesscontrol.BindingAuto,
		AnchorMAC: "aa-bb-cc-dd-ee-ff",
	}}

	got := replaceAccessProposalMembers(current, "rule-a", replacement)
	if len(got) != 1 || len(got[0].LastIPv4) != 1 || got[0].LastIPv4[0] != "10.0.0.20" || len(got[0].LastIPv6) != 1 || got[0].LastIPv6[0] != "2001:db8::20" {
		t.Fatalf("proposal overlay lost the trusted auto-follow resolution: %#v", got)
	}
}
