# Mobile monitoring header unification

## Scope

This design extends the existing mobile-overlap fix without changing dashboard APIs, polling behavior, desktop information architecture, or monitor-page data logic. The implementation remains centered in `web/src/App.tsx` and `web/src/index.css`; Vite and server cache compatibility changes already present in the task stay intact.

## Responsive templates

### Summary monitor template

Terminal and interface monitoring use three mobile rows below 768px:

1. A 44px navigation row containing the mobile menu target and equal-width page tabs.
2. A single-row six-column summary. Each cell may stack its short label and value internally, but the six cells never wrap or scroll.
3. A 44px action row. Terminal monitoring adds a flexible search input before the global controls; interface monitoring uses the available width for the status label.

Terminal summary order is device count, connection count, current upload, current download, active cumulative upload, and active cumulative download. Interface summary order is interface count, physical count, logical count, active count, current upload, and current download.

### Non-summary monitor template

Traffic monitoring, network services, and system runtime use two mobile rows: the existing menu/page-tab navigation row followed by the shared global action row. They do not receive placeholder metrics or an empty row.

Panel settings uses its mobile menu/active-section title row followed by the same global action row.

## Shared global action row

The mobile action row is one reusable visual contract even if the current implementation keeps the JSX in the existing shared topbar:

- status/warning occupies flexible remaining space and may use a compact icon/badge in the terminal row;
- theme is a 44px icon button that opens the existing theme menu;
- manual refresh is a 44px icon button;
- refresh interval is a 44px compact control showing `1s`, `3s`, `5s`, `10s`, or `停` while preserving the full accessible label and existing values;
- controls use the same height, border, radius, background, icon sizing, focus treatment, and pressed/hover treatment;
- gaps are 8px and the row neither wraps nor scrolls horizontally.

Equal visual size means equal height and equal icon-button footprint. The flexible search/status region may be wider because it carries content.

Normal last-updated text is hidden on mobile. Existing global error output remains the explicit recovery signal for refresh failure; any stale-data presentation added during implementation must reuse existing state rather than introduce a new polling contract.

## Component boundaries

- Extend `MonitorSummaryBar` with mobile-specific compact label/value presentation rather than creating separate terminal and interface markup.
- Keep `MonitorPageTabs` as the single tab component.
- Prefer one shared class/component contract for theme, refresh, and refresh-period controls across all views; do not add page-specific copies.
- Preserve current desktop labels (`主题`, full automatic-refresh option text, status and last-updated text).

## Compatibility and accessibility

- Keep legacy `max-width` media-query output and the existing `max-device-width` fallback for older iOS/WebViews.
- Maintain native buttons/select behavior, accessible names, selected/expanded state, and visible focus rings.
- Interactive targets are at least 44px. The compact summary is informational and is not treated as a touch target.
- Verify light, dark, and glass themes because the common surface/border tokens differ.

## Rollout and rollback

Build and verify locally, then create a new timestamped backup on `10.0.0.6` before replacing the prior preview. The prior backup sets listed in `prd.md` remain rollback points. Do not commit until the user approves the newly deployed mobile layout.
