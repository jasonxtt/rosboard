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
