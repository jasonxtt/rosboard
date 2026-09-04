# Fix policy routing review findings

## Goal

Make the policy-routing V2 implementation match the approved scope and make its
preview, reconciliation, and RouterOS discovery behavior safe and explicit.

## Requirements

- Policy routing must not create a RouterOS `srcnat` masquerade rule. Existing
  user NAT remains outside rosboard's ownership boundary. The existing
  policy-owned DNS `dst-nat` redirect remains part of Fake DNS behavior.
- Egress priority/order changes must produce an effective RouterOS ordering
  operation, and the plan must expose that operation.
- For exact managed-object reconciliation, a field removed from desired state
  must be cleared from the RouterOS object when it is a managed field.
- The compact policy plan view must show every operation that the apply action
  will execute, including deletes.
- Discovery must propagate failures for required topology reads and preserve
  successful partial results plus explicit warnings for optional reads. It must
  not report an incomplete required discovery as available.
- The existing V2 contract remains authoritative: no change to the separate
  physical-LAN/Bridge-slave candidate policy and no automatic manual-input
  bypass when discovery is unavailable.

## Acceptance Criteria

- [x] A generated desired state and plan contain no `srcnat` masquerade
  operation, while existing DNS `dst-nat` operations remain intact.
- [x] Reordering managed activation rules creates `move` operations with a
  deterministic anchor/order and apply executes them.
- [x] Removing a managed field creates a patch that clears the old RouterOS
  value; an unchanged desired state remains idempotent.
- [x] Compact and full plan views contain the same operation set, and deletes
  are visibly labeled as deletes.
- [x] Required discovery errors return unavailable/error; optional errors return
  candidates from successful reads and visible warnings.
- [x] Existing backend tests, targeted regression tests, `go vet`, frontend lint,
  and frontend build pass.

## Out of scope

- Changing which physical interfaces are exposed as traffic-ingress candidates.
- Adding manual WAN/interface configuration as a new product capability.
- Fixing the remaining medium-severity review findings.
