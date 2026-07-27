# Design

## Backend

`buildTerminals` already receives the device-scoped `localCIDRs` used by conntrack attribution. Reuse that scope for weak/strong discovery rows:

- Bound DHCP leases can populate terminals only when their address is in scope, unless no CIDR scope is available.
- ARP and IPv6 neighbor entries can populate or mark terminals online only when their address is in scope, unless no CIDR scope is available.
- RouterOS assigned addresses are processed first and remain exact `routeros:self` identities regardless of terminal CIDR scope.
- Conntrack attribution continues to use `orientConnection` and the same `localCIDRs`.

This makes terminal CIDR mean "which local endpoint networks Rosboard treats as terminals", not "which RouterOS interface addresses exist".

## Frontend

- Render the overview range pills in the existing topbar controls only for the overview page.
- Remove the overview-local range row so the metric grid moves up.
- Refactor the metric card markup minimally into top/main/chart/footer zones so sparklines can span a wider area without overlapping value details.
- Keep compact 12px details/footers from the overview typography contract.
- Rename only the table action copy from `编辑终端` to `编辑`.
