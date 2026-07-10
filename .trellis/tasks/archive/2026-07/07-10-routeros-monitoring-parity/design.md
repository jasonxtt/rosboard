# RouterOS monitoring parity and UI overhaul design

## Design Summary

Evolve the existing single-process Go/React application without changing its deployment shape. The backend will use domain-specific pollers and typed normalized snapshots, SQLite will store bounded time-series/accounting data, and the frontend will become a compact routed monitoring console.

## Boundaries

- RouterOS remains a read-only external system.
- SQLite owns panel-derived history, cumulative totals, and retention.
- The API exposes normalized monitoring contracts, not RouterOS field names.
- The frontend owns user interaction state such as sorting, filters, selected range, pagination, and refresh interval.
- RouterOS-native facts and panel estimates must be visibly distinguishable.

## Information Architecture

Use a two/three-level sidebar:

```text
System overview
Status monitoring
  Line monitoring
  Terminal monitoring
    All terminals
    IPv4
    IPv6
  Traffic monitoring
    Protocol statistics
    Policy statistics
  Runtime monitoring
    Load history
    Routing / split flow
```

Terminal detail remains inside the terminal section and uses a compact horizontal tab bar. Detail navigation state should be represented in the URL so refresh/back navigation is predictable.

## Backend Polling Design

Split refresh into domains with independent last-good snapshots:

1. `systemPoller`
   - system resource, health, storage where exposed
2. `interfacePoller`
   - interfaces, ethernet attributes, addresses, live rate/statistics
3. `terminalPoller`
   - DHCP, ARP, IPv6 neighbors, v4/v6 connections
4. `policyPoller`
   - existing queues, queue trees, mangle/filter counters, marks
5. `routingPoller`
   - routing tables/rules and active routes

Required core-domain failures keep the previous last-good domain snapshot and update its freshness/error metadata. Unsupported optional endpoints produce an unavailable capability rather than failing the whole refresh.

Independent RouterOS reads may run concurrently within the 25-second refresh deadline. Connection accounting is accumulated in memory and committed in one SQLite transaction per refresh.

## Domain Contracts

Add contracts for:

- `PollerStatus { name, updatedAt, stale, error }`
- `InterfaceStatus` extended with address, packets, errors/drops, link rate/duplex, role, route availability
- `InterfaceDetail { interface, samples, pollerStatus }`
- `Terminal` extended with primary addresses, state, onlineSince, trackingSince, evidence, lastSeen
- `TerminalConnection` extended with NAT/public address, mark fields, accurate/unknown egress line, current rates
- `LoadSample` and aggregated load series
- `ProtocolStat` and protocol time series
- `PolicyStat` with source kind and native counters
- `RouteStat` / `RoutingTableStat` with active state, gateway, distance, marks, and observed connection totals

## Terminal Evidence Rules

Use ordered evidence rather than treating all discovery rows as equally live:

1. Active connection involving the terminal: online and `lastSeen=now`.
2. Bound DHCP lease or reachable/current neighbor/ARP status: online/idle depending on RouterOS status fields.
3. Persisted identity without current evidence: offline; preserve the stored last-seen value.

`onlineSince` represents the current continuous online session. `trackingSince` remains the start of panel accounting and is labelled accordingly if shown.

Primary IPv4 is the numerically smallest current IPv4 unless a stronger current DHCP/connection address preference is available. IPv4-less terminals sort after IPv4 terminals. IPv6 sorting uses parsed 16-byte address order.

## Egress-Line Attribution

Do not reuse `primaryInterface` as line. Derive in this order:

1. native connection/routing mark mapped to an existing routing rule/table/route;
2. NAT/public address mapped to a RouterOS WAN address;
3. unambiguous active route/gateway evidence;
4. otherwise `Unknown`.

Expose attribution source/quality internally so the UI can avoid overstating inferred values.

## Time-Series and Retention

Store high-frequency samples in fixed buckets instead of one arbitrary row per UI refresh:

- recent: 5-second or configured poll interval buckets, retained 48 hours;
- hourly aggregates: retained at least 35 days;
- daily aggregates may be computed from hourly data for the one-month view.

Aggregate load, interface, terminal-count, and protocol statistics using the same bucket boundaries. Prune connection-state records not observed beyond a bounded TTL. Store terminal online sessions separately from traffic snapshots.

Schema changes are additive and applied by idempotent migrations in the existing store initialization path.

## API Shape

Keep `/api/dashboard` for the global shell, but add focused endpoints so large pages do not refetch unrelated data:

- `GET /api/interfaces`
- `GET /api/interfaces/{id}?window=1h`
- `GET /api/terminals?family=all|ipv4|ipv6`
- `GET /api/terminals/{id}`
- `GET /api/load?window=1h|1d|1w|1m`
- `GET /api/protocols?window=30m|1h|1d`
- `GET /api/policies`
- `GET /api/routes`

Sorting and filtering remain client-side for the current single-router scale. Pagination is client-side initially, with API contracts kept compatible with later server-side pagination.

## Frontend Design

Break the monolithic `App.tsx` into page/components and typed API modules only where reuse or independent state warrants it. Avoid introducing a third-party state manager.

Core reusable UI:

- sidebar navigation
- page header/status strip
- data toolbar
- sortable header
- pagination
- status/freshness indicator
- compact metric strip
- chart with shared axis/tooltip formatting

Use URL path/query parameters for active page, terminal detail, selected family, and history window. Keep sort/filter/pagination in local component state unless URL state materially helps sharing.

## Compatibility and Migration

- Preserve current config keys and defaults.
- Preserve existing terminal IDs and cumulative totals.
- Migrations must not drop current tables or reset accounting.
- New optional RouterOS endpoint failures must not prevent startup when the current core endpoints still work.
- Existing embedded build path remains unchanged.

## Risks and Mitigations

- RouterOS endpoint fields vary by interface and configuration: decode optional string fields and test normalization with fixtures.
- Full connection tables are expensive: request only required properties where supported, batch DB work, and avoid sending full connections in dashboard payloads.
- Native policy/routing state may be empty: model empty as valid.
- Historical schema can grow: bucket, aggregate, index, and prune.
- UI scope is large: land coherent vertical slices in the order terminal, line, load, then native traffic/policy/routing.

## Rollback

- Add schema and API changes compatibly so the previous embedded frontend can still read `/api/dashboard` during development.
- Keep new pollers optional until their normalized snapshot is valid.
- Each phase should leave `go test ./...` and frontend build passing.
