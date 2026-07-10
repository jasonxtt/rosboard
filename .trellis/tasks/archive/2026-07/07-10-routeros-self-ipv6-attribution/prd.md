# Attribute RouterOS self IPv6 connections

## Goal

Classify currently unattributed IPv6 connection rows and include RouterOS self traffic without misassigning WAN or transit traffic.

## Requirements

- Treat every enabled address assigned to RouterOS itself as an exact local identity, including LAN, WAN, WireGuard, PPPoE, link-local, and loopback addresses.
- Do not expand WAN or tunnel interface prefixes into terminal networks; only the router's exact address may identify RouterOS-self traffic.
- Attribute previously unowned connections to RouterOS self when the selected local source/reply-source endpoint is an exact RouterOS address; retain original-source LAN terminal ownership when both endpoints are local.
- Merge all RouterOS IPv4 and IPv6 addresses into one stable terminal identity instead of creating one terminal per interface address.
- In IPv4/IPv6 terminal lists, show that family's connection count, current rates, and active-connection bytes; keep combined values only in All terminals.
- Preserve the existing LAN terminal attribution logic and family-scoped detail behavior.
- Keep the implementation read-only with respect to RouterOS configuration.

## Acceptance Criteria

- [x] RouterOS-self IPv6 connections appear under one RouterOS terminal and are not dropped as unattributed.
- [x] LAN, WAN, tunnel, link-local, and loopback RouterOS addresses do not create duplicate terminal rows.
- [x] Exact WAN self-address matching does not cause unrelated WAN-prefix traffic to be treated as local terminals.
- [x] Live classification reports raw, terminal-attributed, RouterOS-self, and truly unattributed IPv6 counts.
- [x] Existing IPv4/IPv6/All terminal detail scope checks continue to pass.
- [x] RouterOS list statistics match the selected IPv4/IPv6/All scope.
- [x] Unit tests cover router-address precedence, exact matching, and stable identity merging.
- [x] Go tests, race tests, vet/build, frontend lint/build, API smoke checks, and browser checks pass.

## Notes

- Live sample at 2026-07-10 13:57: 188 raw IPv6 rows, 99 already attributed, 93 RouterOS-self in total, 88 RouterOS-self rows unattributed, and one remaining row associated with an excluded-interface neighbor.
- RouterOS-self detection uses exact interface addresses, not broad external networks.
