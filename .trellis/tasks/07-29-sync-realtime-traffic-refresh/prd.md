# Sync realtime traffic chart refresh

## Goal

Make the overview realtime traffic line chart update at the operator-selected
global auto-refresh interval.

## Requirements

- The traffic-history request interval follows the topbar global auto-refresh
  selection (1, 3, 5, or 10 seconds).
- Selecting "停止刷新" stops periodic traffic-history requests while retaining
  the initial load when the overview or time range changes.
- Do not change backend collection, API contracts, configuration, or unrelated
  chart behavior.
- Rebuild the embedded frontend, run the project validation suite, and deploy to
  `10.0.0.6` with timestamped backups before user acceptance.
- Do not commit or push before the user approves the deployed result.

## Acceptance Criteria

- [x] The realtime traffic line chart refresh timer uses the current global
  auto-refresh interval instead of the previous hard-coded interval.
- [x] Stopping global auto-refresh stops periodic traffic-history requests.
- [x] Frontend lint/build, `git diff --check`, and Go tests pass.
- [x] `internal/ui/dist` is regenerated and the verified binary is deployed to
  `10.0.0.6` after backing up the binary, configuration, and SQLite data.
- [x] Remote systemd, health/API, and embedded frontend checks pass, then work
  stops for manual user acceptance without a commit or push.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
