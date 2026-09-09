# Complete UI variants

## Scope
The application ships exactly two complete interfaces: Compact (PR #6) and Aurora (PR #7). The old main frontend is replaced. Keep the policy/access backend shared; UI selection is never a RouterOS or backend configuration mutation.

## Entry and isolation
- `web/src/main.tsx` is a neutral dispatcher, with no static UI/CSS import.
- `web/src/aurora.tsx` boots the Aurora root source tree.
- `web/src/compact/main.tsx` boots the preserved Compact tree in `src/compact/`.
- Dynamically import exactly one variant. Switching uses full document navigation, clearing all outgoing CSS, dialogs, chart instances and polling.
- Keep React/Vite and the Go embed build shared. Both uPlot and ECharts ship, but their UI graphs are loaded separately.
- Do not globally import the other variant's styles or convert this to a root CSS-class toggle.

## Preferences and navigation
`uiPreference.ts` owns validated cross-UI helpers. `?ui=compact|aurora` takes precedence over `rosboard:ui-variant`, with Aurora as a clean-browser fallback. Keep the query override for recovery when storage is denied. Both settings pages provide a UI style selector; choosing an option does not navigate until explicit Save and Switch. The destination is `#/settings/ui`. Compact clears this handoff hash when leaving the settings section.

Selection is browser-local. Preserve the shared selected-device key; never persist credentials or server responses. Preference writes merge the panel record instead of discarding unknown/other-variant fields. Prefer shared theme and refresh keys, fall back to the legacy Compact record. Missing refresh means 1000ms, explicit 0 means stopped. Validate unsupported/corrupt values. Local and session storage denial must not break Compact startup or UI switching. Reset display preferences without changing the chosen variant.

## API compatibility
Both UI policy consumers use the same canonical backend contract through the Compact re-export. Terminal metadata is POST `/api/terminals/{id}/metadata?device={device}` with `customName` only; blank restores auto naming. Terminal remarks are no longer exposed/editable in either interface; the old database column remains inert for compatibility. Preserve terminal ID and device scope.

## Verification
`npm --prefix web test`, lint, build and `npm --prefix web run check:ui-build` are required. Build checks assert a neutral startup stylesheet graph, both entrypoints, disjoint root CSS graphs and existence of referenced assets. `.vite/manifest.json` is generated verification metadata, ignored in Git and excluded by Go embedding. Component tests exercise both save/switch paths, persistence/migration, denied storage and terminal payloads. Vite development proxy defaults to local 8090; opt into a specific backend using ROSBOARD_DEV_PROXY.

Visual acceptance remains user-led per quality-guidelines.md. Provide exact production URLs plus desktop/mobile and light/dark switch steps; do not substitute component tests for full visual acceptance.

## Conditional preload regression
Do not express the entry selection as a ternary between two dynamic imports. The current Vite/Rolldown production transform can fold it into one preload wrapper carrying only Aurora's CSS dependencies, while correctly selecting Compact's JS. Manifest-only checks cannot detect this. Use separate awaited import statements with an early return. The build checker must execute the emitted bootstrap for both variants and assert the exact loaded CSS graph, in addition to inspecting the manifest. Browser reproduction/fix verification is authorized by the user's explicit UI bug report; check the real built output rather than Vite development mode.

## Version and update maintenance card

Both maintenance views (including Compact's no-device shell) expose the shared
`features/update/UpdatePanel` markup/behavior in UI-native card wrappers. Styling
stays in each variant's own CSS graph. The card has check/install actions only,
with a pinned-version confirmation dialog and the latest update result below.
Release notes render as text and release links are restricted to the official
repository. Status polling is local API polling, never automatic GitHub checks.
Abort polling on unmount; stale reads must not overwrite a newer manual action.
On an observed job's verified success, reload to use the new embedded frontend.

Version metadata uses four columns on desktop and two columns at viewport widths
up to 768px: current/latest version, then platform/last check. Preserve DOM order
and permit long values to wrap inside their own cells.
