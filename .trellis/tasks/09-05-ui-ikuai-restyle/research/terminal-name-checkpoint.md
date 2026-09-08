# Header search and inline terminal naming — 2026-09-08

User-authorized follow-up to Draft PR #6, on `codex/ui-compact-rebuild`.

## Changes

- Fleet and terminal search move into the header immediately before Theme, with the same compact control height and responsive width. Search scope is unchanged except removed terminal remarks no longer participate.
- Clicking any ordinary terminal row cell opens that terminal's detail. Existing detail actions remain keyboard-accessible; the operations Edit button is removed.
- A small pencil appears next to the name on row hover/focus (always available for touch). Clicking it opens an anchored input/cancel/confirm bubble and does not navigate. The bubble normally opens below the name and flips above near the viewport bottom. Escape/cancel/outside interaction closes an idle editor; failed saves retain the draft, and polling does not overwrite it.
- Remove terminal remarks from list, detail, editor, sorting/search, public payload, service projection and metadata update path. The legacy database column is inert for existing migration/import compatibility, with no active read/write feature. No destructive database migration or conversion of remarks to names.
- Persist the existing local custom name by device ID + terminal ID; discovered name, MAC, addresses and policy identity stay unchanged. Clearing restores the automatic name. No RouterOS call is introduced.
- Public iKuai documentation found during research describes older remark/name management but does not verify the exact 4.0 hover bubble; this implementation follows the user's explicit interaction description, without claiming a pixel-verified 4.0 copy.

## Verification and preview

Frontend lint/build and Go build pass. API/store/service/subject/policyv2/accesscontrol tests pass. API regression checks cover missing/null/removed fields and the 100-code-point boundary. Monitor regression checks retain ID/address/automatic name and restore automatic naming without a RouterOS client; existing store tests verify device isolation and persistence.

A temporary React DOM test using happy-dom (no browser operation) passes row navigation, pencil propagation, polling draft retention, scoped save with stable ID/MAC, another device remaining unchanged, cancel, failed-save draft retention/retry, and clearing to automatic naming. Layout remains user-reviewed.

Actual built app: http://10.0.0.86:8792/ . The isolated preview now allows **ephemeral display-name edits only**, keyed per simulated device, so the full bubble interaction can be tried. Restarting its fixture API resets these names. Other business writes remain rejected. No production credentials, deployment or merge.

Manual review: at desktop width inspect both header searches immediately left of Theme; hover a terminal row and rename via the pencil. Click rate/status/empty cell areas to open detail. At 390 px verify search/Theme/refresh fit, the pencil is available by touch, the bubble fits, and horizontal table scrolling remains usable. Check light/dark, Escape, cancel and empty-name reset.
