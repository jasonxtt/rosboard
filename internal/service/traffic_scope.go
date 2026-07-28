package service

import (
	"fmt"
	"sort"
	"strings"

	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/routeros"
)

type TrafficInterface struct {
	Name      string
	Kind      string
	Reasons   []string
	Automatic bool
	Running   bool
	Disabled  bool
	order     int
}

type trafficScope struct {
	Interfaces       []TrafficInterface
	Warnings         []string
	Mode             string
	Legacy           bool
	OverridesApplied bool
}

func (scope trafficScope) selectedNames() []string {
	result := make([]string, 0, len(scope.Interfaces))
	for _, item := range scope.Interfaces {
		result = append(result, item.Name)
	}
	return result
}

func (scope trafficScope) projection() model.TrafficScope {
	result := model.TrafficScope{
		Mode: scope.Mode, Legacy: scope.Legacy, Warnings: append([]string(nil), scope.Warnings...), OverridesApplied: scope.OverridesApplied,
		Interfaces: make([]model.TrafficScopeInterface, 0, len(scope.Interfaces)),
	}
	for _, item := range scope.Interfaces {
		result.Interfaces = append(result.Interfaces, model.TrafficScopeInterface{
			Name: item.Name, Kind: item.Kind, Reasons: append([]string(nil), item.Reasons...), Automatic: item.Automatic, Running: item.Running, Disabled: item.Disabled,
		})
	}
	return result
}

// deriveTrafficScope selects actual ISP ingress/egress interfaces. It is kept
// independent from deriveTerminalScope: a terminal-topology WAN can be a VPN,
// proxy, or transit hop and must not be counted as ISP traffic.
func deriveTrafficScope(
	cfg config.RouterOSConfig,
	terminal terminalScope,
	interfaces []routeros.Interface,
	pppoeClients []routeros.PPPoEClient,
	dhcpClients []routeros.DHCPClient,
	interfaceLists []routeros.InterfaceList,
	interfaceListMembers []routeros.InterfaceListMember,
	routes []routeros.RoutingRoute,
) trafficScope {
	known := make(map[string]routeros.Interface, len(interfaces))
	for _, item := range interfaces {
		if name := strings.TrimSpace(item.Name); name != "" {
			known[name] = item
		}
	}

	if len(cfg.TrafficInterfaces) > 0 && !strings.EqualFold(strings.TrimSpace(cfg.TrafficScope.Mode), "auto") {
		return legacyTrafficScope(cfg.TrafficInterfaces, known)
	}

	scope := trafficScope{Mode: "auto"}
	resolved, listWarnings := resolveInterfaceLists(interfaceLists, interfaceListMembers)
	scope.Warnings = append(scope.Warnings, listWarnings...)
	wanMembers := resolved["wan"]
	parents := make(map[string]struct{})
	pppoeByName := make(map[string]routeros.PPPoEClient)
	for _, item := range pppoeClients {
		name := strings.TrimSpace(item.Name)
		if name == "" || parseBool(item.Disabled) || parseBool(item.Invalid) {
			continue
		}
		pppoeByName[name] = item
		if parent := strings.TrimSpace(item.Interface); parent != "" {
			parents[parent] = struct{}{}
		}
	}
	if len(pppoeClients) == 0 {
		for name, item := range known {
			if strings.EqualFold(item.Type, "pppoe-out") && !parseBool(item.Disabled) {
				pppoeByName[name] = routeros.PPPoEClient{Name: name, Disabled: item.Disabled, Running: item.Running}
			}
		}
	}

	defaults := make(map[string]struct{})
	for _, route := range routes {
		if parseBool(route.Disabled) || !parseBool(route.Active) || (route.DstAddress != "0.0.0.0/0" && route.DstAddress != "::/0") {
			continue
		}
		name := directRouteInterface(route.ImmediateGateway)
		if name == "" {
			name = directRouteInterface(route.Gateway)
		}
		if name != "" {
			defaults[name] = struct{}{}
		}
	}

	selected := map[string]TrafficInterface{}
	add := func(name, kind string, order int, automatic bool, reason string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		item, found := known[name]
		if found && parseBool(item.Disabled) && automatic {
			return
		}
		if automatic && terminal.interfaceIsLAN(name) {
			scope.Warnings = append(scope.Warnings, fmt.Sprintf("接口 %s 当前被识别为 LAN，未自动纳入采集范围。", name))
			return
		}
		if automatic && trafficExcludedType(item.Type) {
			scope.Warnings = append(scope.Warnings, fmt.Sprintf("接口 %s 为隧道或内部转发类型，未自动纳入采集范围。", name))
			return
		}
		if automatic {
			if _, parent := parents[name]; parent && !strings.EqualFold(kind, "pppoe") {
				scope.Warnings = append(scope.Warnings, fmt.Sprintf("接口 %s 承载已选 PPPoE 接口，为避免重复统计已排除。", name))
				return
			}
			if weakInternalTrafficName(name) {
				scope.Warnings = append(scope.Warnings, fmt.Sprintf("接口 %s 名称疑似内部代理或中转，未自动纳入采集范围。", name))
				return
			}
		}
		candidate := selected[name]
		if candidate.Name == "" {
			candidate = TrafficInterface{Name: name, Kind: kind, Automatic: automatic, Running: parseBool(item.Running), Disabled: found && parseBool(item.Disabled), order: order}
		}
		if order < candidate.order || candidate.order == 0 {
			candidate.order = order
			candidate.Kind = kind
		}
		candidate.Automatic = candidate.Automatic && automatic
		if !containsString(candidate.Reasons, reason) {
			candidate.Reasons = append(candidate.Reasons, reason)
		}
		selected[name] = candidate
	}

	for name := range pppoeByName {
		add(name, "PPPoE", 1, true, "PPPoE 上网接口")
	}
	for _, client := range dhcpClients {
		name := strings.TrimSpace(client.Interface)
		if name == "" || parseBool(client.Disabled) {
			continue
		}
		_, listedWAN := wanMembers[name]
		_, defaultRoute := defaults[name]
		if strings.EqualFold(strings.TrimSpace(client.Status), "bound") || parseBool(client.AddDefaultRoute) || listedWAN || defaultRoute {
			reason := "DHCP Client 上网接口"
			if !strings.EqualFold(strings.TrimSpace(client.Status), "bound") && parseBool(client.AddDefaultRoute) {
				reason = "DHCP Client 备用线路（配置默认路由）"
			}
			add(name, "DHCP", 2, true, reason)
		}
	}
	for name, item := range known {
		if parseBool(item.Disabled) || terminal.interfaceIsLAN(name) {
			continue
		}
		if cellularTrafficType(item.Type, name) {
			add(name, "LTE/WWAN", 3, true, "移动上网接口")
			continue
		}
		if _, listedWAN := wanMembers[name]; listedWAN {
			if _, defaultRoute := defaults[name]; defaultRoute {
				add(name, "静态 WAN", 4, true, "WAN 接口列表与默认路由")
			}
		}
	}

	includes := normalizedInterfaceNames(cfg.TrafficScope.IncludeInterfaces)
	excludes := normalizedInterfaceNames(cfg.TrafficScope.ExcludeInterfaces)
	if len(includes) > 0 || len(excludes) > 0 {
		scope.OverridesApplied = true
	}
	for _, name := range includes {
		if _, found := known[name]; !found {
			scope.Warnings = append(scope.Warnings, fmt.Sprintf("手动纳入的采集接口 %s 当前不存在，已忽略。", name))
			continue
		}
		add(name, "手动纳入", 5, false, "手动强制纳入")
		if terminal.interfaceIsLAN(name) {
			scope.Warnings = append(scope.Warnings, fmt.Sprintf("接口 %s 当前被识别为 LAN，纳入后可能统计局域网内部流量。", name))
		}
	}
	for _, name := range excludes {
		delete(selected, name)
	}
	for _, item := range selected {
		scope.Interfaces = append(scope.Interfaces, item)
	}
	sort.Slice(scope.Interfaces, func(i, j int) bool {
		if scope.Interfaces[i].order != scope.Interfaces[j].order {
			return scope.Interfaces[i].order < scope.Interfaces[j].order
		}
		return scope.Interfaces[i].Name < scope.Interfaces[j].Name
	})
	if len(scope.Interfaces) == 0 {
		scope.Warnings = append(scope.Warnings, "未能自动识别真实上网线路，请在高级设置中强制纳入采集接口。")
	}
	return scope
}

func legacyTrafficScope(names []string, known map[string]routeros.Interface) trafficScope {
	scope := trafficScope{Mode: "legacy", Legacy: true}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		item, found := known[name]
		candidate := TrafficInterface{Name: name, Kind: "旧版手动", Reasons: []string{"旧版手动采集接口配置"}, Running: parseBool(item.Running), Disabled: found && parseBool(item.Disabled)}
		if !found {
			scope.Warnings = append(scope.Warnings, fmt.Sprintf("旧版采集接口 %s 不存在。", name))
		} else if candidate.Disabled {
			scope.Warnings = append(scope.Warnings, fmt.Sprintf("旧版采集接口 %s 已禁用。", name))
		}
		scope.Interfaces = append(scope.Interfaces, candidate)
	}
	return scope
}

func trafficExcludedType(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	for _, value := range []string{"wireguard", "gre", "ipip", "eoip", "vxlan", "l2tp", "sstp", "ovpn", "openvpn", "pptp", "tun", "tap"} {
		if strings.Contains(kind, value) {
			return true
		}
	}
	return false
}

func weakInternalTrafficName(name string) bool {
	value := strings.ToLower(strings.TrimSpace(name))
	for _, hint := range []string{"wan-xray", "sing-box", "singbox", "proxy", "tproxy", "tun", "tunnel", "transit"} {
		if strings.Contains(value, hint) {
			return true
		}
	}
	return false
}

func cellularTrafficType(kind, name string) bool {
	value := strings.ToLower(kind + " " + name)
	return strings.Contains(value, "lte") || strings.Contains(value, "wwan") || strings.Contains(value, "cellular") || strings.Contains(value, "5g")
}

func normalizedInterfaceNames(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
