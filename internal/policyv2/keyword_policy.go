package policyv2

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	routingKeywordRegexpPrecedenceCode = "routing_keyword_regexp_precedence"
)

// keywordDomainsForRoutingRule reads one rule's selected target content and
// returns the unique parsed DOMAIN-KEYWORD values in deterministic order.
// Pending content is used for the proposed graph; callers comparing against
// the existing rule can request the active version explicitly.
func keywordDomainsForRoutingRule(ctx context.Context, repository Repository, rule RoutingRule, targetScope map[string]bool, preferActive bool) ([]string, error) {
	sources, err := repository.ListSources(ctx, "")
	if err != nil {
		return nil, err
	}
	sourceByID := make(map[string]Source, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	seen := make(map[string]bool)
	keywords := make([]string, 0)
	for _, targetID := range sortedUniqueProjectionIDs(rule.TargetListIDs) {
		source, ok := sourceByID[targetID]
		if !ok || source.PendingDeletion || source.Kind != KindDomain {
			continue
		}
		versionID := keywordVersionForComparison(source, targetScope, preferActive)
		if versionID == "" {
			continue
		}
		rules, err := allRules(ctx, repository, versionID)
		if err != nil {
			return nil, err
		}
		for _, sourceRule := range rules {
			if !isKeywordRule(sourceRule.RuleType) {
				continue
			}
			keyword := strings.TrimSpace(sourceRule.Domain)
			if keyword != "" && !seen[keyword] {
				seen[keyword] = true
				keywords = append(keywords, keyword)
			}
		}
	}
	sort.Strings(keywords)
	return keywords, nil
}

func keywordVersionForComparison(source Source, targetScope map[string]bool, preferActive bool) string {
	if preferActive {
		if active := strings.TrimSpace(source.ActiveVersionID); active != "" {
			return active
		}
	}
	return targetVersionForPlan(source, targetScope)
}

// keywordImpactForProposal describes the proposed keyword projection. The
// boolean itself is the user's explicit opt-in; the impact is informational
// and must never become a second acknowledgement gate. A source refresh has
// no proposal and therefore does not call this path.
func keywordImpactForProposal(ctx context.Context, baseRepository, plannedRepository Repository, proposal *PolicyProposal, targetScope map[string]bool) (*KeywordImpact, error) {
	if proposal == nil || proposal.RoutingRule == nil {
		return nil, nil
	}
	rule := *proposal.RoutingRule
	keywords, err := keywordDomainsForRoutingRule(ctx, plannedRepository, rule, targetScope, false)
	if err != nil {
		return nil, err
	}
	impact := &KeywordImpact{
		Enabled:        rule.IncludeKeywordDomains,
		AvailableCount: len(keywords),
		Keywords:       append([]string(nil), keywords...),
		PrecedenceMode: "routeros-regexp-first",
	}
	if impact.Enabled {
		impact.ProjectedCount = len(keywords)
	}
	if !impact.Enabled || len(keywords) == 0 {
		return impact, nil
	}

	previous, found := RoutingRule{}, false
	if routingRepository, ok := baseRepository.(RoutingRuleRepository); ok && strings.TrimSpace(rule.ID) != "" {
		candidate, getErr := routingRepository.GetRoutingRule(ctx, rule.ID)
		if getErr == nil {
			previous, found = candidate, true
		} else if !errors.Is(getErr, ErrRoutingRuleNotFound) {
			return nil, getErr
		}
	}
	targetsChanged := !sameRoutingTargetSet(previous.TargetListIDs, rule.TargetListIDs)
	previousKeywords := []string{}
	if found && previous.IncludeKeywordDomains {
		if targetsChanged {
			previousKeywords, err = keywordDomainsForRoutingRule(ctx, baseRepository, previous, nil, true)
			if err != nil {
				return nil, err
			}
		} else {
			// A pending source refresh may have changed the backing content while
			// the selected target IDs stayed the same. Keep the comparison useful
			// for informational metadata, but never turn it into a new gate.
			previousKeywords = append([]string(nil), keywords...)
		}
	}
	previousSet := make(map[string]bool, len(previousKeywords))
	for _, keyword := range previousKeywords {
		previousSet[keyword] = true
	}
	for _, keyword := range keywords {
		if !previousSet[keyword] {
			impact.IntroducedKeywords = append(impact.IntroducedKeywords, keyword)
		}
	}
	// Kept in the response for compatibility with existing clients. Explicitly
	// enabling includeKeywordDomains is the complete user opt-in; keyword
	// warnings never require a second acknowledgement.
	impact.RequiresConfirmation = false
	return impact, nil
}

func sameRoutingTargetSet(left, right []string) bool {
	return strings.Join(sortedUniqueProjectionIDs(left), "\x00") == strings.Join(sortedUniqueProjectionIDs(right), "\x00")
}

func appendKeywordImpactToPlan(plan *Plan, impact *KeywordImpact, logicalID string) {
	if plan == nil || impact == nil {
		return
	}
	plan.KeywordImpact = impact
	if !impact.Enabled || impact.ProjectedCount == 0 {
		return
	}
	keywordSummary := strings.Join(impact.Keywords[:min(len(impact.Keywords), 10)], "、")
	if len(impact.Keywords) > 10 {
		keywordSummary += " 等"
	}
	ruleLabel := logicalID
	if ruleLabel == "" {
		ruleLabel = "当前策略"
	}
	plan.Warnings = append(plan.Warnings, PlanIssue{
		Code:      routingKeywordRegexpPrecedenceCode,
		Status:    "warning",
		LogicalID: logicalID,
		Reason:    fmt.Sprintf("策略「%s」将启用 %d 条关键字规则（%s），并生成 RouterOS DNS Static regexp。RouterOS 会先匹配 regexp，再匹配普通 DOMAIN / DOMAIN-SUFFIX；当域名同时命中时，关键字规则可能绕过普通 Priority 顺序，并优先于普通策略路由或访问控制域名规则生效。若不希望启用，请返回“高级设置”关闭“启用关键字域名规则”。", ruleLabel, impact.ProjectedCount, keywordSummary),
	})
}
