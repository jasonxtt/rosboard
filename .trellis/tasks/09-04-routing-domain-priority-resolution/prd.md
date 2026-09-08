# Routing domain projection priority resolution

## Background

RoutingRule already carries `Priority` (smaller = higher) and `sortedRoutingRules()`
orders by it, but the domain conflict model still treats any exact/suffix domain
overlap across distinct physical `(egress, targetList)` DNS projections as a hard
blocker (`domain_projection_context_ambiguous`). This blocks legitimate setups,
including multiple overlapping TargetLists inside a single RoutingRule (the
"7d6... / 7d6..." field reports), and prevents users from arbitrating cross-egress
domain conflicts by priority.

## Product semantics (user-confirmed)

1. Priority is a device-global DNS conflict arbiter. RouterOS DNS Static is
   device-global with ordered first-match; RoutingRule subjects cannot create
   per-client DNS contexts, so the higher-priority (smaller number) rule wins the
   overlapping domain space on the whole device.
2. Same RoutingRule with multiple overlapping TargetLists → allowed (they are OR'ed;
   one physical projection wins the matcher, the rule routes all its lists to the
   same egress).
3. Different projections, different Priority → allowed + warning. Higher priority's
   matcher is ordered before the lower one in RouterOS DNS Static; the lower rule
   keeps its non-overlapping space (no domain-set subtraction — rely on first-match).
4. Different projections, equal Priority → blocker with a user-readable reason
   naming the overlapped matcher, both rule names, and both priorities.
5. Priority order beats matcher specificity (a higher-priority DOMAIN-SUFFIX fully
   shadows a lower-priority exact DOMAIN inside it).
6. A physical projection shared by multiple enabled rules takes the minimum
   (highest) Priority among its consumers as its effective DNS priority; record the
   winning consumer for attribution. No internal conflict.
7. `RoutingRuleConflicts()` must no longer unconditionally block domain-content
   overlap across egresses: domain overlap is resolved by the projection priority
   semantics. IP overlap/logical conflict semantics are unchanged.
8. Access↔Access overlap keeps the fail-closed
   `access_domain_projection_ambiguous` blocker. Access↔Routing overlap is
   resolved by the accepted Access-first warning and DNS Static ordering
   contract described below.
9. RouterOS `/ip dns static` ordering must actually converge: managed DNS Static
   rules are emitted in effective-priority order and the reconciler must be able
   to `move` existing DNS Static entries into that order. Foreign DNS Static
   objects are never moved, modified, or deleted; moves only reorder owned objects
   relative to each other.

## Acceptance criteria

- Unit tests cover: same-rule overlap allowed; different-priority cross-egress
  exact/exact → warning + order; equal priority → blocker with readable reason;
  high exact + low suffix and high suffix + low exact (priority beats specificity);
  nested suffix both directions; shared projection multi-consumer effective
  priority; same egress different targets overlap (different priority → warning,
  equal → blocker); IP conflict regression unchanged; Access↔Access blockers
  unchanged; Access↔Routing warning/order behavior covered.
- Desired DNS Static `Order` reflects effective projection priority.
- `desiredOrderMoves` emits move operations for `ip/dns/static` owned objects and
  the real `DiffDesired → PlanOperation move` chain is tested (not just a helper
  sort).
- Reason text exposed to users names rule name / priority / matcher, not UUIDs or
  "physical projection" jargon.
- Gates: gofmt, `go test ./...`, `go test -race ./...`, `go vet ./...`,
  `git diff --check`; web lint/build only if frontend changes.
- No deploy, no commit, no production access.

## Constraints

- Minimal change; no new persisted schema/entities; no domain subtraction engine;
  no TargetList/AccessRule model changes; do not widen IP conflict semantics.

## Design

See `design.md` for the projection-resolution data model, the DNS Static ordering
contract, and the foreign-object safety argument.

## Root-review follow-up: AccessControl precedence

The accepted follow-up extends the device-global DNS ordering contract across
the two execution domains. An active AccessControl domain projection must
physically precede every overlapping active Routing projection on the same
RouterOS device. This is device-global first-match shadowing: it is not a
per-client exception and it does not add an AccessControl priority field.

- Replace the normal Access↔Routing domain blocker with warning
  `cross_domain_access_priority_shadowed` and an owned-object ordering
  constraint; keep Access↔Access blocking and all Routing↔Routing/IP semantics.
- Only current manager-owned Access/Routing DNS Statics may be moved. Foreign,
  unowned, and ambiguous legacy objects remain untouched.
- Plans must bind the cross-domain model and the physical DNS order in their
  stale checks. Routing activation must fail closed when its active overlap has
  no usable Access counterpart; the Access plan may establish the counterpart.
- A shared domain target-list refresh must apply Access first, then generate a
  fresh Routing plan after the Access domain has committed. A failure in the
  second domain must retain the first domain's commit without applying the
  second. A shared IP-only target retains the established Routing-first order.
- No production deployment or commit is part of this implementation round.
