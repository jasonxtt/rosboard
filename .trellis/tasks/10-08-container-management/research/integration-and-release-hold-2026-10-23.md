# Consolidated development and release hold — 2026-10-23

## User decision

The user considers the new container features unfinished and does not want users
to update yet. They approved consolidating the related development into the
existing `codex/container-management` branch and Draft PR #30. PRs #32 and #33
are superseded by that development entry after verification. Preserve source
history and branches; do not merge main, publish, deploy or archive tasks.

## Integration scope

- PR #30 source 56d858e: native lifecycle/Files, host registry/SFTP, proxy,
  onboarding, memory/error handling and historical root cleanup 5a887a4.
- PR #32 source 956da0b: Compose/Docker run import and native container UI.
- PR #33 source 8a77d34: Aurora routing page layout, descending from the PR #32
  implementation. Merge both source heads to preserve their complete histories.
- Existing #31 policy adoption remains a separate planning-only hold.

The functional merge is conflict-free. Historical deletion source/tests remain
identical to #30; the UI/import changes do not replace that cleanup amendment.
Rebuild embedded frontend assets from the combined source. Existing per-feature
research remains historical evidence; delivery/acceptance statements there do not
approve the newly integrated build for release.

## Publication boundary

- `VERSION` stays 0.2.7. The latest official release observed is v0.2.7.
- `.github/workflows/release.yml` runs only for pushes to main changing VERSION.
- `.github/workflows/docker.yml` publishes only for pushes to main changing
  VERSION; pull-request jobs build without pushing images.
- `internal/update/release.go` discovers `/releases/latest` and explicitly rejects
  drafts, prereleases and non-stable version tags.
- Development branch pushes and a Draft PR therefore do not publish an update.
  No workflow modification is necessary for this consolidation.

## Remaining work and acceptance

- Continue unfinished container behavior and UI fixes on #30 as user feedback
  identifies them; consolidation itself does not assert feature completeness.
- Exercise subsequent runtime changes on isolated 10.0.0.60 with independent data.
- Review both shells' create/import/edit/delete/recovery flows at desktop/mobile
  widths and light/dark themes before final acceptance; verify Aurora routing
  layout and access-control page isolation as well.
- Any later production deployment still requires the NAS backup and manual
  acceptance gate. No production or test service is replaced in this session.
- Keep #30 Draft and all three task records in_progress. Formal main merge and
  release require a separate explicit acceptance decision after remaining work.

## Integration validation

- `TMPDIR=/private/tmp go test ./...`, `go vet ./...`, `go build ./...`: pass.
- `npm --prefix web ci`, all 108 frontend tests, lint, TypeScript/Vite build and
  `check:ui-build`: pass. Rebuilt embedded assets exactly match the merged assets.
- Four existing FastTrack JSX-key lint warnings remain. npm reports two high
  dependency audit findings in the unchanged lockfile; dependency remediation is
  outside this branch-consolidation change.
- The first default macOS test run rejected symlinked temporary-directory
  ancestors in image staging. Using a real `/private/tmp` TMPDIR passes without
  weakening production path validation or changing runtime source.
- `git diff --check`: pass. VERSION, workflows and dependency locks unchanged.
- The latest #30 historical deletion implementation and tests are unchanged.
