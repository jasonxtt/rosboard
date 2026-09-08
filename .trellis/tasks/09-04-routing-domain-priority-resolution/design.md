# Design — Routing domain projection priority resolution

## Data model (internal only, no persistence changes)

`routingDomainProjection` gains:

- `priority` — effective DNS priority = minimum of all ACTIVE consumer rule priorities.
- `ruleID` / `ruleName` — the highest-priority active consumer (winning attribution).
- `activeConsumers` — set of rules that are DNS-active: `rule.Enabled` AND the
  Egress exists, is `Enabled`, and is not `PendingDeletion`. Only active
  projections join conflict arbitration; an enabled rule on a disabled/missing
  Egress projects `disabled=yes` DNS Statics and can never be a real winner.
- `consumerRuleIDs` — all consumer rule IDs (active or not), used only for the
  deterministic fallback ordering priority of an otherwise inactive projection.

## Resolution semantics

`DomainProjectionResolutions(rules, targets, kinds, egresses)` replaces the old
`DomainProjectionContextAmbiguities` blocker-only detector:

- Same physical `(egress, target)` projection → single projection, never compared to itself.
- Two projections whose enabled consumer sets intersect (same RoutingRule references
  both TargetLists) → allowed, no issue (intra-rule OR).
- Overlapping domain matchers across distinct projections:
  - different effective Priority → one `warning`: the higher-priority projection wins
    the overlap; the loser keeps its non-overlapping space via RouterOS DNS Static
    ordered first-match. No domain-set subtraction.
  - equal effective Priority → one `blocker` (`domain_projection_context_ambiguous`)
    whose reason names the overlapped matcher(s), both rule names and the shared
    priority, asking the user to differentiate priorities. No UUID / "physical
    projection" jargon.

`RoutingDomainProjectionPriorities` exposes `egressID\x00targetID → effective priority`
so the desired builder can order DNS Statics.

## RoutingRuleConflicts scoping

`routingTargetsOverlap` no longer reports domain-only content overlap; those pairs are
delegated to the projection resolver. Identical Target IDs and different-ID pairs
still conflict when the overlap is IP content (or the list has no materialized
content, which is the conservative pre-existing behavior). The store regression
`TestRoutingRuleDesiredBlocksOnlyProvenOverlappingIPPolicies` keeps passing.

## DNS Static ordering (the part that makes priority real)

1. `buildRoutingDesiredWithTargetScope` collects routing `routing-dns:` static objects
   instead of adding them inline; after all egresses are processed it flushes them in
   one globally sorted pass: (effective priority ↑, egressID, targetID, ruleType,
   domain, logicalID). This makes the desired `Order` of `/ip dns static` reflect the
   device-global priority sequence across egresses.
2. `movablePolicyMenus` gains `ip/dns/static` (excluding access-owned logical IDs via
   `isAccessLogicalID`), so `desiredOrderMoves` emits anchored `move` operations when
   RouterOS order drifts; `validateMoveMenu` allowlists the REST `/ip/dns/static/move`
   path. Creates already land in desired order because creation follows `Order`.
3. Foreign objects are safe: unscoped/uncommented DNS Static entries never enter the
   owned actual graph, and `ScanManagedForDomain(routing)` excludes access-owned ones,
   so generic moves only re-anchor rosboard-routing-owned entries relative to each
   other. Cross-domain Access/Routing overlap is handled by the separate
   Access-first owned-object move pass described below.

## Out of scope (explicit)

- Access↔Access projection blockers remain fail-closed; AccessRule has no priority
  model. Access↔Routing precedence is the explicit follow-up below.
- IP conflict semantics unchanged.
- No schema, entity, or TargetList model changes; wizard UX unchanged (warnings never
  gate apply in `PolicyPlanPreview`).

## Root-review follow-up: cross-domain DNS order

Keep `appendAccessDomainProjectionBlockers` for Access↔Access conflicts, and
replace only the old Access↔Routing blocker with an internal resolution model.
The model records the Access rule/target, Routing rule/egress/target, and the
overlapping DOMAIN/DOMAIN-SUFFIX matcher pairs. It is built from the planning
repositories so proposal overlays are included. The resulting warning is
`cross_domain_access_priority_shadowed`; Access wins the device-global DNS
first-match overlap regardless of matcher specificity or Routing Priority.

`DesiredResult`/the cached plan carries the cross-domain model and the other
domain's desired DNS statics for planning only. The model participates in the
desired hash. A combined RouterOS scan maps comments to both domain graphs and
produces a composite actual fingerprint containing the plan-domain graph plus
all current-manager-owned Access/Routing DNS Static order and structural
fields. Every generate/apply/proposal/final-commit checkpoint compares that
same state.

Cross-domain move planning compares only active desired DNS statics whose
match spaces overlap. For an Access plan, it also compares every active,
current-manager-owned Routing DNS Static's real `name` and `match-subdomain`
matcher, including stale and duplicate owned entries not present in the new
desired graph; the Access static moves before the earliest overlap. Foreign,
unowned, and ambiguous objects are excluded. Missing Access statics are
allowed only in an Access plan that will create them; a Routing plan blocks
with `cross_domain_access_precedence_unavailable` until Access has been
applied and its planned `name`, `type`, `match-subdomain`, `address-list`, and
`forward-to` fields match. The apply path repeats the order convergence after
staged creation, verifies physical order before activation even when a staged
Routing object is currently disabled, verifies active state after activation,
and never moves a foreign or ambiguous object.

For a shared domain TargetList, the manager no longer pre-generates a second
plan. It applies Access, atomically records the first domain as non-terminal,
releases the device gate, generates Routing from the newly committed state,
and applies that fresh plan. Access failure prevents Routing; Routing
planning/apply failure leaves the committed Access domain intact. A shared
IP-only TargetList retains the established Routing-first order and its
follow-up Access failure preserves Routing.
