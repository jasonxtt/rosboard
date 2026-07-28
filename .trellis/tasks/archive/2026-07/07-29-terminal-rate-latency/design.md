# Terminal rate latency design

## Context

The existing background loop schedules both terminal refreshes and full
refreshes. It resets the next terminal deadline after each completed work item;
a full refresh also resets it. `refreshTerminals` fetches discovery and
conntrack sources together, and the browser then reads an in-memory snapshot
every three seconds. This composition produces the reported 6–10 second lag.

## Scope and data flow

```
RouterOS conntrack -> terminal-rate worker -> Monitor snapshot -> API cache -> terminal UI
RouterOS topology  -> discovery/full worker -> Monitor metadata cache -> terminal-rate worker
```

The rate worker is deliberately a snapshot producer. No GET endpoint performs
RouterOS work, preventing a collection storm from multiple tabs.

## Backend design

Add a terminal-viewer activity deadline and wake channel alongside the existing
general viewer state. `TerminalViewerHeartbeat` will extend the same 30-second
TTL and signal the rate scheduler only on its idle-to-active transition. The
new API accepts only `POST /api/terminal-viewer-heartbeat` and returns its
`activeUntil` timestamp.

Run the new scheduler in its own goroutine. It uses one-second ticker deadlines
while terminal viewing is active, a wake signal for the first refresh, and does
no RouterOS work while inactive. A dedicated terminal-rate mutex prevents rate
worker overlap but is separate from `refreshMu`, which continues to serialize
the discovery and full refreshes.

The rate refresh reads IPv4 conntrack and, when available, IPv6 conntrack in
parallel. It reuses the latest terminal scope, router-assigned addresses,
terminal address-to-identity mapping, metadata, and route matcher published by
the discovery/full refresh. It updates only current connection rows, current
per-terminal/family/flow/protocol rates, connection counts, terminal scope
summaries, and a `ratesUpdatedAt` timestamp. It neither reads DHCP/ARP/neighbor
data nor writes terminal identity, presence, totals, history, or protocol
history to SQLite.

The full/discovery commit records the metadata inputs needed by the rate worker.
If a rate worker finishes after a full refresh starts but before that full
refresh commits, the commit preserves the newer terminal rate projection,
analogous to the existing realtime overview merge. This avoids stale rate data
being published by a slower full refresh.

## API and UI contract

`TerminalDetail` gains RFC3339 `ratesUpdatedAt`. Its value is zero until a
successful terminal projection exists; the UI renders a non-disruptive freshness
label only when a timestamp is available.

The terminal route sends an immediate terminal-viewer heartbeat when entering
terminal monitoring or opening a detail, then renews it every 10 seconds while
the document is visible. The terminal list uses the selected
`dashboardRefreshMs`; a selected terminal detail uses a one-second interval
when automatic refresh is enabled. Both retain their non-overlap guards.

## Compatibility and rollback

Existing dashboard and terminal endpoints remain cached reads. No persisted
schema, RouterOS configuration, or config-key changes are introduced. If the
new rate collection fails, the last valid rate projection remains displayed and
the next one-second tick retries. Rolling back consists of restoring the prior
binary; no data migration is involved.

## Verification focus

Unit tests will cover terminal-viewer activity edge triggering, rate projection
for known IPv4/IPv6 identities, no mutation of persisted totals/history, and
newer rate data surviving a full-commit merge. API tests will cover method
restrictions and the returned deadline. Browser checks will confirm 1-second
detail polling, selector-controlled list polling, freshness text, and no
console errors.
