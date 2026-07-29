# 接口监控分类与终端出接口推导

## Goal

Make interface health easier to inspect by separating physical, logical, and
system interfaces, and make each terminal connection's inferred RouterOS
egress interface visible alongside its route and gateway attribution.

## Requirements

- Rename the user-facing "线路监控" section and related copy to "接口监控".
- Divide the interface page into two primary sections:
  - Physical interfaces returned by RouterOS Ethernet inventory, including
    renamed Ethernet ports and virtual-machine NICs exposed by RouterOS as
    Ethernet interfaces.
  - Logical interfaces, including PPPoE, VLAN, WireGuard, bridge, veth, and
    other non-Ethernet business interfaces.
- Keep loopback/system interfaces out of the two primary sections and expose
  them in a collapsed "系统接口" section.
- Do not render a separate interface-page summary toolbar above the category
  sections. Keep category counts and the physical Down count only in their
  respective physical, logical, and system section headings.
- Physical interface state must distinguish:
  - Online: enabled and running.
  - Down: enabled but not running.
  - Disabled: administratively disabled.
- Put Down physical interfaces before healthy interfaces so link failures are
  visible without scanning the full table.
- Preserve the existing per-interface status, address, MAC, rate, cumulative
  traffic, MTU, link-down count, packet, error, drop, and detail/history data.
- Expose known logical-interface relationships read-only:
  - PPPoE client to carrier interface.
  - VLAN to parent interface.
  - Bridge to member interfaces, and member back to its bridge.
  - WireGuard remains an independent logical interface because it has no fixed
    physical carrier interface.
- Logical interface state must use RouterOS interface state without claiming
  that a running WireGuard interface proves peer reachability.
- Do not change which interfaces contribute to overview WAN traffic totals;
  physical and logical rows may both be displayed without double-counting
  aggregate traffic.
- In terminal detail, infer egress interfaces from the same policy-route and
  route candidates already used for route table and gateway attribution.
- Add an "出接口" column to terminal connection details and trace the selected
  RouterOS route interface to its final physical carrier whenever the collected
  topology makes that relationship unambiguous.
- A PPPoE route through `pppoe-out1` must retain `pppoe-out1` as its next-hop /
  logical-route evidence while displaying the PPPoE carrier, such as `wan`, in
  the "出接口" column.
- Parse an immediate gateway such as `10.0.2.1%wan-xray` as next-hop gateway
  `10.0.2.1` and egress interface `wan-xray`.
- Summarize the distinct currently inferred physical egress interfaces in
  terminal basic information, respecting the selected All/IPv4/IPv6 scope.
- VLAN egress follows its parent chain to a physical interface when the chain
  is known. Interfaces without a fixed or unambiguous physical carrier, such as
  WireGuard or a bridge with multiple possible members, must not be assigned a
  guessed physical interface.
- ECMP attribution must expose all equal-best physical egress candidates.
  Unavailable physical attribution must display `-` rather than guessing.
- The terminal connection table must use a viewport-bounded internal scroll
  area when many rows or columns are present. Its horizontal scrollbar must be
  reachable at the bottom of the visible table region without scrolling to the
  bottom of the whole page.
- Keep connection column headers visible while vertically scrolling the
  internal connection-table region.
- Remove global connection search and the all-state "清除筛选" action entirely;
  do not retain them as icons, popovers, desktop controls, or mobile controls.
- Keep per-column sort and filter controls. The IP-version header must expose
  direct `全部 / IPv4 / IPv6` filtering whenever the terminal-detail scope is
  `all`; family-specific terminal scopes remain constrained to their selected
  family and must not leak rows from the other family.
- Each column filter must retain its own "全部" option so that filter can be
  cleared without a global clear action.
- Remove the terminal connection table's user-facing "外网地址" column and its
  frontend sorting support. Keep the backend conntrack/NAT projection unchanged
  to avoid an unrelated API compatibility change.
- Keep all RouterOS access read-only.
- The `终端监控` navigation group must toggle open and closed. Collapsing the
  group must preserve the active terminal view, family scope, and selected
  terminal instead of navigating or clearing detail state.

## Acceptance Criteria

- [x] Navigation, page titles, and relevant copy consistently say "接口监控".
- [x] The interface page shows separate physical and logical sections and a
      collapsed system-interface section.
- [x] The interface page has no separate top summary toolbar or relocated
      all-interface total; section-level titles, counts, and Down information
      remain visible.
- [x] An enabled non-running Ethernet interface is visibly labeled Down and is
      ordered ahead of online physical interfaces; disabled is a distinct state.
- [x] Current deployed topology classifies `lan`, `wan`, and `wan-xray` as
      physical, and classifies `pppoe-out1`, `vlan2-aruba`, WireGuard, bridge,
      and veth interfaces as logical.
- [x] `pppoe-out1` shows `wan` as its carrier; `vlan2-aruba` shows `lan` as its
      parent; bridge/member relationships are visible in both relevant rows.
- [x] WireGuard rows do not claim a physical carrier or peer-online state.
- [x] Existing interface detail and traffic history remain accessible for all
      displayed interface categories.
- [x] A terminal connection using `pppoe-out1` over `wan` retains
      `pppoe-out1` as the next-hop/logical-route value and displays `wan` as its
      physical egress interface.
- [x] A connection whose immediate gateway is `10.0.2.1%wan-xray` displays
      gateway `10.0.2.1` and egress interface `wan-xray`.
- [x] Policy routing, routing marks, longest-prefix choice, route distance,
      IPv4, IPv6, lookup fallback, and ECMP continue to follow the existing
      route-attribution contract.
- [x] Terminal basic information summarizes distinct physical egress
      interfaces for the active address-family scope.
- [x] A long/wide connection table scrolls vertically and horizontally inside
      a viewport-bounded region; its horizontal scrollbar is reachable without
      going to the bottom of the whole page, and its header remains visible.
- [x] No global search or "清除筛选" control renders on desktop or mobile, and
      no obsolete search/clear state remains.
- [x] In all-family terminal detail, the IP-version header directly filters
      `全部 / IPv4 / IPv6`; scoped IPv4/IPv6 detail never leaks the other family.
- [x] Terminal connection details no longer render or sort an "外网地址"
      column; all remaining header/body/empty-state column counts stay aligned.
- [x] Desktop and 375px layouts have no document-level horizontal overflow;
      mobile has no search/clear actions in the tab row or above the table.
- [x] Existing overview traffic totals and selected traffic-interface behavior
      are unchanged.
- [x] Clicking `终端监控` a second time collapses its nested menu without
      changing or clearing the currently selected terminal page.
- [x] Backend tests, frontend checks/build, race-relevant tests, local runtime
      verification, and visual verification pass.
- [x] The verified build is deployed to `10.0.0.6` only after timestamped
      backups of the existing binary, configuration, and SQLite data.
- [x] The remote service, health endpoint, interface API, terminal-detail API,
      and embedded frontend assets are verified after deployment.
- [x] No work commit is created until the user manually inspects the deployed
      instance and explicitly approves it.

## Notes

- Confirmed by the user on 2026-07-29 after reviewing the physical/logical
  grouping and PPPoE egress-display semantics.
- The deployed build and all acceptance-feedback iterations were manually
  inspected and explicitly accepted by the user on 2026-07-29 before release
  preparation began.
- This is one cross-layer deliverable rather than separate tasks: interface
  topology projection and route egress projection share the same RouterOS
  collection cycle, API model, frontend types, and deployment acceptance gate.
