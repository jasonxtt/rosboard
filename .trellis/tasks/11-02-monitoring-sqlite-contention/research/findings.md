# Investigation evidence (2026-11-02)

Source: https://github.com/jasonxtt/rosboard/issues/38 . GitHub main and v0.3.1 both resolved to 551ea2356c7590e3b790ccad27a6f0d55ea52617 during investigation. Local branch chore/release-v0.3.1 HEAD 33c9ef95f3a5159a37fd3f4ef68d7db3877970ea had identical internal/service/monitor.go and internal/store/sqlite.go.

- refreshRealtime store context is 5 seconds; RouterOS poll context is 15 seconds. Full refresh store context is 15 seconds.
- PruneInterfaceSamples and PruneRuntimeState run inside full refresh, after SaveInterfaceSamples, not inside refreshRealtime.
- Each device DB has SetMaxOpenConns(1). Connection-pool starvation alone can produce the reported BeginTx error; SQLITE_BUSY is not proven by this error.
- TerminalHistories selects all matching terminal history rows, scans them all, and discards entries after the per-terminal limit in Go. buildTerminals requests 30 entries per terminal on collection.
- terminal_history PK is (device_id, terminal_id, ts). Cleanup EXPLAIN: SEARCH terminal_history USING INDEX sqlite_autoindex_terminal_history_1 (device_id=?). No timestamp cleanup index exists. connection_state has an analogous missing time index.
- Full-refresh SaveInterfaceSamples failure skips that invocation's cleanup. Realtime SaveInterfaceSamples failure does not directly skip a separate worker's cleanup. Sustained positive feedback and WAL/CPU causal claims remain unproven.

## Temporary isolated Go probes
Executed against actual store code via Go overlay; files lived only in /private/tmp and databases in test temp directories. No production/test-machine data used.
- Occupy single connection and call SaveInterfaceSamples with 5s context: 5.001247292s, exact error `begin interface sample batch: context deadline exceeded`; release connection and sampling succeeds.
- Generate 2,500,764 rows (49 terminals x 51,036 minutes): seed 4.440291792s; existing TerminalHistories(...,30) 1.681673417s; 49 existing indexed TerminalHistory(...,30) calls 2.248208ms; PruneRuntimeState with no expired rows 275.68425ms.
- Command used GOCACHE=/private/tmp/rosboard-issue38-gocache go test -overlay=/private/tmp/rosboard_issue38_overlay.json ./internal/store with selected temporary tests.

Measurements establish avoidable work and timeout mechanism, not the reporter's exact multi-hour outage cause or deployment-machine performance. Repeat controlled comparison during implementation and validate concurrent sampling on the test machine.

## Lifecycle inspection
MonitorManager.Start calls startMonitor and retries failures through retryMonitor with the same context and Monitor instance. Monitor.Start currently returns immediately on initial refresh failure before launching its three polling goroutines. Maintenance must start before that failure path and be guarded against duplicate launch on retry.
