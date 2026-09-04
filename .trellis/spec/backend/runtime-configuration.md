# Runtime Configuration

## Scenario: Multi-device RouterOS configuration

### Contracts

- `devices[]` owns immutable `id`, operator `name`, `enabled`, `archived`, and per-device RouterOS REST credentials, traffic interfaces, and terminal CIDRs.
- Poll intervals, retention, listener, API allowlist, and data directory remain process-global.
- Legacy singular `routeros` YAML loads as one enabled device with ID `default`; the next settings save emits `devices` and omits the legacy block.
- `ROSBOARD_ROUTEROS_*` overrides target the first configured device for backward compatibility.
- Normal deletion sets `archived=true` and `enabled=false`; only `DELETE /api/devices/{id}/data` with exact device-name confirmation removes history and the YAML record.
- RouterOS passwords stay in the private `0600` YAML for runtime use. Settings/device projections expose only `passwordSet`; an empty password on an existing-device write preserves the stored value.

### Tests Required

- Config: legacy YAML normalizes to `default`; device YAML round-trips without a legacy block; duplicate IDs fail validation.
- API: create/update/archive/restore validate connection fields; archive retains data; confirmed purge removes only the owning device.
- Race: concurrent settings saves do not share mutable device slices or overwrite another save.

## Scenario: Local non-interactive YAML startup

### 1. Scope / Trigger

- Trigger: local development or review service startup that must not depend on chat history, secret extraction, or `ROSBOARD_*` injection.
- The real local RouterOS credential is machine-local state and must never enter Git.

### 2. Signatures

- Binary: `./rosboard -config ./configs/config.local.yaml`.
- Stable launcher: `./scripts/run-local.sh`.
- Public template: `configs/config.example.yaml`.
- Private config: `configs/config.local.yaml` with mode `0600` and a `.gitignore` rule.

### 3. Contracts

- A tracked example may contain no RouterOS device. A configured device requires `devices[].routeros.base_url`, `username`, `password`, at least one `traffic_interface`, and at least one `terminal_cidr`.
- Local defaults are explicit in YAML: `listen_address: 0.0.0.0:8080`, `data_dir: ./data`, positive poll/retention values, selected traffic interfaces, and allowed CIDRs.
- Allowed CIDRs include IPv4/IPv6 loopback plus the intended LAN ranges so both local and LAN review URLs work.
- `scripts/run-local.sh` resolves the repository root, verifies that the binary and config are available, changes to the root so relative `data_dir` is stable, and then uses `exec ... -config ...`.
- The launcher contains no credentials and sets no `ROSBOARD_*` values. Environment overrides remain supported by `config.Load`, but are not required by this workflow.

### 4. Validation & Error Matrix

- Missing/non-executable binary -> launcher exits with a build command hint.
- Missing/unreadable local YAML -> launcher exits before starting the service.
- Missing required RouterOS field -> `config.Load` fails startup.
- Invalid credential -> initial RouterOS refresh fails; validate with `/rest/system/resource` without printing the secret.
- Loopback omitted from `allowed_cidrs` -> `127.0.0.1` API requests return HTTP 403 even though LAN access works.
- Relative `data_dir` with a launcher that does not change directory -> data can be created under the caller's working directory.

### 5. Good/Base/Bad Cases

- Good: `scripts/run-local.sh` starts a process whose command line contains `-config /absolute/path/configs/config.local.yaml`, parent PID becomes 1 after detaching, and local/LAN URLs return 200.
- Base: operators can still invoke the binary manually with the same YAML path.
- Bad: extract a password from a Codex rollout at every start.
- Bad: store the real credential in the tracked example file or a shell script.
- Bad: rely on ambient working directory while YAML uses `data_dir: ./data`.

### 6. Tests Required

- Shell: `zsh -n scripts/run-local.sh`.
- Secret boundary: launcher contains no `password`, `ROLLOUT`, or `ROSBOARD_*`; local YAML is ignored and untracked with mode `0600`.
- Authentication: local YAML credential returns HTTP 200 from RouterOS system-resource REST endpoint without logging the credential.
- Delivery: running process command line contains the YAML `-config` argument; `127.0.0.1:8080` and the Mac LAN URL return HTTP 200.
- Runtime: Dashboard `updatedAt` advances across polling intervals.

### 7. Wrong vs Correct

#### Wrong

```zsh
PASSWORD=$(extract_from_chat_history)
ROSBOARD_ROUTEROS_PASSWORD="$PASSWORD" ./rosboard
```

#### Correct

```zsh
cd "$root_dir"
exec "$root_dir/rosboard" -config "$root_dir/configs/config.local.yaml"
```

## Scenario: Panel-managed runtime settings

### 1. Scope / Trigger

- Trigger: changes to first-install setup, RouterOS REST connection editing, collection editing, `/api/settings*`, browser-local panel preferences, maintenance restart, or RouterOS credential visibility in the UI.
- The panel can save RouterOS REST connection and collection fields into the configured YAML file and restart the process so systemd reloads the monitor with the new settings.

### 2. Signatures

- API: `GET /api/settings`.
- API: `POST /api/settings/connection` is retired with HTTP 410; verified `/api/devices` writes own RouterOS connection fields.
- API: `POST /api/settings/collection` with positive numeric `pollIntervalSeconds`, `realtimePollIntervalSeconds`, `terminalPollIntervalSeconds`, and `sampleRetentionHours`.
- API: `/api/devices` and `/api/devices/{id}` own per-device RouterOS REST fields, `trafficInterfaces`, and `terminalCidrs`.
- API: `POST /api/settings/restart` with no request body.
- Response root fields: `connection`, `collection`, and `diagnostics`.
- Recognition settings are **fully per device** (`devices[].protocol_analysis`, `devices[].feature_library`, `devices[].mosdns`); there is no global recognition YAML section (legacy global `protocol_analysis:` / `feature_library:` / `mosdns:` sections are ignored and removed on the next save). The recognition page saves the selected device's whole recognition set through `POST /api/settings/recognition` (`devices[]` entries keyed by device id). A device's `protocol_analysis: false` forces its `mosdns.enabled` and `feature_library.enabled` off on save. Device create/update requests that omit `mosdns` must preserve the stored value.
- Each enabled device owns its own feature-library synchronizer with an isolated cache file (`data/feature-library-<device>.yml`; the `default` device keeps the legacy `feature-library.yml`) and its own application resolver; observations, learned features, watermarks, and sync state live in that device's database. Per-device recognition live status is exposed through the device status projection (`status.mosdns`, `status.featureLibrary`), `GET /api/recognition?deviceId=`, `GET /api/mosdns?deviceId=`, and `GET /api/mosdns/observations?deviceId=`. `GET /api/protocols` gates on the requested device's `protocol_analysis` flag.
- There is no global `protocol_analysis` section and no migration: recognition defaults to **off** on every device until the operator enables it per device. Legacy global sections are ignored and removed on the next save.
- Connection fields: `apiBasePath`, `configured`, `listenAddress`, `allowedCidrs`, `routerosBaseUrl`, `routerosScheme`, `routerosHost`, `routerosPort`, `routerosUsername`, and `routerosPasswordSet`; no password plaintext is returned.
- Collection fields: `pollIntervalSeconds`, `realtimePollIntervalSeconds`, `terminalPollIntervalSeconds`, and `sampleRetentionHours`.
- MosDNS fields (per device): `mosdns.enabled`, `mosdns.baseUrl`, `mosdns.syncIntervalMinutes` plus the live sync counters (`lastImported`, `learnedFeatureCount`, ...). Synchronization is disabled by default, leaves `baseUrl` empty until the operator enters an address such as `10.0.0.3`, and then uses the normalized local HTTP endpoint at a 30-minute interval. Learned IP features live in the owning device database and are never removed by DNS TTL expiry or raw observation retention.
- Diagnostics fields: `routerName`, `version`, `updatedAt`.
- Browser preference storage key: `rosboard:panel-preferences`.

### 3. Contracts

- Missing config files at the `-config` path start with defaults and retain the path so setup can write the first YAML file.
- Device create/update requests preserve stored recognition settings (`protocolAnalysis`, `featureLibrary`, `mosdns`) unless the request carries them; device editors do not manage recognition fields.
- RouterOS REST defaults to `http://10.0.0.1:80`. HTTPS uses default REST port `443`. The panel does not use classic RouterOS API ports `8728` / `8729`.
- `/api/settings` is a projection of the effective `config.Config` plus current snapshot diagnostics.
- The device projection reports each device's stored `protocolAnalysis` and `featureLibrary`/`mosdns` child values even when the device's master switch is closed; the UI disables their fieldsets while that device's master switch is closed.
- `connection.apiBasePath` is `/api` while the frontend uses same-origin requests.
- Do not JSON-encode the full config or return `routerosPassword`. Existing-device editors start with an empty password input and use `passwordSet` to explain that empty means preserve.
- Slice fields such as `allowedCidrs`, `trafficInterfaces`, and `terminalCidrs` serialize as arrays. Empty values serialize as `[]`, not `null`.
- Verified device APIs atomically replace `Config.Path`. Normal ready-phase device writes schedule process exit; onboarding save-only writes intentionally defer restart until the operator completes setup.
- `POST /api/settings/collection` writes only process-global collection intervals and retention to `Config.Path`, then schedules the same restart. It must not mutate any `devices[].routeros.traffic_interfaces` or `devices[].routeros.terminal_cidrs` values; those fields are saved only through device APIs. Serialize writes under `cfgMu` so simultaneous settings saves cannot overwrite one another.
- `POST /api/settings/recognition` persists the per-device recognition set (`protocolAnalysis`, `mosdns`, `featureLibrary`) for every device listed in the request; devices not listed keep their stored values. When a device's master switch is closed, its child `enabled` values must persist as `false`.
- Closing protocol analysis must not stop RouterOS firewall connection collection, terminal rate calculation, connection counts, raw connection protocol fields, or TCP/UDP/other overview counts. It skips application classification, protocol aggregation, and new `protocol_samples` writes; existing protocol samples remain available for natural retention expiry.
- `POST /api/settings/restart` schedules the injected restart callback without changing YAML. It is available only when the runtime provides that callback.
- Start the HTTP server without waiting for the first RouterOS full refresh. The monitor manager initializes in the background so restart downtime is limited to the process supervisor delay.
- Dashboard snapshots serialize empty collections and terminal scope summaries as `[]` and `{}`, never `null`, including the interval before the first successful refresh.
- If RouterOS is unconfigured or the initial monitor start fails, the HTTP server still serves the setup UI and `/api/settings`; dashboard endpoints return setup-required service-unavailable JSON.
- Browser-local preferences may affect default refresh interval, default landing view, and default terminal family. They do not rewrite YAML and do not change monitor scheduling on the server.
- After a settings save, the browser must observe the service become unavailable and then verify health plus current JS/CSS assets before reloading. Do not reload after a fixed delay. Normalize legacy `null` dashboard collections at the frontend response boundary so a startup snapshot renders an empty state instead of crashing.

### 4. Validation & Error Matrix

- Missing config file -> start setup defaults instead of fatal startup.
- Missing config path on save -> HTTP 400 because there is nowhere to persist settings.
- `scheme` other than `http` / `https` -> HTTP 400.
- Empty host, username, or password -> HTTP 400.
- Port outside `1..65535` -> HTTP 400.
- Any collection interval or retention value at or below zero -> HTTP 400.
- Restart callback unavailable -> HTTP 503.
- Missing devices -> `configured=false`; the authenticated ready panel renders its empty-device state.
- Empty per-device CIDR/interface lists -> reject device persistence.
- Request from a disallowed CIDR -> existing API allowlist returns HTTP 403 before the settings handler.
- Invalid browser-local preference JSON -> frontend falls back to product defaults.

### 5. Good/Base/Bad Cases

- Good: first install starts with a missing config file, creates the administrator, optionally verifies and saves multiple devices without intermediate restarts, then completes once into active monitoring.
- Good: editing an existing connection saves `https://10.0.0.6:443`, username, and password, then restarts once.
- Good: collection interval edits preserve every device's existing `traffic_interfaces` and `terminal_cidrs`.
- Good: device input `[' ether1 ', 'ether1', '']` persists as `['ether1']` on that device and restarts the collectors.
- Base: no devices are configured; the ready panel renders empty monitoring states while settings and account actions remain available.
- Bad: tell users to use port `8728` for this panel; that is classic RouterOS API, not REST.
- Bad: save settings but keep the old RouterOS client running indefinitely.
- Bad: return or export a settings payload containing a RouterOS password.

### 6. Tests Required

- API: `GET /api/settings` returns effective config values and HTTP 200 for an allowed loopback request.
- API: `POST /api/settings/connection` returns HTTP 410; device writes require verification when creating or changing connection fields.
- API: `POST /api/settings/collection` persists positive global values, preserves device interface/CIDR values, and rejects zero values.
- API: device create/update persists trimmed/de-duplicated non-empty `trafficInterfaces` and canonical non-empty `terminalCidrs` per device and never projects passwords.
- API: `POST /api/settings/restart` invokes the injected callback after returning HTTP 200.
- Concurrency: `go test -race ./internal/api` passes for the settings server.
- Config: missing config path loads setup defaults and keeps `Config.Path` for the first save.
- Config: a missing configuration file leaves every device's recognition off; legacy global recognition sections are ignored and removed on the next save; per-device values round-trip.
- API: disabled protocol endpoints return empty arrays without querying protocol history; settings preserve stored child toggles while reporting the master switch.
- Service: disabling protocol analysis preserves connection counts, raw protocols, and terminal rates while omitting application classification, flow categories, protocol aggregation, and protocol sample writes.
- JSON shape: empty string slices serialize as arrays, not `null`.
- Frontend: production TypeScript build and oxlint pass.
- Live: local service serves `/`, `/api/dashboard` when configured, and `/api/settings`; saving a connection restarts the process under systemd.
- Test deployments: risky, experimental, or destructive verification runs on the disposable test machine `10.0.0.60` (`ssh rosboard-test`, LXC Debian 12, free to break) with its own config/data directory — no backup or acceptance gate. The backup + manual-acceptance gate applies only to the production instance `10.0.0.6` (see AGENTS.md).
- Restart regression: save multiple traffic interfaces, observe the waiting state through restart, and confirm the dashboard automatically returns to healthy without a blank page or a `null.filter` runtime error.

### 7. Wrong vs Correct

#### Wrong

```go
next := s.cfg
next.PollIntervalSeconds = payload.PollIntervalSeconds
config.Save(next.Path, next) // concurrent saves can persist stale fields
```

#### Correct

```go
s.cfgMu.Lock()
defer s.cfgMu.Unlock()
next := s.cfg
update(&next)
if err := config.Save(next.Path, next); err == nil {
    s.cfg = next
}
```
