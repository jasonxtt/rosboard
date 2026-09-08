> Scope clarification: this audit describes the OTHER agent’s UI branch, not the new implementation worktree. The user subsequently required a fresh start from `7a7d463` on `codex/ui-compact-rebuild`. Reuse recommendations below are historical except the explicitly approved overview modules; inspect the functional baseline before implementing other areas.

# Source audit — 2026-09-07

Audited `feat/ui-ikuai-restyle` at `1049314` against functional baseline `7a7d463`. This is a source audit, not a new browser acceptance of the current implementation. The working tree had no tracked modifications before planning; unrelated untracked artifacts are excluded.

## Subsequent user decision

The user now explicitly likes the branch’s four overview header components (including their colors/charts) and quick-link module. Those are adopted as exceptions; the historical difference findings below no longer imply replacing them with the study’s header. See [the amendment](overview-reference-amendment.md). Other source findings remain applicable.

## What already changed

The branch contains token replacement, shell navigation, component styling, overview/authentication changes, chart recoloring, dark theme, wizard layout and rebuilt embedded assets. There are 34 changed files against the functional reference. Reusing working logic is preferable to resetting the branch.

No diff was found in `RoutingRuleWizard.tsx`, policy `canonical.ts`, `source.ts`, `gateway.ts`, `web/src/lib/types.ts`, or backend source under `internal/` excluding embedded UI assets. This supports selective presentation work; it does not prove all runtime behavior is regression-free.

| Area | Current source evidence | Planned treatment |
|---|---|---|
| Reference | Old PRD points to root `preview-ikuai/` | Replace authority with frozen compact study; old research is background only |
| Navigation | `App.tsx` uses primary/secondary navigation; CSS assigns 140 px to each | Retain navigation state/actions, change proportions to 224 px total; place switcher above both columns |
| Search | Primary nav search changes fleet query on fleet and terminal query elsewhere | Place existing searches on fleet/terminal toolbars so scope is visible; preserve state and filtering |
| Switcher | Primary-column styling hides address in trigger | Compact trigger with full name accessible and address/status available in menu; do not discard data |
| Tokens | Primary/chart upload use bright `#4794EB`; title token is 24 px | Use approved muted palette, 18 px page title and sans metrics with tabular digits |
| Overview | Identity plus three separate metric cards; terminal purple, resources blue, connections orange | Superseded by user amendment: copy these four cards, their existing colors/charts and internal layouts |
| Overview details | WAN aggregate, nine quick links, traffic/resource charts, interface table, status and alerts exist | Preserve their information/actions; rearrange using study's left information/right monitoring hierarchy |
| Empty state | `EmptyDevicePanel` retains another shell structure | Bring into same visual system while preserving setup/maintenance routes |
| Wizard | State machine unchanged; step/preview CSS already altered | Restyle existing components, retain jumping/editing/locking and application rules |
| Plan preview | Six metadata fields moved into details; blockers/acknowledgements retained | Reuse metadata disclosure, keep all blocking and required confirmation information visible |
| Source selector | MAC/IP binding helper text removed in `Selectors.tsx` | Restore equivalent explanation without changing controls or binding semantics |
| List pages | Existing row actions retained; tags, row counts and Chinese labels added | Reuse improvements; preserve actual search/tabs and all status variants |
| Development proxy | `ROSBOARD_DEV_PROXY` with localhost default | Retain; never revert to a production-targeting default for visual work |

## Important interpretation limits

The user reports dissatisfaction; the source differences above explain measurable divergence, not an invented diagnosis of every preference. iKuai itself and the accepted adaptation are different references: earlier measured iKuai blue is not a reason to override the user's softer approved study. Dark colors are a Rosboard adaptation, not an observed iKuai specification.

The study contains illustrative terminal rows, WAN selection and form actions. Those are not proof of existing capabilities. Conversely, absent prototype pages, alerts, quick links, advanced fields and helper text are not permission to delete them. Keep the baseline's data semantics, and document the final mapping during implementation.

Shared frontend guidelines contain historical layout assumptions (including earlier light-only/four-card conventions). Use the task's approved appearance for these conflicts while retaining shared state, type and quality requirements. Do not rewrite shared specifications as if the new implementation had already passed acceptance.
