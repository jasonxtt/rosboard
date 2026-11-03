# Implementation and validation plan

1. Obtain explicit approval for these artifacts before `task.py start` or runnable-code edits.
2. Refresh origin/main and create `codex/fix-issue-38-sqlite-contention` from the accepted remote baseline, preserving unrelated local artifacts. Set task branch and base branch metadata. Keep all slices on one Draft PR.
3. Read backend database, monitoring, quality, error/logging guidelines and monitor-manager lifecycle. Implement bounded history retrieval and semantic regression tests.
4. Add migration-safe indexes and batched, cancellable retention operations. Test boundary timestamps, more than one batch, default/non-default stores, isolation, reopen, and cancellation.
5. Integrate maintenance lifecycle, remove inline pruning, and test startup failures/retries, shutdown, viewer independence, and sampling during maintenance. Refine design if lifecycle inspection reveals a different owner is necessary.
6. Record synthetic large-history measurements against the existing query and inspect query plans. Avoid flaky hard performance thresholds in normal unit tests.
7. Run gofmt, `TMPDIR=/private/tmp go test ./...`, `go build ./...`, `go vet ./...`, targeted `go test -race ./internal/store ./internal/service`, and `git diff --check`; use writable caches if sandbox requires them. Record unrelated failures explicitly.
8. Deploy isolated build/data on `ssh rosboard-test` (10.0.0.60), validate service/health and historical/realtime behavior, and verify maintenance with synthetic backlog. No production RouterOS credentials or SQLite files.
9. Update relevant specs and research/validation notes. Inspect exact staged paths/diff/status for secrets and unrelated files; make focused checkpoint commit(s), push, and create/update one Draft PR targeting main. Record PR URL.
10. Finalize the evidence-based issue reply with PR and validation results. The user requested advice on replying; keep it as a draft unless publishing is explicitly authorized.
11. Production deployment is outside the immediate checkpoint: if later requested, follow NAS backup, remote verification, manual user acceptance, and root-review APPROVED gates. Keep task unarchived and PR Draft while unfinished.
