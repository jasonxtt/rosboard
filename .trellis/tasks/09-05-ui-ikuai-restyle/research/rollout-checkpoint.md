# Compact rollout checkpoint — 2026-09-07

Branch: `codex/ui-compact-rebuild`; Draft PR #6. The user approved the overview direction and authorized these amendments plus rollout. This is an implementation checkpoint, not final acceptance; no production deployment or merge.

## Implementation and preservation

- Overview: remove the system-status block as expressly requested; storage percentage and freshness move to device information with their existing status thresholds. Rename the summary WAN信息. Mobile order after the four unchanged header cards: real-time traffic, shortcuts, WAN information, CPU, memory, interface status, device information, current alerts.
- Monitoring and settings: apply compact table typography, rectangular buttons, form rows, dialogs, settings groups and authentication/onboarding controls through their existing classes. Mobile interface/terminal tables retain every column in horizontal scrollers.
- Policy and access: apply the frozen study's label/control layout, bordered rectangular choice cards, compact selection controls, numbered wizard steps and full-width modal sections. Restore the explanation of automatic MAC binding versus fixed addresses. Preserve locked/stale steps, hints/errors, advanced fields, acknowledgement and blocker controls.
- Business-source diff is limited to the requested overview changes, a field wrapper, wizard number markup and explanatory text. Routing/access state machines, API payloads, validators, callbacks, sorting/filtering, device editors and backend sources are unchanged. Original preview files and pinned overview modules are preserved; no whole-branch import.
- Feature inventory preservation is based on source inspection and representative interaction checks. Exhaustive real-device create/edit/toggle/delete, pending jobs and every conditional capability branch are still unverified; no claim of full functional acceptance.

## Verification

- Frontend lint, TypeScript/Vite build and `go build ./...` pass; embedded assets rebuilt together with source.
- `go test ./internal/api ./internal/policyv2 ./internal/subject ./internal/accesscontrol` passes. Dependency audit reports no vulnerabilities.
- Temporary React interaction harness outside the repository passes: navigation and overview content, terminal search, source binding selection, required-name/subject wizard gate and advancing, plan required acknowledgements/blockers/write-error recovery, collection settings save payload, login/setup password constraints. This is deterministic component evidence, not browser layout acceptance.
- HTTP checks cover the actual built page/assets and isolated fixture routes. The fixture includes terminal/target/routing/access examples and a synthetic plan; it refuses business writes and has no RouterOS connection.
- The frozen study's four file hashes match its recorded values. No dependency manifests or backend files changed.

## Preview and manual review

Open **http://10.0.0.86:8792/** and refresh to load the new build. This is the actual React app with synthetic data. The API runs only on loopback 18792; startup commands and logs remain those in the phase-1 checkpoint. Simulated plan preview is available; apply/save actions deliberately return a read-only error. Unsupported fixture APIs report that limitation explicitly.

1. At 390 px check the requested overview order and scroll all table columns; at 768/1024 px check settings/editor wrapping and navigation; at 1440/1920 px compare button shape, spacing and form alignment with the frozen study.
2. Switch light/dark. Inspect monitoring tabs, target library, access controls and all settings groups; open their dialogs and advanced sections.
3. In policy routing choose 新增策略路由. Inspect the source choice cards, automatic/fixed-address explanation, target and egress steps, validation messages and synthetic plan acknowledgements. Check keyboard focus, Escape and scrolling.

Per frontend guidelines, no browser was operated to claim visual approval. User review and integrated business acceptance remain open; keep the task in progress and PR Draft.
