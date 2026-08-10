# Journal - tom (Part 2)

> Continuation from `journal-1.md` (archived at ~2000 lines)
> Started: 2026-08-07

---



## Session 59: Disable recognition defaults for v0.1.0

**Date**: 2026-08-07
**Task**: Disable recognition defaults for v0.1.0
**Branch**: `main`

### Summary

Defaulted MosDNS and feature-library recognition switches to off, left the MosDNS address blank until the operator enters a plain address such as 10.0.0.3, kept the feature-library URL preconfigured, and added migration for legacy default-enabled configs. Verified tests/build, deployed and manually accepted on 10.0.0.6, then republished v0.1.0.

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `1d5df0c` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 60: Mobile dashboard topbar layout fix

**Date**: 2026-08-10
**Task**: Mobile dashboard topbar layout fix
**Branch**: `main`

### Summary

Normalized the mobile monitor topbar into one shared control contract, stopped summary-row overlap at phone widths, targeted safari12 for CSS output, and served index.html with no-cache so phones stop pinning stale asset hashes. Accepted directly by the user instead of a fresh 10.0.0.6 deploy cycle; task archived with follow-ups deferred.

### Main Changes

### Main Changes

- Shared one mobile control contract across the monitor topbars so theme, manual
  refresh, and refresh-period controls stop overlapping at narrow widths, while
  desktop labels and behavior stay unchanged.
- Kept summary rows from overflowing on phones; hid the redundant status count
  and swapped in the compact refresh-period select below 767px.
- Set `cssTarget: 'safari12'` in `web/vite.config.ts` so the legacy-compatible
  media queries survive the CSS build for older iOS Safari.
- Served `index.html` with `Cache-Control: no-cache` in `internal/api/server.go`
  so mobile browsers stop pinning a stale document that references asset hashes
  which no longer exist after a rebuild.

### Testing

- `npm --prefix web run build` (tsc -b clean; asset hashes reproduced the
  committed `internal/ui/dist/` byte-for-byte, confirming dist matched source)
- `go build ./...`
- `go vet ./...`
- `go test ./...` — all packages ok

### Notes

Acceptance deviated from the AGENTS.md deployment gate: the user accepted the
mobile work directly rather than through a fresh deploy-and-inspect cycle on
10.0.0.6, and asked to archive the task with follow-up fixes deferred.

Process defect found while archiving — worth knowing before the next archive:
`task.py archive` auto-commits with `run_git(["commit", "-m", msg])` at
`.trellis/scripts/common/task_store.py:603`, with **no pathspec**. The narrow
scoping promised by `safe_archive_paths_to_add` only governs what the script
`git add`s; anything already staged is swept into the `chore(task): archive`
commit. Here that pulled `internal/api/server.go`, part of `web/src/index.css`,
and `web/vite.config.ts` into a commit labeled as an archive. Recovered with
`git reset --soft HEAD~1` (unpushed) and re-split into two scoped commits.
Prefer `task.py archive --no-commit` whenever the working tree has staged code.


### Git Commits

| Hash | Message |
|------|---------|
| `eb893ed` | (see git log) |
| `de13b21` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete
