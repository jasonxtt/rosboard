# Redesign fleet dashboard list

## Goal

Match the fleet dashboard's information hierarchy and dense list presentation
to the supplied reference image while retaining Rosboard's existing data and
navigation behavior.

## Requirements

- Continue from the existing uncommitted dashboard work on
  `codex/fleet-dashboard-cards`; do not create another branch.
- Keep the top summary as exactly four items, ordered and labeled:
  `全部设备`, `在线设备`, `离线设备`, `告警设备`.
- Present devices only as a dense unified list. Remove the card/grid mode and
  any view preference, switch, or alternate renderer used only by that mode.
- The desktop device-list columns must be ordered and labeled:
  `设备信息`, `CPU`, `内存`, `流量速率`, `终端数量`, `连接数量`, `运行时间`.
- Preserve the existing search, status filter, name sort, pagination, device
  status, per-device metrics, update time, and click-through to device detail.
- Online rows show CPU and memory percentages, upload/download rates, terminal
  distribution, connection distribution, uptime, and update time.
- Offline or unavailable rows keep the device identity and runtime/update area
  visible, with a clear unavailable-state message across the live-metric area.
- Closely reproduce the reference image's light, compact operations-dashboard
  feel: white surfaces, subtle borders/shadows, restrained radii, aligned
  columns, circular metric indicators, muted metadata, and semantic status
  colors. Existing dark-theme support must remain legible.
- Responsive layouts must remain usable without document-level horizontal
  overflow; mobile controls must retain 44px touch targets.
- Do not change fleet API contracts or backend collection behavior.
- Before any commit, run automated checks, build the embedded frontend, verify
  locally, back up the deployed binary/configuration/SQLite data with one
  timestamp, deploy to `10.0.0.6`, and verify systemd, health, affected APIs,
  and embedded assets.
- Stop after deployment for the user's manual inspection and explicit approval.
  Do not commit, archive, or journal before that approval.

## Acceptance Criteria

- [x] The four summary items appear in the required order with correct fleet
  counts and no legacy summary labels.
- [x] Only one device presentation exists; there is no card/grid view switch or
  persisted fleet-view preference.
- [x] At desktop width, a single header row and every online device row align to
  the required seven-column order.
- [x] Search includes name/model/version/IP, filters and sorting reset pagination
  as before, pagination remains functional, and device rows open the selected
  device overview.
- [x] Online, alerting, offline, no-result, light-theme, and dark-theme states
  remain readable and distinguishable without relying on color alone.
- [x] At 375px, summary cards and device content reflow without document-level
  horizontal overflow, and interactive controls are at least 44px high.
- [x] `npm --prefix web run lint`, `npm --prefix web run build`, relevant Go
  tests, and embedded-asset verification pass.
- [x] The deployed instance at `http://10.0.0.6:8080/` has a timestamped rollback
  backup and passes service, health, fleet/device API, and asset checks.
- [x] The user explicitly approves the deployed UI before a work commit is made.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
