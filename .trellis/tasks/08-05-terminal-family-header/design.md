# Technical Design

## Scope

This is a frontend-only change in `web/src/App.tsx` and
`web/src/index.css`. No API, model, or persisted-data contract changes are
needed.

## Layout

- Keep the top-level `状态监控` group in the sidebar, but render the terminal
  monitor entry as a second-level item without its family children.
- Add a reusable presentational `MonitorPageTabs` control in the grouped page
  header. It receives the current value, options, and an `onChange` callback
  so navigation state remains owned by `PanelApp`.
- Keep `query` controlled by `PanelApp`, place the terminal search in the
  terminal topbar, and remove the duplicate input from `TerminalsPage`'s local
  toolbar.
- Use a grouped monitor topbar layout: page tabs occupy the left/title area;
  health, last-updated, and the existing page-specific controls stay in the
  right control cluster. The existing non-grouped topbars remain unchanged.

## Behavior

- Terminal family tab clicks use the existing `setTerminalFamily` path and
  preserve the current filtering, sorting, pagination, and detail semantics.
- Other tab clicks switch the existing `ActiveView` routes without changing
  their page data or API contracts.
- The tab control uses native buttons with `aria-selected` and a tablist
  label. The terminal search keeps its existing controlled state and
  placeholder.
- The old toolbar keeps state, interface, non-online, result count, refresh
  interval, and manual refresh controls. Only its search input is removed.
- The terminal title/subtitle is omitted from the terminal topbar while the
  global health indicator remains in the right cluster.

## Responsive behavior

- Desktop grouped tabs are compact text tabs with a visible active underline.
- On narrow screens, the tabs become a full-width equal-column control. The
  terminal search remains in the topbar control area and is allowed to occupy
  a full row if required; it keeps a 44px height and 16px input text to avoid
  mobile zoom.
- Avoid adding a second horizontal scrolling region or changing the existing
  terminal table's responsive columns.

## Risks and rollback

- The main risk is topbar crowding at tablet widths. CSS media rules should
  wrap the terminal controls without changing other page headers.
- Rollback is a two-file revert before deployment; no data migration is
  involved.
