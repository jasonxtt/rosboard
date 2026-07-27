# Terminal route attribution

## Goal

Show which routing rule, table, and route each terminal connection uses, while keeping the existing route table and clearly distinguishing inferred results from RouterOS counters.

## Requirements

- Extend terminal connection data with route table, rule, route, and attribution-confidence fields.
- Evaluate policy-routing marks and longest-prefix matching against active routes.
- Collect and attribute both IPv4 and IPv6 routes and connections.
- Show unavailable and estimated states explicitly.
- Show current matched-connection counts derived from conntrack and label them as snapshot values.
- Preserve RouterOS-native mangle packet/byte counters as exact policy counters when present.
- Do not create or modify RouterOS rules to obtain counters.

## Dependencies

- Uses the existing route/routing-rule collection and terminal connection snapshots.
- Depends on the multi-device child to provide the device-scope contract before integration.
- Official RouterOS routing-rule and route properties provide lookup inputs but no generic cumulative hit counter; the agreed contract therefore uses current snapshot-derived connection counts.

## Acceptance Criteria

- [ ] Terminal details show rule/table/route attribution per connection when possible.
- [ ] Inactive, disabled, unmatched, ambiguous, and insufficient-data cases are represented correctly.
- [ ] Static route-table behavior is preserved.
- [ ] Current matched-connection counts are labeled as snapshot-derived, while native mangle counters remain distinguishable as exact RouterOS counters.
