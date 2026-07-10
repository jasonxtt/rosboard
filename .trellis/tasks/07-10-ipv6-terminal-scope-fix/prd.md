# Fix IPv6 connection attribution and family-scoped details

## Goal

Make rosboard attribute RouterOS IPv6 connections to LAN terminals correctly and make terminal list/detail behavior follow the entry family: IPv4-only, IPv6-only, or combined.

## Confirmed Evidence

- Live RouterOS `/rest/ipv6/firewall/connection` returned 171 entries during diagnosis; Winbox counts fluctuate in the same general range.
- The live rosboard instance had 61 terminals and 19 terminals with discovered IPv6 addresses, but terminal details exposed no meaningful IPv6 connection set.
- `deriveLocalCIDRs` currently receives only `/rest/ip/address`; it therefore cannot recognize ULA, delegated global IPv6, or neighbor privacy addresses as local.
- The frontend filters the IPv6 list by presence of IPv6 but still renders `primaryIpv4` first and always opens detail with `connectionFamily='ipv4'`.

## Requirements

- Read `/rest/ipv6/address` without changing RouterOS configuration.
- Add non-WAN IPv6 interface networks to the local-network set used for connection orientation.
- Add current non-WAN IPv6 neighbor addresses as exact `/128` local identities so privacy/global addresses can be attributed even when the delegated prefix is not visible as a LAN interface address.
- Exclude loopback, WAN/overview traffic interfaces, and clearly external IPv6 interface addresses from terminal attribution.
- Keep RouterOS-wide connection totals distinct from terminal-attributable connection totals.
- In the IPv4 terminal view, render IPv4 as the main address and open details scoped to IPv4 addresses, counts, rates, traffic distribution, and connections.
- In the IPv6 terminal view, render IPv6 as the main address and open details scoped to IPv6 addresses, counts, rates, traffic distribution, and connections.
- In the All terminals view, render the device-level combined view and open details with combined IPv4+IPv6 data; the connection tab may switch among All, IPv4, and IPv6.
- Do not duplicate a dual-stack device into separate terminal identities when MAC correlation is available.

## Acceptance Criteria

- [x] Live raw IPv6 connection count and terminal-attributable IPv6 count are both observable and their difference is explainable.
- [x] IPv6 terminal connections are nonzero when RouterOS has active LAN IPv6 connections.
- [x] IPv4 list rows use IPv4 as their primary address and IPv4 detail contains no IPv6 connection rows.
- [x] IPv6 list rows use IPv6 as their primary address and IPv6 detail contains no IPv4 connection rows.
- [x] All-terminal detail defaults to all connections and exposes All/IPv4/IPv6 switches.
- [x] Family-scoped basic information, connection count, current rates, and traffic distribution use the selected family rather than combined totals.
- [x] Unit tests cover IPv6 CIDR derivation, exact neighbor attribution, numeric address behavior, and detail-family selection logic where practical.
- [x] Go tests, race tests, frontend lint/build, API smoke checks, and live browser checks pass.

## Out of Scope

- RouterOS configuration writes.
- Making terminal-attributable counts equal every RouterOS connection-tracking row.
- Traffic Flow/IPFIX or DPI changes.
- Unrelated monitoring/UI redesign.
