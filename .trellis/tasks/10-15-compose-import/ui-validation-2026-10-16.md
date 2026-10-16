# Container UI alignment — 2026-10-16

## Change

Both container entries now provide shell-native presentation adapters. Aurora
uses public Card/Button/Badge components and table styling; Compact uses panel,
control and table classes. The shared feature keeps its existing API/draft/job
logic and native section/footer semantics. Shell CSS remains isolated.

Removed the independent brand/English headings, outer page frame and four large
metric cards. Statistics sit inside the list heading; search/status/refresh/create
share its toolbar. Primary row actions use text; secondary actions expand inline
inside the scrolling table, avoiding clipped floating menus. Selection, outside
pointer and Escape dismiss the disclosure. Native dialogs share a heading/close
control and restore focus. Destructive confirmations use the danger treatment.

Editor sections, directory browser, Compose report/dialog, progress and logs use
the active shell's typography, spacing, radii, surfaces and elevation. The editor
retains seven flat sections and advanced controls, with return actions at both
ends. Background read errors and explicit operation errors render independently.
The development preview mounts the same selected presentation adapter.

## Automated validation

- 107 frontend tests passed, including real Aurora/Compact adapter coverage of
  confirmations, keyboard/outside dismissal, focus restoration and no accidental
  writes. Existing create/edit, drafts, directory, resources, Compose cancellation,
  stale/device responses, sorting/filtering/pagination and submit-error tests pass.
- TypeScript, Vite build and dual-UI checks passed. Emitted bootstrap execution
  confirms each UI's correct CSS graph; container CSS graphs stay disjoint and
  development preview/fixtures are excluded.
- oxlint passed with only the four pre-existing FastTrack test key warnings.
- Full Go tests and vet passed on the isolated test host (Go 1.27.1). The final
  embedded assets passed the UI package tests and candidate build. A cold
  trimpath rebuild was stopped when the 512 MiB host began swapping; the final
  build reused the checked cache with one build worker and a 256 MiB Go memory
  target. The source and embedded assets were unchanged by this build choice.
- Source/staged paths and patch inspected for unrelated files and secret material.

## Test delivery

Only rosboard-container-test on the disposable test machine was replaced, with
version dev-container-ui-1016. No production deployment was performed.

Before replacement, authenticated container reads passed and showed no unsettled
job. After replacement, systemd was active; health identified the candidate;
authenticated device/container reads passed for the enabled isolated device.
Unauthenticated container reads remained HTTP 401. Temporary verification
sessions were deleted. Configuration, supervisor and service-unit hashes stayed
identical, and the installed binary matched the checked candidate.

All 78 distributable embedded files matched local SHA-256 hashes over HTTP.
Both UI entry URLs returned HTTP 200. Verification exercised no RouterOS writes.

## Pending user visual acceptance

- Aurora: http://10.0.0.60:8080/?ui=aurora#/containers
- Compact: http://10.0.0.60:8080/?ui=compact#/containers
- Inspect each at 1440px and 390px, in light and dark themes. Compare with access
  control/target-library pages: page and card hierarchy, typography, buttons,
  table density and dialog surfaces should follow the selected shell.
- Exercise search/status/sort/pagination; inspect More, Escape and a cancelled
  delete confirmation. Create -> Compose import -> parse/select/apply/cancel;
  browse directories and check advanced options. No configuration submission or
  destructive operation is necessary for visual review.
- Expect a compact summary, readable text actions, seven flat form sections and
  no document horizontal overflow; the table alone may scroll horizontally.

Browser visual acceptance was not performed by the agent. Keep the task active
and PR Draft; manual acceptance and root APPROVED remain pending. Production
still has the previously delivered Compose build documented in validation.md.
