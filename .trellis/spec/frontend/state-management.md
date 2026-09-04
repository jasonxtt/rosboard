# State Management

> How state is managed in this project.

## Overview

The application uses React built-in state and props. There is no Redux,
Zustand, MobX, or query-cache dependency. State ownership follows the smallest
component or feature boundary that can correctly reset it when the selected
device changes.

## State Categories

- **Shell state**: `App.tsx` owns active view, selected device, theme, and
  shared refresh preferences. Non-sensitive display preferences use the
  existing `localStorage` keys.
- **Server state**: feature hooks fetch and hold API results, loading, errors,
  cursors, and explicit reload behavior. There is no normalized global cache.
- **Draft state**: forms and the policy wizard keep editable values locally;
  `drafts.ts` contains pure/defaulting helpers for converting API models to
  drafts and validating them.
- **Transient browser state**: a value such as pending RouterOS cleanup may
  use the existing `sessionStorage` flow when it must survive a navigation but
  must not become a long-lived preference.
- **Derived state**: use `useMemo` or a small helper for values derived from
  current props/state; do not persist a second copy of a derivable value.

## When to Use Shared State

Promote state to `App.tsx` only when multiple top-level views need the same
value or navigation must preserve it. Keep a policy source, egress draft,
wizard step, filter, preview, or pagination state inside the policy feature.
Pass values and callbacks through props before introducing a new global store.

Every device-scoped view must reset or reload when `deviceID` changes. Do not
reuse a draft, cursor, preview, or job selection from another device.

## Server State

Feature API modules fetch and parse data; hooks decide when to load and when to
poll. A successful response clears the hook error, while a failed reload keeps
the current draft or last usable view where the feature's UX requires it.
Explicit refresh callbacks should call the same loader as the initial effect,
not duplicate its parsing logic.

## Persistent and Sensitive State

Only non-sensitive UI preferences may go to `localStorage`. Passwords,
verification secrets, raw RouterOS credentials, and API authorization material
must remain in short-lived component/request state and be cleared after use.
Do not persist server payloads merely to avoid a reload.

## Examples

- [Shell preference state](../../../web/src/App.tsx) validates local storage
  values and falls back to product defaults.
- [Policy drafts](../../../web/src/features/policy-routing/drafts.ts) create
  defaults and copy API models into editable feature-local values.
- [Feature server state](../../../web/src/features/policy-routing/hooks.ts)
  resets data by device/cache key and keeps loading/error state explicit.

## Common Mistakes

- Keeping a policy draft in the shell makes device switching able to submit
  data for the previous router.
- Copying fetched data into multiple component states creates stale views and
  conflicting reload behavior.
- Saving a password or raw API response in browser storage violates the
  project security boundary.
- Mutating a state object in place without producing the expected new React
  value can leave the UI unchanged; use the existing draft-copy helpers and
  spread/update patterns at state boundaries.
