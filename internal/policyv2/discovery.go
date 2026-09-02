package policyv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"rosboard/internal/routeros"
)

type Discovery struct {
	Device         map[string]string         `json:"device"`
	Available      bool                      `json:"available"`
	Reason         string                    `json:"reason,omitempty"`
	Warnings       []string                  `json:"warnings,omitempty"`
	Snapshot       DiscoverySnapshot         `json:"snapshot"`
	WANs           []WANCandidate            `json:"wans"`
	TrafficIngress []TrafficIngressCandidate `json:"trafficIngress"`
	ExistingPolicy []any                     `json:"existingPolicy"`
}

type DiscoverySnapshot struct {
	Fingerprint    string         `json:"fingerprint"`
	DeviceIdentity map[string]any `json:"deviceIdentity"`
	Capabilities   map[string]any `json:"capabilities"`
}

type WANCandidate struct {
	Interface    string     `json:"interface"`
	Type         string     `json:"type"`
	Running      bool       `json:"running"`
	PointToPoint bool       `json:"pointToPoint"`
	Proven       bool       `json:"proven"`
	Routes       []WANRoute `json:"routes"`
}

type WANRoute struct {
	ID               string `json:"id"`
	Family           string `json:"family"`
	Destination      string `json:"destination"`
	Gateway          string `json:"gateway"`
	ImmediateGateway string `json:"immediateGateway"`
	Table            string `json:"table"`
	Source           string `json:"source"`
	Distance         int    `json:"distance"`
	Active           bool   `json:"active"`
	Proven           bool   `json:"proven"`
}

type TrafficIngressCandidate struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Include        []string `json:"include"`
	Exclude        []string `json:"exclude"`
	StaticMembers  []string `json:"staticMembers"`
	DynamicMembers bool     `json:"dynamicMembers"`
	Frozen         bool     `json:"frozen"`
	Addresses      []string `json:"addresses"`
	Reason         string   `json:"reason"`
	CoveredBy      []string `json:"coveredBy"`
	Default        bool     `json:"default"`
	Dynamic        bool     `json:"dynamic"`
	Running        bool     `json:"running"`
}

type Scanner struct {
	reader PolicyReader
}

func NewScanner(reader PolicyReader) *Scanner { return &Scanner{reader: reader} }

func (s *Scanner) Scan(ctx context.Context, deviceID string) (Discovery, error) {
	if s == nil || s.reader == nil {
		return Discovery{}, fmt.Errorf("policy scanner is not configured")
	}
	interfaces, err := s.reader.PolicyList(ctx, routeros.ReadMenuInterface, []string{".id", "name", "type", "running", "disabled", "dynamic"})
	if err != nil {
		return Discovery{}, fmt.Errorf("read RouterOS interfaces: %w", err)
	}
	resource, err := s.reader.PolicyList(ctx, routeros.ReadMenuSystemResource, []string{"board-name", "platform", "version"})
	if err != nil {
		return Discovery{}, fmt.Errorf("read RouterOS identity: %w", err)
	}
	warnings := make([]string, 0)
	ipv4Routes, err := s.reader.PolicyList(ctx, routeros.ReadMenuIPRoute, []string{".id", "dst-address", "gateway", "immediate-gw", "immediate-interface", "routing-table", "distance", "active", "disabled", "dynamic"})
	if err != nil {
		return Discovery{}, fmt.Errorf("read RouterOS IPv4 routes: %w", err)
	}
	ipv6Routes, ipv6RouteErr := s.reader.PolicyList(ctx, routeros.ReadMenuIPv6Route, []string{".id", "dst-address", "gateway", "immediate-gw", "immediate-interface", "routing-table", "distance", "active", "disabled", "dynamic"})
	if ipv6RouteErr != nil {
		warnings = append(warnings, "IPv6 默认路由发现失败："+ipv6RouteErr.Error())
		ipv6Routes = nil
	}
	readOptional := func(menu routeros.ReadMenu, proplist []string, label string) []routeros.RouterOSObject {
		objects, err := s.reader.PolicyList(ctx, menu, proplist)
		if err != nil {
			warnings = append(warnings, label+"读取失败："+err.Error())
			return nil
		}
		return objects
	}
	ipv4DHCP := readOptional(routeros.ReadMenuIPDHCPClient, []string{"interface", "status", "disabled", "gateway"}, "IPv4 DHCP Client")
	ipv6DHCP := readOptional(routeros.ReadMenuIPv6DHCPClient, []string{"interface", "status", "disabled", "gateway"}, "IPv6 DHCP Client")
	pppoeClients := readOptional(routeros.ReadMenuPPPoEClient, []string{"interface", "disabled", "invalid", "running"}, "PPPoE Client")
	lists := readOptional(routeros.ReadMenuInterfaceList, []string{".id", "name", "include", "exclude", "comment"}, "interface list")
	members := readOptional(routeros.ReadMenuInterfaceListMember, []string{"list", "interface", "dynamic", "disabled"}, "interface list member")
	bridgePorts, bridgePortsAvailable := readOptionalWithStatus(s.reader, ctx, routeros.ReadMenuBridgePort, []string{"interface", "bridge", "disabled"}, "bridge port", &warnings)
	ipv4Addresses := readOptional(routeros.ReadMenuIPAddress, []string{"address", "interface", "disabled"}, "IPv4 address")
	ipv6Addresses := readOptional(routeros.ReadMenuIPv6Address, []string{"address", "interface", "disabled"}, "IPv6 address")

	routes := append(defaultRoutes(ipv4Routes, "ipv4"), defaultRoutes(ipv6Routes, "ipv6")...)
	routes = append(routes, dhcpClientRoutes(ipv4DHCP, "ipv4")...)
	routes = append(routes, dhcpClientRoutes(ipv6DHCP, "ipv6")...)
	lanInterfaces := explicitLANInterfaceMembers(lists, members, interfaces)
	pppoeParents := pppoeParentInterfaces(pppoeClients)
	wans := buildWANCandidates(interfaces, routes, lanInterfaces)
	lan := buildTrafficIngressCandidates(interfaces, lists, members, append(ipv4Addresses, ipv6Addresses...), bridgePorts, bridgePortsAvailable, wans, pppoeParents)
	fingerprint, err := discoveryFingerprint(interfaces, resource, ipv4Routes, ipv6Routes, ipv4DHCP, ipv6DHCP, pppoeClients, lists, members, bridgePorts)
	if err != nil {
		return Discovery{}, err
	}
	identity := map[string]any{}
	if len(resource) > 0 {
		for key, value := range resource[0] {
			identity[key] = value
		}
	}
	return Discovery{
		Device:    map[string]string{"id": deviceID},
		Available: true,
		Warnings:  warnings,
		Snapshot: DiscoverySnapshot{
			Fingerprint:    fingerprint,
			DeviceIdentity: identity,
			Capabilities:   map[string]any{},
		},
		WANs:           wans,
		TrafficIngress: lan,
		ExistingPolicy: []any{},
	}, nil
}

func readOptionalWithStatus(reader PolicyReader, ctx context.Context, menu routeros.ReadMenu, proplist []string, label string, warnings *[]string) ([]routeros.RouterOSObject, bool) {
	objects, err := reader.PolicyList(ctx, menu, proplist)
	if err != nil {
		*warnings = append(*warnings, label+"读取失败："+err.Error())
		return nil, false
	}
	return objects, true
}

func defaultRoutes(objects []routeros.RouterOSObject, family string) []WANRoute {
	result := make([]WANRoute, 0)
	for _, object := range objects {
		destination := strings.TrimSpace(object["dst-address"])
		if destination == "" {
			if family == "ipv4" {
				destination = "0.0.0.0/0"
			} else {
				destination = "::/0"
			}
		}
		if destination != "0.0.0.0/0" && destination != "::/0" {
			continue
		}
		gateway := strings.TrimSpace(object["gateway"])
		immediate := strings.TrimSpace(object["immediate-gw"])
		iface := routeInterface(object["immediate-interface"])
		if iface == "" {
			iface = routeInterface(immediate)
		}
		if iface == "" {
			iface = routeInterface(gateway)
		}
		distance, _ := strconv.Atoi(object["distance"])
		active := routerBool(object["active"], false) && !routerBool(object["disabled"], false)
		result = append(result, WANRoute{
			ID: object[".id"], Family: family, Destination: destination,
			Gateway: gateway, ImmediateGateway: immediate,
			Table:  firstNonEmpty(object["routing-table"], "main"),
			Source: iface, Distance: distance, Active: active,
			Proven: active && (gateway != "" || immediate != ""),
		})
	}
	return result
}

func dhcpClientRoutes(objects []routeros.RouterOSObject, family string) []WANRoute {
	result := make([]WANRoute, 0, len(objects))
	for _, object := range objects {
		if strings.TrimSpace(object["interface"]) == "" || !strings.EqualFold(strings.TrimSpace(object["status"]), "bound") || routerBool(object["disabled"], false) {
			continue
		}
		gateway := parseGatewayIP(object["gateway"], AddressFamily(family))
		if gateway == "" {
			continue
		}
		destination := "0.0.0.0/0"
		if family == "ipv6" {
			destination = "::/0"
		}
		result = append(result, WANRoute{
			ID: object[".id"], Family: family, Destination: destination,
			Gateway: gateway, Table: "main", Source: strings.TrimSpace(object["interface"]),
			Active: true, Proven: true,
		})
	}
	return result
}

func pppoeParentInterfaces(clients []routeros.RouterOSObject) map[string]bool {
	result := make(map[string]bool)
	for _, client := range clients {
		if routerBool(client["disabled"], false) || routerBool(client["invalid"], false) {
			continue
		}
		if name := strings.TrimSpace(client["interface"]); name != "" {
			result[name] = true
		}
	}
	return result
}

func buildWANCandidates(interfaces []routeros.RouterOSObject, routes []WANRoute, excluded map[string]bool) []WANCandidate {
	byName := make(map[string]routeros.RouterOSObject, len(interfaces))
	for _, object := range interfaces {
		byName[object["name"]] = object
	}
	grouped := make(map[string][]WANRoute)
	for _, route := range routes {
		if route.Source != "" && !excluded[route.Source] {
			grouped[route.Source] = append(grouped[route.Source], route)
		}
	}
	result := make([]WANCandidate, 0, len(grouped))
	for name, candidateRoutes := range grouped {
		object := byName[name]
		if name == "" || routerBool(object["dynamic"], false) || isInboundVPNInterfaceType(object["type"]) {
			continue
		}
		running := routerBool(object["running"], true) && !routerBool(object["disabled"], false)
		typeName := object["type"]
		proven := false
		for _, route := range candidateRoutes {
			proven = proven || route.Proven
		}
		result = append(result, WANCandidate{
			Interface: name, Type: typeName, Running: running,
			PointToPoint: pointToPointInterfaceType(typeName),
			Proven:       running && proven, Routes: candidateRoutes,
		})
	}
	for _, object := range interfaces {
		name := strings.TrimSpace(object["name"])
		if name == "" || excluded[name] || routerBool(object["disabled"], false) || routerBool(object["dynamic"], false) || isInboundVPNInterfaceType(object["type"]) || !isPotentialEgressInterfaceType(object["type"]) {
			continue
		}
		if _, exists := grouped[name]; exists {
			continue
		}
		result = append(result, WANCandidate{
			Interface: name, Type: object["type"],
			Running:      routerBool(object["running"], true),
			PointToPoint: pointToPointInterfaceType(object["type"]),
			Routes:       []WANRoute{},
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Interface < result[j].Interface })
	return result
}

func pointToPointInterfaceType(typeName string) bool {
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	if strings.Contains(typeName, "ppp") || strings.Contains(typeName, "wireguard") || typeName == "wg" {
		return true
	}
	kind := trafficIngressInterfaceKind(typeName)
	return kind == "vpn" || kind == "tunnel"
}

func isPotentialEgressInterfaceType(typeName string) bool {
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	if typeName == "" || isInboundVPNInterfaceType(typeName) {
		return false
	}
	kind := trafficIngressInterfaceKind(typeName)
	return strings.Contains(typeName, "ppp") || strings.Contains(typeName, "wireguard") || typeName == "wg" || kind == "vpn" || kind == "tunnel"
}

func isInboundVPNInterfaceType(typeName string) bool {
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	return strings.Contains(typeName, "-in") || strings.Contains(typeName, "_in") || strings.Contains(typeName, "server")
}

func explicitLANInterfaceMembers(lists, members, interfaces []routeros.RouterOSObject) map[string]bool {
	membersByList := make(map[string][]string)
	for _, member := range members {
		if routerBool(member["disabled"], false) {
			continue
		}
		membersByList[member["list"]] = append(membersByList[member["list"]], member["interface"])
	}
	interfaceByName := make(map[string]routeros.RouterOSObject, len(interfaces))
	for _, object := range interfaces {
		interfaceByName[object["name"]] = object
	}
	resolved := resolveInterfaceLists(lists, membersByList, interfaceByName)
	result := make(map[string]bool)
	for _, list := range lists {
		if !isTrafficIngressInterfaceList(list) {
			continue
		}
		for _, name := range resolved[list["name"]] {
			result[name] = true
		}
	}
	return result
}

func isTrafficIngressInterfaceList(list routeros.RouterOSObject) bool {
	name := strings.TrimSpace(list["name"])
	if strings.EqualFold(name, "LAN") {
		return true
	}
	comment := strings.TrimSpace(list["comment"])
	return isManagedComment(comment) && (strings.Contains(comment, "策略流量入口") || strings.HasSuffix(strings.ToLower(name), "_ingress"))
}

func buildTrafficIngressCandidates(interfaces, lists, members, addresses, bridgePorts []routeros.RouterOSObject, bridgePortsAvailable bool, wans []WANCandidate, nonIngress map[string]bool) []TrafficIngressCandidate {
	membersByList := make(map[string][]string)
	dynamicByList := make(map[string]bool)
	for _, member := range members {
		if routerBool(member["disabled"], false) {
			continue
		}
		listName := member["list"]
		membersByList[listName] = append(membersByList[listName], member["interface"])
		dynamicByList[listName] = dynamicByList[listName] || routerBool(member["dynamic"], false)
	}
	addressesByInterface := make(map[string][]string)
	for _, address := range addresses {
		if !routerBool(address["disabled"], false) {
			addressesByInterface[address["interface"]] = append(addressesByInterface[address["interface"]], address["address"])
		}
	}
	interfaceByName := make(map[string]routeros.RouterOSObject, len(interfaces))
	for _, object := range interfaces {
		interfaceByName[object["name"]] = object
	}
	resolvedLists := resolveInterfaceLists(lists, membersByList, interfaceByName)
	result := make([]TrafficIngressCandidate, 0, len(lists)+len(interfaces))
	for _, list := range lists {
		name := strings.TrimSpace(list["name"])
		if name == "" || reservedInterfaceLists[strings.ToLower(name)] || isManagedComment(list["comment"]) {
			continue
		}
		staticMembers := uniqueSorted(membersByList[name])
		candidateAddresses := make([]string, 0)
		for _, member := range resolvedLists[name] {
			candidateAddresses = append(candidateAddresses, addressesByInterface[member]...)
		}
		result = append(result, TrafficIngressCandidate{
			Name: name, Kind: "interface-list", Include: splitCSV(list["include"]), Exclude: splitCSV(list["exclude"]),
			StaticMembers: staticMembers, DynamicMembers: dynamicByList[name], Frozen: false,
			Addresses: uniqueSorted(candidateAddresses), Reason: "RouterOS 接口列表", Default: strings.EqualFold(name, "LAN"), Running: true,
		})
	}
	wanNames := make(map[string]bool)
	for _, wan := range wans {
		if wan.Proven || len(wan.Routes) > 0 {
			wanNames[wan.Interface] = true
		}
	}
	bridgeSlaves := make(map[string]bool)
	for _, port := range bridgePorts {
		if !routerBool(port["disabled"], false) {
			bridgeSlaves[port["interface"]] = true
		}
	}
	for _, object := range interfaces {
		name := strings.TrimSpace(object["name"])
		if name == "" || wanNames[name] || nonIngress[name] || routerBool(object["disabled"], false) || routerBool(object["dynamic"], false) {
			continue
		}
		kind := trafficIngressInterfaceKind(object["type"])
		if kind == "" || kind == "physical" && (!bridgePortsAvailable || bridgeSlaves[name]) {
			continue
		}
		coveredBy := make([]string, 0)
		for listName, listMembers := range resolvedLists {
			if !reservedInterfaceLists[strings.ToLower(listName)] && containsString(listMembers, name) {
				coveredBy = append(coveredBy, listName)
			}
		}
		result = append(result, TrafficIngressCandidate{
			Name: name, Kind: kind, Addresses: uniqueSorted(addressesByInterface[name]), CoveredBy: uniqueSorted(coveredBy),
			Reason: trafficIngressReason(kind), Dynamic: false,
			Running: routerBool(object["running"], true) && !routerBool(object["disabled"], false),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Default != result[j].Default {
			return result[i].Default
		}
		if result[i].Kind != result[j].Kind {
			return result[i].Kind == "interface-list"
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func resolveInterfaceLists(lists []routeros.RouterOSObject, direct map[string][]string, interfaces map[string]routeros.RouterOSObject) map[string][]string {
	byName := make(map[string]routeros.RouterOSObject, len(lists))
	for _, list := range lists {
		byName[list["name"]] = list
	}
	resolved := make(map[string][]string)
	var resolve func(string, map[string]bool) []string
	resolve = func(name string, visiting map[string]bool) []string {
		if value, ok := resolved[name]; ok {
			return value
		}
		if visiting[name] {
			return nil
		}
		visiting[name] = true
		members := make(map[string]bool)
		switch strings.ToLower(name) {
		case "all":
			for interfaceName := range interfaces {
				members[interfaceName] = true
			}
		case "dynamic":
			for interfaceName, object := range interfaces {
				members[interfaceName] = routerBool(object["dynamic"], false)
			}
		case "static":
			for interfaceName, object := range interfaces {
				members[interfaceName] = !routerBool(object["dynamic"], false)
			}
		case "none":
		default:
			list := byName[name]
			for _, included := range splitCSV(list["include"]) {
				for _, member := range resolve(included, visiting) {
					members[member] = true
				}
			}
			for _, excluded := range splitCSV(list["exclude"]) {
				for _, member := range resolve(excluded, visiting) {
					delete(members, member)
				}
			}
			for _, member := range direct[name] {
				members[member] = true
			}
		}
		delete(visiting, name)
		values := make([]string, 0, len(members))
		for member, included := range members {
			if included {
				values = append(values, member)
			}
		}
		resolved[name] = uniqueSorted(values)
		return resolved[name]
	}
	for name := range byName {
		resolve(name, make(map[string]bool))
	}
	return resolved
}

func trafficIngressInterfaceKind(typeName string) string {
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	switch {
	case strings.Contains(typeName, "bridge"):
		return "bridge"
	case strings.Contains(typeName, "vlan"):
		return "vlan"
	case strings.Contains(typeName, "wireguard") || typeName == "wg":
		return "wireguard"
	case strings.Contains(typeName, "l2tp") || strings.Contains(typeName, "sstp") || strings.Contains(typeName, "ovpn") || strings.Contains(typeName, "pptp"):
		return "vpn"
	case strings.Contains(typeName, "gre") || strings.Contains(typeName, "ipip") || strings.Contains(typeName, "eoip") || strings.Contains(typeName, "vxlan") || strings.Contains(typeName, "zerotier"):
		return "tunnel"
	case strings.Contains(typeName, "ether") || strings.Contains(typeName, "wifi") || strings.Contains(typeName, "wlan"):
		return "physical"
	default:
		return ""
	}
}

func trafficIngressReason(kind string) string {
	switch kind {
	case "bridge":
		return "Bridge 三层入口"
	case "vlan":
		return "VLAN 三层入口"
	case "wireguard":
		return "WireGuard 解密后的客户端流量入口"
	case "vpn":
		return "固定 VPN 流量入口"
	case "tunnel":
		return "固定隧道流量入口"
	case "physical":
		return "未加入 Bridge 的物理三层入口"
	default:
		return "RouterOS 接口"
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func discoveryFingerprint(groups ...[]routeros.RouterOSObject) (string, error) {
	payload, err := json.Marshal(groups)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func routeInterface(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if index := strings.LastIndex(value, "%"); index >= 0 {
		return value[index+1:]
	}
	if _, err := netip.ParseAddr(value); err == nil || strings.Contains(value, ":") {
		return ""
	}
	return value
}

func routerBool(value string, defaultValue bool) bool {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	parsed, err := routeros.ParseRouterOSBool(value)
	return err == nil && parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	return uniqueSorted(strings.Split(value, ","))
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
