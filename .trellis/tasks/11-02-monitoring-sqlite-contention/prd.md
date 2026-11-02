# Issue #38: Reliable monitoring with large SQLite histories

## Problem
Realtime samples can fail with `begin interface sample batch: context deadline exceeded` when another operation occupies the device database's single connection. Terminal-history retrieval reads all selected terminals' history despite returning only 30 entries per terminal. Retention cleanup also scans history on every full refresh.

## Requirements
- Keep realtime sampling responsive as retained terminal history grows.
- Preserve history values, descending timestamps, per-terminal limits, empty-input behavior, and device isolation.
- Continue expiring interface samples using configured retention, connection state after two hours, and terminal/load/protocol history after 35 days.
- Maintenance must run independently of viewers and successful collection, respect shutdown, release the connection between bounded batches, and report errors without publishing misleading collection failures.
- Existing databases must upgrade without deleting retained data or changing ownership boundaries.
- Prepare an evidence-based response to GitHub Issue #38, distinguishing reproduced behavior from the reporter's unverified causal hypotheses.

## Acceptance criteria
- SQL limits history reads for each requested terminal rather than discarding unbounded results in Go.
- Cleanup uses indexes for device/time predicates and bounded transactions; neither realtime nor full refresh performs retention deletion.
- Tests cover history equivalence, migration/reopen, expiry boundaries, device isolation, bounded cleanup, cancellation, and maintenance behavior independent of collection success.
- Large synthetic-history measurements compare old and new query behavior; concurrent sampling is exercised during maintenance.
- Build, full tests, vet, relevant race tests, and diff checks pass. Test-machine validation is recorded before production delivery is proposed.
- One focused work branch and Draft PR target main; no production acceptance, merge, release, or task archive without required approval.

## Scope
Store/query/index changes, monitor retention scheduling, regression tests, related specifications, and issue response draft. Preserve existing retention policy and public APIs. No new configurable retention settings, writer queues, connection-count changes, RouterOS writes, or production data access are required.
