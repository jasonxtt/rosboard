# Add fleet dashboard list and icon views

## Goal

Provide a top-level fleet dashboard that lets operators compare every enabled
RouterOS device at a glance, then open the existing per-device system overview
for a selected device.

## Requirements

- Add a top-level sidebar destination named `仪表台`. It is the fleet view;
  the existing `系统概览` remains the selected device's detailed overview.
- Show enabled, non-archived devices only. Disabled and archived devices remain
  available through device management and do not occupy fleet-dashboard rows or
  cards.
- The default presentation is a list: every device occupies one full-width,
  independently clickable row. Clicking it selects that device and opens the
  existing system overview.
- The fleet header contains exactly four summary metrics: `全部设备`, `在线设备`,
  `离线设备`, and `告警设备`.
- Each healthy device uses the following fixed metric order: device identity,
  CPU, memory, realtime upload/download rate, terminal count, connection
  count, then uptime and last-update time.
- Offline or unavailable devices retain their list/card position and identity,
  but replace unavailable measurements with a concise offline/error state;
  no other row may shift or change its metric order.
- Supply search, status filtering, device-name sorting, and pagination for the
  list. These controls are shared by the future icon view.
- Add a list/icon view toggle. The icon view is the second delivery stage and
  renders the same enabled-device data as one card per device without creating
  a second monitoring API or collection path.
- Persist the chosen list/icon presentation in browser preferences. List is the
  default for existing and new users.
- The overview must read cached monitor snapshots only. It must not cause an
  N-device fan-out of `/api/dashboard` calls or any direct RouterOS requests.
- Existing per-device pages and the global device selector must keep their
  present device-scoping behavior.

## Acceptance Criteria

- [ ] The sidebar exposes `仪表台`, while `系统概览` opens the current selected
  device's existing detailed overview.
- [ ] The dashboard renders only enabled, non-archived devices and its four
  top counts match the returned fleet data.
- [ ] A healthy list row shows CPU, memory, traffic rate, terminal count,
  connection count, uptime, and update time in the agreed order.
- [ ] An unavailable device remains visible with an explicit offline/error
  state and has no misleading stale realtime measurements.
- [ ] Clicking a list row or icon card selects its device and displays its
  existing system overview.
- [ ] Search, status filter, name sorting, and pagination work in list view
  and retain their state when switching to icon view.
- [ ] Icon view shows the same filtered device set and values as list view;
  it is responsive without document-level horizontal overflow.
- [ ] Fleet API responses are built solely from monitor-manager cached
  snapshots, and neither the route nor the browser performs per-device full
  dashboard requests.
- [ ] Relevant Go tests, frontend lint, and production frontend build pass.

## Notes

- The four header counts are overlapping operational classifications:
  a device can be both offline and alerting. `告警设备` also includes online
  devices with current monitor alerts or warnings.
- `trellis-brainstorm` is not installed in this Codex environment; requirement
  exploration is recorded directly from the user discussion.
