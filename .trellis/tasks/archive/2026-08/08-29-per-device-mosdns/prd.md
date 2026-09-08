# PRD: Per-device recognition (MosDNS + protocol analysis + feature library)

## Problem

Recognition settings (识别设置) were a single process-global set: one MosDNS
source, one protocol-analysis master switch, one feature library. With
multiple RouterOS devices this cross-contaminates application labels on
overlapping LAN subnets, prevents per-router MosDNS instances, and forces one
global feature-library configuration.

## Decision (confirmed with user, evolved over review)

1. **Everything in 识别设置 is per device**: protocol-analysis master switch,
   MosDNS integration, and the feature library. No global recognition
   settings remain.
2. The recognition page lives as a second-level submenu under 主机设置,
   below 策略路由, and follows the top device switcher: it shows and saves
   only the selected device's recognition set.
3. **No compatibility migration.** Legacy global `mosdns:`,
   `protocol_analysis:`, and `feature_library:` YAML sections are ignored and
   removed on the next save; legacy global DNS data (observations, learned
   features, and the unscoped watermark) is wiped once. Devices start with
   recognition off until configured.

## Scope

- `internal/config`: `DeviceConfig` owns `ProtocolAnalysis`,
  `FeatureLibrary`, `MosDNS`; global sections, `ROSBOARD_MOSDNS_*` /
  `ROSBOARD_FEATURE_LIBRARY_*` env overrides, and the recognition/protocol
  migrations are removed.
- `internal/store`: DNS tables + `mosdns_state` exist in every device
  database; versioned one-time wipe (`dns_scope_migrated` marker, includes
  watermark); `purgeDNSData` wired into `PurgeDevice` and `ResetAll`
  (including closed device database files); `ForDevice` returns nil instead
  of aliasing the owner database when a child store fails to open.
- `internal/service`: per-device `MosDNSSynchronizer`,
  `FeatureLibrarySynchronizer` (isolated cache file per device,
  collision-safe naming), and `ApplicationResolver`; device-scoped
  `RecognitionStatus(deviceID)` / `MosDNSStatus(deviceID)`; init failures
  surface in status `LastError`.
- `internal/api`: recognition settings saved per device through
  `POST /api/settings/recognition`; device writes preserve recognition
  fields they do not carry; `/api/protocols` gates on the requested device's
  switch with the same device fallback as monitor resolution;
  `/api/recognition|mosdns|mosdns/observations` require `deviceId`.
- `web`: recognition page under 主机设置 > 识别设置 renders the selected
  device's three fieldsets; protocols view gating waits for settings and
  follows the selected device.

## Out of scope

- Policy-routing domain/IP sources (separate task, different session).

## Acceptance Criteria

- [x] Two devices can each point at a different MosDNS instance and feature
      library; observations, learned features, watermarks, caches, and sync
      status never cross devices.
- [x] A legacy YAML with global recognition sections still starts; the
      sections disappear on the next save; legacy DNS data is wiped once.
- [x] Device edits cannot clobber recognition settings; recognition saves
      cannot clobber other devices or connection fields.
- [x] `go vet ./...`, `go test ./...`, and the production web build pass.
- [x] Deployed to 10.0.0.6 behind the backup + manual-acceptance gate.
