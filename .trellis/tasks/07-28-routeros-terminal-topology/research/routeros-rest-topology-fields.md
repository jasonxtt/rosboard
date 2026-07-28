# Live RouterOS REST topology evidence (2026-07-28)

Existing ignored local configuration supplied the minimal-privilege account. GET-only probes printed status, row count, and field names; no credentials were logged and no RouterOS state changed.

| Menu | Status / rows | Relevant confirmed fields |
|---|---:|---|
| `/rest/interface/list` | 200 / 7 | `.id`, `name`, `include`, `exclude`, `dynamic` |
| `/rest/interface/list/member` | 200 / 2 | `.id`, `list`, `interface`, `disabled`, `dynamic` |
| `/rest/ip/dhcp-server` | 200 / 2 | `.id`, `name`, `interface`, `disabled`, `invalid` |
| `/rest/ip/dhcp-client` | 200 / 1 | `.id`, `interface`, `disabled`, `status` |
| `/rest/ipv6/nd` | 200 / 2 | `.id`, `interface`, `disabled`, `invalid` |
| `/rest/ipv6/nd/prefix` | 200 / 1 | `.id`, `interface`, `prefix`, `disabled`, `invalid`, `on-link`, `autonomous`, `dynamic` |
| `/rest/routing/route` | 200 / 53 | `.id`, `afi`, `dst-address`, `gateway`, `immediate-gw`, `active`, `disabled`, `connect`, `vrf-interface` |
| `/rest/ip/address` | 200 / 10 | `.id`, `address`, `interface`, `actual-interface`, `disabled`, `dynamic`, `invalid` |
| `/rest/ipv6/address` | 200 / 15 | `.id`, `address`, `interface`, `actual-interface`, `dynamic`, `disabled`, `invalid`, `advertise` |
| `/rest/ip/dhcp-server/lease` | 200 / 20 | `.id`, `address`, `server`, `comment`, `host-name`, `mac-address`, `status` |
| `/rest/ip/arp` | 200 / 261 | `.id`, `address`, `interface`, `mac-address`, `status`, `complete`, `disabled`, `invalid` |
| `/rest/ipv6/neighbor` | 200 / 39 | `.id`, `address`, `interface`, `mac-address`, `status`, `disabled` |

Current strong evidence: `lan` is LAN-list/DHCP/ND and publishes `fc00::/64`; `vlan2-aruba` has DHCP-server LAN evidence; `pppoe-out1` is WAN-list/default-route; `wan-xray` has an active default route despite private addressing. Candidate LAN prefixes are `10.0.0.0/24`, `10.10.10.0/24`, and `fc00::/64`; final live output awaits implementation.
