# Slice 1 Technical Design

## Boundary

`internal/diagnostics` owns report construction and quick checks. It consumes
the existing monitor manager, update manager, and device repositories directly
as read-only sources so the policy and monitor implementations remain the
source of truth. `internal/api` only resolves the device, wires dependencies,
and serializes the report.

## Report model

`DiagnosticReport` contains `GeneratedAt`, `Mode`, `DeviceID`, `Overall`, and
ordered `Findings`. `DiagnosticFinding` contains stable `ID`, `Group`,
`Status`, `Title`, `Summary`, optional `Recommendation`/`Evidence`, and
`AffectsOverall`. Overall status is derived only from findings with
`AffectsOverall=true`; disabled/skipped findings never participate.

The overall calculation distinguishes `healthy`, `warning`, and `error`.
Core RouterOS/Monitor/storage failures affect the result. Optional MosDNS and
update failures are visible in their own findings but do not mechanically make
the core report error.

## Quick sources

- Monitor: `MonitorManager.Statuses` plus the selected monitor snapshot. Use
  started/startup error and snapshot freshness as the primary health signal;
  do not treat every dashboard alert/warning as a monitor failure.
- MosDNS: `MonitorManager.MosDNSStatus`.
- Policy: device `PolicyRepository.GetDeviceState`.
- Access: device `AccessRepository.GetState`; the device-level apply job is
  the policy device state's `Job`, matching the existing Access API.
- Update: `update.Manager.Status` (panel-global).
- Runtime/storage: config/build metadata, data-directory/statfs checks, and a
  bounded read-only SQLite probe through the existing store.

## API/UI

`GET /api/diagnostics?device=<id>` is dispatched before the generic monitor
routes. Auth and allowed-CIDR handling remain in `Server.ServeHTTP`.

`web/src/features/diagnostics/` contains the single frontend parser/types and
fetcher. Aurora's Settings page and Compact's Settings page only select the
device, invoke the fetcher, and render findings; neither recomputes health.

No `internal/policyv2` discovery or RouterOS client code is changed in this
slice.
