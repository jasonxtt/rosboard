package policyv2

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/subject"
)

const (
	RoutingRuleAuthorityKey = "routing_rules_authoritative"
	RoutingRuleAuthorityV1  = "v1"

	SubjectModeAll      = subject.ModeAll
	SubjectModeSelected = subject.ModeSelected
	SubjectModeExcluded = subject.ModeExcluded
)

var (
	ErrRoutingRuleNotFound            = errors.New("routing rule not found")
	ErrRoutingRuleInUse               = errors.New("routing rule still has references")
	ErrRoutingExcludedRequiresIngress = errors.New("excluded routing subjects require a valid traffic ingress")
)

// Subject is the shared client-subject core. RoutingRule and AccessRule keep
// their own persistence and policy semantics around this value.
type Subject = subject.Subject
type SubjectMember = subject.Member

// RoutingRule is the authoritative policy-routing relation between a subject,
// reusable target lists and one Egress.
type RoutingRule struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Subject       Subject             `json:"subject"`
	Ingress       TrafficIngressScope `json:"ingress"`
	TargetListIDs []string            `json:"targetListIds"`
	EgressID      string              `json:"egressId"`
	Priority      int                 `json:"priority"`
	Enabled       bool                `json:"enabled"`
	Revision      int64               `json:"revision"`
	CreatedAt     time.Time           `json:"-"`
	UpdatedAt     time.Time           `json:"-"`
}

// RoutingRuleRepository is optional on the legacy Repository interface so
// existing narrow fakes remain source-compatible during the migration.
type RoutingRuleRepository interface {
	EnsureRoutingRulesMigrated(context.Context) error
	RoutingAuthority(context.Context) (string, error)
	ListRoutingRules(context.Context) ([]RoutingRule, error)
	GetRoutingRule(context.Context, string) (RoutingRule, error)
	SaveRoutingRule(context.Context, RoutingRule) (RoutingRule, error)
	DeleteRoutingRule(context.Context, string, int64) error
}

func NormalizeRoutingRule(rule RoutingRule) (RoutingRule, error) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Name = strings.TrimSpace(rule.Name)
	rule.EgressID = strings.TrimSpace(rule.EgressID)
	if rule.ID == "" {
		return RoutingRule{}, errors.New("routing rule id is required")
	}
	if rule.Name == "" {
		return RoutingRule{}, errors.New("routing rule name is required")
	}
	if rule.EgressID == "" {
		return RoutingRule{}, errors.New("routing rule egress id is required")
	}
	seen := make(map[string]bool, len(rule.TargetListIDs))
	targets := make([]string, 0, len(rule.TargetListIDs))
	for _, targetID := range rule.TargetListIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" || seen[targetID] {
			continue
		}
		seen[targetID] = true
		targets = append(targets, targetID)
	}
	sort.Strings(targets)
	if len(targets) == 0 {
		return RoutingRule{}, errors.New("routing rule requires at least one target list")
	}
	rule.TargetListIDs = targets
	var err error
	rule.Subject, err = subject.Normalize(rule.Subject)
	if err != nil {
		return RoutingRule{}, err
	}
	rule.Ingress = NormalizeTrafficIngressScopeUnvalidated(rule.Ingress)
	if (rule.Subject.Mode == SubjectModeAll || rule.Subject.Mode == SubjectModeExcluded) && !HasTrafficIngress(rule.Ingress) {
		return RoutingRule{}, ErrRoutingExcludedRequiresIngress
	}
	if rule.Subject.Mode == SubjectModeSelected {
		// selected/source-only deliberately ignores ingress for matching. Do not
		// preserve an unused scope as part of its canonical execution identity.
		rule.Ingress = TrafficIngressScope{}
	}
	return rule, nil
}

func RoutingRuleMembers(rule RoutingRule) []accesscontrol.RuleMember {
	result := make([]accesscontrol.RuleMember, len(rule.Subject.Members))
	for index, member := range rule.Subject.Members {
		result[index] = accesscontrol.RuleMember{
			RuleID: rule.ID, TerminalID: member.TerminalID, Binding: member.Binding,
			AnchorMAC: member.AnchorMAC, PinnedIPv4: append([]string{}, member.PinnedIPv4...),
			PinnedIPv6: append([]string{}, member.PinnedIPv6...), LastIPv4: append([]string{}, member.LastIPv4...),
			LastIPv6: append([]string{}, member.LastIPv6...),
		}
	}
	return result
}

type RoutingRuleConflict struct {
	RuleAID string `json:"ruleAId"`
	RuleBID string `json:"ruleBId"`
	EgressA string `json:"egressAId"`
	EgressB string `json:"egressBId"`
	Kind    string `json:"kind"`
	Reason  string `json:"reason"`
	Unknown bool   `json:"indeterminate"`
}

// RoutingRuleConflicts reports only enabled rules whose subject and target
// projections overlap while their Egresses differ. Priority only orders the
// result and never resolves a conflict.
func RoutingRuleConflicts(rules []RoutingRule, targets map[string][]SourceRule, kinds map[string]string) []RoutingRuleConflict {
	ordered := append([]RoutingRule{}, rules...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority < ordered[j].Priority
		}
		return ordered[i].ID < ordered[j].ID
	})
	result := make([]RoutingRuleConflict, 0)
	for leftIndex := 0; leftIndex < len(ordered); leftIndex++ {
		left := ordered[leftIndex]
		if !left.Enabled {
			continue
		}
		for rightIndex := leftIndex + 1; rightIndex < len(ordered); rightIndex++ {
			right := ordered[rightIndex]
			if !right.Enabled || left.EgressID == right.EgressID {
				continue
			}
			subjectOverlap, indeterminate := SubjectsOverlap(left.Subject, right.Subject)
			if !subjectOverlap {
				if !indeterminate {
					continue
				}
				// An unresolved subject is a warning, not a guessed logical
				// conflict. Keep it available through the explicit warning helper.
				continue
			}
			if targetOverlap, targetKind := routingTargetsOverlap(left.TargetListIDs, right.TargetListIDs, targets, kinds); targetOverlap {
				result = append(result, RoutingRuleConflict{RuleAID: left.ID, RuleBID: right.ID, EgressA: left.EgressID, EgressB: right.EgressID, Kind: targetKind, Reason: "enabled routing rules have overlapping subjects and targets"})
			}
		}
	}
	return result
}

// RoutingRuleSubjectWarnings identifies selected-subject pairs for which the
// available terminal evidence cannot prove whether overlap exists.
func RoutingRuleSubjectWarnings(rules []RoutingRule) []RoutingRuleConflict {
	ordered := append([]RoutingRule{}, rules...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	result := make([]RoutingRuleConflict, 0)
	for leftIndex := 0; leftIndex < len(ordered); leftIndex++ {
		left := ordered[leftIndex]
		if !left.Enabled {
			continue
		}
		for rightIndex := leftIndex + 1; rightIndex < len(ordered); rightIndex++ {
			right := ordered[rightIndex]
			if !right.Enabled || left.EgressID == right.EgressID {
				continue
			}
			overlap, indeterminate := SubjectsOverlap(left.Subject, right.Subject)
			if indeterminate && !overlap {
				result = append(result, RoutingRuleConflict{RuleAID: left.ID, RuleBID: right.ID, EgressA: left.EgressID, EgressB: right.EgressID, Kind: "subject", Unknown: true, Reason: "terminal address evidence is insufficient to determine subject overlap"})
			}
		}
	}
	return result
}

func SubjectsOverlap(left, right Subject) (overlap, indeterminate bool) {
	left, leftErr := subject.Normalize(left)
	right, rightErr := subject.Normalize(right)
	if leftErr != nil || rightErr != nil {
		return false, true
	}
	if left.Mode == subject.ModeAll || right.Mode == subject.ModeAll {
		return true, false
	}
	// An excluded subject is the ingress scope minus a set of addresses. The
	// compiler cannot prove that its complement is empty from the stored
	// address evidence, so cross-egress comparisons remain conservative.
	if left.Mode == subject.ModeExcluded || right.Mode == subject.ModeExcluded {
		return true, false
	}
	for _, leftPrefix := range left.Prefixes {
		for _, rightPrefix := range right.Prefixes {
			if subject.PrefixesOverlap(leftPrefix, rightPrefix) {
				return true, false
			}
		}
	}
	leftMembers := make(map[string]SubjectMember, len(left.Members))
	for _, member := range left.Members {
		leftMembers[member.TerminalID] = member
	}
	for _, rightMember := range right.Members {
		if _, ok := leftMembers[rightMember.TerminalID]; ok {
			return true, false
		}
	}
	leftAddresses, leftUnknown := subjectMemberAddresses(left.Members)
	rightAddresses, rightUnknown := subjectMemberAddresses(right.Members)
	for address := range leftAddresses {
		if rightAddresses[address] {
			return true, false
		}
	}
	for _, prefix := range left.Prefixes {
		for address := range rightAddresses {
			if prefixContains(prefix, address) {
				return true, false
			}
		}
	}
	for _, prefix := range right.Prefixes {
		for address := range leftAddresses {
			if prefixContains(prefix, address) {
				return true, false
			}
		}
	}
	return false, leftUnknown || rightUnknown
}

func subjectMemberAddresses(members []SubjectMember) (map[string]bool, bool) {
	result := make(map[string]bool)
	unknown := false
	for _, member := range members {
		addresses := append(append([]string{}, member.PinnedIPv4...), member.PinnedIPv6...)
		if member.Binding == subject.BindingAuto {
			addresses = append(append([]string{}, member.LastIPv4...), member.LastIPv6...)
		}
		if len(addresses) == 0 {
			unknown = true
		}
		for _, address := range addresses {
			result[strings.TrimSpace(address)] = true
		}
	}
	return result, unknown
}

func prefixContains(prefix, address string) bool {
	parsedPrefix, errPrefix := netip.ParsePrefix(strings.TrimSpace(prefix))
	parsedAddress, errAddress := netip.ParseAddr(strings.TrimSpace(address))
	return errPrefix == nil && errAddress == nil && parsedPrefix.Contains(parsedAddress)
}

func routingTargetsOverlap(leftIDs, rightIDs []string, targets map[string][]SourceRule, kinds map[string]string) (bool, string) {
	for _, leftID := range leftIDs {
		for _, rightID := range rightIDs {
			if leftID == rightID {
				return true, firstNonEmptyString(kinds[leftID], "target")
			}
			kind := firstNonEmptyString(kinds[leftID], kinds[rightID])
			if kinds[leftID] != "" && kinds[rightID] != "" && kinds[leftID] != kinds[rightID] {
				continue
			}
			if targetRuleSetsOverlap(targets[leftID], targets[rightID], kind) {
				return true, firstNonEmptyString(kind, "target")
			}
		}
	}
	return false, ""
}

func targetRuleSetsOverlap(left, right []SourceRule, kind string) bool {
	for _, leftRule := range left {
		for _, rightRule := range right {
			if kind == KindIP || (kind == "" && isIPRule(leftRule.RuleType) && isIPRule(rightRule.RuleType)) {
				leftPrefix, leftErr := targetRulePrefix(leftRule.Domain)
				rightPrefix, rightErr := targetRulePrefix(rightRule.Domain)
				if leftErr == nil && rightErr == nil && leftPrefix.Addr().Is4() == rightPrefix.Addr().Is4() && leftPrefix.Overlaps(rightPrefix) {
					return true
				}
				continue
			}
			if kind == KindDomain || (kind == "" && isDomainRule(leftRule.RuleType) && isDomainRule(rightRule.RuleType)) {
				if domainRulesOverlap(leftRule, rightRule) {
					return true
				}
			}
		}
	}
	return false
}

func targetRulePrefix(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked(), nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	if address.Zone() != "" {
		return netip.Prefix{}, errors.New("zoned target address is not a prefix")
	}
	bits := 128
	if address.Is4() {
		bits = 32
	}
	return netip.PrefixFrom(address, bits), nil
}

func domainRulesOverlap(left, right SourceRule) bool {
	leftDomain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(left.Domain)), ".")
	rightDomain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(right.Domain)), ".")
	if leftDomain == "" || rightDomain == "" {
		return false
	}
	leftSuffix := left.RuleType == "DOMAIN-SUFFIX"
	rightSuffix := right.RuleType == "DOMAIN-SUFFIX"
	switch {
	case !leftSuffix && !rightSuffix:
		return leftDomain == rightDomain
	case leftSuffix && rightSuffix:
		return leftDomain == rightDomain || strings.HasSuffix(leftDomain, "."+rightDomain) || strings.HasSuffix(rightDomain, "."+leftDomain)
	case leftSuffix:
		return rightDomain == leftDomain || strings.HasSuffix(rightDomain, "."+leftDomain)
	default:
		return leftDomain == rightDomain || strings.HasSuffix(leftDomain, "."+rightDomain)
	}
}

func isIPRule(ruleType string) bool {
	return ruleType == "IP-CIDR" || ruleType == "IP-CIDR6"
}

func isDomainRule(ruleType string) bool {
	return ruleType == "DOMAIN" || ruleType == "DOMAIN-SUFFIX"
}

type routingDomainProjection struct {
	egressID string
	targetID string
	ruleID   string
	rules    []SourceRule
}

// DomainProjectionContextAmbiguities is separate from logical overlap: DNS
// static forwarding is device-global and has no RoutingRule subject matcher.
// A projection is physical, so rules sharing one (egress, target) are
// normalized before overlapping projections are compared.
func DomainProjectionContextAmbiguities(rules []RoutingRule, targets map[string][]SourceRule, kinds map[string]string, egresses map[string]Egress) []RoutingRuleConflict {
	projections := routingDomainProjections(rules, targets, kinds, egresses)
	result := make([]RoutingRuleConflict, 0)
	for leftIndex := 0; leftIndex < len(projections); leftIndex++ {
		left := projections[leftIndex]
		for rightIndex := leftIndex + 1; rightIndex < len(projections); rightIndex++ {
			right := projections[rightIndex]
			if !domainProjectionRulesOverlap(left.rules, right.rules) {
				continue
			}
			result = append(result, RoutingRuleConflict{
				RuleAID: left.ruleID, RuleBID: right.ruleID, EgressA: left.egressID, EgressB: right.egressID,
				Kind:   "domain_projection_context_ambiguous",
				Reason: "重叠的域名内容需要多个不同的 RouterOS DNS/address-list 物理投影",
			})
		}
	}
	return result
}

func routingDomainProjections(rules []RoutingRule, targets map[string][]SourceRule, kinds map[string]string, egresses map[string]Egress) []routingDomainProjection {
	ordered := append([]RoutingRule{}, rules...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	byKey := make(map[string]*routingDomainProjection)
	for _, rule := range ordered {
		if !rule.Enabled || egresses[rule.EgressID].PendingDeletion {
			continue
		}
		for _, targetID := range sortedUniqueProjectionIDs(rule.TargetListIDs) {
			if kinds[targetID] != "" && kinds[targetID] != KindDomain {
				continue
			}
			key := rule.EgressID + "\x00" + targetID
			projection := byKey[key]
			if projection == nil {
				projection = &routingDomainProjection{egressID: rule.EgressID, targetID: targetID, ruleID: rule.ID, rules: append([]SourceRule{}, targets[targetID]...)}
				byKey[key] = projection
			}
		}
	}
	result := make([]routingDomainProjection, 0, len(byKey))
	for _, projection := range byKey {
		result = append(result, *projection)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].egressID != result[j].egressID {
			return result[i].egressID < result[j].egressID
		}
		return result[i].targetID < result[j].targetID
	})
	return result
}

func domainProjectionRulesOverlap(left, right []SourceRule) bool {
	for _, leftRule := range left {
		if !isDomainRule(leftRule.RuleType) {
			continue
		}
		for _, rightRule := range right {
			if isDomainRule(rightRule.RuleType) && domainRulesOverlap(leftRule, rightRule) {
				return true
			}
		}
	}
	return false
}
