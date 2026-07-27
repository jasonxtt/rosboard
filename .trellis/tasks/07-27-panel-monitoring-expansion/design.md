# Panel monitoring capability expansion design

## Architecture

The existing Go process, embedded React frontend, RouterOS REST access, and SQLite database remain the deployment shape. The change introduces a stable device boundary through every layer:

```text
YAML devices -> MonitorManager -> one Monitor per enabled device
                                -> shared SQLite, every row scoped by device_id

browser-selected device -> API `device` parameter -> MonitorManager snapshot/store query
```

The server never stores a process-global "selected device". Each browser keeps its own selection and includes the device ID in monitoring requests, allowing two browsers to view different routers concurrently.

## Configuration Contract

Replace the singular runtime model with a device list while retaining legacy load compatibility:

```yaml
devices:
  - id: default
    name: Main router
    enabled: true
    archived: false
    routeros:
      base_url: http://10.0.0.1:80
      username: rosboard
      password: secret
      traffic_interfaces: [pppoe-out1]
      terminal_cidrs: [10.0.0.0/24]
```

- `id` is generated once, immutable, and used only as an internal scope key.
- `name` is operator-managed and does not depend on RouterOS identity.
- Credentials, traffic-interface selection, and terminal CIDRs are per-device because routers can differ.
- Poll intervals, retention, listen address, API allowlist, and data directory remain global.
- `enabled=false` keeps a visible configured device but stops its monitor.
- `archived=true` hides the device from normal selection and stops collection while retaining configuration and history for restoration.
- A legacy singular `routeros` block is normalized in memory into one device with ID `default`. The first successful settings save writes the new `devices` structure. No credential is moved into SQLite.
- First-install setup creates the first enabled device and then follows the existing save/restart lifecycle.

## SQLite Migration

Use a versioned transactional migration instead of adding more best-effort `ALTER TABLE` calls. Add `device_id TEXT NOT NULL` to the ownership key of every monitoring table:

| Table | New key |
|---|---|
| `interface_samples` | `(device_id, ts, interface_name)` |
| `terminals` | `(device_id, id)` and unique `(device_id, mac)` for non-empty MAC |
| `terminal_addresses` | `(device_id, terminal_id, family, address)` |
| `terminal_totals` | `(device_id, terminal_id)` |
| `connection_state` | `(device_id, conn_key)` |
| `terminal_history` | `(device_id, terminal_id, ts)` |
| `load_samples` | `(device_id, ts)` |
| `protocol_samples` | `(device_id, ts, name, kind)` |

The migration rebuilds affected SQLite tables inside one transaction and assigns all legacy rows to `default`. Store methods require a device ID, including terminal merge and metadata updates. Empty device IDs are rejected at the store boundary. Normal device removal performs no row deletion. Permanent purge deletes all rows for one archived device in a transaction and then removes its archived YAML record.

Before deployment, back up both the existing YAML and `rosboard.db`. A migration error aborts startup without modifying the old schema because the transaction rolls back.

## Runtime Monitoring

Add a `MonitorManager` that owns `map[deviceID]*Monitor` plus per-device status. Startup creates one RouterOS client and monitor for every enabled, non-archived device.

- Monitors retain their existing realtime/terminal/full scheduler separation.
- Viewer heartbeat is process-wide and fans out to every enabled monitor. Existing active/idle cadence remains: all enabled devices collect at normal cadence while a viewer is active and at the existing reduced idle cadence when no viewer is present.
- Start or refresh failure updates only that device's status and leaves its last snapshot readable.
- One device's retry/backoff and locks are not shared with another device.
- Add/edit/enable/disable/archive continues to save YAML and restart the service, matching the existing systemd lifecycle. Device selection itself requires no restart.

## API Contracts

### Device management

- `GET /api/devices` returns configured non-archived devices with ID, display name, enabled state, health, RouterOS identity/version, last update, and masked credential status.
- `POST /api/devices` creates a device after validating name, scheme, host, port, username, password, and unique immutable ID.
- `PUT /api/devices/{id}` updates operator name, enabled state, connection, traffic interfaces, and terminal CIDRs.
- `DELETE /api/devices/{id}` archives a device and preserves data.
- `POST /api/devices/{id}/restore` restores an archived device.
- `DELETE /api/devices/{id}/data` permanently purges an archived device only when the explicit confirmation payload matches the device name.

Settings responses continue to expose global collection/UI/maintenance data. The legacy connection settings endpoint remains usable for first setup and maps to the first/default device.

### Device-scoped monitoring

Monitoring endpoints accept `?device=<id>`. Missing device IDs fall back to the first enabled device for backward compatibility; unknown, disabled, or archived IDs return a typed error. Terminal IDs remain local IDs and are always resolved under the request's device scope.

### Dashboard traffic history

Add `GET /api/traffic-history?device=<id>&window=5m|1h|6h|24h`. It returns the selected traffic interfaces, normalized window, and chronologically ordered rate samples. The server averages samples into deterministic time buckets and returns no more than 360 points. Empty buckets are omitted rather than fabricated; the existing realtime path continues to supply fresh samples.

## Route Collection and Attribution

Prefer RouterOS v7 `/rest/routing/route` because it exposes both IPv4 and IPv6 routes and resolved gateway information. Keep the existing IPv4 route path as a compatibility fallback if the richer endpoint is unavailable. Extend routing-rule collection with stable rule ID/order, comment, interface, routing mark, source/destination match, minimum prefix, action, and table.

For each terminal-attributed conntrack row:

1. Determine address family and terminal-oriented source/destination.
2. Treat a RouterOS-provided `routing-mark` as an authoritative input fact; mangle policy has priority over user routing rules.
3. Otherwise evaluate enabled routing rules in RouterOS order. A rule requiring unavailable inputs, such as an unknown incoming interface, makes that branch unavailable rather than falsely matched.
4. Apply `lookup` versus `lookup-only-in-table` fallback semantics.
5. Select active routes in the chosen table by longest destination prefix, then route preference/distance where represented.
6. Represent equal best routes as ECMP/ambiguous and expose all matching gateways rather than choosing one arbitrarily.

Per-connection output includes matched rule, table, destination prefix, gateway(s), match basis, and attribution state (`inferred`, `ambiguous`, or `unavailable`). RouterOS-provided fields are separately identified so the UI does not imply that the final route came directly from conntrack.

The route page counts current conntrack rows attributed to each rule/route for the selected snapshot. These are labeled `current matched connections`. Existing mangle packet/byte values remain RouterOS-native exact counters. RosBoard never creates RouterOS rules for counting.

## Frontend Design

- Upgrade the sidebar device summary into a compact global selector showing the current device name and health. It has no aggregate option.
- Persist the selected ID in browser storage. On startup, validate it against `/api/devices`; if unavailable, choose the first enabled device. After archive/disable, choose the next enabled device.
- Centralize device-scoped URL construction so dashboard, realtime, terminal detail, metadata updates, load, protocol, route, and history calls cannot accidentally omit the device.
- `面板设置 > 连接设置` becomes a device list plus editor. Passwords remain masked with the existing eye/eye-off control.
- Maintenance settings list archived devices with restore and confirmed permanent-purge actions.
- The home traffic chart uses a compact segmented control for `5min`, `1h`, `6h`, and `24h`; selection is stored for the browser session.
- Route rows add address family and current matched-connection count. Terminal connection details add rule/table/route/gateway and attribution-state columns or compact detail fields without disrupting the existing dense monitoring layout.

## Compatibility and Rollback

- Legacy YAML and database rows map to device `default`; no user action is required.
- Existing same-origin API paths remain. Missing `device` retains first-device behavior for older frontend assets during a rolling restart.
- No RouterOS write endpoint is introduced.
- Deployment to `10.0.0.6` must preserve and back up the remote config, database, and old binary before replacement.
- Rollback restores the old binary, YAML, and pre-migration database together. An old binary must not run against the migrated database.

## Key Trade-offs

- YAML remains the credential source of truth, avoiding a second secret store, at the cost of process restart after device edits.
- No aggregate view keeps per-router semantics honest and reduces initial scope.
- Snapshot-derived route counts are useful without RouterOS mutation, but are not lifetime counters.
- Shared SQLite keeps deployment simple; explicit device keys and one connection serialize writes, which is acceptable for the expected small device count and current poll rates. Performance will be verified with concurrent monitor tests.
