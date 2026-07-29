# Technical Design

## Scope and boundaries

This change extends the existing read-only RouterOS monitoring projection. It
does not alter RouterOS configuration, traffic-scope selection, aggregate WAN
traffic accounting, terminal identity, persistence schemas, or polling cadence.

Data flow:

```text
RouterOS interface/topology REST endpoints
  -> routeros typed payloads
  -> Monitor full-refresh projection
  -> DashboardSnapshot.Interfaces
  -> /api/dashboard and /api/interfaces/:name
  -> physical/logical/system interface sections

RouterOS routing rules + routing routes + conntrack
  -> routeMatcher
  -> TerminalConnection route attribution
  -> /api/terminals/:id
  -> egress summary and per-connection egress column
```

## Interface classification

### Sources

- Continue using `/rest/interface` as the complete interface inventory.
- Continue using `/rest/interface/ethernet` for Ethernet details and as the
  authoritative physical-interface membership set.
- Add read-only collection for `/rest/interface/vlan` and
  `/rest/interface/bridge/port`.
- Reuse the already collected `/rest/interface/pppoe-client` payload.

### API contract

Extend `InterfaceStatus` with additive fields:

```text
category: "physical" | "logical" | "system"
relations: [{ kind, interface }]
```

Relation kinds are semantic and intentionally small:

- `carrier`: PPPoE -> carrier interface.
- `parent`: VLAN -> parent interface.
- `bridge`: bridge member -> owning bridge.
- `member`: bridge -> member interface.

The frontend owns Chinese presentation labels for these stable values. Missing
or older relation arrays normalize to an empty array.

Classification order:

1. Loopback type -> `system`.
2. Present in Ethernet inventory -> `physical`.
3. Ethernet-type fallback when the Ethernet detail request is temporarily
   unavailable -> `physical`.
4. Every other interface -> `logical`.

This deliberately treats RouterOS/CHR Ethernet NICs as physical-facing
interfaces even when the host hardware itself is virtualized.

### Status and ordering

The UI derives a three-state display from existing fields:

- disabled -> `已禁用`
- enabled and not running -> `Down`
- enabled and running -> `在线`

Physical rows sort Down first, then online, then disabled, with interface name
as the stable secondary key. Logical rows retain a deterministic name order.
The system section is collapsed initially.

The interface page starts directly with the physical/logical/system sections.
It does not render a separate top summary toolbar or an aggregate interface
count elsewhere; each section continues to own its local count, and the
physical section continues to expose its Down count.

The `终端监控` navigation header is a true disclosure toggle. Expanding it
retains the existing behavior of opening the all-terminal list; collapsing it
changes only disclosure state and does not mutate the active view, family
scope, or selected terminal ID.

The existing interface-detail endpoint continues looking up by interface name;
classification and relations travel as part of the same `InterfaceStatus`, so
no new endpoint is needed.

## Route and physical-egress attribution

### Parsing

Extend route attribution with the selected logical route interfaces and an
additive list of resolved physical egress interfaces. For each equal-best route
candidate:

- `IP%interface` in `immediate-gw` yields gateway `IP` and logical route
  interface `interface`.
- A direct interface value such as `pppoe-out1` yields gateway and logical
  route interface both as `pppoe-out1`.
- If `immediate-gw` is absent, use a direct-interface `gateway` value only when
  it is not an IPv4 or IPv6 address.
- The logical route interface is then resolved through the same collected
  topology used by interface monitoring:
  - PPPoE -> carrier interface, recursively until physical.
  - VLAN -> parent interface, recursively until physical.
  - Already physical -> itself.
- WireGuard has no fixed physical carrier. A bridge may have several possible
  members. These and other ambiguous chains produce no physical egress instead
  of a guess.
- Unknown, cyclic, or unresolvable chains produce no physical egress interface.

Deduplicate gateway, logical-route-interface, and physical-egress lists while
preserving selected-route order. ECMP returns all equal-best candidates. The
existing `ambiguous` attribution state remains the UI signal for multiple
equal-best routes.

### Terminal API contract

Add `routeInterfaces: string[]` for logical route evidence and
`egressInterfaces: string[]` for resolved physical interfaces to
`TerminalConnection`. Do not replace or reinterpret
`Terminal.PrimaryInterface`, which remains the terminal's access interface
learned from ARP/neighbor data.

The connection table renders:

```text
路由表 | 下一跳网关 | 出接口
main   | pppoe-out1 | wan
```

The visible "出接口" value uses `egressInterfaces`, not the logical
`routeInterfaces`. It includes physical-egress sorting/filtering consistently
with the existing route-table and gateway controls. Terminal basic information
derives a sorted, deduplicated physical-egress summary from connections in the
selected All/IPv4/IPv6 scope.

### Connection-table viewport and controls

The connection table uses a connection-specific scroll viewport rather than
the generic unbounded `.table-scroll` behavior. The viewport has a responsive
maximum height derived from the visible browser height, `overflow: auto` on
both axes, and sticky header cells. Short result sets keep their natural height;
long result sets scroll internally so the native horizontal scrollbar remains
at the bottom of the visible table region.

Global search and global clear controls are removed from the connection detail
entirely, including their state, event handlers, toolbar, popover mode, desktop
overlays, and mobile tab-row actions. Column filter buttons continue to open the
existing floating filter panel outside the clipping scroll viewport.

IP-version filtering remains owned by the IP-version column header. In `all`
terminal scope it presents `全部 / IPv4 / IPv6`; in family-specific scope the
scope is authoritative and the opposite family is never offered or rendered.
Each enum-style column filter keeps its own `全部…` option for local reset.

The frontend omits the `publicAddress`/"外网地址" connection column and removes
its sort key. `TerminalConnection.publicAddress` remains an additive API field
for compatibility; this acceptance change is presentation-only.

## Compatibility and failure behavior

- All new JSON fields are additive.
- Missing frontend arrays normalize to empty arrays.
- Failure to collect VLAN or bridge-port topology retains the interface list
  and classification while omitting only unavailable relations and recording
  a collection warning/log consistent with existing optional topology reads.
- A route may still have valid route/gateway attribution while physical egress
  is unavailable. In that case the "出接口" UI shows `-`; it never infers a
  physical port from an interface name substring.
- WireGuard is classified as logical but receives no synthetic carrier.
- No SQLite migration is required.
- Connection presentation-state changes remain frontend-local and do not
  change terminal API payloads or polling cadence.

## Files expected to change

- `internal/routeros/types.go`, `internal/routeros/client.go`
- `internal/model/types.go`
- `internal/service/monitor.go`, `internal/service/routes.go`
- Focused Go tests in RouterOS/service/API packages as required
- `web/src/lib/types.ts`, `web/src/lib/format.ts`, `web/src/App.tsx`, and
  interface-specific CSS in `web/src/index.css`
- `README.md` only where it uses the old user-facing section name
- `.trellis/spec/backend/monitoring-contracts.md` after implementation if the
  new stable contracts are confirmed by tests

## Rollout and rollback

Build and verify locally first. Before remote replacement, copy the deployed
binary, `/opt/rosboard/config.yaml`, and SQLite data to one timestamped backup
directory on `10.0.0.6`. Deploy one binary containing both API and embedded
frontend to prevent version skew. Rollback restores the timestamped binary;
configuration and SQLite should remain byte-identical because this change has
no configuration or database migration.
