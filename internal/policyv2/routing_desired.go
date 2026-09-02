package policyv2

import (
	"context"
	"net/netip"
	"sort"
	"strings"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/routeros"
	"rosboard/internal/subject"
)

type routingTargetProjection struct {
	id     string
	source Source
	rules  []SourceRule
	list   string
	active bool
}

type routingExecutionGroup struct {
	boundary    string
	subjectList string
	ingressList string
	mark        string
	logicalID   string
	enabled     bool
}

func buildRoutingDesired(ctx context.Context, result *DesiredResult, add func(string, routeros.MutationMenu, string, string, map[string]string), repository Repository, reader PolicyReader, managerID, deviceID string, defaultIngress TrafficIngressScope, allowDefaultIngress bool, egresses []Egress, sources []Source, rules []RoutingRule, terminals []accesscontrol.Terminal) error {
	return buildRoutingDesiredWithTargetScope(ctx, result, add, repository, reader, managerID, deviceID, defaultIngress, allowDefaultIngress, egresses, sources, rules, terminals, nil)
}

func buildRoutingDesiredWithTargetScope(ctx context.Context, result *DesiredResult, add func(string, routeros.MutationMenu, string, string, map[string]string), repository Repository, reader PolicyReader, managerID, deviceID string, defaultIngress TrafficIngressScope, allowDefaultIngress bool, egresses []Egress, sources []Source, rules []RoutingRule, terminals []accesscontrol.Terminal, targetScope map[string]bool) error {
	egressByID := make(map[string]Egress, len(egresses))
	for _, egress := range egresses {
		egressByID[egress.ID] = egress
	}
	sourceByID := make(map[string]Source, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	conflictRules := routingRulesWithTerminalEvidence(rules, terminals)
	targetIDs := make(map[string]bool)
	for _, rule := range rules {
		for _, targetID := range rule.TargetListIDs {
			targetIDs[targetID] = true
		}
	}
	targetRules := make(map[string][]SourceRule, len(targetIDs))
	targetKinds := make(map[string]string, len(targetIDs))
	for _, targetID := range sortedBoolKeys(targetIDs) {
		source, ok := sourceByID[targetID]
		if !ok || source.PendingDeletion {
			targetKinds[targetID] = ""
			continue
		}
		targetKinds[targetID] = source.Kind
		versionID := targetVersionForPlan(source, targetScope)
		if versionID == "" {
			continue
		}
		content, err := allRules(ctx, repository, versionID)
		if err != nil {
			return err
		}
		targetRules[targetID] = content
		if targetPromotionAllowed(source.ID, targetScope) {
			appendTargetPromotion(&result.TargetPromotions, targetID, source.PendingVersionID)
		}
	}
	ingressLists, ingressReady := buildRoutingIngressProjections(result, add, managerID, deviceID, defaultIngress, allowDefaultIngress, rules)
	for ruleIndex := range conflictRules {
		filteredTargets := make([]string, 0, len(conflictRules[ruleIndex].TargetListIDs))
		for _, targetID := range conflictRules[ruleIndex].TargetListIDs {
			source, ok := sourceByID[targetID]
			if ok && !source.PendingDeletion && source.Enabled {
				filteredTargets = append(filteredTargets, targetID)
			}
		}
		conflictRules[ruleIndex].TargetListIDs = filteredTargets
	}
	for _, conflict := range RoutingRuleConflicts(conflictRules, targetRules, targetKinds) {
		result.Blockers = append(result.Blockers, PlanIssue{Code: "routing_rule_conflict", Status: "blocker", LogicalID: conflict.RuleAID, EgressID: conflict.EgressA, Reason: conflict.Reason + ": " + conflict.RuleAID + " / " + conflict.RuleBID})
	}
	for _, warning := range RoutingRuleSubjectWarnings(conflictRules) {
		result.Warnings = append(result.Warnings, PlanIssue{Code: "routing_subject_overlap_indeterminate", Status: "warning", LogicalID: warning.RuleAID, EgressID: warning.EgressA, Reason: warning.Reason + ": " + warning.RuleAID + " / " + warning.RuleBID})
	}
	for _, ambiguity := range DomainProjectionContextAmbiguities(conflictRules, targetRules, targetKinds, egressByID) {
		result.Blockers = append(result.Blockers, PlanIssue{Code: "domain_projection_context_ambiguous", Status: "blocker", LogicalID: ambiguity.RuleAID, EgressID: ambiguity.EgressA, Reason: ambiguity.Reason + ": " + ambiguity.RuleAID + " / " + ambiguity.RuleBID})
	}

	byEgress := make(map[string]map[string]*routingTargetProjection)
	for _, rule := range sortedRoutingRules(rules) {
		egress, ok := egressByID[rule.EgressID]
		if !ok || egress.PendingDeletion {
			if rule.Enabled {
				result.Blockers = append(result.Blockers, PlanIssue{Code: "routing_rule_egress_unavailable", Status: "blocker", LogicalID: rule.ID, EgressID: rule.EgressID, Reason: "routing rule references a missing or pending-deletion egress"})
			}
			continue
		}
		for _, targetID := range rule.TargetListIDs {
			source, sourceOK := sourceByID[targetID]
			if !sourceOK || source.PendingDeletion {
				if rule.Enabled {
					result.Blockers = append(result.Blockers, PlanIssue{Code: "routing_rule_target_unavailable", Status: "blocker", LogicalID: rule.ID, EgressID: rule.EgressID, Reason: "routing rule references a missing or pending-deletion target list: " + targetID})
				}
				continue
			}
			if targetVersionForPlan(source, targetScope) == "" {
				result.Warnings = append(result.Warnings, PlanIssue{Code: "target_list_has_no_version", Status: "warning", LogicalID: rule.ID, EgressID: rule.EgressID, Reason: "目标列表尚无可应用版本：" + source.Name})
				continue
			}
			if !source.Enabled {
				continue
			}
			if byEgress[rule.EgressID] == nil {
				byEgress[rule.EgressID] = make(map[string]*routingTargetProjection)
			}
			projection := byEgress[rule.EgressID][targetID]
			if projection == nil {
				projection = &routingTargetProjection{id: targetID, source: source, rules: targetRules[targetID], list: RoutingTargetListNameForSource(managerID, deviceID, rule.EgressID, source)}
				byEgress[rule.EgressID][targetID] = projection
			}
			projection.active = projection.active || rule.Enabled
		}
	}

	for _, egress := range egresses {
		if egress.PendingDeletion {
			continue
		}
		targetsByID := byEgress[egress.ID]
		if len(targetsByID) == 0 {
			continue
		}
		families := enabledFamilies(egress.Families)
		if len(families) == 0 {
			result.Blockers = append(result.Blockers, issue("family_required", "", egress.ID, "至少启用一个地址族"))
			continue
		}
		disabled := "no"
		if !egress.Enabled {
			disabled = "yes"
		}
		strategyLabel := "策略 " + cleanReadableLabel(egress.Name)
		targets := sortedRoutingTargets(targetsByID)
		targetByList := make(map[string]*routingTargetProjection, len(targets))
		for _, target := range targets {
			targetByList[target.list] = target
		}
		addEgress := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
			target := targetByList[firstNonEmptyString(fields["list"], fields["dst-address-list"], fields["address-list"])]
			label := routingObjectCommentLabel(logicalID, strategyLabel, target)
			add(logicalID, menu, phase, label, fields)
		}
		domainTargets := make([]*routingTargetProjection, 0)
		for _, target := range targets {
			if target.source.Kind == KindDomain {
				domainTargets = append(domainTargets, target)
			}
		}
		if len(domainTargets) > 0 {
			buildRoutingDomainObjects(result, addEgress, egress, families, domainTargets, disabled, managerID, deviceID)
		}
		for _, target := range targets {
			if target.source.Kind != KindIP {
				continue
			}
			listDisabled := "yes"
			if egress.Enabled && target.active {
				listDisabled = "no"
			}
			for _, targetRule := range target.rules {
				family := ipRuleFamily(targetRule.RuleType)
				if family == "" || !familyEnabled(families, family) {
					continue
				}
				menu := routeros.MenuIPFirewallAddressList
				if family == FamilyIPv6 {
					menu = routeros.MenuIPv6FirewallAddressList
				}
				logicalID := "routing-target-addr:" + egress.ID + ":" + target.id + ":" + targetRule.RuleType + ":" + targetRule.Domain
				addEgress(logicalID, menu, "foundation", map[string]string{"list": target.list, "address": targetRule.Domain, "disabled": listDisabled})
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
			table, ok := buildRoutingFamilyFoundation(result, addEgress, egress, family, gateway, managerID, deviceID, disabled)
			if !ok {
				continue
			}
			buildRoutingMangleFamily(result, addEgress, egress, family, ingressLists, ingressReady, table, targets, rules, terminals, disabled, managerID, deviceID)
		}
	}
	return nil
}

func buildRoutingIngressProjections(result *DesiredResult, add func(string, routeros.MutationMenu, string, string, map[string]string), managerID, deviceID string, defaultScope TrafficIngressScope, allowDefaultScope bool, rules []RoutingRule) (map[string]string, map[string]bool) {
	defaultScope = NormalizeTrafficIngressScopeUnvalidated(defaultScope)
	defaultKey := TrafficIngressScopeKey(defaultScope)
	listByRule := make(map[string]string)
	readyByRule := make(map[string]bool)
	listByScope := make(map[string]string)
	for _, rule := range rules {
		if !rule.Enabled || (rule.Subject.Mode != SubjectModeAll && rule.Subject.Mode != SubjectModeExcluded) {
			continue
		}
		scope := NormalizeTrafficIngressScopeUnvalidated(rule.Ingress)
		if !HasTrafficIngress(scope) && allowDefaultScope {
			scope = defaultScope
		}
		if !HasTrafficIngress(scope) {
			result.Blockers = append(result.Blockers, PlanIssue{Code: "traffic_ingress_required", Status: "blocker", LogicalID: rule.ID, Reason: "all / excluded routing rule requires an ingress scope"})
			continue
		}
		key := TrafficIngressScopeKey(scope)
		listName := listByScope[key]
		if listName == "" {
			listName = ManagedIngressListName(managerID, deviceID)
			logicalSuffix := ""
			if key != defaultKey || !HasTrafficIngress(defaultScope) {
				logicalSuffix = ":" + shortHash("traffic-ingress:"+key, 8)
				listName += "_" + shortHash("traffic-ingress-list:"+key, 8)
			}
			fields := map[string]string{"name": listName}
			if len(scope.InterfaceLists) > 0 {
				fields["include"] = strings.Join(scope.InterfaceLists, ",")
			}
			add("traffic-ingress:list"+logicalSuffix, routeros.MenuInterfaceList, "foundation", "策略流量入口聚合列表", fields)
			for _, interfaceName := range scope.Interfaces {
				add("traffic-ingress:member"+logicalSuffix+":"+interfaceName, routeros.MenuInterfaceListMember, "foundation", "策略流量入口成员 "+cleanReadableLabel(interfaceName), map[string]string{"list": listName, "interface": interfaceName})
			}
			listByScope[key] = listName
		}
		listByRule[rule.ID] = listName
		readyByRule[rule.ID] = true
	}
	return listByRule, readyByRule
}

func routingRulesWithTerminalEvidence(rules []RoutingRule, terminals []accesscontrol.Terminal) []RoutingRule {
	byID := make(map[string]accesscontrol.Terminal, len(terminals))
	for _, terminal := range terminals {
		byID[terminal.ID] = terminal
	}
	result := append([]RoutingRule{}, rules...)
	for ruleIndex := range result {
		members := append([]SubjectMember{}, result[ruleIndex].Subject.Members...)
		for memberIndex := range members {
			member := &members[memberIndex]
			if member.Binding != subject.BindingAuto {
				continue
			}
			terminal, ok := byID[member.TerminalID]
			if !ok {
				continue
			}
			if len(member.LastIPv4) == 0 {
				member.LastIPv4 = append([]string{}, terminal.IPv4...)
			}
			if len(member.LastIPv6) == 0 {
				member.LastIPv6 = append([]string{}, terminal.IPv6...)
			}
		}
		result[ruleIndex].Subject.Members = members
	}
	return result
}

func buildRoutingDomainObjects(result *DesiredResult, add func(string, routeros.MutationMenu, string, map[string]string), egress Egress, families []EgressFamily, targets []*routingTargetProjection, egressDisabled, managerID, deviceID string) {
	alias := firstNonEmptyString(egress.FakeAlias, deterministicFakeAliasForEgress(egress))
	upstream := firstNonEmptyString(egress.DNSUpstream, "1.1.1.1")
	aliasIP, aliasErr := netip.ParseAddr(alias)
	upstreamIP, upstreamErr := netip.ParseAddr(upstream)
	if aliasErr != nil || upstreamErr != nil || aliasIP.Is4() != upstreamIP.Is4() {
		result.Blockers = append(result.Blockers, issue("invalid_dns_transport", "", egress.ID, "Fake DNS 别名和 DNS 上游必须是同一地址族的 IP"))
		return
	}
	forwarder := "rosboard_" + shortHash("forwarder:"+egress.ID, 10)
	forwarderDisabled := "yes"
	if egress.Enabled {
		for _, target := range targets {
			if target.active {
				forwarderDisabled = "no"
				break
			}
		}
	}
	add("forwarder:"+egress.ID, routeros.MenuIPDNSForwarders, "dns", map[string]string{"name": forwarder, "dns-servers": alias, "disabled": forwarderDisabled})
	for _, target := range targets {
		staticDisabled := "yes"
		if egress.Enabled && target.active {
			staticDisabled = "no"
		}
		for _, targetRule := range target.rules {
			if !isDomainRule(targetRule.RuleType) {
				continue
			}
			matchSubdomain := "no"
			if targetRule.RuleType == "DOMAIN-SUFFIX" {
				matchSubdomain = "yes"
			}
			logicalID := "routing-dns:" + egress.ID + ":" + target.id + ":" + targetRule.RuleType + ":" + targetRule.Domain
			add(logicalID, routeros.MenuIPDNSStatic, "dns", map[string]string{"name": targetRule.Domain, "type": "FWD", "forward-to": forwarder, "address-list": target.list, "disabled": staticDisabled, "match-subdomain": matchSubdomain})
		}
	}
	dnsTable := ""
	for _, family := range families {
		if aliasIP.Is4() == (family.Family == FamilyIPv4) {
			dnsTable = firstNonEmptyString(family.RouteTable, DefaultRouteTable(managerID, deviceID, egress.ID, family.Family))
			break
		}
	}
	if dnsTable == "" {
		result.Blockers = append(result.Blockers, issue("dns_transport_family_missing", "", egress.ID, "DNS 上游地址族没有对应的已启用出口"))
		return
	}
	// The caller supplies the final route table through the family foundation;
	// the DNS transport rules are stable per Egress and can use the configured
	// family table when it is present.
	for _, family := range families {
		if aliasIP.Is4() != (family.Family == FamilyIPv4) {
			continue
		}
		table := firstNonEmptyString(family.RouteTable, DefaultRouteTable(managerID, deviceID, egress.ID, family.Family))
		addDNSTransport(add, egress.ID, alias, upstream, table, aliasIP.Is4(), egressDisabled)
		break
	}
}

func buildRoutingFamilyFoundation(result *DesiredResult, add func(string, routeros.MutationMenu, string, map[string]string), egress Egress, family EgressFamily, gateway, managerID, deviceID, disabled string) (string, bool) {
	familyName := string(family.Family)
	if family.Family != FamilyIPv4 && family.Family != FamilyIPv6 {
		result.Blockers = append(result.Blockers, issue("invalid_family", familyName, egress.ID, "不支持的地址族"))
		return "", false
	}
	gateway = strings.TrimSpace(gateway)
	if gateway == "" {
		result.Blockers = append(result.Blockers, issue("route_incomplete", familyName, egress.ID, "出口接口或下一跳网关不能为空"))
		return "", false
	}
	autoTable := DefaultRouteTable(managerID, deviceID, egress.ID, family.Family)
	table := firstNonEmptyString(family.RouteTable, autoTable)
	routeMode := firstNonEmptyString(family.RouteMode, egress.FailureMode, "strict")
	if routeMode != "strict" && routeMode != "fallback" && routeMode != "existing" {
		result.Blockers = append(result.Blockers, issue("invalid_route_mode", familyName, egress.ID, "不支持的路由模式"))
		return "", false
	}
	isMain := strings.EqualFold(table, "main")
	if isMain && routeMode == "strict" {
		result.Blockers = append(result.Blockers, issue("main_table_strict_invalid", familyName, egress.ID, "main 路由表不能提供严格断线阻断"))
		return "", false
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
	return table, true
}

func buildRoutingMangleFamily(result *DesiredResult, add func(string, routeros.MutationMenu, string, map[string]string), egress Egress, family EgressFamily, ingressLists map[string]string, ingressReady map[string]bool, table string, targets []*routingTargetProjection, rules []RoutingRule, terminals []accesscontrol.Terminal, egressDisabled, managerID, deviceID string) {
	familyName := string(family.Family)
	mangleMenu := routeros.MenuIPFirewallMangle
	if family.Family == FamilyIPv6 {
		mangleMenu = routeros.MenuIPv6FirewallMangle
	}
	sortedRules := sortedRoutingRules(rules)
	usedRouterTargets := make(map[string]bool)
	executionGroups := make(map[string]routingExecutionGroup)
	byTarget := make(map[string]*routingTargetProjection, len(targets))
	for _, target := range targets {
		byTarget[target.id] = target
	}
	for _, rule := range sortedRules {
		if rule.EgressID != egress.ID {
			continue
		}
		ruleTargets := make([]*routingTargetProjection, 0)
		seenTargets := make(map[string]bool)
		for _, targetID := range rule.TargetListIDs {
			target := byTarget[targetID]
			if target == nil || seenTargets[targetID] || !routingTargetSupportsFamily(target, family.Family) {
				continue
			}
			seenTargets[targetID] = true
			ruleTargets = append(ruleTargets, target)
		}
		if len(ruleTargets) == 0 {
			continue
		}
		ingressList := ingressLists[rule.ID]
		ruleIngressReady := ingressReady[rule.ID]
		subjectList := ""
		boundary := "ingress"
		switch rule.Subject.Mode {
		case SubjectModeSelected:
			subjectList = RoutingSubjectListName(managerID, deviceID, rule.ID, family.Family)
			if !buildRoutingSubjectList(result, add, rule, family.Family, terminals, subjectList, egress.Enabled && rule.Enabled) {
				continue
			}
			boundary = "selected"
		case SubjectModeExcluded:
			if !ruleIngressReady || strings.TrimSpace(ingressList) == "" {
				result.Blockers = append(result.Blockers, PlanIssue{Code: "routing_excluded_requires_ingress", Status: "blocker", Family: familyName, LogicalID: rule.ID, EgressID: egress.ID, Reason: "excluded source mode requires a valid TrafficIngress scope"})
				continue
			}
			subjectList = RoutingSubjectListName(managerID, deviceID, rule.ID, family.Family)
			if !buildRoutingSubjectList(result, add, rule, family.Family, terminals, subjectList, egress.Enabled && rule.Enabled) {
				continue
			}
			boundary = "excluded"
		case SubjectModeAll:
			if !ruleIngressReady || strings.TrimSpace(ingressList) == "" {
				continue
			}
		default:
			result.Blockers = append(result.Blockers, PlanIssue{Code: "routing_subject_invalid", Status: "blocker", Family: familyName, LogicalID: rule.ID, EgressID: egress.ID, Reason: "routing rule has an unsupported source mode"})
			continue
		}
		disabled := "no"
		if !egress.Enabled || !rule.Enabled {
			disabled = "yes"
		}
		effectiveEnabled := egress.Enabled && rule.Enabled
		groupKey := routingExecutionGroupKey(egress, family, table, boundary, rule, subjectList, ingressList, effectiveEnabled)
		group, ok := executionGroups[groupKey]
		if !ok {
			logicalID := "routing-rule-routing:" + egress.ID + ":" + familyName + ":" + boundary
			if boundary != "ingress" {
				logicalID += ":" + rule.ID
			}
			if boundary == "ingress" || boundary == "excluded" {
				logicalID += ":" + shortHash("ingress:"+ingressList, 8)
			}
			executionState := "disabled"
			if effectiveEnabled {
				executionState = "enabled"
			}
			logicalID += ":" + executionState
			group = routingExecutionGroup{
				boundary: boundary, subjectList: subjectList, ingressList: ingressList,
				mark:      "rb_" + shortHash("routing-execution-mark:"+groupKey, 12),
				logicalID: logicalID, enabled: effectiveEnabled,
			}
			executionGroups[groupKey] = group
		}
		for _, target := range ruleTargets {
			fields := map[string]string{"chain": "prerouting", "dst-address-type": "!local", "connection-state": "new", "connection-mark": "no-mark", "dst-address-list": target.list, "action": "mark-connection", "new-connection-mark": group.mark, "passthrough": "yes", "disabled": disabled}
			if boundary == "ingress" || boundary == "excluded" {
				fields["in-interface-list"] = ingressList
			}
			if subjectList != "" {
				if rule.Subject.Mode == SubjectModeExcluded {
					fields["src-address-list"] = "!" + subjectList
				} else {
					fields["src-address-list"] = subjectList
				}
			}
			logicalID := "routing-rule-connection:" + rule.ID + ":" + familyName + ":" + target.id
			add(logicalID, mangleMenu, "activation", fields)
		}
		if rule.Enabled {
			for _, target := range ruleTargets {
				if routingTargetSupportsFamily(target, family.Family) {
					usedRouterTargets[target.id] = true
				}
			}
		}
	}
	groupKeys := make([]string, 0, len(executionGroups))
	for key := range executionGroups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	for _, key := range groupKeys {
		group := executionGroups[key]
		disabled := "no"
		if !group.enabled {
			disabled = "yes"
		}
		fields := map[string]string{"chain": "prerouting", "dst-address-type": "!local", "connection-mark": group.mark, "action": "mark-routing", "new-routing-mark": table, "passthrough": "no", "disabled": disabled}
		if group.boundary == "ingress" {
			fields["in-interface-list"] = group.ingressList
		}
		if group.boundary == "selected" {
			fields["src-address-list"] = group.subjectList
		}
		if group.boundary == "excluded" {
			fields["in-interface-list"] = group.ingressList
			fields["src-address-list"] = "!" + group.subjectList
		}
		add(group.logicalID, mangleMenu, "activation", fields)
	}
	if !egress.RouterOutput {
		return
	}
	activeTargets := make([]*routingTargetProjection, 0)
	for _, target := range targets {
		if usedRouterTargets[target.id] && routingTargetSupportsFamily(target, family.Family) {
			activeTargets = append(activeTargets, target)
		}
	}
	if len(activeTargets) == 0 {
		return
	}
	routerMark := "rb_" + shortHash("routing-router:"+egress.ID+":"+familyName, 12)
	for _, target := range activeTargets {
		logicalID := "routing-router-connection:" + egress.ID + ":" + familyName + ":" + target.id
		add(logicalID, mangleMenu, "activation", map[string]string{"chain": "output", "dst-address-type": "!local", "connection-state": "new", "connection-mark": "no-mark", "dst-address-list": target.list, "action": "mark-connection", "new-connection-mark": routerMark, "passthrough": "yes", "disabled": egressDisabled})
	}
	add("routing-router-routing:"+egress.ID+":"+familyName, mangleMenu, "activation", map[string]string{"chain": "output", "connection-mark": routerMark, "action": "mark-routing", "new-routing-mark": table, "passthrough": "no", "disabled": egressDisabled})
}

func routingExecutionGroupKey(egress Egress, family EgressFamily, table, boundary string, rule RoutingRule, subjectList, ingressList string, enabled bool) string {
	effective := "disabled"
	if enabled {
		effective = "enabled"
	}
	parts := []string{
		egress.ID, string(family.Family), table,
		firstNonEmptyString(family.RouteMode, egress.FailureMode, "strict"),
		boundary, effective,
	}
	if boundary != "ingress" {
		parts = append(parts, rule.ID, subjectList)
	}
	if boundary == "ingress" || boundary == "excluded" {
		parts = append(parts, ingressList)
	}
	return strings.Join(parts, "\x00")
}

func buildRoutingSubjectList(result *DesiredResult, add func(string, routeros.MutationMenu, string, map[string]string), rule RoutingRule, family AddressFamily, terminals []accesscontrol.Terminal, listName string, enabled bool) bool {
	addresses := make(map[string]bool)
	for _, prefix := range rule.Subject.Prefixes {
		if prefixFamily, err := netip.ParsePrefix(prefix); err == nil && prefixFamily.Addr().Is4() == (family == FamilyIPv4) {
			addresses[prefixFamily.Masked().String()] = true
		}
	}
	evaluations := accesscontrol.EvaluateMembers(RoutingRuleMembers(rule), terminals)
	for _, evaluation := range evaluations {
		if evaluation.State != accesscontrol.MemberResolved {
			result.Warnings = append(result.Warnings, PlanIssue{Code: "routing_subject_member_unresolved", Status: "warning", LogicalID: rule.ID, EgressID: rule.EgressID, Reason: evaluation.Reason})
		}
		values := evaluation.IPv4
		if family == FamilyIPv6 {
			values = evaluation.IPv6
		}
		for _, value := range values {
			if address, err := netip.ParseAddr(value); err == nil && address.Is4() == (family == FamilyIPv4) {
				addresses[address.String()] = true
			}
		}
	}
	if len(addresses) == 0 {
		result.Warnings = append(result.Warnings, PlanIssue{Code: "routing_subject_unresolved", Status: "warning", LogicalID: rule.ID, EgressID: rule.EgressID, Reason: "selected routing subject has no trustworthy address evidence for this address family"})
		return false
	}
	values := make([]string, 0, len(addresses))
	for value := range addresses {
		values = append(values, value)
	}
	sort.Strings(values)
	disabled := "yes"
	if enabled {
		disabled = "no"
	}
	menu := routeros.MenuIPFirewallAddressList
	if family == FamilyIPv6 {
		menu = routeros.MenuIPv6FirewallAddressList
	}
	for _, value := range values {
		logicalID := "routing-subject:" + rule.ID + ":" + string(family) + ":" + value
		add(logicalID, menu, "foundation", map[string]string{"list": listName, "address": value, "disabled": disabled})
	}
	return true
}

func routingTargetSupportsFamily(target *routingTargetProjection, family AddressFamily) bool {
	if target == nil {
		return false
	}
	if target.source.Kind == KindDomain {
		return true
	}
	for _, rule := range target.rules {
		if ipRuleFamily(rule.RuleType) == family {
			return true
		}
	}
	return false
}

func sortedRoutingRules(rules []RoutingRule) []RoutingRule {
	result := append([]RoutingRule{}, rules...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority < result[j].Priority
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func sortedRoutingTargets(targets map[string]*routingTargetProjection) []*routingTargetProjection {
	result := make([]*routingTargetProjection, 0, len(targets))
	for _, target := range targets {
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func sortedBoolKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func RoutingTargetListName(managerID, deviceID, egressID, targetID string) string {
	return "rb_rt_" + shortHash("routing-target-list:"+managerID+":"+deviceID+":"+egressID+":"+targetID, 12)
}

// RoutingTargetListNameForSource keeps custom target projections hash-only,
// while giving preset projections a stable, readable slug derived from the
// preset ID (never its editable display name).
func RoutingTargetListNameForSource(managerID, deviceID, egressID string, source Source) string {
	if source.Type == TargetSourceTypePreset && strings.TrimSpace(source.PresetID) != "" {
		suffix := "d"
		if source.Kind == KindIP {
			suffix = "ip"
		}
		slug := readableNameKey(source.PresetID, "preset", 24)
		return "rb_rt_" + shortHash("routing-target-list:"+managerID+":"+deviceID+":"+egressID+":"+source.ID, 6) + "_" + slug + "_" + suffix
	}
	return RoutingTargetListName(managerID, deviceID, egressID, source.ID)
}

func routingTargetKindLabel(kind string) string {
	if kind == KindIP {
		return "IP"
	}
	return "域名"
}

func routingTargetKindsLabel(targets []*routingTargetProjection) string {
	kinds := make(map[string]bool)
	for _, target := range targets {
		if target != nil {
			kinds[routingTargetKindLabel(target.source.Kind)] = true
		}
	}
	labels := make([]string, 0, len(kinds))
	for _, kind := range []string{"域名", "IP"} {
		if kinds[kind] {
			labels = append(labels, kind)
		}
	}
	return strings.Join(labels, "/")
}

func routingObjectCommentLabel(logicalID, strategyLabel string, target *routingTargetProjection) string {
	prefix := logicalID
	if index := strings.IndexByte(prefix, ':'); index >= 0 {
		prefix = prefix[:index]
	}
	targetLabel := ""
	if target != nil {
		targetLabel = " · " + routingTargetNameLabel(target.source.Name) + " " + routingTargetKindLabel(target.source.Kind)
	}
	switch prefix {
	case "routing-rule-connection":
		return "入口连接标记 · " + strategyLabel + targetLabel
	case "routing-router-connection":
		return "路由器连接标记 · " + strategyLabel + targetLabel
	case "routing-rule-routing":
		return "入口路由标记 · " + strategyLabel + " · " + routingFamilyLabel(logicalID)
	case "routing-router-routing":
		return "路由器路由标记 · " + strategyLabel + " · " + routingFamilyLabel(logicalID)
	case "routing-target-addr":
		return "目标地址 · " + strategyLabel + targetLabel
	case "routing-dns":
		return "目标 DNS · " + strategyLabel + targetLabel
	default:
		label := strategyLabel + " · " + managedObjectPurpose(logicalID)
		if family := routingObjectFamily(logicalID); family != "" {
			label += " · " + routingFamilyLabel(logicalID)
		}
		return label
	}
}

func routingTargetNameLabel(name string) string {
	label := cleanReadableLabel(name)
	for _, suffix := range []string{"域名", "IP", "Domain"} {
		if strings.HasSuffix(label, " · "+suffix) {
			return strings.TrimSpace(strings.TrimSuffix(label, " · "+suffix))
		}
	}
	return label
}

func routingFamilyLabel(logicalID string) string {
	switch routingObjectFamily(logicalID) {
	case string(FamilyIPv4):
		return "IPv4"
	case string(FamilyIPv6):
		return "IPv6"
	default:
		return ""
	}
}

func routingObjectFamily(logicalID string) string {
	for _, part := range strings.Split(logicalID, ":") {
		if part == string(FamilyIPv4) || part == string(FamilyIPv6) {
			return part
		}
	}
	return ""
}

func RoutingSubjectListName(managerID, deviceID, ruleID string, family AddressFamily) string {
	return "rb_sub_" + shortHash("routing-subject-list:"+managerID+":"+deviceID+":"+ruleID+":"+string(family), 12)
}
