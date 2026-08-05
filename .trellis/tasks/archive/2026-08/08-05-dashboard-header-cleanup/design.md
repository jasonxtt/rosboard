# Technical Design

## Scope

Frontend-only change in `web/src/App.tsx` and `web/src/index.css`, plus the
tracked embedded frontend output under `internal/ui/dist`. No API, model, or
persisted-data changes are needed.

## Dashboard search

- Lift the dashboard query state to `PanelApp` so the shared topbar owns the
  controlled input and its placement.
- Pass the query and change callback into `FleetDashboardPage` for the same
  client-side device matching behavior.
- Remove `FleetStatusFilter`, `FleetSort`, their local state, sorting logic,
  and both selects. The list keeps only text search and existing pagination.
- Render the search input in the topbar immediately before the existing theme
  control. Desktop fleet controls use a no-wrap row; on mobile the search may
  wrap to a full-width 44px row.

## Shared page headings

- Add a small `hideTopbarHeading` condition in `PanelApp` for `interfaces` and
  `settings`.
- Keep the mobile menu button and right-side health/last-updated controls;
  remove only the redundant left title/subtitle block.
- Give headingless topbars a compact height so removing the shared heading also
  moves the settings/interface content upward instead of leaving an empty title
  band.
- Keep `SettingsPage`'s own section headings. `InterfacesPage` uses the shared
  monitor tab component for `物理接口` / `逻辑接口`, renders one selected
  category in a `data-toolbar` + table block matching the terminal list's
  information height, and keeps system interfaces in a collapsed section.
- Use one shared monitor-summary presentation for terminal and interface
  topbars. The interface summary shows total, physical, logical, active, and
  current upload/download values without adding a second content toolbar.
- Apply the same interface-heading rule to the empty-device shell.

## Responsive and accessibility behavior

- Use the existing native input and focus styles; preserve the `搜索设备`
  accessible label and existing placeholder.
- Keep the search bounded on desktop and 44px/high 16px text on mobile to
  avoid mobile zoom.
- Recheck 375px, 768px, 1024px, and 1440px layouts for no document overflow.

## DHCP Server list

- Replace the grid of stacked cards with one compact list whose desktop rows
  use labeled cells for server, interface, pool, range, lease, usage, and
  status. Keep the pool usage value numeric and add a restrained progress bar
  only as a visual aid.
- At 1000px and below, collapse rows into three grid bands; at 767px and
  below, use a two-column labeled card layout so long ranges remain readable
  without document-level horizontal overflow.

## Terminal connection detail height

- Add a connections-only modifier to `TerminalDetailPage`. Lock only this
  mode's shell/content to the browser viewport, then use three grid rows:
  natural-height device summary, natural-height detail tabs, and a
  `minmax(0, 1fr)` detail panel.
- Make that detail panel and its connection-table shell flex columns. The
  table viewport takes the remaining height, removes the previous `dvh` max
  clamp, and keeps its internal two-axis scrolling, sticky header, and
  scrollbar gutter. Use `dvh` with a `vh` fallback for desktop and mobile;
  leave an 8px content-bottom gap so the table boundary stays inside the
  browser viewport without growing the outer document.

## Sticky connection-header mask

- Keep the connection table's header row sticky above the scrolling rows and
  raise its stacking order so the `IP版本` header cannot be covered by table
  content.
- Add a pseudo-element mask inside each sticky header cell. Light and dark
  themes use `--color-surface`; the glass theme uses a fixed, opaque copy of
  its `--glass-page-background` through `--connection-header-mask`, so the
  existing pink/blue background remains visible while row text cannot bleed
  through the header background.
- Keep the header controls above the mask and preserve the existing table
  scroll container, focus ring, and horizontal overflow behavior.

## Terminal list viewport and presence selector

- Reuse the monitor topbar tabs (`全部` / `IPv4` / `IPv6`) for every terminal
  list scope. The list panel fills the remaining viewport below that topbar;
  only its table area scrolls vertically and horizontally, so the document
  itself does not extend below the visible browser window.
- Remove the terminal table's separate online-state and interface selects.
  Add an explicit `在线状态` column immediately before `在线时长`.
- Default the list to online terminals. Put the downward-arrow filter button
  directly beside `在线状态` in the table header; its popover offers `在线设备`,
  `全部设备`, and non-online-device choices. Selecting a choice resets
  pagination and keeps the current table sorting. Remove the separate
  `终端列表` / `在线设备` toolbar title block.
- Apply the same theme-adaptive sticky header mask to the terminal list table
  so its header remains readable during internal scrolling in light, dark, and
  glass themes.

## Rollback

Revert the frontend source and regenerated embedded assets. No data migration
or remote configuration change is involved.
