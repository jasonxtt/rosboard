package service

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"rosboard/internal/routeros"
)

type routeAttribution struct {
	Table       string
	Rule        string
	RuleID      string
	Destination string
	RouteID     string
	RouteIDs    []string
	Gateways    []string
	Basis       string
	State       string
}

type routeMatcher struct {
	rules  []routeros.RoutingRule
	routes []routeros.RoutingRoute
}

func newRouteMatcher(rules []routeros.RoutingRule, routes []routeros.RoutingRoute) routeMatcher {
	return routeMatcher{rules: rules, routes: routes}
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
		fallback := newRouteMatcher(nil, m.routes).match(family, source, destination, inputInterface, "main")
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
		gateway := preferredName(candidate.route.ImmediateGateway, candidate.route.Gateway)
		attribution.RouteIDs = append(attribution.RouteIDs, stableRouteID(candidate.route, candidate.index))
		if gateway != "" {
			attribution.Gateways = append(attribution.Gateways, gateway)
		}
	}
	if len(attribution.Gateways) > 1 {
		attribution.State = "ambiguous"
	}
	return attribution
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
