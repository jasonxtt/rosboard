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

Bubble follow-up (same day, user report): a disabled icon's bubble was
occluded by the sticky table header. Root cause: any opacity/filter on the
button makes the button a stacking context, trapping its bubble ::after under
the z-index 2 header cells; the first attempt traded opacity for grayscale
filter, which traps identically. Final fix: the disabled button itself stays
free of opacity/filter (faint ink color only) and only its glyph span is
dimmed, so the bubble keeps its z-index 60 above the header; the first data
row (when it is not the only row) also opens its bubbles downward. Both shell
stylesheets carry the fix.

## Follow-up 2026-10-28 (second round): declutter pass per user review

- 容器管理说明 disclosure removed from the native-writes page head.
- The job progress card renders only while a task is queued/running/unknown
  or needs recovery (failed native / cleanupPending); a finished job no
  longer lingers under 操作进度 — its result stays in the notice/error line.
- Editor: pair-field inputs now level (`align-content: start` on `.ct-field`;
  grid stretch was distributing free space into label/input tracks and
  pushing the second input down); the directory combobox is exactly 36px
  like every other control; the 最终目录 echo hint and the mount-placement
  hint paragraph removed on user request; env name/value fields share equal
  width and single-line height (textarea rows=1, min-height 36px, still
  multiline-capable); the three startup toggles share one compact wrapping
  row (Aurora only — Compact keeps its stacked layout, zero CSS change).
- 110/110 tests updated only where the removed help disclosure was asserted.

## Follow-up 2026-10-28 (third round): collapsible sections + masonry

- 资源限制 / 健康检查 collapse to a header bar by default (button toggle,
  no `<details>` — the no-details editor constraint stands). They auto-open
  when the draft holds explicit values, on Compose import of resource
  fields, and when validation flags their fields; a collapsed section with
  non-default values shows a 已配置 marker. Collapsing never clears values.
- Aurora form layout switched from a paired 2-col grid to CSS multi-column
  masonry (`column-count: 2`, `break-inside: avoid`): short sections no
  longer leave blank pockets beside taller partners (the 01/04 gaps the
  user reported). DOM order is unchanged, so keyboard tab order follows the
  visual column-major flow; mobile stays single column. Compact keeps its
  paired grid and gains the same collapse behavior with its native look.
- Tests: expand-before-interact steps added where sections start collapsed;
  collapsed-by-default assertions added for new drafts; the resource test
  asserts auto-expanded state when editing a container with limits.

## Follow-up 2026-10-28 (fourth round): close editor on accepted submission

Per user report, the create/edit dialog lingered with a disabled form until
the background job finished, hiding the page-level job card behind the modal.
`run()` now closes the editor as soon as a validated create/edit task is
accepted by the API; progress and any later failure surface through the job
card and the page error line. Submission failures (e.g. 409
ssh_key_required) still keep the dialog open with the error in the editor
footer; the unmanaged-adopt guard and the read-only validation preview keep
the draft open as before.

## Pending

User-led visual acceptance (quality-guidelines): preview via
`ROSBOARD_CONTAINER_PREVIEW=1 go test ./internal/api -run '^TestContainerPreview$' -v -timeout 0`
plus `ROSBOARD_DEV_PROXY=http://127.0.0.1:8099 npm --prefix web run dev`,
then `/container-preview.html?ui=aurora` at 1440/390px, light+dark: list
column alignment, create/edit dialog controls, directory picker, sticky
resolution footer, mobile column hiding. No production deployment, merge or
task completion before explicit approval.
