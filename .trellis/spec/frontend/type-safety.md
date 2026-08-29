# Type Safety

> Type safety patterns in this project.

## Overview

The frontend uses TypeScript with Vite's bundler resolution and React's JSX
transform. `web/tsconfig.app.json` enables `noEmit`, `noUnusedLocals`,
`noUnusedParameters`, `noFallthroughCasesInSwitch`, and
`erasableSyntaxOnly`; a production build is therefore part of type checking.

Compile-time types do not validate JSON received from a RouterOS-backed API.
Treat network data as `unknown` until the feature API adapter parses it.

## Type Organization

- Shared monitoring/dashboard types live in `web/src/lib/types.ts`.
- Feature contracts live beside the feature, for example
  `web/src/features/policy-routing/types.ts`.
- Keep component-only prop types near the component when they are not part of
  an API contract.
- Use `import type` for type-only imports and preserve the backend's field
  names through one adapter rather than repeated casts in JSX.

## Validation at Boundaries

There is no schema-validation dependency in `web/package.json`. API adapters
use small runtime guards such as `safeObject`, `safeArray`, `safeString`,
`safeNumber`, and `safeBoolean`, then construct the feature type. Failures in
the HTTP layer become `PolicyApiError` with status/code/details for the UI.

```ts
function parseSource(value: unknown): PolicySource {
  const o = safeObject(value)
  return {
    id: safeString(o.id ?? o.ID),
    kind: safeString(o.kind ?? o.Kind) || 'domain',
    enabled: safeBoolean(o.enabled ?? o.Enabled),
    versions: safeArray<unknown>(o.versions ?? o.Versions).map(parseSourceVersion),
  }
}
```

Validate user-editable values in the draft/helper layer before submitting;
runtime validation must also remain at the backend API boundary.

## Common Patterns

- Use string-literal unions for finite UI modes, tabs, families, and kinds.
- Use generic hooks for repeated page behavior, as with
  `useCursorPagination<TPage>`.
- Use type predicates when filtering `unknown[]` into a typed array.
- Prefer a discriminated field and explicit narrowing over optional fields
  whose meaning depends on an undocumented convention.

## Forbidden Patterns

- Do not use `any` to silence a response or compiler error.
- Do not cast an entire network response directly to a feature type without
  checking its object/array members.
- Do not duplicate the same backend-to-frontend field mapping in multiple
  components.
- Do not add a runtime schema library for a single field when the existing
  adapter guard is sufficient; if a contract becomes broad or shared, document
  the decision first.

## Examples

- [Shared TypeScript models](../../../web/src/lib/types.ts) define dashboard,
  terminal, and RouterOS-facing display contracts.
- [Policy API adapters](../../../web/src/features/policy-routing/api.ts)
  parse unknown JSON once and expose typed results to pages.
- [Draft validation](../../../web/src/features/policy-routing/drafts.ts)
  keeps editable form checks separate from rendering.
