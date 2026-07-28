# 修复终端实时速率显示延迟

## Goal

Reduce visible per-terminal upload and download rate latency from the current
roughly 6–10 seconds to a stable 1–2 seconds while a visible terminal page is
being viewed, without increasing the expensive terminal-discovery work to a
one-second cadence.

## Requirements

- Keep RouterOS collection read-only.
- Separate one-second terminal rate collection (IPv4 and IPv6 connection
  tracking only) from the existing terminal discovery, identity, presence,
  history, and persistence collection.
- Run terminal-rate collection independently of the full refresh and terminal
  discovery refresh so a slow full refresh cannot freeze displayed rates.
- Activate terminal-rate collection only while a visible terminal list or
  terminal-detail page is being viewed, and let it return to the existing
  lower-frequency behavior after the 30-second viewer TTL expires.
- Immediately activate that collection when the user opens terminal monitoring
  or a terminal detail; browser reads must continue to return cached snapshots
  and must not synchronously collect from RouterOS.
- Preserve the existing terminal identity, metadata, presence, totals,
  history, route-attribution, IPv4/IPv6, and multi-device behavior.
- Add a rate-snapshot timestamp to terminal-detail responses so the UI can
  show how fresh the displayed rates are.
- Make terminal-list polling honor the selected dashboard refresh interval;
  poll a selected terminal detail every second while automatic refresh is
  enabled.

## Acceptance Criteria

- [ ] A terminal-page heartbeat activates a dedicated rate worker without
  making the heartbeat request wait for RouterOS.
- [ ] While terminal viewing is active, the rate worker requests only
  `/rest/ip/firewall/connection` and `/rest/ipv6/firewall/connection` (the
  latter remains best-effort), at one-second intervals measured from scheduled
  ticks rather than from completion of the prior collection.
- [ ] Terminal discovery still uses the configured terminal polling interval
  and continues to read DHCP, ARP, neighbor, and address data.
- [ ] A full refresh and a terminal-rate refresh may run concurrently; an older
  full-refresh commit cannot overwrite newer terminal rate data.
- [ ] The terminal list and selected detail reflect the new rate snapshot, and
  the detail displays its rate update age.
- [ ] Changing the global automatic refresh selector changes terminal-list
  polling; a selected terminal detail polls every second when automatic refresh
  is on.
- [ ] Targeted backend/API tests, `go test ./...`, `go vet ./...`, frontend
  lint/build, and local runtime/visual checks pass.
- [ ] Before committing, the verified runnable build is deployed to `10.0.0.6`
  with timestamped backups; service, health/API/assets are verified, then the
  user manually approves the deployed instance.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
