# Redesign status-monitor navigation and header search

## Goal

Make the status-monitor pages match the supplied reference: expose each
page's third-level choices as page-level navigation, reduce those headers to
useful global controls, and move the terminal search input into the top-right
header.

## Requirements

- Remove all third-level entries from the left sidebar for the four status
  monitor groups and render them as page-level navigation above the relevant
  page content:
  - 终端监控: `全部` / `IPv4` / `IPv6`
  - 流量监控: `协议统计` / `策略统计`
  - 网络服务: `DHCP` / `路由 / 分流`
  - 系统运行: `资源监控` / `负载历史`
- Preserve the existing `TerminalFamily` behavior: changing a family filters
  the list, updates the scope summary, clears the selected terminal detail,
  and keeps the selected family in panel preferences.
- Remove the grouped monitor pages' left header title/subtitle presentation
  (`系统正常 · 更新于 ...`) while retaining the page-level choices there.
- Keep the global `系统正常` health indicator available in the top-right
  control cluster; only the terminal title and its old updated-at subtitle are
  removed.
- Show `最后更新 <相对时间>` in the top-right cluster and place the existing
  terminal search input immediately to its right.
- Remove the duplicate terminal search input from the old terminal toolbar;
  keep the remaining state/interface/presence/result/refresh controls working.
- Maintain keyboard focus states, accessible labels/selected state for the
  family navigation, and the existing light/dark theme tokens.
- Keep desktop and mobile layouts free of document-level horizontal overflow;
  preserve the project's 44px mobile control targets.

## Acceptance Criteria

- [x] The four status-monitor groups have no third-level submenu items in the
      sidebar.
- [x] Each grouped page shows its page-level choices above the content and the
      active choice is visually and semantically selected.
- [x] The grouped monitor title/subtitle line is gone; the top-right cluster
      still shows health and last-updated status.
- [x] The terminal top-right cluster shows the search input immediately after
      the last-updated text.
- [x] There is exactly one terminal search input, and it filters the same rows
      as before from its new location.
- [x] Existing terminal filters, sorting, pagination, detail navigation, and
      refresh controls continue to work.
- [x] `npm --prefix web run lint` and `npm --prefix web run build` pass.
- [x] The verified build is deployed to `10.0.0.6` with timestamped backups,
      and the remote service, health endpoint, API response, and embedded
      frontend assets are verified.
- [x] The user manually inspects the deployed page and approves it before any
      work commit is created.

## Notes

- The supplied screenshot is a visual reference, not a request to change the
  terminal table's data columns or backend contracts.
- The original toolbar search is moved, not removed from behavior.

## User Acceptance

The user manually inspected the final deployment at
`http://10.0.0.6:8080` and replied `通过` on 2026-08-05.
