# Implementation Plan

1. Update dashboard query ownership, remove dashboard filter/sort controls,
   and place the search input in the shared topbar.
2. Hide the redundant shared headings for interface and settings pages while
   preserving content headings and status controls.
3. Adjust only the affected desktop/mobile CSS and remove CSS made orphaned by
   the dashboard toolbar removal.
4. Run frontend lint/build, Go tests/vet, diff checks, and a temporary local
   runtime check against embedded assets.
5. Back up the current binary, configuration, SQLite database, and sidecars on
   `10.0.0.6`; deploy the verified build and validate systemd, health, API, and
   embedded frontend assets.
6. Ask the user to inspect the dashboard, interface, and settings pages on
   desktop and mobile. Do not commit before explicit approval.

## Execution Record

- `npm --prefix web run lint`, `npm --prefix web run build`, `go test ./...`,
  `go vet ./...`, and `git diff --check` passed.
- A temporary local instance returned healthy `/api/health`, the expected
  `needs_admin` bootstrap phase, embedded HTML asset references, HTTP 200 JS
  and CSS responses, and the dashboard-search marker. The rebuilt CSS no
  longer contains the removed fleet toolbar/search classes.
- Remote backup: `/opt/rosboard/backups/20260805-154001-dashboard-header-cleanup/`.
  The service was stopped before copying the existing binary, configuration,
  SQLite database, and any present database sidecars; required backup files
  are present.
- The new Linux amd64 binary SHA-256 is
  `3885212e8e88851a0571bba3c7076981fe91062e4ad60afa7714dbc95a8288f2`.
- Remote `rosboard.service` is active and enabled; `/api/health` returns
  `{"ok":true}`, `/api/bootstrap` returns the expected logged-out ready phase,
  unauthenticated `/api/dashboard` returns `401`, and the deployed JS/CSS
  assets return `200`.
- Awaiting the user's manual visual acceptance at `http://10.0.0.6:8080` for
  the dashboard search, interface-monitor header, and settings child pages on
  desktop and mobile widths.

## User Feedback Follow-up

- The user requested the dashboard search and theme controls to remain on one
  desktop topbar row, and requested settings content to move up after removing
  the shared heading.
- Added a dedicated fleet topbar layout that prevents desktop control wrapping
  while retaining mobile wrapping, and added a compact headingless topbar so
  settings/interface content no longer sits below an empty 62px title area.
- Repeated lint/build, Go test/vet, local runtime, and embedded asset checks.
- Re-deployed with backup:
  `/opt/rosboard/backups/20260805-154606-dashboard-header-feedback-fix/`.
  Final Linux amd64 binary SHA-256:
  `8733ea6255a289d0435ac0c8520f7bfce1e9476df8d8593a0a98c70e7c502272`.
- Final deployment service, health, API, and JS/CSS asset checks passed;
  awaiting renewed user visual acceptance.

## Interface and DHCP Follow-up

- Added interface-monitor topbar tabs for `物理接口` and `逻辑接口`, a
  shared monitor summary layout with terminal monitoring, and one selected
  interface table using the terminal list's compact toolbar/table rhythm.
  System interfaces remain available in a collapsed section.
- Replaced the DHCP Server card grid with a horizontal labeled list row. Each
  row keeps server/interface/pool/range/lease/status values and adds numeric
  pool usage with a restrained usage bar; 999px and 767px breakpoints stack
  the same fields without document overflow.
- Repeated lint/build, Go test/vet, diff checks, and a local embedded-asset
  runtime check passed. The final local assets exposed the interface tab,
  monitor-summary, DHCP Server, and DHCP usage markers.
- Remote deployment and manual visual acceptance are still pending for this
  follow-up scope; no work commit has been created.

## Follow-up Deployment Record

- Linux amd64 binary SHA-256:
  `2fb19ed76e0584cb6f69d777e1023bfa131040ec2c9e96bab19bf47f219fe7cd`.
- Remote backup before replacement:
  `/opt/rosboard/backups/20260805-160245-interface-dhcp-layout/`; it contains
  the prior binary, configuration, and the SQLite database after the service
  was stopped. SQLite WAL/SHM files had been checkpointed and were absent at
  copy time.
- `rosboard.service` is active and enabled after deployment. Remote
  `/api/health` returns `{"ok":true}`, `/api/bootstrap` remains in the
  expected logged-out `needs_login` phase, unauthenticated `/api/dashboard`
  returns `401`, and the deployed JS/CSS assets return `200`.
- Remote JS markers include the interface tabs, monitor summary, DHCP Server,
  and usage bar; CSS markers include the shared monitor summary, horizontal
  server-card grid areas, and responsive rules.
- Awaiting the user's manual visual acceptance of the interface and DHCP
  follow-up before creating the work commit.

## Connection Detail Height Deployment Record

- Linux amd64 binary SHA-256:
  `4125752de6ad676535961bd4a7c9a30dd2f779e2b04b39ca0b57fb484cc456e2`.
- Remote backup before replacement:
  `/opt/rosboard/backups/20260805-161210-terminal-connection-height/`; it
  contains the current binary, configuration, and SQLite database after the
  service was stopped. SQLite WAL/SHM files were absent at copy time.
- `rosboard.service` is active and enabled after deployment. Remote
  `/api/health` returns `{"ok":true}`, `/api/bootstrap` remains in the
  expected logged-out `needs_login` phase, unauthenticated `/api/dashboard`
  returns `401`, and the deployed JS/CSS assets return `200`.
- Remote JS/CSS markers confirm `detail-page-connections`,
  `detail-panel-connections`, `终端连接明细`, `100dvh`, and the full-height
  `max-height:none` override are embedded.
- Awaiting manual visual acceptance of the full-height terminal connection
  detail before creating the work commit.

## Connection Detail Overflow Correction

- The user reported that the first full-height attempt pushed the connection
  viewport below the browser window. Root cause: the detail page `min-height`
  was combined with the outer content padding without a definite parent
  height.
- Corrected the connections-only mode to lock the shell/content to `vh`/`dvh`,
  use an 8px bottom gap, and let the internal connection viewport consume the
  remaining flex height. Other terminal detail tabs retain their normal page
  scrolling behavior.
- Repeated lint/build, Go test/vet, diff checks, and local embedded-resource
  checks passed. A remote redeployment is still pending for this correction.

## Connection Detail Height Follow-up

- Added a connections-only modifier to `TerminalDetailPage` so the device
  summary and detail tabs keep natural height while the connection panel and
  table viewport flex to fill the remaining `vh`/`dvh` space.
- Removed the fixed connection viewport maximum only for this full-height
  mode; internal vertical/horizontal scrolling, sticky headers, and the
  existing mobile overflow protections remain in place.
- Repeated lint/build, Go test/vet, diff checks, and local embedded-resource
  checks passed. Local JS/CSS exposed the new detail modifiers and `100dvh`
  rules; remote deployment and manual acceptance remain pending.

## Connection Detail Overflow Correction Deployment Record

- Linux amd64 binary SHA-256:
  `302058d2fffbb8aa894dbb6d91547bf261204f9514d215b4d34dd3c2821afb23`.
- Remote backup before replacement:
  `/opt/rosboard/backups/20260805-162015-terminal-connection-viewport-fix/`; it
  contains the prior binary, configuration, and SQLite database after the
  service was stopped. SQLite WAL/SHM files were absent at copy time.
- `rosboard.service` is active and enabled after deployment. Remote
  `/api/health` returns `{"ok":true}`, `/api/bootstrap` remains in the
  expected logged-out `needs_login` phase, unauthenticated `/api/dashboard`
  returns `401`, and the deployed JS/CSS assets return `200`.
- Remote JS/CSS markers confirm the connections-only shell/content classes,
  detail page modifiers, 8px bottom gap, and internal `max-height:none`
  override are embedded.
- Awaiting the user's manual visual acceptance of this correction before
  creating the work commit.

## Sticky Connection Header Mask

- Added a theme-adaptive overlay to the sticky connection table header row.
  The row now uses a higher stacking order and each header cell paints a
  surface-colored, blurred pseudo-element behind the header controls. The
  glass theme receives a high-opacity translucent surface so the design keeps
  its glass character while scrolled row text no longer shows through the
  `IP版本` header.
- Repeated frontend lint/build, Go tests, and diff checks passed. The Linux
  amd64 binary SHA-256 is
  `4214470a8204e0769325c3c317b235f136ce40870867ca4a52e0a1b15534a7ff`.
- Remote backup before the final replacement:
  `/opt/rosboard/backups/20260805-162758-terminal-sticky-header-mask-linux-fix/`;
  it contains the previous binary, configuration, SQLite database, and any
  present SQLite sidecars after the service was stopped.
- Final remote service is active and enabled. `/api/health` returns
  `{"ok":true}`, `/api/bootstrap` remains in the expected logged-out
  `needs_login` phase, unauthenticated `/api/dashboard` returns `401`, and
  the deployed JS/CSS assets return `200`. Embedded markers for the
  connection table, header mask, and connections-only shell are present.
- Browser smoke verification reaches the deployed login page. An
  authenticated terminal-detail visual check still requires the user's
  existing Rosboard session, so manual acceptance remains pending.

## Glass Theme Header Mask Correction

- The user reported that the first glass-theme mask looked like a white
  title bar. The next transparent-only attempt allowed row text to overlap the
  header. The mask now uses two matching theme-adaptive glass layers with
  `--connection-header-mask: rgba(255,255,255,.46)` and 16px blur: opaque
  enough to isolate row text, but still translucent enough to retain the
  pink/blue glass background instead of becoming white.
- Rebuilt and redeployed with backup:
  `/opt/rosboard/backups/20260805-163423-terminal-glass-header-mask/`.
  Linux amd64 binary SHA-256:
  `d3ddb70927c3f587c9082341003fe58cc0bae7ea17ab0e0f949915562b251465`.
- Remote service is active; health is `{"ok":true}`, bootstrap is the
  expected logged-out `needs_login` phase, unauthenticated dashboard is
  `401`, and the new JS/CSS assets return `200`. The deployed CSS contains
  the theme mask variable and 16px blur marker.
- Manual visual acceptance of the glass theme remains pending.

## Glass Theme Text Overlap Correction Deployment

- The user then reported that the transparent-only version let table text
  overlap the fixed header. Increased the glass mask to
  `rgba(255,255,255,.46)` and applied it to both the sticky cell and its
  pseudo-element. This keeps the mask theme-adaptive and translucent while
  providing enough coverage to isolate scrolled text.
- Rebuilt and redeployed with backup:
  `/opt/rosboard/backups/20260805-163754-terminal-glass-header-mask-overlap-fix/`.
  Linux amd64 binary SHA-256:
  `93e648922d3771983d6a3c6d41903c198495bab70c531da98bcd8b715bd0f40a`.
- Remote service is active; health is `{"ok":true}`, bootstrap remains
  `needs_login`, unauthenticated dashboard returns `401`, and the new JS/CSS
  assets return `200`. The deployed CSS contains the `.46` alpha marker and
  the 16px blur marker.
- Manual visual acceptance remains pending.

## Glass Theme Header Background Isolation

- The user provided a further screenshot showing that even the stronger
  translucent layers still let text visibly overlap the sticky header. The
  final approach keeps the table header visually in the glass theme by
  reusing the exact `--glass-page-background` gradient as an opaque,
  viewport-fixed header background. It is no longer dependent on alpha
  transparency for occlusion, so the scrolled row text cannot appear inside
  the title cells.
- Rebuilt and redeployed with backup:
  `/opt/rosboard/backups/20260805-164452-terminal-glass-header-background-fix/`.
  Linux amd64 binary SHA-256:
  `f763428e4128d89e178470cde8801ca005a5f8005bf2c909b42849fb2fc16a72`.
- Remote service is active; health is `{"ok":true}`, bootstrap remains
  `needs_login`, unauthenticated dashboard returns `401`, and the new JS/CSS
  assets return `200`. Deployed CSS markers confirm the theme background,
  header mask, and fixed background attachment.
- Manual visual acceptance remains pending.

## Terminal List Layout and Presence Selector

- Removed the terminal list's separate online-state and interface selects and
  added a title-adjacent native visibility selector. The default is online
  devices; the other choices expose all devices or non-online devices,
  including inactive and offline states.
- Added an explicit `在线状态` column before `在线时长`, with a state badge and
  a dash for duration on non-online rows. The same change applies to all,
  IPv4, and IPv6 terminal scopes.
- Made the terminal list content and panel viewport-bounded. The table now
  owns internal two-axis scrolling, and its sticky header uses the existing
  theme-adaptive mask used by connection details. Mobile controls retain two
  44px rows and hide only the less essential metric columns.
- `npm --prefix web run lint`, `npm --prefix web run build`, `go test ./...`,
  `go vet ./...`, and `git diff --check` passed. A temporary local instance
  returned healthy `/api/health`, the expected logged-out `needs_admin`
  bootstrap phase, `401` for unauthenticated `/api/dashboard`, and HTTP 200
  for the rebuilt JS/CSS assets; embedded markers for the terminal list,
  status selector, and header mask were present.
- Linux amd64 binary SHA-256:
  `b6a46c1cd69863267a3dceecd20e5d277d5a15ade5b8cef625d3cced47e5c430`.
- Remote backup before replacement:
  `/opt/rosboard/backups/20260805-1705-terminal-list-layout-final/`; it contains
  the prior binary, configuration, and SQLite database after the service was
  stopped. SQLite WAL/SHM files were absent at copy time.
- Remote `rosboard.service` is active after deployment; `/api/health` returns
  `{"ok":true}`, `/api/bootstrap` remains in the expected logged-out
  `needs_login` phase, unauthenticated `/api/dashboard` returns `401`, and
  the deployed CSS/JS assets return `200`. Remote markers confirm
  `terminal-list-content`, `terminal-table-scroll`, `在线状态`, and
  `终端显示范围` are embedded.
- Awaiting the user's manual visual acceptance of the terminal `全部` / IPv4 /
  IPv6 list layout, viewport-bound table scrolling, and online-device selector
  before creating the work commit.

## Terminal Status Header Filter Follow-up

- Removed the separate `终端列表` / `在线设备` toolbar title block while
  keeping the result count and refresh controls.
- Moved the online/all/non-online selector to a downward-arrow filter button
  directly beside the `在线状态` table heading. The popover is keyboard- and
  pointer-dismissible, stays inside the terminal list panel, and keeps the
  existing default-online and pagination-reset behavior.
- Repeated frontend lint/build, Go test/vet, diff checks, and local runtime
  asset checks. Linux amd64 binary SHA-256:
  `afca54e344f1d5598ec903bfed7471c0cdb3a2e40fe948e1995673534cf1ae3a`.
- Remote backup before replacement:
  `/opt/rosboard/backups/20260805-1713-terminal-status-filter/`; the service
  is active, health is `{"ok":true}`, unauthenticated dashboard is `401`,
  and the rebuilt CSS/JS assets return `200` with the status-header filter
  markers embedded.
- Awaiting manual visual acceptance of the revised table header placement
  before creating the work commit.

## Manual Acceptance and Archive Approval

- The user confirmed the deployed terminal status-header filter placement and
  approved archiving the completed task. All PRD acceptance criteria are now
  satisfied; no further UI changes are pending before commit.
