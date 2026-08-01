# Scale multi-device monitoring to 30 RouterOS devices

## Goal

Redesign the monitoring runtime and persistence path so rosboard remains
responsive and device-isolated with 10, 20, and 30 enabled RouterOS devices.
The existing single-device API behavior and device-scoped data semantics must
remain compatible.

## Requirements

- A slow, unavailable, or overloaded RouterOS device must not block healthy
  devices from collecting, publishing snapshots, serving APIs, or retrying.
- Collection network deadlines must be independent from persistence deadlines;
  an exhausted RouterOS request budget must not cause an unrelated SQLite
  operation to fail with `context deadline exceeded`.
- Realtime, terminal discovery, terminal-rate, and full-refresh workloads must
  be scheduled per device with bounded concurrency, no duplicate in-flight
  work of the same type, and staggered execution across devices.
- Terminal identity merges, address updates, presence, connection state,
  totals, history, and protocol samples must preserve their current atomic and
  device-scoped correctness while avoiding one transaction per small operation.
- Persistence must be isolated enough that one device's write pressure cannot
  exhaust a global database connection queue for every other device.
- Cached dashboard and fleet APIs must continue to serve the last valid snapshot
  while collection or persistence for one device is degraded.
- Existing YAML configuration, authentication, onboarding, archive/purge
  behavior, API device scoping, and legacy database migration semantics must be
  preserved unless an explicitly reviewed compatibility change is required.
- Logs and runtime diagnostics must identify the device, workload, phase,
  queue/wait state, and failure class so RouterOS latency can be separated from
  local persistence pressure.
- The implementation must include a reproducible load-test scenario for 10, 20,
  and 30 devices, including slow and unavailable devices, high conntrack
  volumes, active dashboard viewing, and history queries.

## Acceptance Criteria

- [ ] A 30-device controlled workload can run for at least 60 minutes without
      unbounded collection or persistence queue growth.
- [ ] Under the 30-device workload, one injected slow/unavailable device does
      not stop healthy-device snapshots from advancing within their configured
      polling budget.
- [ ] Healthy devices do not emit database-pool deadline errors caused by the
      degraded device; errors are device-scoped and the last valid snapshot is
      retained.
- [ ] Batch persistence preserves terminal identity merge invariants, address
      ownership, connection totals, history, protocol samples, and device
      isolation; focused store tests and full Go tests pass.
- [ ] Startup and viewer activation do not create a synchronized thundering
      herd of full, terminal, realtime, or history work.
- [ ] Existing dashboard, realtime, fleet, history, onboarding, archive, and
      purge API contracts remain compatible and their tests pass.
- [ ] The deployment gate is completed: automated checks, local runtime/load
      verification, timestamped remote backups, deployment to `10.0.0.6`,
      systemd/health/API/embedded-asset verification, and explicit manual user
      acceptance before any work commit.

## Constraints

- RouterOS access remains read-only.
- SQLite remains the supported local persistence technology unless the design
  review explicitly approves a different migration target.
- Do not solve the issue only by increasing context deadlines or opening an
  unbounded number of SQLite connections.
- Existing user changes in the working tree are outside this task and must not
  be overwritten.
