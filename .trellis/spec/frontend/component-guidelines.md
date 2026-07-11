# Component Guidelines

## Terminal monitor filter controls

The terminal monitor uses component-local state for table presentation. Its initial state is the product default, while server data remains unchanged.

- Default `stateFilter` to `online` so the first list shows only currently online terminals.
- Keep the explicit status select for precise filtering.
- The nearby inactive-device toggle maps `online -> all -> online`; its label and `aria-pressed` state must follow the current filter.
- Any filter change resets pagination to page 1.

```tsx
const [stateFilter, setStateFilter] = useState('online')
const showingInactive = stateFilter !== 'online'

<button
  type="button"
  aria-pressed={showingInactive}
  onClick={() => setStateFilter(showingInactive ? 'online' : 'all')}
>
  {showingInactive ? '隐藏离线设备' : '显示离线设备'}
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
