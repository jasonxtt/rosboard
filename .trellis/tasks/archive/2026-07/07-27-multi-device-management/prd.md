# Multi-device RouterOS management

## Goal

Allow one RosBoard installation to configure and monitor multiple RouterOS devices without mixing credentials, state, history, or terminal identity.

## Requirements

- Manage device records and HTTP connection settings from panel settings.
- Mask passwords by default and reveal them only through the existing eye-control pattern.
- Scope every live snapshot, historical record, terminal, route, and setting to a device.
- Monitor every enabled device continuously; selecting a device changes the displayed scope, not collection activity.
- Isolate collection errors and reconnect backoff per device.
- Migrate the existing single-device configuration and database automatically.
- Archive devices on normal removal and retain their history.
- Provide a separate confirmed maintenance action to permanently purge an archived device and all scoped data.
- Provide a device selector in operational pages.
- Upgrade the sidebar device summary into one global selector shared by all monitoring pages.
- Remember the last selected device in the browser and select the next available device when the current one is removed or disabled.
- Keep panel settings global while the connection section manages the device list and per-device connection editor.
- Provide per-device monitoring views only in the first release; do not add an aggregate device option.
- Show every configured device's health and last update in the management list.

## Dependencies

- This child owns the device identifier and storage migration contracts consumed by the dashboard-time-range and terminal-route-attribution children.
- The parent PRD requires continuous background collection for every enabled device.

## Acceptance Criteria

- [ ] Users can add, edit, remove, enable, disable, and select devices.
- [ ] Every enabled device continues collecting while another device is selected.
- [ ] Same-MAC terminals on different devices remain distinct.
- [ ] Existing installations migrate without manual database changes or lost history.
- [ ] A failing device does not make healthy devices unavailable.
- [ ] Removing a device preserves its data, while permanent purge requires an explicit separate confirmation.
