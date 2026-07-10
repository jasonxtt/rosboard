# Live snapshot audit

Source snapshot: `/rest/ipv6/firewall/connection` captured at 2026-07-10 14:04, containing 166 rows. RouterOS assigned-address and neighbor snapshots were captured in the same operation.

## Reproduced result

- 69 rows matched the exact current `routeros:self` algorithm.
- 63 matched `reply-src-address=2408:826c:6912:4516::1` on `pppoe-out1`.
- 6 matched original `src-address`: one public PPPoE address and five interface link-local addresses.
- 0 matched `fc00::1001` in either owning field.

## 63 reply-source matches

- 62 were UDP packets from 62 remote source addresses to `2408:826c:6912:4516::1:58680`.
- All 62 had `seen-reply=false`, `assured=false`, `srcnat=false`, and `dstnat=false`.
- One was UDP to local port `25798`, with `seen-reply=true` and `assured=true`.
- For every row, original `dst-address` equaled the matched RouterOS public address. This is endpoint-consistent input traffic, not forwarded/NAT traffic.

## 6 original-source matches

- Five were link-local UDP/5678 multicasts to `ff02::1`, consistent with RouterOS neighbor discovery traffic.
- One was a public-address-originated UDP flow with `seen-reply=true` and `assured=true`.
- All six had `srcnat=false` and `dstnat=false`; reply destination matched the original RouterOS source address.

## Conclusion

The algorithm did not count these 69 because of `fc00::1001`, and no NAT/forwarding false positive was found in the captured rows. The ownership classification is technically correct: the rows terminate at or originate from an exact RouterOS address. The UI wording is misleading, however: 62 of 69 rows were unreplied inbound UDP conntrack entries, likely probes or invalid handshakes, not bidirectional/assured connections.

The smallest follow-up is to keep raw tracking rows observable but split RouterOS self statistics into meaningful states such as `bidirectional/assured`, `router-originated discovery`, and `unreplied inbound`, instead of presenting their sum only as a connection count.
