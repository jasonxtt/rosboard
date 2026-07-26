# Panel Settings Menu Design

## Architecture

Add settings as a normal React view under the existing application shell. The frontend owns presentation and browser-local preferences; the backend owns the effective runtime configuration projection and RouterOS REST connection persistence.

Keep this iteration split by persistence boundary:

- Backend: expose a typed settings snapshot and save RouterOS REST connection or collection settings to the configured YAML path.
- Frontend: render `面板设置` with section navigation, editable connection settings, first-install setup, and browser-local UI preference persistence.
- Runtime config file: remains the startup source of truth for RouterOS credentials and server polling/network fields; saving connection settings exits the process so systemd restarts with the new YAML.

## Backend Contract

Add `GET /api/settings` returning a JSON object shaped around sections instead of leaking the YAML struct directly.

Add `POST /api/settings/connection` to save RouterOS REST connection settings.

Add `POST /api/settings/collection` to save polling intervals, retention, traffic interfaces, and terminal CIDRs. Add `POST /api/settings/restart` for the maintenance restart action.

Proposed payload:

```json
{
  "connection": {
    "apiBasePath": "/api",
    "listenAddress": ":8080",
    "allowedCidrs": ["127.0.0.0/8"],
    "routerosBaseUrl": "http://10.0.0.1",
    "routerosScheme": "http",
    "routerosHost": "10.0.0.1",
    "routerosPort": 80,
    "routerosUsername": "admin",
    "routerosPassword": "secret",
    "routerosPasswordSet": true
  },
  "collection": {
    "pollIntervalSeconds": 10,
    "realtimePollIntervalSeconds": 1,
    "terminalPollIntervalSeconds": 3,
    "sampleRetentionHours": 48,
    "trafficInterfaces": ["pppoe-out1"],
    "terminalCidrs": []
  },
  "diagnostics": {
    "routerName": "RouterOS",
    "version": "7.x",
    "updatedAt": "..."
  }
}
```

The panel treats the YAML `routeros.password` value as the editable RouterOS password field. The API returns it through the explicit `routerosPassword` field and must not JSON-encode the full config struct.

## Frontend Design

Extend `ActiveView` with `settings` and add an icon to the local icon map. Add a first-level sidebar group named `面板设置`; selecting it should open the group, show the connection section by default, close the mobile sidebar, and clear terminal detail state.

The settings view should use left-sidebar second-level section navigation, matching the existing `状态监控` pattern:

- `连接设置`: RouterOS REST scheme, IP/host, port, username, password, API base path, listen address, allowed CIDRs.
- `采集设置`: editable polling intervals, retention, traffic interfaces, and terminal CIDRs.
- `界面设置`: editable browser-local defaults like refresh interval, default landing view, and terminal family preference.
- `维护设置`: redacted export, reset UI preferences, process restart, and diagnostics.

Use semantic CSS tokens and existing shell/menu visual patterns. Do not nest cards inside cards. Desktop connection/UI forms use three columns, collection interval fields use four columns, and collection lists use two half-width fields. Mobile collapses all setting forms to one column. The RouterOS password is masked by default and uses an accessible eye icon button for temporary visibility.

## Data Flow

YAML/env config -> `config.Config` -> API settings projection -> typed frontend settings type -> settings view.

Connection write:

settings/setup form -> `POST /api/settings/connection` -> validate scheme/host/port/user/password -> save YAML -> process exits -> systemd restarts with new config.

Collection write:

collection form -> `POST /api/settings/collection` -> validate positive numeric values and normalize lists -> save YAML -> process exits -> systemd restarts with new config.

Browser-local UI preferences:

`localStorage` -> typed helper with defaults -> React state initialization -> controls -> localStorage updates.

Server listen/allowlist settings remain read-only. RouterOS REST connection and collection settings are writable through the panel.

## Compatibility

Existing API endpoints and dashboard same-origin fetches remain unchanged. `/api/settings` is additive.

Existing `configs/config.local.yaml` and env override behavior remains supported. A missing config file at the `-config` path starts setup defaults and can be created by saving the connection form.

## Risks

- Exposing the RouterOS password is intentional for this LAN-only operator panel; keep the response explicit and avoid leaking unrelated config fields.
- Saving config with an empty config path is invalid; the UI should show the returned error.
- Changing the global refresh state for UI preferences can affect live monitoring feel; keep current 1 second default.
