# Multi-device monitoring scale design

## 1. Design goals and workload envelope

The design targets 10, 20, and 30 enabled devices, with the 30-device profile
used for acceptance. The baseline profile should represent the existing active
defaults: realtime sampling around one second, terminal discovery around five
seconds, full refresh around ten seconds, at least two selected traffic
interfaces, approximately 100 discovered terminals, and approximately 500
current conntrack rows per device. The load harness must also vary these
figures and include high-churn conntrack data.

The target is isolation and bounded work, not a promise that every device can
be polled at the same rate under arbitrary RouterOS response sizes. When a
device cannot meet its budget, that device becomes stale/degraded while other
devices continue.

## 2. Runtime topology

```text
MonitorManager
  ├── DeviceRuntime[id A]
  │     ├── RouterOS client
  │     ├── priority-aware scheduler
  │     ├── collector / immutable batch builder
  │     ├── in-memory snapshot + freshness/error state
  │     └── ordered device persistence lane + database
  ├── DeviceRuntime[id B]
  └── ...
```

`MonitorManager` remains responsible for device inventory, fleet projection,
viewer activation fan-out, and lifecycle. It must not be the hot-path lock for
all device persistence. Each `DeviceRuntime` owns the complete lifecycle of
one device and exposes the existing monitor-facing behavior through the
current service boundaries.

The runtime has four logical job classes:

- `realtime`: short resource/traffic reads and volatile in-memory samples;
- `terminal`: address, lease, neighbor, ARP, and conntrack discovery;
- `full`: low-frequency topology, policy, route, DHCP, and static metadata;
- `terminal-rate`: conntrack-only projection for a visible terminal view.

Jobs are coalesced by class. A due signal never creates a second in-flight job
of the same class. Terminal and full jobs share a per-device collection lane;
realtime and terminal-rate have separate short lanes but publish through the
same snapshot merge rules. Each device gets a deterministic initial phase
offset and bounded retry backoff so startup and heartbeat activation do not
create a thundering herd.

## 3. Collection and deadline boundaries

The collector should use separate contexts for:

1. each RouterOS endpoint or endpoint group;
2. the in-memory derivation and batch construction;
3. persistence submission/commit;
4. optional history reads requested by the API.

The current 25-second context must not be passed through all four phases. A
slow optional endpoint should be independently degradable. Required endpoint
failure preserves the last valid sub-snapshot and marks the device/stage
stale; it does not discard unrelated realtime data. Persistence receives a
bounded context of its own and reports queue saturation explicitly.

Each device should also have a small RouterOS in-flight budget. The exact
value is a benchmark result, but it must prevent full, terminal, realtime,
and retry work from multiplying unboundedly for a single slow router. A
per-device circuit/backoff state should suppress immediate repeated retries
after consecutive deadline failures.

## 4. Batch persistence model

The collector produces a typed `CollectionBatch` containing normalized terminal
builders, address ownership, connection snapshots, protocol aggregates,
interface samples, and the metadata required to publish the in-memory snapshot.
The batch is derived completely before persistence begins.

The persistence layer applies one ordered transaction for the durable terminal
state of a collection cycle. The transaction owns the existing invariants for:

- temporary-to-stable terminal identity merges;
- terminal and address upserts/replacements;
- presence and online timestamps;
- connection-state deltas and terminal totals;
- terminal history and protocol samples.

The implementation must not call one transaction per terminal or per observed
connection. It must preserve merge ordering and duplicate-address behavior by
performing the same upsert/delete logic inside the batch transaction. Realtime
display samples may be coalesced when persistence is behind; ordered
connection-counter state cannot be silently dropped because it is used to
calculate deltas.

Pruning is moved out of the critical collection commit or made a bounded,
low-priority maintenance job. Connection-state churn needs explicit retention
and row-count measurements so a high-churn router cannot grow the database
without bound.

## 5. Storage isolation choice

### Recommended: one monitoring database per device

Keep the owner/auth database for administrator and application state, and place
device monitoring tables under a device-specific path such as
`data/devices/<device-id>.db`. Each `DeviceRuntime` owns its database handle;
terminal state is applied in bounded ordered transactions. SQLite WAL mode
allows the device database to be maintained independently from every other
device, while the per-device serialized writer preserves predictable write
ordering.

This gives the 30-device target a hard isolation boundary: a slow or heavily
churning device cannot occupy the only connection for every other device. The
existing device-scoped schema remains useful within each file, but the
`device_id` column should remain during the first migration for compatibility
and defensive validation.

### Rejected as the target: one shared database with more connections

Raising `SetMaxOpenConns` on the existing shared file does not solve SQLite's
single-writer behavior. It can trade the current pool wait for cross-device
database locks and unpredictable write starvation. It also leaves one device
able to create global pressure. A single-file design could be revisited only
if a benchmark proves a bounded writer queue and WAL configuration meet the
30-device profile, but it is not the default direction.

### Migration and rollback

On first startup after the change, detect the legacy shared monitoring tables,
create per-device stores, and copy rows by `device_id` in a recoverable
migration. Keep the original database untouched until every device copy is
validated. The migration must be restartable and idempotent, and must preserve
the existing default-device legacy mapping. A timestamped backup is mandatory
before deployment; an old binary must be able to use the legacy database if
the new binary is rolled back.

## 6. API and snapshot behavior

Dashboard, realtime, terminal, fleet, and device-status APIs continue reading
in-memory snapshots for live data. A stale/degraded device still returns its
last valid snapshot plus explicit freshness/error metadata. Fleet overview must
not synchronously fan out to every device database.

History APIs read only the selected device store with a bounded request
context, explicit device ID, and result limits. A slow history query must not
block a collection writer or another device. Authentication, onboarding,
archive, purge, and existing device selection semantics remain unchanged.

## 7. Diagnostics and operational contracts

Every collection and persistence error must include device ID/name, job class,
stage, attempt, elapsed time, and whether time was spent waiting for a queue,
RouterOS response, SQLite transaction, or API request. Repeated identical
errors should be deduplicated or rate-limited in the dashboard alert stream.

Expose enough internal counters or debug output to measure:

- collection duration and last successful timestamp per job class;
- RouterOS request duration/error by endpoint and device;
- persistence queue depth, batch row count, wait duration, and commit duration;
- database size and connection-state row count per device;
- dropped/coalesced realtime samples and retry/backoff state.

## 8. Compatibility risks

- Per-device database migration can affect backup, purge, and archive paths;
  these must be tested before any deployment.
- A batch transaction changes failure boundaries. Snapshot publication must occur
  only after the durable state required by the existing monitor contract is
  committed, unless the contract explicitly marks the field volatile.
- WAL and multiple read connections require driver-specific validation with the
  bundled `modernc.org/sqlite` version; do not assume a generic SQLite DSN is
  correct.
- Historical data from the shared database must never cross device boundaries,
  including same-MAC terminals and connection keys.
- Existing user frontend changes are unrelated and must remain untouched.
