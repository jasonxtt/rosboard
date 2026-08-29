# Technical Design (final state)

## Config

- `DeviceConfig` owns `ProtocolAnalysis bool`, `FeatureLibraryConfig`, and
  `MosDNSConfig` (yaml `protocol_analysis` / `feature_library` / `mosdns`).
  `Config` keeps a runtime-only `ProtocolAnalysis ProtocolAnalysisConfig`
  carrier (`yaml:"-"`) that the manager fills per device so `NewMonitor`
  needs no signature change. Legacy global sections and the
  `ROSBOARD_MOSDNS_*` / `ROSBOARD_FEATURE_LIBRARY_*` env overrides are gone.
- Per-device normalization in `normalizeDevices`: MosDNS base URL
  normalization + default 30-minute interval; feature library defaults
  (source URL, 168h, 30min window). Validation per device.

## Storage

- `dns_observations`, `dns_features`, and `mosdns_state` exist in **every**
  device database; owner-only guards removed. The `default` device keeps
  using the owner database.
- Versioned one-time wipe (`dns_scope_migrated = "2"`): drops owner rows of
  `dns_observations` / `dns_features` **and the legacy watermark keys**, so a
  device-scoped source never resumes the removed global boundary. The
  version bump also repairs owner databases that already ran marker
  version 1 without the watermark deletion.
- `purgeDNSData` is wired into `PurgeDevice` and `ResetAll`. `ResetAll` also
  wipes closed device database files found under `data/devices/*.db`.
- `ForDevice` returns `nil` when `OpenDevice` fails instead of silently
  aliasing the owner database; the observations API rejects nil stores.

## Service

- Per device: `MosDNSSynchronizer` and `FeatureLibrarySynchronizer` bound to
  the device store; the feature library cache file is per device
  (`data/feature-library-<sanitized id>.yml`, plus a sha256 suffix when the
  id needed sanitizing, mirroring device database naming); the
  `ApplicationResolver` matches only that device's observations and features
  with that device's match window.
- Synchronizer init failures are recorded (`mosdnsInitErr` /
  `featureInitErr`) and surface in every status projection's `LastError`
  instead of showing a healthy "已启用" state.
- `RecognitionStatus(deviceID)` / `MosDNSStatus(deviceID)` are device-scoped;
  `DeviceStatus` carries live `mosdns` and `featureLibrary` entries.

## API

- `POST /api/settings/recognition` persists the recognition set
  (`protocolAnalysis`, `mosdns`, `featureLibrary`) for each listed device;
  unlisted devices are untouched; a closed master switch forces both child
  toggles off; validation is per device.
- Device create/update preserve recognition fields they do not carry.
- `GET /api/recognition?deviceId=` (config projection when the manager is
  absent), `GET /api/mosdns?deviceId=`, `GET /api/mosdns/observations?deviceId=`.
- `GET /api/protocols` gates on the requested device's switch; the fallback
  device resolution mirrors `MonitorForDevices` (first enabled, non-archived,
  RouterOS-configured device).
- `/api/settings` root drops `protocolAnalysis` / `featureLibrary`; the
  device projection carries per-device recognition values.

## Frontend

- 主机设置 > 识别设置 (second-level submenu below 策略路由) renders the
  selected device's three fieldsets and live statuses; saving posts only the
  selected device. Protocols view gating follows the selected device and
  waits for settings before redirecting a protocols landing view.
