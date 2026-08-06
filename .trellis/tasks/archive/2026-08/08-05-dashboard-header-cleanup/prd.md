# Refine dashboard and monitor page headers

## Goal

Keep the dashboard and monitor pages focused by placing the dashboard search
in the shared topbar and removing redundant page headings and dashboard
controls.

## Requirements

- On `仪表台`, move the existing device search input into the shared page
  topbar, aligned to the right immediately before the `主题` control.
- Remove the dashboard's visible device status-filter and sort controls. Keep
  free-text search, pagination, device rows, and device navigation working.
- On `接口监控`, remove the redundant topbar page title while retaining the
  interface details, status, and last-updated controls. Add topbar tabs for
  `物理接口` and `逻辑接口`; show one selected category at a time with the
  same compact information-table height and topbar information rhythm as
  `终端监控`. Keep system interfaces available in a collapsed section.
- On `DHCP`, change each `dhcp-server` card into a horizontal information row
  showing server name, interface, address pool/range, lease duration, pool
  usage, and status. Preserve the existing lease table and responsive layout.
- In terminal detail `连接详情`, make the detail page fill the available
  viewport height. The connection table takes the remaining height below the
  device summary and detail tabs, with internal vertical/horizontal scrolling
  and no fixed viewport maximum.
- In terminal detail `连接详情`, keep the `IP版本` header row fixed above the
  internal scroll area and add a theme-adaptive transparent mask so scrolled
  rows do not appear through the header background.
- In terminal monitor `全部`, `IPv4`, and `IPv6` lists, keep the monitor tabs
  in the shared topbar and keep the list/table inside the visible viewport with
  internal scrolling. Remove the separate online-state and interface filter
  controls; add `在线状态` immediately before `在线时长`, show online devices by
  default, and provide a downward-arrow selector beside `在线状态` for all
  devices or non-online devices. Remove the separate `终端列表` / `在线设备`
  toolbar title block.
- On every `面板设置` child page, remove the redundant shared topbar
  `面板设置` heading/subtitle. Keep each child panel's own heading (for
  example `设备管理` or `采集设置`) so the current settings section remains
  identifiable.
- Preserve existing API contracts and light/dark/glass theme tokens.
- Keep the moved search accessible, controlled, keyboard-focusable, and free
  of document-level horizontal overflow at desktop and mobile widths.

## Acceptance Criteria

- [x] Dashboard search is in the shared topbar immediately left of `主题`.
- [x] Dashboard filter and sort controls are absent; search, pagination, and
      device-row navigation still work.
- [x] The `接口监控` shared topbar heading is absent while interface content
      and right-side status controls remain available; its `物理接口` /
      `逻辑接口` tabs switch a single category table with matching monitor
      page rhythm and height.
- [x] DHCP Server entries render as horizontal rows with readable numeric
      pool usage and status values; the lease table remains available.
- [x] Terminal detail `连接详情` expands to the available screen height;
      the table scrolls internally, the detail header/tabs remain visible, and
      scrolled rows do not bleed through the fixed `IP版本` header.
- [x] Terminal monitor `全部` / `IPv4` / `IPv6` lists keep their table inside
      the visible browser viewport with internal scrolling; the list has an
      `在线状态` column before `在线时长`, defaults to online devices, and its
      `在线状态` header selector can switch to all or non-online devices while
      the separate `终端列表` / `在线设备` toolbar title block is absent.
- [x] The shared `面板设置` heading/subtitle is absent for all settings child
      pages while child panel headings remain visible.
- [x] Frontend lint/build and Go checks pass.
- [x] The verified build is deployed to `10.0.0.6`, backed up, and remotely
      verified before user inspection.
- [x] User manually approves the deployed result before commit.

## Notes

- The requested search move changes only UI state ownership and placement; no
  backend search contract is introduced.
- The interpretation is to remove the shared redundant heading, not the
  meaningful child section headings inside settings panels.
