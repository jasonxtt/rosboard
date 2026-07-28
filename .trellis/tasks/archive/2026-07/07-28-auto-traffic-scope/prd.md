# Automatic ISP traffic scope

## Goal

Replace ordinary manual traffic-interface selection with an explainable automatic `TrafficScope`: the enabled RouterOS interfaces that represent real ISP access and therefore drive overview upload/download, traffic history, and load traffic values. It must support multi-WAN and failure tolerance without altering the independently completed `TerminalScope` behavior.

## Confirmed facts

- The current topology implementation provides a pure `deriveTerminalScope`, `terminalScope`, `InterfaceEvidence`, resolved interface lists, DHCP Server/Client, IPv6 ND, and direct-default-route evidence. It must be reused, not reimplemented or repurposed.
- Current `selectTrafficInterfaces` chooses one running PPPoE/`wan`/arbitrary running interface and remains used by full refresh; realtime/history also fall back to YAML/overview interface names. This is incompatible with multi-WAN and can count an arbitrary LAN.
- Current terminal count, state count, and terminal summaries still infer LAN by excluding `trafficInterfaces`; this must be changed to use `TerminalScope` only.
- `Store.LoadInterfaceSamples` already sums RX/TX rows by interface and timestamp; the task must retain and test this aggregate behavior.
- `traffic_interfaces` is an existing persisted user setting. A non-empty value remains legacy manual mode unless `traffic_scope.mode: auto` is explicitly saved.
- RouterOS access is read-only. The historical live topology task established that GET-only interface-list, DHCP, IPv6 ND, and route menus are readable. PPPoE fields must nevertheless be probed and recorded from the current minimal account before modeling them.

## Requirements

### 1. Scope, compatibility, and configuration

1. Add `TrafficScopeConfig { mode, include_interfaces, exclude_interfaces }` under each `RouterOSConfig`, while retaining `TrafficInterfaces` as the legacy manual field.
2. New saved devices use `traffic_scope.mode: auto`; automatic selected names are runtime output and are never persisted into `traffic_interfaces`.
3. When `traffic_interfaces` is non-empty and `traffic_scope.mode` is not explicitly `auto`, retain exactly those names in a `legacy` TrafficScope; do not add any other WAN. Missing or disabled names appear as warnings.
4. A user action named `恢复自动识别` changes only the browser draft to `trafficScope.mode = auto` and `trafficInterfaces = []`; YAML changes only after Save.
5. Trim, deduplicate, and validate override names. A name present in both include and exclude is HTTP 400/config validation failure, never a silent precedence choice.

### 2. Automatic ISP selection

1. `TrafficScope` and `TerminalScope` share one RouterOS topology snapshot/evidence set but remain separate types, derivations, projections, and responsibilities. Terminal WAN classification is not sufficient ISP-traffic evidence.
2. Include every enabled PPPoE Client logical interface, whether currently running or standby. Prefer `/rest/interface/pppoe-client`; if unavailable, warn and degrade to enabled `/rest/interface` entries with `type=pppoe-out`.
3. Exclude a selected PPPoE client's parent interface (physical or VLAN) from automatic selection to prevent double counting; explain the exclusion. An explicit include may override it.
4. Include an enabled DHCP Client interface only with Internet evidence: bound status, `add-default-route=yes`, WAN interface-list membership, or an enabled direct default route. A standby DHCP client with `add-default-route=yes` remains selected; an internal DHCP client with no such evidence does not.
5. Include static IP WAN only when it is in the WAN interface list, has an enabled direct IPv4/IPv6 default route, is not terminal LAN, and is not a tunnel/internal forwarding type. A `gateway-ip%lan` route is never proof that LAN is ISP access.
6. Include enabled LTE/WWAN/5G/cellular interfaces not classified as terminal LAN.
7. By default exclude WireGuard, TUN/TAP, GRE, IPIP, EOIP, VxLAN, L2TP, SSTP, OpenVPN/OVPN, PPTP, and similar tunnel/internal-forwarding types even if they have routes, WAN-list membership, private addresses, or traffic. Weak names such as `wan-xray`, `sing-box`, `proxy`, `tproxy`, `tun`, `tunnel`, and `transit` are exclusion hints, not the sole hard classifier.
8. Never automatically select an interface that `TerminalScope` identifies as LAN. Manual include is allowed but must produce the LAN/internal-flow warning.
9. Apply priority: manual exclude > manual include > automatic PPPoE/DHCP/LTE/static-WAN evidence > weak hints. Empty automatic results remain empty and show a directive warning; never fall back to a `wan` name or arbitrary running/bridge/LAN interface.
10. Produce stable interface ordering: PPPoE, DHCP, LTE/WWAN, static WAN, manual inclusion, then interface name.

### 3. Monitoring, history, and terminal isolation

1. Add an immutable Monitor `trafficScope` snapshot. Full refresh derives terminal scope, then traffic scope, collects/saves all monitorable interface samples, and publishes both scopes atomically.
2. Overview traffic names, live rates, load traffic values, charts, and traffic history use `trafficScope.selectedNames()`. Sum WAN RX as download and WAN TX as upload across every selected interface.
3. Realtime refresh copies selected names under the monitor read lock and makes RouterOS calls without holding the lock. Per-interface monitor failure logs/records a warning and contributes zero for that refresh; healthy links continue to update.
4. Continue retaining per-interface samples. History reads only the current selected range, sums same time buckets, and never deletes/re-writes prior samples merely because scope changes.
5. Replace/remove the old automatic `selectTrafficInterfaces` fallback after all callers migrate. Delete `deriveLocalCIDRs` only when no references remain.
6. Make connected-device count, terminal state counts, terminal summaries, and LAN-terminal eligibility depend on the immutable `TerminalScope`, never on TrafficScope/selected traffic names. Changing traffic settings must not change terminal count or terminal scope.

### 4. API, verification, and frontend

1. Add runtime/API model `TrafficScope { mode, legacy, interfaces[], warnings[], overridesApplied }` and `TrafficScopeInterface { name, kind, reasons, automatic, running, disabled }` to dashboard alongside the existing `Overview.TrafficInterfaces` compatibility field.
2. Connection verification must collect a read-only topology snapshot and return both TrafficScope and TerminalScope previews. Optional helper failures become warnings. Verification tickets bind endpoint/username/password fingerprint plus the safe topology snapshot (or safe immutable summary) needed to recompute previews.
3. Connection edits require retest; include/exclude-only edits recompute previews from ticket data and do not require credential retest. No response, log, ticket projection, or export exposes a password.
4. Device persistence permits empty `traffic_interfaces` in auto mode. New-device save may not silently choose an arbitrary interface: it requires an automatic interface, valid include, or legacy manual selection according to the selected product flow.
5. When editing a saved device, fetch `/api/dashboard?device=<id>` and use that device's interfaces/scopes. On switch, clear stale verification/scope state and visibly load or warn instead of showing another device's values.
6. Replace the ordinary traffic checkbox grid and ordinary CIDR/interface grids with one collapsed `自动识别与高级设置` entry. Its summary states traffic lines, LAN interfaces, and prefixes (or legacy/attention state). Expanded content shows read-only traffic interfaces with status/reasons, existing terminal evidence, and one advanced override section for both scopes.
7. Legacy traffic mode displays the migration notice and `恢复自动识别` button. TypeScript response/draft arrays normalize missing legacy fields to empty arrays; old/partial dashboard payloads never crash.
8. Replace obsolete onboarding/settings copy with wording that connection testing automatically recognizes ISP lines, LAN interfaces, and local prefixes. Save enablement is based on valid verification/scope/overrides rather than manual traffic checkbox count.

## Acceptance criteria

### Pure derivation and configuration

- [ ] Table tests cover one/two/standby PPPoE, PPPoE-over-ether/VLAN parent exclusion, DHCP bound/standby/internal cases, static WAN, direct-vs-`%lan` route, LTE/WWAN, tunnel/`wan-xray` exclusion, manual include/exclude, conflict rejection, empty result, and exact legacy preservation.
- [ ] Config/API tests cover auto defaults, legacy persistence, restore-auto migration, normalized overrides, conflict HTTP 400, password-safe settings, dashboard scope, verification dual scopes, optional PPPoE helper degradation, and multi-device isolation.

### Monitoring and terminal isolation

- [ ] Two selected WAN interfaces sum RX/download and TX/upload for realtime, overview, load samples, and history buckets.
- [ ] A failed or disconnected standby interface contributes zero without stopping healthy realtime updates; scope retains the standby interface.
- [ ] Parent/PPPoE interfaces are never automatically summed together.
- [ ] Scope changes do not delete historic samples.
- [ ] Terminal count/state/summaries are invariant when TrafficScope changes, including double-WAN and manual internal-interface inclusion cases.

### User experience and operations

- [ ] New devices can preview automatic traffic and terminal scopes after connection test without selecting ordinary interface/CIDR checkboxes.
- [ ] Settings are collapsed by default; expansion presents both scopes and advanced overrides. Legacy restore, device switching, absent arrays, 375px, light/dark theme, and browser console behavior are verified.
- [ ] All required Go tests, race tests, vet, frontend lint/build, local live GET-only RouterOS verification, and embedded-asset verification pass.
- [ ] Before any commit, deploy the verified binary to `network-vm` (`10.0.0.6`) with one timestamped rollback bundle for binary/config/SQLite/WAL/SHM/unit as applicable; verify systemd, health, device dashboard scopes, affected APIs, traffic aggregation, terminal scope preservation, assets, and browser behavior.
- [ ] Stop after deployment for the user's manual inspection and approval at `http://10.0.0.6:8080`; do not commit, archive, journal, or push before approval.

## Out of scope

- Any RouterOS write, interface-list creation, configuration mutation, credential disclosure, SQLite reset, historic data deletion, automatic legacy migration, fallback to arbitrary running interfaces, TerminalScope semantic changes, unrelated refactors, commits, or pushes before remote manual acceptance.
