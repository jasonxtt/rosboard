# Integration verification checkpoint

- Source history: baseline 7a7d463; Aurora ab3b7bb; Compact 1dafe17. Remote main c1bcdef is ancestor of baseline. Worktree is codex/dual-ui-integration with uncommitted Compact merge; no source branch changed.
- Complete Compact tree imported; Aurora remains root. Backend changes from both PRs retained. Shared canonical policy adapter; customName-only terminal contract aligned.
- Added independent CSS/runtime bootstrap, both settings selectors, same-origin full navigation, browser persistence, legacy preference migration, storage-denial protection and settings handoff cleanup.
- 33 frontend contract/component tests pass; lint has no warnings; TypeScript/Vite build passes; package audit reports zero vulnerabilities.
- Build graph verification passes: zero bootstrap styles, one Compact root CSS dependency and two Aurora root CSS dependencies, no overlapping root styles.
- Go build, all package tests and vet pass. Race tests pass for api/service/store/policyv2/accesscontrol.
- Isolated systemd service rosboard-dual-ui on 10.0.0.60:8090, /opt/rosboard-dual-ui with fresh data and no RouterOS configuration. Administrator setup, explicit skip RouterOS, bootstrap, devices/settings/fleet reads, logout and login verified over HTTP.
- Final linux amd64 SHA256: 3627a91ea18d5516b5dd57d212ea499c2f2a2142f1f4b615c5f27c60b5b2fb33; test machine matches; NRestarts=0. All 69 public embedded resources match local files byte-for-byte; JS/CSS MIME and both entry URLs verified. Unauthenticated affected API endpoints retain auth protection.
- Visual acceptance is pending and remains user-led. No claims of full visual or live RouterOS mutation validation are made. Existing backend integration tests use fakes/synthetic databases; no destructive checks were run against production.

## Production handoff

- NAS mounted/writable confirmed. Removed only oldest timestamped backup 20260829T183827Z-policy-source-multi-format before creating the new tenth timestamped backup. `.retired` left untouched.
- Backup: /Users/tom/nas/wyp/github/rosboard/backups/20260908T094421Z-dual-ui-integration. Includes original binary, config, full data directory (all device databases and sidecars), and actual systemd unit. Service was stopped for a consistent copy; 129 file hashes match remote originals and all SQLite quick_check operations passed. SHA256.json records hashes in the private backup directory.
- Initial checksum attempt used a Python API unavailable on local Python; original service was automatically restarted without replacement. Retried the same incomplete backup with streaming SHA256, re-copied under a stopped service, and verified before installation.
- Production installed final tested binary SHA256 3627a91ea18d5516b5dd57d212ea499c2f2a2142f1f4b615c5f27c60b5b2fb33. systemd active/running, exit status 0, NRestarts=0, health 200. Config and unit unchanged.
- Both UI query URLs and all 69 public embedded assets match the local final build; JS/CSS MIME checks pass. Bootstrap requires login without credentials; affected API routes enforce authentication. Authenticated live device operations are left for the user's existing session/manual acceptance; automated authenticated runtime checks were on isolated test data only.
- Pending: user desktop/mobile and light/dark visual acceptance, explicit production approval, then new program commit/push and main merge. Neither original PR branch nor main was modified. Do not merge PR #6 and PR #7 independently: use the prepared combined merge state in codex/dual-ui-integration.

## Default Aurora amendment

User requested first visits default to Aurora. Changed only the variant fallback; saved valid choices and explicit query overrides remain authoritative. Updated regression test and docs. All 33 frontend tests, lint, build, UI graph checks and linux build pass; isolated test and production both match all 69 embedded resources. New NAS backup: /Users/tom/nas/wyp/github/rosboard/backups/20260908T142248Z-default-aurora; 129 hashes and all SQLite quick_check operations verified before replacement. Production active/running, NRestarts=0, health 200; final binary SHA256 f3a8b96c654a3797fbfd2c113a1146552c19a43f8498ff68dca0a6f38cc0cf7e. Earlier deployment hash above is superseded. Explicit manual acceptance remains pending before commit/main merge.

## Conditional stylesheet preload fix

User supplied screenshot of broken Compact UI. Browser reproduced Compact DOM with Aurora CSS only. Emitted Vite bootstrap had folded the ternary dynamic imports into one preload call using Aurora's dependency list. Replaced ternary with separate awaited branches and early return. Extended check-ui-build.mjs to execute emitted dispatcher in a minimal DOM harness for both variants; reproduced failure before fix and passes after fix. Manifest checks alone were insufficient.

33 component/contract tests, lint, build, executed-bootstrap checks and linux build pass. Browser verified actual built output with isolated synthetic data: Compact light -> Aurora light -> Aurora dark -> Compact dark, correct complete layouts and CSS isolation. Production browser reload verified Compact sidebar, controls and 16px icons restored, only main-CND2geYR.css loaded and no Aurora gradient. No production business writes performed.

NAS backup 20260908T144151Z-ui-stylesheet-fix: 129 files verified and SQLite quick_check passed. Final binary 8ef525c1fefb79b07987b94138d0b40c85c8958875ba9b9f50481173580a20ba supersedes previous hashes. Test/live services and all 69 resources verified; live active/running, NRestarts=0. Manual user acceptance and commit/main merge remain pending.

## User acceptance
User explicitly authorized merging into main on 2026-09-08, acknowledging remaining minor bugs and deferring those fixes. This satisfies the production manual acceptance gate for the delivered build; no additional bug-fix scope is included in this merge.
