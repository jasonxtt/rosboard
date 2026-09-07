# Phase 1 checkpoint — 2026-09-07

Status: implementation checkpoint, awaiting user visual confirmation. Branch `codex/ui-compact-rebuild`, Draft PR #6. No deployment, merge, release, task completion or acceptance.

## Changes and preservation

- Shared controls use the approved study's soft blue, 5 px button corners, 34–36 px desktop heights, 44 px mobile targets, system typography and light/dark palette.
- NAV-01: all five primary groups and all fourteen secondary destinations remain. Groups open their real landing view; the active group owns the secondary column. Desktop widths are 112 + 112 px, overview/fleet use 132 px, intermediate widths use 168 px, and mobile uses a 188 px drawer. Device switcher stays above both columns.
- NAV-02: existing selected-device state, theme persistence and stop/1/3/5/10-second refresh handlers remain. Escape closes the drawer and restores focus; opening it focuses its first control. Hidden mobile navigation cannot receive keyboard focus.
- FLEET-01 / MON-02: the existing device and terminal queries move into their own page toolbar; filtering logic remains unchanged. Other page redesign and business regression verification remain pending.
- OVER-01: only the four header cards and quick-link module are extracted from immutable commit `74ac43aa6a6d3ca8b3fde33f8002b912d0f70757`. Their palette is scoped locally. MiniSparkline remains identical to the pinned implementation. CPU/memory history, current value, progress, average and peak remain below the traffic chart.
- OVER-02 / OVER-03: left order is interface information, nine quick links, device information. The WAN summary retains aggregate semantics and lists all contributing addresses; no fictitious interface selector. Device identity, architecture/core fields, full eight-column interface table, all six system-status rows and existing severity/source/time/count alert presentation remain. Tables scroll rather than dropping mobile columns.
- AUTH-02: the existing no-device routes remain in the compact shell. Authentication forms and later page layouts await their planned stages.
- No policy/access-control feature source, API payload, validation, polling cadence or backend source was changed. The frozen preview's four SHA-256 hashes still match its README. The other UI branch was not merged or cherry-picked.

## Verification

- `npm --prefix web ci`: installed the locked dependencies; no package or lockfile changes.
- `npm --prefix web run lint`: passes without warnings.
- `npm --prefix web run build`: TypeScript and Vite pass; generated `internal/ui/dist` included.
- `go build ./...`: passes against the embedded build.
- React server-render smoke checks: populated overview, empty interfaces/history/counts, and long names with an alert render without exceptions or NaN; nine shortcut actions and the expected information/status fields remain.
- LAN HTTP checks: page, linked entry assets, bootstrap, devices, settings, dashboard, realtime, load, traffic history and fleet responses return 200. Device-specific fixtures and history range queries are isolated.
- Source review: navigation callbacks, refresh/theme/search/device state, all overview fields and the pinned module dependencies inspected. `git diff --check` passes.
- No browser visual acceptance was performed, per frontend quality guidelines. Actual layout, keyboard interactions and real-device business regression remain for user review / subsequent stages. The fixture API cannot validate real RouterOS behavior.
- Spec review: this is an unaccepted visual checkpoint. Existing task-local precedence already documents the departure from historical shared layout guidance; no new shared invariant needs to be published yet.

## Actual application preview

Open http://10.0.0.86:8792/ and choose **系统概览**. This serves the actual built React frontend, with two clearly named simulated devices and read-only synthetic monitoring data. It is separate from the frozen 8791 study. It contains no RouterOS credentials and does not proxy production. Business APIs outside phase 1 return an explicit unsupported-preview error; writes are rejected.

To reproduce after building, start these in two terminals from the repository root (ports must be free):

```sh
python3 .trellis/tasks/09-05-ui-ikuai-restyle/research/phase1-preview.py
ROSBOARD_DEV_PROXY=http://127.0.0.1:18792 npm --prefix web run preview -- --host 10.0.0.86 --port 8792 --strictPort
```

Current process logs are under `/tmp/rosboard-compact-api.log` and `/tmp/rosboard-compact-web.log` on the development Mac. These processes are not installed as system services. The safe Vite proxy default is localhost; the above explicit target is required for fixture mode.

## User review checklist

1. At 1440×900 and 1920×1080, compare the overall shell/buttons against the frozen study and the four header cards / shortcut grid against the pinned exception. Confirm left-panel order and all information remains readable.
2. At 1024, 768 and 390 px widths, inspect navigation proportions, card wrapping, table scrolling and reachable actions. Use Tab/Shift+Tab and Escape for menus and the drawer.
3. Switch light/dark, select each preview device, inspect sparkline tooltips, change traffic range, stop automatic refresh and use manual refresh.
4. Follow each primary/secondary destination and quick link to check navigation. Later-page fixture errors are expected and do not represent an integrated business test. Return to overview before reviewing this phase.

Do not spread the redesign to other pages until the user explicitly confirms this phase's actual appearance.
