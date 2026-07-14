# Audit RouterOS traffic monitoring semantics

## Goal

Define accurate and explainable traffic-accounting semantics for rosboard's
system overview and per-client monitoring.

## Confirmed Application Facts

- System overview is an interface-boundary metric. The one-second collector
  calls RouterOS `monitor-traffic` only for configured
  `routeros.traffic_interfaces`, maps TX to upload and RX to download, and sums
  those interfaces.
- With the current configuration, overview therefore represents the monitored
  main RouterOS `pppoe-out1` load. Traffic leaving through another router is
  outside that boundary.
- Per-client metrics do not use `traffic_interfaces`. Every three seconds they
  read IPv4 and IPv6 conntrack, orient original/reply bytes relative to local
  addresses, and aggregate rates per terminal.
- Persisted per-client totals are locally accumulated conntrack deltas. A newly
  observed connection is baselined at its current byte count, and connections
  that start and finish between polls are absent.
- Automatic local-network discovery excludes the selected traffic interface,
  dynamic IPv4 addresses, loopback, and WAN-like interface names, but includes
  static LAN, VLAN, and WireGuard ranges.
- Policy-rule counters are cumulative rule counters; rosboard does not derive
  directional per-client rates or selected-route attribution from them.
- Interface counters from PPPoE, tunnels, LAN, and transit links cannot be
  blindly added because the same flow may cross several monitored boundaries.
- Overview, terminal conntrack, and full interface rows use different sampling
  intervals and are not a synchronized accounting identity.

## RouterOS Observation Boundary

- Per-client upload/download is complete only when RouterOS conntrack observes
  both directions of the relevant client tuple.
- A row with `seen-reply=false` and zero reply bytes cannot provide client
  download accounting, even if a separate downstream or encrypted connection
  is bidirectional.
- Rosboard cannot reconstruct a reliable client mapping between separate inner
  and outer connections; multiplexing and encryption make tuple/time/byte
  correlation ambiguous.
- This is an input-data limitation at the monitored RouterOS boundary, not a
  condition rosboard can correct by combining unrelated interface counters.

## Product Decisions

- System overview remains scoped to selected main-RouterOS uplinks and should
  identify that boundary in the UI.
- Per-client traffic remains conntrack-based and separate from overview WAN
  load.
- Interface detail should use neutral TX/RX wording unless the interface is a
  known Internet-egress boundary.
- Whole-network multi-router usage requires collecting each egress router and
  defining deduplication explicitly; it must not be presented as a simple sum
  of the main router's interfaces.

## Acceptance Criteria

- [x] System-overview and per-client calculations are traced through the
  RouterOS, backend, persistence, and frontend data flow.
- [x] Selected-interface inclusion, alternate-router omissions, WireGuard
  behavior, and double-counting risks are identified.
- [x] The difference between WAN-boundary load and client conntrack traffic is
  documented.
- [x] Polling, baselining, short-lived-connection, and asymmetric-observation
  limitations are documented.

## Out of Scope

- RouterOS route, mangle, firewall, queue, interface, or address changes.
- Transparent-proxy VM configuration or topology changes.
- Rosboard application changes before a separate implementation decision.
