package policyv2

import (
	"context"
	"errors"
	"fmt"
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

// RoutingRuleConflicts reports only enabled rules whose subject and IP-target
// projections overlap while their Egresses differ. Domain-content overlap is
// deliberately excluded: RouterOS DNS Static is device-global and its overlap
// is resolved deterministically by rule Priority through
// DomainProjectionResolutions, never by this logical-conflict check.
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
				// Two rules sharing a domain TargetList across different Egresses
				// produce two physical DNS projections whose overlap is resolved
				// by projection priority, not by this logical conflict.
				if kinds[leftID] == KindDomain {
					continue
				}
				return true, firstNonEmptyString(kinds[leftID], "target")
			}
			if kinds[leftID] != "" && kinds[rightID] != "" && kinds[leftID] != kinds[rightID] {
				continue
			}
			// Domain-content overlap between distinct target lists is resolved by
			// the DNS projection priority model, not this logical conflict check.
			// Unmaterialized lists (empty rules) keep the conservative same-ID
			// style conflict signal only when both sides are actually IP content.
			if targetIPRulesOverlap(targets[leftID], targets[rightID]) {
				return true, KindIP
			}
		}
	}
	return false, ""
}

func targetIPRulesOverlap(left, right []SourceRule) bool {
	for _, leftRule := range left {
		if !isIPRule(leftRule.RuleType) {
			continue
		}
		for _, rightRule := range right {
			if !isIPRule(rightRule.RuleType) {
				continue
			}
			leftPrefix, leftErr := targetRulePrefix(leftRule.Domain)
			rightPrefix, rightErr := targetRulePrefix(rightRule.Domain)
			if leftErr == nil && rightErr == nil && leftPrefix.Addr().Is4() == rightPrefix.Addr().Is4() && leftPrefix.Overlaps(rightPrefix) {
				return true
			}
		}
	}
	return false
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

// routingDomainProjection is one physical RouterOS DNS/address-list projection
// keyed by (egressID, targetListID). Several RoutingRules may share it, so the
// projection carries the active-consumer set and the effective DNS priority
// (the smallest, i.e. highest, Priority among active consumers) instead of a
// single arbitrary rule reference. An active DNS consumer is an enabled rule on
// an enabled, non-pending-deletion Egress: only those projections materialize a
// live `disabled=no` RouterOS DNS Static, so only they may arbitrate device DNS
// conflicts. Disabled rules and rules on a disabled/missing Egress still keep a
// deterministic ordering priority but never shadow, warn, block, or become a
// winner/loser.
type routingDomainProjection struct {
	egressID        string
	targetID        string
	ruleID          string
	ruleName        string
	priority        int
	consumerRuleIDs map[string]bool
	activeConsumers map[string]bool
	rules           []SourceRule
}

const projectionPriorityUnset = 1 << 30

// DomainProjectionResolution is one outcome of device-global DNS conflict
// arbitration between two physical domain projections.
type DomainProjectionResolution struct {
	Severity string `json:"severity"` // "warning" or "blocker"
	Code     string `json:"code"`
	Reason   string `json:"reason"`
	RuleAID  string `json:"ruleAId"`
	RuleBID  string `json:"ruleBId"`
	EgressA  string `json:"egressAId"`
	EgressB  string `json:"egressBId"`
}

// DomainProjectionResolutions implements the device-global priority semantics
// for overlapping domain content. RouterOS DNS Static is ordered first-match
// and device-global, so RoutingRule subjects cannot create per-client DNS
// contexts; rule Priority decides the winner among ACTIVE projections only:
//
//	same physical projection            → one projection, no comparison
//	projections sharing an active rule  → intra-rule OR of TargetLists, allowed
//	projection without an active consumer → kept out of arbitration entirely
//	overlap, different effective Priority
//	                       → warning; the higher-priority projection is ordered
//		                      first and wins only the overlap; the loser keeps its
//		                      non-overlapping domain space via first-match
//	overlap, equal effective Priority  → blocker (no deterministic winner)
func DomainProjectionResolutions(rules []RoutingRule, targets map[string][]SourceRule, kinds map[string]string, egresses map[string]Egress) []DomainProjectionResolution {
	projections := routingDomainProjections(rules, targets, kinds, egresses)
	result := make([]DomainProjectionResolution, 0)
	for leftIndex := 0; leftIndex < len(projections); leftIndex++ {
		left := projections[leftIndex]
		if len(left.activeConsumers) == 0 {
			continue
		}
		for rightIndex := leftIndex + 1; rightIndex < len(projections); rightIndex++ {
			right := projections[rightIndex]
			if len(right.activeConsumers) == 0 || projectionsShareActiveRule(left, right) {
				continue
			}
			overlaps := domainProjectionOverlaps(left.rules, right.rules)
			if len(overlaps) == 0 {
				continue
			}
			winner, loser := left, right
			if left.priority > right.priority {
				winner, loser = right, left
			}
			if left.priority == right.priority {
				result = append(result, DomainProjectionResolution{
					Severity: "blocker", Code: "domain_projection_context_ambiguous",
					RuleAID: left.ruleID, RuleBID: right.ruleID, EgressA: left.egressID, EgressB: right.egressID,
					Reason: fmt.Sprintf(
						"域名冲突无法自动裁决：%s。策略「%s」与策略「%s」的 Priority 均为 %d，无法确定唯一的 DNS 解析出口。请修改其中一条策略的优先级（数值更小者优先）后重试。",
						domainOverlapPhrase(overlaps), displayName(left.ruleName, left.ruleID), displayName(right.ruleName, right.ruleID), left.priority),
				})
				continue
			}
			result = append(result, DomainProjectionResolution{
				Severity: "warning", Code: "domain_projection_priority_shadowed",
				RuleAID: winner.ruleID, RuleBID: loser.ruleID, EgressA: winner.egressID, EgressB: loser.egressID,
				Reason: fmt.Sprintf(
					"域名冲突已按优先级处理：%s。策略「%s」(Priority %d) 优先于策略「%s」(Priority %d)，重叠域名将按「%s」执行；「%s」中的重叠部分在本设备上不会生效，其余不重叠域名不受影响。",
					domainOverlapPhrase(overlaps),
					displayName(winner.ruleName, winner.ruleID), winner.priority,
					displayName(loser.ruleName, loser.ruleID), loser.priority,
					displayName(winner.ruleName, winner.ruleID),
					displayName(loser.ruleName, loser.ruleID)),
			})
		}
	}
	return result
}

// DomainProjectionPriorities maps "egressID\x00targetListID" to the effective
// DNS projection priority used to order managed RouterOS DNS Static entries.
// Projections with no active consumer fall back to the highest priority among
// their inactive consumers so ordering stays deterministic while the
// projection is still kept out of active conflict arbitration.
func DomainProjectionPriorities(rules []RoutingRule, kinds map[string]string, egresses map[string]Egress) map[string]int {
	projections := routingDomainProjections(rules, nil, kinds, egresses)
	result := make(map[string]int, len(projections))
	for _, projection := range projections {
		result[projection.egressID+"\x00"+projection.targetID] = projection.priority
	}
	return result
}

func projectionsShareActiveRule(left, right routingDomainProjection) bool {
	for ruleID := range left.activeConsumers {
		if right.activeConsumers[ruleID] {
			return true
		}
	}
	return false
}

func displayName(name, fallback string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return fallback
}

func domainOverlapPhrase(overlaps [][2]SourceRule) string {
	const maxListed = 3
	parts := make([]string, 0, min(len(overlaps), maxListed))
	for _, pair := range overlaps[:min(len(overlaps), maxListed)] {
		parts = append(parts, fmt.Sprintf("%s 与 %s 存在重叠", domainMatcherLabel(pair[0]), domainMatcherLabel(pair[1])))
	}
	if len(overlaps) > maxListed {
		parts = append(parts, fmt.Sprintf("等 %d 处重叠", len(overlaps)))
	}
	return strings.Join(parts, "；")
}

func domainMatcherLabel(rule SourceRule) string {
	if rule.RuleType == "DOMAIN-SUFFIX" {
		return rule.Domain + "（含子域）"
	}
	return rule.Domain
}

func routingDomainProjections(rules []RoutingRule, targets map[string][]SourceRule, kinds map[string]string, egresses map[string]Egress) []routingDomainProjection {
	ordered := append([]RoutingRule{}, rules...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority < ordered[j].Priority
		}
		return ordered[i].ID < ordered[j].ID
	})
	byKey := make(map[string]*routingDomainProjection)
	for _, rule := range ordered {
		egress, egressKnown := egresses[rule.EgressID]
		if egressKnown && egress.PendingDeletion {
			continue
		}
		// A projection on a missing or disabled Egress materializes no live
		// DNS Static (`disabled=yes` at most), so its rules are consumers for
		// ordering only and never arbitration participants.
		activeConsumer := rule.Enabled && egressKnown && egress.Enabled
		for _, targetID := range sortedUniqueProjectionIDs(rule.TargetListIDs) {
			if kinds[targetID] != "" && kinds[targetID] != KindDomain {
				continue
			}
			key := rule.EgressID + "\x00" + targetID
			projection := byKey[key]
			if projection == nil {
				projection = &routingDomainProjection{
					egressID: rule.EgressID, targetID: targetID,
					priority: projectionPriorityUnset, consumerRuleIDs: map[string]bool{}, activeConsumers: map[string]bool{},
					rules: domainDomainRules(targets[targetID]),
				}
				byKey[key] = projection
			}
			projection.consumerRuleIDs[rule.ID] = true
			if !activeConsumer {
				continue
			}
			projection.activeConsumers[rule.ID] = true
			// Rules are visited in ascending Priority order, so the first active
			// consumer is the winning (highest-priority) attribution and the
			// minimum is the effective DNS projection priority.
			if projection.ruleID == "" || rule.Priority < projection.priority {
				projection.ruleID, projection.ruleName, projection.priority = rule.ID, rule.Name, rule.Priority
			}
		}
	}
	result := make([]routingDomainProjection, 0, len(byKey))
	for _, projection := range byKey {
		if projection.priority == projectionPriorityUnset {
			// No active consumer: fall back to the highest priority among the
			// inactive consumers for deterministic ordering only.
			for _, rule := range ordered {
				if rule.EgressID != projection.egressID || !projection.consumerRuleIDs[rule.ID] {
					continue
				}
				if rule.Priority < projection.priority {
					projection.priority = rule.Priority
					if projection.ruleID == "" {
						projection.ruleID, projection.ruleName = rule.ID, rule.Name
					}
				}
			}
		}
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

// domainProjectionOverlaps returns every overlapping matcher pair between two
// projections in deterministic (left rule, right rule) order. Overlap is
// detected on the DOMAIN / DOMAIN-SUFFIX match space only; the winner keeps the
// shared space and the loser keeps its non-overlapping space through RouterOS
// DNS Static ordered first-match, so no domain-set subtraction is needed.
func domainProjectionOverlaps(left, right []SourceRule) [][2]SourceRule {
	result := make([][2]SourceRule, 0)
	for _, leftRule := range left {
		for _, rightRule := range right {
			if domainRulesOverlap(leftRule, rightRule) {
				result = append(result, [2]SourceRule{leftRule, rightRule})
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i][0].Domain != result[j][0].Domain {
			return result[i][0].Domain < result[j][0].Domain
		}
		return result[i][1].Domain < result[j][1].Domain
	})
	return result
}
