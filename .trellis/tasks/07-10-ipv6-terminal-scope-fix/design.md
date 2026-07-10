# IPv6 terminal scope fix design

## Backend

Add a typed `IPv6Address` RouterOS endpoint and extend local-network derivation with three inputs: configured terminal CIDRs, IPv4 interface addresses, and IPv6 interface/neighbor evidence.

Configured terminal CIDRs remain authoritative. Otherwise:

1. include static/non-WAN IPv4 interface networks as today;
2. include non-WAN IPv6 interface networks except loopback and overview traffic interfaces;
3. include non-WAN IPv6 neighbor addresses as `/128` networks.

The connection normalizer continues to discard rows where neither side can be identified as local. This intentionally excludes router-originated/external rows from terminal details.

Expose family-specific terminal projections in `TerminalDetail`: IPv4, IPv6, and combined summaries, derived from the already normalized connection list. Keep the existing combined `terminal` field for API compatibility.

## Frontend

Carry the terminal entry scope (`all`, `ipv4`, `ipv6`) when opening detail.

- List address presentation is scope-aware.
- Detail header and basic information consume the corresponding family projection.
- Scoped entries lock connection results to that family.
- Combined entries default to all and show the three family switches.

## Compatibility

- Existing terminal IDs, cumulative totals, and `/api/dashboard` remain compatible.
- No database migration is required.
- Existing clients can continue using the combined `terminal` field.
