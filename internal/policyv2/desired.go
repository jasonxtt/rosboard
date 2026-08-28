package policyv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"unicode"

	"rosboard/internal/routeros"
)

type DesiredResult struct {
	Revision int64
	Hash     string
	Objects  []DesiredObject
	Blockers []PlanIssue
	Warnings []PlanIssue
}

func BuildDesired(ctx context.Context, repository Repository, reader PolicyReader) (DesiredResult, error) {
	state, err := repository.GetDeviceState(ctx)
	if err != nil {
		return DesiredResult{}, err
	}
	managerID, err := repository.ManagerInstanceID(ctx)
	if err != nil {
		return DesiredResult{}, err
	}
	egresses, err := repository.ListEgresses(ctx)
	if err != nil {
		return DesiredResult{}, err
	}
	sources, err := repository.ListSources(ctx, "")
	if err != nil {
		return DesiredResult{}, err
	}
	sourcesByEgress := enabledSourcesByEgress(sources)
	prefix := managedCommentPrefix(managerID, repository.DeviceID())
	result := DesiredResult{Revision: state.DesiredRevision, Objects: []DesiredObject{}, Blockers: []PlanIssue{}, Warnings: []PlanIssue{}}
	order := 0
	addWithLabel := func(logicalID string, menu routeros.MutationMenu, phase, label string, fields map[string]string) {
		order++
		if menu != routeros.MenuRoutingTable {
			fields["comment"] = managedComment(prefix, logicalID, label)
		}
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Fields: fields, Phase: phase, Order: order})
	}
	ingressList := ManagedIngressListName(managerID, repository.DeviceID())
	hasEgress := hasEnabledEgress(egresses)
	ingress, ingressErr := ParseTrafficIngressScope(state.TrafficIngress)
	if ingressErr != nil {
		if hasEgress {
			result.Blockers = append(result.Blockers, PlanIssue{Code: "invalid_traffic_ingress", Status: "blocker", Reason: ingressErr.Error()})
		}
	} else if len(ingress.InterfaceLists) == 0 && len(ingress.Interfaces) == 0 {
		if hasEgress {
			result.Blockers = append(result.Blockers, PlanIssue{Code: "traffic_ingress_required", Status: "blocker", Reason: "至少选择一个策略流量入口"})
		}
	} else if hasEgress {
		fields := map[string]string{"name": ingressList}
		if len(ingress.InterfaceLists) > 0 {
			fields["include"] = strings.Join(ingress.InterfaceLists, ",")
		}
		addWithLabel("traffic-ingress:list", routeros.MenuInterfaceList, "foundation", "策略流量入口聚合列表", fields)
		for _, interfaceName := range ingress.Interfaces {
			addWithLabel("traffic-ingress:member:"+interfaceName, routeros.MenuInterfaceListMember, "foundation", "策略流量入口成员 "+cleanReadableLabel(interfaceName), map[string]string{"list": ingressList, "interface": interfaceName})
		}
	}

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
		alias := firstNonEmptyString(egress.FakeAlias, deterministicFakeAlias(egress.ID))
		upstream := firstNonEmptyString(egress.DNSUpstream, "1.1.1.1")
		aliasIP, aliasErr := netip.ParseAddr(alias)
		upstreamIP, upstreamErr := netip.ParseAddr(upstream)
		if aliasErr != nil || upstreamErr != nil || aliasIP.Is4() != upstreamIP.Is4() {
			result.Blockers = append(result.Blockers, issue("invalid_dns_transport", "", egress.ID, "Fake DNS 别名和 DNS 上游必须是同一地址族的 IP"))
			continue
		}
		strategyLabel := "策略 " + cleanReadableLabel(egress.Name)
		addEgress := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
			addWithLabel(logicalID, menu, phase, strategyLabel+" · "+managedObjectPurpose(logicalID), fields)
		}

		listBySource := make(map[string]string)
		for _, source := range sourcesByEgress[egress.ID] {
			if mode == ListModeDedicated {
				listBySource[source.ID] = dedicatedListName(source)
			} else {
				listBySource[source.ID] = firstNonEmptyString(egress.ListName, SharedListName(egress.Name))
			}
		}
		forwarder := "rosboard_" + shortHash("forwarder:"+egress.ID, 10)
		addEgress("forwarder:"+egress.ID, routeros.MenuIPDNSForwarders, "dns", map[string]string{"name": forwarder, "dns-servers": alias})
		for _, source := range sourcesByEgress[egress.ID] {
			versionID := firstNonEmptyString(source.PendingVersionID, source.ActiveVersionID)
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
	appendDNSCacheWarning(&result)
	sort.SliceStable(result.Objects, func(i, j int) bool { return result.Objects[i].Order < result.Objects[j].Order })
	payload, err := json.Marshal(result.Objects)
	if err != nil {
		return DesiredResult{}, err
	}
	digest := sha256.Sum256(payload)
	result.Hash = hex.EncodeToString(digest[:])
	return result, nil
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

	mangleMenu, natMenu := routeros.MenuIPFirewallMangle, routeros.MenuIPFirewallNAT
	if family.Family == FamilyIPv6 {
		mangleMenu, natMenu = routeros.MenuIPv6FirewallMangle, routeros.MenuIPv6FirewallNAT
	}
	lists := uniqueSortedValues(listBySource)
	for _, listName := range lists {
		identity := egress.ID + ":" + familyName + ":" + listName
		connectionMark := "rb_" + shortHash("connection:"+identity, 12)
		add("lan-connection:"+identity, mangleMenu, "activation", map[string]string{"chain": "prerouting", "in-interface-list": lanList, "dst-address-type": "!local", "connection-state": "new", "connection-mark": "no-mark", "dst-address-list": listName, "action": "mark-connection", "new-connection-mark": connectionMark, "passthrough": "yes", "disabled": disabled})
		add("lan-routing:"+identity, mangleMenu, "activation", map[string]string{"chain": "prerouting", "in-interface-list": lanList, "connection-mark": connectionMark, "action": "mark-routing", "new-routing-mark": table, "passthrough": "no", "disabled": disabled})
		if egress.RouterOutput {
			routerMark := "rb_" + shortHash("router:"+identity, 12)
			add("router-connection:"+identity, mangleMenu, "activation", map[string]string{"chain": "output", "dst-address-type": "!local", "connection-state": "new", "connection-mark": "no-mark", "dst-address-list": listName, "action": "mark-connection", "new-connection-mark": routerMark, "passthrough": "yes", "disabled": disabled})
			add("router-routing:"+identity, mangleMenu, "activation", map[string]string{"chain": "output", "connection-mark": routerMark, "action": "mark-routing", "new-routing-mark": table, "passthrough": "no", "disabled": disabled})
		}
	}
	if family.NATMode == "masquerade" || family.NATMode == "" && family.Family == FamilyIPv4 {
		fields := map[string]string{"chain": "srcnat", "action": "masquerade", "disabled": disabled}
		if family.WANInterface != "" {
			fields["out-interface"] = family.WANInterface
		}
		add("masquerade:"+egress.ID+":"+familyName, natMenu, "activation", fields)
	}
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

func deterministicFakeAlias(egressID string) string {
	digest := sha256.Sum256([]byte("fake-alias:" + egressID))
	return fmt.Sprintf("192.0.2.%d", int(digest[0])%254+1)
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

func managedCommentPrefix(managerID, deviceID string) string {
	return "rosboard:v2:" + shortHash(managerID, 12) + ":" + shortHash(deviceID, 12) + ":"
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

func managedComment(prefix, logicalID string, labels ...string) string {
	identity := prefix + shortHash(logicalID, 16)
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
	comment = strings.TrimSpace(comment)
	if index := strings.Index(comment, " | "); index >= 0 {
		return strings.TrimSpace(comment[:index])
	}
	return comment
}

func managedObjectPurpose(logicalID string) string {
	prefix := logicalID
	if index := strings.IndexByte(prefix, ':'); index >= 0 {
		prefix = prefix[:index]
	}
	switch prefix {
	case "forwarder":
		return "DNS 转发器"
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
	case "lan-routing":
		return "入口路由标记"
	case "router-connection":
		return "路由器连接标记"
	case "router-routing":
		return "路由器路由标记"
	case "masquerade":
		return "出口地址转换"
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
