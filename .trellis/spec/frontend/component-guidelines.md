# Component Guidelines

## Interface monitoring categories

- Render physical and logical interfaces through the shared topbar tab row;
  show one selected category at a time from `InterfaceStatus.category`.
  Keep the system section collapsed initially below the selected category.
- Physical state labels distinguish enabled/running `在线`, enabled/not-running
  `Down`, and administratively disabled `已禁用`. Sort Down physical rows first.
- Render semantic interface relations from `relations[]`; do not invent a
  carrier for WireGuard or another topology without a fixed parent.
- The selected category uses the same compact `data-toolbar` + table block
  rhythm as the terminal monitor. Its local count and active count stay in
  that toolbar; the monitor topbar may show total, physical, logical, active,
  and current upload/download summary values.
- Every category retains the same interface-detail/history action.

## Fleet dashboard search and headings

- The fleet dashboard exposes one controlled device search in the shared
  topbar, immediately before the theme control. On desktop the search and
  theme remain on one row; on mobile the search may use a full-width row. Its
  matching fields are
  device name, RouterOS/router name, board name, version, and address.
- The fleet dashboard does not render status-filter or sort selects. Keep
  free-text matching, pagination, and device-row navigation available.
- `接口监控` hides only the redundant shared topbar heading/subtitle. Its
  headingless topbar keeps the standard desktop topbar row height, so the first
  content block sits at the same vertical offset as on the monitor pages; only
  the narrow-viewport rule collapses it. Interface category headings remain
  visible so the content context is preserved.
- `面板设置` child pages render the active section name in the shared topbar,
  in the slot and style the monitor pages use for their third-level tabs
  (static text, not a control). The matching panel heading is not repeated
  inside the card, so each settings card starts directly with its data.

## Terminal monitor filter controls

The terminal monitor uses component-local state for table presentation. Its initial state is the product default, while server data remains unchanged.

- Default `visibilityFilter` to `online` so the first list shows only currently online terminals.
- Do not render separate online-state or interface filter controls in the table toolbar.
- Place a visible downward-arrow filter button beside `在线状态` in the table header; its popover offers online devices, all devices, and non-online devices. Remove the separate `终端列表` / `在线设备` toolbar title block. The non-online option covers both `inactive` and `offline` states and is labeled accordingly.
- Add an explicit `在线状态` table column immediately before `在线时长`; online rows show their duration and non-online rows show `-` in the duration column.
- Any filter change resets pagination to page 1.
- Default terminal sorting is numeric address ascending (`sortKey = "address"`), not device-name sorting.
- The terminal identity column renders display name first and MAC as muted second-line metadata. The separate IP column renders primary IPv4 first and primary IPv6 second in `all` scope; family-specific scopes render only their selected family.

```tsx
const [visibilityFilter, setVisibilityFilter] = useState<'online' | 'all' | 'offline'>('online')

<button type="button" aria-label="筛选在线状态" aria-expanded={filterOpen}>
  <span aria-hidden="true">▾</span>
</button>
{filterOpen ? <FilterOptions value={visibilityFilter} onChange={setVisibilityFilter} /> : null}
```

## Scenario: Terminal connection column filters

### 1. Scope / Trigger

- Trigger: changes to terminal list mobile controls, terminal detail header,
  connection family scope, connection table viewport, or column filters.

### 2. Signatures

- Terminal default sort: `TerminalSortKey = "address"`, direction `"asc"`; reuse `compareTerminal` and its numeric IPv4 comparison.
- Detail scope: `TerminalFamily = "all" | "ipv4" | "ipv6"`; connection filter: `ConnectionFamily` with the same values.
- UI: `ConnectionColumnHeader({ label, sortKey, filterKey?, ... })` plus a
  floating `.connection-filter-panel` outside `.connection-table-viewport`.

### 3. Contracts

- Terminal mobile controls form one 44px row for result count, refresh period,
  and manual refresh. The online-state filter lives beside the table header and
  opens its popover from a 44px filter target. The terminal search lives in the
  topbar and may occupy its own 44px row. Every interactive control is at least
  44px high.
- The `状态监控` group has five direct second-level items: `接口监控`,
  `终端监控`, `流量监控`, `网络服务`, and `系统运行`; it does not render
  third-level entries in the sidebar. The grouped monitor choices render as
  topbar page tabs: `全部` / `IPv4` / `IPv6`, `协议统计` / `策略统计`,
  `DHCP` / `路由 / 分流`, and `资源监控` / `负载历史` respectively.
- Clicking `终端监控` opens the all-terminal list with `全部` selected;
  clicking the other group items opens their first page tab. Tab buttons use
  native controls with `role="tab"` and `aria-selected`.
- Detail scope is applied before any user filter. `all` may show IPv4 and IPv6; `ipv4` can only produce IPv4 rows; `ipv6` can only produce IPv6 rows.
- Connection table column 1 is an explicit textual IPv4/IPv6 badge. Only `all` scope exposes an IP-version filter control.
- Family, application, protocol, source IP, source port, destination endpoint,
  route-table, next-hop-gateway, and physical-egress filters open from their
  column headers. Source IP and source port are independent. RouterOS self
  connection tracking also exposes the status filter; ordinary terminal
  details do not. Every filter owns a local `全部…` reset. Active filters use
  both blue styling and accessible state/labels.
- Every connection column label is a dedicated sort button. First click selects ascending, the next click toggles descending; a separate filter-arrow button never triggers sorting.
- Keep the filter arrow immediately adjacent to the sort label. Do not reserve an empty sort-indicator width for unsorted columns; render the direction indicator only for the active sort.
- Filter panels compute their horizontal position from the clicked arrow's bounding rect. Desktop panels align to the trigger when space permits; mobile panels use 240px width and clamp inside the visible table shell.
- Family, application, protocol, route-table, next-hop-gateway, and egress
  filters render direct options in the first panel layer. A gateway or egress
  option represents one member of its array, including ECMP members. Missing
  values remain locally filterable. Selecting a direct option applies it and
  closes the panel.
- Global connection search, global clear actions, and connection toolbars do
  not render on desktop or mobile. Column-local reset options are the only
  filter-clearing controls.
- The connection table scrolls on both axes inside a viewport-bounded region;
  in terminal detail `连接详情`, the detail page fills the available
  viewport below the shell padding and the table viewport takes the remaining
  height below the summary and tabs. Lock only this mode's shell/content to
  the viewport, keep an 8px bottom gap, and do not let the outer document grow
  past the browser window. Its horizontal scrollbar stays at the bottom of
  that visible region and its header cells remain sticky during internal
  vertical scrolling. Do not cap this detail viewport with a fixed `dvh`
  maximum.
- The connection table's sticky header row, including `IP版本`, must sit above
  scrolled rows and use a theme-adaptive pseudo-element mask. Keep header
  controls above the mask; light/dark use the theme surface color, while glass
  uses its fixed `--glass-page-background` through
  `--connection-header-mask`, so the theme background remains visible without
  content bleeding into the header.
- The terminal list table uses the same sticky-header mask and remains inside a
  viewport-bounded panel. Its vertical and horizontal overflow belong to the
  `.terminal-table-scroll` area rather than the document.
- The user-facing connection columns omit `publicAddress`; the field may remain
  in the API for compatibility.
- On mobile, the back button remains in the detail-card upper-right with a 44px target and never takes a separate full-width row.
- Mobile detail tabs use a fixed three- or four-column grid with no extra
  connection-action column and no horizontal scrolling.
- Second-level monitor items use the same 12px typography and visual weight as
  the interface-monitor item.

### 4. Validation & Error Matrix

- `all` scope + IPv4 filter -> zero IPv6 badges, active family header indicator.
- `ipv4` or `ipv6` scope -> no family filter button; every rendered row matches the scope.
- Filter combination has no matches -> one empty row spans every rendered column: 14 for ordinary terminals and 15 for RouterOS self with status.
- Filter panel opens -> it overlays outside the clipping table viewport and
  does not increase document width or add a toolbar row.
- Direct enum filter opens -> all current choices are visible immediately; selection closes the panel and activates the header indicator.
- Column label click -> changes only sort state; filter-arrow click -> changes only the active filter panel.
- A filter's `全部…` option clears that filter without changing other filters
  or the active sort.
- Live detail refresh -> local filter state remains component-local and continues filtering the new connection snapshot.
- Original tuple -> source/destination cells show RouterOS `src-*` / `dst-*`; upload/download remain terminal-oriented.

### 5. Good/Base/Bad Cases

- Good: addresses render in `10.0.0.8, 10.0.0.10` order and `IP / MAC ↑` is visible initially.
- Good: an all-scope detail contains both IPv4 and IPv6 badges; selecting IPv4 removes only IPv6 rows.
- Base: clicking the protocol filter arrow opens direct options in the common
  floating panel.
- Bad: connection family tabs, global search, or global clear controls consume
  a separate row above the table.
- Bad: entering through IPv6 but filtering from the unscoped connection array leaks IPv4 rows.

### 6. Tests Required

- Build/lint/audit: production TypeScript build, oxlint, and dependency audit pass.
- Browser 375px: terminal toolbar has one 44px control row, the online-state
  header filter target is 44px, refresh value is `1000`, back target is 44px,
  and document width does not exceed viewport.
- Browser sort: first addresses demonstrate numeric ascending order including `.8` before `.10`.
- Browser all scope: both badge families appear and the family filter removes the opposite family.
- Browser IPv6 scope: all badges are IPv6 and no IP-version filter button renders.
- Browser runtime: header filters work with no console errors; no global
  connection search or clear control renders.
- Browser runtime: terminal connection details render source IP and source port as independently filterable columns.
- Browser interaction: application ascending begins with `常用协议`, descending
  begins with `未知应用`; local filter reset preserves the active sort.
- Browser geometry: desktop filter panel left equals its trigger left when space permits; mobile arrow is 44x44 and the clamped panel stays within the viewport.
- Browser geometry: unsorted filterable headers have a zero-width visual gap
  between sort and filter buttons; sticky headers and the native horizontal
  scrollbar remain reachable inside the bounded viewport.
- Browser mobile: terminal toolbar has one row; every interactive control is
  44px high; the status filter opens from the table header; all detail tabs fit
  without horizontal scrolling; no duplicate connection search/clear actions
  render.

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
<ConnectionColumnHeader label="下一跳网关" sortKey="gateway" filterKey="gateway" onSort={changeSort} onOpenFilter={openFilter} />
<div className="connection-table-viewport"><ConnectionTable /></div>
```

## Terminal scope summary layout

- Render the selected `terminalScopeSummaries` entry only in the terminal-list topbar, inside the right-side global control cluster next to system status, last-updated, refresh, and auto-refresh controls. Do not place it between the title and controls, and do not place it in the terminal filter toolbar.
- Desktop uses the topbar controls' muted 11px inline typography and tabular numerals so device/connection/rate/cumulative values do not look louder than `系统正常` or `最后更新`. Mobile uses a full-width controls row with a two-row, three-column grid.
- The six labels are device count, connection count, upload, download, active cumulative upload, and active cumulative download. Use `formatBits` for current bit/s values and `formatBytes` for active bytes.
- Unexpectedly missing summary data renders zero values; never fall back to persisted combined terminal totals.
- Verify 375px layout has no document-level overflow and the existing two-row, 44px terminal toolbar remains unchanged.

## Scenario: Panel settings forms

### 1. Scope / Trigger

- Trigger: changes to the panel-settings sidebar group, connection/collection/UI forms, maintenance actions, password visibility, or settings responsiveness.

### 2. Signatures

- Sections: `connection | collection | ui | maintenance` under the first-level `面板设置` item; user-facing `connection` label is `设备管理`.
- Connection password control: password input plus `显示 RouterOS 密码` / `隐藏 RouterOS 密码` icon button.
- Browser preference key: `rosboard:panel-preferences`.

### 3. Contracts

- Settings section navigation stays in the left sidebar and follows the existing status-monitor submenu pattern.
- Desktop device-management and UI forms use three equal columns. Collection numeric controls use four equal columns and does not include per-device interface/CIDR fields. Do not render sparse two-column setting-card rows.
- Device management is the only UI section that edits per-device RouterOS connection, `采集接口`, and terminal CIDRs. It must make the selected device scope obvious.
- `采集接口` uses picker-style checkbox cards, retaining configured interfaces that are missing from the latest live interface list.
- Terminal CIDR suggestions may be derived from the selected device's RouterOS interface addresses, but must be presented as operator-reviewed candidates instead of automatic LAN truth. Keep manual CIDR entry available.
- At widths below 768px, every settings grid becomes one column and controls are at least 44px high without document-level horizontal overflow.
- Passwords use `type=password` by default. The eye icon has a tooltip, `aria-label`, and `aria-pressed`; it may reveal the current value only after an explicit click.
- UI preferences edit a draft and persist only when `保存界面设置` is submitted.
- Maintenance export clones the settings response and masks `routerosPassword`; it never downloads the raw response object.

### 4. Validation & Error Matrix

- Missing settings response -> render loading or explicit load error, not an empty form.
- Configured settings arriving before the first dashboard response -> keep the loading view; do not flash the initialization form. Show setup only when `configured=false` or dashboard loading has failed.
- Save request failure -> keep the current draft and render the returned error near the form.
- Invalid localStorage JSON -> fall back to product defaults.
- Mobile width -> no multi-column spans remain and no page overflow appears.

### 5. Good/Base/Bad Cases

- Good: 1280px collection layout renders four interval fields only; device-specific `采集接口` and terminal CIDR controls appear under `设备管理`.
- Good: password appears as bullets, becomes text after the eye click, and returns to bullets after the next click.
- Base: an empty per-device interface/CIDR list renders empty picker/manual controls and saves as `[]` through device APIs.
- Bad: render each field as a card inside the settings panel or leave half of each desktop row empty.
- Bad: export a JSON payload containing the real RouterOS password.

### 6. Tests Required

- Frontend: oxlint and production TypeScript/Vite build pass.
- Browser desktop: measure three connection columns and four collection interval columns; verify password type toggling.
- Browser 375px: all settings labels have the form width and `scrollWidth <= clientWidth`.
- Dependency: `npm audit --audit-level=high` reports zero vulnerabilities.

### 7. Wrong vs Correct

#### Wrong

```tsx
const payload = JSON.stringify(settings)
<input value={password} />
```

#### Correct

```tsx
const payload = JSON.stringify({
  ...settings,
  connection: { ...settings.connection, routerosPassword: '********' },
})
<input type={passwordVisible ? 'text' : 'password'} value={password} />
```

## Scenario: First-run onboarding and administrator forms

### 1. Scope / Trigger

- Trigger: changes to bootstrap routing, administrator setup/login/account forms, onboarding device editing, empty-device panels, or full-reset UI.

### 2. Signatures

- Root phase component mapping: `needs_admin -> AdminSetupPage`, `needs_login -> LoginPage`, `needs_routeros -> RouterOSSetupPage`, and `ready -> PanelApp`.
- Shared device editor props include `onboarding`, `initialDeviceID`, `onSaved`, and `onRestartingAction`.
- Onboarding device payloads use `completeOnboarding` and save-only `deferRestart`.
- Administrator account update: `PUT /api/account { username, password, passwordConfirmation }`.

### 3. Contracts

- Render from `/api/bootstrap`; never infer setup completion from dashboard availability, browser storage, or RouterOS errors.
- Administrator setup and account forms are single-column and bounded to `24rem` (`384px`). Username, password, confirmation, and submit controls are equal width.
- The post-admin choice page is the only place that offers explicit RouterOS skip. The device editor does not repeat skip.
- Before a successful connection test, hide collection interface/CIDR controls. Any connection-field change clears the verification result.
- The onboarding editor keeps the device list and `+` entry visible. Save-only refreshes the list without waiting for restart, so another device can be added.
- Quick-provisioning script content uses progressive disclosure: keep it out of the default layout, provide separate copy and `查看脚本` / `隐藏脚本` controls, and announce expansion with `aria-expanded`.
- Keep the three quick-provisioning steps vertically stacked at every width. The collapsed default must fit the normal desktop viewport without making the script the visual focus.
- Each onboarding save returns to a blank new-device editor without restarting. Once at least one device is saved, a separate bottom `完成设置并进入面板` action completes onboarding and restarts once.
- Ready-phase new-device saves also defer restart. Keep a visible `应用全部设备并重启` action until the user applies the batch; existing-device edits retain their normal save-and-restart behavior.
- On mobile, onboarding controls are at least 44px high and the document has no horizontal overflow.
- Account security is one compact username/password/confirmation form. It does not request the old password; successful save returns to login. Logout is a separate session section.
- Full reset uses a visually separate solid danger action and one native confirm; it does not require typed confirmation.

### 4. Validation & Error Matrix

- Bootstrap request failure -> keep an explicit startup error, not a guessed panel/setup page.
- Password mismatch -> disable submission and preserve inputs.
- Connection test failure -> retain connection inputs and keep collection controls unavailable.
- Missing interface/CIDR -> disable onboarding save; the separate final completion action appears only after at least one device has been saved.
- Save-only success -> show saved message, refresh settings/list, keep onboarding active, and do not show restart progress.
- Completion request/restart failure -> show a recoverable inline error and keep the draft/setup phase.
- API HTTP 401 -> dispatch authentication-required, clear sensitive in-memory state through bootstrap rerouting.

### 5. Good/Base/Bad Cases

- Good: save device A, immediately add and save device B, then click the separate final completion action; both devices remain configured and only one restart occurs.
- Good: from ready-phase device management, save multiple new devices and apply the batch once.
- Base: skip RouterOS and use the full empty-panel shell with device/account/maintenance settings available.
- Bad: use one generic restart wrapper for onboarding save-only; it waits for a restart that must not occur.
- Bad: hide the device list with a `createOnly` layout or route back to the initial choice after completion.
- Bad: style completion as an unfilled toolbar button that is visually much smaller than save.

### 6. Tests Required

- Frontend: oxlint, TypeScript/Vite production build, and dependency audit.
- Browser desktop: setup/account forms remain 384px; quick-provisioning steps are vertical and the script is collapsed by default.
- Browser 375px: action targets are 44px high, the device list and `+` are visible, and document overflow is false.
- Browser runtime: no console warnings/errors; save-only shows no restart wait; the batch apply/final completion action reloads only after service recovery.
- API regression: onboarding, ready-phase creation, and quick provisioning honor explicit deferred restart; completion still schedules one restart.

### 7. Wrong vs Correct

#### Wrong

```tsx
await onRestartingAction(() => saveDevice({ completeOnboarding: false }))
```

#### Correct

```tsx
await saveDevice({ completeOnboarding: false, deferRestart: true })
await refreshDeviceList()
// Later, after all devices are saved:
await onRestartingAction(() => completeSetup())
```

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
- When metric details share a row with sparklines, prefer short two-line detail copy over a single long slash-delimited line so labels do not squeeze the chart or ellipsize.
- Mini sparklines inside metric cards should use a content-sized value column plus a flexible chart column. Set SVG `preserveAspectRatio="none"` with non-scaling strokes so the line fills the available width instead of being centered with large side gutters; keep a modest, balanced chart inset when full-width lines feel too close to the card edges.
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
- The overview has one shared `5min`, `1h`, `6h`, and `24h` control above the metric cards. It drives CPU, memory, online-terminal, active-connection, and traffic histories together; do not place a traffic-only range selector inside the chart panel.
- Metric-card primary values remain live. Sparklines, averages, and peaks consume load samples from the selected range. Ignore negative connection counts because they mark legacy rows where that metric was not collected.
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

## Scenario: Monitoring display preferences

### 1. Scope / Trigger

- Trigger: route table filters, collection interface controls, panel preferences, or application theme changes.

### 2. Contracts

- Route and routing-rule views hide disabled rows by default. A checked `隐藏已禁用` checkbox controls the filter and the toolbar reports visible, total, and disabled counts.
- Traffic interface selection is a checkbox list built from the selected device's current interfaces. Merge configured-but-unavailable names into the list and keep them selected until the user explicitly clears them.
- Save traffic interfaces as `string[]`; do not require comma-separated or newline-separated input.
- `PanelPreferences.theme` is `light` or `dark`, defaults to `light`, and is stored with the other browser-local interface preferences.
- Apply the theme through the root `data-theme` attribute and `color-scheme`. Canvas charts must observe theme changes and update axis, grid, tooltip, and text colors without requiring a reload.
- The UI settings theme radio previews immediately by applying the draft theme to the root document. It is still only persisted through `保存界面设置`; refreshing before save restores the saved theme.

### 3. Responsive And State Validation

- Interface choices use three columns on desktop and one column on mobile without document-level horizontal overflow.
- Theme choices use visible light/dark swatches, radio semantics, and a clear selected state.
- Reloading preserves the saved theme. Older preference payloads without `theme` migrate to `light`.
- Switching themes must keep form controls, topbar controls, tables, status text, and charts legible.

## Scenario: Overview metric-card composition and hover details

### 1. Scope / Trigger

- Trigger: changes to overview `MetricCard`, `MiniSparkline`, current composition bars, title legends, or Overview composition payload fields.

### 2. Signatures

- Historical point: `MetricSample { timestamp: string; value: number }`.
- Current composition item: `{ label: string; value: number }`.
- Payloads: `overview.terminalStateCounts.{online,inactive,offline}` and `overview.connectionProtocolCounts.{tcp,udp,other}`.

### 3. Contracts

- CPU, memory, online-terminal, and active-connection sparklines receive timestamps from the same `LoadSample` rows as their values. Hover selects the nearest horizontal sample and shows `HH:mm:ss` plus the metric-formatted value.
- Online-terminal and active-connection cards render a three-item color-dot legend in the existing title row, a three-segment current-composition bar, and unchanged historical average/peak footers.
- Legends explain category names only. Segment hover/focus supplies current count and one-decimal percentage; zero-value categories stay in the legend but do not create zero-width interaction targets.
- Keep the visible bar 4px high but provide a 12px pointer/focus hit area without increasing the card's total vertical allocation.
- CPU/memory retain the existing single-color percentage bars. Composition colors always have adjacent text or accessible labels; color is not the only cue.

### 4. Validation & Error Matrix

- No historical samples -> create one point from the live Overview value and `updatedAt`.
- Legacy negative connection sample -> omit it before building the connection sparkline.
- Missing composition object -> terminal fallback preserves the live online count; connection fallback treats the current total as unknown/other rather than inventing TCP/UDP.
- Composition total zero -> show an empty neutral track with `暂无构成数据` semantics.
- Pointer at a chart edge -> clamp the selected sample and flip/clamp the tooltip so it remains inside the card region.

### 5. Good/Base/Bad Cases

- Good: title legend, four-pixel bar, and average/peak footer remain distinct visual layers; 375px and 1440px layouts have no overflow.
- Base: a one-point fallback sparkline still exposes its current time and value on hover.
- Bad: add a second legend row below the bar, replace average/peak with composition counts, or instantiate four ECharts canvases for the tiny sparklines.

### 6. Tests Required

- Frontend: oxlint, TypeScript/Vite production build, and dependency audit pass.
- Browser: all four sparklines show time/value details; composition segments show count/percentage by pointer and keyboard focus; console has no warnings/errors during refresh.
- Responsive: at 375, 768, 1024, and 1440px the title legend does not wrap/overflow, all cards have equal height, and document horizontal overflow is zero.
- Theme: legend dots, bars, focus rings, and tooltips remain legible in light and dark themes.

### 7. Wrong vs Correct

#### Wrong

```tsx
<MetricCard footerLeft="TCP 165" footerRight="UDP 123" />
```

#### Correct

```tsx
<MetricCard
  composition={[{ label: 'TCP', value: counts.tcp }, { label: 'UDP', value: counts.udp }, { label: '其他', value: counts.other }]}
  footerLeft={`平均 ${average(values)}`}
  footerRight={`峰值 ${maximum(values)}`}
/>
```

## Scenario: Automatic terminal scope in device settings

### 1. Scope / Trigger

- Trigger: changes to device management, `DashboardResponse.terminalScope`, or advanced terminal-scope overrides.

### 2. Signatures

- Runtime response: `terminalScope.interfaces`, `terminalScope.prefixes`, and `terminalScope.warnings`.
- Device save request: `terminalScope { mode: 'auto', include_interfaces, exclude_interfaces, include_cidrs, exclude_cidrs }`.

### 3. Contracts

- Ordinary settings show automatic results read-only. CIDR candidates and dynamic IPv6 prefixes are not editable ordinary controls.
- `自动识别范围` and `高级覆盖设置` are separate, sibling disclosure cards inside the device form. Both are collapsed by default, and the automatic card summary always exposes WAN-line, LAN-interface, and prefix counts.
- Expanded automatic results have two top-level groups: `上网流量` and `本地终端`. The terminal group separates LAN interfaces from one compact prefix list; prefix rows use visible IPv4/IPv6 badges instead of reserving independent family cards that become sparse when one family has little data.
- Each automatic result puts its value/status and lower-priority evidence on separate lines. Evidence remains readable but must not compete visually with interface names, status, or CIDRs.
- Advanced overrides stay in the parent device form and reuse `保存设备`; do not introduce a modal, drawer, independent save, or nested disclosure. Group fields under `流量采集覆盖` and `终端范围覆盖`, with include/exclude pairs in two desktop columns and one mobile column.
- Scope fields from a partial/older response are normalized to empty arrays before rendering. Settings-device drafts must also tolerate an absent `terminalScope` object.

### 4. Validation & Error Matrix

- No scope/prefix evidence -> show a readable waiting/advanced-settings state, never throw.
- Legacy mode -> show migration notice without rewriting the legacy CIDR configuration.
- Include/exclude conflict API error -> retain draft and display the returned error.

### 5. Good/Base/Bad Cases

- Good: the collapsed automatic card shows `1 条上网线路 · 3 个 LAN 接口 · 4 个网段`; its expanded prefix list labels every row IPv4 or IPv6 without leaving an empty family card.
- Base: an empty array presents `尚未识别`.
- Bad: nest advanced overrides inside the automatic-results disclosure or render all six textareas as full-width desktop rows.

### 6. Tests Required

- Frontend lint/build pass.
- Browser verifies Device Management opens with complete, empty, and missing scope arrays; both disclosure cards are collapsed by default and no console error or overlap occurs at 375px and desktop.
- Browser verifies the automatic summary counts, the IPv4/IPv6 row badges, the two-column desktop override grid, the one-column mobile override grid, and zero document-level horizontal overflow.
- Browser verifies dynamic output does not enter editable fields and advanced values still submit through the existing device-save request.

### 7. Wrong vs Correct

#### Wrong

```tsx
<div className="interface-options">{scope.prefixes.map(renderPrefix)}</div>
```

#### Correct

```tsx
const prefixes = scope?.prefixes ?? []
<details className="settings-disclosure auto-scope-settings">
  <summary>自动识别范围{/* line, interface, and prefix counts */}</summary>
  <section aria-label="本地终端">
    {prefixes.map((prefix) => <span key={`${prefix.family}-${prefix.cidr}`}>{prefix.family} {prefix.cidr}</span>)}
  </section>
</details>
<details className="settings-disclosure advanced-scope-settings">
  <summary>高级覆盖设置</summary>
  {/* existing device-form override fields */}
</details>
```
