# Quality Guidelines

> Code quality standards for frontend development.

---

## Overview

Frontend changes are verified with deterministic checks first: TypeScript/Vite
build, oxlint, targeted unit or API tests where available, and source inspection
for cross-layer payload contracts. Visual acceptance is user-led unless the user
explicitly asks Codex to operate a browser.

---

## Forbidden Patterns

### Browser-driven visual acceptance by default

Do not open Chrome, the in-app browser, or browser automation to perform final
visual verification by default. Browser visual checks can accidentally validate
the wrong local state, depend on cached sessions, or make UI approval feel more
authoritative than the user's real inspection.

If visual verification is needed, provide the user with:

- the exact URL or deployed instance to open;
- the viewport(s) or device width(s) to inspect;
- the interaction steps to perform;
- the expected visible result and any known risk areas.

Only use Chrome/in-app browser for visual checks when the user explicitly asks
for it in that turn, or when a higher-priority project acceptance gate requires
browser evidence and the user has approved that route.

---

## Required Patterns

- Prefer `npm --prefix web run lint` and `npm --prefix web run build` for
  frontend correctness before handing off visual review.
- For UI changes with layout risk, include a concise manual QA checklist in the
  final handoff instead of silently performing Chrome-based visual approval.
- Treat `max-width` and `max-device-width` as different matching contracts.
  Adding a `max-device-width` fallback can activate a narrow layout in a wide
  desktop viewport when the physical display still satisfies the device-width
  query. Do not add, remove, or normalize that fallback under a zero-visual-
  change requirement without computed-style evidence at desktop and mobile
  widths; defer the cleanup when either direction changes the matched set.

---

## Testing Requirements

- Run the existing frontend lint/build commands for frontend source changes.
- When a change affects a user-visible workflow, describe the manual visual
  verification steps the user should run, including desktop and mobile widths
  when relevant.

---

## Code Review Checklist

- Did the implementation avoid using Chrome/in-app browser as the default visual
  approval mechanism?
- If visual validation is needed, did the handoff tell the user exactly how to
  inspect it themselves?

---

## Embedded dist and visual-verification workflow (learned 2026-09, ui-ikuai-restyle)

- `npm run build` writes to the **git-tracked** `internal/ui/dist` (Go embed
  source). During iteration, restore it after each verification build
  (`git checkout -- internal/ui/dist && git clean -fd internal/ui/dist`) to
  keep the tree clean; commit the rebuilt dist together with the feature work
  that requires it, as the repo convention tracks built assets.
- The vite dev proxy target is configurable via `ROSBOARD_DEV_PROXY`
  (default `http://127.0.0.1:8080`). Point dev tooling at a scratch instance,
  never at production.
- For flows the backend blocks in a dev environment (e.g. policy plan
  generation under a read-only RouterOS account), verify rendering with
  playwright `page.route()` intercepting the API and returning a
  schema-conformant response (derive the schema from the feature's API parser,
  e.g. `features/policy/canonical.ts`). Keep the mock in the test script only —
  never in product code.
- A running binary serves the UI from its embedded dist, not from `web/src`.
  After UI changes, rebuild the binary and restart the instance before asking
  for visual acceptance; vite-dev screenshots and the served binary can
  otherwise silently diverge.
