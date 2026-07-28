# Automatic ISP traffic scope design

## Architecture

```text
RouterOS read-only topology snapshot
  ├─ deriveTerminalScope(snapshot, terminal config) -> terminalScope
  └─ deriveTrafficScope(snapshot, terminalScope, traffic config) -> trafficScope

full refresh -> publish both immutable scopes -> realtime/history read selected traffic names
```

`TrafficScope` is deliberately not a terminal-role alias. `TerminalScope` answers which interfaces/prefixes identify local terminals; `TrafficScope` answers which logical RouterOS interfaces carry real ISP ingress/egress for panel traffic accounting. Both consume the same interface list/DHCP/ND/route evidence, but no traffic decision changes terminal discovery or counts.

## Data contracts

### RouterOS snapshot

Extend typed GET-only RouterOS reads with `/rest/interface/pppoe-client`, recording actual returned field names before final implementation. Add `PPPoEClient` fields for ID/name/parent interface/disabled/running/status/add-default-route using confirmed JSON tags. Extend `DHCPClient` for `add-default-route` and `default-route-distance` after probe confirmation.

Topology collection remains fault-tolerant. PPPoE helper failure is an optional warning and supplies an empty PPPoE list; `deriveTrafficScope` falls back to `interface.type == pppoe-out`. Existing interface lists, DHCP Clients, routes, and `InterfaceEvidence` are fed directly rather than separately rediscovered.

A small topology snapshot type owns only RouterOS value types and warnings. It can be stored inside the in-memory verification ticket without credentials, then reused to recompute scope previews for include/exclude changes. Endpoint/user/password fingerprint remains the ticket authority.

### Runtime/config/API models

- Add `config.TrafficScopeConfig` to `RouterOSConfig`; zero-value auto configuration serializes as `{ mode: auto, include_interfaces: [], exclude_interfaces: [] }` on new saves.
- Add service-private `trafficScope` and `TrafficInterface`; it provides `selectedNames()` and a `model.TrafficScope` projection with defensive copies/stable order.
- Add `model.TrafficScope` to `DashboardSnapshot`; retain `Overview.TrafficInterfaces` as its selected-name compatibility projection.
- Add matching TypeScript `TrafficScopeConfig`/`TrafficScope`, optional-safe normalizers, settings payload fields, and verification response fields.

Legacy detection is strictly `TrafficInterfaces != empty && TrafficScope.Mode != "auto"`. Legacy skips auto derivation entirely, preserves names/order as configured after normalization, and warns for unavailable/disabled entries. Auto has no hidden YAML materialization of derived names.

## Selection algorithm

`deriveTrafficScope` is a pure service function. It receives config, already-derived terminal scope, interfaces, PPPoE Clients, DHCP Clients, resolved list data, and routes. It first indexes interface state/type and WAN membership, establishes PPPoE parents, then accumulates candidate objects and explanatory reasons.

1. Reject automatic LAN members, tunnel types, known internal forwarding types, and PPPoE parents before lower-priority evidence. The rejection is retained as a warning/reason when relevant.
2. Add enabled PPPoE logical interfaces even when down. PPPoE REST is authoritative; `/interface` type is fallback only.
3. Add enabled DHCP Client interfaces only with bound/default-route/WAN-list/direct-default-route proof. Retain configured, non-bound backups when they use `add-default-route=yes`.
4. Add enabled cellular types/names only when not terminal LAN.
5. Add static WAN only with both WAN-list and active direct IPv4/IPv6 default-route proof; route `gateway%interface` is excluded as non-direct proof.
6. Apply manual include after automatic classification, allowing LAN/tunnel candidates with warnings; apply manual exclude last. Validate include/exclude collisions at config/API boundaries.
7. Sort by classifier category then name. If no selected names remain, issue the explicit manual-include guidance and return no fallback.

Any tunnel exclusion is type-first. Name hints are only weak evidence; they do not become a generic arbitrary-name heuristic.

## Monitor and persistence flow

Full refresh continues sampling every enabled non-loopback interface and saving its independent sample. It derives both scopes before aggregate calculations, samples selected names from the all-interface rate map, and publishes `m.trafficScope` under `m.mu` with the dashboard snapshot.

Realtime obtains a copied `m.trafficScope.selectedNames()` while holding `RLock` briefly. Each selected MonitorTraffic request is isolated: errors log and contribute zero, successful interfaces save samples, and history loading aggregates the current selected names. No RouterOS call occurs under `m.mu`.

`TrafficHistory` also obtains selected names from the immutable scope. SQLite already groups selected interface rows per timestamp by summing RX into Download and TX into Upload; tests will make this contract explicit. Scope transitions query the new set only and do not mutate past rows.

Terminal aggregators receive `terminalScope` (or a scope-derived LAN predicate). Router self stays excluded; unknown-interface terminals retain their existing eligibility. This severs every traffic-name argument from `connectedLANDeviceCount`, `terminalStateCounts`, `terminalScopeSummaries`, and `scopedLANTerminal`.

## Verification and device writes

`routeros.Verify` remains a required-identity/basic-permission check and grows read-only topology collection. The API receives the snapshot, derives both preview scopes using the input draft/default auto config, emits them with the token, and stores the snapshot safely in the ticket.

`prepareDevice` validates traffic overrides before persistence. In legacy mode it preserves selected manual names and retains the existing monitor-traffic permission checks. In auto mode it accepts empty `trafficInterfaces`; its save eligibility is evaluated from the ticket-derived automatic scope or non-empty include override. Override-only re-previews use the ticket snapshot; changed connection fingerprints invalidate the ticket and require retest.

## Frontend state and view

A selected saved device loads its own `/api/dashboard?device=<id>` scope and interfaces; a new draft uses verification scopes. Device switching clears verification and stale scoped data before fetch completion. One `<details>`-style `自动识别与高级设置` region owns compact status, traffic result, terminal result, overrides, and legacy migration. The traffic checkbox picker is removed from normal UI.

## Rollout / rollback

Run local tests/build and GET-only live probes first. Build an embedded frontend binary only after the frontend build. Before replacing the remote service, create a single timestamped bundle with binary, config, SQLite database, WAL, SHM, and service unit when present. Restore the matching bundle for rollback. Deployment verification ends before commit and waits for user approval.
