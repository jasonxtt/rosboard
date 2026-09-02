package policyv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// ProposedTargetList carries the source content needed to project a newly
// selected application target without writing its backing row before apply.
// The row and its pending version are committed together with the rest of the
// policy proposal.
type ProposedTargetList struct {
	Target  TargetList        `json:"target"`
	Version TargetListVersion `json:"version"`
	Rules   []TargetListRule  `json:"rules"`
}

// PolicyProposal is the small mutation bundle used by the policy wizard. It
// is intentionally not a general transaction framework: it is an in-memory
// proposed graph used to build a reviewable plan, then committed atomically
// only after that exact plan is approved.
type PolicyProposal struct {
	Egress              *Egress              `json:"egress,omitempty"`
	TrafficIngress      *TrafficIngressScope `json:"trafficIngress,omitempty"`
	RoutingRule         *RoutingRule         `json:"routingRule,omitempty"`
	TargetLists         []ProposedTargetList `json:"targetLists,omitempty"`
	EgressRevisions     map[string]int64     `json:"egressRevisions,omitempty"`
	TargetListRevisions map[string]int64     `json:"targetListRevisions,omitempty"`
}

// ProposalCommitter is implemented by the durable policy repository. The
// expected revision is checked in the same transaction as every proposal
// mutation so a source refresh or another editor cannot interleave with the
// approval.
type ProposalCommitter interface {
	CommitPolicyProposal(context.Context, PolicyProposal, int64) (int64, error)
}

func (proposal PolicyProposal) Empty() bool {
	return proposal.Egress == nil && proposal.TrafficIngress == nil && proposal.RoutingRule == nil && len(proposal.TargetLists) == 0
}

func ProposalHash(proposal PolicyProposal) (string, error) {
	payload, err := json.Marshal(proposal)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// CaptureProposalDependencies snapshots the revisions of existing objects a
// proposal reads but does not replace. New proposal rows are checked by their
// zero revision during the atomic commit instead.
func CaptureProposalDependencies(ctx context.Context, repository Repository, proposal *PolicyProposal) error {
	if proposal == nil {
		return nil
	}
	if proposal.EgressRevisions == nil {
		proposal.EgressRevisions = make(map[string]int64)
	}
	if proposal.TargetListRevisions == nil {
		proposal.TargetListRevisions = make(map[string]int64)
	}
	addEgress := func(id string) error {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil
		}
		if _, alreadyCaptured := proposal.EgressRevisions[id]; alreadyCaptured {
			return nil
		}
		egress, err := repository.GetEgress(ctx, id)
		if errors.Is(err, ErrEgressNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		proposal.EgressRevisions[id] = egress.Revision
		return nil
	}
	addTarget := func(id string) error {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil
		}
		if _, alreadyCaptured := proposal.TargetListRevisions[id]; alreadyCaptured {
			return nil
		}
		source, err := repository.GetSource(ctx, id)
		if errors.Is(err, ErrSourceNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		proposal.TargetListRevisions[id] = source.Revision
		return nil
	}
	if proposal.Egress != nil {
		if err := addEgress(proposal.Egress.ID); err != nil {
			return err
		}
	}
	if proposal.RoutingRule != nil {
		if err := addEgress(proposal.RoutingRule.EgressID); err != nil {
			return err
		}
		for _, targetID := range proposal.RoutingRule.TargetListIDs {
			if err := addTarget(targetID); err != nil {
				return err
			}
		}
	}
	for _, target := range proposal.TargetLists {
		if err := addTarget(target.Target.ID); err != nil {
			return err
		}
	}
	return nil
}

// ValidateProposalDependencies rejects a preview whose referenced canonical
// egress or target list changed while it was awaiting approval.
func ValidateProposalDependencies(ctx context.Context, repository Repository, proposal PolicyProposal) error {
	for id, expectedRevision := range proposal.EgressRevisions {
		egress, err := repository.GetEgress(ctx, id)
		if err != nil || egress.Revision != expectedRevision {
			return ErrRevisionStale
		}
	}
	for id, expectedRevision := range proposal.TargetListRevisions {
		source, err := repository.GetSource(ctx, id)
		if err != nil || source.Revision != expectedRevision {
			return ErrRevisionStale
		}
	}
	return nil
}

func clonePolicyProposal(proposal *PolicyProposal) *PolicyProposal {
	if proposal == nil {
		return nil
	}
	payload, err := json.Marshal(proposal)
	if err != nil {
		return nil
	}
	var clone PolicyProposal
	if err := json.Unmarshal(payload, &clone); err != nil {
		return nil
	}
	for index := range clone.TargetLists {
		clone.TargetLists[index].Version.CompressedYAML = append([]byte(nil), proposal.TargetLists[index].Version.CompressedYAML...)
		clone.TargetLists[index].Rules = append([]TargetListRule(nil), proposal.TargetLists[index].Rules...)
	}
	return &clone
}

// proposalRepository overlays only the graph-bearing reads used by desired
// state construction. All other reads and every write remain on Repository.
// In particular, it never materializes an application target or increments a
// revision while a preview is being generated.
type proposalRepository struct {
	Repository
	proposal       PolicyProposal
	egresses       []Egress
	sources        []Source
	routingRules   []RoutingRule
	routingEnabled bool
	authority      string
}

func newProposalRepository(ctx context.Context, repository Repository, proposal PolicyProposal) (*proposalRepository, error) {
	result := &proposalRepository{Repository: repository, proposal: proposal}
	var err error
	result.egresses, err = repository.ListEgresses(ctx)
	if err != nil {
		return nil, err
	}
	if proposal.Egress != nil {
		result.egresses = replaceEgress(result.egresses, *proposal.Egress)
	}
	result.sources, err = repository.ListSources(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, proposed := range proposal.TargetLists {
		source := proposed.Target.ToSource()
		source.PendingVersionID = proposed.Version.ID
		source.Versions = appendWithoutVersion(source.Versions, proposed.Version.ToSource())
		source.Counts = map[string]int{"valid": len(proposed.Rules)}
		result.sources = replaceSource(result.sources, source)
	}

	if routingRepository, ok := repository.(RoutingRuleRepository); ok {
		result.authority, err = routingRepository.RoutingAuthority(ctx)
		if err != nil {
			return nil, err
		}
		result.routingEnabled = result.authority == RoutingRuleAuthorityV1
		if result.routingEnabled {
			result.routingRules, err = routingRepository.ListRoutingRules(ctx)
			if err != nil {
				return nil, err
			}
		}
	}
	if proposal.RoutingRule != nil {
		result.routingEnabled = true
		result.authority = RoutingRuleAuthorityV1
		result.routingRules = replaceRoutingRule(result.routingRules, *proposal.RoutingRule)
	}
	return result, nil
}

func replaceEgress(egresses []Egress, replacement Egress) []Egress {
	for index := range egresses {
		if egresses[index].ID == replacement.ID {
			egresses[index] = replacement
			return egresses
		}
	}
	return append(egresses, replacement)
}

func replaceSource(sources []Source, replacement Source) []Source {
	for index := range sources {
		if sources[index].ID == replacement.ID {
			sources[index] = replacement
			return sources
		}
	}
	return append(sources, replacement)
}

func replaceRoutingRule(rules []RoutingRule, replacement RoutingRule) []RoutingRule {
	for index := range rules {
		if rules[index].ID == replacement.ID {
			rules[index] = replacement
			return rules
		}
	}
	return append(rules, replacement)
}

func appendWithoutVersion(versions []SourceVersion, replacement SourceVersion) []SourceVersion {
	for index := range versions {
		if versions[index].ID == replacement.ID {
			versions[index] = replacement
			return versions
		}
	}
	return append(versions, replacement)
}

func (r *proposalRepository) ListEgresses(context.Context) ([]Egress, error) {
	return append([]Egress(nil), r.egresses...), nil
}

func (r *proposalRepository) GetEgress(_ context.Context, id string) (Egress, error) {
	for _, egress := range r.egresses {
		if egress.ID == id {
			return egress, nil
		}
	}
	return Egress{}, ErrEgressNotFound
}

func (r *proposalRepository) ListSources(_ context.Context, egressID string) ([]Source, error) {
	result := make([]Source, 0, len(r.sources))
	for _, source := range r.sources {
		if egressID == "" || source.EgressID == egressID {
			result = append(result, source)
		}
	}
	return result, nil
}

func (r *proposalRepository) GetSource(_ context.Context, id string) (Source, error) {
	for _, source := range r.sources {
		if source.ID == id {
			return source, nil
		}
	}
	return Source{}, ErrSourceNotFound
}

func (r *proposalRepository) GetDeviceState(ctx context.Context) (DeviceState, error) {
	state, err := r.Repository.GetDeviceState(ctx)
	if err != nil || r.proposal.TrafficIngress == nil {
		return state, err
	}
	payload, err := MarshalTrafficIngressScope(*r.proposal.TrafficIngress)
	if err != nil {
		return DeviceState{}, err
	}
	state.TrafficIngress = payload
	return state, nil
}

func (r *proposalRepository) EnsureRoutingRulesMigrated(context.Context) error {
	// The real migration can write rows and bump desired_revision. A proposal
	// preview must remain read-only, so migration is deliberately not invoked
	// on this overlay. Normal non-proposal plans retain the existing migration
	// path.
	return nil
}

func (r *proposalRepository) RoutingAuthority(ctx context.Context) (string, error) {
	if r.routingEnabled {
		return RoutingRuleAuthorityV1, nil
	}
	if r.authority != "" {
		return r.authority, nil
	}
	if repository, ok := r.Repository.(RoutingRuleRepository); ok {
		return repository.RoutingAuthority(ctx)
	}
	return "", nil
}

func (r *proposalRepository) ListRoutingRules(context.Context) ([]RoutingRule, error) {
	if !r.routingEnabled {
		return []RoutingRule{}, nil
	}
	return append([]RoutingRule(nil), r.routingRules...), nil
}

func (r *proposalRepository) GetRoutingRule(ctx context.Context, id string) (RoutingRule, error) {
	for _, rule := range r.routingRules {
		if rule.ID == id {
			return rule, nil
		}
	}
	if repository, ok := r.Repository.(RoutingRuleRepository); ok {
		return repository.GetRoutingRule(ctx, id)
	}
	return RoutingRule{}, ErrRoutingRuleNotFound
}

func (r *proposalRepository) SaveRoutingRule(context.Context, RoutingRule) (RoutingRule, error) {
	return RoutingRule{}, errors.New("proposal repository is read-only")
}

func (r *proposalRepository) DeleteRoutingRule(context.Context, string, int64) error {
	return errors.New("proposal repository is read-only")
}

func (r *proposalRepository) ListSourceVersions(ctx context.Context, sourceID string) ([]SourceVersion, error) {
	source, err := r.GetSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return append([]SourceVersion(nil), source.Versions...), nil
}

func (r *proposalRepository) ListSourceRules(ctx context.Context, versionID string, query RuleQuery) ([]SourceRule, bool, error) {
	var rules []SourceRule
	proposedVersion := false
	for _, source := range r.sources {
		for _, version := range source.Versions {
			if version.ID != versionID {
				continue
			}
			for _, proposed := range r.proposal.TargetLists {
				if proposed.Target.ID != source.ID || proposed.Version.ID != versionID {
					continue
				}
				proposedVersion = true
				for _, rule := range proposed.Rules {
					rules = append(rules, rule.ToSource())
				}
			}
		}
	}
	if !proposedVersion {
		if repository, ok := r.Repository.(interface {
			ListSourceRules(context.Context, string, RuleQuery) ([]SourceRule, bool, error)
		}); ok {
			return repository.ListSourceRules(ctx, versionID, query)
		}
		return nil, false, nil
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].RuleType != rules[j].RuleType {
			return rules[i].RuleType < rules[j].RuleType
		}
		return rules[i].Domain < rules[j].Domain
	})
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	filtered := rules[:0]
	for _, rule := range rules {
		if query.RuleType != "" && rule.RuleType != query.RuleType {
			continue
		}
		if query.Query != "" && !strings.Contains(rule.Domain, query.Query) {
			continue
		}
		if query.AfterType != "" && (rule.RuleType < query.AfterType || rule.RuleType == query.AfterType && rule.Domain <= query.AfterDomain) {
			continue
		}
		filtered = append(filtered, rule)
	}
	hasNext := len(filtered) > limit
	if hasNext {
		filtered = filtered[:limit]
	}
	return filtered, hasNext, nil
}

var _ Repository = (*proposalRepository)(nil)
var _ RoutingRuleRepository = (*proposalRepository)(nil)
