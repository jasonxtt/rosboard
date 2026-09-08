# Execution plan

- [x] Inspect PR metadata, branch ancestry, frontend entrypoints/settings and backend diffs.
- [x] Record approved scope and concrete design before implementation.
- [x] Merge PR histories without commit; preserve Aurora root and add isolated Compact source.
- [x] Implement shared variant bootstrap, preference compatibility and both settings selectors.
- [x] Reconcile terminal naming and other cross-UI API differences.
- [x] Add focused preference/bootstrap/settings integration and embedded asset checks.
- [x] npm --prefix web run lint; npm --prefix web run build; run web/scripts contract tests; npm audit.
- [x] go build ./...; go test ./...; go vet ./...; targeted race tests; git diff --check.
- [x] Deploy and exercise isolated test service and synthetic data including auth/API/asset contracts.
- [x] Review final integrated diff and update stable frontend specs.
- [x] Create verified NAS backup; deploy production and verify service/API/frontend assets.
- [x] Await explicit user manual production acceptance. Accepted with minor bugs deferred on 2026-09-08.
- [ ] After acceptance: commit, push integration branch, merge into main without old UI, archive task and record session.

Manual visual handoff: desktop and 375px mobile; Compact -> Aurora -> Compact through settings; reload persistence; light/dark; selected router retention; dashboards/monitor pages/policy wizards/terminal rename/settings. Browser visual approval remains user-led per frontend quality guidelines.
