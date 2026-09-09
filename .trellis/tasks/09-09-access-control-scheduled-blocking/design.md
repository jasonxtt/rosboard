# Technical design: access-control scheduled blocking

## Scope and semantics

`AccessRule.Enabled` remains the master switch. With `enabled=false`, the rule
does not block regardless of schedule. With `enabled=true`:

- `schedule.mode=always` keeps the current all-day deny behavior.
- `schedule.mode=weekly` blocks only during the union of its windows.
- Windows use local wall-clock `HH:MM` values and Monday-to-Sunday canonical
  weekday names (`mon` through `sun`). The interval is `[start, end)` at
  minute precision. `start == end` is invalid; an all-day rule uses `always`.
- A window crossing midnight is split by the compiler into the source day's
  tail and the next day's beginning. Overlapping or adjacent windows are
  normalized deterministically; the API rejects invalid overlap if the existing
  validation boundary requires an explicit user correction.
- Legacy/missing schedule data normalizes to `always`. A weekly schedule must
  contain at least one window and is bounded to prevent RouterOS rule
  explosion.

The schedule is evaluated by RouterOS using its configured local timezone. The
API may return read-only clock metadata for the UI, but the stored schedule is
not converted to server or browser time.

## Domain and persistence

Add typed schedule/window values in `internal/accesscontrol/model.go`, with
validation, canonical ordering, duplicate-day removal, cross-midnight
expansion, and RouterOS projection helpers kept pure and unit-testable.

Add `schedule_json TEXT NOT NULL` to `access_rules` and advance the access
schema marker from v2 to v3. Fresh schemas, v1/v2 upgrades, and existing rules
must all get the canonical `always` JSON default. Once the v3 marker is
committed, missing required columns remain a fail-closed schema error rather
than being silently recreated.

Every load/save path must carry schedule data, including:

- ordinary rule reads and writes;
- access-proposal commit transactions;
- revision/audit before/after snapshots;
- device-scoped queries and response builders.

The existing production backup contract covers rollback of a v3 database with
an older binary; do not introduce a partial compatibility path that can silently
discard schedule data.

## API contract

Extend access-rule request/response types with:

```json
{
  "schedule": {
    "mode": "always|weekly",
    "windows": [
      {"days": ["mon", "tue"], "start": "20:00", "end": "22:00"}
    ]
  }
}
```

Absent schedule on an old request is `always`; responses always return the
canonical schedule. Schedule edits participate in the same plan hash, stale
revision/CAS, desired revision bump, and apply flow as other rule edits.

If RouterOS clock metadata is added to the overview, it is informational only:
timezone name, current offset/DST state, and a clear warning when the device
uses a manual or unavailable clock configuration. It must not change the
meaning of a stored schedule.

## RouterOS desired-state projection

Keep access enforcement inside the existing typed RouterOS client and desired
object/reconcile system.

- For target-list rules, keep the deny chain unchanged and put the compiled
  time matcher on each managed jump activation object. Outside the window the
  packet does not enter the deny chain.
- For internet-scope rules, expand the direct protocol/direction objects per
  compiled window, or use a managed subchain if required by the existing object
  identity/order contracts. Preserve TCP reset and UDP/other drop behavior.
- Generate deterministic object identities and ordering for both address
  families and all windows.
- Add `time` to the managed RouterOS field allowlist so weekly-to-always changes
  remove stale actual-only fields.
- Extend access capability/mutation probes to verify the `time` matcher on each
  supported firewall family. A capability failure must not silently claim a
  scheduled rule is applied.
- Preserve the existing access-before-broad-accept/FastTrack ordering and
  readback verification.

## Frontend

Extend the canonical policy types and API boundary first. Add a shared pure
schedule helper (for example `web/src/features/policy/timeSchedule.ts`) for
normalization, validation, weekday labels, cross-midnight display, and compact
serialization. Aurora and Compact components should be thin UI adapters around
that helper.

Aurora's access-rule modal gets a `When` section. Compact's current disabled
placeholder becomes the same editor. Both must display wording equivalent to
"Block access during these times", show RouterOS timezone context, and summarize
multiple windows without overflowing tables/cards.

## Verification and rollout

Add focused backend tests for model/compiler, migration/round-trip, proposal
atomicity, API compatibility, desired projection, capability probing, rule
ordering, and stale-time cleanup. Add frontend tests for serialization,
validation, display, and old-response fallback. Run all Go checks, all web
checks, and the embedded UI build check.

After a committed checkpoint, send the branch/commit to the referenced review
conversation. Fix every blocking finding on the same branch and resubmit. Only
after review approval may the disposable test machine be used; production
delivery remains subject to the separate acceptance gate.
