# Terminal scope summaries design

## Architecture and boundary

Compute the three scope summaries in the backend and add them to the existing
dashboard snapshot. The backend already owns terminal identity, online state,
selected-traffic-interface exclusion, WAN detection, and family-specific
conntrack statistics. Keeping aggregation there avoids a second, weaker LAN
classification in React.

Add an additive dashboard field:

```text
terminalScopeSummaries: {
  all:  TerminalScopeSummary,
  ipv4: TerminalScopeSummary,
  ipv6: TerminalScopeSummary
}

TerminalScopeSummary {
  deviceCount
  connectionCount
  currentUploadBps
  currentDownloadBps
  activeUploadBytes
  activeDownloadBytes
}
```

No new endpoint, RouterOS read, SQLite table, migration, or persisted counter is
required.

## Aggregation contract

Start from the complete terminal snapshot before any frontend search, filter,
sort, pagination, or page-size state.

1. Reuse the predicate behind `connectedLANDeviceCount` to retain only online
   LAN clients and exclude `routeros:self`, inactive/offline terminals,
   selected traffic interfaces, loopback, and WAN-like interfaces.
2. For each eligible terminal, increment `all.deviceCount` once.
3. Increment `ipv4.deviceCount` only when the terminal has at least one IPv4
   address; increment `ipv6.deviceCount` only when it has at least one IPv6
   address. A dual-stack device contributes once to each family but only once
   to `all`.
4. Sum `Terminal.FamilyStats.ipv4` into the IPv4 summary and
   `Terminal.FamilyStats.ipv6` into the IPv6 summary.
5. Build all-scope connection, rate, and active-byte values by adding those two
   family contributions. Do not use the persisted combined terminal totals.

The resulting invariant is exact for a single snapshot:

```text
all.connection/rate/active-bytes = ipv4 + ipv6
```

Device count intentionally does not obey that equation because dual-stack
devices are deduplicated in `all`.

## Frontend presentation

Render a `TerminalScopeSummary` only on the terminal list, not terminal detail.
Select `all`, `ipv4`, or `ipv6` directly from the current sidebar scope.

Desktop topbar uses three regions: title, summary, global controls. The summary
follows the existing terminal-detail metadata language and displays:

```text
设备 18  连接 126  ↑ 25.9 KB/s  ↓ 8.35 KB/s  活动累计↑ 19.2 GB  活动累计↓ 1.54 GB
```

Use existing `formatBits` and `formatBytes`, muted 11-12px text, 13px values,
tabular numerals, and wrapping gaps. Do not add a card, heavy border, new icon
set, tooltip, or interaction.

At widths below 768px, the topbar becomes a two-column first row for title and
global controls, followed by a full-width two-row/three-column summary. The
terminal filter toolbar remains unchanged.

## Compatibility and failure behavior

- The JSON field is additive; existing consumers can ignore it.
- The embedded frontend and Go server ship together, so no mixed-version API
  fallback is needed. If a summary key is unexpectedly absent, render zeros
  rather than reuse combined persisted totals and violate family semantics.
- Empty scope data renders six zero values with existing formatters.
- Dashboard refresh replaces the six values atomically from one snapshot.

## Trade-offs

- Backend aggregation adds a small response object but centralizes the
  authoritative LAN boundary and is directly unit-testable.
- Frontend-only reduction would touch fewer layers but would duplicate WAN and
  selected-interface rules that are not fully represented in the terminal
  table payload.
- Active cumulative bytes are intentionally snapshot-scoped. They are accurate
  by family and additive, but they are not lifetime usage.

## Rollback

Rollback is limited to the additive model field, aggregation helper, topbar
component/styles, and regenerated embedded assets. No stored data needs repair.
