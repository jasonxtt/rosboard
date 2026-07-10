# RouterOS visual monitoring panel

## Goal

Build a read-only visual monitoring panel for the user's main RouterOS router, inspired by the iKuai "System Overview" and "Status Monitoring" pages, with emphasis on live system status and unified client monitoring rather than router configuration management.

## Confirmed Facts

- Repository baseline: `/Users/tom/github/rosboard` currently contains Trellis scaffolding only and no existing application code.
- Target router for the initial request is the live main router at `10.0.0.1`.
- Live RouterOS access is available now:
  - HTTP web service on port `80` responds.
  - TCP port `8639` is reachable.
  - REST reads over `http://10.0.0.1/rest/...` with the provided credentials succeed.
- Current router identity from live REST:
  - Platform: `MikroTik`
  - Board: `CHR VMware, Inc. VMware Virtual Platform`
  - Version: `7.21.4 (long-term)`
  - Architecture: `x86_64`
  - Total memory: `1073741824`
- Live REST endpoints already verified as readable:
  - `/rest/system/resource`
  - `/rest/system/health`
  - `/rest/interface`
  - `/rest/interface/ethernet`
  - `/rest/interface/monitor-traffic`
  - `/rest/ip/address`
  - `/rest/ip/dhcp-server/lease`
  - `/rest/ip/neighbor`
  - `/rest/ip/arp`
  - `/rest/ipv6/neighbor`
  - `/rest/ip/firewall/connection`
  - `/rest/ipv6/firewall/connection`
- Current capability limits observed from the live router:
  - `/rest/system/health` is present but currently reports `state=disabled`, which means hardware-health values such as temperature are not available on this current CHR VM deployment.
  - `/rest/ip/accounting` is not available on this router via REST, so per-host cumulative traffic and protocol/category traffic are not directly exposed through the currently verified data surfaces.
  - Interface live rates are available from `/rest/interface/monitor-traffic`, so 5-minute dynamic charts are feasible through polling plus local time-series retention.
  - IPv4 and IPv6 neighbor / connection data both exist, so unified dual-stack client monitoring appears feasible if the panel correlates records by MAC and address sets.

## Requirements

### Product Intent

- The panel is primarily for statistics and monitoring display.
- The panel does not need to become a general RouterOS configuration console.
- `v1` should remain strictly read-only against RouterOS.
- Wireless, AC, AP, camera, switch, and peripheral monitoring are explicitly out of scope for the initial target.

### System Overview

- Show router uptime.
- Show CPU usage.
- Show memory usage.
- Show total upload and download rate.
- Show physical connection / interface status summary.
- Show interface status cards or equivalent compact interface visualization.
- Show a dynamically updating upload/download chart for the most recent 5 minutes.
- Consider a "last 30 minutes protocol traffic distribution" widget only if the live RouterOS data surface can support a meaningful version of it.

### Status Monitoring

- Prioritize terminal monitoring above all other status-monitoring modules.
- Terminal monitoring should aim to present IPv4 and IPv6 together in one unified view rather than split into separate tables.
- Per terminal, prefer to show:
  - IP address set
  - MAC address
  - Connection count
  - Current upload rate
  - Current download rate
  - Cumulative upload
  - Cumulative download
- For `v1`, cumulative upload/download should be implemented as panel-side persisted totals rather than a session-only approximation.
- For line / protocol / policy / load / split-flow monitoring, the panel should include only what is supportable, and must clearly distinguish:
  - not supported by RouterOS itself
  - supportable by RouterOS after enabling or adding configuration
  - supportable immediately on the current live router
- `v1` should not ship half-supported monitoring modules merely to mirror iKuai navigation breadth.

### Capability Reporting

- Before implementation starts, planning must produce a capability matrix for the requested iKuai-like widgets.
- The matrix must separate:
  - feasible now from current live RouterOS state
  - feasible after RouterOS configuration changes
  - not realistically available from RouterOS native monitoring data

### Capability Matrix

| Area | Requested item | Classification | Notes |
| --- | --- | --- | --- |
| System overview | Uptime | Supported now | Available from `/rest/system/resource`. |
| System overview | CPU usage | Supported now | Available from `/rest/system/resource`. |
| System overview | Memory usage | Supported now | Available from `/rest/system/resource` free/total memory. |
| System overview | Total upload/download rate | Supported now | Available by polling `/rest/interface/monitor-traffic` on key interfaces and aggregating. |
| System overview | Physical connection count / interface summary | Supported now | Available from `/rest/interface` and `/rest/interface/ethernet`. |
| System overview | Interface status | Supported now | Available from `/rest/interface` and `/rest/interface/ethernet`. |
| System overview | Recent 5-minute up/down chart | Supported now | Requires panel-side polling and short-window local retention. |
| System overview | Recent 30-minute protocol traffic distribution | Not natively feasible for `v1` | Current live REST surface does not expose ready-made protocol/category accounting comparable to iKuai. |
| Status monitoring | Unified IPv4/IPv6 terminal table | Supported now | Feasible by correlating IPv4 ARP/DHCP, IPv6 neighbor, and v4/v6 firewall connections. |
| Status monitoring | Per-terminal connection count | Supported now | Derived from v4/v6 firewall connection tables. |
| Status monitoring | Per-terminal current up/down rate | Supported now | Derived from `orig-rate` / `repl-rate` in v4/v6 firewall connection tables. |
| Status monitoring | Per-terminal cumulative up/down | Supported with panel persistence | RouterOS does not provide the desired presentation directly; panel must persist totals locally. |
| Status monitoring | Line/interface status | Supported now | Available from interface status and monitor endpoints. |
| Status monitoring | Protocol / policy / load / split-flow pages | Deferred | Reserved for later only if future RouterOS data plus panel logic can make them genuinely useful. |
| Status monitoring | Camera / switch / peripheral monitoring | Out of scope | Explicitly excluded by the user. |

### MVP Shape

- The first usable version should focus on the user's current single live router at `10.0.0.1` and deliver a real dashboard rather than a marketing or placeholder screen.
- The application should poll live RouterOS data and retain short-window history locally when RouterOS does not provide the exact historical view directly.
- The application should persist per-terminal cumulative traffic locally so totals survive app restarts and connection churn.
- `v1` is intended for LAN-only use and does not need a working panel login flow, but the architecture should leave a clean place to add authentication later.
- `v1` should be designed to run on an always-on Linux host in the LAN.
- Initial deployment should favor a single native binary plus local data directory and service management, with Docker/container packaging deferred to a later phase.
- `v1` scope should stay tight around:
  - system overview
  - interface / line status
  - unified terminal monitoring
  - concise capability notes for unsupported or config-dependent metrics
- Future expansion can plan for protocol, policy, load, and split-flow monitoring, but those should not appear in `v1` unless they are genuinely complete enough to be useful.

## Acceptance Criteria

- [ ] A planning artifact defines the MVP scope for an initial RouterOS monitoring panel.
- [ ] A capability matrix classifies the requested dashboard sections into "supported now", "supported with RouterOS changes", or "not natively feasible".
- [ ] The MVP requirements include a concrete System Overview view and a concrete terminal-monitoring view.
- [ ] The planning artifacts record the current live-router constraints that affect temperature, protocol distribution, and cumulative per-terminal traffic.
- [ ] The planning artifacts record that `v1` targets a single RouterOS device rather than multi-router management.
- [ ] The planning artifacts record that per-terminal cumulative totals in `v1` come from panel-side persisted accounting.
- [ ] The planning artifacts record that `v1` is LAN-only and intentionally ships without an active login flow while preserving an upgrade path for later authentication.
- [ ] The planning artifacts record that `v1` is designed first for always-on Linux binary deployment, not Docker-first deployment.
- [ ] The planning artifacts record that `v1` is a read-only RouterOS monitoring panel.
- [ ] The planning artifacts record that `v1` intentionally excludes half-supported protocol / policy / load / split-flow modules while preserving an expansion path.
- [ ] Open product decisions that materially change architecture are resolved before implementation starts.

## Out of Scope

- RouterOS settings/configuration management as a general feature set.
- Wireless / AC / AP management.
- Camera monitoring.
- Switch monitoring.
- Peripheral-device monitoring.

## Open Questions

- None currently blocking planning.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
