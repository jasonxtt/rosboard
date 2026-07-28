# Implementation plan

1. **Evidence probe and typed client**
   - Use ignored local configuration only for a GET-only `/rest/interface/pppoe-client` probe; record HTTP status, row count, and field names without credentials.
   - Add PPPoE/DHCP fields and optional topology collection/verification snapshot handling with no RouterOS writes.

2. **Pure TrafficScope**
   - Add `internal/service/traffic_scope.go` and table-driven tests.
   - Reuse `terminalScope`, `InterfaceEvidence`, list resolution, DHCP Client, routes, and interface types; implement legacy, PPPoE-parent exclusion, automatic categories, overrides, warning and stable ordering rules.

3. **Config, models, API, verification**
   - Add traffic scope config/model projections and clone/normalization paths.
   - Change verification ticket/result to carry safe topology preview inputs and both scope projections.
   - Relax auto-mode manual traffic requirement while preserving legacy behavior and validating overrides/permissions.

4. **Monitor and history**
   - Publish immutable traffic scope during full refresh; replace `selectTrafficInterfaces` and config/overview fallbacks in realtime/history.
   - Isolate per-interface realtime failures; retain all-interface sample writes and selected-range aggregate reads.
   - Delete obsolete helper only after a reference search confirms no users.

5. **Terminal decoupling**
   - Pass TerminalScope/predicate into terminal count, state, and summary aggregation.
   - Update existing tests and add regressions proving traffic changes cannot alter terminal results.

6. **Frontend**
   - Add TypeScript contracts/normalization and per-device dashboard scope loading.
   - Replace manual traffic/CIDR selection UI with collapsed auto/advanced region, preview scopes, legacy restore-auto, validation messages, and responsive/theme-safe styles.
   - Update onboarding and settings copy.

7. **Tests and local verification**
   - Add pure derivation, config/API/verification, multi-WAN aggregation/history, realtime partial-failure, terminal-decoupling, multi-device, and frontend coverage.
   - Run `gofmt`, `go test ./...`, `go test -race ./internal/service ./internal/api`, `go vet ./...`, `npm --prefix web run lint`, `npm --prefix web run build`, and `go build -o ./rosboard ./cmd/rosboard`.
   - Start with `go run ./cmd/rosboard -config configs/config.local.yaml`; inspect actual scope/reasons and unchanged terminal scope via read-only RouterOS calls/browser.

8. **Remote acceptance gate**
   - Build a timestamped backup bundle on `network-vm`; deploy the verified binary/assets, validate systemd/health/device dashboard/API/browser, then stop for user manual review at `http://10.0.0.6:8080`.
   - Do not commit, archive, write the session journal, or push until explicit user approval.

## Review gates

- Before monitor changes: confirm no `TrafficScope` branch can classify a TerminalScope LAN automatically.
- Before deletion: `rg` must show no old selection/CIDR helper callers.
- Before deployment: full local checks and embedded-asset binary rebuild are green.
- After deployment: report recognized/excluded interfaces and reasons, aggregation behavior, terminal-count preservation, every check result, backup path, and the specific pages needing user inspection.
