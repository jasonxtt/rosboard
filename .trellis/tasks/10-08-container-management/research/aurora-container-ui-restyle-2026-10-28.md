# Aurora container page restyle — 2026-10-28

## Trigger

User reviewed the previous Aurora container UI (codex/container-aurora-ui tip
61f052ab, unpushed) and rejected the visual result: form controls in the
create/edit dialog looked native and inconsistent with the policy-routing
wizard, and the container list did not distribute column widths (the trailing
操作 column left dead space at the card's right edge). Compact UI explicitly
out of scope for this round.

## Root cause found

The Aurora `Dialog` adapter renders `ui/Modal`, which portals to
`document.body` — outside the page's `.ct` root. Every `.ct input`,
`.ct select`, `.ct-field …` rule in `ContainersPage.css` therefore never
matched dialog content: the editor's inputs/selects rendered as unstyled
native controls. Buttons looked different again because the adapter maps them
to shell `.btn` variants while a blanket `.ct button` rule re-skinned them.

## Changes (Aurora-only; shared feature TSX and Compact untouched)

- `web/src/pages/ContainerPresentation.tsx`: the Dialog adapter now wraps
  children in `<div className="ct …">` and forwards the dialog `className`
  (previously dropped, which also made `ct-compose-dialog` dead in Aurora).
  One wrapper restores the full `.ct` selector scope inside every portalled
  container dialog; no `ui/Modal` change was needed.
- `web/src/pages/ContainersPage.css`: full rewrite on shell tokens.
  - Controls mirror §7 `.input/.select/.textarea` (glass bg, `--r-md`, 36px,
    accent ring focus, select chevron, file-selector button skin).
  - Buttons: blanket skin removed; adapter-provided `.btn` variants show
    through. Only specials are styled: sort/name text buttons, 30px row icons,
    flat rows inside the inline more-menu, dashed full-width 添加 rows,
    full-width disclosure-style 高级设置 toggles.
  - Editor sections become flat `pol-section`-like boxes (no shadow/blur,
    `--r-md`, 16px padding); hints/errors match `.pol-hint`/`.pol-field-error`;
    notices match `.pol-notice` tones; mount/env repeats become bordered
    sub-cards.
  - Resolved-defaults footer is an opaque sticky bar (bottom 20px) so submit
    stays reachable in the long form.
  - Table uses `table-layout: fixed`: 状态 128 / 网络 190 / 资源 124 /
    操作 156, 名称 takes the remainder; 资源 and 操作 right-aligned so actions
    end flush at the card edge; th/td styling defers to shell `.table`.
    ≤767px hides `.col-hide-sm` (网络/资源) like the policy tables.
- Directory tree/picker behaviour and markup untouched (pure token polish).

## Validation

- `npm --prefix web run lint` — pass (only pre-existing fasttrack warnings).
- `npm --prefix web test` — 110/110 pass (structure-sensitive container tests
  unchanged: 7 sections, field labels, aria labels, tree classes, 🗑 glyph).
- `npm --prefix web build` + `check:ui-build` — pass; both CSS graphs isolated,
  container CSS chunk regenerated under `internal/ui/dist`.
- `go build ./...` — pass.

## Follow-up 2026-10-28: flat row actions with icon bubbles

User found the 更多 overflow menu messy and translucent. Removed the
expandable menu (and `ContainerMoreActions`): restart/update/delete/adopt are
now direct icon buttons in the last column alongside start/log/edit, widening
操作 to 228px (152px with wrapping at ≤767px). Every row icon and the
editor's mount/env delete buttons now carry `data-tip` instead of `title`;
each shell's stylesheet renders an instant hover/focus bubble (Aurora
glass-float tokens, Compact surface tokens) with no native-tooltip doubling.
Destructive actions still pass through the confirmation dialog with focus
restored to the triggering icon; the dual-adapter regression test was updated
accordingly and the dual-ui spec bullet now documents the flat-icon pattern.

## Pending

User-led visual acceptance (quality-guidelines): preview via
`ROSBOARD_CONTAINER_PREVIEW=1 go test ./internal/api -run '^TestContainerPreview$' -v -timeout 0`
plus `ROSBOARD_DEV_PROXY=http://127.0.0.1:8099 npm --prefix web run dev`,
then `/container-preview.html?ui=aurora` at 1440/390px, light+dark: list
column alignment, create/edit dialog controls, directory picker, sticky
resolution footer, mobile column hiding. No production deployment, merge or
task completion before explicit approval.
