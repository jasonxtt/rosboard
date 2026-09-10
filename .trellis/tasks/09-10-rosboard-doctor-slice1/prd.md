# rosboard Doctor Slice 1

## Goal

Implement the first read-only diagnostics slice from
`/Users/tom/Downloads/rosboard-doctor-PRD-v0.1.md`: one stable diagnostic
report model, a cached quick-health report, and a System Diagnostics view in
both Aurora and Compact.

## Requirements

- Add stable `DiagnosticFinding` and `DiagnosticReport` contracts with
  separate finding status and `AffectsOverall` semantics.
- Keep `disabled` and `skipped` findings out of the overall status.
- Reuse existing MonitorManager/MosDNS, Policy, Access, and Update state;
  do not create a second authoritative state model.
- Quick health must not perform a new RouterOS scan. It may read cached monitor
  snapshots and local SQLite/config/update state.
- Preserve device isolation. Device-scoped reports use the existing selected
  device convention; Update remains panel-global.
- Expose a read-only `GET /api/diagnostics?device=<id>` endpoint.
- Add a System Diagnostics entry under Settings in Aurora and Compact. Both
  UIs consume the shared backend report and shared frontend API/types; health
  decisions stay server-side.
- Keep existing untracked workspace assets untouched.

## Out of scope

- Deep RouterOS evidence snapshots.
- Ingress decision traces or changes to policy discovery/candidate behavior.
- Diagnostic ZIP/log export.
- Full RouterOS policy/access drift checks, target-library checks, CLI Doctor,
  or liveness/readiness endpoints.

## Acceptance Criteria

- [ ] `go test ./...`, `go vet ./...`, and `git diff --check` pass.
- [ ] Frontend tests, lint, production build, and dual-UI build verification
      pass.
- [ ] Quick endpoint is read-only and performs no new RouterOS requests.
- [ ] Core Monitor/RouterOS/storage failures can affect overall health;
      optional module failures do not automatically make the whole report
      error.
- [ ] Aurora and Compact show the same report statuses and findings.
- [ ] Existing Monitor, Policy, Access, and RouterOS behavior is unchanged.
