package policyv2

import (
	"bytes"
	"testing"
)

func TestClonePolicyProposalPreservesProposedTargetContent(t *testing.T) {
	proposal := &PolicyProposal{
		TargetLists: []ProposedTargetList{{
			Target: TargetList{ID: "target-a"},
			Version: TargetListVersion{
				ID:             "version-a",
				TargetListID:   "target-a",
				SHA256:         "sha-a",
				CompressedYAML: []byte("compressed-content"),
			},
			Rules: []TargetListRule{{VersionID: "version-a", RuleType: "DOMAIN", Domain: "example.com"}},
		}},
	}

	clone := clonePolicyProposal(proposal)
	if clone == nil || len(clone.TargetLists) != 1 {
		t.Fatalf("proposal was not cloned: %#v", clone)
	}
	if !bytes.Equal(clone.TargetLists[0].Version.CompressedYAML, proposal.TargetLists[0].Version.CompressedYAML) {
		t.Fatalf("proposed target content was dropped during clone: %#v", clone.TargetLists[0].Version)
	}
	if len(clone.TargetLists[0].Rules) != 1 || clone.TargetLists[0].Rules[0].VersionID != "version-a" {
		t.Fatalf("proposed target rule version was dropped during clone: %#v", clone.TargetLists[0].Rules)
	}
	clone.TargetLists[0].Version.CompressedYAML[0] = 'C'
	if bytes.Equal(clone.TargetLists[0].Version.CompressedYAML, proposal.TargetLists[0].Version.CompressedYAML) {
		t.Fatal("cloned target content shares storage with the original proposal")
	}
}

func TestClonePolicyProposalPreservesRoutingSubjectIdentityState(t *testing.T) {
	proposal := &PolicyProposal{
		RoutingRule: &RoutingRule{
			Subject: Subject{
				Mode: SubjectModeSelected,
				Members: []SubjectMember{{
					TerminalID: "mac:aa",
					Binding:    "auto",
					AnchorMAC:  "AA:BB:CC:DD:EE:FF",
					LastIPv4:   []string{"10.0.0.30"},
					LastIPv6:   []string{"2001:db8::30"},
				}},
			},
		},
	}

	clone := clonePolicyProposal(proposal)
	if clone == nil || clone.RoutingRule == nil || len(clone.RoutingRule.Subject.Members) != 1 {
		t.Fatalf("routing subject was not cloned: %#v", clone)
	}
	member := clone.RoutingRule.Subject.Members[0]
	if member.AnchorMAC != "AA:BB:CC:DD:EE:FF" || len(member.LastIPv4) != 1 || member.LastIPv4[0] != "10.0.0.30" || len(member.LastIPv6) != 1 || member.LastIPv6[0] != "2001:db8::30" {
		t.Fatalf("routing subject identity state was dropped during clone: %#v", member)
	}
	member.LastIPv4[0] = "10.0.0.31"
	if proposal.RoutingRule.Subject.Members[0].LastIPv4[0] == member.LastIPv4[0] {
		t.Fatal("cloned routing subject state shares storage with the original proposal")
	}
}
