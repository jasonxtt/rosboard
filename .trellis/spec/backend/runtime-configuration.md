# Runtime Configuration

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

- Required YAML fields: `routeros.base_url`, `routeros.username`, and `routeros.password`.
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
- API: `POST /api/settings/connection` with `{ "scheme": "http" | "https", "host": string, "port": number, "username": string, "password": string }`.
- API: `POST /api/settings/collection` with positive numeric `pollIntervalSeconds`, `realtimePollIntervalSeconds`, `terminalPollIntervalSeconds`, and `sampleRetentionHours`, plus string-array `trafficInterfaces` and `terminalCidrs`.
- API: `POST /api/settings/restart` with no request body.
- Response root fields: `connection`, `collection`, and `diagnostics`.
- Connection fields: `apiBasePath`, `configured`, `listenAddress`, `allowedCidrs`, `routerosBaseUrl`, `routerosScheme`, `routerosHost`, `routerosPort`, `routerosUsername`, `routerosPassword`, `routerosPasswordSet`.
- Collection fields: `pollIntervalSeconds`, `realtimePollIntervalSeconds`, `terminalPollIntervalSeconds`, `sampleRetentionHours`, `trafficInterfaces`, `terminalCidrs`.
- Diagnostics fields: `routerName`, `version`, `updatedAt`.
- Browser preference storage key: `rosboard:panel-preferences`.

### 3. Contracts

- Missing config files at the `-config` path start with defaults and retain the path so setup can write the first YAML file.
- RouterOS REST defaults to `http://10.0.0.1:80`. HTTPS uses default REST port `443`. The panel does not use classic RouterOS API ports `8728` / `8729`.
- `/api/settings` is a projection of the effective `config.Config` plus current snapshot diagnostics.
- `connection.apiBasePath` is `/api` while the frontend uses same-origin requests.
- `routerosPassword` mirrors the YAML `routeros.password` value for operator editing in this LAN-only panel. Do not JSON-encode the full config struct; expose only the explicit settings response fields.
- The password input masks `routerosPassword` by default. The eye control only changes the input type in the browser; exported settings replace the value with `********` when a password is set.
- Slice fields such as `allowedCidrs`, `trafficInterfaces`, and `terminalCidrs` serialize as arrays. Empty values serialize as `[]`, not `null`.
- `POST /api/settings/connection` writes `routeros.base_url`, `routeros.username`, and `routeros.password` to `Config.Path`, then schedules process exit. Under `Restart=always`, systemd starts a new process with the saved config.
- `POST /api/settings/collection` trims list entries, drops blanks and duplicates, writes all collection fields to `Config.Path`, and schedules the same restart. Serialize writes under `cfgMu` so simultaneous connection and collection saves cannot overwrite one another.
- `POST /api/settings/restart` schedules the injected restart callback without changing YAML. It is available only when the runtime provides that callback.
- If RouterOS is unconfigured or the initial monitor start fails, the HTTP server still serves the setup UI and `/api/settings`; dashboard endpoints return setup-required service-unavailable JSON.
- Browser-local preferences may affect default refresh interval, default landing view, and default terminal family. They do not rewrite YAML and do not change monitor scheduling on the server.

### 4. Validation & Error Matrix

- Missing config file -> start setup defaults instead of fatal startup.
- Missing config path on save -> HTTP 400 because there is nowhere to persist settings.
- `scheme` other than `http` / `https` -> HTTP 400.
- Empty host, username, or password -> HTTP 400.
- Port outside `1..65535` -> HTTP 400.
- Any collection interval or retention value at or below zero -> HTTP 400.
- Restart callback unavailable -> HTTP 503.
- Missing or placeholder RouterOS values -> `configured=false`; setup page renders.
- Empty configured CIDR/interface lists -> response array is empty.
- Request from a disallowed CIDR -> existing API allowlist returns HTTP 403 before the settings handler.
- Invalid browser-local preference JSON -> frontend falls back to product defaults.

### 5. Good/Base/Bad Cases

- Good: first install starts with a missing config file, displays RouterOS IP/port/user/password setup, saves YAML, exits, and systemd restarts into active monitoring.
- Good: editing an existing connection saves `https://10.0.0.6:443`, username, and password, then restarts once.
- Good: collection input `[' ether1 ', 'ether1', '']` persists as `['ether1']` and restarts the collectors.
- Base: `terminal_cidrs: []` in YAML or omitted terminal CIDRs render as an empty array in JSON and `-` in the UI.
- Bad: tell users to use port `8728` for this panel; that is classic RouterOS API, not REST.
- Bad: save settings but keep the old RouterOS client running indefinitely.
- Bad: export the settings response directly and leak `routerosPassword` into a downloaded diagnostic file.

### 6. Tests Required

- API: `GET /api/settings` returns effective config values and HTTP 200 for an allowed loopback request.
- API: `POST /api/settings/connection` validates scheme/host/port/user/password and writes the expected YAML fields.
- API: `POST /api/settings/collection` persists positive values, trims/de-duplicates lists, and rejects zero values.
- API: `POST /api/settings/restart` invokes the injected callback after returning HTTP 200.
- Concurrency: `go test -race ./internal/api` passes for the settings server.
- Config: missing config path loads setup defaults and keeps `Config.Path` for the first save.
- JSON shape: empty string slices serialize as arrays, not `null`.
- Frontend: production TypeScript build and oxlint pass.
- Live: local service serves `/`, `/api/dashboard` when configured, and `/api/settings`; saving a connection restarts the process under systemd.

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
