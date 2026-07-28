# Implementation plan

1. Add terminal-viewer activation, scheduler state, timestamped terminal-rate
   projections, and merge protection in the Monitor.
   Verify: focused service tests cover activity, projection, and stale-commit
   protection.

2. Expose the POST-only terminal-viewer heartbeat endpoint and update API
   tests.
   Verify: method matrix and response deadline pass.

3. Update terminal UI heartbeats, polling intervals, types, and freshness
   indication.
   Verify: frontend lint/build pass; local browser has no errors and its
   network requests match the intended cadence.

4. Run backend/frontend automated checks and local runtime verification.
   Verify: `go test ./...`, `go vet ./...`, frontend lint/build, and a local
   RouterOS-backed or controlled-runtime check pass.

5. Build and deploy to `10.0.0.6` after timestamped backups of binary,
   configuration, and SQLite data. Verify systemd, health, affected APIs, and
   embedded assets. Request the user's manual acceptance before committing.
