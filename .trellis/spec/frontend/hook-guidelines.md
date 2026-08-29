# Hook Guidelines

> How hooks are used in this project.

## Overview

Hooks are small, feature-local adapters around React state and browser/server
lifecycle. The project does not use a query/cache library; hooks explicitly
own loading, error, cancellation, polling, and pagination behavior.

## Custom Hook Patterns

- Name custom hooks with `use...` and keep them in a feature `hooks.ts` when
  they are only used by that feature.
- Store server results as `T | null`, with separate loading and error state so
  the UI can distinguish initial loading, empty data, and failed reads.
- Use `useEffect` for fetch/lifecycle work, `useCallback` for exposed reload or
  event callbacks, and `useRef` for mutable timer/fetcher state that should
  not trigger a render.
- Create an `AbortController` for a request that belongs to an effect and
  abort it in the cleanup function. Also guard state updates with a
  cancellation flag when a request can complete after unmount.

```tsx
useEffect(() => {
  let cancelled = false
  const controller = new AbortController()
  void fetchOverview(deviceID, controller.signal).then((data) => {
    if (!cancelled) setOverview(data)
  })
  return () => { cancelled = true; controller.abort() }
}, [deviceID, refreshNonce])
```

## Data Fetching and Polling

- Reset feature data when its device ID or enabling condition changes.
- Use the shared visibility/refresh behavior: initial loads still happen when
  periodic refresh is stopped, hidden pages should not keep active polling,
  and active policy jobs may poll more frequently than idle overview data.
- Keep one timer per hook and clear it during cleanup. Do not add a second
  chart/table timer when the shared refresh setting already controls polling.
- For cursor endpoints, use the generic `useCursorPagination<TPage>` pattern:
  keep loaded pages and the current page index locally, pass the returned
  cursor to the next request, and reset pages when `cacheKey` changes.

## Naming Conventions

- Use `useNoun` for a data hook (`usePolicyOverview`, `useSourceRules`) and
  `reload` for an explicit refresh callback.
- Keep fetcher parameters explicit, normally `{ deviceID, ... }`, and keep
  `AbortSignal` optional at the API boundary.
- Hook return values use named fields such as `{ data, loading, error, reload }`
  rather than positional tuples.

## Common Mistakes

- Updating state after an aborted request or unmounted component causes stale
  device data to appear in the next view.
- Omitting device ID, refresh nonce, or an enable flag from an effect's
  dependency list can leave a hook reading the previous router.
- Starting a timer before the first load resolves can create overlapping
  requests; schedule the next poll from the completed load path.
- Reusing a cursor or page array after changing device/source without changing
  the cache key leaks rows from the previous scope.

## Examples

- [Overview polling](../../../web/src/features/policy-routing/hooks.ts)
  handles cancellation, active-job cadence, visibility, and manual reload.
- [Cursor pagination](../../../web/src/features/policy-routing/hooks.ts)
  is shared by source rules and audit entries through a typed extractor.
