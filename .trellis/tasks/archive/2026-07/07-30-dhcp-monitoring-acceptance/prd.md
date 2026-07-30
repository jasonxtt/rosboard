# Complete DHCP monitoring deployment acceptance

## Goal

Complete the already-implemented DHCP panel, route-page enrichment, and
monitor-menu reorganization through local/runtime verification and the
project's remote manual-acceptance gate.

## Requirements

- Preserve the existing uncommitted implementation; make no unrelated edits.
- Verify the Go tests, static checks, and embedded frontend production build.
- Build the verified Linux amd64 binary, then deploy it to `10.0.0.6` only
  after creating one timestamped remote backup of the current binary,
  configuration, SQLite database, and SQLite sidecars if present.
- Verify the remote systemd service, health endpoint, DHCP/dashboard API
  contracts, and served embedded assets.
- Stop for the user's manual inspection and explicit approval before any work
  commit, task archival, or journal update.

## Acceptance Criteria

- [x] `go test ./...`, `go vet ./...`, frontend lint, and frontend production
  build pass locally.
- [x] The verified deployment is active on `10.0.0.6` and an exact rollback
  bundle exists.
- [x] The remote systemd service, health endpoint, and embedded asset
  references are verified. The protected DHCP/dashboard endpoints correctly
  require authentication; their authenticated payload review is assigned to
  the operator's manual acceptance below.
- [x] The user manually accepts the deployed DHCP, route, and menu workflows.
- [ ] Only after approval, the implementation is committed and the task is
  recorded/archived.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
