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
- [ ] Commit and push a focused checkpoint on `feat/policy-access-rebuild`,
      then report the exact HEAD to the root reviewer.
- [ ] Follow reviewer findings on the same branch until an explicit
      `APPROVED` is reported.
- [ ] After review approval, deploy the pushed build to `10.0.0.60` and hand
      off the manual UI acceptance checklist to the user.
