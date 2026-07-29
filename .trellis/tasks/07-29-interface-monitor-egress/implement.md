# Implementation Plan

## 1. Lock baseline and tests

- Record the current branch/worktree state and preserve unrelated user changes.
- Add route-attribution tests for `%interface`, direct PPPoE, unavailable
  physical egress, IPv6 zone syntax, and ECMP candidates.
- Add interface-projection tests for physical/logical/system classification,
  three-state inputs, PPPoE/VLAN/bridge relations, and missing optional data.

Verify: focused tests fail only for the unimplemented contract.

## 2. Collect and project interface topology

- Add minimal RouterOS types/client reads for VLAN and bridge-port records.
- Collect them during the full refresh as optional topology evidence.
- Extend `InterfaceStatus` and build classification/relations without changing
  traffic-scope selection or aggregate calculations.
- Preserve interface detail lookup and history behavior.

Verify: focused service/client/API tests pass; existing traffic-scope tests are
unchanged and pass.

## 3. Project route egress interfaces

- Parse immediate gateways into normalized gateway and logical-route-interface
  components.
- Resolve logical route interfaces through PPPoE/VLAN carrier chains to
  physical interfaces, with cycle and ambiguity protection.
- Extend equal-best route attribution and `TerminalConnection` with logical
  route-interface evidence and physical egress interfaces.
- Preserve policy-routing, routing-mark, longest-prefix, distance, fallback,
  IPv4/IPv6, and route-ID behavior.

Verify: all route matcher and terminal projection tests pass, including
`pppoe-out1 -> wan` physical tracing and `10.0.2.1%wan-xray` normalization.

## 4. Update interface and terminal UI

- Rename all user-facing "线路监控" copy to "接口监控".
- Render physical and logical sections, Down-first physical ordering, distinct
  online/Down/disabled labels, relations, and collapsed system interfaces.
- Keep the existing interface detail interaction for every section.
- Add the connection-table physical-egress column, sorting/filtering, and
  scoped physical-egress summary.
- Keep responsive behavior usable on desktop and mobile.

Verify: TypeScript/build checks pass; search confirms no stale user-facing
"线路监控" labels; visual inspection covers desktop and mobile widths.

## 5. Full local verification

- Run formatting on changed Go/TypeScript sources using project tooling.
- Run the complete Go test suite and race checks proportionate to the changed
  monitor/route code.
- Run frontend lint/type/build checks available in `web/package.json`.
- Run the built application locally against a safe read-only RouterOS setup or
  representative fixtures; verify interface and terminal API payloads.
- Render/open the embedded frontend and inspect both themes where practical,
  physical Down visibility, logical relations, system collapse, terminal
  physical-egress summary/table, empty states, and browser console errors.

Verify: all automated checks pass and local runtime/visual evidence matches the
acceptance criteria.

## 6. Deploy for manual acceptance

- On `10.0.0.6`, resolve exact binary/config/SQLite paths and free space with
  read-only checks.
- Create one timestamped backup directory containing the existing binary,
  configuration, and SQLite data before replacement.
- Deploy the locally verified binary and restart `rosboard.service`.
- Verify systemd status/logs, health endpoint, dashboard/interface APIs,
  terminal-detail logical-route and physical-egress fields, and embedded asset
  responses.
- Confirm the live device shows expected examples including physical Ethernet,
  `pppoe-out1 -> wan`, `vlan2-aruba -> lan`, bridge/veth, and WireGuard without
  a fabricated carrier.

Rollback point: restore the timestamped binary and restart the service if any
remote verification fails. Configuration and SQLite restoration is only needed
if their hashes changed unexpectedly.

## 7. User gate and finish

- Ask the user to manually inspect `http://10.0.0.6:8080` and wait for explicit
  approval.
- Only after approval: review the final diff, update the monitoring spec with
  confirmed stable contracts, create the work commit, record the Trellis
  session, and archive the task.

Verify: approval is recorded before the commit; task artifacts and final commit
refer to the same verified deployment.

## 8. Acceptance feedback: connection-table usability

- Replace the connection table's unbounded generic scroll wrapper with a
  responsive viewport-bounded two-axis scroll region and sticky header cells.
- Remove global search, visible/scoped toolbar, and global clear action from all
  desktop and mobile layouts and remove their obsolete state/styles.
- Preserve per-column filters and sorting. Explicitly verify IP-version header
  filtering offers All/IPv4/IPv6 in all-family scope and that family-specific
  scope remains authoritative.
- Preserve each column filter's local "全部" reset option and keep the layout
  free of document overflow at 375px.
- Rebuild embedded assets and repeat frontend lint/build, full Go regression,
  runtime/API checks, remote timestamped backup/deployment, and manual user
  acceptance. Do not commit before the renewed approval.

Verify: long tables expose a reachable horizontal scrollbar within the visible
table region; sticky headers remain visible during internal vertical scroll;
no global search/clear controls remain; IP-version filtering works from its
column header without cross-family leakage.

## 9. Acceptance feedback: remove external-address column

- Remove the "外网地址" header/cell and `publicAddress` frontend sort key.
- Decrement ordinary and RouterOS-self connection-table column counts while
  keeping empty rows aligned.
- Leave the backend/API `publicAddress` field intact for compatibility.
- Rebuild embedded assets, run source audits and automated checks, then repeat
  timestamped backup/deployment and user-led visual acceptance only.

Verify: generated assets contain no "外网地址" connection-column label; header,
body, and empty-row spans match for ordinary and RouterOS-self details.

## 10. Acceptance feedback: remove interface summary toolbar

- Remove the interface page's top summary toolbar containing the page label,
  read-only description, and aggregate interface count.
- Do not relocate the aggregate count; preserve physical, logical, and system
  section headings, local counts, and the physical Down count.
- Rebuild embedded assets and repeat frontend lint/build and source checks.

Verify: the removed toolbar text is absent from source/generated assets while
all three section-level headings and their required count/status text remain.

## 11. Acceptance feedback: terminal navigation toggle

- Make the `终端监控` group header toggle its nested menu closed on a second
  click.
- On collapse, change disclosure state only; preserve the current terminal
  view, family scope, and selected terminal detail.
- Rebuild embedded assets and repeat frontend checks.

Verify: the collapse branch returns before any navigation/detail-reset state
updates, while the expand branch preserves the existing all-terminal behavior.

## Completion status

- [x] Implementation and automated verification completed.
- [x] Timestamped remote backup, deployment, and service/API/assets checks completed.
- [x] User manually inspected the deployed interface and terminal workflows and explicitly accepted the result.
- [ ] Work commit, push, release publication, session recording, and task archival remain for the release/finish workflow.
