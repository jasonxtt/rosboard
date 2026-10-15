# Implementation and verification

1. Implement closed pure parser and table-driven regression tests.
2. Add authenticated/device-scoped no-I/O parse route and API contract tests.
3. Add shared modal, typed API adapter, safe field replacement and both CSS graphs.
4. Add interaction tests; run Go build/test/vet and frontend test/lint/build/
   check:ui-build, then inspect the complete feature diff and secret boundaries.
5. Commit/push focused checkpoint and open/attach a Draft PR targeting main;
   note dependency on unmerged PR #30. Keep task and PR unfinished.
6. Deploy isolated config/data to test machine. Verify parser API/embedded assets
   and actual create/start/delete with unique lab names, IPs and owned paths.
7. After successful tests, verify writable NAS, retain max 10 timestamped backups,
   preserve consistent binary/config/SQLite/unit, deploy production and verify.
8. Hand off exact UI URLs and desktop/mobile manual steps. Do not merge/release/
   archive before explicit acceptance and root APPROVED.
