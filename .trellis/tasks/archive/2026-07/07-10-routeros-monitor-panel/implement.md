# RouterOS Monitor Panel Implementation Plan

## Ordered Checklist

1. Scaffold repository structure
   - create Go module, frontend app, build wiring, and data/config directories
   - verify: clean bootstrap instructions work on the local machine

2. Implement RouterOS client and typed payload decoding
   - add endpoint clients for all verified `v1` reads
   - centralize payload decoding and normalization helpers
   - verify: local smoke command can fetch and print normalized snapshots from `10.0.0.1`

3. Implement SQLite schema and persistence layer
   - schema for router snapshots, interface samples, terminal identities, terminal cumulative totals, and connection counter cache
   - verify: migrations/init create a usable database from empty state

4. Implement pollers and aggregation services
   - overview poller
   - interface-rate poller
   - identity poller
   - connection-accounting poller
   - verify: polling cycle stores fresh samples and accumulates terminal totals without negative deltas

5. Implement HTTP API
   - overview endpoint
   - interface list/history endpoints
   - terminals endpoint
   - capabilities endpoint
   - verify: API responses are frontend-ready and do not leak raw RouterOS shapes

6. Build the dashboard UI
   - system overview
   - interface / line status
   - 5-minute chart
   - unified terminal table
   - capability notes area
   - verify: desktop layout works, data refreshes, and dense table content remains readable

7. Wire binary packaging and embedded assets
   - build frontend assets into the Go binary
   - add config loading and writable data directory handling
   - verify: built Linux binary serves the UI and persists data correctly

8. Add deployment artifacts
   - example config
   - `systemd` unit
   - operator README
   - verify: docs match actual binary/config behavior

9. Run focused quality checks
   - backend tests for normalization/accounting logic
   - API smoke checks
   - frontend build checks
   - verify: critical flows pass before task start completion report

## Validation Commands

Planned validation commands after implementation:

- `go test ./...`
- `go build ./...`
- `npm install`
- `npm run build`
- local smoke run against the live RouterOS target

If command names change during scaffolding, update this file before implementation review.

## Risky Areas

- connection-key stability for cumulative accounting
- terminal merge rules across IPv4, IPv6, and MAC discovery
- asset embedding / build pipeline between frontend and Go binary

## Rollback Points

- keep connection-accounting logic isolated so it can be debugged without destabilizing the rest of the dashboard
- keep API contracts stable even if UI composition changes
- keep deployment packaging separate from the monitoring logic so Docker can be added later without core rewrites

## Pre-Start Review Gate

Before `task.py start`:

- `prd.md` has no blocking open questions
- `design.md` matches the chosen single-router / Linux-binary / read-only scope
- `implement.md` reflects real validation commands and risk areas
- the user approves planning artifacts and implementation direction
