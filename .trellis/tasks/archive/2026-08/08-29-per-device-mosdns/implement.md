# Implementation Notes

## Changes

- `internal/config`: `DeviceConfig.MosDNS` added; global `Config.MosDNS`,
  `recognition_defaults_migrated`, legacy recognition migration, and
  `ROSBOARD_MOSDNS_*` env overrides removed. Per-device normalization runs in
  `normalizeDevices` (and the legacy single-device branch).
- `internal/store`: DNS tables + `mosdns_state` added to `initDeviceSchema`;
  owner-only guards removed from all DNS methods; `migrateLegacyDNSScope`
  wipes legacy global DNS rows once (marker `dns_scope_migrated` in
  `mosdns_state`); `purgeDNSData` added and wired into `PurgeDevice`.
- `internal/service`: per-device `MosDNSSynchronizer` (owned by
  `managedMonitor`, started in `Start`) and per-device `ApplicationResolver`
  bound to the device store. `MosDNSStatus(deviceID)` is device-scoped;
  `RecognitionStatus` keeps only the feature library.
- `internal/api`: device create/update payload carries `mosdns`;
  `settingsDevice` projects it; device status carries live `mosdns` status;
  `/api/settings` + `/api/settings/recognition` drop global mosdns;
  `/api/mosdns` and `/api/mosdns/observations` require `?deviceId=` and serve
  the device store via `store.ForDevice`.
- `web`: recognition settings section keeps protocol analysis + feature
  library; device settings gains a "MosDNS 应用识别" card (enable, address,
  interval) with per-device sync counters from the device status payload.
- Docs: `configs/config.example.yaml`, `runtime-configuration.md`,
  `database-guidelines.md` updated.

## Verification

- `go vet ./...`, `go test ./...` all pass; web `tsc`, `vite build`, oxlint pass.
- `go test -race ./internal/api` surfaces a pre-existing race in the test
  helper's restart counter (`newDeviceOrderTestServer`), reproduced on HEAD.
- Local smoke run: legacy global `mosdns:` ignored at startup; per-device
  synchronizer dials the device's own MosDNS address; `/api/health` 200;
  unauthenticated API endpoints correctly require auth.

## Review fixes (external review, 8 findings)

- P1 Device PUT now preserves `ProtocolAnalysis` / `FeatureLibrary`
  (`device_validation.go`), not just MosDNS.
- P1 Legacy DNS scope wipe is a versioned marker (`"2"`) that also deletes the
  unscoped watermark keys; upsert of the marker fixed a UNIQUE-constraint
  startup failure found on deploy (regression test added).
- P1 `ResetAll` purges DNS data from open child stores and every closed
  device database file under `data/devices/`.
- P1 `ForDevice` returns nil instead of aliasing the owner database when a
  child store fails to open; observations API rejects nil stores.
- P1 `/api/protocols` fallback device resolution mirrors
  `MonitorForDevices` (first enabled + RouterOS-configured device).
- P2 Feature-library cache names get a sha256 suffix when the device ID
  required sanitizing (collision-safe, mirrors device DB naming).
- P2 MosDNS synchronizer init failures surface in status `LastError`
  (`mosdnsInitErr` on managedMonitor, all three status projections).
- P2 Frontend protocols-view redirect waits for settings before judging the
  per-device switch.
- Docs: PRD/design rewritten to final per-device scope; runtime
  configuration spec no longer describes the removed global protocol-analysis
  migration.

## Incident note

First deployment of the review-fix build failed startup for ~3 minutes
(UNIQUE constraint on `dns_scope_migrated`); fixed with an upsert and
redeployed. Service healthy after (health 200, no errors).

## Pending

- User manual acceptance on 10.0.0.6, then commit (recognition files only —
  the working tree also holds another session's in-flight policy-routing WIP,
  which must not be committed here) and Trellis archival.

