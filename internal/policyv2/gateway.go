package policyv2

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"rosboard/internal/routeros"
)

// GatewayResolution is deliberately small: policy routing only needs to know
// whether an interface is point-to-point and whether one unambiguous IP next
// hop can be read from RouterOS.
type GatewayResolution struct {
	PointToPoint bool
	Gateway      string
	Candidates   []string
}

// ResolveGateway reads only the selected interface and authoritative default
// route sources. Arbitrary policy tables are intentionally not considered;
// users can still enter a gateway explicitly when their topology needs one.
func ResolveGateway(ctx context.Context, reader PolicyReader, family EgressFamily) (GatewayResolution, error) {
	if reader == nil {
		return GatewayResolution{}, fmt.Errorf("RouterOS gateway discovery is unavailable")
	}
	interfaceName := strings.TrimSpace(family.WANInterface)
	if interfaceName == "" || family.WANSource == "next-hop" {
		return GatewayResolution{}, nil
	}
	interfaces, err := reader.PolicyList(ctx, routeros.ReadMenuInterface, []string{"name", "type"})
	if err != nil {
		return GatewayResolution{}, err
	}
	resolution := GatewayResolution{}
	for _, object := range interfaces {
		if strings.TrimSpace(object["name"]) == interfaceName {
			resolution.PointToPoint = pointToPointInterfaceType(object["type"])
			break
		}
	}
	if resolution.PointToPoint {
		return resolution, nil
	}

	var routeMenu, dhcpMenu routeros.ReadMenu
	familyName := string(family.Family)
	if familyName == string(FamilyIPv6) {
		routeMenu = routeros.ReadMenuIPv6Route
		dhcpMenu = routeros.ReadMenuIPv6DHCPClient
	} else {
		routeMenu = routeros.ReadMenuIPRoute
		dhcpMenu = routeros.ReadMenuIPDHCPClient
	}
	routes, err := reader.PolicyList(ctx, routeMenu, []string{".id", "dst-address", "gateway", "immediate-gw", "routing-table", "distance", "active", "disabled"})
	if err != nil {
		return GatewayResolution{}, err
	}
	candidates := make([]string, 0, 2)
	for _, route := range defaultRoutes(routes, familyName) {
		if !route.Active || !route.Proven || route.Table != "main" || route.Source != interfaceName {
			continue
		}
		if gateway := routeGatewayIP(route, family.Family); gateway != "" {
			candidates = appendUniqueGateway(candidates, gateway)
		}
	}
	// A bound DHCP client is an authoritative source even when the user has
	// disabled installation of its dynamic default route.
	clients, _ := reader.PolicyList(ctx, dhcpMenu, []string{"interface", "status", "disabled", "gateway"})
	for _, client := range clients {
		if strings.TrimSpace(client["interface"]) != interfaceName || !strings.EqualFold(strings.TrimSpace(client["status"]), "bound") || routerBool(client["disabled"], false) {
			continue
		}
		if gateway := parseGatewayIP(client["gateway"], family.Family); gateway != "" {
			candidates = appendUniqueGateway(candidates, gateway)
		}
	}
	resolution.Candidates = candidates
	if len(candidates) == 1 {
		resolution.Gateway = candidates[0]
	}
	return resolution, nil
}

func gatewayIPMatchesFamily(value string, family AddressFamily) bool {
	addr, ok := parseGatewayAddr(value)
	if !ok {
		return false
	}
	return (family == FamilyIPv4 && addr.Is4()) || (family == FamilyIPv6 && !addr.Is4())
}

func routeGatewayIP(route WANRoute, family AddressFamily) string {
	if gateway := parseGatewayIP(route.Gateway, family); gateway != "" {
		return gateway
	}
	return parseGatewayIP(route.ImmediateGateway, family)
}

func parseGatewayIP(value string, family AddressFamily) string {
	addr, ok := parseGatewayAddr(value)
	if !ok || (family == FamilyIPv4 && !addr.Is4()) || (family == FamilyIPv6 && addr.Is4()) || addr.IsUnspecified() {
		return ""
	}
	return addr.String()
}

func parseGatewayAddr(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr, true
	}
	if index := strings.IndexByte(value, '%'); index > 0 {
		addr, err := netip.ParseAddr(value[:index])
		if err == nil {
			return addr.WithZone(value[index+1:]), true
		}
	}
	return netip.Addr{}, false
}

func appendUniqueGateway(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
