# Design: policy routing review fixes

## Boundaries

The policy V2 builder must not add a `srcnat` masquerade rule. Existing user
NAT remains outside rosboard's ownership boundary. Policy-owned DNS `dst-nat`
redirects remain managed because they are part of Fake DNS transport. The
persisted `natMode` field remains readable for compatibility with old drafts,
but it has no masquerade effect.

The current three planning documents remain authoritative. In particular,
Bridge slave filtering and the discovery-only ingress candidate contract are
not changed by this task.

## Reconciliation

Desired objects already carry an `Order`. The diff will preserve phase and DNS
dependency ordering, then compare desired order against the actual order for
each menu/order domain. A mismatched existing object receives a `move`
operation with an anchor logical ID. The manager executes the move after
creates/patches and before cleanup. The operation remains a normal cached plan
operation, so stale-plan checks still protect it.

For managed fields, diff compares the union of actual and desired keys. A key
missing from desired is represented by an empty RouterOS value in the patch.
Non-managed structural fields continue to be excluded by the existing field
normalization.

## Plan presentation

Compact mode is a presentation mode only. It uses the exact same grouped
operation list as full mode and may not remove any executable operation. Delete
groups remain visible with the existing delete label.

## Discovery

The scanner keeps the existing `available`/`reason` contract and adds a small
`warnings` string array. Interface inventory, device identity, and the IPv4
route inventory are required for the default automatic WAN discovery. IPv6
routes, DHCP, and topology enrichment reads are optional; their errors are collected as warnings and
successful data is still used. When an optional read is needed to safely
classify a candidate, that affected candidate class is omitted rather than
fabricated from incomplete data.

No discovery state machine or manual-mode persistence is introduced. The UI
shows warnings and blocks automatic configuration when required discovery is
unavailable; manual interface input remains out of scope under the current
contract.

## Compatibility

The API parser accepts an optional `warnings` array. Existing clients that do
not know the field continue to parse the response. Existing stored NAT mode
values are accepted but ignored by Desired Builder.
