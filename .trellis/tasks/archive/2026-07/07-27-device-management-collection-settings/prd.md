# Device management collection settings

## Goal

Make panel settings clearly model multi-device RouterOS management: each RouterOS device owns its own REST connection, collection interface selection, and terminal CIDR scope, while process-wide collection intervals remain separate. Settings saves that restart the panel must wait for the service to actually return instead of showing transient fetch errors or reloading into an unavailable page.

## Background

- `devices[].routeros.traffic_interfaces` and `devices[].routeros.terminal_cidrs` are already per-device YAML fields.
- The current UI still shows a global `采集设置` form with `流量接口` and `终端 CIDR`, and `/api/settings/collection` writes those values to `cfg.RouterOS` plus `devices[0]`, which can mix concepts after multi-device support.
- The current per-device editor has raw textareas for `流量接口` and `终端 CIDR`, so it does not preserve the nicer interface-picker tab/card operation the collection form already has.
- The current device save and archived-device restore/purge paths use a fixed `setTimeout(...reload...)` instead of the existing `waitForPanelRestart` health/assets loop, which explains the remaining blank-page / `Failed to fetch` restart race on some save actions.
- Existing terminal CIDR behavior: if explicit terminal CIDRs are empty, the monitor derives local CIDRs from RouterOS IP addresses on non-selected, non-WAN-ish interfaces, and supports multiple IPv4/IPv6 CIDRs.
- Product wording should answer the operator's question: terminal CIDR means the RouterOS-side local terminal subnet(s), usually LAN/VLAN prefixes such as `10.0.0.0/24`; multiple RouterOS LAN IPs/subnets should be represented as multiple CIDR entries.

## Requirements

- Rename the settings subsection `连接设置` to `设备管理` everywhere user-facing in the settings navigation and panel heading.
- Rename user-facing `流量接口` to `采集接口` for the RouterOS interfaces selected for traffic sampling.
- Move `采集接口` and `终端 CIDR` out of global `采集设置`; they belong only to each device in `设备管理`.
- Keep `采集设置` only for process-wide polling and retention values: full poll interval, realtime poll interval, terminal poll interval, and sample retention.
- Make the device editor visually and behaviorally emphasize that settings apply to the selected device only.
- Preserve the interface picker interaction for `采集接口`: available interface options, selected count, checkbox selection, status/type detail, and retention of configured-but-currently-missing interfaces.
- Support multiple terminal CIDRs per device with a picker-style interaction similar to the interface picker, backed by RouterOS discovered interface addresses when available and manual entries when needed.
- Offer terminal CIDR suggestions from the current device's RouterOS interface addresses, including multiple IPv4/IPv6 LAN/VLAN addresses, but do not claim automatic discovery can perfectly identify LAN.
- Exclude selected `采集接口` and clearly WAN-like interfaces from terminal CIDR suggestions when possible, while requiring the operator to review/select the final terminal CIDR list.
- Keep manual terminal CIDR editing possible for unusual topologies, because automatic discovery cannot always infer operator intent.
- Apply the robust restart wait/reload path to all settings/device actions that schedule a panel restart: create device, update device, archive device, restore device, purge archived device data, global collection interval save, legacy connection save if still present, and manual restart.
- During restart wait, suppress expected transient dashboard refresh failures such as `Failed to fetch` so the page stays in an intentional restart state until health and current assets are available.
- Do not add aggregate multi-device monitoring in this task.
- Do not change RouterOS configuration; all discovery remains read-only.

## Acceptance Criteria

- [x] Settings navigation and headings show `设备管理`, not `连接设置`.
- [x] Device management copy and layout make it clear that each device has independent connection, `采集接口`, terminal CIDR, enabled/archive state, and password.
- [x] `采集设置` no longer displays or saves interface/CIDR fields, and saving it does not mutate any device's interface/CIDR arrays.
- [x] Each device editor can select `采集接口` via the existing picker-style operation, including configured interfaces missing from the latest live interface list.
- [x] Each device editor can select or add multiple terminal CIDRs; the UI explains that these are RouterOS local terminal subnet(s), usually LAN/VLAN prefixes.
- [x] Terminal CIDR suggestions are derived from the selected device's RouterOS interface addresses, support multiple IPv4/IPv6 prefixes, and are presented as reviewable suggestions rather than unquestioned auto-filled truth.
- [x] Changing one device's `采集接口` or terminal CIDRs does not change another device's saved values.
- [x] All restart-producing saves use the same health/assets restart wait before reloading; no fixed 3.5-second reload remains on those paths.
- [x] A restart in progress does not surface an expected `最近一次刷新失败: Failed to fetch` global error.
- [x] API/config tests cover that collection settings no longer write device interface/CIDR fields and device settings still persist them per device.
- [x] Frontend build/lint and relevant Go tests pass.
