# Directory Structure

> How frontend code is organized in this project.

## Overview

The frontend is a Vite React app under `web/src`. The shell and cross-feature
state are in `App.tsx`; reusable UI and formatting are in shared directories;
feature-specific pages, API adapters, hooks, types, and draft helpers are
co-located.

## Directory Layout

```text
web/src/
├── App.tsx                         application shell and navigation
├── main.tsx                        React entrypoint
├── index.css                       global styles and design tokens
├── assets/                         images and bundled fonts
├── components/                     shared UI components
├── lib/                            shared types, formatters, theme helpers
└── features/
    └── policy-routing/             feature page, API, hooks, drafts, types
        ├── *.tsx
        ├── *.ts
        └── mock/mockApi.ts
```

The production build is emitted by Vite and copied/embedded under
`internal/ui/dist`; do not hand-edit generated assets.

## Module Organization

- Put application-wide navigation, selected-device handling, theme, and
  refresh preferences in `App.tsx`.
- Put reusable visual components in `web/src/components/` and reusable data
  types/formatters in `web/src/lib/`.
- Put a feature page and its supporting contracts together under
  `web/src/features/<feature>/`. For example, policy routing keeps
  `api.ts`, `hooks.ts`, `types.ts`, `drafts.ts`, pages, and mock data together.
- Keep fetch/parsing code in a feature `api.ts`; do not fetch directly from a
  presentational table or form.
- Keep CSS in the existing global/style system unless a feature-specific rule
  is clearly scoped and needed.

## Naming Conventions

- React components and component files use PascalCase, such as
  `PolicyWizard.tsx` and `RealtimeTrafficChart.tsx`.
- Hooks, API adapters, formatters, and draft helpers use camelCase names in
  `.ts` files; hooks start with `use`.
- Use `.tsx` for JSX and `.ts` for data, types, API, and utility modules.
- Use `type`-only imports for types (`import type { ... }`).
- Keep feature names and API concepts consistent with the backend contract;
  do not create a second name for `deviceId`, source, egress, or rule kind.

## Examples

- [Application shell](../../../web/src/App.tsx) owns navigation and shared
  preferences while lazy-loading the policy feature.
- [Policy feature layout](../../../web/src/features/policy-routing/) keeps
  pages, API parsing, hooks, drafts, types, and mocks together.
- [Shared formatting](../../../web/src/lib/format.ts) contains display
  formatting used across pages rather than duplicating it in components.

## Common Mistakes

- Adding a feature API call directly in `App.tsx` or a table component makes
  device scoping and response parsing inconsistent.
- Hand-editing `internal/ui/dist` creates a build that cannot be reproduced
  from `web/src`.
- Placing a feature-only type in the global library increases coupling and
  encourages unrelated components to depend on it.
