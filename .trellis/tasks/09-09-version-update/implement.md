# Implementation

1. Add metadata/discovery/download validation and meaningful unit tests.
2. Add durable supervised install/backup/recovery and candidate activation handshake; update service and installation docs.
3. Wire authenticated update APIs, mutation exclusion and graceful stop.
4. Add both UI cards using shared parsing/state; validate interactions with component tests.
5. Run Go tests/vet/build/race and web tests/lint/build/asset checks. Review full diff.
6. Exercise real supervised Linux success/failure/recovery on disposable rosboard-test using synthetic data and local fixtures (no production credentials).
7. Update specs, create focused checkpoint commit and Draft PR, push after secret/diff review. Keep VERSION/release/main/production acceptance untouched until gate.
