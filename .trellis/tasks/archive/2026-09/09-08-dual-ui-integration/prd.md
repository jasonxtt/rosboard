# Policy backend and dual UI integration

## Authorization
User explicitly requested end-to-end integration of PR #6 and #7 plus feat/policy-access-rebuild into main, removal of the previous main UI, and said to start after the execution/delivery sequence was described. Task creation and implementation are authorized. Production manual acceptance is still required before new program commits and main integration, per the user-supplied AGENTS.md (stricter than the branch-local checkpoint-commit rule).

## Requirements
- Preserve the rebuilt backend and functionality from feat/policy-access-rebuild.
- Preserve complete Compact (PR #6) and Aurora (PR #7) interfaces, with no third legacy main interface.
- Both Panel Settings / Interface Settings screens provide an accessible UI style selector and explicit save-and-switch action.
- Full UI switching includes shells, pages, dialogs, charts, login and setup; saved preference survives reload in this browser.
- Share server, authentication and device data; changing UI must never mutate RouterOS or business data.
- Preserve selected device and compatible theme/refresh/landing/family preferences across switches. Warn that switching reloads the page and unsaved edits should be saved first.
- Preserve PR-specific terminal naming and policy recovery/target-list fixes under one API contract.
- Automated checks, isolated test-machine deployment, production NAS backup and deployment verification precede user acceptance and final main merge.

## Acceptance
- Bidirectional switch works, including reload and unavailable/corrupt storage; only one UI CSS/runtime is loaded.
- Both UIs build and support all original pages and light/dark modes.
- Go build/test/vet, frontend lint/build, focused integration tests, dependency audit and embedded asset verification pass.
- Test data/config are isolated from production and no production credentials/data are copied to test.
- Production remains awaiting manual acceptance until the user explicitly approves; do not finalize commits/merge/archive before approval.

## Default UI amendment (2026-09-08)
First visits and missing/invalid UI preferences default to Aurora. Preserve explicit URL overrides and valid saved Compact/Aurora selections.
