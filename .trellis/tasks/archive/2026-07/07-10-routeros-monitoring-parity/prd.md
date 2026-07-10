# RouterOS monitoring parity and UI overhaul

## Goal

Turn the current RouterOS monitoring MVP into a dense, iKuai-inspired read-only operations panel that is useful for day-to-day monitoring without changing the RouterOS configuration.

## Confirmed Facts

- The live panel already provides system overview, interface status, unified IPv4/IPv6 terminal monitoring, terminal details, local cumulative terminal accounting, and short interface-rate history.
- The current application targets one RouterOS 7.21.4 device and polls RouterOS REST endpoints read-only.
- The user approved implementation of:
  1. terminal-monitoring and UI/interaction overhaul;
  2. complete line/interface monitoring supported by the current RouterOS state;
  3. system-load history;
  4. protocol, policy, and split-routing monitoring only where current RouterOS read-only data supports it.
- RouterOS configuration must not be changed. Features that require enabling Traffic Flow/IPFIX, adding probes, adding queues, or changing routing/mangle rules are not part of this task.
- Camera, switch, peripheral-device, AC/AP, and other external-device monitoring are out of scope.
- The panel remains LAN-only and read-only against RouterOS. Local panel persistence is allowed.

## Requirements

### R1. Navigation and console layout

- Replace the current two-item status navigation with a structured monitoring hierarchy:
  - line monitoring;
  - terminal monitoring with All, IPv4, and IPv6 views;
  - traffic monitoring with protocol and policy views;
  - runtime monitoring with load history and routing/split-flow views.
- Do not show empty placeholder pages for unsupported features.
- Preserve the top CPU, memory, upload, and download status strip.
- Use a compact operations-console layout: one page heading, one toolbar, then primary data.
- Remove repeated page titles, repeated breadcrumbs, large fixed empty panels, and capability banners that push data downward.

### R2. Terminal list

- Remove the repeated in-panel terminal title and descriptive subtitle.
- Support sortable columns for IPv4/MAC, connection count, current rates, cumulative traffic, online duration, interface, device name, and remark.
- Default to numeric ascending order by the terminal's primary IPv4 address. IPv4-less rows follow IPv4 rows and use IPv6/MAC as stable fallbacks.
- Preserve the user's selected sort across automatic data refreshes so rows do not jump back to backend traffic order.
- Provide filters for keyword, address family, online state, and interface.
- Provide pagination, page-size selection, manual refresh, and automatic refresh interval controls.
- Avoid duplicated MAC/name/remark presentation. Collapse secondary addresses behind a compact expansion affordance.
- Keep `Modify remark` only in the terminal-list action column.

### R3. Terminal state correctness

- Separate panel tracking age from current online-session duration.
- Derive explicit online/offline/idle state from current connection, DHCP, ARP, and neighbor evidence with documented rules.
- Do not advance `lastSeen` merely because a stale discovery-table row still exists.
- Sort IPv4 and IPv6 address arrays deterministically, with numeric IPv4 ordering.
- Detail data must refresh while the detail page is open.

### R4. Terminal detail

- Remove `Modify remark` from terminal detail.
- Keep one breadcrumb and one compact terminal summary strip.
- Keep Basic information, Connections, Traffic statistics, and Online history as detail tabs.
- Put IPv4/IPv6, protocol, line, and text filters in one compact toolbar immediately above the connection table.
- Display local address/port, remote address/port, NAT/public address when available, protocol, egress-line information when accurately derivable, current rates, byte totals, and connection state.
- Never label the LAN ingress interface as the WAN line. Show `Unknown` if a reliable egress line cannot be derived.
- Clearly label port-based application classification as estimated rather than iKuai-equivalent DPI.
- Replace raw five-second history snapshots with useful online-session history and/or aggregated traffic history.

### R5. Line monitoring

- Show all RouterOS interfaces with name, type, MAC, IP addresses, enabled/running state, negotiated link attributes when exposed, current rates, cumulative bytes/packets, error/drop counters when exposed, MTU, last-link time, and link-down count.
- Distinguish physical interface state, logical interface state, and WAN/routing availability.
- Poll live rates for every interface that can be monitored, not only the configured overview-traffic interface.
- Provide per-interface detail with a recent rate chart and connection/health information available from the existing RouterOS state.
- Any WAN health conclusion must come from existing RouterOS status, routes, or current connection facts; the panel must not create RouterOS probes.

### R6. Load history

- Persist CPU usage, memory usage, storage usage when exposed, online-terminal count, total rates, and packet counters when exposed.
- Provide selectable 1-hour, 1-day, 1-week, and 1-month views.
- Use bounded retention and downsampling so high-frequency samples do not grow without limit.
- Charts must include time labels, value scale or tooltip, current value, and maximum value.

### R7. Native protocol, policy, and routing/split monitoring

- Protocol monitoring must aggregate currently available connection-tracking data by IP protocol and the existing estimated application category.
- Provide current upload/download, connection count, cumulative panel-observed traffic, a recent trend, and a 30-minute distribution where local samples support it.
- Policy monitoring must display existing RouterOS simple queues, queue trees, firewall/mangle counters, connection marks, or routing marks only when those objects already exist and are readable.
- Routing/split monitoring must display existing routing tables/rules, active routes, gateways, route/connection marks, and observable per-mark connection/traffic counts where readable.
- Empty native configuration is a valid state and must be presented as an informative empty result, not as an error.
- No monitoring page may claim DPI application identification, policy hits, or egress-line attribution without supporting RouterOS evidence.

### R8. Polling, storage, and resilience

- Split independent polling domains so one optional endpoint failure does not discard all otherwise fresh dashboard data.
- Batch connection-accounting database writes per refresh.
- Prune stale connection state.
- Add retention/aggregation for load, protocol, interface, and terminal-history samples.
- API responses must remain frontend-ready typed contracts and must not expose RouterOS raw payloads directly.
- Surface data freshness and partial-poller errors in the UI without replacing valid stale data with an empty page.

## Acceptance Criteria

- [x] Terminal list opens sorted by numeric ascending primary IPv4 and every specified sortable column toggles ascending/descending correctly.
- [x] Terminal sorting, filtering, pagination, and refresh controls work together and remain stable across automatic refreshes.
- [x] Repeated terminal headings, duplicate device values, detail-page remark editing, duplicate breadcrumbs, capability banners, and fixed 620px empty detail space are removed.
- [x] Connection data begins near the terminal summary and filter toolbar rather than below stacked title panels.
- [x] Terminal detail refreshes while open and supports family/protocol/line/text filtering.
- [x] Online state, online duration, last activity, and egress-line labels follow documented evidence rules and do not present known false meanings.
- [x] Interface list shows current rates for monitorable interfaces and the available RouterOS-native status fields.
- [x] Interface detail provides a useful recent chart without changing RouterOS configuration.
- [x] CPU, memory, terminal-count, and traffic history can be viewed at 1-hour, 1-day, 1-week, and 1-month ranges with bounded storage.
- [x] Protocol monitoring provides honest native/estimated statistics and a 30-minute distribution from panel-side samples.
- [x] Policy and routing/split pages show existing readable RouterOS state or an informative empty state without modifying RouterOS.
- [x] Poller/API partial failures retain valid prior data and show freshness/error state.
- [x] Connection state and high-frequency history have cleanup/retention coverage.
- [x] Backend unit tests, API smoke checks, frontend lint/type-check/build, and browser verification pass.
- [x] Existing system overview, local cumulative terminal totals, LAN CIDR restrictions, embedded-asset build, and Linux service deployment continue to work.

## Out of Scope

- Any RouterOS write operation or configuration change.
- Enabling or configuring Traffic Flow, NetFlow, IPFIX, SNMP, probes, queues, mangle rules, or routing rules.
- Full DPI or parity with iKuai's proprietary application-identification database.
- Camera, switch, peripheral-device, AC/AP, or other external-device monitoring.
- Blocking terminals, rate limiting, policy editing, or other configuration-console actions.
- Authentication, multi-router management, Docker packaging, or deployment redesign.
