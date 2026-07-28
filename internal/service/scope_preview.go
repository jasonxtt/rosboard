package service

import (
	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/routeros"
)

// PreviewScopes derives the same read-only runtime projections used by Monitor
// from a verification topology snapshot. It has no client, store, or lock.
func PreviewScopes(cfg config.RouterOSConfig, snapshot routeros.TopologySnapshot) (model.TerminalScope, model.TrafficScope) {
	terminal := deriveTerminalScope(cfg, snapshot.Interfaces, snapshot.IPv4Addresses, snapshot.IPv6Addresses, snapshot.InterfaceLists, snapshot.InterfaceListMembers, snapshot.DHCPServers, snapshot.DHCPClients, snapshot.IPv6NDs, snapshot.IPv6NDPrefixes, snapshot.Routes)
	traffic := deriveTrafficScope(cfg, terminal, snapshot.Interfaces, snapshot.PPPoEClients, snapshot.DHCPClients, snapshot.InterfaceLists, snapshot.InterfaceListMembers, snapshot.Routes)
	return terminal.projection(), traffic.projection()
}
