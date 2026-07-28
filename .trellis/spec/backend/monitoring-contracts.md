# Monitoring Contracts

## Scenario: Multi-device monitoring and route attribution

### Contracts

- `MonitorManager` owns one independent Monitor, RouterOS client, scheduler locks, snapshot, and retry state for each enabled non-archived device.
- One viewer heartbeat activates every enabled Monitor; existing no-viewer idle cadence remains.
- Device-scoped APIs accept `?device=<id>` and fall back to the first enabled device only when omitted. Unknown IDs return 404; disabled/archived IDs return 503.
- `/api/traffic-history` accepts only `5m`, `1h`, `6h`, and `24h`, averages deterministic buckets, and returns at most 360 chronological points.
- Route attribution covers IPv4 and IPv6. RouterOS routing marks are source facts; rule and route selection are inferred from ordered rules, lookup fallback, active routes, longest prefix, distance, and ECMP.
- A rule requiring unavailable input yields unavailable attribution instead of a guessed match. ECMP exposes all equal-best gateways.
- Route `currentMatches` counts current conntrack rows and is never described as a lifetime counter. Native mangle packet/byte counters remain exact RouterOS counters.
- Monitoring and attribution remain read-only and never create or edit RouterOS rules.

### Tests Required

- Route matcher: ordered rule, routing mark, longest prefix, lookup fallback, disabled/inactive routes, `min-prefix`, IPv6, ECMP, and unavailable input.
- Store/API: traffic ranges are bounded and scoped by device.
- Race: one failed device retry does not block healthy device snapshots or heartbeat fan-out.

## Scenario: Viewer-aware idle polling

### 1. Scope / Trigger

- Trigger: changes to viewer heartbeats, page visibility handling, active/idle Monitor scheduling, or polling while no dashboard is being viewed.

### 2. Signatures

- API: `POST /api/viewer-heartbeat` returns `{ "activeUntil": <RFC3339 timestamp> }`; other methods return 405 with `Allow: POST`.
- Service: `ViewerHeartbeat()` extends activity by 30 seconds. `viewerHeartbeatTTL` is 30 seconds and `idlePollInterval` is 60 seconds.
- Frontend: a visible document sends a heartbeat immediately, every 10 seconds, and on `visibilitychange` back to visible.

### 3. Contracts

- Any visible Rosboard page counts as a viewer. Multiple tabs share one process-local `activeUntil`; no viewer identity is persisted.
- The first heartbeat after expiry sends one non-blocking wake signal to each scheduler. Renewal heartbeats only extend the deadline and never trigger additional immediate collections.
- Active mode retains 1-second realtime, 5-second terminal discovery, and 10-second full polling. A visible terminal page additionally enables one-second conntrack-only terminal-rate polling; it stops after the terminal-viewer TTL expires. After general activity expires, realtime and terminal discovery stop; one full refresh runs every 60 seconds.
- React sends no heartbeat while `document.visibilityState !== "visible"`. Closing or hiding the last page requires no unload request; activity expires naturally.
- The heartbeat handler updates memory and returns without waiting for RouterOS. Scheduler wake-up performs the immediate realtime/full refresh asynchronously.

### 4. Validation & Error Matrix

- GET/PUT heartbeat -> HTTP 405 and `Allow: POST`.
- Repeated heartbeat inside TTL -> extend `activeUntil`; do not enqueue another wake transition.
- No heartbeat for 30 seconds -> active mode expires without an alert.
- Stale wake signal while already idle -> at most one buffered signal; channels never block the API handler.
- Page becomes visible after idle -> heartbeat immediately restores active scheduling; existing snapshot remains readable during refresh.

### 5. Good/Base/Bad Cases

- Good: the last page closes, `updatedAt` stops advancing each second, then advances once around the next 60-second idle refresh.
- Base: two visible tabs heartbeat every 10 seconds; closing one does not enter idle while the other remains visible.
- Bad: use `beforeunload` as the only close signal; mobile suspension and crashes leave the server permanently active.
- Bad: call `refresh()` inside the heartbeat HTTP handler; page load waits on RouterOS and simultaneous tabs create a collection storm.

### 6. Tests Required

- Unit: first heartbeat transitions to active, renewal only extends, and activity is false exactly at the deadline.
- API: POST returns a future `activeUntil`; GET returns 405 with `Allow: POST`.
- Race: activity reads/writes and scheduler wake channels pass `go test -race`.
- Live idle: after heartbeat expiry, repeated `/api/realtime` reads show a stable `updatedAt`; after about 60 seconds it advances once.
- Browser wake: loading a visible page restores one-second updates, default chart behavior remains intact, and console/global errors are empty.

### 7. Wrong vs Correct

#### Wrong

```tsx
window.addEventListener('beforeunload', () => fetch('/api/stop-monitoring'))
```

#### Correct

```tsx
if (document.visibilityState === 'visible') {
  void fetch('/api/viewer-heartbeat', { method: 'POST' })
}
// The server TTL handles close, suspension, crashes, and lost networks.
```

## Scenario: Tiered overview polling

### 1. Scope / Trigger

- Trigger: changes to Monitor scheduling, RouterOS polling intervals, overview realtime fields, `/api/realtime`, or the dashboard refresh selector.

### 2. Signatures

- Config: `realtime_poll_interval_seconds` (default 1), `terminal_poll_interval_seconds` (default 5), and legacy/full `poll_interval_seconds` (default 10); all are positive integer seconds.
- API: `GET /api/realtime` returns `model.Overview` from the in-memory snapshot and never initiates RouterOS work.
- Service: `refreshRealtime`, `refreshTerminals`, `refreshTerminalRates`, and `refresh` own the realtime, terminal discovery, terminal-rate, and full data layers respectively.

### 3. Contracts

- The realtime scheduler runs independently from the terminal/full scheduler. A slow conntrack or route read must not block CPU, memory, or selected-WAN traffic sampling.
- Terminal discovery and full refreshes share `refreshMu` and never overlap each other. The terminal-rate worker has its own mutex and reads only IPv4/IPv6 conntrack while a terminal page is visible, so full refreshes cannot freeze displayed terminal rates.
- Realtime snapshot writes use `mu`. A full refresh that started before a newer realtime update preserves the newer CPU, memory, traffic, chart samples, selected interfaces, and `updatedAt` fields when committing.
- Missing one-second chart slots carry forward the last measured rate for display continuity. The next actual measurement is not interpolated or altered.
- Overview and terminal-list clients poll cached snapshots at the selected interval (default 1 second); selected terminal details poll their cached snapshot each second. Neither browser endpoint directly polls RouterOS.

### 4. Validation & Error Matrix

- Any configured interval <= 0 -> configuration load fails with the owning YAML key in the error.
- Realtime RouterOS failure -> keep the last valid overview and record a refresh alert; the next tick retries.
- Terminal/full failure -> keep the last valid snapshot for that layer; realtime polling continues.
- Realtime response arrives before an older dashboard response -> frontend retains the newer Overview by comparing `updatedAt`.
- RouterOS response exceeds one second -> no overlapping realtime request is started; missing visual seconds use the previous valid sample.

### 5. Good/Base/Bad Cases

- Good: a 10-second full route refresh is running while 1-second WAN samples continue and the last 30 chart timestamps remain consecutive.
- Base: one RouterOS traffic request is late; the graph carries its previous value for that second and resumes with the next real value.
- Bad: put all three layers behind `refreshMu`; a terminal refresh blocks the graph and timestamps jump from `:13` to `:20`.
- Bad: let a late full refresh replace the entire snapshot and roll `updatedAt` and chart samples backward.

### 6. Tests Required

- Unit: default intervals are 1/3/10 and non-positive realtime/terminal values fail validation.
- Unit: chart gap filling produces consecutive timestamps, retains the last measured value only for missing points, and preserves the next actual sample.
- Go race/behavior: terminal/full writes and realtime writes use the documented lock boundary; full commit preserves newer realtime fields.
- Browser: refresh select defaults to 1 second, chart Canvas loads, live values change, and console/global error output remains empty.
- Live: inspect at least 30 recent `/api/realtime` chart timestamps for one-second continuity while terminal/full scheduling runs; verify local and LAN HTTP 200.

### 7. Wrong vs Correct

#### Wrong

```go
refreshMu.Lock()
defer refreshMu.Unlock()
refreshRealtime(ctx) // blocked behind conntrack and route collection
```

#### Correct

```go
// Realtime owns only its short RouterOS reads and merges through snapshot mu.
go runRealtimeSchedule(ctx)
// Expensive terminal and full refreshes remain mutually exclusive.
go runBackgroundSchedule(ctx)
```

## Scenario: RouterOS self and IP-family terminal attribution

### 1. Scope / Trigger

- Trigger: changes to RouterOS address/connection normalization, terminal identity, `/api/dashboard` terminal fields, or IPv4/IPv6 list metrics.
- RouterOS access remains read-only. Address ownership is derived from REST snapshots and never changes router configuration.

### 2. Signatures

- Router address source: `GET /rest/ip/address` and `GET /rest/ipv6/address`.
- Connection sources: `GET /rest/ip/firewall/connection` and `GET /rest/ipv6/firewall/connection`.
- Stable router terminal ID: `routeros:self`.
- Dashboard projection: `Terminal.FamilyStats map[string]TerminalFamilyStats` with `ipv4` and `ipv6` keys.
- Terminal detail projection: `TerminalConnection.SeenReply` and `TerminalConnection.Assured` mirror RouterOS `seen-reply` and `assured`.
- Terminal connection projection: `TerminalConnection.SourceAddress/SourcePort` and `DestinationAddress/DestinationPort` preserve RouterOS original `src-*` and `dst-*`; upload/download fields remain oriented to the owning terminal.
- Connection identity prefers RouterOS REST `.id` scoped by IP family; the tuple-derived key is only a compatibility fallback when `.id` is absent.
- Overview projection: `Overview.ConnectedDeviceCount` and `Overview.ConnectionCount` serialize as `connectedDeviceCount` and `connectionCount`.
- WAN rate projection: `Overview.UploadBps` is selected-interface TX bit/s; `Overview.DownloadBps` is selected-interface RX bit/s.

### 3. Contracts

- Every enabled address assigned to RouterOS is an exact self identity, including WAN, tunnel, link-local, and loopback addresses.
- WAN/tunnel address prefixes are not terminal CIDRs. Only the exact assigned router address can identify self traffic.
- Terminal CIDRs scope terminal discovery as well as conntrack attribution: DHCP leases, ARP entries, and IPv6 neighbors outside the local CIDR set must not create non-router terminal rows. RouterOS self addresses remain exact identities even when their prefixes are outside terminal CIDRs.
- Original-source LAN terminal ownership wins when both connection endpoints are local; otherwise an exact RouterOS source/reply-source may own the connection.
- All assigned RouterOS addresses merge into `routeros:self`; preferred list addresses come from the `lan` interface when available.
- `familyStats.<family>` contains current connection count/rates plus bytes accumulated by currently active conntrack rows. It is not the persisted all-time total.
- `routeros:self` is displayed as `RouterOS 本机连接跟踪`; its counts are raw conntrack rows, not a claim that every row is bidirectional or generated by RouterOS.
- Winbox flags retain their RouterOS meaning: `S` means `seen-reply=true`; `A` means `assured=true`. `assured` is not a synonym for validity.
- Terminal presence is three-state: `online`, `inactive`, or `offline`. Current connections, RouterOS-assigned addresses, and reachable/permanent ARP or IPv6 neighbor entries are strong online evidence. Bound DHCP leases and complete-but-stale discovery rows identify a device but never advance `lastSeen` or `onlineSince`.
- When strong evidence disappears, preserve the last strong `lastSeen`: emit `inactive` for at most five minutes, then `offline`. Only `online` contributes to online duration; entering `inactive` ends the current online interval.
- `connectedDeviceCount` includes only `state == online` LAN terminals. It excludes `routeros:self`, inactive/offline terminals, selected traffic interfaces, loopback, and interfaces identified as WAN. An online terminal with no known interface remains countable so missing neighbor metadata does not hide a real device.
- `connectionCount` is the raw RouterOS snapshot size: `len(ipv4 conntrack) + len(ipv6 conntrack)`. Do not derive this value by summing terminal connections because attribution intentionally omits external/unmatched rows.
- Attributed terminal-detail rows preserve the original src/dst tuple while upload/download rates and bytes remain oriented to the matched terminal.
- The selected WAN/PPPoE interface defines overview direction: TX is upload and RX is download. The UI names the metric as WAN traffic and displays the sampled interface instead of implying an all-interface aggregate.
- `ROSBOARD_LISTEN_ADDRESS=0.0.0.0:8080` is the review/development delivery binding so LAN devices can open the built panel.
- A live review endpoint uses the real RouterOS REST client. Replay data must be visibly identified and must not remain on port 8080 for live acceptance.

### 4. Validation & Error Matrix

- Disabled RouterOS address -> exclude from exact self identities.
- Invalid address/CIDR text -> ignore that address.
- Exact RouterOS WAN address -> RouterOS self is eligible.
- Different address in the same WAN prefix -> remain external.
- LAN terminal original source plus RouterOS reply source -> retain LAN terminal ownership.
- Missing `familyStats` in an older dashboard payload -> frontend falls back to combined terminal metrics.
- RouterOS self/known WAN/offline terminal -> exclude from `connectedDeviceCount`.
- Online terminal with an unknown interface -> retain in `connectedDeviceCount`.
- `complete=true,status=stale` ARP with no connection -> do not advance `lastSeen`; emit `inactive` only while the previous strong `lastSeen` is within five minutes, otherwise `offline`.
- Bound DHCP lease with no other strong evidence -> retain identity/address metadata but do not mark online.
- IPv4 or IPv6 conntrack changes between polls -> update `connectionCount` on the next snapshot; a later Winbox read can differ as rows expire or appear.
- Selected WAN monitor returns TX/RX -> expose TX as upload and RX as download without byte conversion in the API; UI formatting converts bit/s to B/s.
- `seen-reply=false` -> keep the row for Winbox parity and display `未见回包` with no `S` flag.
- `seen-reply=true, assured=false` -> display `S` and `已见回包`.
- `seen-reply=true, assured=true` -> display `S A` and `已见回包 · Assured`.

### 5. Good/Base/Bad Cases

- Good: `reply-src-address` equals the PPPoE IPv6 assigned to RouterOS; count it under `routeros:self`.
- Good: overview connection count matches the two raw conntrack array lengths from the same poll.
- Good: overview shows `TX · pppoe-out1` for WAN upload and `RX · pppoe-out1` for WAN download.
- Good: a quiet terminal transitions `online -> inactive -> offline` after strong evidence disappears, while its `lastSeen` remains the timestamp of the last strong evidence.
- Base: a LAN IPv6 source connects through RouterOS to the internet; count it under the MAC-correlated LAN terminal.
- Bad: treat the entire PPPoE `/64` as local and create terminals for arbitrary internet addresses.
- Bad: label every RouterOS-owned conntrack row as an established RouterOS-generated connection.
- Bad: count RouterOS self as a connected LAN device, or use a terminal-attributed subtotal as RouterOS total connections.
- Bad: accept a replay snapshot with one address and zero monitor traffic as evidence that live terminal/rate behavior works.
- Bad: treat `complete=true` alone as proof that an ARP entry is current; RouterOS can retain complete stale rows for powered-off devices.

### 6. Tests Required

- Unit: exact router WAN address is oriented as RouterOS self with reply-side upload/download direction.
- Unit: another address in the WAN prefix is rejected.
- Unit: DHCP/ARP/IPv6-neighbor discovery outside terminal CIDRs is rejected for non-router terminals while exact RouterOS self remains visible.
- Unit: LAN original-source ownership is not stolen by a router reply-source address.
- Unit: a reply-side LAN terminal retains the raw RouterOS src/dst tuple while upload/download use the terminal direction.
- Unit: all textual forms of assigned router addresses resolve to `routeros:self`; disabled addresses do not.
- Unit: connected LAN device count excludes RouterOS self, offline, WAN, and selected traffic-interface terminals while retaining online unknown-interface terminals.
- Unit: reachable ARP is online; a later stale-only poll is inactive inside five minutes and offline after five minutes, without changing `lastSeen`.
- Unit: bound DHCP and first-seen stale ARP are weak evidence and do not become online.
- Unit: selected interface TX maps to overview upload and RX maps to overview download; unselected interfaces are ignored.
- Integration/browser: RouterOS appears once in All/IPv4/IPv6 lists and scoped list metrics match scoped detail metrics at the same snapshot.
- Integration/browser: `connectedDeviceCount` and `connectionCount` render after memory; connection count equals raw IPv4+IPv6 conntrack lengths from that monitor snapshot.
- Integration/browser: overview and topbar label rates as WAN traffic and identify the selected interface.
- Integration/browser: unreplied UDP input rows show `- - / 未见回包`; replied assured rows show `S A / 已见回包 · Assured`.
- Delivery: verify HTTP 200 through both `127.0.0.1:8080` and the Mac LAN address.

### 7. Wrong vs Correct

#### Wrong

```go
// Expanding a WAN-assigned prefix makes arbitrary external peers look local.
localCIDRs = append(localCIDRs, routerWANPrefix)
```

#### Correct

```go
// Exact ownership identifies only the address actually assigned to RouterOS.
routerAddresses[assignedIP(address.Address)] = routerAssignedAddress{
    Family: family,
    Interface: address.Interface,
}
```

#### Wrong overview aggregation

```go
// Terminal attribution is intentionally incomplete and self is not a LAN device.
connectedDevices := onlineTerminals
connectionCount := sumTerminalConnectionCounts(terminals)
```

#### Correct overview aggregation

```go
connectedDevices := connectedLANDeviceCount(terminals, trafficInterfaces)
connectionCount := len(connectionsV4) + len(connectionsV6)
uploadBps := totalSelectedTXBps(trafficRates, trafficInterfaces)
downloadBps := totalSelectedRXBps(trafficRates, trafficInterfaces)
```

#### Wrong presence evidence

```go
if parseBool(entry.Complete) {
    markOnline(builder)
}
```

#### Correct presence evidence

```go
if strings.EqualFold(entry.Status, "reachable") || strings.EqualFold(entry.Status, "permanent") {
    markOnline(builder)
}
// Stale/complete rows may populate identity metadata, but never lastSeen.
```

## Scenario: Terminal scope summaries

### 1. Scope / Trigger

- Trigger: changes to terminal-list header totals, LAN-terminal eligibility, family statistics, or the `terminalScopeSummaries` dashboard payload.

### 2. Signatures

- Dashboard projection: `terminalScopeSummaries.{all,ipv4,ipv6}` contains `deviceCount`, `connectionCount`, `currentUploadBps`, `currentDownloadBps`, `activeUploadBytes`, and `activeDownloadBytes`.
- Service aggregation: `terminalScopeSummaries(terminals, trafficInterfaces)` runs after both terminal-only and full refreshes.

### 3. Contracts

- Aggregate from the complete terminal snapshot before frontend search, state/interface filters, sorting, pagination, or page-size state.
- All six values use the same online-LAN eligibility predicate as `connectedDeviceCount`: exclude `routeros:self`, inactive/offline terminals, selected traffic interfaces, loopback, and WAN-like interfaces; retain an online terminal whose interface is unknown.
- `all.deviceCount` deduplicates dual-stack terminals. Family device counts require at least one address in that family.
- Connection, rate, and active-byte values come only from `Terminal.FamilyStats`. All-scope values equal IPv4 plus IPv6 for the same snapshot.
- `activeUploadBytes` and `activeDownloadBytes` are active conntrack cumulative bytes, not persisted terminal lifetime totals.

### 4. Validation & Error Matrix

- Empty terminal snapshot -> return all three keys with six zero values.
- Missing family stats -> add zero for that family.
- Family address missing -> exclude only from that family's device count; never synthesize an address or row.
- Search/filter/pagination change -> summary remains unchanged while the table result count may change.

### 5. Good/Base/Bad Cases

- Good: one dual-stack client contributes once to `all.deviceCount`, once to each family device count, and both family traffic totals to `all`.
- Base: an online IPv4-only LAN client contributes to all and IPv4, but not IPv6 device count.
- Bad: aggregate from filtered React rows or use `TotalUploadBytes` / `TotalDownloadBytes` in the summary.

### 6. Tests Required

- Unit: mixed dual-stack, IPv4-only, and IPv6-only terminals preserve the all-equals-family-sum invariant for connection/rate/active bytes.
- Unit: RouterOS self, inactive/offline, selected-interface, WAN, and loopback terminals contribute to none of the six fields.
- Unit: empty input returns `all`, `ipv4`, and `ipv6` zero summaries.
- Browser: switching scope changes the six values; search, table filters, sorting, and pagination do not.

### 7. Wrong vs Correct

#### Wrong

```tsx
const summary = aggregate(filteredTerminals)
```

#### Correct

```tsx
const summary = dashboard.terminalScopeSummaries[terminalFamily]
```

## Scenario: Overview composition counts

### 1. Scope / Trigger

- Trigger: changes to overview terminal-state composition, connection-protocol composition, terminal/full refresh projections, or the metric-card composition bars.

### 2. Signatures

- `Overview.TerminalStateCounts` serializes as `terminalStateCounts: { online, inactive, offline }`.
- `Overview.ConnectionProtocolCounts` serializes as `connectionProtocolCounts: { tcp, udp, other }`.
- Service projections: `terminalStateCounts(terminals, trafficInterfaces)` and `connectionProtocolCounts(connectionsV4, connectionsV6)`.

### 3. Contracts

- Terminal-state counts use the same LAN scope as `connectedDeviceCount`: exclude `routeros:self`, selected traffic interfaces, loopback, and WAN-like interfaces; retain unknown-interface terminals. Unlike `connectedDeviceCount`, the composition includes all three presence states. `terminalStateCounts.online == connectedDeviceCount` for the same snapshot.
- Protocol counts traverse the raw IPv4 and IPv6 conntrack arrays. Case-insensitive `tcp` and `udp` map to their named buckets; every other or empty protocol maps to `other`.
- `tcp + udp + other == connectionCount`. Never derive protocol composition from `ProtocolStat`, terminal attribution, or application categories because those projections intentionally have different grouping/scope.
- Full and terminal-only refreshes update the two totals and both compositions under the same snapshot lock.

### 4. Validation & Error Matrix

- Empty terminal/connection inputs -> all six count fields are zero.
- Inactive/offline LAN terminal -> included only in its state bucket, never in `connectedDeviceCount`.
- Unknown connection protocol -> counted under `other`, never dropped.
- Missing composition fields from an older server during rolling delivery -> frontend uses explicit zero/unknown fallbacks; backend does not infer historical composition.

### 5. Good/Base/Bad Cases

- Good: 20 online, 1 inactive, and 13 offline eligible LAN terminals produce `20/1/13`, while `connectedDeviceCount` remains 20.
- Base: 165 TCP, 123 UDP, and 2 ICMP rows produce `165/123/2` and `connectionCount=290`.
- Bad: sum application-level `ProtocolStat.Kind` values; one application bucket can contain connections whose original protocols differ.

### 6. Tests Required

- Unit: terminal composition excludes self/WAN/selected interfaces, retains unknown interfaces, covers three states, and asserts `online == connectedLANDeviceCount(...)`.
- Unit: mixed-case TCP/UDP plus ICMP/empty values assert exact buckets and total equality with both raw conntrack slices.
- Browser/API: the two composition sums match their displayed overview totals at one terminal refresh snapshot.

### 7. Wrong vs Correct

#### Wrong

```go
counts := connectionProtocolCountsFromProtocolStats(snapshot.Protocols)
```

#### Correct

```go
counts := connectionProtocolCounts(connectionsV4, connectionsV6)
states := terminalStateCounts(terminals, trafficInterfaces)
```

## Scenario: Editable terminal metadata

### 1. Scope / Trigger

- Trigger: changes to terminal display names, remarks, metadata API handlers, or polling behavior while an edit dialog is open.
- User names and remarks are panel-local state; they never write RouterOS configuration.

### 2. Signatures

- Database: `terminals.custom_name TEXT NOT NULL DEFAULT ''`; `display_name` remains the automatically discovered name.
- Store: `UpdateTerminalMetadata(ctx, terminalID, customName, remark string) error`.
- Service: `UpdateTerminalMetadata(ctx, id, customName, remark string) (model.TerminalDetail, error)`.
- API: `POST /api/terminals/{id}/metadata` with `{customName, remark}` and a `TerminalDetail` response.
- Payload: `Terminal.autoName`, `Terminal.customName`, and effective `Terminal.displayName`.

### 3. Contracts

- Effective name precedence is `customName > autoName > primary IPv4 > primary IPv6 > MAC > 未命名设备`.
- DHCP comment/hostname is automatic evidence; IP and MAC are display fallbacks, not recognized model names.
- Empty `customName` restores automatic/fallback naming without changing `remark`.
- Metadata save is serialized with full collection by `Monitor.refreshMu`, then updates SQLite and the current dashboard/detail/family-summary projections under the monitor snapshot lock. It must not call full RouterOS `refresh()`.
- Frontend edit draft state is keyed by a separate editing terminal ID. Dashboard/detail polling must never overwrite an open draft.

### 4. Validation & Error Matrix

- Invalid JSON -> HTTP 400.
- `customName` over 100 Unicode code points or `remark` over 500 -> HTTP 400.
- Unknown terminal ID -> HTTP 404 / `store.ErrTerminalNotFound`.
- SQLite failure -> HTTP 500; keep the frontend dialog and draft open.
- Successful local update -> HTTP 200, close the dialog, update list and detail without waiting for a poll.

### 5. Good/Base/Bad Cases

- Good: DHCP reports `iPhone`, user saves `iPhone 13 PM`; list shows the custom name and the dialog still shows `iPhone` as automatic.
- Base: no automatic or custom name; list uses the primary IP and labels automatic detection as unavailable.
- Good: clearing custom name preserves the remark and restores the automatic name.
- Bad: saving a remark calls full RouterOS refresh and returns HTTP 500 after SQLite already committed.
- Bad: a 3-second detail poll calls a draft setter and erases text being entered.

### 6. Tests Required

- Store: metadata persists, survives reload, and an unknown ID returns `ErrTerminalNotFound`.
- Service: one update changes dashboard, terminal detail, and family summaries consistently without a RouterOS client.
- Unit: effective-name precedence and MAC/IP auto-name rejection.
- Browser: type for longer than two poll intervals, assert both drafts remain unchanged, save, assert dialog closes and refresh preserves values.

### 7. Wrong vs Correct

#### Wrong

```go
store.UpdateTerminalRemark(ctx, id, remark)
return monitor.refresh(ctx)
```

```tsx
// A background poll must not own an active form draft.
setRemarkDraft(payload.terminal.remark)
```

#### Correct

```go
store.UpdateTerminalMetadata(ctx, id, customName, remark)
// Patch the current snapshot/detail projections under Monitor.mu.
```

```tsx
// Initialize once when opening; polling updates server state only.
setEditingTerminalID(terminal.id)
setCustomNameDraft(terminal.customName)
setRemarkDraft(terminal.remark)
```

## Scenario: RouterOS terminal topology scope

### 1. Scope / Trigger

- Trigger: changing RouterOS terminal discovery, connection orientation, `terminal_scope`, or the dashboard terminal-scope projection.

### 2. Signatures

- Config: `RouterOSConfig.TerminalScope { mode, include_interfaces, exclude_interfaces, include_cidrs, exclude_cidrs }` and legacy `terminal_cidrs`.
- Dashboard: `terminalScope { mode, legacy, interfaces[], prefixes[], warnings[], overridesApplied }`.
- Service: `deriveTerminalScope(...)` is pure and consumes batched RouterOS snapshots.

### 3. Contracts

- RouterOS self is exact assigned-address matching only and always merges to `routeros:self`; never widen a WAN/tunnel self address to its prefix.
- Interface lists resolve include, exclude, then enabled static members. DHCP server/IPv6 ND are LAN evidence; direct-interface default routes and DHCP clients are WAN evidence.
- A route expressed as `gateway-ip%lan` proves a next hop is reachable through LAN, not that the LAN interface is WAN. Only a direct interface gateway, such as `pppoe-out1`, is default-route WAN evidence.
- Empty terminal scope never accepts all DHCP, ARP, neighbor, or conntrack addresses. IPv6 neighbors never create `/128` scope prefixes.
- Auxiliary topology permission/feature failures are warnings, not full-refresh failures.

### 4. Validation & Error Matrix

- Include/exclude same interface -> config/API validation error.
- Conflicting genuine strong LAN/WAN evidence -> UNKNOWN plus warning; no automatic prefix.
- Invalid optional topology endpoint -> warning and remaining evidence still publishes.
- Missing scope arrays in a legacy/early dashboard snapshot -> API remains valid; UI must treat them as empty arrays.

### 5. Good/Base/Bad Cases

- Good: `lan` has LAN-list/DHCP evidence and a default route via `10.0.0.99%lan`; it remains LAN and `10.0.0.0/24` is eligible.
- Good: private address on `wan-xray` with direct default-route evidence remains WAN.
- Bad: accepting all terminal sources when no prefix exists, or classifying WireGuard as LAN solely because it has `10.x`.

### 6. Tests Required

- Pure topology tests cover list order/cycles, direct versus next-hop default routes, WAN-private/tunnel rejection, IPv6 ND prefix and no neighbor `/128` behavior.
- Service tests retain exact RouterOS self ownership, MAC merge, and empty-scope rejection.
- API/UI tests verify the projection and tolerate absent arrays without rendering failure.

### 7. Wrong vs Correct

#### Wrong

```go
if _, interfaceName, ok := strings.Cut(route.ImmediateGateway, "%"); ok {
	markWAN(interfaceName)
}
```

#### Correct

```go
// Only a direct interface gateway is unambiguous WAN evidence.
if name := directRouteInterface(route.ImmediateGateway); name != "" {
	markWAN(name)
}
```
