# RouterOS self attribution design

Build exact IPv4 and IPv6 address sets from enabled `/rest/ip/address` and `/rest/ipv6/address` rows. Pass those identities separately from terminal CIDRs so address ownership and LAN-prefix membership remain distinct.

Connection orientation considers exact RouterOS addresses alongside terminal CIDRs. Original-source LAN terminal ownership remains first so a LAN client talking to the router is not moved away from that client; otherwise an exact RouterOS source/reply-source endpoint identifies RouterOS-self traffic. Byte/rate direction remains the same as normal source/reply-source orientation.

Terminal construction maps every RouterOS-self connection and assigned address to one stable ID (`routeros:self`). Existing address-based RouterOS rows are merged into that ID so accumulated panel state is retained. RouterOS addresses may be displayed together, but exact WAN/tunnel matches never add their surrounding prefix to LAN terminal CIDRs.

Dashboard terminal rows carry small IPv4/IPv6 activity projections derived from the same normalized connection list as terminal details. Scoped lists use these projections, while the All list keeps the existing combined/all-time fields. Family byte values are explicitly presented as active-connection accumulation rather than persisted all-time totals.

No database migration or RouterOS write is required.
