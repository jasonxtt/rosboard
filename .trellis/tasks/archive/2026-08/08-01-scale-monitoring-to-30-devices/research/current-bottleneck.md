# Current bottleneck research

## Scope

This research is read-only. No runnable source code was changed while
investigating the reported `begin replace addresses: context deadline exceeded`
failure.

## Repository findings

- `store.Open` creates one `*sql.DB`, sets `SetMaxOpenConns(1)`, and
  `Store.ForDevice` creates device views over that same database connection.
- `MonitorManager` creates one `Monitor` per enabled device, but starts each
  monitor independently. Each monitor has its own refresh mutex, so the mutex
  does not serialize work across devices.
- A visible viewer activates every enabled monitor. Each active monitor has
  independent realtime, terminal/full, and terminal-rate workers.
- Terminal/full collection uses one 25-second context for RouterOS requests,
  in-memory derivation, and subsequent SQLite writes.
- `buildTerminals` currently performs repeated store operations while iterating
  identities and connections. These include merge transactions, terminal
  upserts, address transactions, presence updates, history writes, and a final
  connection snapshot transaction.
- `ReplaceTerminalAddresses` only fails at `begin replace addresses` when the
  transaction cannot begin before the supplied context expires. The error does
  not indicate malformed address text or an address uniqueness conflict.

## Remote observations

- The remote service has three enabled devices sharing one data directory and
  database.
- Journal entries from July 30 through August 1 contain database deadline
  failures in multiple unrelated stages, including connection-state moves,
  terminal totals, presence updates, terminal history, terminal merges, and
  address replacement. This is a shared queue/deadline symptom rather than an
  address-table-only defect.
- The same period contains repeated RouterOS REST deadline failures against
  `192.168.2.1`, especially IPv4 conntrack, plus ARP, IPv6 address/neighbor,
  and IPv6 conntrack requests. This is an intermittent upstream trigger.
- At the time of read-only inspection, direct requests to all three configured
  routers completed successfully, and the current SQLite read-only queries
  completed quickly. This rules out a permanently locked or obviously corrupt
  database, but not intermittent local write pressure.
- The remote database contains device-scoped historical state, including a
  materially larger number of connection-state rows than current terminals.
  This reinforces the need to batch current-state persistence and bound/prune
  churn, but does not by itself prove that row volume is the only cause.

## Initial conclusion

The failure is caused by an interaction between intermittent RouterOS latency
and a program-side global SQLite connection bottleneck. Increasing the shared
deadline would mask the symptom and increase cross-device blocking. The target
architecture should isolate device runtimes and persistence, use stage-specific
budgets, and commit collection results in bounded batches.

## Relevant source/spec references

- `internal/store/sqlite.go`
- `internal/service/manager.go`
- `internal/service/monitor.go`
- `.trellis/spec/backend/database-guidelines.md`
- `.trellis/spec/backend/monitoring-contracts.md`
