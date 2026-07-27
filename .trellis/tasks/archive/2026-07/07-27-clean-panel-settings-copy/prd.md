# Clean panel settings copy

## Goal

Clean up redundant panel-settings guidance and make maintenance actions less tied to a single selected RouterOS device.

## Requirements

- Remove redundant helper text from device management, collection settings, and per-device edit forms.
- Remove the device-specific diagnostics grid from maintenance settings, including device name, RouterOS version, last collection time, and health collection status.
- Keep maintenance actions that are panel-wide or multi-device appropriate: export settings, reset browser preferences, restart panel service, and archived device restore/purge.
- Ensure exported settings mask passwords for every configured device, not only the legacy connection object.
- Theme choices preview immediately when clicked and remain active until page refresh, but still persist only after saving interface settings.
- Build and deploy to `10.0.0.6` for manual acceptance before committing.

## Acceptance Criteria

- [ ] The listed redundant Chinese helper strings no longer appear in the embedded frontend bundle.
- [ ] Maintenance settings no longer show selected-device name/version/collection diagnostics.
- [ ] Exported settings mask `connection.routerosPassword` and every `devices[].password`.
- [ ] Clicking light/dark theme immediately updates the page theme; unsaved theme changes are not persisted across refresh.
- [ ] Automated checks pass.
- [ ] Updated panel is deployed to `http://10.0.0.6:8080/` and awaits user approval before commit.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
