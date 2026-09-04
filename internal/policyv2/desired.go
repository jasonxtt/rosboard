package policyv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"unicode"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/ownership"
	"rosboard/internal/policy"
	"rosboard/internal/routeros"
)

type DesiredResult struct {
	Domain                   PolicyDomain
	Revision                 int64
	AccessRevision           int64
	AccessTargetIDs          []string
	InternetEgressCandidates map[string][]accesscontrol.InternetEgressCandidate
	Hash                     string
	Objects                  []DesiredObject
	Blockers                 []PlanIssue
	Warnings                 []PlanIssue
	AccessResolutions        []accesscontrol.MemberResolution
	TargetPromotions         []TargetVersionPromotion
	// Cross-domain state is kept out of the public desired object graph. It
	// binds a domain plan to the other domain's DNS projections and lets the
	// manager reconcile the device-global Access-before-Routing order without
	// making either domain delete the other's objects.
	crossDomainDesired     []DesiredObject
	crossDomainResolutions []CrossDomainProjectionResolution
	crossDomainConstraints []crossDomainDNSConstraint
}

func BuildDesired(ctx context.Context, repository Repository, reader PolicyReader) (DesiredResult, error) {
	return BuildDesiredWithAccess(ctx, repository, reader, nil, nil, nil)
}

type TerminalResolver func() []accesscontrol.Terminal

type ScopeResolver func() accesscontrol.Scope

type CanonicalAccessMigrator interface {
	EnsureCanonicalAccessMigrated(context.Context) error
}

func BuildDesiredWithAccess(ctx context.Context, repository Repository, reader PolicyReader, accessRepository accesscontrol.Repository, terminalResolver TerminalResolver, scopeResolver ScopeResolver) (DesiredResult, error) {
	return BuildDesiredWithAccessOptions(ctx, repository, reader, accessRepository, terminalResolver, scopeResolver, nil)
}

func BuildDesiredWithAccessOptions(ctx context.Context, repository Repository, reader PolicyReader, accessRepository accesscontrol.Repository, terminalResolver TerminalResolver, scopeResolver ScopeResolver, selectedInternetEgresses map[string][]string) (DesiredResult, error) {
	return buildDesiredForDomain(ctx, repository, reader, accessRepository, terminalResolver, scopeResolver, selectedInternetEgresses, PolicyDomainCombined)
}

func BuildRoutingDesired(ctx context.Context, repository Repository, reader PolicyReader, terminalResolver TerminalResolver) (DesiredResult, error) {
	return buildDesiredForDomain(ctx, repository, reader, nil, terminalResolver, nil, nil, PolicyDomainRouting)
}

func BuildAccessDesired(ctx context.Context, repository Repository, reader PolicyReader, accessRepository accesscontrol.Repository, terminalResolver TerminalResolver, scopeResolver ScopeResolver) (DesiredResult, error) {
	return BuildAccessDesiredWithOptions(ctx, repository, reader, accessRepository, terminalResolver, scopeResolver, nil)
}

func BuildAccessDesiredWithOptions(ctx context.Context, repository Repository, reader PolicyReader, accessRepository accesscontrol.Repository, terminalResolver TerminalResolver, scopeResolver ScopeResolver, selectedInternetEgresses map[string][]string) (DesiredResult, error) {
	return buildDesiredForDomain(ctx, repository, reader, accessRepository, terminalResolver, scopeResolver, selectedInternetEgresses, PolicyDomainAccess)
}

func buildDesiredForDomain(ctx context.Context, repository Repository, reader PolicyReader, accessRepository accesscontrol.Repository, terminalResolver TerminalResolver, scopeResolver ScopeResolver, selectedInternetEgresses map[string][]string, domain PolicyDomain) (DesiredResult, error) {
	return buildDesiredForDomainWithTargetScope(ctx, repository, reader, accessRepository, terminalResolver, scopeResolver, selectedInternetEgresses, domain, nil)
}

func buildDesiredForDomainWithTargetScope(ctx context.Context, repository Repository, reader PolicyReader, accessRepository accesscontrol.Repository, terminalResolver TerminalResolver, scopeResolver ScopeResolver, selectedInternetEgresses map[string][]string, domain PolicyDomain, targetScope map[string]bool) (DesiredResult, error) {
	routingDomain := domain == PolicyDomainRouting || domain == PolicyDomainCombined
	accessDomain := domain == PolicyDomainAccess || domain == PolicyDomainCombined
	managerID, err := repository.ManagerInstanceID(ctx)
	if err != nil {
		return DesiredResult{}, err
	}
	routingRepository, hasRoutingRepository := repository.(RoutingRuleRepository)
	routingRulesAuthoritative := false
	routingRules := []RoutingRule{}
	if routingDomain && hasRoutingRepository {
		if err := routingRepository.EnsureRoutingRulesMigrated(ctx); err != nil {
			return DesiredResult{}, err
		}
		authority, err := routingRepository.RoutingAuthority(ctx)
		if err != nil {
			return DesiredResult{}, err
		}
		routingRulesAuthoritative = authority == RoutingRuleAuthorityV1
		if routingRulesAuthoritative {
			routingRules, err = routingRepository.ListRoutingRules(ctx)
			if err != nil {
				return DesiredResult{}, err
			}
		}
	}
	state := DeviceState{}
	if routingDomain {
		state, err = repository.GetDeviceState(ctx)
		if err != nil {
			return DesiredResult{}, err
		}
	}
	egresses := []Egress{}
	if routingDomain {
		egresses, err = repository.ListEgresses(ctx)
		if err != nil {
			return DesiredResult{}, err
		}
	}
	sources, err := repository.ListSources(ctx, "")
	if err != nil {
		return DesiredResult{}, err
	}
	accessRules := []accesscontrol.AccessRule{}
	accessMembers := []accesscontrol.RuleMember{}
	var accessMigrationErr error
	// -1 means this is a policy-only plan without the access-control
	// repository. Zero is a real access-control revision for an empty device
	// and must still participate in the commit race check.
	accessRevision := int64(-1)
	if accessDomain && accessRepository != nil {
		if migrator, ok := accessRepository.(CanonicalAccessMigrator); ok {
			if err := migrator.EnsureCanonicalAccessMigrated(ctx); err != nil {
				accessMigrationErr = err
			}
		}
		accessRules, err = accessRepository.ListRules(ctx)
		if err != nil {
			return DesiredResult{}, err
		}
		accessMembers, err = accessRepository.ListMembers(ctx)
		if err != nil {
			return DesiredResult{}, err
		}
		accessState, stateErr := accessRepository.GetState(ctx)
		if stateErr != nil {
			return DesiredResult{}, stateErr
		}
		accessRevision = accessState.DesiredRevision
	}
	sourcesByEgress := map[string][]Source{}
	if routingDomain && !routingRulesAuthoritative {
		sourcesByEgress = enabledSourcesByEgress(sources)
	}
	deviceID := repository.DeviceID()
	accessTargetIDs := make([]string, 0)
	for _, rule := range accessRules {
		if !rule.Enabled || rule.TargetScope != accesscontrol.TargetScopeTargets {
			continue
		}
		accessTargetIDs = append(accessTargetIDs, rule.TargetListIDs...)
	}
	sort.Strings(accessTargetIDs)
	uniqueTargetIDs := accessTargetIDs[:0]
	for _, targetID := range accessTargetIDs {
		if len(uniqueTargetIDs) == 0 || uniqueTargetIDs[len(uniqueTargetIDs)-1] != targetID {
			uniqueTargetIDs = append(uniqueTargetIDs, targetID)
		}
	}
	result := DesiredResult{Domain: domain, Revision: state.DesiredRevision, AccessRevision: accessRevision, AccessTargetIDs: uniqueTargetIDs, InternetEgressCandidates: map[string][]accesscontrol.InternetEgressCandidate{}, Objects: []DesiredObject{}, Blockers: []PlanIssue{}, Warnings: []PlanIssue{}, AccessResolutions: []accesscontrol.MemberResolution{}, TargetPromotions: []TargetVersionPromotion{}}
	if accessMigrationErr != nil {
		result.Blockers = append(result.Blockers, PlanIssue{Code: "access_application_migration_required", Status: "blocker", Reason: accessMigrationErr.Error()})
	}
	order := 0
	addWithLabel := func(logicalID string, menu routeros.MutationMenu, phase, label string, fields map[string]string) {
		order++
		if menu != routeros.MenuRoutingTable {
			fields["comment"] = managedComment(managerID, deviceID, logicalID, label)
		}
		result.Objects = append(result.Objects, DesiredObject{Domain: domain, LogicalID: logicalID, Menu: string(menu), Fields: fields, Phase: phase, Order: order})
	}
	egressByID := make(map[string]Egress, len(egresses))
	for _, egress := range egresses {
		egressByID[egress.ID] = egress
	}
	sourceLists := make(map[string]string, len(sources))
	for _, source := range sources {
		sourceLists[source.ID] = SourceListName(managerID, repository.DeviceID(), source)
	}
	sourceByID := make(map[string]Source, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	accessForwarder := ""
	canonicalTargetSources := map[string]Source{}
	if accessDomain {
		canonicalTargetSources = canonicalAccessTargetSources(accessRules, sourceByID)
		if err := appendAccessDomainProjectionBlockers(ctx, repository, accessRules, canonicalTargetSources, targetScope, &result); err != nil {
			return DesiredResult{}, err
		}
	}
	if accessDomain && needsAccessForwarder(accessRules, canonicalTargetSources) {
		upstream, upstreamErr := accessDNSUpstream(ctx, reader)
		if upstreamErr != nil {
			result.Blockers = append(result.Blockers, PlanIssue{Code: "access_dns_upstream_unavailable", Status: "blocker", Reason: upstreamErr.Error()})
		} else {
			accessForwarder = ownership.Namespace(managerID, repository.DeviceID()) + "dns_" + shortHash("access-forwarder:"+managerID+":"+repository.DeviceID(), 10)
			addWithLabel("access-forwarder", routeros.MenuIPDNSForwarders, "dns", "访问规则 DNS 转发器", map[string]string{"name": accessForwarder, "dns-servers": upstream})
		}
	}
	accessTargetLists := make(map[string]string)
	accessTargetDisabled := make(map[string]bool)
	if accessDomain {
		activeAccessTargetIDs := enabledAccessTargetIDs(accessRules, canonicalTargetSources)
		for _, key := range sortedSourceKeys(canonicalTargetSources) {
			source := canonicalTargetSources[key]
			targetID := key
			versionID := targetVersionForPlan(source, targetScope)
			if targetPromotionAllowed(source.ID, targetScope) {
				appendTargetPromotion(&result.TargetPromotions, targetID, source.PendingVersionID)
			}
			if source.PendingDeletion || versionID == "" {
				continue
			}
			if source.Kind == KindDomain && accessForwarder == "" {
				continue
			}
			rules, err := allRules(ctx, repository, versionID)
			if err != nil {
				return DesiredResult{}, err
			}
			listName := AccessTargetListName(managerID, repository.DeviceID(), targetID)
			if len(rules) == 0 {
				continue
			}
			accessTargetLists[targetID] = listName
			accessTargetDisabled[targetID] = !activeAccessTargetIDs[targetID]
			for _, targetRule := range rules {
				if source.Kind == KindIP {
					family := ipRuleFamily(targetRule.RuleType)
					if family == "" {
						continue
					}
					menu := routeros.MenuIPFirewallAddressList
					if family == FamilyIPv6 {
						menu = routeros.MenuIPv6FirewallAddressList
					}
					addWithLabel("access-target:"+targetID+":"+targetRule.RuleType+":"+targetRule.Domain, menu, "foundation", "访问规则目标列表 · 地址条目", map[string]string{
						"list": listName, "address": targetRule.Domain, "disabled": routerBoolValue(!activeAccessTargetIDs[targetID]),
					})
					continue
				}
				if targetRule.RuleType != string(policy.RuleTypeExact) && targetRule.RuleType != string(policy.RuleTypeSuffix) {
					continue
				}
				matchSubdomain := "no"
				if targetRule.RuleType == string(policy.RuleTypeSuffix) {
					matchSubdomain = "yes"
				}
				addWithLabel("access-target-dns:"+targetID+":"+targetRule.RuleType+":"+targetRule.Domain, routeros.MenuIPDNSStatic, "dns", "访问规则目标列表 · DNS 静态规则", map[string]string{
					"name": targetRule.Domain, "type": "FWD", "forward-to": accessForwarder, "address-list": listName,
					"disabled": routerBoolValue(!activeAccessTargetIDs[targetID]), "match-subdomain": matchSubdomain,
				})
			}
		}
	}
	ingressList := ManagedIngressListName(managerID, repository.DeviceID())
	hasEgress := hasEnabledEgress(egresses)
	requiresTrafficIngress := hasEgress
	if routingRulesAuthoritative {
		requiresTrafficIngress = false
		for _, rule := range routingRules {
			if !rule.Enabled {
				continue
			}
			if rule.Subject.Mode == SubjectModeAll || rule.Subject.Mode == SubjectModeExcluded {
				requiresTrafficIngress = true
				break
			}
		}
	}
	ingress, ingressErr := ParseTrafficIngressScope(state.TrafficIngress)
	if routingDomain && !routingRulesAuthoritative && ingressErr != nil {
		if requiresTrafficIngress {
			result.Blockers = append(result.Blockers, PlanIssue{Code: "invalid_traffic_ingress", Status: "blocker", Reason: ingressErr.Error()})
		}
	} else if routingDomain && !routingRulesAuthoritative && len(ingress.InterfaceLists) == 0 && len(ingress.Interfaces) == 0 {
		if requiresTrafficIngress {
			result.Blockers = append(result.Blockers, PlanIssue{Code: "traffic_ingress_required", Status: "blocker", Reason: "至少选择一个策略流量入口"})
		}
	} else if routingDomain && !routingRulesAuthoritative && hasEgress {
		fields := map[string]string{"name": ingressList}
		if len(ingress.InterfaceLists) > 0 {
			fields["include"] = strings.Join(ingress.InterfaceLists, ",")
		}
		addWithLabel("traffic-ingress:list", routeros.MenuInterfaceList, "foundation", "策略流量入口聚合列表", fields)
		for _, interfaceName := range ingress.Interfaces {
			addWithLabel("traffic-ingress:member:"+interfaceName, routeros.MenuInterfaceListMember, "foundation", "策略流量入口成员 "+cleanReadableLabel(interfaceName), map[string]string{"list": ingressList, "interface": interfaceName})
		}
	}

	terminals := []accesscontrol.Terminal{}
	if terminalResolver != nil {
		terminals = terminalResolver()
	}
	if routingDomain && routingRulesAuthoritative {
		// Keep the global scope available for stable naming of a migrated rule
		// that matches it, but do not use it as a runtime fallback.
		if err := buildRoutingDesiredWithTargetScope(ctx, &result, addWithLabel, repository, reader, managerID, repository.DeviceID(), ingress, false, egresses, sources, routingRules, terminals, targetScope); err != nil {
			return DesiredResult{}, err
		}
	}
	if !routingRulesAuthoritative {
		for _, egress := range egresses {
			if egress.PendingDeletion {
				continue
			}
			disabled := "no"
			if !egress.Enabled {
				disabled = "yes"
			}
			mode := firstNonEmptyString(egress.ListMode, ListModeShared)
			if mode != ListModeShared && mode != ListModeDedicated {
				result.Blockers = append(result.Blockers, issue("invalid_list_mode", "", egress.ID, "不支持的标记列表模式"))
				continue
			}
			families := enabledFamilies(egress.Families)
			if len(families) == 0 {
				result.Blockers = append(result.Blockers, issue("family_required", "", egress.ID, "至少启用一个地址族"))
				continue
			}
			strategyLabel := "策略 " + cleanReadableLabel(egress.Name)
			addEgress := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
				addWithLabel(logicalID, menu, phase, strategyLabel+" · "+managedObjectPurpose(logicalID), fields)
			}

			listBySource := make(map[string]string)
			for _, source := range sourcesByEgress[egress.ID] {
				listBySource[source.ID] = sourceLists[source.ID]
			}

			// DNS objects exist only while the egress has an applicable domain
			// source; IP-only egresses route purely through static address-list
			// entries and must not be blocked by DNS transport configuration.
			dnsEnabled := hasApplicableDomainSource(sourcesByEgress[egress.ID])
			if dnsEnabled {
				alias := firstNonEmptyString(egress.FakeAlias, deterministicFakeAliasForEgress(egress))
				upstream := firstNonEmptyString(egress.DNSUpstream, "1.1.1.1")
				aliasIP, aliasErr := netip.ParseAddr(alias)
				upstreamIP, upstreamErr := netip.ParseAddr(upstream)
				if aliasErr != nil || upstreamErr != nil || aliasIP.Is4() != upstreamIP.Is4() {
					result.Blockers = append(result.Blockers, issue("invalid_dns_transport", "", egress.ID, "Fake DNS 别名和 DNS 上游必须是同一地址族的 IP"))
					continue
				}
				forwarder := "rosboard_" + shortHash("forwarder:"+egress.ID, 10)
				addEgress("forwarder:"+egress.ID, routeros.MenuIPDNSForwarders, "dns", map[string]string{"name": forwarder, "dns-servers": alias})
				for _, source := range sourcesByEgress[egress.ID] {
					if source.Kind == KindIP {
						continue
					}
					versionID := targetVersionForPlan(source, targetScope)
					if versionID == "" {
						result.Warnings = append(result.Warnings, PlanIssue{Code: "source_has_no_version", Status: "warning", EgressID: egress.ID, Reason: "域名来源尚无可应用版本：" + source.Name})
						continue
					}
					rules, err := allRules(ctx, repository, versionID)
					if err != nil {
						return DesiredResult{}, err
					}
					for _, rule := range rules {
						fields := map[string]string{"name": rule.Domain, "type": "FWD", "forward-to": forwarder, "address-list": listBySource[source.ID], "disabled": disabled, "match-subdomain": "no"}
						if rule.RuleType == "DOMAIN-SUFFIX" {
							fields["match-subdomain"] = "yes"
						}
						logicalID := "dns:" + egress.ID + ":" + source.ID + ":" + rule.RuleType + ":" + rule.Domain
						label := strategyLabel + " · 域名列表 " + cleanReadableLabel(source.Name) + " · DNS 静态规则"
						addWithLabel(logicalID, routeros.MenuIPDNSStatic, "dns", label, fields)
					}
				}
				dnsTable := ""
				for _, family := range families {
					if aliasIP.Is4() == (family.Family == FamilyIPv4) {
						dnsTable = firstNonEmptyString(family.RouteTable, DefaultRouteTable(managerID, repository.DeviceID(), egress.ID, family.Family))
						break
					}
				}
				if dnsTable == "" {
					result.Blockers = append(result.Blockers, issue("dns_transport_family_missing", "", egress.ID, "DNS 上游地址族没有对应的已启用出口"))
					continue
				}
				addDNSTransport(addEgress, egress.ID, alias, upstream, dnsTable, aliasIP.Is4(), disabled)
			}

			// IP sources materialize as static address-list entries per enabled
			// family; rules of a disabled family are ignored, not blockers.
			for _, source := range sourcesByEgress[egress.ID] {
				if source.Kind != KindIP {
					continue
				}
				versionID := targetVersionForPlan(source, targetScope)
				if versionID == "" {
					result.Warnings = append(result.Warnings, PlanIssue{Code: "source_has_no_version", Status: "warning", EgressID: egress.ID, Reason: "IP 列表尚无可应用版本：" + source.Name})
					continue
				}
				rules, err := allRules(ctx, repository, versionID)
				if err != nil {
					return DesiredResult{}, err
				}
				for _, rule := range rules {
					family := ipRuleFamily(rule.RuleType)
					if family == "" || !familyEnabled(families, family) {
						continue
					}
					menu := routeros.MenuIPFirewallAddressList
					if family == FamilyIPv6 {
						menu = routeros.MenuIPv6FirewallAddressList
					}
					logicalID := "source-addr:" + source.ID + ":" + rule.RuleType + ":" + rule.Domain
					label := strategyLabel + " · IP 列表 " + cleanReadableLabel(source.Name) + " · 地址条目"
					addWithLabel(logicalID, menu, "foundation", label, map[string]string{
						"list":     listBySource[source.ID],
						"address":  rule.Domain,
						"disabled": disabled,
					})
				}
			}

			for _, family := range families {
				gateway := strings.TrimSpace(family.Gateway)
				if gateway == "" && family.WANSource != "next-hop" {
					resolution, resolveErr := ResolveGateway(ctx, reader, family)
					if resolveErr != nil {
						result.Blockers = append(result.Blockers, issue("gateway_discovery_failed", string(family.Family), egress.ID, "无法读取该出口的下一跳网关："+resolveErr.Error()))
						continue
					}
					if resolution.PointToPoint {
						gateway = strings.TrimSpace(family.WANInterface)
					} else if resolution.Gateway != "" {
						gateway = resolution.Gateway
					} else if len(resolution.Candidates) > 1 {
						result.Blockers = append(result.Blockers, issue("gateway_ambiguous", string(family.Family), egress.ID, "该出口发现了多个可能的下一跳网关，请手动选择"))
						continue
					} else {
						result.Blockers = append(result.Blockers, issue("gateway_required", string(family.Family), egress.ID, "普通出口接口未发现明确下一跳网关，请手动填写网关 IP"))
						continue
					}
				}
				buildFamily(&result, addEgress, egress, family, gateway, listBySource, ingressList, managerID, repository.DeviceID(), disabled)
			}
		}
	}

	scope := accesscontrol.Scope{}
	if scopeResolver != nil {
		scope = scopeResolver()
	}
	internetEgresses := map[string][]string{}
	internetEgressIssues := map[string]string{}
	if hasInternetAccessRules(accessRules) {
		discovery := discoverInternetEgressesDetailed(ctx, reader, scope)
		internetEgresses, internetEgressIssues = discovery.Egresses, discovery.Issues
		result.InternetEgressCandidates = discovery.Candidates
		if selectedInternetEgresses != nil {
			internetEgresses, internetEgressIssues = selectInternetEgresses(discovery, selectedInternetEgresses)
		}
	}
	if accessDomain {
		accessDesired := accesscontrol.BuildDesired(accesscontrol.DesiredInput{
			ManagerID: managerID, DeviceID: repository.DeviceID(), Rules: accessRules, Members: accessMembers,
			Terminals: terminals, Scope: scope,
			TargetList: accessTargetLists, TargetListDisabled: accessTargetDisabled,
			InternetEgresses: internetEgresses, InternetEgressIssues: internetEgressIssues,
		})
		result.AccessResolutions = append(result.AccessResolutions, accessDesired.Resolutions...)
		for _, blocker := range accessDesired.Blockers {
			result.Blockers = append(result.Blockers, PlanIssue{Code: blocker.Code, Status: "blocker", Family: blocker.Family, LogicalID: blocker.RuleID, Reason: blocker.Reason})
		}
		for _, issue := range accessDesired.Issues {
			result.Warnings = append(result.Warnings, PlanIssue{Code: issue.Code, Status: "warning", Family: issue.Family, LogicalID: issue.RuleID, Reason: issue.Reason})
		}
		for _, object := range accessDesired.Objects {
			order++
			result.Objects = append(result.Objects, DesiredObject{Domain: domain, LogicalID: object.LogicalID, Menu: string(object.Menu), Fields: object.Fields, Phase: object.Phase, Order: order})
		}
	}
	appendDNSCacheWarning(&result)
	sort.SliceStable(result.Objects, func(i, j int) bool { return result.Objects[i].Order < result.Objects[j].Order })
	if err := hashDesiredResult(&result); err != nil {
		return DesiredResult{}, err
	}
	return result, nil
}

func hashDesiredResult(result *DesiredResult) error {
	payload, err := json.Marshal(struct {
		Objects                []DesiredObject                   `json:"Objects"`
		AccessResolutions      []accesscontrol.MemberResolution  `json:"AccessResolutions"`
		CrossDomainDesired     []DesiredObject                   `json:"CrossDomainDesired,omitempty"`
		CrossDomainResolutions []CrossDomainProjectionResolution `json:"CrossDomainResolutions,omitempty"`
		CrossDomainConstraints []crossDomainDNSConstraint        `json:"CrossDomainConstraints,omitempty"`
	}{
		Objects:                result.Objects,
		AccessResolutions:      result.AccessResolutions,
		CrossDomainDesired:     result.crossDomainDesired,
		CrossDomainResolutions: result.crossDomainResolutions,
		CrossDomainConstraints: result.crossDomainConstraints,
	})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	result.Hash = hex.EncodeToString(digest[:])
	return nil
}

func needsAccessForwarder(rules []accesscontrol.AccessRule, targetSources map[string]Source) bool {
	for _, rule := range rules {
		if rule.TargetScope != accesscontrol.TargetScopeTargets || !rule.Enabled {
			continue
		}
		for _, targetID := range rule.TargetListIDs {
			targetID = strings.TrimSpace(targetID)
			source := targetSources[targetID]
			if source.ID == "" {
				// Keep narrow callers that still provide the pre-Slice-4C
				// rule/target map shape source-compatible.
				source = targetSources[accesscontrol.AccessTargetKey(rule.ID, targetID)]
			}
			if source.Kind == KindDomain && !source.PendingDeletion {
				return true
			}
		}
	}
	return false
}

// canonicalAccessTargetSources keeps targets required by enabled rules. A
// target referenced only by disabled rules is omitted and reconciled away;
// TargetList.Enabled is a legacy field and does not gate this projection.
func canonicalAccessTargetSources(rules []accesscontrol.AccessRule, sources map[string]Source) map[string]Source {
	result := make(map[string]Source)
	for _, rule := range rules {
		if !rule.Enabled || rule.TargetScope != accesscontrol.TargetScopeTargets {
			continue
		}
		for _, targetID := range rule.TargetListIDs {
			targetID = strings.TrimSpace(targetID)
			if source, ok := sources[targetID]; ok {
				result[targetID] = source
			}
		}
	}
	return result
}

func accessTargetConsumerActive(rule accesscontrol.AccessRule, source Source) bool {
	return rule.Enabled && rule.TargetScope == accesscontrol.TargetScopeTargets && source.ID != "" && !source.PendingDeletion
}

func enabledAccessTargetIDs(rules []accesscontrol.AccessRule, sources map[string]Source) map[string]bool {
	result := make(map[string]bool)
	for _, rule := range rules {
		if rule.TargetScope != accesscontrol.TargetScopeTargets {
			continue
		}
		for _, targetID := range rule.TargetListIDs {
			targetID = strings.TrimSpace(targetID)
			source, ok := sources[targetID]
			if !ok {
				// Keep narrow callers that still provide the pre-Slice-4C
				// rule/target map shape source-compatible.
				source = sources[accesscontrol.AccessTargetKey(rule.ID, targetID)]
			}
			if accessTargetConsumerActive(rule, source) {
				result[targetID] = true
			}
		}
	}
	return result
}

func sortedSourceKeys(values map[string]Source) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func routerBoolValue(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func hasInternetAccessRules(rules []accesscontrol.AccessRule) bool {
	for _, rule := range rules {
		if rule.TargetScope == accesscontrol.TargetScopeInternet {
			return true
		}
	}
	return false
}

func accessDNSUpstream(ctx context.Context, reader PolicyReader) (string, error) {
	objects, err := reader.PolicyList(ctx, routeros.ReadMenuIPDNS, []string{"servers", "dynamic-servers"})
	if err != nil {
		return "", fmt.Errorf("读取 RouterOS DNS 上游失败: %w", err)
	}
	for _, field := range []string{"servers", "dynamic-servers"} {
		for _, object := range objects {
			for _, candidate := range strings.Split(object[field], ",") {
				address, parseErr := netip.ParseAddr(strings.TrimSpace(candidate))
				if parseErr == nil && address.Zone() == "" && !address.IsUnspecified() && !address.IsLoopback() && !address.IsMulticast() {
					return address.String(), nil
				}
			}
		}
	}
	return "", errors.New("RouterOS /ip/dns 未提供可用的 servers 或 dynamic-servers")
}

func appendDNSCacheWarning(result *DesiredResult) {
	if result == nil {
		return
	}
	dnsStaticCount := 0
	for _, object := range result.Objects {
		if object.Menu == string(routeros.MenuIPDNSStatic) {
			dnsStaticCount++
		}
	}
	if dnsStaticCount <= 1000 {
		return
	}
	result.Warnings = append(result.Warnings, PlanIssue{
		Code:   "dns_cache_capacity",
		Status: "warning",
		Reason: fmt.Sprintf("本次将应用 %d 条 DNS 静态规则；默认 DNS cache 上限为 32MiB，可能不足。若 RouterOS 日志出现 cache full, not storing，请适当增大 cache-size。", dnsStaticCount),
	})
}

func buildFamily(result *DesiredResult, add func(string, routeros.MutationMenu, string, map[string]string), egress Egress, family EgressFamily, gateway string, listBySource map[string]string, lanList, managerID, deviceID, disabled string) {
	familyName := string(family.Family)
	if family.Family != FamilyIPv4 && family.Family != FamilyIPv6 {
		result.Blockers = append(result.Blockers, issue("invalid_family", familyName, egress.ID, "不支持的地址族"))
		return
	}
	gateway = strings.TrimSpace(gateway)
	if gateway == "" {
		result.Blockers = append(result.Blockers, issue("route_incomplete", familyName, egress.ID, "出口接口或下一跳网关不能为空"))
		return
	}
	autoTable := DefaultRouteTable(managerID, deviceID, egress.ID, family.Family)
	table := firstNonEmptyString(family.RouteTable, autoTable)
	routeMode := firstNonEmptyString(family.RouteMode, egress.FailureMode, "strict")
	if routeMode != "strict" && routeMode != "fallback" && routeMode != "existing" {
		result.Blockers = append(result.Blockers, issue("invalid_route_mode", familyName, egress.ID, "不支持的路由模式"))
		return
	}
	isMain := strings.EqualFold(table, "main")
	if isMain && routeMode == "strict" {
		result.Blockers = append(result.Blockers, issue("main_table_strict_invalid", familyName, egress.ID, "main 路由表不能提供严格断线阻断"))
		return
	}
	if !isMain && table == autoTable {
		add("table:"+egress.ID+":"+familyName, routeros.MenuRoutingTable, "foundation", map[string]string{"name": table, "fib": "yes"})
	}
	if !isMain {
		routeMenu, destination := routeros.MenuIPRoute, "0.0.0.0/0"
		if family.Family == FamilyIPv6 {
			routeMenu, destination = routeros.MenuIPv6Route, "::/0"
		}
		add("route:"+egress.ID+":"+familyName, routeMenu, "routing", map[string]string{"dst-address": destination, "gateway": gateway, "routing-table": table, "distance": "1", "disabled": disabled})
		if routeMode == "strict" {
			add("blackhole:"+egress.ID+":"+familyName, routeMenu, "routing", map[string]string{"dst-address": destination, "routing-table": table, "blackhole": "yes", "distance": "254", "disabled": disabled})
		}
		if routeMode == "fallback" {
			add("fallback:"+egress.ID+":"+familyName, routeros.MenuRoutingRule, "routing", map[string]string{"action": "lookup", "table": table, "routing-mark": table, "disabled": disabled})
			result.Warnings = append(result.Warnings, PlanIssue{Code: "fallback_main_table", Status: "warning", Family: familyName, EgressID: egress.ID, Reason: "WAN 失效时将回落 main 路由表"})
		}
	}

	mangleMenu := routeros.MenuIPFirewallMangle
	if family.Family == FamilyIPv6 {
		mangleMenu = routeros.MenuIPv6FirewallMangle
	}
	if firstNonEmptyString(egress.ListMode, ListModeShared) == ListModeShared {
		buildSharedFamily(add, egress, familyName, mangleMenu, listBySource, lanList, table, disabled)
		return
	}
	for _, sourceID := range sortedKeys(listBySource) {
		listName := listBySource[sourceID]
		identity := egress.ID + ":" + familyName + ":" + listName
		connectionMark := "rb_" + shortHash("connection:"+identity, 12)
		add("lan-connection:"+identity, mangleMenu, "activation", map[string]string{"chain": "prerouting", "in-interface-list": lanList, "dst-address-type": "!local", "connection-state": "new", "connection-mark": "no-mark", "dst-address-list": listName, "action": "mark-connection", "new-connection-mark": connectionMark, "passthrough": "yes", "disabled": disabled})
		add("lan-routing:"+identity, mangleMenu, "activation", map[string]string{"chain": "prerouting", "in-interface-list": lanList, "dst-address-type": "!local", "connection-mark": connectionMark, "action": "mark-routing", "new-routing-mark": table, "passthrough": "no", "disabled": disabled})
		if egress.RouterOutput {
			routerMark := "rb_" + shortHash("router:"+identity, 12)
			add("router-connection:"+identity, mangleMenu, "activation", map[string]string{"chain": "output", "dst-address-type": "!local", "connection-state": "new", "connection-mark": "no-mark", "dst-address-list": listName, "action": "mark-connection", "new-connection-mark": routerMark, "passthrough": "yes", "disabled": disabled})
			add("router-routing:"+identity, mangleMenu, "activation", map[string]string{"chain": "output", "connection-mark": routerMark, "action": "mark-routing", "new-routing-mark": table, "passthrough": "no", "disabled": disabled})
		}
	}
}

// buildSharedFamily 为 shared 模式生成“多来源共用一个连接标记”的 mangle 规则。
// 标记身份只绑定出口与地址族（与可编辑的列表名无关），
// 因此重命名列表不会重建连接标记规则。
func buildSharedFamily(add func(string, routeros.MutationMenu, string, map[string]string), egress Egress, familyName string, menu routeros.MutationMenu, listBySource map[string]string, lanList, table, disabled string) {
	if len(listBySource) == 0 {
		return
	}
	identity := egress.ID + ":" + familyName
	connectionMark := "rb_" + shortHash("connection:"+identity, 12)
	for _, sourceID := range sortedKeys(listBySource) {
		listName := listBySource[sourceID]
		sourceIdentity := identity + ":" + sourceID
		add("lan-source-connection:"+sourceIdentity, menu, "activation", map[string]string{"chain": "prerouting", "in-interface-list": lanList, "dst-address-type": "!local", "connection-state": "new", "connection-mark": "no-mark", "dst-address-list": listName, "action": "mark-connection", "new-connection-mark": connectionMark, "passthrough": "yes", "disabled": disabled})
	}
	add("lan-routing:"+identity, menu, "activation", map[string]string{"chain": "prerouting", "in-interface-list": lanList, "dst-address-type": "!local", "connection-mark": connectionMark, "action": "mark-routing", "new-routing-mark": table, "passthrough": "no", "disabled": disabled})
	if !egress.RouterOutput {
		return
	}
	routerMark := "rb_" + shortHash("router:"+identity, 12)
	for _, sourceID := range sortedKeys(listBySource) {
		listName := listBySource[sourceID]
		sourceIdentity := identity + ":" + sourceID
		add("router-source-connection:"+sourceIdentity, menu, "activation", map[string]string{"chain": "output", "dst-address-type": "!local", "connection-state": "new", "connection-mark": "no-mark", "dst-address-list": listName, "action": "mark-connection", "new-connection-mark": routerMark, "passthrough": "yes", "disabled": disabled})
	}
	add("router-routing:"+identity, menu, "activation", map[string]string{"chain": "output", "connection-mark": routerMark, "action": "mark-routing", "new-routing-mark": table, "passthrough": "no", "disabled": disabled})
}

func addDNSTransport(add func(string, routeros.MutationMenu, string, map[string]string), egressID, alias, upstream, routeTable string, ipv4 bool, disabled string) {
	mangleMenu, natMenu := routeros.MenuIPFirewallMangle, routeros.MenuIPFirewallNAT
	toField := "to-addresses"
	if !ipv4 {
		mangleMenu, natMenu, toField = routeros.MenuIPv6FirewallMangle, routeros.MenuIPv6FirewallNAT, "to-address"
	}
	for _, protocol := range []string{"udp", "tcp"} {
		add("dns-mark:"+egressID+":"+protocol, mangleMenu, "activation", map[string]string{"chain": "output", "protocol": protocol, "dst-address": alias, "dst-port": "53", "action": "mark-routing", "new-routing-mark": routeTable, "passthrough": "no", "disabled": disabled})
		fields := map[string]string{"chain": "output", "protocol": protocol, "dst-address": alias, "dst-port": "53", "action": "dst-nat", toField: upstream, "to-ports": "53", "disabled": disabled}
		add("dns-dnat:"+egressID+":"+protocol, natMenu, "activation", fields)
	}
}

func enabledSourcesByEgress(sources []Source) map[string][]Source {
	result := make(map[string][]Source)
	for _, source := range sources {
		if source.Enabled && !source.PendingDeletion && source.EgressID != "" {
			result[source.EgressID] = append(result[source.EgressID], source)
		}
	}
	for id := range result {
		sort.Slice(result[id], func(i, j int) bool { return result[id][i].ID < result[id][j].ID })
	}
	return result
}

// hasApplicableDomainSource reports whether the egress still has an enabled
// domain source with a pending or active version; only then are DNS objects
// built for it.
func hasApplicableDomainSource(sources []Source) bool {
	for _, source := range sources {
		if source.Kind == KindIP {
			continue
		}
		if firstNonEmptyString(source.PendingVersionID, source.ActiveVersionID) != "" {
			return true
		}
	}
	return false
}

// ipRuleFamily maps a stored IP rule type to its address family; an empty
// result marks an unknown rule type that must not be materialized.
func ipRuleFamily(ruleType string) AddressFamily {
	switch ruleType {
	case "IP-CIDR":
		return FamilyIPv4
	case "IP-CIDR6":
		return FamilyIPv6
	default:
		return ""
	}
}

func familyEnabled(families []EgressFamily, family AddressFamily) bool {
	for _, candidate := range families {
		if candidate.Family == family {
			return true
		}
	}
	return false
}

func enabledFamilies(families []EgressFamily) []EgressFamily {
	result := make([]EgressFamily, 0, 2)
	for _, family := range families {
		if family.Enabled {
			result = append(result, family)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Family < result[j].Family })
	return result
}

func issue(code, family, egressID, reason string) PlanIssue {
	return PlanIssue{Code: code, Status: "blocker", Family: family, EgressID: egressID, Reason: reason}
}

func hasEnabledEgress(egresses []Egress) bool {
	for _, egress := range egresses {
		if !egress.PendingDeletion {
			return true
		}
	}
	return false
}

// SharedListName returns the stable address-list name used by a shared egress.
func SharedListName(egressName string) string {
	return "manual_" + readableNameKey(egressName, "policy", 48) + "_lab"
}

func dedicatedListName(source Source) string {
	return "manual_" + readableNameKey(source.Name, "source", 12) + "_" + shortHash("source:"+source.ID, 6) + "_lab"
}

// SourceListName returns the physical address-list shared by policy routing
// and access control for one source on one RouterOS device. The readable
// source name belongs in object comments; keeping it out of the list identity
// makes source renames non-disruptive.
func SourceListName(managerID, deviceID string, source Source) string {
	return "rb_src_" + shortHash("source-list:"+managerID+":"+deviceID+":"+source.ID, 12)
}

// AccessTargetListName is the stable physical projection for one canonical
// target list on one device. The optional second argument preserves the old
// (manager, device, rule, target) call shape while deliberately ignoring the
// obsolete rule identity.
func AccessTargetListName(managerID, deviceID string, targetIDs ...string) string {
	targetID := ""
	if len(targetIDs) == 1 {
		targetID = targetIDs[0]
	} else if len(targetIDs) >= 2 {
		targetID = targetIDs[1]
	}
	return ownership.Namespace(managerID, deviceID) + "target_" + shortHash("access-target:"+managerID+":"+deviceID+":"+strings.TrimSpace(targetID), 12)
}

func deterministicFakeAlias(egressID string) string {
	digest := sha256.Sum256([]byte("fake-alias:" + egressID))
	return fmt.Sprintf("192.0.2.%d", int(digest[0])%254+1)
}

func deterministicFakeAliasForEgress(egress Egress) string {
	upstream, err := netip.ParseAddr(strings.TrimSpace(egress.DNSUpstream))
	if err == nil && !upstream.Is4() {
		digest := sha256.Sum256([]byte("fake-alias:" + egress.ID))
		host := uint16(digest[0])<<8 | uint16(digest[1])
		if host == 0 {
			host = 1
		}
		return fmt.Sprintf("2001:db8::%x", host)
	}
	return deterministicFakeAlias(egress.ID)
}

func uniqueSortedValues(values map[string]string) []string {
	seen := make(map[string]bool)
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedKeys(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func allRules(ctx context.Context, repository Repository, versionID string) ([]SourceRule, error) {
	query := RuleQuery{Limit: 1000}
	result := make([]SourceRule, 0)
	for {
		page, hasNext, err := repository.ListSourceRules(ctx, versionID, query)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if !hasNext || len(page) == 0 {
			return result, nil
		}
		query.AfterType, query.AfterDomain = page[len(page)-1].RuleType, page[len(page)-1].Domain
	}
}

// managedCommentIdentityPrefix marks RouterOS objects owned by this feature.
// The complete canonical identity also includes the installation/device scope
// and the logical object hash; the short prefix remains operator-readable.
const managedCommentIdentityPrefix = ownership.Prefix
const legacyScopedCommentIdentityPrefix = ownership.LegacyPrefix

const legacyManagedCommentNamespace = "rosboard:v2:"

// legacyManagedCommentPrefix reproduces the pre-shortening comment prefix so
// identities written by older builds stay recognizable and can be migrated in
// place by the reconciler.
func legacyManagedCommentPrefix(managerID, deviceID string) string {
	return legacyManagedCommentNamespace + shortHash(managerID, 12) + ":" + shortHash(deviceID, 12) + ":"
}

// isManagedComment reports whether a RouterOS comment has a recognized
// policy-routing identity shape.
func isManagedComment(comment string) bool {
	identity := managedCommentIdentity(comment)
	if ownership.IsCanonical(identity) || ownership.IsLegacyScoped(identity) || ownership.IsUnscopedLegacy(identity) {
		return true
	}
	if strings.HasPrefix(identity, legacyScopedCommentIdentityPrefix+"v1_") {
		parts := strings.Split(strings.TrimPrefix(identity, legacyScopedCommentIdentityPrefix+"v1_"), "_")
		return len(parts) == 3 && hasLowerHex(parts[0], 12) && hasLowerHex(parts[1], 12) && hasLowerHex(parts[2], 8)
	}
	if strings.HasPrefix(identity, managedCommentIdentityPrefix) {
		return hasLowerHex(strings.TrimPrefix(identity, managedCommentIdentityPrefix), 8)
	}
	if !strings.HasPrefix(identity, legacyManagedCommentNamespace) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(identity, legacyManagedCommentNamespace), ":")
	return len(parts) == 3 && hasLowerHex(parts[0], 12) && hasLowerHex(parts[1], 12) && hasLowerHex(parts[2], 16)
}

func legacyManagedCommentPrefixV1(managerID, deviceID string) string {
	return legacyScopedCommentIdentityPrefix + "v1_" + shortHash("manager:"+managerID, 12) + "_" + shortHash("device:"+deviceID, 12) + "_"
}

func isManagedCommentFor(managerID, deviceID, comment string) bool {
	identity := managedCommentIdentity(comment)
	if ownership.IsCanonicalFor(managerID, deviceID, identity) || ownership.IsLegacyScopedFor(managerID, deviceID, identity) {
		return true
	}
	if strings.HasPrefix(identity, legacyManagedCommentPrefixV1(managerID, deviceID)) {
		return isManagedComment(identity)
	}
	legacyPrefix := legacyManagedCommentPrefix(managerID, deviceID)
	return strings.HasPrefix(identity, legacyPrefix) && hasLowerHex(strings.TrimPrefix(identity, legacyPrefix), 16)
}

func hasLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func ManagedTablePrefix(managerID, deviceID string) string {
	return "rb_" + shortHash(managerID, 6) + shortHash(deviceID, 6) + "_"
}

func DefaultRouteTable(managerID, deviceID, egressID string, family AddressFamily) string {
	suffix := "4"
	if family == FamilyIPv6 {
		suffix = "6"
	}
	return ManagedTablePrefix(managerID, deviceID) + shortHash(egressID, 8) + suffix
}

func managedComment(managerID, deviceID, logicalID string, labels ...string) string {
	identity := ownership.Identity(managerID, deviceID, logicalID)
	if len(labels) == 0 {
		return identity
	}
	label := cleanReadableLabel(labels[0])
	if label == "" {
		return identity
	}
	return identity + " | " + label
}

func managedCommentIdentity(comment string) string {
	return ownership.CommentIdentity(comment)
}

func managedObjectPurpose(logicalID string) string {
	prefix := logicalID
	if index := strings.IndexByte(prefix, ':'); index >= 0 {
		prefix = prefix[:index]
	}
	switch prefix {
	case "forwarder":
		return "DNS 转发器"
	case "addr":
		return "地址列表条目"
	case "source-addr":
		return "来源地址列表条目"
	case "dns-mark":
		return "DNS 路由标记"
	case "dns-dnat":
		return "DNS 目标转换"
	case "table":
		return "路由表"
	case "route":
		return "默认路由"
	case "blackhole":
		return "断线阻断路由"
	case "fallback":
		return "主线路回落规则"
	case "lan-connection":
		return "入口连接标记"
	case "lan-source-connection":
		return "来源入口连接标记"
	case "lan-routing":
		return "入口路由标记"
	case "router-connection":
		return "路由器连接标记"
	case "router-source-connection":
		return "来源路由器连接标记"
	case "router-routing":
		return "路由器路由标记"
	case "masquerade":
		return "出口地址转换"
	case "routing-target-addr":
		return "路由目标地址"
	case "routing-dns":
		return "路由目标 DNS"
	case "routing-rule-connection":
		return "路由连接标记"
	case "routing-rule-routing":
		return "路由策略执行组"
	case "routing-router-connection":
		return "路由器连接标记"
	case "routing-router-routing":
		return "路由器路由标记"
	case "routing-subject":
		return "路由来源地址列表"
	default:
		return "受管配置"
	}
}

func cleanReadableLabel(value string) string {
	value = strings.Join(strings.Fields(strings.ReplaceAll(value, "|", "/")), " ")
	runes := []rune(value)
	if len(runes) > 96 {
		runes = runes[:96]
	}
	return string(runes)
}

func readableNameKey(value, fallback string, maxRunes int) string {
	var result []rune
	separator := false
	for _, character := range strings.TrimSpace(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result = append(result, unicode.ToLower(character))
			separator = false
		} else if len(result) > 0 && !separator {
			result = append(result, '_')
			separator = true
		}
		if len(result) >= maxRunes {
			break
		}
	}
	for len(result) > 0 && result[len(result)-1] == '_' {
		result = result[:len(result)-1]
	}
	if len(result) == 0 {
		return fallback
	}
	return string(result)
}

func shortHash(value string, length int) string {
	digest := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(digest[:])
	if length > len(encoded) {
		return encoded
	}
	return encoded[:length]
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func targetVersionForPlan(source Source, targetScope map[string]bool) string {
	if targetScope != nil && !targetScope[source.ID] {
		return strings.TrimSpace(source.ActiveVersionID)
	}
	return firstNonEmptyString(source.PendingVersionID, source.ActiveVersionID)
}

func targetPromotionAllowed(targetID string, targetScope map[string]bool) bool {
	return targetScope == nil || targetScope[targetID]
}
