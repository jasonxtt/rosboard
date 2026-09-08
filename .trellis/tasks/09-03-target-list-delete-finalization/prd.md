# Target-list deletion finalization

## Goal

Make an applied, unreferenced custom target list delete completely instead of
remaining in the `清理中` / `pending_delete` state. This must work for lists
created by the current unified Target Library flow, including manual and
uploaded lists.

## Requirements

- A target list with no canonical Routing or Access Control references must be
  removed from the device-scoped SQLite policy store in the same transaction as
  the delete request, even when it has an active version or was previously
  applied.
- Deleting an unreferenced list must not create a RouterOS apply job or change
  either domain's applied revision.
- Preserve the existing pending-delete path when a legacy routing association
  still requires RouterOS cleanup; this task does not add a compatibility
  migration or cross-device sharing.
- A target list already left behind as `pending_delete` by the faulty path must
  be removable by retrying its delete request once it has no consumers.
- Keep preset protection and routing/access reference protection unchanged.

## Post-acceptance root-review follow-up

- A P0 cross-manager ownership correction must stop the continuous Access DNS
  Static delete/recreate loop. RouterOS managed comments must be scoped by the
  installation/manager, device, and logical object identity, and policy-v2 and
  access-control must use the same ownership contract.
- A manager must never delete, patch, move, or enable/disable another manager's
  scoped objects. Old unscoped `rb_<8>` comments are always ambiguous and are
  never auto-adopted; they remain untouched or block a matching desired object
  until an explicit migration path exists.

- TargetList is shared library input and has no standalone enabled semantic;
  an enabled canonical RoutingRule or AccessRule is the only activation
  authority. Unreferenced lists remain in standby and are not written to
  RouterOS.
- The RoutingRule and AccessRule target selectors must reuse the target-list
  editor for inline Domain/IP list creation and automatically select the newly
  created list when the editor closes.
- New AccessRules are enabled by default and do not expose a redundant
  “启用此规则” or “启用此目标列表” checkbox.
- Access proposal preview and atomic commit must preserve a trusted
  auto-follow member's last IPv4/IPv6 resolution when the same terminal and
  normalized MAC anchor are retained.
- Plan application is fail-closed: exact plan hash, revision, desired hash,
  and RouterOS fingerprint mismatches fail the job and are not hidden by a
  generic automatic replan.
- An enabled AccessRule is `applying` while its apply job is queued, staging,
  or verifying; it is `pending` when its desired revision is ahead of the
  applied revision, and `applied` only after a committed, non-degraded state.

## Acceptance Criteria

- [x] A regression test proves an applied target with an active version and no
  consumers is physically deleted and its version/rule rows are cascaded.
- [x] An API regression test proves deleting an applied, unreferenced custom
  list returns success without `pendingDeletion` and the list is absent after
  reload.
- [x] Existing referenced-target rejection and legacy pending-delete behavior
  remain green.
- [x] `go test ./...`, `go vet ./...`, `go build ./...`, and `git diff --check`
  pass.
- [x] The rebuilt binary is deployed to test machine `10.0.0.60` using its own
  test configuration/data, and systemd, health, target-list API, and embedded
  frontend assets are verified.
- [x] Root-review regressions cover Access proposal enabled→disabled and
  disabled→enabled toggles without a false `policy plan is stale` failure.
- [x] Root-review UI and semantics are covered: no standalone target-list
  enable control, inline target creation/selection, new AccessRule defaults
  enabled, and correct applying/pending/applied status precedence.
- [x] The generic automatic stale-plan retry is removed and the backend
  policy-routing spec/task records the fail-closed contract.
- [x] Cross-manager ownership regressions cover scoped coexistence, foreign
  object isolation, same-manager device isolation, and conservative legacy
  migration without old unscoped Access adoption.
- [x] Access terminal refresh remains idempotent after a committed apply:
  repeated `access-terminal-refresh` plans have zero operations and zero
  resolutions.

## Constraints

- Do not modify production `10.0.0.6` in this task.
- Do not alter unrelated dirty worktree files.
