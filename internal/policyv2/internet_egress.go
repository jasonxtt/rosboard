package policyv2

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/routeros"
)

var internetRouteProplist = []string{
	".id", "afi", "dst-address", "gateway", "immediate-gw", "immediate-interface",
	"routing-table", "distance", "active", "disabled", "dynamic",
}

type internetEgressDiscovery struct {
	Egresses   map[string][]string
	Issues     map[string]string
	Candidates map[string][]accesscontrol.InternetEgressCandidate
}

// discoverInternetEgresses derives the interfaces that can actually carry a
// default route on RouterOS. It considers every routing table and keeps
// configured non-disabled standby routes, so a failover interface is already
// covered before it becomes active.
func discoverInternetEgresses(ctx context.Context, reader PolicyReader, scope accesscontrol.Scope) (map[string][]string, map[string]string) {
	discovery := discoverInternetEgressesDetailed(ctx, reader, scope)
	return discovery.Egresses, discovery.Issues
}

func discoverInternetEgressesDetailed(ctx context.Context, reader PolicyReader, scope accesscontrol.Scope) internetEgressDiscovery {
	discovery := internetEgressDiscovery{
		Egresses:   map[string][]string{accesscontrol.FamilyIPv4: {}, accesscontrol.FamilyIPv6: {}},
		Issues:     map[string]string{},
		Candidates: map[string][]accesscontrol.InternetEgressCandidate{accesscontrol.FamilyIPv4: {}, accesscontrol.FamilyIPv6: {}},
	}
	if reader == nil {
		for _, family := range []string{accesscontrol.FamilyIPv4, accesscontrol.FamilyIPv6} {
			discovery.Issues[family] = "RouterOS 实际互联网出口接口不可读取。"
		}
		return discovery
	}

	interfaces, err := reader.PolicyList(ctx, routeros.ReadMenuInterface, []string{"name", "type", "running", "disabled", "dynamic"})
	if err != nil {
		for _, family := range []string{accesscontrol.FamilyIPv4, accesscontrol.FamilyIPv6} {
			discovery.Issues[family] = "读取 RouterOS 接口失败：" + err.Error()
		}
		return discovery
	}
	interfaceByName := make(map[string]routeros.RouterOSObject, len(interfaces))
	for _, object := range interfaces {
		if name := strings.TrimSpace(object["name"]); name != "" {
			interfaceByName[name] = object
		}
	}

	localInterfaces := make(map[string]bool)
	for _, name := range scope.LocalInterfaces {
		if name = strings.TrimSpace(name); name != "" {
			localInterfaces[name] = true
		}
	}
	for _, prefix := range scope.Prefixes {
		if name := strings.TrimSpace(prefix.Interface); name != "" {
			localInterfaces[name] = true
		}
	}
	// Interface-list discovery is supplemental. Route discovery must still
	// work when a RouterOS account cannot read interface-list members.
	lists, listsErr := reader.PolicyList(ctx, routeros.ReadMenuInterfaceList, []string{"name", "include", "exclude"})
	members, membersErr := reader.PolicyList(ctx, routeros.ReadMenuInterfaceListMember, []string{"list", "interface", "dynamic", "disabled"})
	if listsErr == nil && membersErr == nil {
		for name := range explicitLANInterfaceMembers(lists, members, interfaces) {
			localInterfaces[name] = true
		}
	}
	for _, family := range []string{accesscontrol.FamilyIPv4, accesscontrol.FamilyIPv6} {
		for name, object := range interfaceByName {
			if !manualInternetEgressCandidate(name, object) {
				continue
			}
			reason := "默认路由未能自动确认，请人工确认此接口确实连接互联网"
			if localInterfaces[name] && !isKnownInternetInterfaceType(object["type"], name) {
				reason = "当前被识别为本地接口；仅在确认它实际连接互联网时选择"
			}
			discovery.Candidates[family] = append(discovery.Candidates[family], accesscontrol.InternetEgressCandidate{
				Interface: name,
				Type:      strings.TrimSpace(object["type"]),
				Running:   routerBool(object["running"], true),
				Reason:    reason,
			})
		}
		discovery.Candidates[family] = uniqueInternetEgressCandidates(discovery.Candidates[family])
	}

	for _, family := range []string{accesscontrol.FamilyIPv4, accesscontrol.FamilyIPv6} {
		routeObjects, routeErr := readInternetRouteObjects(ctx, reader, family)
		if routeErr != nil {
			discovery.Issues[family] = "读取 RouterOS " + familyDisplay(family) + " 默认路由失败：" + routeErr.Error()
			continue
		}
		unresolved := ""
		localRoute := ""
		for _, route := range configuredDefaultRoutes(routeObjects, family) {
			source := strings.TrimSpace(route.Source)
			if source == "" {
				if route.Active && unresolved == "" {
					unresolved = "存在活动的 " + familyDisplay(family) + " 默认路由，但无法解析其实际出口接口。"
				}
				continue
			}
			interfaceObject, found := interfaceByName[source]
			if !found {
				if route.Active && unresolved == "" {
					unresolved = "默认路由引用了 RouterOS 中不存在的接口：" + source
				}
				continue
			}
			if localInterfaces[source] && !isKnownInternetInterfaceType(interfaceObject["type"], source) {
				localRoute = source
				continue
			}
			if routerBool(interfaceObject["disabled"], false) {
				if route.Active && unresolved == "" {
					unresolved = "活动默认路由引用了已禁用的接口：" + source
				}
				continue
			}
			if isInboundVPNInterfaceType(interfaceObject["type"]) || isInboundVPNInterfaceType(source) {
				continue
			}
			discovery.Egresses[family] = appendUniqueString(discovery.Egresses[family], source)
		}
		discovery.Egresses[family] = uniqueSorted(discovery.Egresses[family])
		if len(discovery.Egresses[family]) > 0 {
			continue
		}
		if unresolved != "" {
			discovery.Issues[family] = unresolved
			continue
		}
		if localRoute != "" {
			discovery.Issues[family] = "默认路由只解析到本地接口 " + localRoute + "，没有可安全用于禁止联网的独立出口接口。"
			continue
		}
		discovery.Issues[family] = "RouterOS 未发现可用于禁止联网的 " + familyDisplay(family) + " 默认路由出口接口。"
	}
	return discovery
}

func manualInternetEgressCandidate(name string, object routeros.RouterOSObject) bool {
	name = strings.TrimSpace(name)
	if name == "" || routerBool(object["disabled"], false) || routerBool(object["dynamic"], false) {
		return false
	}
	if isInboundVPNInterfaceType(object["type"]) || isInboundVPNInterfaceType(name) {
		return false
	}
	typeName := strings.ToLower(strings.TrimSpace(object["type"]))
	name = strings.ToLower(name)
	return typeName != "loopback" && name != "lo" && !strings.Contains(typeName, "dummy") && !strings.HasPrefix(name, "dummy")
}

func isKnownInternetInterfaceType(typeName, interfaceName string) bool {
	value := strings.ToLower(strings.TrimSpace(typeName) + " " + strings.TrimSpace(interfaceName))
	for _, hint := range []string{"ppp", "lte", "wwan", "cellular", "5g", "wireguard", "gre-out", "ipip-out", "eoip-out", "vxlan-out", "l2tp-out", "sstp-out", "ovpn-out", "pptp-out"} {
		if strings.Contains(value, hint) {
			return true
		}
	}
	return false
}

func uniqueInternetEgressCandidates(values []accesscontrol.InternetEgressCandidate) []accesscontrol.InternetEgressCandidate {
	seen := make(map[string]bool, len(values))
	result := make([]accesscontrol.InternetEgressCandidate, 0, len(values))
	for _, value := range values {
		if value.Interface == "" || seen[value.Interface] {
			continue
		}
		seen[value.Interface] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Interface < result[j].Interface })
	return result
}

func selectInternetEgresses(discovery internetEgressDiscovery, selected map[string][]string) (map[string][]string, map[string]string) {
	result := map[string][]string{accesscontrol.FamilyIPv4: {}, accesscontrol.FamilyIPv6: {}}
	issues := make(map[string]string, len(discovery.Issues))
	for family, reason := range discovery.Issues {
		issues[family] = reason
	}
	for _, family := range []string{accesscontrol.FamilyIPv4, accesscontrol.FamilyIPv6} {
		if egresses := uniqueSorted(discovery.Egresses[family]); len(egresses) > 0 {
			result[family] = egresses
			delete(issues, family)
			continue
		}
		choices := uniqueSorted(selected[family])
		if len(choices) == 0 {
			continue
		}
		allowed := make(map[string]bool, len(discovery.Candidates[family]))
		for _, candidate := range discovery.Candidates[family] {
			allowed[candidate.Interface] = true
		}
		invalid := make([]string, 0)
		for _, choice := range choices {
			if !allowed[choice] {
				invalid = append(invalid, choice)
			}
		}
		if len(invalid) > 0 {
			issues[family] = familyDisplay(family) + " 手动选择的接口已不在本次扫描候选中，请重新扫描后再选择：" + strings.Join(invalid, "、")
			continue
		}
		result[family] = choices
		delete(issues, family)
	}
	return result, issues
}

func readInternetRouteObjects(ctx context.Context, reader PolicyReader, family string) ([]routeros.RouterOSObject, error) {
	legacyMenu := routeros.ReadMenuIPRoute
	if family == accesscontrol.FamilyIPv6 {
		legacyMenu = routeros.ReadMenuIPv6Route
	}
	legacy, legacyErr := reader.PolicyList(ctx, legacyMenu, internetRouteProplist)
	if legacyErr == nil && len(legacy) > 0 {
		return legacy, nil
	}

	combined, combinedErr := reader.PolicyList(ctx, routeros.ReadMenuRoutingRoute, internetRouteProplist)
	if combinedErr == nil {
		filtered := make([]routeros.RouterOSObject, 0, len(combined))
		for _, object := range combined {
			if routeObjectMatchesCombinedFamily(object, family) {
				filtered = append(filtered, object)
			}
		}
		return filtered, nil
	}
	if legacyErr != nil {
		return nil, fmt.Errorf("%v；统一路由表回退失败：%w", legacyErr, combinedErr)
	}
	return legacy, nil
}

func routeObjectMatchesCombinedFamily(object routeros.RouterOSObject, family string) bool {
	if strings.TrimSpace(object["afi"]) == "" && strings.TrimSpace(object["dst-address"]) == "" {
		return false
	}
	return routeObjectMatchesFamily(object, family)
}

func routeObjectMatchesFamily(object routeros.RouterOSObject, family string) bool {
	afi := strings.ToLower(strings.TrimSpace(object["afi"]))
	switch afi {
	case "ip", "ipv4", "ip4":
		return family == accesscontrol.FamilyIPv4
	case "ip6", "ipv6":
		return family == accesscontrol.FamilyIPv6
	}
	destination := strings.TrimSpace(object["dst-address"])
	if destination == "" {
		return true
	}
	if family == accesscontrol.FamilyIPv6 {
		return destination == "::/0"
	}
	return destination == "0.0.0.0/0"
}

func configuredDefaultRoutes(objects []routeros.RouterOSObject, family string) []WANRoute {
	result := make([]WANRoute, 0)
	for _, object := range objects {
		if !routeObjectMatchesFamily(object, family) || routerBool(object["disabled"], false) {
			continue
		}
		destination := strings.TrimSpace(object["dst-address"])
		if destination == "" {
			if family == accesscontrol.FamilyIPv6 {
				destination = "::/0"
			} else {
				destination = "0.0.0.0/0"
			}
		}
		if (family == accesscontrol.FamilyIPv6 && destination != "::/0") || (family != accesscontrol.FamilyIPv6 && destination != "0.0.0.0/0") {
			continue
		}
		gateway := strings.TrimSpace(object["gateway"])
		immediate := strings.TrimSpace(object["immediate-gw"])
		if isNonEgressGateway(gateway) || isNonEgressGateway(immediate) {
			continue
		}
		iface := routeInterface(object["immediate-interface"])
		if iface == "" {
			iface = routeInterface(immediate)
		}
		if iface == "" {
			iface = routeInterface(gateway)
		}
		distance, _ := strconv.Atoi(object["distance"])
		result = append(result, WANRoute{
			ID: object[".id"], Family: family, Destination: destination,
			Gateway: gateway, ImmediateGateway: immediate,
			Table: firstNonEmpty(object["routing-table"], "main"), Source: iface,
			Distance: distance, Active: routerBool(object["active"], false),
			Proven: iface != "",
		})
	}
	return result
}

func isNonEgressGateway(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "blackhole", "unreachable", "prohibit", "throw":
		return true
	default:
		return false
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func familyDisplay(family string) string {
	if family == accesscontrol.FamilyIPv6 {
		return "IPv6"
	}
	return "IPv4"
}
