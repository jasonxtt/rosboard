# Implementation

1. Add metadata/discovery/download validation and meaningful unit tests.
2. Add durable supervised install/backup/recovery and candidate activation handshake; update service and installation docs.
3. Wire authenticated update APIs, mutation exclusion and graceful stop.
4. Add both UI cards using shared parsing/state; validate interactions with component tests.
5. Run Go tests/vet/build/race and web tests/lint/build/asset checks. Review full diff.
6. Exercise real supervised Linux success/failure/recovery on disposable rosboard-test using synthetic data and local fixtures (no production credentials).
7. Update specs, create focused checkpoint commit and Draft PR, push after secret/diff review. Keep VERSION/release/main/production acceptance untouched until gate.

## PR #13 review follow-up

All three reported findings were confirmed and fixed on the same workstream:
- Keep `verifying_startup` through post-activation HTTP health observation (5 seconds continuously healthy, 20-second deadline, exact version/PID). Restore after child exit, timeout, or interruption. Gate typed background RouterOS writes until durable success.
- Publish pending restart synchronously; reject installation during the delayed restart window, including full reset. Compact maintenance actions share their shell restart state, including the empty-device shell.
- Use temporary-inode atomic replacement for smoke fixture executables. Regression deliberately replaces an executable while its previous inode remains running.

Validation: full Go tests/vet/build; race tests for update/API/RouterOS/main; 43 frontend tests, lint/build and dual UI asset checks. Linux unprivileged real-executable download verification and runtime smoke cover activation crashes, binary/data/session restoration, interrupted download/install, full reset, and atomic executable replacement. No production deployment or release acceptance.
