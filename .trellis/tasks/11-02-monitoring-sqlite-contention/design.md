# Design

## Evidence and boundaries
See research/findings.md. Both expensive history reads and cleanup share a single device connection with realtime writes. Fix both sources of avoidable work; moving cleanup alone is insufficient.

## Bounded history retrieval
Use the existing `(device_id, terminal_id, ts)` primary-key index to execute `ORDER BY ts DESC LIMIT ?` per unique requested terminal, reuse the existing single-terminal reader where practical, and close each result before the next query. Preserve partial-failure semantics (return an error rather than partial success), per-device ownership, duplicate-ID behavior, and map contents. This is a deliberate small number of bounded indexed queries; a window function that ranks all historical rows would retain the scan cost.

## Retention indexes
Add idempotent indexes for `terminal_history(device_id, ts)` and `connection_state(device_id, last_seen)`. Existing interface/load/protocol primary keys already begin with device/time. Initialize indexes on both owner/default and per-device databases. Owner initialization must create them after legacy device-scope migration. Account for index build cost on large existing files during test-machine validation; no automatic VACUUM or database reset.

## Independent maintenance
One maintenance worker per Monitor, tied to its lifecycle context, runs at startup and then on a five-minute cadence independent of viewers and RouterOS results. Remove inline prune calls from full refresh. Start the maintenance worker before the initial refresh in Monitor.Start, guarded by a per-monitor sync.Once. MonitorManager.startMonitor/retryMonitor retry the same Monitor with the same manager lifecycle context; the guard prevents duplicate maintenance workers on collection startup failures. Cancellation stops the worker and cancellable batch waits. Existing Monitor instances are not restarted under new contexts after manager shutdown. Test this lifecycle explicitly.

Each pass uses its own bounded context, never the 5-second realtime or 15-second full-refresh store context. Delete by indexed selection of at most 500 rowids per statement; bind device and cutoff explicitly. Release the connection after each statement and yield with a cancellable short delay between batches. Bound each pass by time/work so sustained backlog cannot monopolize the device. Resume remaining expired data on a later pass. Compute fixed cutoffs per pass; preserve the strict `<` expiry boundary. Keep all table identifiers static/allowlisted.

Implemented limits: 500 rows per batch, 25ms after nonempty batches, a 30-second pass budget, and a two-second per-statement context. Tables with remaining backlog are visited round-robin. Row counts are not a hard latency guarantee; validate driver cancellation and concurrent sampling on synthetic data. Log maintenance failures separately; retain observability of collection failures.

## Compatibility and risk
No schema-destructive migration, retention-policy change, new dependency, API change, or SQLite pool expansion. Separate workers still share a connection: batch limits, indexes, and bounded history reads are all needed. Slow storage may exceed deadlines; tests must establish cancellation behavior rather than promise hard latency from row counts alone. Rollback restores the old binary; additive indexes are backward compatible, with production backup/acceptance governed by AGENTS.md.
