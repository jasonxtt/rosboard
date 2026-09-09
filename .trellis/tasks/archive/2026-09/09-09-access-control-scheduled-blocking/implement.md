# Implementation plan: access-control scheduled blocking

## Phase 1 — Domain, storage, and API contract

- [x] Add schedule/window types, canonical weekday/time parsing, validation,
      cross-midnight normalization, and deterministic serialization.
- [x] Add v3 `schedule_json` migration and strict schema validation; cover fresh,
      v1, v2, replay, and broken-schema cases.
- [x] Thread schedule through rule load/save, proposal commits, snapshots,
      audits, revisions, and API responses.
- [x] Preserve old request compatibility by defaulting missing schedule to
      canonical `always`.
- [x] Run focused Go tests and inspect the diff before checkpoint commit.

## Phase 2 — RouterOS desired state and reconciliation

- [x] Compile weekly windows to RouterOS `time` strings, including IPv4/IPv6,
      multiple windows, and cross-midnight segments.
- [x] Update target-list jump projection and internet-scope projection without
      changing permanent-deny behavior.
- [x] Add `time` to managed fields and prove weekly-to-always stale-field
      removal in reconcile tests.
- [x] Extend capability/mutation probes and failure reporting for scheduled
      rules; preserve access ordering/readback verification.
- [x] Run backend package tests, race tests for affected packages, vet, and
      `git diff --check`; commit and push a focused checkpoint.

## Phase 3 — Aurora UI

- [x] Extend canonical types/API parsing and add shared schedule helpers.
- [x] Add the explicit blocking-window editor, validation, summaries, and
      RouterOS timezone context to Aurora.
- [x] Add frontend tests and run lint/test/build/check:ui-build.

## Phase 4 — Compact UI and parity

- [x] Replace the Compact disabled time placeholder with the shared contract and
      editor.
- [x] Ensure table summaries, modal copy, and validation match Aurora behavior.
- [x] Run dual-entry UI tests, build, and embedded asset verification; inspect
      the staged diff for unrelated/generated/private files.

## Phase 5 — Review loop and runtime verification

- [ ] Ask the referenced conversation to review the current branch/commit and
      current GitHub diff.
- [ ] If review is not approved, fix findings, rerun required validation,
      commit/push the same branch, and resubmit until explicitly approved.
- [ ] Deploy only to disposable `10.0.0.60` after automated checks and review;
      test always, weekly, multiple windows, cross-midnight, IPv4/IPv6,
      disabled rules, edit/delete, weekly-to-always cleanup, and service restart.
- [ ] Do not deploy to `10.0.0.6` until its backup/manual-acceptance gate is
      explicitly authorized and completed.

## Required validation commands

```bash
gofmt -w <changed-go-files>
go test ./...
go test -race ./internal/accesscontrol ./internal/store ./internal/policyv2
go vet ./...
git diff --check
npm --prefix web test
npm --prefix web run lint
npm --prefix web run build
npm --prefix web run check:ui-build
```
