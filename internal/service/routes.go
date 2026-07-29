package service

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"rosboard/internal/model"
	"rosboard/internal/routeros"
)

type routeAttribution struct {
	Table            string
	Rule             string
	RuleID           string
	Destination      string
	RouteID          string
	RouteIDs         []string
	Gateways         []string
	RouteInterfaces  []string
	EgressInterfaces []string
	Basis            string
	State            string
}

type routeMatcher struct {
	rules    []routeros.RoutingRule
	routes   []routeros.RoutingRoute
	topology interfaceTopology
}

type interfaceTopology struct {
	physical  map[string]bool
	parents   map[string]string
	relations map[string][]model.InterfaceRelation
}

func newRouteMatcher(rules []routeros.RoutingRule, routes []routeros.RoutingRoute) routeMatcher {
	return routeMatcher{rules: rules, routes: routes}
}

func (m routeMatcher) withTopology(topology interfaceTopology) routeMatcher {
	m.topology = topology
	return m
}

func newInterfaceTopology(interfaces []routeros.Interface, ethernet []routeros.EthernetInterface, pppoe []routeros.PPPoEClient, vlans []routeros.VLANInterface, bridgePorts []routeros.BridgePort) interfaceTopology {
	topology := interfaceTopology{physical: map[string]bool{}, parents: map[string]string{}, relations: map[string][]model.InterfaceRelation{}}
	for _, item := range ethernet {
		topology.physical[item.Name] = true
	}
	for _, item := range interfaces {
		if strings.EqualFold(strings.TrimSpace(item.Type), "ether") {
			topology.physical[item.Name] = true
		}
	}
	addParent := func(child, parent, kind string) {
		child, parent = strings.TrimSpace(child), strings.TrimSpace(parent)
		if child == "" || parent == "" {
			return
		}
		topology.parents[child] = parent
		topology.relations[child] = appendUniqueRelation(topology.relations[child], model.InterfaceRelation{Kind: kind, Interface: parent})
	}
	for _, item := range pppoe {
		addParent(item.Name, item.Interface, "carrier")
	}
	for _, item := range vlans {
		addParent(item.Name, item.Interface, "parent")
	}
	for _, item := range bridgePorts {
		member, bridge := strings.TrimSpace(item.Interface), strings.TrimSpace(item.Bridge)
		if member == "" || bridge == "" {
			continue
		}
		topology.relations[member] = appendUniqueRelation(topology.relations[member], model.InterfaceRelation{Kind: "bridge", Interface: bridge})
		topology.relations[bridge] = appendUniqueRelation(topology.relations[bridge], model.InterfaceRelation{Kind: "member", Interface: member})
	}
	for name := range topology.relations {
		sort.Slice(topology.relations[name], func(i, j int) bool {
			left, right := topology.relations[name][i], topology.relations[name][j]
			if left.Kind != right.Kind {
				return left.Kind < right.Kind
			}
			return left.Interface < right.Interface
		})
	}
	return topology
}

func appendUniqueRelation(relations []model.InterfaceRelation, relation model.InterfaceRelation) []model.InterfaceRelation {
	for _, existing := range relations {
		if existing == relation {
			return relations
		}
	}
	return append(relations, relation)
}

func (t interfaceTopology) physicalEgress(name string) string {
	visited := map[string]bool{}
	for name = strings.TrimSpace(name); name != "" && !visited[name]; name = strings.TrimSpace(t.parents[name]) {
		if t.physical[name] {
			return name
		}
		visited[name] = true
		if _, exists := t.parents[name]; !exists {
			return ""
		}
	}
	return ""
}

func (m routeMatcher) match(family, source, destination, inputInterface, routingMark string) routeAttribution {
	destinationIP := net.ParseIP(strings.TrimSpace(destination))
	if destinationIP == nil {
		return routeAttribution{State: "unavailable", Basis: "invalid destination"}
	}
	table := strings.TrimSpace(routingMark)
	basis := "routing mark"
	var matchedRule routeros.RoutingRule
	matchedRuleIndex := -1
	if table == "" {
		basis = "routing rule"
		for index, rule := range m.rules {
			if parseBool(rule.Disabled) || !addressMatches(source, rule.SrcAddress) || !addressMatches(destination, rule.DstAddress) {
				continue
			}
			if mark := strings.TrimSpace(rule.RoutingMark); mark != "" && mark != routingMark {
				continue
			}
			if required := strings.TrimSpace(rule.Interface); required != "" {
				if inputInterface == "" {
					return routeAttribution{State: "unavailable", Basis: "incoming interface unavailable"}
				}
				if required != inputInterface {
					continue
				}
			}
			matchedRule = rule
			matchedRuleIndex = index
			table = strings.TrimSpace(rule.Table)
			if table == "" {
				table = "main"
			}
			break
		}
		if table == "" {
			table = "main"
			basis = "main table"
		}
	}

	attribution := routeAttribution{Table: table, Basis: basis, State: "inferred"}
	if matchedRuleIndex >= 0 {
		attribution.RuleID = stableRuleID(matchedRule, matchedRuleIndex)
		attribution.Rule = preferredName(matchedRule.Comment, fmt.Sprintf("rule #%d", matchedRuleIndex+1))
		switch strings.ToLower(strings.TrimSpace(matchedRule.Action)) {
		case "drop", "unreachable":
			return attribution
		}
	}

	candidates := make([]struct {
		index    int
		route    routeros.RoutingRoute
		prefix   int
		distance int64
	}, 0)
	for index, route := range m.routes {
		if parseBool(route.Disabled) || !parseBool(route.Active) || normalizedTable(route.RoutingTable) != normalizedTable(table) {
			continue
		}
		_, network, err := net.ParseCIDR(normalizeRoutePrefix(route.DstAddress, family))
		if err != nil || !network.Contains(destinationIP) {
			continue
		}
		ones, _ := network.Mask.Size()
		if matchedRuleIndex >= 0 && strings.TrimSpace(matchedRule.MinPrefix) != "" {
			minimum, parseErr := strconv.Atoi(strings.TrimSpace(matchedRule.MinPrefix))
			if parseErr == nil && ones <= minimum {
				continue
			}
		}
		candidates = append(candidates, struct {
			index    int
			route    routeros.RoutingRoute
			prefix   int
			distance int64
		}{index: index, route: route, prefix: ones, distance: parseInt(route.Distance)})
	}
	if len(candidates) == 0 && matchedRuleIndex >= 0 && strings.EqualFold(matchedRule.Action, "lookup") && normalizedTable(table) != "main" {
		fallback := newRouteMatcher(nil, m.routes).withTopology(m.topology).match(family, source, destination, inputInterface, "main")
		fallback.Rule = attribution.Rule
		fallback.RuleID = attribution.RuleID
		fallback.Basis = "routing rule fallback"
		return fallback
	}
	if len(candidates) == 0 {
		attribution.State = "unavailable"
		return attribution
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].prefix != candidates[j].prefix {
			return candidates[i].prefix > candidates[j].prefix
		}
		return candidates[i].distance < candidates[j].distance
	})
	best := candidates[0]
	attribution.Destination = best.route.DstAddress
	attribution.RouteID = stableRouteID(best.route, best.index)
	for _, candidate := range candidates {
		if candidate.prefix != best.prefix || candidate.distance != best.distance || candidate.route.DstAddress != best.route.DstAddress {
			break
		}
		gateway, routeInterface := routeGatewayAndInterface(candidate.route)
		attribution.RouteIDs = append(attribution.RouteIDs, stableRouteID(candidate.route, candidate.index))
		attribution.Gateways = appendUniqueString(attribution.Gateways, gateway)
		attribution.RouteInterfaces = appendUniqueString(attribution.RouteInterfaces, routeInterface)
		attribution.EgressInterfaces = appendUniqueString(attribution.EgressInterfaces, m.topology.physicalEgress(routeInterface))
	}
	if len(attribution.RouteIDs) > 1 {
		attribution.State = "ambiguous"
	}
	return attribution
}

func routeGatewayAndInterface(route routeros.RoutingRoute) (string, string) {
	value := strings.TrimSpace(preferredName(route.ImmediateGateway, route.Gateway))
	if value == "" {
		return "", ""
	}
	if separator := strings.LastIndex(value, "%"); separator > 0 && separator < len(value)-1 {
		return strings.TrimSpace(value[:separator]), strings.TrimSpace(value[separator+1:])
	}
	if net.ParseIP(value) == nil {
		return value, value
	}
	return value, ""
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func addressMatches(address, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "0.0.0.0/0" || prefix == "::/0" {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(address))
	_, network, err := net.ParseCIDR(prefix)
	return err == nil && ip != nil && network.Contains(ip)
}

func normalizeRoutePrefix(prefix, family string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix != "" {
		if strings.Contains(prefix, "/") {
			return prefix
		}
		if strings.Contains(prefix, ":") {
			return prefix + "/128"
		}
		return prefix + "/32"
	}
	if family == "ipv6" {
		return "::/0"
	}
	return "0.0.0.0/0"
}

func normalizedTable(table string) string {
	table = strings.TrimSpace(table)
	if table == "" {
		return "main"
	}
	return table
}

func stableRuleID(rule routeros.RoutingRule, index int) string {
	return preferredName(rule.ID, fmt.Sprintf("rule:%d", index))
}

func stableRouteID(route routeros.RoutingRoute, index int) string {
	return preferredName(route.ID, fmt.Sprintf("route:%d", index))
}
