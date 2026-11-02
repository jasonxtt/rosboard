# Validation checkpoint — 2026-11-02

## Automated checks

All passed on macOS:
- `TMPDIR=/private/tmp go test ./...` (all packages)
- `go build ./...`
- `go vet ./...`
- `go test -race ./internal/store ./internal/service`
- gofmt on changed Go files and `git diff --check`

Used `GOCACHE=/private/tmp/rosboard-issue38-gocache`; tests requiring httptest ran with sandbox permission for loopback listeners. An initial restricted invocation could not bind httptest ports; rerunning with loopback access passed. These were environment restrictions, not product failures.

Coverage includes bounded latest-history reads (an invalid old row catches accidental over-reading), duplicate/missing terminal IDs, values/order, cancellation, additive index recreation/reopen, cleanup query plans, per-batch row limits, strict expiry boundaries, device isolation within one file, cancellation while waiting for the connection, sampling during deletion, no-viewer maintenance, initial collection failure/retry, shutdown, and separate maintenance-error logging.

## Large synthetic history

49 terminals x 51,036 minute entries = 2,500,764 rows. Original query measured 1.681673417s in investigation; the implemented `BenchmarkTerminalHistoriesLargeHistory` measured 3,104,472 ns/op (~3.10ms, three iterations) on Apple M4. Measurements are synthetic and were taken in separate runs, so do not interpret the ratio as a deployment SLA. The semantic regression test imposes no timing threshold. Replacing TerminalHistories with the origin/main implementation via a temporary overlay makes TestTerminalHistoriesReadsOnlyLatestRows fail by scanning the deliberately invalid old row; the fixed implementation passes. The overlay omitted duplicate legacy prune declarations solely to compile the old query alongside the new retention file.

## Disposable Linux validation (10.0.0.60)

- Cross-compiled Linux amd64 application and store/service test binaries after local checks passed.
- All five focused store/history/retention tests and three maintenance service tests passed on Debian 12.
- Created separate `/opt/rosboard-issue38` config/data and transient `rosboard-issue38` / `rosboard-issue38-mock` units, bound only to loopback ports 18038/18039.
- Fixture simulated 12 interfaces and 49 terminals in documentation-only 192.0.2.0/24, with synthetic credentials. No production RouterOS or database was accessed.
- Seeded 2,500,764 history rows; removed the new indexes before startup to exercise additive upgrade. Startup rebuilt them on the existing populated file. About 31k expired rows were cleaned.
- Systemd candidate and mock were active; `/api/health` returned ok. Authenticated API probe completed setup/login with same-origin admission enforced.
- Probe result: 50 terminal records (49 clients plus router), 30 history records for the selected client, 22 distinct realtime timestamps over 22 one-second reads, maximum realtime API latency 4.07ms, 19 distinct interface sample timestamps in the last 20 seconds, no history older than retention plus maintenance cadence, and embedded entry JavaScript HTTP 200. Final history row count 2,469,652 while new minute samples continued.
- Candidate PID logs contained the serving message and no collection or retention failures during the checked run. An earlier fixture initialization had one refused mock connection before the mock became ready; it was stopped before the populated-data run.
- Probe corrections were confined to the temporary fixture: systemd transient units must be recreated after stop, startup must await index creation, POSTs require Origin, asset paths are relative, and expiry checks allow the five-minute cadence. No application relaxation was needed.
- Stopped both temporary units after verification. Existing rosboard-test, rosboard-container-test, and rosboard-dns-test services remained active. Isolated synthetic files remain available on the disposable machine.

## Limits and delivery state

This reproduces the connection-wait failure mechanism and fixes two verified sources of unbounded work. It does not establish the sole cause of the reporter's multi-hour episodes or guarantee latency on their storage. Additive index creation can extend first startup; row batching and contexts cannot promise a hard disk-latency bound.

Production has not been deployed. Keep the PR Draft and task in progress pending root review / required acceptance; do not merge, release, or archive. The GitHub issue response remains a draft until publishing is explicitly authorized.
