# Panel Settings Menu Implementation Plan

## Checklist

1. Load pre-development context with `trellis-before-dev`.
2. Add backend settings projection:
   - Add typed settings response model or API-local response structs.
   - Store `cfg` on `api.Server` or otherwise pass it to the handler.
   - Add `GET /api/settings`.
   - Add explicit RouterOS REST connection fields for editing.
   - Add `POST /api/settings/connection` to save RouterOS REST scheme, host, port, username, and password.
   - Allow missing config files to start setup defaults.
3. Add backend tests:
   - `/api/settings` returns effective config values.
   - `/api/settings/connection` writes expected YAML fields.
   - Missing config path loads setup defaults.
   - Collection settings save numeric values and normalized lists.
   - Non-positive collection values are rejected.
4. Add frontend types and settings fetch:
   - Extend `ActiveView` and `viewTitle`.
   - Add `SettingsResponse` types.
   - Add settings view loading/error states.
5. Add sidebar/settings UI:
   - First-level `面板设置`.
   - Second-level settings section navigation.
   - Render connection, collection, UI, and maintenance sections.
   - Render first-install setup when dashboard data is unavailable but settings are readable.
   - Mask the RouterOS password by default and add a show/hide eye button.
   - Use dense three/four-column desktop forms and one-column mobile forms.
6. Add browser-local UI preference helper if included in this iteration:
   - Persist default refresh/landing/terminal family.
   - Reset preferences from maintenance section.
   - Save preference drafts explicitly.
7. Add editable collection and maintenance actions:
   - Save collection settings to YAML and restart.
   - Export a redacted settings snapshot.
   - Restart the panel from maintenance settings.
8. Update CSS with responsive settings layout using existing tokens and shell patterns.
9. Build and verify:
   - `go test ./...`
   - `npm --prefix web run build`
   - `npm --prefix web run lint`
   - If visual verification is needed, rebuild binary and inspect mobile/desktop browser states.

## Rollback Points

- Backend settings endpoint is additive and can be removed without touching monitor behavior.
- Frontend settings route is isolated behind `ActiveView = "settings"`.
- Browser-local preferences should have defaults and a reset path; deleting localStorage keys restores current behavior.

## Approved Scope

This iteration implements editable RouterOS REST connection settings, collection settings, browser-local UI preferences, and safe maintenance actions. Server listen/allowlist settings remain read-only. Saving RouterOS connection or collection settings writes YAML and restarts the process under systemd.
