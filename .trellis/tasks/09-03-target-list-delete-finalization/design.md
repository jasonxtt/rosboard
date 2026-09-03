# Target-list deletion finalization design

## Current failure

`PolicyRepository.DeleteSource` marks a source with an active version or an
applied flag as `pending_delete`. The API then asks
`GenerateAndApplyTarget` to reconcile the target. Under the unified model, an
unreferenced target has no Routing or Access consumer domain, so target-scoped
generation intentionally returns an empty job. No domain commit runs the
legacy `DELETE FROM policy_v2_sources WHERE pending_delete = 1` cleanup, and
the row remains visible as `pending_delete` forever.

## Change boundary

Keep the fix in `internal/store/policy.go`, where the repository already has
the transaction, reference checks, and consumer-domain calculation needed to
choose the lifecycle. Before deciding between tombstoning and physical
deletion:

1. Reject preset and canonical Routing/Access references exactly as today.
2. Calculate the pre-delete consumer domains.
3. If neither domain consumes the source, delete the source row immediately.
   Foreign-key cascades remove its stored versions and rules. The unified
   desired graph does not materialize an unreferenced target, so no RouterOS
   apply is needed.
4. If a legacy routing association still makes the source part of a routing
   domain, retain the current tombstone (`pending_delete=1`) so the existing
   apply path can remove its RouterOS projection.

The API contract remains unchanged. For the no-consumer case its existing
empty-job success path will now observe a source that is actually gone. A row
left by an earlier buggy binary can be retried and follows the same immediate
deletion branch.

## Safety and compatibility

- Reference checks remain inside the same SQLite transaction, so a source
  cannot be physically removed while a canonical consumer is present.
- Device isolation is unchanged because the repository operates on the
  device-owned database.
- Preset sources remain protected.
- Legacy sources with an outstanding routing association retain the staged
  cleanup behavior; no broad RouterOS deletion is introduced.

## Verification

- Store test: seed a source, promote an active version, delete it without
  consumers, and assert the source, versions, and rules are gone.
- Store/API test: exercise the actual applied unreferenced delete request and
  assert a synchronous success response and a missing target on reload.
- Run the backend quality checks, build the Linux AMD64 binary, deploy only to
  `10.0.0.60`, and verify the service and HTTP surface.

## Root-review follow-up design

The target library remains a shared, device-scoped input store. `TargetList`
does not have an independent runtime enabled state: canonical RoutingRule and
AccessRule consumers determine whether its projection is materialized. A list
without an enabled consumer is standby data, including after upload or inline
creation, and must not create RouterOS DNS/address-list objects.

Both policy wizards use the same target-list editor from their target selector.
Saving an inline Domain/IP list returns to the selector with that list selected.
New AccessRules start enabled, so the editor does not ask for a second
activation decision.

The false-stale access edit came from two different member snapshots. The
proposal overlay replaced the member with the API payload (which intentionally
omits internal `LastIPv4`/`LastIPv6`), while the atomic store commit preserved
the previous trusted resolution for the same auto-follow identity. The overlay
now preserves those fields when both members are auto-follow and their
normalized MAC anchors match. The exact proposal plan hash, access revision,
target revisions, desired hash, and RouterOS fingerprint remain the gate; a
mismatch fails closed and is not automatically regenerated.

Access status is derived in this order: active non-terminal apply job →
`applying`; failed job → `failed`; disabled rule → `disabled`; desired access
revision ahead of applied → `pending`; target/member degradation → `degraded`;
otherwise committed and equal state → `applied`.

## P0 cross-manager ownership correction

The observed Access DNS Static loop was caused by two rosboard writers sharing
the old unscoped `rb_<8>` comment identity. RouterOS object ownership is now a
single shared contract in `internal/ownership`: the canonical identity is
`rbs_<scope8>_<object8>`. This is the equivalent short scoped format chosen
because the other writer's legacy `rb_` cleanup boundary must not recognize the
current graph. `scope8` is the first eight lowercase hexadecimal characters of
SHA-256 over the length-framed input
`scope:v1:<manager-byte-length>:<trimmed-manager>:<device-byte-length>:<trimmed-device>`
and `object8` is the first eight lowercase hexadecimal characters of SHA-256
over the logical ID. Readable labels remain after ` | `.

The scanner recognizes a canonical identity as current only when its scope
matches the current manager and device. A different scope is foreign and is
not stale, owned, or actionable. Old scoped V1 formats may be mapped only when
their manager/device namespace is the current one. Old unscoped `rb_<8>` rows
are never auto-adopted, including Access objects: when their hash uniquely
matches a desired logical ID they are surfaced as ambiguous so the plan blocks
without replacing them; unknown rows remain untouched. Legacy physical
prefixes and readable labels are domain hints only after ownership is proven,
never ownership evidence.

The ownership rule is deliberately shared by policy-v2 and Access Control so
one writer cannot interpret another writer's objects as stale. Repeated scans
after both managers coexist must converge to zero operations, including the
Access terminal refresh path.
