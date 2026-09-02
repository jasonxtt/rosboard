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
