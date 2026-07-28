# Live RouterOS traffic-scope REST evidence (2026-07-28)

Used the ignored local configuration's existing minimal-privilege account for GET-only requests. The probe printed only HTTP status, row count, and returned field names. It did not write RouterOS state or print endpoints, usernames, passwords, or response values.

| Menu | Status / rows | Confirmed fields |
|---|---:|---|
| `/rest/interface/pppoe-client` | 200 / 1 | `.id`, `ac-name`, `add-default-route`, `allow`, `default-route-distance`, `dial-on-demand`, `disabled`, `interface`, `invalid`, `keepalive-timeout`, `max-mru`, `max-mtu`, `mrru`, `name`, `password`, `profile`, `running`, `service-name`, `use-peer-dns`, `user` |
| `/rest/ip/dhcp-client` | 200 / 1 | `.id`, `add-default-route`, `allow-reconfigure`, `check-gateway`, `default-route-distance`, `default-route-tables`, `dhcp-options`, `disabled`, `dynamic`, `interface`, `status`, `use-broadcast`, `use-peer-dns`, `use-peer-ntp` |

## Modeling implications

- `PPPoEClient` must model `ID`, `Name`, `Interface`, `Disabled`, `Invalid`, `Running`, `AddDefaultRoute`, and `DefaultRouteDistance`. The live menu does **not** return `status`; the implementation must not invent a `status` field dependency.
- `DHCPClient` can model `ID`, `Interface`, `Disabled`, `Status`, `AddDefaultRoute`, and `DefaultRouteDistance` with the confirmed RouterOS JSON tags.
- The PPPoE response contains credential-bearing fields (`password`, `user`) in the raw RouterOS payload. The typed struct must deliberately omit them and no logging/projection may serialize raw rows.
