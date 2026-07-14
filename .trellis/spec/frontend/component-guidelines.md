# Component Guidelines

## Terminal monitor filter controls

The terminal monitor uses component-local state for table presentation. Its initial state is the product default, while server data remains unchanged.

- Default `stateFilter` to `online` so the first list shows only currently online terminals.
- Keep the explicit status select for precise filtering.
- The nearby non-online-device toggle maps `online -> all -> online`; its label and `aria-pressed` state must follow the current filter. It covers both `inactive` and `offline` states.
- Any filter change resets pagination to page 1.
- Default terminal sorting is numeric address ascending (`sortKey = "address"`), not device-name sorting.

```tsx
const [stateFilter, setStateFilter] = useState('online')
const showingInactive = stateFilter !== 'online'

<button
  type="button"
  aria-pressed={showingInactive}
  onClick={() => setStateFilter(showingInactive ? 'online' : 'all')}
>
  {showingInactive ? '隐藏非在线设备' : '显示非在线设备'}
</button>
```

## Scenario: Terminal connection column filters

### 1. Scope / Trigger

- Trigger: changes to terminal list mobile controls, terminal detail header, connection family scope, connection search, or connection table filters.

### 2. Signatures

- Terminal default sort: `TerminalSortKey = "address"`, direction `"asc"`; reuse `compareTerminal` and its numeric IPv4 comparison.
- Detail scope: `TerminalFamily = "all" | "ipv4" | "ipv6"`; connection filter: `ConnectionFamily` with the same values.
- UI: `ConnectionColumnHeader({ label, sortKey, filterKey?, ... })`; global search is `.table-search-button` plus a floating `.connection-filter-panel`.

### 3. Contracts

- Terminal mobile controls form a five-column, two-row grid. Search spans the first four columns and the result count occupies the fifth; state, interface, non-online, refresh-period, and manual-refresh controls occupy the second row. Every interactive control is exactly 44px high, and mobile-only short text keeps narrow controls readable.
- Detail scope is applied before any user filter. `all` may show IPv4 and IPv6; `ipv4` can only produce IPv4 rows; `ipv6` can only produce IPv6 rows.
- Connection table column 1 is an explicit textual IPv4/IPv6 badge. Only `all` scope exposes an IP-version filter control.
- Family, application, protocol, line, local endpoint, destination endpoint, flags, and status filters open from their column headers. Active filters use both blue styling and accessible state/labels.
- Every connection column label is a dedicated sort button. First click selects ascending, the next click toggles descending; a separate filter-arrow button never triggers sorting.
- Keep the filter arrow immediately adjacent to the sort label. Do not reserve an empty sort-indicator width for unsorted columns; render the direction indicator only for the active sort.
- Filter panels compute their horizontal position from the clicked arrow's bounding rect. Desktop panels align to the trigger when space permits; mobile panels use 240px width and clamp inside the visible table shell.
- Family, application, protocol, outbound-line, and flags filters render their available values as direct option buttons in the first panel layer. Selecting an option applies it and closes the panel; do not put these enum choices behind a native select that requires a second click.
- Global fuzzy search is an icon button at table-header height on desktop and in the connection-detail tab row on mobile. Its floating input searches application, protocol, line, local/destination/public address, ports, and connection mark without adding a permanent toolbar row.
- A textless SVG clear button sits immediately left of global search. It clears all connection filters, search, family selection, panel, and sorting; it is disabled when no table state is active. Mobile clear/search actions render only while the connection tab is active.
- Reserve enough width and right padding in the final connection-status header for the floating clear/search actions. The action buttons must not overlap the status label even when the inner table is horizontally scrolled to the end.
- The legacy `.connection-toolbar` and family tab strip do not render.
- On mobile, the back button remains in the detail-card upper-right with a 44px target and never takes a separate full-width row.
- Mobile detail tabs use a fixed three- or four-column grid, with an additional auto-sized action column only on connection details. The row must not scroll horizontally. Clear/search use 36px visible boxes with pseudo-element hit areas extending to 44px, and they must not cover the history tab.
- Second-level monitor group toggles use the same 12px typography and visual weight as the line-monitor item.

### 4. Validation & Error Matrix

- `all` scope + IPv4 filter -> zero IPv6 badges, active family header indicator.
- `ipv4` or `ipv6` scope -> no family filter button; every rendered row matches the scope.
- Filter combination has no matches -> one empty row spans all 14 table columns.
- Search/filter panel opens -> it overlays the table and does not increase document width or add a toolbar row.
- Direct enum filter opens -> all current choices are visible immediately; selection closes the panel and activates the header indicator.
- Column label click -> changes only sort state; filter-arrow click -> changes only the active filter panel.
- Clear with active sort/filter -> reset to unsorted scope rows and disable the clear button.
- Live detail refresh -> local filter state remains component-local and continues filtering the new connection snapshot.

### 5. Good/Base/Bad Cases

- Good: addresses render in `10.0.0.8, 10.0.0.10` order and `IP / MAC ↑` is visible initially.
- Good: an all-scope detail contains both IPv4 and IPv6 badges; selecting IPv4 removes only IPv6 rows.
- Base: clicking the protocol header opens a select in the common floating panel.
- Bad: connection family tabs and search controls consume a separate row above the table.
- Bad: entering through IPv6 but filtering from the unscoped connection array leaks IPv4 rows.

### 6. Tests Required

- Build/lint/audit: production TypeScript build, oxlint, and dependency audit pass.
- Browser 375px: terminal toolbar has exactly two 44px control rows, refresh value is `1000`, back target is 44px, and document width does not exceed viewport.
- Browser sort: first addresses demonstrate numeric ascending order including `.8` before `.10`.
- Browser all scope: both badge families appear and the family filter removes the opposite family.
- Browser IPv6 scope: all badges are IPv6 and no IP-version filter button renders.
- Browser runtime: header filters and global search work with no console errors.
- Browser interaction: application ascending begins with `常用协议`, descending begins with `未知应用`; filtering preserves sort until clear.
- Browser geometry: desktop filter panel left equals its trigger left when space permits; mobile arrow is 44x44 and the clamped panel stays within the viewport.
- Browser geometry: unsorted filterable headers have a zero-width visual gap between the sort button and filter button; at maximum horizontal table scroll, the final status controls end before the clear button begins.
- Browser mobile: terminal toolbar has two rows; every interactive control is 44px high; all available detail tabs fit without horizontal scrolling; 36px connection-action visuals do not overlap history; switching to another detail tab removes the actions.

### 7. Wrong vs Correct

#### Wrong

```tsx
<div className="connection-toolbar">
  <FamilyTabs />
  <select aria-label="连接协议" />
  <input placeholder="目标地址" />
</div>
```

#### Correct

```tsx
<ConnectionColumnHeader label="协议" sortKey="protocol" filterKey="protocol" onSort={changeSort} onOpenFilter={openFilter} />
<button className="table-clear-button" aria-label="清除全部筛选和排序"><Icon name="clear" /></button>
```

## Terminal scope summary layout

- Render the selected `terminalScopeSummaries` entry only in the terminal-list topbar, between the title and global refresh controls. Do not place it in the terminal filter toolbar.
- Desktop uses the terminal-detail header's muted 11-12px inline typography and tabular numerals. Mobile uses a full-width second topbar row with a two-row, three-column grid.
- The six labels are device count, connection count, upload, download, active cumulative upload, and active cumulative download. Use `formatBits` for current bit/s values and `formatBytes` for active bytes.
- Unexpectedly missing summary data renders zero values; never fall back to persisted combined terminal totals.
- Verify 375px layout has no document-level overflow and the existing two-row, 44px terminal toolbar remains unchanged.

## Toolbar sizing

Inputs and selects placed in `.data-toolbar` use a 34px control height. Search inputs should use a bounded width rather than `width: 100%` so they remain visually balanced with adjacent filters.

## Text-like buttons

Address, detail, and remark actions are native buttons styled as text links. Reset native appearance and borders, but preserve a visible keyboard-only focus ring.

```css
.link-button {
  appearance: none;
  border: 0;
}

.link-button:focus { outline: none; }
.link-button:focus-visible {
  outline: 2px solid rgba(47, 126, 230, .55);
  outline-offset: 2px;
}
```

Do not use `outline: none` without a matching `:focus-visible` rule; that removes essential keyboard navigation feedback.

## Verification

- Browser: initial status is online and no offline rows render.
- Browser: the toggle expands to all states and returns to online.
- Computed style: search and select heights are 34px; text-like button border is `0px none`.
- Keyboard: tab focus on a text-like button displays the blue focus ring.

## Responsive application shell

The monitoring console is desktop/tablet-first but must remain fully usable on mobile.

- At widths below 1200px, replace the fixed sidebar with an off-canvas drawer and a labelled menu button.
- The drawer needs a full-page backdrop that closes it and must not create page-level horizontal overflow.
- At widths below 768px, use one-column cards and 44px minimum touch targets.
- Dense terminal and interface tables show only key columns on mobile. Keep the terminal detail/interface detail action available so hidden fields remain reachable.
- Exceptionally wide connection-detail tables may scroll inside `.table-scroll`; the document itself must not scroll horizontally.

```css
@media (max-width: 767px) {
  .terminal-table { min-width: 0; }
  .terminal-table th:nth-child(5),
  .terminal-table td:nth-child(5) { display: none; }
}
```

Verify document overflow at 375, 768, 1024, and 1440px, and verify the mobile drawer open/close state rather than checking CSS alone.

## Visual tokens

New UI colors, radii, surfaces, shadows, and focus colors belong in semantic custom properties under `:root`. Components consume tokens rather than defining parallel brand colors.

```css
:root {
  --color-primary: #2563eb;
  --color-surface: #fff;
  --color-border: #dce4ef;
}
```

Status colors must be paired with text or another non-color cue. The current UI is light-only, but semantic tokens preserve a clean future theme boundary.

## Embedded frontend verification

`npm run build` writes to `internal/ui/dist`, but an already-running `rosboard` binary still serves the assets embedded when that binary was compiled. After a frontend build, rebuild the Go binary and restart it before browser verification:

```bash
npm --prefix web run build
go build -o ./rosboard ./cmd/rosboard
./scripts/run-local.sh
```

If the browser still shows the previous title, brand casing, or asset hash after reload, check the running process before diagnosing the React/CSS change.

## Reference-driven UI fidelity

When the user approves a high-fidelity reference image, treat its information architecture as an implementation contract, not a loose palette suggestion.

- Inventory the reference before coding: card count, major grid ratios, panel order, table density, icons, status rows, and topbar/sidebar controls.
- Map every visible metric to a real API field. If the reference includes unsupported sensors or historical events, replace them with honest current-state rows rather than inventing values.
- Validate with an actual 1440×900 browser screenshot and compare the rendered structure to the reference. A passing build is not visual acceptance.
- Repeat the render/compare loop while major structural gaps remain.

```tsx
// Correct: preserve the four-card reference structure with real Rosboard fields.
<MetricCard title="CPU 使用率" value={`${overview.cpuLoadPercent}%`} />
<MetricCard title="内存使用率" value={`${overview.memoryUsedPercent}%`} />
<MetricCard title="在线终端" value={`${overview.connectedDeviceCount}`} />
<MetricCard title="活动连接" value={`${overview.connectionCount}`} />
```

Do not replace an approved four-card dashboard with eight generic tiles merely because more fields are available. Extra facts belong in the status panel or detail pages.

## Overview data typography

The overview keeps compact headings but must not render operational data at the
old 9-10px reference-image scale. At 100% browser zoom, use the established
data hierarchy below:

- Primary metric values remain 23px.
- Metric details and footers are 12px; live upload/download values are 13px.
- ECharts axis labels are 12px and tooltips are 13px. When increasing axis
  text, also increase the chart grid's left and bottom margins so labels are
  not clipped by the Canvas boundary.
- System-status labels and values are 12px; compact status text is 11px.
- Overview interface headers are 11px and values are 12px.
- Alert rows are 12px and their compact summary is 11px.

Scope these sizes to overview-specific selectors where a shared selector would
change denser detail pages. Preserve tabular numerals for changing values, and
validate the final embedded build at 100% zoom without page-level horizontal
overflow.

## Scenario: Real-time traffic chart

### 1. Scope / Trigger

- Trigger: changes to overview/interface time-series charts, current WAN rate labels, ECharts dependencies, or chart responsive sizing.

### 2. Signatures

- Data: `RateSample { timestamp, uploadBps, downloadBps }`; values are bits per second.
- Formatter: `formatBitRate(value)` returns `bps`, `Kbps`, `Mbps`, `Gbps`, or `Tbps` without dividing by eight.
- Component: `RealtimeTrafficChart({ samples, ariaLabel? })`.
- Dependency: ECharts 6.1+ via `echarts/core`, registered with line, grid, tooltip, and canvas modules only.

### 3. Contracts

- Overview current values, y-axis labels, and tooltip values must use the same bit-rate formatter.
- Download is blue and listed first; upload is green and listed second. Visible text labels accompany colors.
- The chart component is React-lazy-loaded so ECharts remains outside the initial application chunk.
- Initialize one Canvas instance, update it with `setOption`, resize through `ResizeObserver`, and dispose it on unmount.
- Use a 280px desktop height and 220px mobile height; the Canvas fills the panel content width.

### 4. Validation & Error Matrix

- Empty samples -> render the explicit empty state instead of an empty Canvas.
- Non-finite/negative rate -> format as `0 bps`.
- Reduced-motion preference -> disable chart animation.
- Container resize -> Canvas width follows the container; document width must not exceed viewport width.
- Dependency audit finding -> use a patched ECharts release rather than copying an older reference version exactly.

### 5. Good/Base/Bad Cases

- Good: API reports `1_795_328`; visible current rate and tooltip show `1.80 Mbps`.
- Base: 61 five-minute samples render as smooth lines with gradient areas and no permanent point markers.
- Bad: label says `bps` while the formatter divides by eight and emits `MB/s`.
- Bad: fixed SVG viewBox plus forced CSS height leaves large horizontal whitespace on a wide dashboard panel.

### 6. Tests Required

- Build/lint: TypeScript and production Vite build pass with ECharts in a separate lazy chunk.
- Security: `npm audit` reports zero known vulnerabilities.
- Browser desktop: Canvas inner width matches panel content width and height is 280px.
- Browser mobile: Canvas height is 220px, fills available width, and the page has no horizontal overflow.
- Browser runtime: continuous dashboard refresh produces no console warning/error.

### 7. Wrong vs Correct

#### Wrong

```tsx
<span>下载 (bps)</span>
<svg viewBox="0 0 820 280" className="chart-svg" />
```

#### Correct

```tsx
<span>下载（{formatBitRate(overview.downloadBps)}）</span>
<Suspense fallback={<div className="realtime-traffic-chart chart-loading" />}>
  <RealtimeTrafficChart samples={overview.chartSamples} />
</Suspense>
```
