# Technical design

## Boundaries

The change is limited to policy discovery and the policy wizard's WAN presentation. Existing RouterOS configuration remains outside rosboard's mutation boundary. The current traffic-ingress contract remains a shared managed interface list; direct members are still represented as members of that aggregate list.

## Role evidence

`Scanner.Scan` already reads interface lists and their members before constructing candidates. Resolve the exact case-insensitive `LAN` list with the existing recursive interface-list resolver. Its resolved members become an exclusion set for automatic WAN candidates. This gives explicit operator configuration precedence without adding a name heuristic or treating every list containing the word `LAN` as authoritative.

Traffic-ingress construction should still receive the complete set of WAN candidates, but it must only hide an interface from direct ingress when the WAN candidate is proven by an active route. An unproven VPN candidate has not demonstrated that it is an Internet egress and may still be a valid inbound client-traffic interface.

## Route evidence

RouterOS REST omits false boolean properties in the observed route payload. `defaultRoutes` will therefore interpret missing `active` as false and combine it with `disabled`. The scanner and gateway resolver request `disabled` alongside `active`.

The route-backed WAN set continues to consider default routes from all routing tables so existing non-main policy tables can be discovered. However, LAN-role evidence is applied before a route source becomes a WAN candidate. Inactive route records may remain visible as unproven route evidence only when they are not explicit LAN members; they cannot suppress ingress candidates.

## VPN and tunnel candidates

After route-backed candidates are built, add running, enabled, non-dynamic interfaces whose type identifies an outbound PPP/VPN or fixed tunnel. The type classifier covers:

- PPPoE and PPP-style interfaces;
- WireGuard (`wg`/`wireguard`);
- L2TP, SSTP, OpenVPN and PPTP;
- GRE, IPIP, EoIP, VXLAN, ZeroTier and related fixed tunnel type names.

Dynamic interfaces and type names containing inbound/server markers are excluded from this supplemental set. A static WireGuard interface cannot be reliably classified as client or server from the interface row alone, so it is intentionally shown as an unproven operator-confirmed candidate when no active default route proves it.

RouterOS IPsec is modeled through IPsec policies and security associations rather than a normal selectable `/interface` row, so it is intentionally outside this interface-candidate change and would need a separate policy-aware egress design.

All VPN/tunnel candidates are point-to-point for gateway validation. An active route may supply a gateway evidence row; without one, the existing desired-state path uses the interface name as the RouterOS gateway. No fake IP gateway is generated.

## UI contract

The existing `WANCandidate.proven` field is sufficient for the API contract. The WAN select option will append a concise `未验证路由` marker when false. The existing draft validation already treats point-to-point interfaces as not requiring a gateway; expanding the shared backend classifier keeps frontend and desired-state behavior aligned.

## Compatibility and risks

- Existing ordinary WAN candidates backed by active default routes remain selectable.
- Existing selected interface names are not silently rewritten in SQLite.
- A LAN interface intentionally reused as a WAN must use an explicit next-hop route mode or be removed from the authoritative `LAN` list; automatic role inference must not choose it over explicit LAN evidence.
- Existing bridge-slave filtering remains unchanged: a physical port that is a Bridge member is represented by its L3 Bridge/interface list, not as a separate L3 ingress.
