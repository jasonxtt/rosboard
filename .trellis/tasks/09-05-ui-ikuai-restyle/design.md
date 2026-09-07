# Design and migration specification

## Authority and implementation boundary

Use [the frozen preview](research/approved-preview/README.md) for visual judgment, with the user-approved [overview exceptions](research/overview-reference-amendment.md), and the functional baseline for behavior. This supersedes the old 280 px navigation and bright-blue design in this task. Existing root preview files and earlier measured iKuai tokens are not acceptance targets.

Start implementation in `codex/ui-compact-rebuild` from functional baseline `7a7d463`, in `/Users/tom/github/rosboard-ui-compact`. Do not cherry-pick or merge the other agent’s UI commits. Selectively extract only the approved overview components and their necessary presentation dependencies from the pinned reference. Keep React, ECharts, request/state logic and policy modules. Change existing design tokens, CSS, presentation markup and small reusable patterns where repetition warrants it. Do not create a parallel component framework, copy demo JavaScript into React, or wholesale replace `App.tsx`.

The four overview header cards and quick-link module explicitly reuse the pinned branch reference. Their card palette, chart styling and internal composition override the general tokens below locally; do not recolor them to the study’s terminal-blue/connection-purple/resource-amber scheme. All other areas, especially the new-routing-policy selector controls and layout, retain the study’s design.

## Visual contract

| Element | Target |
|---|---|
| Desktop navigation | Primary 112 px + secondary approximately 112 px including divider: total 224 px |
| Pages without secondary group | 132 px navigation; keep content aligned to actual shell width |
| Compact single-column mode | 168 px; not an extra user-facing layout selector |
| Mobile drawer | 188 px, keyboard-operable close/overlay and existing navigation behavior |
| Navigation text/targets | 13 px, 44 px row targets, approximately 5–6 px horizontal padding, 16 px icons |
| Topbar / page spacing | 69 px topbar; 22 px vertical / 24 px horizontal content padding; wide viewport 24 / 28 px |
| Type | System sans/PingFang SC; page title 18, panel title 15, body 13–14, caption 12 px; tabular digits for metrics |
| Surfaces | Canvas `#f7f9fc`, surface `#ffffff`, soft `#f8fafc`, border `#e9edf2` |
| Text | Main `#343b46`, secondary `#737e8f`, muted `#8893a3` |
| Primary | `#5b8fdb`, hover `#467cc9`, active background `#edf5fd` |
| Status/chart accents | Green `#449979`, purple `#9181cf`, amber `#c79a4d`, red `#cc6366`; upload purple, download green |
| Metric tints | Blue `#f0f6fd`, purple `#f5f2fc`, amber `#fdf7ec` |
| Controls | Desktop 34–36 px; mobile interaction targets at least 44 px; control radius 5, panel radius 7 px |

Avoid heavy shadows, oversized headings and pervasive monospace. Keep monospace where it improves endpoint/code readability. Never encode status only by color. Keep visible focus, labels and readable disabled/error states; improve token contrast locally if needed rather than hiding low-contrast information.

Dark mode uses the preview's root palette: canvas `#151e2a`, surface `#1b2533`, soft `#202b3a`, border `#303b4b`, text `#d9e0eb`, secondary `#a1adbe`, muted `#8c99aa`, primary `#87b1ef`; semantic green/purple/amber/red `#76bd9f`/`#b3a4e6`/`#d9b270`/`#ec9194`. ECharts must read the same semantic tokens and respond to theme changes through the existing mechanism.

## Shell and responsive behavior

Device switcher sits above the two columns. Preserve all primary/submenu entries, actual group landing destinations, active selection, device switching and stored preferences. Fleet/overview have no empty secondary rail. Put existing page searches with their respective page toolbars; keep topbar page identity, refresh and existing theme controls. The study's review toolbar and layout selector are not shipped.

Proposed production breakpoints adapt the study to the existing mobile contract: above 900 px dual navigation; 768–900 px compact single navigation; at or below 767 px drawer. The study itself switches the mobile drawer at 650 px; this adjustment preserves usable controls on existing mobile widths. The four overview cards retain the branch reference’s desktop composition and adapt to two/one columns on smaller screens; monitoring stacks as width runs out. Tables scroll inside their own container, not the entire page. Dialogs fit the viewport, scroll their body and keep actions reachable.

## Overview: visual structure and real-data mapping

1. Copy the pinned branch reference’s four independent header cards: system/device identity, terminal count (purple), resource usage (blue), and active connections (orange). Preserve their card backgrounds, internal layout, terminal/connection sparklines, legends, composition bars, average/peak footers and CPU/memory progress bars. Keep the source order (identity → terminals → resources → connections); the user’s enumeration names the components rather than explicitly requiring a reorder. Do not use the former single-banner proposal. See the amendment for exact source anchors and colors.
2. Desktop left information rail is approximately 300 px (330 px on wide displays), strictly ordered: interface information (study direction, real WAN/collection fields) → quick links (copy the branch reference’s three-column icon/text module and all nine actions) → device information (the study’s module, adapted to real fields). Keep the lower device-information panel even when some identity fields also appear in the header; no deduplication/deletion is authorized.
3. Flexible right region: real upload/download chart, CPU and memory trends, actual interface-status table, current alerts and remaining system status information. Preserve severity counts and freshness/last-success signals.
4. The study's illustrative terminal table does not replace the real interface table. It establishes table appearance only; no terminal ranking feature is added. Keep all nine quick-link actions unless the user later approves removal.
5. Preserve current aggregate WAN semantics and label them accurately. Do not represent aggregated rates as a selected single interface or add a fake selector. Keep actual time ranges, units, series and empty/error states; prototype sample values never enter production.

## Lists, dialogs and forms

Tables use a clear toolbar, subtle header, thin separators, aligned numeric columns and restrained tags. Preserve real column sorting/filter menus, offline states and pagination. Do not add demo batch selection, export or disabled fake pagination. Routing and target tags may wrap without hiding values; row actions remain available at narrow widths.

Settings retain independent save scopes and every advanced connection/collection field. Group related fields into sections with label/control alignment, local help and footer actions. Existing dialogs keep close/cancel/save, pending, validation and failure handling. Authentication and no-device flows share typography and surfaces, without new login features.

## Policy workflow

Retain `RoutingRuleWizard.tsx` state machine and source/target/gateway contracts. Style the four stages as compact numbered steps, an orderly form body and clear footer. Existing editable/locked/jump behavior governs navigation, not the simplified demo.

Preview retains all configuration and plan metadata, grouped operations, family/action badges and expanded details. Blocking errors, warnings, pending-review requirements and required acknowledgements stay visible before application. Busy state and double-submit prevention must remain. Restore equivalent MAC-following versus fixed-IP helper explanation removed by the intervening restyle.

## File ownership and adaptation

- `web/src/index.css`: replace relevant shell/component rules coherently; avoid accumulating a second override stylesheet.
- `web/src/lib/themeTokens.ts`: synchronize semantic chart colors with CSS, retain theme observation.
- `web/src/App.tsx`: targeted shell, overview, authentication, monitoring/settings markup only; preserve handlers and effects.
- `web/src/features/policy/*` and `features/access-control/*`: presentation wrappers, labels and style classes; preserve model/API behavior.
- `web/vite.config.ts`: inspect the functional baseline’s proxy before runtime work. If it points at production, use a local/test target; selectively port the safe proxy change if needed, without importing the other UI changes.
- `internal/ui/dist`: update only as a deliberate verified frontend build checkpoint during later implementation, never during planning.

The preserved study stays immutable. If the user changes the visual direction, record an explicit new revision rather than silently editing the accepted reference.


## Accepted overview amendment — 2026-09-07

Latest user feedback approves the overall overview direction and authorizes rollout to remaining pages. Remove the separate system-status module; retain storage usage and data freshness in device information. Rename the WAN summary to **WAN信息**. Desktop left order remains WAN信息 → 快捷入口 → 设备信息. On mobile keep the header cards unchanged; after 活动连接 use 实时流量 → 快捷入口 → WAN信息 → CPU使用率 → 内存使用率 → 接口状态 → 设备信息 → 当前告警. This explicit amendment supersedes earlier system-status preservation and mobile ordering instructions; it does not authorize other business-field or control deletions. The frozen preview stays unchanged.
