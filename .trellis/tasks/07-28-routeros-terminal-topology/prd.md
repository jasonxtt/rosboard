# RouterOS automatic terminal topology discovery

## Goal

Replace ordinary-user terminal CIDR selection with explainable, read-only RouterOS LAN/WAN topology discovery. A device normally needs only its RouterOS connection, credentials, REST port, and enabled state. Advanced operators retain explicit interface/CIDR overrides, while legacy `terminal_cidrs` configurations remain stable until deliberately migrated.

## Confirmed facts

- `traffic_interfaces` and `terminal_cidrs` are per-device. The old `deriveLocalCIDRs` in `internal/service/monitor.go` feeds DHCP/ARP/neighbor discovery and conntrack; an empty scope currently accepts every address.
- RouterOS self ownership is exact-address logic and merges assigned addresses to `routeros:self`; it must remain separate from terminal-prefix scope.
- The existing minimal-privilege RouterOS account successfully read every proposed topology menu. Fields and current evidence are recorded in `research/routeros-rest-topology-fields.md`; no RouterOS state was changed.
- Current live evidence: `lan` is LAN-list/DHCP/ND, `vlan2-aruba` has DHCP-server LAN evidence, `pppoe-out1` is WAN-list/default-route, and `wan-xray` has a private address but an active default route. Confirmed candidate LAN prefixes are `10.0.0.0/24`, `10.10.10.0/24`, and `fc00::/64`.

## Requirements

1. Add typed, batched, read-only optional RouterOS topology reads for interface lists/members, DHCP servers/clients, IPv6 ND/prefixes, and routes. 403/404/empty auxiliary results produce warnings and never fail refresh.
2. Add pure `internal/service/topology.go` using `net/netip`: expand lists in include/exclude/static order, detect cycles, classify logical L3 interfaces as LAN/WAN/UNKNOWN with evidence, and preserve bridge/VLAN semantics.
3. Strong evidence includes LAN/WAN lists, DHCP server/client, ND/RA/prefix, known egress types, and active default route. Weak name/type/address/neighbor signals cannot defeat stronger evidence. Private addresses, ULA, static addressing, traffic interfaces, and tunnel addresses are never sufficient LAN proof.
4. Derive normalized IPv4 prefixes only from LAN interfaces (no automatic `/31`/`/32`); derive IPv6 from LAN ND prefix, advertised address, then usable LAN address. Exclude invalid/disabled, loopback, link-local, multicast, and neighbor-created `/128` scope. Support ULA, GUA, PD changes, public LAN IPv4, and multiple VLANs.
5. Add per-device `terminal_scope` auto/override config. Manual exclude wins; include/exclude conflicts fail validation. Legacy non-empty `terminal_cidrs` remains a legacy manual-inclusion mode and is not silently deleted or rewritten. `allowed_cidrs` remains unrelated.
6. Replace empty-scope-is-all discovery. DHCP maps lease server to LAN interface; ARP/neighbor require LAN interface and matching interface prefix unless explicitly included. Link-local may enrich an existing MAC terminal but cannot create an independent terminal or Internet attribution.
7. Conntrack orientation is exact router-self, known terminal, scope prefix, then ignore unknown; retain original-source ownership for local-to-local traffic. Never broaden router-self WAN addresses to their network.
8. Expose device-scoped terminal scope runtime mode/evidence/prefixes/warnings/overrides. Normal forms display read-only results; a collapsed advanced section edits overrides and warns about legacy mode. Dynamic automatic IPv6 prefixes are never editable.

## Acceptance criteria

- [ ] Unit tests cover list resolution/cycles/bridge semantics; role evidence/conflict/overrides; IPv4/IPv6 derivation including WAN-private and tunnel rejection, PD changes, and no neighbor `/128` scope.
- [ ] Service tests cover DHCP/ARP/IPv6-neighbor provenance, empty scope rejection, MAC merging, exact router-self, connection direction/original-source precedence, preserved names/remarks/history, and multi-device isolation.
- [ ] Config/API tests cover auto default, legacy compatibility, validation/canonicalization, masked export, evidence/warnings, and auxiliary-endpoint degradation.
- [ ] Normal device creation needs no IPv4/IPv6 CIDR; advanced controls are collapsed; automatic/legacy/warning displays work across selected devices, themes, and 375px viewports.
- [ ] Go tests, frontend lint/build, local runtime/browser verification, and the remote acceptance gate pass without credential disclosure.
- [ ] Before any commit: `10.0.0.6` has timestamped binary/config/SQLite backups, the existing systemd deployment is verified, and the user explicitly approves manual inspection at `10.0.0.6:8080`.

## Out of scope

- RouterOS writes, automatic list creation, broad RFC1918/ULA assumptions, data resets, SQLite rebuilds, automatic legacy deletion, unrelated refactors, commits, or pushes before manual acceptance.
