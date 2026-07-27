# Panel monitoring capability expansion implementation plan

## Execution Order

### 1. Establish device configuration and migration contracts

- Add typed device configuration, legacy singular-router normalization, validation, immutable IDs, and YAML save behavior.
- Add transactional SQLite schema versioning and rebuild all monitoring tables with `device_id` ownership.
- Change store methods to require device scope and add archived-device purge.
- Verify legacy config and database fixtures migrate to `default` without data loss.

### 2. Add multi-device runtime and API management

- Introduce `MonitorManager`, per-device status, heartbeat fan-out, and isolated startup/failure behavior.
- Adapt `Monitor` construction and all store calls to one immutable device ID.
- Add device CRUD/archive/restore/purge handlers and device-scoped monitoring lookup.
- Keep first-install and legacy connection settings compatible with the default device.
- Verify two fake RouterOS clients collect independently and same-MAC terminals remain separate.

### 3. Build the global device selector and settings workflow

- Add shared frontend device types, selected-device persistence, fallback behavior, and a centralized scoped-fetch helper.
- Upgrade the sidebar device card into the global selector.
- Rework connection settings into the device list/editor with enable, archive, and masked password controls.
- Add archived-device restore/permanent-purge controls to maintenance settings.
- Verify every monitoring request, terminal metadata update, and detail refresh uses the selected device.

### 4. Add dashboard traffic ranges

- Add device-scoped interface-history query and deterministic averaging/downsampling capped at 360 points.
- Add the `5min`, `1h`, `6h`, and `24h` endpoint allowlist and response contract.
- Add the home-chart segmented control and session persistence.
- Verify chronological output, empty/partial data, bucket math, refresh behavior, and responsive chart layout.

### 5. Add IPv4/IPv6 route attribution

- Extend RouterOS route/rule/conntrack types and collect richer IPv4/IPv6 route fields with compatibility fallback.
- Implement a pure route-matching component covering routing marks, ordered rules, lookup fallback, longest prefix, disabled/inactive routes, and ECMP.
- Project attribution into terminal connections and snapshot-derived current match counts into routes/rules.
- Add route and terminal-detail UI fields with explicit inferred/ambiguous/unavailable labels.
- Verify native mangle counters remain distinguishable from snapshot-derived connection counts.

### 6. Convergence and local verification

- Update backend executable contracts for multi-device scoping, schema migration, traffic history, and route attribution.
- Run formatting, Go unit/integration tests, race tests, frontend lint, and production build.
- Rebuild embedded frontend assets and the Go binary.
- Start a local server and use browser screenshots at desktop and mobile widths to check the selector, settings, chart ranges, route table, and terminal details for clipping or overlap.
- Confirm API allowlist, setup mode, restart flow, and existing monitoring behavior remain intact.

### 7. Deploy to `10.0.0.6`

- Inspect remote architecture, service unit, binary path, config path, data path, and current health through `ssh net` before changing anything.
- Back up the old binary, YAML config, and SQLite database with one timestamp.
- Build for the remote target, transfer the new binary, replace the old service binary, and restart the existing systemd unit.
- Verify service status, `/api/health`, device list, advancing dashboard timestamps, time-range history, route data, and browser rendering from the LAN.
- If migration or runtime checks fail, stop the service and restore the matching old binary/config/database set.

## Validation Commands

```bash
gofmt -w <changed-go-files>
go test ./...
go test -race ./internal/config ./internal/store ./internal/service ./internal/api
npm --prefix web run lint
npm --prefix web run build
go build ./cmd/rosboard
```

Deployment commands will be finalized only after read-only inspection of `10.0.0.6`; paths and architecture will not be guessed.

## High-risk Boundaries

- `internal/store/sqlite.go`: primary-key rebuild and legacy-row migration. Roll back the transaction on any error and test against a copied legacy database.
- `internal/service/monitor.go`: terminal identity merge and scheduler lock boundaries must remain device-local.
- `internal/api/server.go`: every data read and metadata mutation must resolve the same explicit device scope.
- `internal/config/config.go`: never expose or move credentials outside the explicit settings projection and YAML file.
- `web/src/App.tsx`: current centralized page state makes stale cross-device responses possible; cancel or version requests when selection changes.
- Remote deployment: old and new binaries use different schema expectations, so binary and database rollback must be paired.

## Completion Gate

- All parent acceptance criteria and each child PRD acceptance criterion are checked against tests or live evidence.
- Existing persistence, MAC identity merge, remarks, IPv4/IPv6 display, viewer-aware polling, setup, and systemd restart contracts remain passing.
- The deployed service on `10.0.0.6` is healthy and the previous release can be restored from the timestamped backup.
