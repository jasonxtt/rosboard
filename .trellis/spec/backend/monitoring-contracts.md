# Monitoring Contracts

## Scenario: RouterOS self and IP-family terminal attribution

### 1. Scope / Trigger

- Trigger: changes to RouterOS address/connection normalization, terminal identity, `/api/dashboard` terminal fields, or IPv4/IPv6 list metrics.
- RouterOS access remains read-only. Address ownership is derived from REST snapshots and never changes router configuration.

### 2. Signatures

- Router address source: `GET /rest/ip/address` and `GET /rest/ipv6/address`.
- Connection sources: `GET /rest/ip/firewall/connection` and `GET /rest/ipv6/firewall/connection`.
- Stable router terminal ID: `routeros:self`.
- Dashboard projection: `Terminal.FamilyStats map[string]TerminalFamilyStats` with `ipv4` and `ipv6` keys.

### 3. Contracts

- Every enabled address assigned to RouterOS is an exact self identity, including WAN, tunnel, link-local, and loopback addresses.
- WAN/tunnel address prefixes are not terminal CIDRs. Only the exact assigned router address can identify self traffic.
- Original-source LAN terminal ownership wins when both connection endpoints are local; otherwise an exact RouterOS source/reply-source may own the connection.
- All assigned RouterOS addresses merge into `routeros:self`; preferred list addresses come from the `lan` interface when available.
- `familyStats.<family>` contains current connection count/rates plus bytes accumulated by currently active conntrack rows. It is not the persisted all-time total.
- `ROSBOARD_LISTEN_ADDRESS=0.0.0.0:8080` is the review/development delivery binding so LAN devices can open the built panel.

### 4. Validation & Error Matrix

- Disabled RouterOS address -> exclude from exact self identities.
- Invalid address/CIDR text -> ignore that address.
- Exact RouterOS WAN address -> RouterOS self is eligible.
- Different address in the same WAN prefix -> remain external.
- LAN terminal original source plus RouterOS reply source -> retain LAN terminal ownership.
- Missing `familyStats` in an older dashboard payload -> frontend falls back to combined terminal metrics.

### 5. Good/Base/Bad Cases

- Good: `reply-src-address` equals the PPPoE IPv6 assigned to RouterOS; count it under `routeros:self`.
- Base: a LAN IPv6 source connects through RouterOS to the internet; count it under the MAC-correlated LAN terminal.
- Bad: treat the entire PPPoE `/64` as local and create terminals for arbitrary internet addresses.

### 6. Tests Required

- Unit: exact router WAN address is oriented as RouterOS self with reply-side upload/download direction.
- Unit: another address in the WAN prefix is rejected.
- Unit: LAN original-source ownership is not stolen by a router reply-source address.
- Unit: all textual forms of assigned router addresses resolve to `routeros:self`; disabled addresses do not.
- Integration/browser: RouterOS appears once in All/IPv4/IPv6 lists and scoped list metrics match scoped detail metrics at the same snapshot.
- Delivery: verify HTTP 200 through both `127.0.0.1:8080` and the Mac LAN address.

### 7. Wrong vs Correct

#### Wrong

```go
// Expanding a WAN-assigned prefix makes arbitrary external peers look local.
localCIDRs = append(localCIDRs, routerWANPrefix)
```

#### Correct

```go
// Exact ownership identifies only the address actually assigned to RouterOS.
routerAddresses[assignedIP(address.Address)] = routerAssignedAddress{
    Family: family,
    Interface: address.Interface,
}
```
