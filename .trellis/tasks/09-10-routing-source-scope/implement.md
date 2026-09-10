# Routing source scope and TrafficIngress decoupling — implementation plan

Revision: root-review P1 closure; implementation must not start until the revised design Gate is approved.

This is an implementation plan for later approval. This planning turn must not
start the task, edit production code, deploy, commit, or push.

Fixed baseline: `5a3a2197c1940a0bc72c2778614a8e7da0b9e356`.
Branch: `codex/routing-source-scope`.
Target: `main`.

## Slice 1 — backend source-model seam and compatibility

### Scope

- Add the routing-only typed source discriminator and strict normalization.
- Persist only discriminator/interface-list-name data in the additive source
  seam; do not serialize a second Subject/identity/prefix payload.
- Reuse existing Subject/child tables as canonical Device/IP payload storage.
- Extend rule DTOs, proposal clone/hash, CRUD, reload, and revision paths.
- Keep legacy `Subject` + `Ingress` reads/writes and migration behavior exact.
- Implement canonical-write authority for old-client updates:
  unchanged/equivalent legacy source projection may accompany non-source edits;
  source-semantic mismatch is rejected explicitly.

### Non-scope

- No change to RouterOS desired materialization or execution.
- No source-selector UI.
- No Doctor wording change.
- No production deployment.

### Changed areas

- `internal/policyv2/routing_rule.go` and a routing source model file.
- `internal/store/routing_rule.go`, schema initialization, and proposal store.
- `internal/api/policy_routing.go` / routing-rule DTO preparation.
- Focused policyv2/store/API tests.

### Invariants

- Access Control's shared `Subject` behavior is unchanged.
- `source_scope_json` never contains another full Subject, AnchorMAC set,
  pinned/last-IP set, device member set, or duplicate IP-prefix payload.
- Device/IP source payload remains canonical in existing Subject storage.
- A legacy rule with no typed source reads back semantic-equivalent
  `Subject` and `Ingress`.
- Global `TrafficIngress` is not rewritten when saving a canonical rule.
- A canonical rule cannot enter split authority through an old-client write.
- Every canonical source kind and kind-specific payload participates in
  proposal/desired hash and revision/CAS semantics.

### Tests

- source-kind normalization and invalid-payload tests;
- additive DB migration and old-rule read/edit/save round trips;
- Device/IP persistence proving no payload duplication inside
  `source_scope_json`;
- proposal clone/hash preserving source kind plus existing AnchorMAC,
  pinned/last IPs and prefixes;
- canonical Interface/List rule + old-client unchanged-projection non-source
  edit succeeds and preserves source;
- same rule + old-client changed `Subject`/`Ingress` source fails with stable
  409/422-style error and no revision/source drift;
- legacy excluded compound semantics;
- AccessRule regression tests.

### Rollback point

Revert only the additive schema/model/API seam. Existing legacy columns,
Subject child tables and rows remain readable; no RouterOS objects have changed
in this slice.

### Root-review gate

Root review must approve:

1. exact source discriminator/payload ownership;
2. proof that Device/IP payload is not duplicated;
3. old-client canonical-write conflict semantics;
4. compatibility projection and legacy round-trip behavior;
5. `All` / InterfaceList(`all`) representation as deferred safety cases.

Only then may Slice 2 change the desired graph.

## Slice 2 — source compiler, materializer, fresh preflight, and conflicts

### Scope

- Compile Device, IP, Interface, and InterfaceList source kinds.
- Remove TrafficIngress-candidate dependence for those new kinds.
- Emit direct `in-interface` / `in-interface-list` where applicable.
- Emit family-isolated Device/IP source address-lists without ingress fields.
- Add live plan/apply source preflight and warning/blocker classification.
- Block both first-class `All` and built-in InterfaceList(`all`) with stable
  broad-source safety codes.
- Extend conflict comparison for proven source overlap and indeterminate
  warnings.
- Preserve legacy aggregate materialization.

### Non-scope

- No arbitrary AND/OR builder.
- No source topology solver.
- No automatic discovery mutation or capability probe during discovery.
- No change to RouterOutput or Access ordering semantics.
- No enabling of broad-source All semantics.

### Changed areas

- `internal/policyv2/routing_desired.go` and source validation/compiler.
- `internal/policyv2/traffic_ingress.go` compatibility wrapper/split.
- `internal/policyv2/routing_rule.go` conflict helpers.
- `internal/policyv2/desired.go`, manager/reconcile field coverage only where
  required by new desired objects.
- RouterOS source matcher read/capability contract if required.
- planner, family split, apply/preflight, conflict, and recovery tests.

### Invariants

- Device/IP rules never receive an inferred `in-interface`.
- Direct Interface/List rules never require a TrafficIngress candidate or
  rosboard aggregate.
- A missing/renamed/wrong-kind selector fails before the first RouterOS write.
- WAN/disabled/dynamic/bridge/tunnel recommendation is warning-only when the
  matcher is legal.
- InterfaceList(`all`) is visible to discovery later but plan/apply is blocked
  by safety, not by candidate inference.
- Runtime `.id` binding, actual fingerprint, foreign protection, checkpoints,
  verification, and compensation remain unchanged.
- IPv4 and IPv6 source objects stay in their own desired graphs.
- Legacy all/excluded output remains semantically equivalent.

### Tests

- planner assertions for Device/IP/Interface/InterfaceList;
- acceptance cases A, B, C, E, F, G, H and K;
- fresh preflight deletion/rename/wrong-kind cases with zero erroneous writes;
- built-in InterfaceList(`all`) stable blocker;
- warning-only WAN/disabled/dynamic/non-recommended cases;
- direct matcher versus legacy aggregate matcher assertions;
- exact/indeterminate overlap cases and Priority domain behavior;
- stale actual fingerprint, CAS, restart, partial apply, and foreign-object
  regression suites.

### Rollback point

Disable the new typed-source compiler behind the source-kind gate and continue
reading legacy rows. If RouterOS objects were partially staged, use the
existing ownership-aware reconciliation/recovery path; do not delete foreign
objects or reset the database.

### Root-review gate

Root review must inspect desired objects and apply traces for each source kind,
confirm no candidate fallback exists, confirm InterfaceList(`all`) cannot
bypass the deferred All safety gate, and approve conflict severity before UI
work starts.

## Slice 3 — factual source-selector discovery and UI

### Scope

- Add a factual source-selector projection for all successfully read RouterOS
  interfaces and interface-lists.
- Reuse one read-through evidence/cache layer without making facts depend on
  the full TrafficIngress Scanner succeeding.
- Add recommendation/warning metadata as a separate inference layer with
  explicit available/partial/unavailable state.
- Replace Aurora and compact routing wizard ingress selection with typed source
  selection.
- Show every real selector fact, sort recommendations first, and retain warning
  explanations.
- Show built-in interface-list `all` as a real fact but safety-block it with
  the deferred-All reason.
- Keep preview/apply job flow and legacy rule editing behavior.

### Non-scope

- No Doctor schema change.
- No Access UI or shared Subject semantic change.
- No RouterOS mutation during discovery.
- No enabling of first-class `All` or InterfaceList(`all`) apply.

### Changed areas

- `internal/policyv2/discovery.go` or a sibling factual projection using the
  same read-through snapshot/cache.
- `internal/api/policy_routing.go` source-selector endpoint/tests.
- `web/src/features/policy/canonical.ts`, source helpers, wizard, routing page.
- Compact policy canonical/source/wizard/page copies.

### Invariants

- `/interface` fact success is sufficient to return those interface facts even
  when route/bridge/WAN inference fails.
- `/interface/list` fact success is sufficient to return those list facts under
  the same condition.
- Fact visibility is independent from recommendation status.
- A non-recommended valid selector is never hidden/disabled because of role
  inference.
- Built-in `all` remains visible but carries an explicit safety-blocked state.
- The UI never treats discovery as apply proof.
- Existing legacy rules are explainable and not silently converted.
- Access Control UI remains unchanged.

### Tests

- source-selector API exposes non-recommended bridge/WAN and all factual lists;
- forced `/ip/route` or bridge-analysis failure while `/interface` and
  `/interface/list` succeed still returns the facts and marks inference
  partial/unavailable;
- wizard can select a non-recommended real interface and sends typed
  `sourceScope`;
- InterfaceList(`all`) is displayed with stable broad-source safety reason and
  cannot proceed to a successful plan;
- no-candidate Device/IP wizard path remains valid;
- legacy wizard edit round trip;
- canonical parser/serializer parity for Aurora and compact surfaces.

### Rollback point

Keep the old wizard and old `trafficIngress` field available while the new
source-selector endpoint is disabled. Discovery is read-only, so rollback does
not require RouterOS cleanup.

### Root-review gate

Root review must verify:

1. fact availability survives unrelated inference failure;
2. facts and recommendation metadata are visibly separate in both UIs;
3. recommendation warnings never become admission blockers;
4. built-in `all` is visible but cannot bypass the deferred All safety gate.

## Slice 4 — legacy hard-gate retirement and Doctor wording

### Scope

- Deprecate the old UI-only TrafficIngress gate after canonical rules use the
  new source seam.
- Ensure global scope changes cannot block new source rules or rewrite
  canonical per-rule sources.
- Retain compatibility endpoints and legacy aggregate compilation until a
  separately approved cleanup.
- Adjust Doctor wording from candidate availability to recommendation/anomaly
  language while preserving `IngressDecision` trace and exports.
- Update stable documentation/specs only if the new convention is accepted.

### Non-scope

- No automatic migration of excluded rules into a new boolean model.
- No deletion of the global database field or compatibility API in this slice.
- No production deployment or acceptance claim.
- No implementation of `All` unless a new root-approved safety slice is added.

### Changed areas

- legacy API compatibility tests and UI wording;
- `internal/diagnostics/deep.go` and Doctor UI/tests;
- policy routing specs and task documentation.

### Invariants

- Doctor remains diagnostic and non-blocking for valid source selectors.
- Existing trace/export paths remain schema-compatible.
- Legacy rules preserve their old matcher semantics.

### Tests

- old clients can still read/write global scope;
- canonical source rules ignore candidate false negatives and global edits;
- Doctor “only WireGuard” is a warning about recommendations, not routing
  availability;
- deep report/trace/export regression tests.

### Rollback point

Re-enable legacy wording/UI gate without touching canonical source rows or
RouterOS objects. Keep the compatibility endpoint and trace unchanged.

### Root-review gate

Root review must explicitly approve the deprecation wording and confirm that no
old rule is silently migrated or blocked.

## Deferred safety gate — `All`

Before any implementation of `RoutingSourceKind=all`, produce a separate
review artifact and tests for:

- forwarded `prerouting` versus router-originated `output`;
- existing `RouterOutput` and policy-owned connection/routing marks;
- management/control-plane traffic and local-destination exclusions;
- loop prevention and re-entry behavior;
- rule ordering and multiple egresses;
- fail-closed behavior when the safety proof is unavailable.

Until that gate is approved, `All` remains a stable explicit blocker, never an
implicit empty source or a candidate fallback.

## Validation and handoff checklist

For each implementation slice, run focused Go/frontend tests, inspect the
exact diff and status, and keep the branch/PR draft. Before any production
delivery (not part of this design task), the repository acceptance gate must
also be followed, including test-machine verification, NAS backup, fresh
remote checks, and user manual inspection.

This planning turn's completion criteria are only:

- code archaeology recorded;
- `prd.md`, `design.md`, and `implement.md` reviewed;
- no production code changed;
- no deployment, commit, push, or cleanup performed;
- root review requested before `task.py start`.
