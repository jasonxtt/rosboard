# Panel Settings Menu

## Goal

Add a first-level sidebar menu named `面板设置` so Rosboard is less tied to hard-coded/local deployment assumptions and exposes the settings operators naturally need to understand or adjust a deployment.

The feature should make current runtime choices visible in the panel and provide a clear path for panel-managed settings without breaking the existing secure YAML startup contract.

## Confirmed Facts

- Rosboard is currently a read-only, single RouterOS target, LAN-only monitoring panel.
- Startup configuration is loaded from YAML and environment overrides in `internal/config/config.go`.
- Required RouterOS YAML fields are `routeros.base_url`, `routeros.username`, and `routeros.password`; local credentials must stay machine-local and out of Git.
- The current config also includes `listen_address`, `data_dir`, `poll_interval_seconds`, `realtime_poll_interval_seconds`, `terminal_poll_interval_seconds`, `sample_retention_hours`, `allowed_cidrs`, `routeros.traffic_interfaces`, and `routeros.terminal_cidrs`.
- The frontend currently has sidebar first-level menus for `系统概览` and grouped `状态监控`; there is no settings view and no frontend settings persistence.
- The frontend fetches same-origin API paths such as `/api/dashboard`, `/api/realtime`, `/api/load`, and terminal detail endpoints; the API base path is not currently configurable in the browser.
- Existing runtime configuration guidance treats `configs/config.local.yaml` as the source for secrets and explicitly avoids embedding credentials in scripts or tracked files.
- Rosboard uses RouterOS REST over HTTP/HTTPS, not the classic RouterOS API service; REST defaults are HTTP port 80 and HTTPS port 443.

## Requirements

- R1. Add `面板设置` as a first-level sidebar menu item, visually consistent with existing menu items and mobile drawer behavior.
- R2. Add second-level settings navigation under `面板设置` in the left sidebar, matching the existing `状态监控` expansion pattern and using section-appropriate icons.
- R3. Include a `连接设置` section that covers RouterOS/API deployment fields needed for general use:
  - RouterOS REST scheme, IP/host, and port with HTTP 80 as the default.
  - RouterOS username.
  - RouterOS password value for editing.
  - Panel API mode/base path for browser requests, with the current same-origin `/api` behavior as the default.
  - Server listen address and API allowlist (`allowed_cidrs`) visibility.
- R4. Include a `采集设置` section for monitoring behavior:
  - Full polling interval.
  - Realtime polling interval.
  - Terminal polling interval.
  - Sample retention hours.
  - Traffic interfaces.
  - Terminal CIDRs.
- R5. Include a `界面设置` section for browser-local presentation preferences:
  - Global refresh selector default.
  - Default landing view.
  - Terminal family/default scope preference.
  - Theme placeholder only if it does not imply an implemented dark mode.
- R6. Include a `维护设置` section for safe operations:
  - Export current effective settings with the RouterOS password redacted.
  - Reset browser-local UI preferences.
  - Restart the panel service.
  - Show version/diagnostic summary already available from dashboard data.
- R7. Preserve the existing secure local YAML contract: real RouterOS password must be written only to the configured local YAML file and must not be logged or written to tracked files.
- R8. Allow saving RouterOS REST connection settings from the panel and restart the process so systemd reloads the new config.
- R9. Keep the first implementation compatible with existing single-target monitoring and same-origin API calls.
- R10. First installation must start a setup UI when the config file is missing or RouterOS is not configured, using `10.0.0.1:80` as the default RouterOS REST target.
- R11. RouterOS passwords must render as masked password inputs by default, with an accessible eye button to temporarily show or hide the value.
- R12. Collection settings must be editable from the panel and persisted to YAML; saving restarts the process so the running collectors use the new values.
- R13. UI preferences must use an explicit save action instead of applying an unfinished draft immediately.

## Acceptance Criteria

- [ ] Sidebar shows `面板设置` as a first-level item and it is reachable on desktop and mobile.
- [ ] The left sidebar shows clear second-level sections for connection, collection, UI, and maintenance settings under `面板设置`.
- [ ] Current effective config values render from a backend-owned typed API contract.
- [ ] RouterOS REST IP/host, port, username, and password can be saved from the panel.
- [ ] Missing config or missing RouterOS credentials shows an initialization page instead of preventing the panel from starting.
- [ ] Browser-local UI settings, if implemented, persist across reloads without changing server YAML.
- [ ] Collection intervals, retention, traffic interfaces, and terminal CIDRs can be saved from the panel.
- [ ] Maintenance actions export redacted settings, reset browser preferences, and restart the panel.
- [ ] Desktop setting forms use the available width without sparse two-column rows; mobile forms collapse to one column without horizontal overflow.
- [ ] Same-origin `/api` remains the default API path, so existing deployments keep working.
- [ ] Existing dashboard monitoring views still render and refresh normally.
- [ ] Validation covers backend settings projection and frontend build/type safety.

## Out Of Scope

- Multi-router management.
- Writing configuration directly back to RouterOS.
- A full authentication/user management system.
- Dark mode implementation unless separately requested.
