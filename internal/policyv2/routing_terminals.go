package policyv2

import (
	"net/netip"
	"sort"
	"strings"

	"rosboard/internal/accesscontrol"
)

// RoutingUsableTerminalAddresses returns the address view that RoutingRule
// subjects may use. AccessControl keeps its original terminal snapshot so
// this routing-specific normalization does not change its product semantics.
func RoutingUsableTerminalAddresses(terminal accesscontrol.Terminal) (ipv4, ipv6 []string) {
	return routingUsableAddresses(terminal.IPv4, true), routingUsableAddresses(terminal.IPv6, false)
}

// RoutingUsableTerminals copies a terminal snapshot with only routing-usable
// addresses. The identity and display fields remain unchanged.
func RoutingUsableTerminals(terminals []accesscontrol.Terminal) []accesscontrol.Terminal {
	result := make([]accesscontrol.Terminal, len(terminals))
	for index, terminal := range terminals {
		terminal.IPv4, terminal.IPv6 = RoutingUsableTerminalAddresses(terminal)
		result[index] = terminal
	}
	return result
}

func routingUsableAddresses(values []string, ipv4 bool) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		address, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || address.Zone() != "" || address.Is4() != ipv4 || (!ipv4 && address.IsLinkLocalUnicast()) {
			continue
		}
		canonical := address.String()
		if !seen[canonical] {
			seen[canonical] = true
			result = append(result, canonical)
		}
	}
	sort.Strings(result)
	return result
}
