package policyv2

import (
	"context"
	"sort"
	"strings"

	"rosboard/internal/accesscontrol"
)

const (
	accessDomainProjectionAmbiguousCode = "access_domain_projection_ambiguous"
	crossDomainPriorityShadowedCode     = "cross_domain_access_priority_shadowed"
)

type domainProjectionConsumer struct {
	ruleID   string
	ruleName string
	targetID string
	egressID string
	rules    []SourceRule
}

func appendAccessDomainProjectionBlockers(ctx context.Context, repository Repository, rules []accesscontrol.AccessRule, sources map[string]Source, targetScope map[string]bool, result *DesiredResult) error {
	consumers, err := accessDomainProjectionConsumers(ctx, repository, rules, sources, targetScope)
	if err != nil {
		return err
	}
	for leftIndex := 0; leftIndex < len(consumers); leftIndex++ {
		left := consumers[leftIndex]
		for rightIndex := leftIndex + 1; rightIndex < len(consumers); rightIndex++ {
			right := consumers[rightIndex]
			if left.targetID == right.targetID || !targetRuleSetsOverlap(left.rules, right.rules, KindDomain) {
				continue
			}
			result.Blockers = append(result.Blockers, PlanIssue{
				Code: accessDomainProjectionAmbiguousCode, Status: "blocker", LogicalID: left.ruleID,
				Reason: "启用的访问规则引用了内容重叠的不同域名目标。RouterOS DNS Static 是设备级有序匹配，无法安全投影到两个独立的访问目标列表：" + left.targetID + " / " + right.targetID,
			})
		}
	}
	return nil
}

// CrossDomainProjectionResolution describes one active Access/Routing domain
// overlap. Access has fixed precedence on the device-wide RouterOS DNS Static
// list; the resolution is therefore a warning plus an ordering constraint, not
// a blocker.
type CrossDomainProjectionResolution struct {
	AccessRuleID    string          `json:"accessRuleId"`
	AccessRuleName  string          `json:"accessRuleName,omitempty"`
	AccessTargetID  string          `json:"accessTargetId"`
	RoutingRuleID   string          `json:"routingRuleId"`
	RoutingRuleName string          `json:"routingRuleName,omitempty"`
	RoutingEgressID string          `json:"routingEgressId"`
	RoutingTargetID string          `json:"routingTargetId"`
	Overlaps        [][2]SourceRule `json:"overlaps"`
}

func CrossDomainProjectionResolutions(ctx context.Context, repository Repository, accessRepository accesscontrol.Repository, targetScope map[string]bool) ([]CrossDomainProjectionResolution, error) {
	if accessRepository == nil {
		return nil, nil
	}
	sources, err := repository.ListSources(ctx, "")
	if err != nil {
		return nil, err
	}
	sourceByID := make(map[string]Source, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	routingConsumers, err := routingDomainProjectionConsumers(ctx, repository, sourceByID, targetScope)
	if err != nil {
		return nil, err
	}
	accessRules, err := accessRepository.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	accessConsumers, err := accessDomainProjectionConsumers(ctx, repository, accessRules, sourceByID, targetScope)
	if err != nil {
		return nil, err
	}
	result := make([]CrossDomainProjectionResolution, 0)
	seen := make(map[string]bool)
	for _, routing := range routingConsumers {
		for _, access := range accessConsumers {
			overlaps := domainProjectionOverlaps(access.rules, routing.rules)
			if len(overlaps) == 0 {
				continue
			}
			// Access and Routing may each have several rule consumers for one
			// physical target projection. The DNS order contract is per physical
			// Access-target/Routing-egress-target pair, not per rule pair.
			key := access.targetID + "\x00" + routing.egressID + "\x00" + routing.targetID
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, CrossDomainProjectionResolution{
				AccessRuleID: access.ruleID, AccessRuleName: access.ruleName, AccessTargetID: access.targetID,
				RoutingRuleID: routing.ruleID, RoutingRuleName: routing.ruleName, RoutingEgressID: routing.egressID,
				RoutingTargetID: routing.targetID, Overlaps: overlaps,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		pairs := [][2]string{
			{left.AccessRuleID, right.AccessRuleID}, {left.AccessTargetID, right.AccessTargetID},
			{left.RoutingRuleID, right.RoutingRuleID}, {left.RoutingEgressID, right.RoutingEgressID},
			{left.RoutingTargetID, right.RoutingTargetID},
		}
		for _, pair := range pairs {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
	return result, nil
}

func appendCrossDomainProjectionResolutions(ctx context.Context, repository Repository, accessRepository accesscontrol.Repository, targetScope map[string]bool, result *DesiredResult) error {
	resolutions, err := CrossDomainProjectionResolutions(ctx, repository, accessRepository, targetScope)
	if err != nil {
		return err
	}
	result.crossDomainResolutions = append([]CrossDomainProjectionResolution(nil), resolutions...)
	for _, resolution := range resolutions {
		logicalID := resolution.RoutingRuleID
		if result.Domain == PolicyDomainAccess {
			logicalID = resolution.AccessRuleID
		}
		result.Warnings = append(result.Warnings, PlanIssue{
			Code: crossDomainPriorityShadowedCode, Status: "warning", LogicalID: logicalID, EgressID: resolution.RoutingEgressID,
			Reason: "访问控制域名投影优先于策略路由：" + domainOverlapPhrase(resolution.Overlaps) + "。访问规则「" + displayName(resolution.AccessRuleName, resolution.AccessRuleID) + "」优先于策略路由「" + displayName(resolution.RoutingRuleName, resolution.RoutingRuleID) + "」，重叠域名在本设备上不会按策略路由出口生效；其他不重叠域名不受影响。",
		})
	}
	return nil
}

func accessDomainProjectionConsumers(ctx context.Context, repository Repository, rules []accesscontrol.AccessRule, sources map[string]Source, targetScope map[string]bool) ([]domainProjectionConsumer, error) {
	consumers := make([]domainProjectionConsumer, 0)
	seen := make(map[string]bool)
	for _, rule := range rules {
		if rule.TargetScope != accesscontrol.TargetScopeTargets {
			continue
		}
		for _, targetID := range sortedUniqueProjectionIDs(rule.TargetListIDs) {
			source, ok := sources[targetID]
			if !ok || !accessTargetConsumerActive(rule, source) || source.Kind != KindDomain {
				continue
			}
			versionID := targetVersionForPlan(source, targetScope)
			if versionID == "" {
				continue
			}
			rulesForTarget, err := allRules(ctx, repository, versionID)
			if err != nil {
				return nil, err
			}
			key := rule.ID + "\x00" + targetID
			if seen[key] {
				continue
			}
			seen[key] = true
			consumers = append(consumers, domainProjectionConsumer{ruleID: rule.ID, ruleName: rule.Name, targetID: targetID, rules: domainDomainRules(rulesForTarget)})
		}
	}
	sort.Slice(consumers, func(i, j int) bool {
		if consumers[i].ruleID != consumers[j].ruleID {
			return consumers[i].ruleID < consumers[j].ruleID
		}
		return consumers[i].targetID < consumers[j].targetID
	})
	return consumers, nil
}

func routingDomainProjectionConsumers(ctx context.Context, repository Repository, sources map[string]Source, targetScope map[string]bool) ([]domainProjectionConsumer, error) {
	consumers := make([]domainProjectionConsumer, 0)
	if routingRepository, ok := repository.(RoutingRuleRepository); ok {
		if err := routingRepository.EnsureRoutingRulesMigrated(ctx); err != nil {
			return nil, err
		}
		authority, err := routingRepository.RoutingAuthority(ctx)
		if err != nil {
			return nil, err
		}
		if authority == RoutingRuleAuthorityV1 {
			rules, err := routingRepository.ListRoutingRules(ctx)
			if err != nil {
				return nil, err
			}
			egresses, err := repository.ListEgresses(ctx)
			if err != nil {
				return nil, err
			}
			egressByID := make(map[string]Egress, len(egresses))
			for _, egress := range egresses {
				egressByID[egress.ID] = egress
			}
			for _, rule := range rules {
				if !rule.Enabled {
					continue
				}
				egress, ok := egressByID[rule.EgressID]
				if !ok || egress.PendingDeletion || !egress.Enabled {
					continue
				}
				for _, targetID := range sortedUniqueProjectionIDs(rule.TargetListIDs) {
					source, ok := sources[targetID]
					if !ok || source.PendingDeletion || source.Kind != KindDomain {
						continue
					}
					versionID := targetVersionForPlan(source, targetScope)
					if versionID == "" {
						continue
					}
					rulesForTarget, err := allRules(ctx, repository, versionID)
					if err != nil {
						return nil, err
					}
					consumers = append(consumers, domainProjectionConsumer{ruleID: rule.ID, ruleName: rule.Name, targetID: targetID, egressID: rule.EgressID, rules: domainDomainRules(rulesForTarget)})
				}
			}
			return consumers, nil
		}
	}
	for _, source := range sources {
		if strings.TrimSpace(source.EgressID) == "" || source.PendingDeletion || !source.Enabled || source.Kind != KindDomain {
			continue
		}
		versionID := targetVersionForPlan(source, targetScope)
		if versionID == "" {
			continue
		}
		rulesForTarget, err := allRules(ctx, repository, versionID)
		if err != nil {
			return nil, err
		}
		consumers = append(consumers, domainProjectionConsumer{ruleID: source.ID, ruleName: source.Name, targetID: source.ID, egressID: source.EgressID, rules: domainDomainRules(rulesForTarget)})
	}
	return consumers, nil
}

func domainDomainRules(rules []SourceRule) []SourceRule {
	result := make([]SourceRule, 0, len(rules))
	for _, rule := range rules {
		if isDomainRule(rule.RuleType) {
			result = append(result, rule)
		}
	}
	return result
}

func sortedUniqueProjectionIDs(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
