# Implementation Checklist

- [x] Confirm current branch/PR state, relevant frontend/backend specs, and the
      exact TargetList edit/read path.
- [x] Update the TargetList detail contract to expose only the current
      editable manual content; keep summary responses lightweight.
- [x] Hydrate `TargetListModal` on edit with explicit loading/error state and a
      loaded-content baseline.
- [x] Require a fresh preview when an existing manual text value is changed,
      while preserving metadata-only saves and existing non-manual flows.
- [x] Add backend round-trip regression coverage for detail and content
      version boundaries.
- [x] Run lint, build, audit, targeted Go tests, all Go tests, vet, diff checks,
      and Trellis validation; inspect exact staged paths for secrets/unrelated
      files.
- [x] Commit and push a focused checkpoint on `feat/policy-access-rebuild`,
      then report the exact HEAD to the root reviewer.
- [x] Follow reviewer findings on the same branch until an explicit
      `APPROVED` is reported.
- [x] After review approval, deploy the pushed build to `10.0.0.60` and hand
      off the manual UI acceptance checklist to the user.

Runtime result: root review approved commit
`787656bf8c601fdfa578c621b494d22e6dd5e0fb`. Its Linux AMD64 build was deployed
to `/opt/rosboard-test/rosboard` and restarted through `rosboard-test.service`.
The deployed binary SHA-256 is
`58cbcfeeda3b3c8c8f250ad243b0ffcd9af5756e88a2075e492348508a001dff`, matching
the local build; the service is active, `/api/health` returns `{"ok":true}`, and
the embedded index JS/CSS and Target Library chunk return HTTP 200 on
`10.0.0.60:80`. User manual UI acceptance remains pending.
