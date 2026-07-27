# Device Management Collection Settings Design

## Boundaries

- Frontend owns wording, settings form structure, picker interactions, restart waiting, and transient fetch-error suppression.
- Backend owns settings API contracts and YAML persistence boundaries.
- Monitor discovery remains read-only and continues using existing RouterOS REST snapshots.

## Settings Model

- `devices[].routeros.traffic_interfaces` remains the source of truth for per-device `采集接口`.
- `devices[].routeros.terminal_cidrs` remains the source of truth for per-device terminal scopes.
- Global `collection` keeps only process-wide interval and retention fields.
- `/api/settings/collection` should persist only global interval/retention fields. It must not write `RouterOS.TrafficInterfaces`, `RouterOS.TerminalCIDRs`, or `Devices[0].RouterOS.*` interface/CIDR values.
- `/api/devices` create/update remains the per-device persistence path for connection, `采集接口`, and terminal CIDRs.

## Interface And CIDR Hints

- The current dashboard already returns `interfaces[]` for the selected device, including `addresses[]`.
- The device editor should receive the currently selected device ID and current dashboard interfaces.
- When editing the selected device, picker options can use the current dashboard's interface list and addresses.
- When editing another device, keep its saved values visible and do not mix in the selected device's live interface/address hints. The UI can prompt/select the device for live suggestions rather than using the wrong device data.
- CIDR suggestions are derived from interface `addresses[]`. IPv4/IPv6 address strings that include prefixes can be offered directly after filtering.
- RouterOS does not expose a universally reliable "this address is LAN" flag. Suggestions are therefore heuristics: exclude selected collection interfaces, skip disabled/dynamic addresses when the data is available, down-rank names that look WAN/PPPoE/tunnel, and require the operator to review/select the final CIDR list.
- WAN-like or selected collection interfaces should be excluded from terminal CIDR suggestions by default, matching the backend's existing derive-local-CIDR behavior where possible.

## Restart Handling

- Keep one helper that posts a restart-producing action, shows an intentional restart message, waits for `/api/health` to go down/up, verifies current JS/CSS assets, then reloads.
- Device create/update/archive/restore/purge should use this same helper instead of fixed-time reload.
- A boolean restart state in `App` should suppress ordinary dashboard/realtime/device polling fetch failures while a settings restart is expected.
- The restart helper must handle very fast restarts that do not visibly go offline by continuing to poll until health and assets are OK, then reloading.

## Compatibility

- Existing YAML keeps loading unchanged.
- Existing per-device traffic interface and terminal CIDR values are preserved.
- Legacy single-device top-level `routeros` migration continues to normalize into `devices[0]`.
- The old `/api/settings/connection` path may remain for setup/backward compatibility, but user-facing existing settings should route through Device Management.

## Tradeoffs

- Live interface/CIDR suggestions are available only for the currently selected device unless a new backend hint endpoint is added. For this task, avoiding cross-device hint mixing is more important than showing stale guesses for every device.
- LAN detection is intentionally not fully automatic because RouterOS interface names, bridges, VLANs, tunnels, PPPoE, VRRP, and policy-routing designs vary. The UI should make suggestions easy to accept, not silently decide.
- Manual CIDR entry stays available because automatic inference cannot reliably distinguish every LAN, VLAN, tunnel, and WAN topology.
