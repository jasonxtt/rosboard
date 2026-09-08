package policyv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/subject"
)

// AccessProposal keeps an access rule and any newly selected preset backing
// rows together until the access plan is approved. It is intentionally
// separate from PolicyProposal because it advances access_control_state, not
// policy_v2_device_state.
type AccessProposal struct {
	Rule                accesscontrol.AccessRule   `json:"rule"`
	Members             []accesscontrol.RuleMember `json:"members,omitempty"`
	TargetLists         []ProposedTargetList       `json:"targetLists,omitempty"`
	TargetListRevisions map[string]int64           `json:"targetListRevisions,omitempty"`
}

type AccessProposalCommitter interface {
	CommitAccessProposal(context.Context, AccessProposal, int64, string) (int64, error)
}

func (proposal AccessProposal) Empty() bool {
	return strings.TrimSpace(proposal.Rule.ID) == "" && len(proposal.TargetLists) == 0
}

func AccessProposalHash(proposal AccessProposal) (string, error) {
	type memberHash struct {
		RuleID     string   `json:"ruleId"`
		TerminalID string   `json:"terminalId"`
		Binding    string   `json:"binding"`
		AnchorMAC  string   `json:"anchorMac"`
		PinnedIPv4 []string `json:"pinnedIpv4"`
		PinnedIPv6 []string `json:"pinnedIpv6"`
		LastIPv4   []string `json:"lastIpv4"`
		LastIPv6   []string `json:"lastIpv6"`
	}
	members := make([]memberHash, 0, len(proposal.Members))
	for _, member := range proposal.Members {
		members = append(members, memberHash{
			RuleID: member.RuleID, TerminalID: member.TerminalID, Binding: member.Binding, AnchorMAC: member.AnchorMAC,
			PinnedIPv4: member.PinnedIPv4, PinnedIPv6: member.PinnedIPv6, LastIPv4: member.LastIPv4, LastIPv6: member.LastIPv6,
		})
	}
	payload, err := json.Marshal(struct {
		Rule                accesscontrol.AccessRule `json:"rule"`
		Members             []memberHash             `json:"members,omitempty"`
		TargetLists         []ProposedTargetList     `json:"targetLists,omitempty"`
		TargetListRevisions map[string]int64         `json:"targetListRevisions,omitempty"`
	}{proposal.Rule, members, proposal.TargetLists, proposal.TargetListRevisions})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func cloneAccessProposal(proposal *AccessProposal) *AccessProposal {
	if proposal == nil {
		return nil
	}
	clone := *proposal
	clone.Rule = proposal.Rule
	clone.Rule.Subject = proposal.Rule.Subject
	clone.Rule.Subject.Members = append([]subject.Member(nil), proposal.Rule.Subject.Members...)
	clone.Rule.Subject.Prefixes = append([]string(nil), proposal.Rule.Subject.Prefixes...)
	clone.Members = make([]accesscontrol.RuleMember, len(proposal.Members))
	copy(clone.Members, proposal.Members)
	for index := range clone.Members {
		clone.Members[index].PinnedIPv4 = append([]string(nil), proposal.Members[index].PinnedIPv4...)
		clone.Members[index].PinnedIPv6 = append([]string(nil), proposal.Members[index].PinnedIPv6...)
		clone.Members[index].LastIPv4 = append([]string(nil), proposal.Members[index].LastIPv4...)
		clone.Members[index].LastIPv6 = append([]string(nil), proposal.Members[index].LastIPv6...)
	}
	clone.TargetListRevisions = make(map[string]int64, len(proposal.TargetListRevisions))
	for id, revision := range proposal.TargetListRevisions {
		clone.TargetListRevisions[id] = revision
	}
	clone.TargetLists = make([]ProposedTargetList, len(proposal.TargetLists))
	copy(clone.TargetLists, proposal.TargetLists)
	for index := range clone.TargetLists {
		clone.TargetLists[index].Target = proposal.TargetLists[index].Target
		clone.TargetLists[index].Target.Versions = append([]TargetListVersion(nil), proposal.TargetLists[index].Target.Versions...)
		clone.TargetLists[index].Target.Counts = cloneStringIntMap(proposal.TargetLists[index].Target.Counts)
		clone.TargetLists[index].Version = proposal.TargetLists[index].Version
		clone.TargetLists[index].Version.CompressedYAML = append([]byte(nil), proposal.TargetLists[index].Version.CompressedYAML...)
		clone.TargetLists[index].Version.Counts = cloneStringIntMap(proposal.TargetLists[index].Version.Counts)
		clone.TargetLists[index].Version.Diff = cloneAnyMap(proposal.TargetLists[index].Version.Diff)
		clone.TargetLists[index].Rules = append([]TargetListRule(nil), proposal.TargetLists[index].Rules...)
	}
	return &clone
}

func cloneStringIntMap(value map[string]int) map[string]int {
	if value == nil {
		return nil
	}
	result := make(map[string]int, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func cloneAnyMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func CaptureAccessProposalDependencies(ctx context.Context, repository Repository, proposal *AccessProposal) error {
	if proposal == nil {
		return nil
	}
	if proposal.TargetListRevisions == nil {
		proposal.TargetListRevisions = make(map[string]int64)
	}
	for _, target := range proposal.TargetLists {
		id := strings.TrimSpace(target.Target.ID)
		if id == "" {
			continue
		}
		if _, ok := proposal.TargetListRevisions[id]; ok {
			continue
		}
		current, err := repository.GetSource(ctx, id)
		if errors.Is(err, ErrSourceNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		proposal.TargetListRevisions[id] = current.Revision
	}
	for _, targetID := range proposal.Rule.TargetListIDs {
		id := strings.TrimSpace(targetID)
		if id == "" {
			continue
		}
		if _, ok := proposal.TargetListRevisions[id]; ok {
			continue
		}
		current, err := repository.GetSource(ctx, id)
		if errors.Is(err, ErrSourceNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		proposal.TargetListRevisions[id] = current.Revision
	}
	return nil
}

func ValidateAccessProposalDependencies(ctx context.Context, repository Repository, proposal AccessProposal) error {
	for id, expectedRevision := range proposal.TargetListRevisions {
		current, err := repository.GetSource(ctx, id)
		if err != nil || current.Revision != expectedRevision {
			return ErrRevisionStale
		}
	}
	return nil
}

type accessProposalRepository struct {
	accesscontrol.Repository
	rules   []accesscontrol.AccessRule
	members []accesscontrol.RuleMember
	state   accesscontrol.State
}

func newAccessProposalRepository(ctx context.Context, repository accesscontrol.Repository, proposal AccessProposal) (*accessProposalRepository, error) {
	if repository == nil {
		return nil, errors.New("access repository is unavailable")
	}
	rules, err := repository.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	members, err := repository.ListMembers(ctx)
	if err != nil {
		return nil, err
	}
	state, err := repository.GetState(ctx)
	if err != nil {
		return nil, err
	}
	if proposal.Rule.ID != "" {
		members = replaceAccessProposalMembers(members, proposal.Rule.ID, proposal.Members)
		proposal.Rule.Subject.Members = make([]subject.Member, 0, len(proposal.Members))
		for _, member := range proposal.Members {
			proposal.Rule.Subject.Members = append(proposal.Rule.Subject.Members, subject.Member{
				TerminalID: member.TerminalID, Binding: member.Binding, AnchorMAC: member.AnchorMAC,
				PinnedIPv4: append([]string(nil), member.PinnedIPv4...), PinnedIPv6: append([]string(nil), member.PinnedIPv6...),
				LastIPv4: append([]string(nil), member.LastIPv4...), LastIPv6: append([]string(nil), member.LastIPv6...),
			})
		}
		found := false
		for index := range rules {
			if rules[index].ID == proposal.Rule.ID {
				rules[index] = proposal.Rule
				found = true
				break
			}
		}
		if !found {
			rules = append(rules, proposal.Rule)
		}
	}
	return &accessProposalRepository{Repository: repository, rules: rules, members: members, state: state}, nil
}

func replaceAccessProposalMembers(members []accesscontrol.RuleMember, ruleID string, replacement []accesscontrol.RuleMember) []accesscontrol.RuleMember {
	existingByTerminal := make(map[string]accesscontrol.RuleMember)
	for _, member := range members {
		if member.RuleID == ruleID {
			existingByTerminal[member.TerminalID] = member
		}
	}
	result := make([]accesscontrol.RuleMember, 0, len(members)+len(replacement))
	for _, member := range members {
		if member.RuleID != ruleID {
			result = append(result, member)
		}
	}
	for _, member := range replacement {
		member.RuleID = ruleID
		previous, ok := existingByTerminal[member.TerminalID]
		if ok && previous.Binding == accesscontrol.BindingAuto && member.Binding == accesscontrol.BindingAuto {
			previousAnchor, previousErr := accesscontrol.NormalizeMAC(previous.AnchorMAC)
			memberAnchor, memberErr := accesscontrol.NormalizeMAC(member.AnchorMAC)
			if previousErr == nil && memberErr == nil && previousAnchor != "" && previousAnchor == memberAnchor {
				// Keep the last trusted resolution in the preview overlay. The
				// atomic commit preserves it too; without this, an edit of an
				// auto-follow rule hashes a resolution in preview but not after
				// commit and is reported as a false stale plan.
				member.LastIPv4 = append([]string(nil), previous.LastIPv4...)
				member.LastIPv6 = append([]string(nil), previous.LastIPv6...)
			}
		}
		result = append(result, member)
	}
	return result
}

func (r *accessProposalRepository) ListRules(context.Context) ([]accesscontrol.AccessRule, error) {
	return append([]accesscontrol.AccessRule(nil), r.rules...), nil
}

func (r *accessProposalRepository) ListMembers(context.Context) ([]accesscontrol.RuleMember, error) {
	return append([]accesscontrol.RuleMember(nil), r.members...), nil
}

func (r *accessProposalRepository) GetState(context.Context) (accesscontrol.State, error) {
	return r.state, nil
}
