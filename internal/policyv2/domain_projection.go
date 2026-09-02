package policyv2

import (
	"context"
	"sort"
	"strings"

	"rosboard/internal/accesscontrol"
)

const (
	accessDomainProjectionAmbiguousCode = "access_domain_projection_ambiguous"
	crossDomainProjectionAmbiguousCode  = "cross_domain_dns_projection_ambiguous"
)

type domainProjectionConsumer struct {
	ruleID   string
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

func appendCrossDomainProjectionBlockers(ctx context.Context, repository Repository, accessRepository accesscontrol.Repository, targetScope map[string]bool, result *DesiredResult) error {
	if accessRepository == nil {
		return nil
	}
	sources, err := repository.ListSources(ctx, "")
	if err != nil {
		return err
	}
	sourceByID := make(map[string]Source, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	routingConsumers, err := routingDomainProjectionConsumers(ctx, repository, sourceByID, targetScope)
	if err != nil {
		return err
	}
	accessRules, err := accessRepository.ListRules(ctx)
	if err != nil {
		return err
	}
	accessConsumers, err := accessDomainProjectionConsumers(ctx, repository, accessRules, sourceByID, targetScope)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, routing := range routingConsumers {
		for _, access := range accessConsumers {
			if !targetRuleSetsOverlap(routing.rules, access.rules, KindDomain) {
				continue
			}
			key := routing.ruleID + "\x00" + access.ruleID
			if seen[key] {
				continue
			}
			seen[key] = true
			logicalID := routing.ruleID
			if result.Domain == PolicyDomainAccess {
				logicalID = access.ruleID
			}
			result.Blockers = append(result.Blockers, PlanIssue{
				Code: crossDomainProjectionAmbiguousCode, Status: "blocker", LogicalID: logicalID, EgressID: routing.egressID,
				Reason: "同一域名目标同时用于策略路由和访问控制。RouterOS DNS Static 是设备级有序匹配，无法按客户端为两个独立的 DNS/address-list 上下文分别投影，请改用 IP 目标或避免两边使用重叠的域名目标。",
			})
		}
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
			consumers = append(consumers, domainProjectionConsumer{ruleID: rule.ID, targetID: targetID, rules: domainDomainRules(rulesForTarget)})
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
					if !ok || source.PendingDeletion || !source.Enabled || source.Kind != KindDomain {
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
					consumers = append(consumers, domainProjectionConsumer{ruleID: rule.ID, targetID: targetID, egressID: rule.EgressID, rules: domainDomainRules(rulesForTarget)})
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
		consumers = append(consumers, domainProjectionConsumer{targetID: source.ID, egressID: source.EgressID, rules: domainDomainRules(rulesForTarget)})
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
