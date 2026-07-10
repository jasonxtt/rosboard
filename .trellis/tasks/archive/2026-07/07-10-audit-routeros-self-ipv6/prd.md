# Audit RouterOS self IPv6 attribution

## Goal

Read-only audit of which conntrack fields and assigned addresses classify IPv6 rows as RouterOS self, with NAT/forwarding misclassification checks.

## Requirements

- Read the live RouterOS IPv6 address, neighbor, firewall connection, NAT, and filter-rule snapshots without changing configuration.
- Reproduce rosboard's current `routeros:self` attribution against one captured connection snapshot.
- Break matches down by original `src-address`, `reply-src-address`, exact RouterOS address, interface, protocol, destination port, and connection state.
- Distinguish router input/output traffic from forwarded/NAT traffic using available conntrack and firewall evidence.
- Identify any classification rule that can steal a LAN terminal connection or count an external/forwarded connection as RouterOS self.
- Do not change code until the evidence establishes a concrete defect.

## Acceptance Criteria

- [x] The reported count is reproducible from a single raw RouterOS snapshot.
- [x] Every RouterOS-self match is grouped by the exact matching field and assigned address.
- [x] `fc00::1001` matches are reported separately from public, WireGuard, link-local, and loopback matches.
- [x] Potential NAT/forwarding false positives are listed with the exact evidence used to identify them.
- [x] The user receives a clear conclusion: correct, partially correct, or incorrect attribution, plus the smallest justified next step.

## Notes

- This is a read-only diagnostic task. Code changes require a confirmed defect and will add design/implementation planning before execution.
- Audit result: endpoint ownership is correct, but presenting all raw conntrack rows as meaningful "connections" is semantically misleading because most were unreplied inbound UDP rows.
