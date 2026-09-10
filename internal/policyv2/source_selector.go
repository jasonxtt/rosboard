package policyv2

import (
	"context"
	"sort"
	"strings"

	"rosboard/internal/routeros"
)

// SourceSelectorDiscovery is the factual source-selector inventory used by
// the routing UI. It deliberately does not reuse TrafficIngressCandidate as
// its top-level contract: candidates are an inference result, while these
// arrays must expose every RouterOS object that can be named by a source
// matcher.
type SourceSelectorDiscovery struct {
	Device               map[string]string             `json:"device"`
	Available            bool                          `json:"available"`
	FactStatus           string                        `json:"factStatus"`
	RecommendationStatus string                        `json:"recommendationStatus"`
	Reason               string                        `json:"reason,omitempty"`
	Warnings             []string                      `json:"warnings,omitempty"`
	Snapshot             DiscoverySnapshot             `json:"snapshot"`
	Interfaces           []SourceSelectorInterface     `json:"interfaces"`
	InterfaceLists       []SourceSelectorInterfaceList `json:"interfaceLists"`
}

// SourceSelectorInterface is a real /interface fact plus optional topology
// hints. Recommended and warnings are advisory only; they never make the
// selector unavailable to the user.
type SourceSelectorInterface struct {
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Running     bool     `json:"running"`
	Disabled    bool     `json:"disabled"`
	Dynamic     bool     `json:"dynamic"`
	Recommended bool     `json:"recommended"`
	RoleHints   []string `json:"roleHints,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	CoveredBy   []string `json:"coveredBy,omitempty"`
}

// SourceSelectorInterfaceList is a real /interface/list fact. Include and
// exclude are retained as facts for explanation; they are not used as a
// business admission filter.
type SourceSelectorInterfaceList struct {
	Name          string   `json:"name"`
	Include       []string `json:"include,omitempty"`
	Exclude       []string `json:"exclude,omitempty"`
	StaticMembers []string `json:"staticMembers,omitempty"`
	Recommended   bool     `json:"recommended"`
	RoleHints     []string `json:"roleHints,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	SafetyCode    string   `json:"safetyCode,omitempty"`
}

const (
	sourceSelectorFactAvailable   = "available"
	sourceSelectorFactPartial     = "partial"
	sourceSelectorFactUnavailable = "unavailable"
)

// PolicyDiscoverySnapshot is the single read-through response used by the
// routing wizard. Both projections carry the same RouterOS evidence
// fingerprint, so the UI cannot combine a legacy discovery from one topology
// read with source facts from another.
type PolicyDiscoverySnapshot struct {
	Discovery       Discovery               `json:"discovery"`
	SourceSelectors SourceSelectorDiscovery `json:"sourceSelectors"`
}

// SourceSelectors reads the authoritative RouterOS selector facts without
// making any RouterOS mutation. The /interface and /interface/list reads are
// independent of route and bridge inference: a failed inference read can
// reduce recommendationStatus to partial while the facts remain selectable.
func (s *Scanner) SourceSelectors(ctx context.Context, deviceID string) (SourceSelectorDiscovery, error) {
	evidence, err := s.collectDiscoveryEvidence(ctx)
	if err != nil {
		return SourceSelectorDiscovery{}, err
	}
	return sourceSelectorsFromEvidence(evidence, deviceID)
}

// ScanAndSourceSelectors collects topology evidence once and projects both
// the legacy discovery and the factual source-selector response from it.
// This is intentionally the API-facing path for the wizard; the standalone
// methods remain for existing callers and tests.
func (s *Scanner) ScanAndSourceSelectors(ctx context.Context, deviceID string) (PolicyDiscoverySnapshot, error) {
	evidence, err := s.collectDiscoveryEvidence(ctx)
	if err != nil {
		return PolicyDiscoverySnapshot{}, err
	}
	discovery, discoveryErr := discoveryFromEvidence(evidence, deviceID, nil)
	if discoveryErr != nil {
		discovery = Discovery{
			Device:         map[string]string{"id": deviceID},
			Available:      false,
			Reason:         discoveryErr.Error(),
			Warnings:       []string{},
			Snapshot:       DiscoverySnapshot{Fingerprint: "", DeviceIdentity: map[string]any{}, Capabilities: map[string]any{}},
			WANs:           []WANCandidate{},
			TrafficIngress: []TrafficIngressCandidate{},
			ExistingPolicy: []any{},
		}
	}
	selectors, selectorsErr := sourceSelectorsFromEvidence(evidence, deviceID)
	if selectorsErr != nil {
		selectors = unavailableSourceSelectorDiscovery(deviceID, selectorsErr.Error())
	}
	return PolicyDiscoverySnapshot{Discovery: discovery, SourceSelectors: selectors}, nil
}

func sourceSelectorsFromEvidence(evidence discoveryEvidence, deviceID string) (SourceSelectorDiscovery, error) {
	result := SourceSelectorDiscovery{
		Device:               map[string]string{"id": deviceID},
		FactStatus:           sourceSelectorFactUnavailable,
		RecommendationStatus: sourceSelectorFactUnavailable,
		Warnings:             []string{},
		Interfaces:           []SourceSelectorInterface{},
		InterfaceLists:       []SourceSelectorInterfaceList{},
	}
	interfaces := evidence.interfaces
	lists := evidence.interfaceLists
	interfacesErr := evidence.interfacesErr
	listsErr := evidence.interfaceListsErr
	interfacesOK := interfacesErr == nil
	listsOK := listsErr == nil
	if interfacesErr != nil {
		result.Warnings = append(result.Warnings, "RouterOS 接口事实读取失败："+interfacesErr.Error())
	}
	if listsErr != nil {
		result.Warnings = append(result.Warnings, "RouterOS 接口列表事实读取失败："+listsErr.Error())
	}
	result.Available = interfacesOK || listsOK
	switch {
	case interfacesOK && listsOK:
		result.FactStatus = sourceSelectorFactAvailable
	case result.Available:
		result.FactStatus = sourceSelectorFactPartial
	default:
		result.Reason = "无法读取 RouterOS 接口或接口列表事实"
	}

	// The remaining reads are recommendation evidence only. They intentionally
	// do not gate the factual inventory.
	warnings := &result.Warnings
	appendSourceSelectorReadWarning := func(err error, label string) bool {
		if err != nil {
			*warnings = append(*warnings, label+"读取失败："+err.Error())
			return false
		}
		return true
	}
	ipv4Routes, ipv4RoutesOK := evidence.ipv4Routes, appendSourceSelectorReadWarning(evidence.ipv4RoutesErr, "IPv4 默认路由")
	ipv6Routes, ipv6RoutesOK := evidence.ipv6Routes, appendSourceSelectorReadWarning(evidence.ipv6RoutesErr, "IPv6 默认路由")
	ipv4DHCP, ipv4DHCPOK := evidence.ipv4DHCP, appendSourceSelectorReadWarning(evidence.ipv4DHCPErr, "IPv4 DHCP Client")
	ipv6DHCP, ipv6DHCPOK := evidence.ipv6DHCP, appendSourceSelectorReadWarning(evidence.ipv6DHCPErr, "IPv6 DHCP Client")
	pppoeClients, pppoeOK := evidence.pppoeClients, appendSourceSelectorReadWarning(evidence.pppoeClientsErr, "PPPoE Client")
	members, membersOK := evidence.listMembers, appendSourceSelectorReadWarning(evidence.listMembersErr, "interface list member")
	bridgePorts, bridgePortsOK := evidence.bridgePorts, appendSourceSelectorReadWarning(evidence.bridgePortsErr, "bridge port")
	ipv4Addresses, ipv4AddressesOK := evidence.ipv4Addresses, appendSourceSelectorReadWarning(evidence.ipv4AddressesErr, "IPv4 address")
	ipv6Addresses, ipv6AddressesOK := evidence.ipv6Addresses, appendSourceSelectorReadWarning(evidence.ipv6AddressesErr, "IPv6 address")

	routes := append(defaultRoutes(ipv4Routes, "ipv4"), defaultRoutes(ipv6Routes, "ipv6")...)
	routes = append(routes, dhcpClientRoutes(ipv4DHCP, "ipv4")...)
	routes = append(routes, dhcpClientRoutes(ipv6DHCP, "ipv6")...)
	lanInterfaces := explicitLANInterfaceMembers(lists, members, interfaces)
	pppoeParents := pppoeParentInterfaces(pppoeClients)
	wans := buildWANCandidates(interfaces, routes, lanInterfaces)
	addresses := append(ipv4Addresses, ipv6Addresses...)
	trace := make([]IngressDecision, 0)
	candidates, decisions := buildTrafficIngressCandidatesDetailed(interfaces, lists, members, addresses, bridgePorts, bridgePortsOK, wans, pppoeParents, &trace)
	trace = append(trace, decisions...)

	result.RecommendationStatus = sourceSelectorFactAvailable
	if !interfacesOK && !listsOK {
		result.RecommendationStatus = sourceSelectorFactUnavailable
	} else if !ipv4RoutesOK || !ipv6RoutesOK || !ipv4DHCPOK || !ipv6DHCPOK || !pppoeOK || !membersOK || !bridgePortsOK || !ipv4AddressesOK || !ipv6AddressesOK {
		result.RecommendationStatus = sourceSelectorFactPartial
	}

	candidateByName := make(map[string]TrafficIngressCandidate, len(candidates))
	for _, candidate := range candidates {
		if candidate.Kind != "interface-list" {
			candidateByName[candidate.Name] = candidate
		}
	}
	decisionByName := make(map[string]IngressDecision, len(trace))
	for _, decision := range trace {
		if decision.Interface != "" {
			decisionByName[decision.Interface] = decision
		}
	}
	wanNames := make(map[string]bool)
	for _, wan := range wans {
		if wan.Proven || len(wan.Routes) > 0 {
			wanNames[wan.Interface] = true
		}
	}
	resolvedLists := resolveInterfaceLists(lists, interfaceListMembers(members), interfaceObjectMap(interfaces))

	for _, object := range interfaces {
		name := strings.TrimSpace(object["name"])
		if name == "" {
			continue
		}
		typeName := strings.TrimSpace(object["type"])
		kind := sourceSelectorInterfaceKind(typeName)
		entry := SourceSelectorInterface{
			Name: name, Type: typeName, Kind: kind,
			Running:   routerBool(object["running"], true),
			Disabled:  routerBool(object["disabled"], false),
			Dynamic:   routerBool(object["dynamic"], false),
			RoleHints: sourceSelectorInterfaceRoleHints(kind, typeName),
			Warnings:  []string{}, CoveredBy: uniqueSorted(interfaceListNamesForMember(resolvedLists, name)),
		}
		if candidate, ok := candidateByName[name]; ok {
			entry.Recommended = true
			entry.Reason = candidate.Reason
			entry.CoveredBy = uniqueSorted(append(entry.CoveredBy, candidate.CoveredBy...))
		} else if decision, ok := decisionByName[name]; ok {
			entry.Reason = decision.Reason
		} else {
			entry.Reason = "当前拓扑分析未给出推荐；仍可手动选择。"
		}
		if wanNames[name] {
			entry.RoleHints = appendUnique(entry.RoleHints, "wan")
			entry.Warnings = append(entry.Warnings, "当前被识别为 WAN 出口；作为来源接口通常意味着匹配从该接口进入 RouterOS 的流量，请确认这是你的意图。")
		}
		if entry.Disabled {
			entry.RoleHints = appendUnique(entry.RoleHints, "disabled")
			entry.Warnings = append(entry.Warnings, "当前接口已禁用。")
		}
		if entry.Dynamic {
			entry.RoleHints = appendUnique(entry.RoleHints, "dynamic")
			entry.Warnings = append(entry.Warnings, "当前接口是动态对象；名称可能随 RouterOS 状态变化。")
		}
		if !entry.Recommended && entry.Reason == "" {
			entry.Reason = "未形成推荐候选；不影响手动选择。"
		}
		entry.RoleHints = uniqueSorted(entry.RoleHints)
		entry.Warnings = uniqueSorted(entry.Warnings)
		result.Interfaces = append(result.Interfaces, entry)
	}

	for _, object := range lists {
		name := strings.TrimSpace(object["name"])
		if name == "" {
			continue
		}
		entry := SourceSelectorInterfaceList{
			Name: name, Include: splitCSV(object["include"]), Exclude: splitCSV(object["exclude"]),
			StaticMembers: uniqueSorted(interfaceListMembers(members)[name]),
			Recommended:   isTrafficIngressInterfaceList(object) && !reservedInterfaceLists[strings.ToLower(name)],
			RoleHints:     []string{}, Warnings: []string{},
			Reason: "RouterOS 接口列表事实；推荐仅供参考。",
		}
		if strings.EqualFold(name, "LAN") {
			entry.RoleHints = append(entry.RoleHints, "lan")
			entry.Reason = "名称符合常见 LAN 接口列表约定。"
		}
		if strings.EqualFold(name, "WAN") {
			entry.RoleHints = append(entry.RoleHints, "wan")
			entry.Warnings = append(entry.Warnings, "当前被识别为 WAN 接口列表；作为来源接口列表通常意味着匹配从该列表进入 RouterOS 的流量，请确认这是你的意图。")
		}
		if strings.EqualFold(name, "all") {
			entry.SafetyCode = RoutingSourceInterfaceListAllDeferredCode
			entry.Warnings = append(entry.Warnings, "内置 all 接口列表可见但当前计划会阻止应用，直到全量来源 matcher 的安全语义完成。")
			entry.Reason = "RouterOS 内置接口列表；事实可见，当前策略应用安全门延后。"
		}
		if reservedInterfaceLists[strings.ToLower(name)] && entry.SafetyCode == "" {
			entry.Warnings = append(entry.Warnings, "RouterOS 内置接口列表；请确认选择该来源的语义。")
		}
		entry.RoleHints = uniqueSorted(entry.RoleHints)
		entry.Warnings = uniqueSorted(entry.Warnings)
		result.InterfaceLists = append(result.InterfaceLists, entry)
	}

	sort.SliceStable(result.Interfaces, func(i, j int) bool {
		if result.Interfaces[i].Recommended != result.Interfaces[j].Recommended {
			return result.Interfaces[i].Recommended
		}
		return result.Interfaces[i].Name < result.Interfaces[j].Name
	})
	sort.SliceStable(result.InterfaceLists, func(i, j int) bool {
		if result.InterfaceLists[i].Recommended != result.InterfaceLists[j].Recommended {
			return result.InterfaceLists[i].Recommended
		}
		return result.InterfaceLists[i].Name < result.InterfaceLists[j].Name
	})

	fingerprint, fingerprintErr := discoveryFingerprint(
		evidence.interfaces,
		evidence.resource,
		evidence.ipv4Routes,
		evidence.ipv6Routes,
		evidence.ipv4DHCP,
		evidence.ipv6DHCP,
		evidence.pppoeClients,
		evidence.interfaceLists,
		evidence.listMembers,
		evidence.bridgePorts,
	)
	if fingerprintErr != nil {
		return SourceSelectorDiscovery{}, fingerprintErr
	}
	result.Snapshot.Fingerprint = fingerprint
	return result, nil
}

func unavailableSourceSelectorDiscovery(deviceID, reason string) SourceSelectorDiscovery {
	return SourceSelectorDiscovery{
		Device:               map[string]string{"id": deviceID},
		FactStatus:           sourceSelectorFactUnavailable,
		RecommendationStatus: sourceSelectorFactUnavailable,
		Reason:               reason,
		Warnings:             []string{},
		Interfaces:           []SourceSelectorInterface{},
		InterfaceLists:       []SourceSelectorInterfaceList{},
	}
}

func sourceSelectorInterfaceKind(typeName string) string {
	if kind := trafficIngressInterfaceKind(typeName); kind != "" {
		return kind
	}
	lower := strings.ToLower(strings.TrimSpace(typeName))
	switch {
	case strings.Contains(lower, "pppoe"):
		return "pppoe"
	case strings.Contains(lower, "bond"):
		return "bonding"
	case strings.Contains(lower, "ether") || strings.Contains(lower, "wifi") || strings.Contains(lower, "wlan"):
		return "physical"
	case lower == "loopback" || strings.Contains(lower, "loopback"):
		return "loopback"
	default:
		return "interface"
	}
}

func sourceSelectorInterfaceRoleHints(kind, typeName string) []string {
	hints := []string{}
	if kind != "" {
		hints = append(hints, kind)
	}
	if strings.Contains(strings.ToLower(typeName), "pppoe") {
		hints = append(hints, "wan-candidate")
	}
	return uniqueSorted(hints)
}

func interfaceListMembers(objects []routeros.RouterOSObject) map[string][]string {
	result := make(map[string][]string)
	for _, object := range objects {
		if routerBool(object["disabled"], false) {
			continue
		}
		listName := strings.TrimSpace(object["list"])
		interfaceName := strings.TrimSpace(object["interface"])
		if listName != "" && interfaceName != "" {
			result[listName] = append(result[listName], interfaceName)
		}
	}
	for name := range result {
		result[name] = uniqueSorted(result[name])
	}
	return result
}

func interfaceObjectMap(objects []routeros.RouterOSObject) map[string]routeros.RouterOSObject {
	result := make(map[string]routeros.RouterOSObject, len(objects))
	for _, object := range objects {
		if name := strings.TrimSpace(object["name"]); name != "" {
			result[name] = object
		}
	}
	return result
}

func interfaceListNamesForMember(resolved map[string][]string, name string) []string {
	result := []string{}
	for listName, members := range resolved {
		if containsString(members, name) && !reservedInterfaceLists[strings.ToLower(listName)] {
			result = append(result, listName)
		}
	}
	return result
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
