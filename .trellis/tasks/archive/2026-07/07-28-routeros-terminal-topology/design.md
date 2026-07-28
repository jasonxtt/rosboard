# Automatic terminal topology discovery design

## Flow

`RouterOS full-refresh menus -> pure topology functions -> immutable TerminalScope -> terminal refresh/conntrack/API projection`.

`internal/routeros` only reads typed menu snapshots. `internal/service/topology.go` has no client, store, or monitor lock and uses `netip`. The monitor replaces one complete scope at full refresh; terminal refresh reads that immutable snapshot. Exact `routerAssignedAddress` ownership remains independent: only an exact RouterOS address means `routeros:self`.

## Scope construction

Expand lists recursively as include, remove exclude, then add enabled static members; retain warnings for cycles and unknown members. Aggregate strong LAN/WAN evidence on logical address-bearing interfaces, apply manual exclusions then inclusions, mark unresolved strong conflicts UNKNOWN, and only then use weak signals. Tunnel types remain UNKNOWN unless explicitly included.

Generate masked, deduplicated IPv4 prefixes from LAN interface addresses (optionally connected-route validated). Generate IPv6 in order: valid LAN ND prefix, advertised LAN address, usable LAN address. Neighbor data never creates prefixes. Apply manual include prefixes and final excludes. Non-empty legacy CIDRs become manual legacy prefixes without rewriting YAML.

## Discovery, flow, and API

DHCP lease provenance follows DHCP server to interface. ARP/non-link-local neighbor records need LAN interface plus an interface prefix match unless explicitly included. Link-local only enriches an existing MAC identity. Conntrack evaluates router self, known terminal, prefix, then rejects unknown; local-to-local retains original source. Runtime projection is per device: mode/legacy/overrides, role evidence, prefixes, warnings.

## Compatibility and operations

`terminal_scope` defaults to auto and validates canonical CIDRs and conflicting interface overrides. `terminal_cidrs` stays unchanged until deliberate advanced save; `allowed_cidrs`, terminal store history, IDs, and password-safe projections remain intact. Optional helper failures degrade with warnings. Rollback restores one matching binary/config/SQLite (+ sidecars) backup bundle through the discovered systemd deployment.
