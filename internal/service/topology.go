package service

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/routeros"
)

type InterfaceRole string

const (
	InterfaceRoleLAN     InterfaceRole = "lan"
	InterfaceRoleWAN     InterfaceRole = "wan"
	InterfaceRoleUnknown InterfaceRole = "unknown"
)

type EvidenceLevel string

const (
	EvidenceStrong EvidenceLevel = "strong"
	EvidenceMedium EvidenceLevel = "medium"
	EvidenceWeak   EvidenceLevel = "weak"
)

type InterfaceEvidence struct {
	Interface string
	Role      InterfaceRole
	Level     EvidenceLevel
	Reasons   []string
}
type TerminalPrefix struct {
	Prefix                    netip.Prefix
	Interface, Family, Source string
	Automatic                 bool
}
type terminalScope struct {
	Interfaces       map[string]InterfaceEvidence
	Prefixes         []TerminalPrefix
	Warnings         []string
	Mode             string
	Legacy           bool
	OverridesApplied bool
}

func (scope terminalScope) addressInScope(address string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	for _, prefix := range scope.Prefixes {
		if prefix.Prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (scope terminalScope) interfaceIsLAN(name string) bool {
	return scope.Interfaces[name].Role == InterfaceRoleLAN
}

func (scope terminalScope) interfaceAddressInScope(name, address string) bool {
	if !scope.interfaceIsLAN(name) {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	for _, prefix := range scope.Prefixes {
		if prefix.Interface == name && prefix.Prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (scope terminalScope) projection() model.TerminalScope {
	result := model.TerminalScope{Mode: scope.Mode, Legacy: scope.Legacy, Warnings: append([]string(nil), scope.Warnings...), OverridesApplied: scope.OverridesApplied}
	for _, evidence := range scope.Interfaces {
		result.Interfaces = append(result.Interfaces, model.TerminalScopeInterface{Name: evidence.Interface, Role: string(evidence.Role), Confidence: string(evidence.Level), Reasons: append([]string(nil), evidence.Reasons...)})
	}
	sort.Slice(result.Interfaces, func(i, j int) bool { return result.Interfaces[i].Name < result.Interfaces[j].Name })
	for _, prefix := range scope.Prefixes {
		result.Prefixes = append(result.Prefixes, model.TerminalScopePrefix{CIDR: prefix.Prefix.String(), Family: prefix.Family, Interface: prefix.Interface, Source: prefix.Source, Automatic: prefix.Automatic})
	}
	sort.Slice(result.Prefixes, func(i, j int) bool { return result.Prefixes[i].CIDR < result.Prefixes[j].CIDR })
	return result
}

func resolveInterfaceLists(lists []routeros.InterfaceList, members []routeros.InterfaceListMember) (map[string]map[string]struct{}, []string) {
	byName := map[string]routeros.InterfaceList{}
	for _, list := range lists {
		byName[strings.ToLower(strings.TrimSpace(list.Name))] = list
	}
	static := map[string][]routeros.InterfaceListMember{}
	for _, member := range members {
		if !parseBool(member.Disabled) {
			static[strings.ToLower(strings.TrimSpace(member.List))] = append(static[strings.ToLower(strings.TrimSpace(member.List))], member)
		}
	}
	warnings := []string{}
	var resolve func(string, map[string]bool) map[string]struct{}
	resolve = func(name string, stack map[string]bool) map[string]struct{} {
		key := strings.ToLower(strings.TrimSpace(name))
		result := map[string]struct{}{}
		list, ok := byName[key]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("interface list %q is unavailable", name))
			return result
		}
		if stack[key] {
			warnings = append(warnings, fmt.Sprintf("interface list include cycle at %q", list.Name))
			return result
		}
		next := make(map[string]bool, len(stack)+1)
		for item := range stack {
			next[item] = true
		}
		next[key] = true
		for _, include := range strings.Split(list.Include, ",") {
			if strings.TrimSpace(include) == "" {
				continue
			}
			for item := range resolve(include, next) {
				result[item] = struct{}{}
			}
		}
		for _, exclude := range strings.Split(list.Exclude, ",") {
			if strings.TrimSpace(exclude) == "" {
				continue
			}
			for item := range resolve(exclude, next) {
				delete(result, item)
			}
		}
		for _, member := range static[key] {
			if name := strings.TrimSpace(member.Interface); name != "" {
				result[name] = struct{}{}
			}
		}
		return result
	}
	result := map[string]map[string]struct{}{}
	for key := range byName {
		result[key] = resolve(key, map[string]bool{})
	}
	return result, warnings
}

func deriveTerminalScope(cfg config.RouterOSConfig, interfaces []routeros.Interface, ipv4 []routeros.IPAddress, ipv6 []routeros.IPv6Address, lists []routeros.InterfaceList, members []routeros.InterfaceListMember, servers []routeros.DHCPServer, clients []routeros.DHCPClient, nds []routeros.IPv6ND, ndPrefixes []routeros.IPv6NDPrefix, routes []routeros.RoutingRoute) terminalScope {
	scope := terminalScope{Interfaces: map[string]InterfaceEvidence{}, Mode: "auto"}
	if len(cfg.TerminalCIDRs) > 0 {
		scope.Legacy = true
		scope.Mode = "legacy"
		scope.Prefixes = manualPrefixes(cfg.TerminalCIDRs, "legacy terminal_cidrs", &scope)
		return scope
	}
	resolved, warnings := resolveInterfaceLists(lists, members)
	scope.Warnings = warnings
	known := map[string]routeros.Interface{}
	for _, item := range interfaces {
		known[item.Name] = item
		scope.Interfaces[item.Name] = InterfaceEvidence{Interface: item.Name, Role: InterfaceRoleUnknown, Level: EvidenceWeak}
	}
	for _, address := range ipv4 {
		ensureEvidence(scope.Interfaces, addressInterface(address.Interface, address.ActualInterface))
	}
	for _, address := range ipv6 {
		ensureEvidence(scope.Interfaces, addressInterface(address.Interface, address.ActualInterface))
	}
	setRole := func(name string, role InterfaceRole, level EvidenceLevel, reason string) {
		if name == "" {
			return
		}
		evidence := scope.Interfaces[name]
		if evidence.Interface == "" {
			evidence.Interface = name
			evidence.Role = InterfaceRoleUnknown
			evidence.Level = EvidenceWeak
		}
		if evidence.Role == InterfaceRoleUnknown && hasReason(evidence.Reasons, "conflicting strong LAN/WAN evidence") {
			evidence.Reasons = append(evidence.Reasons, reason)
		} else if evidence.Role != InterfaceRoleUnknown && evidence.Role != role && evidence.Level == EvidenceStrong && level == EvidenceStrong {
			evidence.Role = InterfaceRoleUnknown
			evidence.Reasons = append(evidence.Reasons, "conflicting strong LAN/WAN evidence")
			scope.Warnings = append(scope.Warnings, fmt.Sprintf("interface %s has conflicting role evidence", name))
		} else if evidence.Role == InterfaceRoleUnknown || levelRank(level) >= levelRank(evidence.Level) {
			evidence.Role, evidence.Level = role, level
		}
		evidence.Reasons = append(evidence.Reasons, reason)
		scope.Interfaces[name] = evidence
	}
	for listName, entries := range resolved {
		role := InterfaceRoleUnknown
		if strings.EqualFold(listName, "lan") {
			role = InterfaceRoleLAN
		}
		if strings.EqualFold(listName, "wan") {
			role = InterfaceRoleWAN
		}
		for name := range entries {
			if role != InterfaceRoleUnknown {
				setRole(name, role, EvidenceStrong, "member of interface list "+listName)
			}
		}
	}
	for _, item := range servers {
		if !parseBool(item.Disabled) && !parseBool(item.Invalid) {
			setRole(item.Interface, InterfaceRoleLAN, EvidenceStrong, "DHCP server interface")
		}
	}
	for _, item := range clients {
		if !parseBool(item.Disabled) {
			setRole(item.Interface, InterfaceRoleWAN, EvidenceStrong, "DHCP client interface")
		}
	}
	for _, item := range nds {
		if !parseBool(item.Disabled) && !parseBool(item.Invalid) {
			setRole(item.Interface, InterfaceRoleLAN, EvidenceStrong, "IPv6 ND interface")
		}
	}
	for _, item := range ndPrefixes {
		if !parseBool(item.Disabled) && !parseBool(item.Invalid) {
			setRole(item.Interface, InterfaceRoleLAN, EvidenceStrong, "IPv6 ND prefix")
		}
	}
	for _, route := range routes {
		if parseBool(route.Active) && !parseBool(route.Disabled) && (route.DstAddress == "0.0.0.0/0" || route.DstAddress == "::/0") {
			if name := directRouteInterface(route.ImmediateGateway); name != "" {
				setRole(name, InterfaceRoleWAN, EvidenceStrong, "default route interface")
			}
		}
	}
	for name, item := range known {
		kind := strings.ToLower(item.Type)
		if isTunnelType(kind) {
			continue
		}
		evidence := scope.Interfaces[name]
		if evidence.Role == InterfaceRoleUnknown && (strings.Contains(strings.ToLower(name), "lan") || strings.Contains(strings.ToLower(name), "guest") || strings.Contains(strings.ToLower(name), "iot") || kind == "bridge" || kind == "vlan" || strings.Contains(kind, "wlan")) {
			setRole(name, InterfaceRoleLAN, EvidenceWeak, "weak interface name/type evidence")
		}
	}
	for _, name := range cfg.TerminalScope.ExcludeInterfaces {
		setRole(strings.TrimSpace(name), InterfaceRoleWAN, EvidenceStrong, "manual exclusion")
		scope.OverridesApplied = true
	}
	for _, name := range cfg.TerminalScope.IncludeInterfaces {
		setRole(strings.TrimSpace(name), InterfaceRoleLAN, EvidenceStrong, "manual inclusion")
		scope.OverridesApplied = true
	}
	for _, item := range ipv4 {
		addAddressPrefix(&scope, addressInterface(item.Interface, item.ActualInterface), item.Address, "ipv4", "interface address")
	}
	for _, item := range ndPrefixes {
		addPrefix(&scope, item.Interface, item.Prefix, "ipv6", "IPv6 ND prefix", true)
	}
	for _, item := range ipv6 {
		source := "interface address"
		if parseBool(item.Advertise) {
			source = "advertised IPv6 address"
		}
		addAddressPrefix(&scope, addressInterface(item.Interface, item.ActualInterface), item.Address, "ipv6", source)
	}
	for _, prefix := range manualPrefixes(cfg.TerminalScope.IncludeCIDRs, "manual include", &scope) {
		scope.Prefixes = append(scope.Prefixes, prefix)
		scope.OverridesApplied = true
	}
	excluded := manualPrefixes(cfg.TerminalScope.ExcludeCIDRs, "manual exclude", &scope)
	if len(excluded) > 0 {
		scope.OverridesApplied = true
		scope.Prefixes = filterExcludedPrefixes(scope.Prefixes, excluded)
	}
	scope.Prefixes = normalizeTerminalPrefixes(scope.Prefixes)
	if len(scope.Prefixes) == 0 {
		scope.Warnings = append(scope.Warnings, "未能自动识别终端范围；请在高级设置中手动纳入接口或网段")
	}
	return scope
}

func hasReason(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func ensureEvidence(items map[string]InterfaceEvidence, name string) {
	if name != "" {
		if _, ok := items[name]; !ok {
			items[name] = InterfaceEvidence{Interface: name, Role: InterfaceRoleUnknown, Level: EvidenceWeak}
		}
	}
}
func addressInterface(primary, actual string) string {
	if strings.TrimSpace(actual) != "" {
		return strings.TrimSpace(actual)
	}
	return strings.TrimSpace(primary)
}
func levelRank(level EvidenceLevel) int {
	if level == EvidenceStrong {
		return 3
	}
	if level == EvidenceMedium {
		return 2
	}
	return 1
}
func isTunnelType(kind string) bool {
	for _, value := range []string{"wireguard", "gre", "ipip", "eoip", "vxlan", "l2tp", "sstp", "ovpn", "pppoe", "pptp"} {
		if strings.Contains(kind, value) {
			return true
		}
	}
	return false
}
func directRouteInterface(value string) string {
	value = strings.TrimSpace(value)
	// `gateway-ip%bridge` says the next hop is reachable through bridge; it is
	// not proof that bridge itself is a WAN service. Only a direct interface
	// gateway (for example pppoe-out1) is unambiguous WAN route evidence.
	if strings.Contains(value, "%") || strings.Contains(value, ".") || strings.Contains(value, ":") {
		return ""
	}
	return value
}
func addAddressPrefix(scope *terminalScope, name, address, family, source string) {
	if !scope.interfaceIsLAN(name) {
		return
	}
	addPrefix(scope, name, address, family, source, true)
}
func addPrefix(scope *terminalScope, name, value, family, source string, automatic bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return
	}
	prefix = prefix.Masked()
	if family == "ipv4" && (prefix.Bits() >= 31) {
		return
	}
	if family == "ipv6" && (prefix.Bits() == 128 || !usableIPv6Prefix(prefix)) {
		return
	}
	scope.Prefixes = append(scope.Prefixes, TerminalPrefix{Prefix: prefix, Interface: name, Family: family, Source: source, Automatic: automatic})
}
func usableIPv6Prefix(prefix netip.Prefix) bool {
	addr := prefix.Addr()
	return addr.Is6() && !addr.IsUnspecified() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !addr.IsMulticast()
}
func manualPrefixes(values []string, source string, scope *terminalScope) []TerminalPrefix {
	result := []TerminalPrefix{}
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			scope.Warnings = append(scope.Warnings, fmt.Sprintf("invalid terminal CIDR %q", value))
			continue
		}
		prefix = prefix.Masked()
		family := "ipv4"
		if prefix.Addr().Is6() {
			family = "ipv6"
		}
		result = append(result, TerminalPrefix{Prefix: prefix, Family: family, Source: source})
	}
	return result
}
func filterExcludedPrefixes(values, excludes []TerminalPrefix) []TerminalPrefix {
	result := values[:0]
	for _, value := range values {
		excluded := false
		for _, excludedPrefix := range excludes {
			if excludedPrefix.Prefix.Contains(value.Prefix.Addr()) || value.Prefix.Contains(excludedPrefix.Prefix.Addr()) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, value)
		}
	}
	return result
}
func normalizeTerminalPrefixes(values []TerminalPrefix) []TerminalPrefix {
	seen := map[string]struct{}{}
	result := make([]TerminalPrefix, 0, len(values))
	for _, value := range values {
		key := value.Prefix.String()
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}
