# Component Guidelines

## Terminal monitor filter controls

The terminal monitor uses component-local state for table presentation. Its initial state is the product default, while server data remains unchanged.

- Default `stateFilter` to `online` so the first list shows only currently online terminals.
- Keep the explicit status select for precise filtering.
- The nearby non-online-device toggle maps `online -> all -> online`; its label and `aria-pressed` state must follow the current filter. It covers both `inactive` and `offline` states.
- Any filter change resets pagination to page 1.

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
