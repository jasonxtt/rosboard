package policyv2

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rosboard/internal/accesscontrol"
)

const (
	routingKeywordRegexpPrecedenceCode = "routing_keyword_regexp_precedence"
	routingKeywordAccessUnsafeCode     = "routing_keyword_access_precedence_unsafe"
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

// keywordImpactForProposal compares the proposed routing rule with the
// currently saved rule. A source refresh has no proposal and therefore does
// not call this path: automatic additions to an already-enabled rule do not
// create a repeat newly-introduced-risk marker.
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
			// the selected target IDs stayed the same. That is an automatic
			// consumer update, not a new user choice, so it must not reopen the
			// keyword acknowledgement.
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
	impact.RequiresConfirmation = !found || !previous.IncludeKeywordDomains || len(impact.IntroducedKeywords) > 0
	return impact, nil
}

// RoutingRuleKeywordImpactForSave applies the same acknowledgement decision
// used by proposal plans to the legacy direct routing-rule write path. Direct
// writes must fail closed before changing canonical desired state; otherwise a
// client could opt into RouterOS regexp-first matching without an approved
// plan acknowledgement.
func RoutingRuleKeywordImpactForSave(ctx context.Context, repository Repository, rule RoutingRule) (*KeywordImpact, error) {
	return keywordImpactForProposal(ctx, repository, repository, &PolicyProposal{RoutingRule: &rule}, nil)
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
		Reason:    fmt.Sprintf("策略「%s」将启用 %d 条关键字规则（%s），并生成 RouterOS DNS Static regexp。RouterOS 会先匹配 regexp，再匹配普通域名；当域名同时命中时，关键字规则可能绕过普通 Priority 顺序并由当前策略优先处理，从而改变设备级 DNS 顺序。若不希望启用，请返回“高级设置”关闭“启用关键字域名规则”。", ruleLabel, impact.ProjectedCount, keywordSummary),
	})
}

type routingKeywordAccessProjection struct {
	rule     RoutingRule
	egress   Egress
	targetID string
	keywords []string
}

// appendRoutingKeywordAccessPrecedenceBlockers fails closed for the one
// cross-domain case that cannot be repaired by ordinary Access-before-Routing
// ordering: RouterOS checks regexp DNS Statics before all plain name records.
func appendRoutingKeywordAccessPrecedenceBlockers(ctx context.Context, repository Repository, accessRepository accesscontrol.Repository, targetScope map[string]bool, result *DesiredResult) error {
	if accessRepository == nil || result == nil {
		return nil
	}
	routingRepository, ok := repository.(RoutingRuleRepository)
	if !ok {
		return nil
	}
	if err := routingRepository.EnsureRoutingRulesMigrated(ctx); err != nil {
		return err
	}
	authority, err := routingRepository.RoutingAuthority(ctx)
	if err != nil {
		return err
	}
	if authority != RoutingRuleAuthorityV1 {
		return nil
	}
	routingRules, err := routingRepository.ListRoutingRules(ctx)
	if err != nil {
		return err
	}
	egresses, err := repository.ListEgresses(ctx)
	if err != nil {
		return err
	}
	egressByID := make(map[string]Egress, len(egresses))
	for _, egress := range egresses {
		egressByID[egress.ID] = egress
	}
	sources, err := repository.ListSources(ctx, "")
	if err != nil {
		return err
	}
	sourceByID := make(map[string]Source, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	routingProjections := make([]routingKeywordAccessProjection, 0)
	for _, rule := range routingRules {
		if !rule.Enabled || !rule.IncludeKeywordDomains {
			continue
		}
		egress, ok := egressByID[rule.EgressID]
		if !ok || !egress.Enabled || egress.PendingDeletion {
			continue
		}
		for _, targetID := range sortedUniqueProjectionIDs(rule.TargetListIDs) {
			source, ok := sourceByID[targetID]
			if !ok || source.PendingDeletion || source.Kind != KindDomain {
				continue
			}
			versionID := targetVersionForPlan(source, targetScope)
			if versionID == "" {
				continue
			}
			rulesForTarget, listErr := allRules(ctx, repository, versionID)
			if listErr != nil {
				return listErr
			}
			keywords := domainKeywordRules(rulesForTarget)
			seen := make(map[string]bool, len(keywords))
			values := make([]string, 0, len(keywords))
			for _, keywordRule := range keywords {
				if keywordRule.Domain != "" && !seen[keywordRule.Domain] {
					seen[keywordRule.Domain] = true
					values = append(values, keywordRule.Domain)
				}
			}
			if len(values) > 0 {
				routingProjections = append(routingProjections, routingKeywordAccessProjection{rule: rule, egress: egress, targetID: targetID, keywords: values})
			}
		}
	}
	if len(routingProjections) == 0 {
		return nil
	}
	accessRules, err := accessRepository.ListRules(ctx)
	if err != nil {
		return err
	}
	seenBlockers := make(map[string]bool)
	for _, accessRule := range accessRules {
		if !accessRule.Enabled || accessRule.TargetScope != accesscontrol.TargetScopeTargets {
			continue
		}
		for _, targetID := range sortedUniqueProjectionIDs(accessRule.TargetListIDs) {
			source, ok := sourceByID[targetID]
			if !ok || source.PendingDeletion || source.Kind != KindDomain {
				continue
			}
			versionID := targetVersionForPlan(source, targetScope)
			if versionID == "" {
				continue
			}
			rulesForTarget, listErr := allRules(ctx, repository, versionID)
			if listErr != nil {
				return listErr
			}
			plainRules := domainPlainRules(rulesForTarget)
			for _, accessMatcher := range plainRules {
				if accessMatcher.RuleType != "DOMAIN" && accessMatcher.RuleType != "DOMAIN-SUFFIX" {
					continue
				}
				for _, routing := range routingProjections {
					if !keywordMayPrecedeAccessMatcher(routing.keywords, accessMatcher) {
						continue
					}
					key := routing.rule.ID + "\x00" + routing.targetID + "\x00" + accessRule.ID + "\x00" + targetID + "\x00" + accessMatcher.RuleType + "\x00" + accessMatcher.Domain
					if seenBlockers[key] {
						continue
					}
					seenBlockers[key] = true
					result.Blockers = append(result.Blockers, PlanIssue{
						Code: routingKeywordAccessUnsafeCode, Status: "blocker", LogicalID: routing.rule.ID, EgressID: routing.rule.EgressID,
						Reason: "访问规则「" + displayName(accessRule.Name, accessRule.ID) + "」的 " + accessMatcher.RuleType + " " + accessMatcher.Domain + " 与启用的策略路由关键字目标 " + routing.targetID + " 重叠；RouterOS 会先匹配 regexp，无法安全保证访问控制优先，请拆分目标或关闭「启用关键字域名规则」",
					})
				}
			}
		}
	}
	return nil
}

func keywordMayPrecedeAccessMatcher(keywords []string, accessMatcher SourceRule) bool {
	if accessMatcher.RuleType == "DOMAIN-SUFFIX" {
		return len(keywords) > 0
	}
	if accessMatcher.RuleType != "DOMAIN" {
		return false
	}
	for _, keyword := range keywords {
		if strings.Contains(accessMatcher.Domain, keyword) {
			return true
		}
	}
	return false
}
