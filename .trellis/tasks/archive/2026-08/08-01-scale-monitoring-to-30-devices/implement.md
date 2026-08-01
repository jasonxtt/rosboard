# Implementation and verification plan

## Phase 1: Establish measurable baselines

1. Add focused instrumentation around RouterOS request duration, per-device
   collection duration, persistence wait/commit duration, queue depth, and
   row/batch counts without changing collection behavior.
   - Verify: existing tests pass; logs identify device and stage; a local run
     records a baseline for the current three-device setup.
2. Build a controlled RouterOS test server or fixture that can serve multiple
   devices, configurable response sizes, latency, errors, and conntrack churn.
   - Verify: deterministic scenarios cover 10, 20, and 30 devices plus one
     slow/unavailable device.

## Phase 2: Isolate the runtime scheduler

3. Introduce a device-owned runtime/scheduler abstraction while preserving the
   current `Monitor` API and viewer-aware cadence.
   - Verify: scheduler unit tests prove per-class coalescing, no duplicate
     in-flight work, startup jitter, retry backoff, and device failure
     isolation.
4. Split RouterOS collection, derivation, persistence submission, and history
   query contexts. Add per-device in-flight limits and stage-level degradation.
   - Verify: injected RouterOS delays do not consume the persistence deadline;
     realtime snapshots and healthy-device snapshots continue advancing.

## Phase 3: Batch durable writes

5. Introduce a typed collection batch and a single transaction application path
   that preserves all current terminal merge, address, presence, totals,
   history, protocol, and device-scope invariants.
   - Verify: existing store tests plus new duplicate-address, merge ordering,
     counter delta, rollback, and batch atomicity tests pass.
6. Move pruning to bounded maintenance work and add retention/row-count guards
   for high-churn connection state.
   - Verify: maintenance cannot block a normal collection commit indefinitely;
     database size and row counts remain bounded in the churn fixture.

## Phase 4: Per-device persistence and migration

7. Implement device database ownership, WAL/read configuration validated against
   the bundled SQLite driver, and per-device ordered persistence lanes.
   - Verify: concurrent device writes do not share a connection-pool queue;
     history reads remain bounded; same-MAC and same-key data stay isolated.
8. Implement restartable legacy shared-database migration with validation,
   timestamped backup/rollback support, archive/purge updates, and old-binary
   compatibility checks.
   - Verify: legacy fixtures migrate all device scopes correctly; interruption
     and rerun are safe; rollback opens the pre-migration database.

## Phase 5: Cross-layer quality and scale validation

9. Update API/service tests for stale snapshots, per-device errors, fleet cache
   behavior, bounded history reads, and unchanged auth/onboarding/archive
   contracts.
   - Verify: `go test ./...`, `go test -race ./...`, `go vet ./...`, and any
     frontend checks pass.
10. Run the 10/20/30-device load profiles for at least 60 minutes, including
    active viewer heartbeats, history requests, high conntrack churn, one slow
    device, and one unavailable device.
    - Verify: no unbounded queue growth; healthy snapshots meet the configured
      freshness budget; device-scoped errors are observable; no global database
      deadline failures occur.
11. Perform local runtime and visual/API verification, then deploy the verified
    build to `10.0.0.6` only after timestamped backups of the binary,
    configuration, SQLite data, and relevant sidecars.
    - Verify: systemd service, health endpoint, affected device/fleet/history
      contracts, embedded frontend assets, and remote database migration.
12. Stop for explicit user manual inspection of the deployed instance. Do not
    commit or archive the task until the user approves the deployment.
