package policyv2

import (
	"context"
	"fmt"

	"rosboard/internal/routeros"
)

// discoveryEvidence is the read-through snapshot shared by the legacy
// discovery projection and the source-selector projection. Read errors are
// retained per menu so factual selectors can remain available when an
// inference-only menu is unavailable.
type discoveryEvidence struct {
	interfaces        []routeros.RouterOSObject
	interfacesErr     error
	resource          []routeros.RouterOSObject
	resourceErr       error
	ipv4Routes        []routeros.RouterOSObject
	ipv4RoutesErr     error
	ipv6Routes        []routeros.RouterOSObject
	ipv6RoutesErr     error
	ipv4DHCP          []routeros.RouterOSObject
	ipv4DHCPErr       error
	ipv6DHCP          []routeros.RouterOSObject
	ipv6DHCPErr       error
	pppoeClients      []routeros.RouterOSObject
	pppoeClientsErr   error
	interfaceLists    []routeros.RouterOSObject
	interfaceListsErr error
	listMembers       []routeros.RouterOSObject
	listMembersErr    error
	bridgePorts       []routeros.RouterOSObject
	bridgePortsErr    error
	ipv4Addresses     []routeros.RouterOSObject
	ipv4AddressesErr  error
	ipv6Addresses     []routeros.RouterOSObject
	ipv6AddressesErr  error
}

func (s *Scanner) collectDiscoveryEvidence(ctx context.Context) (discoveryEvidence, error) {
	if s == nil || s.reader == nil {
		return discoveryEvidence{}, fmt.Errorf("策略扫描器未配置")
	}
	read := func(menu routeros.ReadMenu, properties []string) ([]routeros.RouterOSObject, error) {
		return s.reader.PolicyList(ctx, menu, properties)
	}
	var evidence discoveryEvidence
	evidence.interfaces, evidence.interfacesErr = read(routeros.ReadMenuInterface, []string{".id", "name", "type", "running", "disabled", "dynamic"})
	evidence.resource, evidence.resourceErr = read(routeros.ReadMenuSystemResource, []string{"board-name", "platform", "version"})
	evidence.ipv4Routes, evidence.ipv4RoutesErr = read(routeros.ReadMenuIPRoute, []string{".id", "dst-address", "gateway", "immediate-gw", "immediate-interface", "routing-table", "distance", "active", "disabled", "dynamic", "comment"})
	evidence.ipv6Routes, evidence.ipv6RoutesErr = read(routeros.ReadMenuIPv6Route, []string{".id", "dst-address", "gateway", "immediate-gw", "immediate-interface", "routing-table", "distance", "active", "disabled", "dynamic", "comment"})
	evidence.ipv4DHCP, evidence.ipv4DHCPErr = read(routeros.ReadMenuIPDHCPClient, []string{"interface", "status", "disabled", "gateway"})
	evidence.ipv6DHCP, evidence.ipv6DHCPErr = read(routeros.ReadMenuIPv6DHCPClient, []string{"interface", "status", "disabled", "gateway"})
	evidence.pppoeClients, evidence.pppoeClientsErr = read(routeros.ReadMenuPPPoEClient, []string{"interface", "disabled", "invalid", "running"})
	evidence.interfaceLists, evidence.interfaceListsErr = read(routeros.ReadMenuInterfaceList, []string{".id", "name", "include", "exclude", "comment"})
	evidence.listMembers, evidence.listMembersErr = read(routeros.ReadMenuInterfaceListMember, []string{"list", "interface", "dynamic", "disabled"})
	evidence.bridgePorts, evidence.bridgePortsErr = read(routeros.ReadMenuBridgePort, []string{"interface", "bridge", "disabled"})
	evidence.ipv4Addresses, evidence.ipv4AddressesErr = read(routeros.ReadMenuIPAddress, []string{"address", "interface", "disabled"})
	evidence.ipv6Addresses, evidence.ipv6AddressesErr = read(routeros.ReadMenuIPv6Address, []string{"address", "interface", "disabled"})
	return evidence, nil
}
