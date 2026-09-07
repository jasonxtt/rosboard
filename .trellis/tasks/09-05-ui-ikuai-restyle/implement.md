# Execution plan — compact rollout awaiting review

**User authorization on 2026-09-07:** the overview direction is approved, with the explicit amendments recorded in `research/rollout-checkpoint.md`; extend the same styling to remaining pages. Implementation and representative deterministic checks are complete. Full visual and real-device workflow acceptance remain pending. Historical M1–M10 claims are not evidence for this revision.

## Phase 1 — establish the comparison and shared appearance

- [x] After user authorization, refresh HEAD/status in the independent worktree. Verify application code starts at functional baseline `7a7d463`, not the other UI branch; preserve unrelated working files.
- [ ] Complete the feature checklist against real handlers and conditional branches; capture baseline behavior for affected flows without production mutations.
- [x] Apply semantic tokens, compact shell, device switcher and correctly scoped search; include no-device shell and responsive states.
- [x] Rebuild overview using `design.md` and `research/overview-reference-amendment.md`: preserve branch-reference header cards/colors/charts and quick links; order the left rail interface information → quick links → study device information.
- [ ] Present the actual shell/overview in light/dark and narrow layouts beside the frozen study plus the pinned overview exceptions. Do not report the unchanged study as a preview of the amended overview. Resolve user visual feedback before spreading the treatment to all pages.

## Phase 2 — monitoring and settings

- [x] Style fleet, interfaces/detail, terminals/detail/edit, traffic, services and system pages without losing filters, sorting, ranges or charts.
- [x] Style recognition and all five settings groups, including advanced fields, archived devices, pending/error/danger states.
- [x] Style initialization, login, device onboarding and general dialogs.
- [ ] Verify each relevant checklist row against real behavior and record evidence.

## Phase 3 — policy and access

- [x] Style target library, routing list and access list/editors using the same controls and tables.
- [x] Style all wizard steps and plan states; restore source binding explanation.
- [ ] Verify create/edit/toggle/delete, valid/invalid plans, acknowledgement requirements, pending jobs, cancellation and error recovery on isolated test data.
- [ ] Reconcile every baseline control and information field with its new location. Any proposed deletion waits for explicit user agreement.

## Phase 4 — verification and review

- [x] Run `npm --prefix web run lint`, `npm --prefix web run build`, and `go build ./...`; run applicable existing tests for any changed behavior. Do not introduce tests that merely assert CSS constants.
- [x] Inspect generated embedded assets and source together. Avoid blanket checkout/clean commands; use isolated build workspaces if build churn conflicts with other work.
- [ ] Check representative 1440×900, 1920×1080, 1024-wide, 768-wide and 390-wide layouts, long Chinese names/IPs, large lists, empty/error/loading, light/dark and keyboard navigation. Runtime visual review follows the user's authorized preview scope; obtain user acceptance of the real UI.
- [ ] Complete a feature mapping with outcome/evidence for every checklist ID. Build success or a historical test count is not functional or visual acceptance.
- [ ] At meaningful checkpoints inspect exact staged paths/diff for secrets and unrelated changes, commit and push `codex/ui-compact-rebuild` and its separate Draft PR. Do not merge, mark accepted or archive automatically.

## Delivery and rollback

No deployment is part of the current implementation request. When later authorized, test-machine validation uses isolated configuration/data and no production credentials. Production deployment must follow `AGENTS.md`: checks, timestamped NAS backup with retention, service/health/API/embedded-asset verification, then manual user approval. Root review/acceptance is required before merging or completion.

Correct presentation in small coherent commits so a faulty slice can be reverted independently after checking later dependencies. Do not reset the branch to the functional reference, force-push, discard other agents' files or remove the frozen preview. Check the baseline proxy and use a local/test target; the other UI branch’s safe proxy is a selective reference only.
