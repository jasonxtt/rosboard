# Fleet dashboard design

## Scope and boundaries

The feature adds a fleet-level read model over the existing per-device monitor
cache. It does not alter RouterOS collection, device configuration, database
history, device archival rules, or the existing per-device dashboard payload.

```text
MonitorManager cached snapshots
        |
        v
GET /api/fleet-overview
        |
        v
PanelApp fleet state -> list or icon projection -> selected device overview
```

## Backend contract

Add a fleet projection owned by `service.MonitorManager`. It iterates enabled,
non-archived configured devices, reads each monitor's existing snapshot once,
and returns a compact typed result. The API route is authenticated like the
existing operational APIs and does not accept a `device` query parameter.

Each entry contains the stable device ID and display name, collection state and
brief error text, RouterOS identity, snapshot update time, CPU load, memory
usage, current upload/download bit rates, connected terminal count, connection
count, uptime, and current alert/warning state. The response also contains the
four header counts.

`online` means an enabled monitor with a successfully started collector and a
current snapshot. An entry is `offline` when it has no usable monitor snapshot
or the collector is unavailable. An entry is `alerting` when it is offline or
its current snapshot reports alerts or warnings. The implementation must make
the freshness threshold explicit and account for the monitor's viewer-aware
idle cadence; while the fleet page is visible, the existing global viewer
heartbeat already activates every enabled monitor.

The endpoint has no storage work and must never invoke a RouterOS client. The
manager, rather than API handler or React code, owns all fleet classification
and count derivation, so list and icon views cannot diverge.

## Frontend state and navigation

Extend `ActiveView` with a fleet dashboard value and make it the default
landing view for new preferences. Keep `overview` as the selected-device
detailed page. Add `fleetView` (`list | icons`) to the persisted panel
preferences; older local storage values normalize to `list`.

Fetch `/api/fleet-overview` only while the fleet view is active, using the
existing global auto-refresh interval and a guarded single in-flight request.
The existing global heartbeat remains mounted and therefore maintains all
enabled monitors. Do not fetch `/api/dashboard` once per displayed device.

Fleet-local presentation state owns search text, status filter, name sort,
page, and view mode. Filtering and sorting happen before pagination; changing
search, status, sort, or representation resets/normalizes the current page as
needed. Selecting an entry sets `selectedDeviceID`, clears terminal detail
state, and changes `activeView` to `overview`; the existing scoped dashboard
loader then supplies the detailed page.

## Visual layout

The dashboard begins with four summary tiles, followed by a compact search and
control bar. The list is a responsive table-like set of full-width rows with
these columns in order: identity, CPU, memory, traffic rate, terminal count,
connection count, uptime/update. Reuse existing formatting helpers and status
colors. An unavailable row keeps its identity and trailing timing area but
shows a clear unavailable message across measurement columns.

Icon mode uses the same entry component data in a responsive card grid. Cards
preserve the metric order semantically, but use a vertical grouping suitable
for narrow widths. Both modes use the same click target and accessibility
label; neither adds per-device action buttons.

## Compatibility, rollout, and rollback

The new endpoint and TypeScript types are additive. Existing device endpoints,
dashboard payloads, selected-device storage key, and historical data remain
unchanged. A failed fleet response renders an explicit page-level error without
clearing any existing selected-device dashboard data. Rolling back consists of
removing the additive sidebar view and endpoint; no migration or persisted
server state exists.
