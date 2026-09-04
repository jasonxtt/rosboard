# Implement checklist

- [x] `internal/policyv2/routing_rule.go`
  - `routingDomainProjection` gains `priority`, `ruleName`, `consumerRuleIDs`,
    `enabledConsumers`; built in ascending-Priority order so the first enabled
    consumer wins attribution and min(Priority) is the effective priority.
  - `DomainProjectionContextAmbiguities` → `DomainProjectionResolutions`
    (same-rule exemption via `projectionsShareEnabledRule`; different Priority →
    warning `domain_projection_priority_shadowed` with winner/loser reason;
    equal Priority → blocker `domain_projection_context_ambiguous` naming
    matcher + rule names + priority; no UUIDs / projection jargon).
  - `DomainProjectionPriorities` exported helper for the desired builder.
  - `routingTargetsOverlap`: domain-only overlap no longer a logical conflict
    (shared domain TargetList delegates to projection resolution; distinct-ID
    pairs only conflict on IP content via `targetIPRulesOverlap`).
- [x] `internal/policyv2/routing_desired.go`
  - resolutions split into `result.Blockers` / `result.Warnings`.
  - routing DNS Statics collected into `routingDNSStaticEntry` and emitted in
    one globally sorted pass (priority, egress, target, ruleType, domain,
    logicalID) so `DesiredObject.Order` carries the device DNS first-match order.
- [x] `internal/policyv2/reconcile.go`
  - `movablePolicyMenus` gains `ip/dns/static`; `movableDNSStatic` excludes
    access-owned and stale-access statics from generic moves.
- [x] `internal/routeros/mutation.go` — `validateMoveMenu` allowlists
  `MenuIPDNSStatic` (REST `/ip/dns/static/move` via the existing typed Move).
- [x] `internal/policyv2/manager.go` — after a DNS-static batch create, resolve
  created RouterOS IDs by comment identity (`recordBatchCreatedDNSStatics`) so
  later moves referencing batch-created statics never fail on an empty ID.
- [x] Tests (see prd acceptance list) + spec §8 paragraph updated.
- [x] Gates: gofmt clean; `go build`; `go vet ./...`; `go test ./...`;
  `go test -race ./...`; `git diff --check`. No frontend changes → web lint/build
  not required.

## Root-review P1 round (2026-09-04)

- [x] P1-1 `internal/policyv2/routing_rule.go`
  - Active DNS consumer = `rule.Enabled && egress exists && egress.Enabled &&
    !egress.PendingDeletion` (`activeConsumers`, renamed from `enabledConsumers`;
    `projectionsShareActiveRule`). Inactive projections keep a deterministic
    fallback ordering priority (`consumerRuleIDs` min) but never shadow, warn,
    block, or become winner/loser.
  - Regression: `TestDomainProjectionDisabledEgressDoesNotArbitrateDNSConflicts`
    (disabled high-priority vs enabled low-priority → no warning; equal priority
    → no blocker; missing egress → no arbitration; re-enable → warning restored
    with A winning; blocker restored at equal priority; order priority preserved).
  - Unit fixtures updated to set `Enabled: true` on arbitrating egresses.
- [x] P1-2 `internal/policyv2/manager.go` `recordBatchCreatedDNSStatics`
  - Strict per-identity matching: 0 matches → error; 1 non-empty-ID match →
    record; >1 matches → error refusing to choose; matched object without `.id`
    → error; identity collision inside the expected batch → error before any
    List (no silent overwrite). Errors carry logicalID, ownership identity, and
    match count only. Foreign/other-commented statics remain ignored.
  - Extended `TestRecordBatchCreatedDNSStaticsResolvesIDsByCommentIdentity`
    (0/1/2-match, expected-side collision, missing-ID match, plus "failed
    resolution records no ID").
- Unchanged this round: DNS static movable/ordering code, IP conflict logic,
  Access↔Access blocker semantics, TargetList model, RoutingRule schema,
  frontend. Access↔Routing precedence is handled by the follow-up below.
- Gates after P1 round: gofmt clean, build, vet, `go test ./...`,
  `go test -race ./...`, `git diff --check` all pass. Not deployed, not
  committed, not pushed.

## Known pre-existing oddities (left untouched)

- `buildRoutingDesired` wrapper has been dead code since before this task
  (no callers at HEAD); kept to avoid unrelated cleanup.
- Legacy V1-authority source-based DNS statics (`dns:<egress>:...` in
  desired.go) keep insertion order; they predate the RoutingRule model and
  are outside this task's scope.

## Root-review follow-up: Access-first cross-domain order

- [x] Replace the Access↔Routing blocker with Access-first overlap warnings and
  an internal cross-domain constraint model; keep Access↔Access and IP
  blockers unchanged.
- [x] Build combined Access/Routing DNS actual scans, composite order/model
  fingerprints, stale checkpoints, and fail-closed Routing precedence checks.
- [x] Plan and execute only owned Access-before-Routing DNS Static moves,
  including post-create convergence and pre/post-activation verification.
- [x] Change shared TargetList applies to Access commit followed by a fresh
  Routing plan/apply, with per-domain gate release and failure preservation.
- [x] Add resolver, move/idempotency, stale, foreign-object, proposal, and
  shared-target sequencing regressions.
- [x] Update the backend policy-routing spec and run focused tests, race,
  vet/build, full tests, and `git diff --check`; do not deploy or commit.

## Root-review P1 remediation (2026-09-04)

- [x] Routing/Combined precedence checks validate the planned Access DNS
  projection fields (`name`, `type`, `match-subdomain`, `address-list`, and
  `forward-to`) and fail closed on drift; field-by-field regressions added.
- [x] Access order convergence and verification include every active,
  current-manager-owned Routing DNS Static matcher, including stale and
  duplicate entries, while excluding foreign/ambiguous objects; earliest-anchor
  and duplicate regressions added.
- [x] Pre-activation order verification covers staged disabled Routing statics;
  no-op Move integration regression proves activation is not reached.
- [x] Follow-up commits persist Access applied state and the same job's
  non-terminal `follow_up` state atomically; blocked first-commit and final
  state regressions added.
- [x] Shared Domain targets use Access-first/fresh-Routing sequencing while
  IP-only targets retain Routing-first behavior.
