# Panel monitoring capability expansion

## Goal

Extend RosBoard from a single-router dashboard into a panel that can monitor multiple RouterOS devices, inspect traffic over useful time ranges, and explain which route a terminal connection is using, while preserving the existing persistent history and MAC-based terminal identity.

## Background

- The application currently creates one RouterOS client and one monitor from the singular `routeros` configuration (`internal/config/config.go:13`, `cmd/rosboard/main.go:51`).
- The home traffic chart currently uses a five-minute interface sample window (`internal/service/monitor.go:203`, `internal/service/monitor.go:598`, `web/src/App.tsx:1062`).
- SQLite already persists interface, load, protocol, terminal, terminal-address, terminal-total, connection-state, and terminal-history data (`internal/store/sqlite.go:43`, `internal/store/sqlite.go:63`).
- Terminals already use normalized MAC addresses as their stable identity when available, with address identities merged later (`internal/service/monitor.go:1610`). Custom names and remarks are persisted and the detail view can show IPv4, IPv6, and MAC together (`web/src/App.tsx:1573`, `web/src/App.tsx:1729`).
- A static route/routing-rule table already exists (`internal/service/monitor.go:754`, `web/src/App.tsx:1242`). Current route records and terminal connections do not contain route match counts or route-attribution fields.

## Requirements

### R1. Multi-device management

- Users can add, edit, remove, enable, disable, and select RouterOS devices from panel settings.
- The existing device summary area at the bottom of the sidebar becomes the global device selector for all monitoring pages.
- The browser remembers the last selected device and falls back to the next available device if that device is removed or disabled.
- Panel settings remain global; its connection section owns the device list and per-device connection editor.
- The first release provides per-device views only, with no `all devices` aggregation. The device-management list still shows health and last-update status for every configured device.
- Each device has its own display name and RouterOS HTTP connection settings.
- Device credentials remain masked by default and can be revealed with an eye control.
- Monitoring state, historical samples, terminals, totals, and metadata must be isolated by device.
- All enabled devices are monitored continuously in the background; UI selection changes only which device is being viewed.
- Collection failures and reconnect backoff are isolated per device so one unavailable router does not interrupt healthy routers.
- Removing a device archives it: collection stops and it disappears from normal selection, but its historical data remains.
- Maintenance settings provide a separate permanent-purge action with explicit confirmation for archived devices and their data.
- Existing single-device installations must migrate without losing their settings or historical data.

### R2. Dashboard time ranges

- The home dashboard offers one shared `5min`, `1h`, `6h`, and `24h` range control.
- Changing the range refreshes CPU, memory, online-terminal, active-connection, and traffic trends together without page navigation.
- Metric-card primary values remain live; sparklines, averages, and peaks use the selected historical range.
- Longer ranges are downsampled to a bounded number of points so API payload and chart rendering remain responsive.
- The selected range is retained for the current browser session.

### R3. Preserve existing persistence

- Existing SQLite persistence remains the source of historical monitoring data.
- Existing retention cleanup continues to work after device scoping is introduced.
- No parallel persistence subsystem is added.

### R4. Preserve MAC-based terminal management

- MAC remains the stable terminal identity when RouterOS can provide it; IP remains a temporary fallback only.
- Existing custom name, remark, IPv4, IPv6, and MAC display behavior remains available.
- Multi-device scoping prevents identical MAC addresses on different routers from being merged.

### R5. Route display and terminal attribution

- Keep the existing static route and routing-rule table.
- Terminal connection details show the selected route table, matched rule when determinable, and resulting route for each connection.
- Attribution distinguishes RouterOS-provided facts from locally inferred matches.
- The route page shows `current matched connections`, calculated from the current conntrack snapshot and explicitly labeled as a snapshot-derived value rather than a lifetime counter.
- Existing authoritative RouterOS mangle packet/byte counters remain exact policy counters when present.
- RosBoard does not create or modify RouterOS mangle/routing rules to manufacture cumulative counters.
- Policy routing, routing marks, longest-prefix matching, disabled/inactive routes, and unavailable attribution have defined behavior.
- Route collection and attribution cover both IPv4 and IPv6 connections.

### R6. Operational display preferences

- The route and routing-rule table hides disabled entries by default and provides a checkbox to reveal them without changing RouterOS state.
- Collection settings present traffic interfaces from the selected device as checkbox options instead of requiring users to type interface names.
- Previously configured interfaces that are temporarily unavailable remain visible and selected so saving does not silently discard them.
- Interface settings provide light and dark themes. Theme preference is stored in the current browser and applies to charts as well as standard controls.

## Acceptance Criteria

- [ ] AC1: At least two RouterOS devices can be configured and selected without mixing their live or historical data.
- [ ] AC1a: Selecting a device in the sidebar consistently changes the scope of every monitoring page and survives a browser refresh.
- [ ] AC1b: No monitoring page merges metrics across devices; the management list still exposes each device's health and last update.
- [ ] AC2: An existing single-device database and connection configuration migrates automatically and remains usable.
- [ ] AC2a: Removing a device preserves its history; permanently purging that device requires a distinct confirmed maintenance action.
- [ ] AC3: The home range switches among `5min`, `1h`, `6h`, and `24h`; CPU, memory, online-terminal, active-connection, and traffic histories update together with bounded, chronological samples.
- [ ] AC4: Existing persisted history survives restart and remains subject to retention cleanup.
- [ ] AC5: A terminal retains its MAC identity, custom name, remark, IPv4 addresses, and IPv6 addresses within its owning device.
- [ ] AC6: The route page continues to list routes and routing rules after the changes.
- [ ] AC7: Terminal connections display route attribution when sufficient data exists and an explicit unavailable or estimated state otherwise.
- [ ] AC8: Route match counts are labeled as current snapshot-derived connection counts; only RouterOS-native mangle packet/byte counters are presented as exact counters.
- [ ] AC9: Backend tests cover migration, device isolation, time-window queries, and route matching; frontend tests or build checks cover selectors and attribution states.
- [ ] AC10: Disabled routes and rules are hidden initially, can be shown with one checkbox, and visible/total counts remain explicit.
- [ ] AC11: Traffic interfaces can be selected from current RouterOS interfaces without free-form input, including preservation of configured unavailable interfaces.
- [ ] AC12: Light and dark themes can be selected, persist across reload in the same browser, and render without horizontal overflow at desktop and mobile widths.
- [ ] AC13: Saving multiple traffic interfaces survives the service restart, automatically restores the page, and renders pre-refresh empty collections without a blank screen.

## Out of Scope

- Replacing SQLite with another database.
- Reimplementing terminal remarks or IPv4/IPv6 display that already exists.
- Packet capture or deep packet inspection.
- Claiming exact route hit counts when RouterOS does not expose an authoritative counter.
