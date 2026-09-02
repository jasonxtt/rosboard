# Unified Target Library and Policy Rule Model — Implementation Plan

## Status / gate

The original planning gate has already been passed for this task. This
worktree contains the prior slices and the user-approved Slice 4 revision is
now the implementation scope. Root review and test-machine runtime acceptance
remain required before any deployment; this turn must not deploy, commit, or
archive.

The revision is intentionally split into Slice 4A and 4B below. They remain
inside this task; do not create or reactivate another top-level task.

No runnable-code commit is created before the project’s runtime/manual acceptance gate.

## Slice 4 root-review corrections

The RoutingRule wizard now keeps its edits in a read-only proposal overlay
until the ChangePlan's explicit “确认并应用”. Proposal plans carry the base
desired revision, proposal hash, dependency revisions, and desired hash;
approval atomically commits the egress, TrafficIngress, RoutingRule, and any
new preset TargetList/version rows before the reviewed RouterOS job starts.
Back/Close and unrelated source-refresh or GenerateAndApply paths therefore
cannot consume an unapproved draft.

The wizard references existing Egress rows when editing a RoutingRule. Egress
creation/editing remains in its separate page flow, while the shared
RoutingRule/AccessRule TargetSelector keeps application choices as virtual
preset IDs until the relevant explicit save/approval path materializes them.
The compact application tail uses the Chinese `域名`, `IP`, and `域名/IP`
labels, and the review layer retains preset presentation metadata so raw
`preset:*` identifiers are never the primary user-facing label.

## Preflight before Slice 1

### P0.1 Read project instructions

Read:

```text
/Users/tom/github/AGENTS.md
/Users/tom/github/rosboard/AGENTS.md
.trellis/workflow.md
```

Load the active task and the context files from `implement.jsonl`.

### P0.2 Verify baseline without rewriting history

Record:

```text
branch
HEAD
origin/main...HEAD
git status --short
git diff --stat
```

Expected planning baseline is around:

```text
branch = main
HEAD = e952d17
main ahead of origin/main by 50 commits
```

but trust the live repository, not the handoff text.

Current OAF worktree is intentionally dirty and must not be “cleaned.”

Forbidden:

```text
git reset --hard
git clean
git restore .
git checkout .
git rebase
git amend
git revert
git cherry-pick
```

Do not split or rewrite historical commit `389c576`.

### P0.3 Verify OAF snapshot

Before runnable-code changes, confirm the runtime-approved OAF worktree has been preserved outside the repo as required by the handoff.

Expected snapshot family:

```text
/Users/tom/nas/wyp/github/rosboard-dev-snapshots/2026-09-01-oaf-runtime-approved/
```

It must preserve at least:

- HEAD;
- git status;
- git diff --stat;
- `git diff --binary HEAD`;
- required untracked files, especially `internal/store/policy_v2_applications_test.go`.

If absent, run the approved local backup flow first and create/verify the snapshot. Do not create a WIP code commit as backup.

### P0.4 Activate only this task

After root planning approval:

```text
.trellis/tasks/09-01-unified-target-policy-model/
```

becomes the implementation task.

The old task:

```text
.trellis/tasks/09-01-oaf-application-catalog/
```

remains preserved and is not modified, archived or used as the active implementation context for this refactor.

---

# Slice 1 — Canonical Target Library foundation

## Goal

Make `TargetList` the canonical shared backend concept while preserving all existing Source content/version behavior and **without changing RouterOS routing authority yet**.

At the end of Slice 1:

```text
canonical TargetList exists
canonical Target Library API exists
existing Source storage/version/parser is reused
legacy Source.EgressID still drives routing only as a temporary compatibility seam
no RoutingRule yet
no RouterOS behavior change
```

## 1.1 Introduce canonical TargetList model

Create the smallest shared model boundary justified by two consumers.

Preferred direction from `design.md`:

- small `internal/targetlist` model/normalization package, or an equivalently clean shared location;
- no business `EgressID` field;
- domain/ip kind;
- url/upload/manual/preset source type contract;
- preserve active/pending/last-good/version metadata semantics.

Do not duplicate `policyv2.Source` logic long-term. The implementation may use temporary conversion helpers only while old API compatibility exists.

### Verify

Tests prove:

- legacy domain Source row maps to TargetList with same ID/state;
- legacy IP Source row maps to TargetList with same ID/state;
- no canonical TargetList JSON/API field exposes Egress ownership;
- kind/type immutability remains enforced.

## 1.2 Evolve persistence in place

Reuse:

```text
policy_v2_sources
policy_v2_source_versions
policy_v2_source_rules
```

Do not create copied TargetVersion/TargetRule tables.

Add only genuinely required new storage such as `preset_id` with additive/default-safe migration.

Refactor existing Source SQL into canonical TargetList operations rather than maintaining two separate SQL implementations.

Keep `policy_v2_sources.egress_id` only as the temporary legacy routing association for this slice.

### Verify

Regression tests prove preservation of:

- ID;
- revision;
- URL/manual/upload semantics;
- schedule;
- active/pending/last-good IDs;
- ETag / Last-Modified;
- next-run time;
- version SHA/content/state/error/status;
- parsed rules/counts/diff;
- restart/re-open behavior.

Use fixture databases representing both domain and IP content.

## 1.3 Reuse preview/fetch/version pipeline

Canonical Target APIs must call the existing:

- URL fetcher;
- upload service;
- manual parser;
- Clash parser;
- preview cache;
- pending version creation;
- scheduled refresh/ETag/Last-Modified logic.

Do not copy handlers wholesale if helper extraction can make both old and new endpoints call one implementation.

No ApplicationPreset download behavior is implemented in Slice 1 beyond accepting/storing the source-type vocabulary if technically necessary.

### Verify

Targeted API tests for:

```text
URL preview
upload preview
manual preview
save from preview
rules pagination
refresh / not-modified
invalid kind/type
stale revision
```

Existing Source API tests must continue to pass for the compatibility window.

## 1.4 Add canonical Target Library API

Add top-level shared resource per design:

```text
/api/target-lists?device=...
```

including CRUD, rules, refresh, URL/upload/manual preview.

Canonical responses omit `egressId`.

Expose computed usage in a minimal form if available in this slice; before RoutingRule exists, AccessRule usage is sufficient and routing usage may derive from the legacy Egress association. Do not create a usage subsystem.

Deletion must remain blocked when an AccessRule references the TargetList.

## 1.5 Keep legacy policy routing unchanged

`BuildDesired` and current policy-routing Source/Egress behavior stay semantically unchanged in Slice 1.

Mark all direct uses of `Source.EgressID` that will be replaced in Slice 2 as temporary compatibility sites; do not create another abstraction merely to hide them.

No production frontend cutover is required in this slice. Existing UI remains on legacy endpoints until rule authority moves.

## Slice 1 validation

At minimum run the relevant targeted packages, then:

```text
go test ./internal/policyv2/...
go test ./internal/store/...
go test ./internal/api/...
go test ./internal/accesscontrol/...
go test ./internal/service/...
go test ./...
go vet ./...
git diff --check
```

Run focused race tests for changed packages. Full race may be deferred until final if project runtime makes it materially expensive, but any race-sensitive store/API changes need targeted `-race` now.

### Slice 1 root-review checklist

- TargetList is genuinely canonical, not just an extra DTO over duplicated Source logic.
- No new Egress ownership appears in canonical TargetList.
- Storage/version tables were reused.
- RouterOS desired behavior did not change.
- No ApplicationPreset/OAF/Subject work leaked into scope.
- Diff is smaller than a parallel subsystem approach.

**STOP after Slice 1 report.**

---

# Slice 2 — RoutingRule authority, Subject core, migration, RouterOS projection

## Goal

Move routing policy ownership from `Source.EgressID` to explicit `RoutingRule`, while preserving existing routing behavior through deterministic migration.

At the end of Slice 2:

```text
TargetList independent from Egress
RoutingRule authoritative
existing Egress/Source relationships migrated
RoutingRule subject supports all / selected terminal / IP / CIDR
routing desired graph uses rules, not Source.EgressID
minimal overlap conflict validation exists
```

No ApplicationPreset or AccessRule target migration yet.

## 2.1 Add RoutingRule persistence

Implement only the tables in `design.md`:

```text
policy_v2_routing_rules
policy_v2_routing_rule_targets
policy_v2_routing_rule_members
policy_v2_routing_rule_prefixes
policy_v2_schema_meta (small authority marker)
```

No polymorphic/generic rule table.

Implement CRUD with optimistic revision semantics consistent with existing Egress/AccessRule patterns.

Rules:

- Egress delete is blocked while referenced by a RoutingRule;
- TargetList delete is blocked while referenced by RoutingRule or AccessRule;
- deleting/disabling an Egress does not rewrite TargetList ownership;
- rule target ordering is deterministic.

## 2.2 Extract minimal shared Subject semantics

Now that RoutingRule is the second real consumer, extract only the non-trivial common normalization/resolution logic from Access Control.

Reuse current behavior for:

- auto/fixed member binding;
- MAC anchor normalization;
- current/last trusted IPv4/IPv6;
- identity conflict behavior.

Add manual exact IP/CIDR normalization using `netip`.

Keep AccessRule and RoutingRule persistence separate.

Do not build a generic expression tree.

### Tests

- exact IPv4→canonical /32;
- exact IPv6→/128;
- canonical masked CIDRs;
- duplicate normalization;
- auto/fixed terminal behavior parity with existing Access Control;
- identity conflict/last-known parity.

## 2.3 Transactional legacy migration

Implement migration exactly once using deterministic IDs and the authority marker.

For every non-pending-deleted Egress with legacy associated TargetLists:

```text
one RoutingRule
subject=all
targets=all legacy associated targets
egress=existing egress
priority=existing egress priority
enabled=true
```

Include disabled TargetLists as references; their TargetList.Enabled state preserves current behavior.

Unassigned TargetLists remain library-only.

After equivalent rules are inserted, clear legacy `egress_id` values and set the marker in the same transaction.

Bump desired revision.

### Migration tests

Cover:

- one Egress / one target;
- one Egress / multiple targets;
- multiple Egresses;
- unassigned target;
- disabled Egress;
- disabled TargetList;
- pending-deleted Source/Egress not resurrected;
- replay/reopen is idempotent;
- injected migration failure rolls transaction back completely;
- original IDs/version rows unchanged;
- migration marker only appears on successful commit.

## 2.4 Make RoutingRule the only routing authority

Refactor `BuildDesired` and helper paths so routing target selection comes from RoutingRule relations, never `TargetList`/legacy Source Egress ownership.

Remove `enabledSourcesByEgress` authority.

Routing order becomes rule priority with deterministic tie-breakers.

Keep Egress mechanics unchanged where still valid:

- route tables;
- gateway discovery;
- DNS transport;
- NAT;
- failure modes;
- enabled families.

Existing `Egress.ListMode/ListName/Priority` may remain physically but no longer drive canonical rule targeting/order after migration.

## 2.5 Routing target projections

### IP

Generate deterministic `(device,egress,target)` address lists and reuse them across rules sharing same Egress+Target.

### Domain

Generate deterministic `(device,egress,target)` projections. Before applying
the DNS capability check, deduplicate all enabled RoutingRule references by
that physical key. Same Egress plus same Target is therefore one safe shared
projection, while distinct physical projections with exact/suffix-overlapping
domain content are ambiguous even when their DNS context values match.

Do **not** use duplicate same-name static FWD entries to fake source-aware DNS behavior.

If enabled rules require overlapping domain content through distinct physical
`(egressID,targetID)` projections:

- do not report a logical RoutingRule conflict solely for that reason;
- emit `domain_projection_context_ambiguous` as a RouterOS capability blocker;
- leave IP-only rules unaffected.

This distinction must be covered by tests.

## 2.6 Routing subject projection

For selected subjects:

- create stable per-rule IPv4/IPv6 source address lists from resolved terminal addresses + manual prefixes;
- mangle rules match both source subject list and target destination list;
- unresolved member problems degrade only the relevant member evidence where safe, following existing Access Control behavior;
- contradictory identity removes stale wrong projection.

For `subject=all`, omit source list matcher and keep Policy TrafficIngress as the ingress boundary.

For `Egress.RouterOutput`, use the union of enabled RoutingRule targets pointing to the Egress; do not apply end-device Subject to router output.

## 2.7 Minimal conflict validation

Before plan/apply, validate enabled rule pairs with different Egresses for the
logical conflict, then validate distinct physical domain projections:

```text
SubjectOverlap && TargetOverlap
```

Implement only the overlap cases in `design.md`.

Do subject check before loading/comparing target contents.

Priority never overrides a proven contradictory overlap.

Keep physical DNS projection ambiguity as a separate capability issue. The
projection check deduplicates enabled references by `(egressID,targetID)` and
also covers different target IDs on one Egress.

## 2.8 RoutingRule API

Add CRUD under existing policy-routing route family:

```text
/api/policy-routing/rules?device=...
```

Plan/preview responses must identify rule-level blockers/warnings clearly enough for the future UI.

Once RoutingRule is authoritative, legacy Source Egress mutation must no longer be writable authority. Do not keep two writable associations. It is acceptable for the local legacy frontend to be temporarily incompatible between Slice 2 and the final frontend cutover because no runtime deployment occurs before all slices are accepted; do not add a complex write-through compatibility layer solely to preserve that temporary local UI.

## 2.9 Semantic-equivalence tests

Construct existing-policy fixtures and prove that after migration the new all-subject RoutingRule desired graph has equivalent routing meaning for:

- domain source;
- IPv4 source;
- IPv6 source;
- multiple targets sharing an Egress;
- shared/dedicated legacy modes where still represented;
- DNS transport;
- router output;
- disabled target/Egress.

Exact logical IDs may legitimately change when projection context identity changes, but externally managed RouterOS objects must remain untouched and resulting managed semantics must match.

## Slice 2 validation

Run targeted routing/store/API/access regression tests plus:

```text
go test ./internal/policyv2/...
go test ./internal/store/...
go test ./internal/api/...
go test ./internal/accesscontrol/...
go test ./internal/routeros/...
go test ./...
go vet ./...
git diff --check
```

Run focused `-race` for store/policyv2/API.

No deployment to `10.0.0.60` yet unless root reviewer explicitly asks for an intermediate runtime probe.

### Slice 2 root-review checklist

- No routing code consults legacy `egress_id` after authority marker.
- One-per-Egress migration is idempotent and lossless.
- Subject reuse did not become a generic framework.
- Target projections are context-specific without persistent projection registry.
- DNS global limitation is handled explicitly, not hidden by ordering assumptions.
- External RouterOS objects remain outside ownership.
- No AccessRule/OAF migration scope leaked in.

**STOP after Slice 2 report.**

---

# Slice 3 — AccessRule canonical targets + ApplicationPreset backend + attribution

## Goal

Complete the shared Target/Subject backend model for Access Control, add the lightweight application-preset source, and replace OAF traffic attribution lookup without yet doing broad UI cleanup.

At the end of Slice 3:

```text
AccessRule = Subject + internet|targets + block
Application picker creates/reuses ordinary TargetLists
OAF ApplicationIDs are no longer canonical enforcement
traffic attribution can use preset-domain registry
OAF cleanup is ready but final deletion waits for Slice 4 frontend cutover
```

## 3.1 Migrate AccessRule source contract to TargetList

Canonical fields:

```text
subject
targetScope = internet | targets
targetListIds[]
```

Reuse `access_rule_sources` rows as TargetList IDs unless a rename is genuinely required. Do not copy relations just for naming.

Existing source-target rules migrate with identical IDs/order.

Internet rules remain unchanged.

Update validation so rule shape is explicit and mutually exclusive.

## 3.2 Support shared Subject in AccessRule

Map existing AccessRule members to the shared subject semantics without losing stored MAC anchors, pinned/current/last addresses.

Add manual prefix persistence only as needed for the new selected Subject UI; do not overload fake terminals.

### Access `subject=all`

Use already trusted local TerminalScope prefixes to populate the rule’s client subject list per family.

If a family has no trustworthy local scope:

- do not emit an unconstrained forward rule;
- block/degrade that family explicitly.

Continue using existing internet egress discovery for `targetScope=internet`.

## 3.3 Make Access target projections consumer-specific

Stop relying on one physical source list being shared between routing and access.

Build access target lists keyed by access context + TargetList.

- domain target → Access DNS forwarder/static FWD → access target list;
- IP target → static IPv4/IPv6 access target list;
- existing filter jump/deny behavior consumes these lists.

Preserve current TCP reset / UDP+other drop semantics and existing capability gates.

## 3.4 Add the generated ApplicationPreset catalog

Use a source-controlled/generated manifest covering valid YAML files under the
selected bm7 `rule/Clash` tree. Runtime reads local metadata only; it must not
crawl GitHub or download the whole catalog. Each entry contains:

```text
id
name
category
aliases
relative rule path
ruleURL
```

The runtime URL is derived from one fixed raw base. No dynamic
crawler/provider framework is needed.

Use the configured v1 rule source decision from `design.md` and preserve provenance in comments/docs.

Add:

```text
GET /api/application-presets
POST /api/application-presets/{id}/preview?device=...
```

Preset preview must reuse the existing fetcher/Clash parser and return separate domain/IP counts and ignored rule counts.

## 3.5 Preset-backed TargetList creation/reuse

Implement deterministic uniqueness:

```text
(presetID, kind)
```

Selection behavior:

- domain checkbox → create/reuse domain TargetList;
- IP checkbox → create/reuse IP TargetList;
- both → two normal TargetList IDs;
- no mixed target;
- default to Domain when available, otherwise IP;
- materialize only the requested kinds; an existing unrequested kind is not
  implicitly selected;
- IP warning belongs to API metadata/UI, not a risk-scoring subsystem.

Persist `preset_id` and resolved URL; preset rows are hidden from the primary
Target Library table, their URL is not user-editable, and deletion is
protected. Refresh continues through the existing URL/version pipeline.

Refresh uses the existing URL refresh/version path.

## 3.6 Deterministic OAF application-rule migration

Before OAF enforcement is removed:

- convert resolvable OAF application IDs into deterministic domain TargetLists using supported domain signatures;
- replace AccessRule application relation with TargetList relations;
- unresolved IDs become explicit degraded/disabled legacy records/issues and are never silently deleted or widened.

Tests must cover available, missing and ambiguous catalog data.

This migration is transitional only; do not build a compatibility service around OAF IDs.

## 3.7 Replace attribution lookup

Reuse existing `ApplicationResolver` MosDNS evidence collection/window/TTL behavior.

Replace only domain→application lookup with a lightweight preset-domain registry generated from supported preset domain rules.

Preserve:

- device isolation;
- freshness window;
- TTL checks;
- newest evidence behavior;
- ambiguous domain → no guess;
- protocol-analysis-off behavior;
- ApplicationID/Application/Service output contract.

Change new stable application IDs from `oaf:<numeric>` to preset IDs only when canonical preset attribution is active.

## Slice 3 validation

Targeted tests must include:

- Access source→target migration;
- internet rules unchanged;
- selected terminal auto/fixed parity;
- all-subject trusted local scope behavior;
- manual IPv4/IPv6 CIDR;
- domain/IP access projection;
- target disabled behavior;
- preset preview domain/IP split;
- unsupported matcher ignored counts;
- duplicate preset selection reuse;
- OAF resolvable/unresolvable migration;
- attribution exact/suffix/ambiguous/stale/device-isolated cases.

Then run:

```text
go test ./internal/accesscontrol/...
go test ./internal/policyv2/...
go test ./internal/store/...
go test ./internal/api/...
go test ./internal/service/...
go test ./...
go vet ./...
git diff --check
```

Run focused race tests for changed store/service paths.

### Slice 3 root-review checklist

- AccessRule and RoutingRule remain separate models.
- Existing RuleMember semantics are reused, not cloned.
- Access target projection no longer depends on routing Egress ownership.
- Presets feed ordinary TargetLists only.
- No mixed kind or provider framework.
- OAF data is migrated deterministically before deletion.
- Attribution registry is metadata, not enforcement state.

**STOP after Slice 3 report.**

---

# Slice 4 — Frontend cutover, legacy/OAF cleanup, full acceptance

## Goal

Switch the user experience to the already-planned product model, remove obsolete OAF/legacy Source-owned surfaces, and perform final full-system acceptance.

Backend contracts must already be root-approved before frontend work begins.

## 4.1 Target Library UI

Create the user-visible Target Library page from `research/ui-flow-and-api.md`.

Reuse current source editor pieces for:

- URL/upload/manual preview;
- rule samples;
- ignored counts;
- version/refresh state;
- domain/IP terminology.

Add Application source choice backed by the preset API.

Do not copy the existing Source page wholesale and keep both forever.

## 4.2 Shared TargetSelector

Extract one real shared component used by both rule flows:

```text
应用
我的域名列表
我的 IP 列表
quick add URL/upload/manual
```

Application selection returns ordinary TargetList IDs.

## 4.3 Shared SubjectSelector

Extract one shared interaction component for:

```text
all
selected devices
manual IP/CIDR
```

Reuse existing Access Control device picker behavior and advanced fixed binding where applicable.

Do not build GenericRuleForm.

## 4.4 RoutingRule UI

Replace Egress-owned Source workflow with the three-step RoutingRule flow:

```text
谁的流量
→ 访问什么
→ 怎么走
```

Policy routing page shows explicit rule rows and keeps Egress configuration separate.

Remove user-facing source assignment from Egress.

Surface blockers distinctly:

- logical overlap conflict;
- DNS physical-projection capability ambiguity;
- unresolved subject evidence;
- unavailable target/Egress.

## 4.5 AccessRule UI

Switch to:

```text
谁的流量
→ 阻止整个互联网 / 指定目标
→ 确认
```

Remove OAF-as-independent-target UX.

Keep AccessRule-specific future time-control area separate from shared selector abstractions.

## 4.6 Remove obsolete OAF enforcement/runtime surfaces

Only after new backend + frontend no longer depend on them, remove:

- `TargetScopeApplications` canonical path;
- `ApplicationIDs` canonical rule relation;
- `dns:application:*` generation;
- `rb_app_*` generation;
- OAF feature.cfg catalog parser/runtime where no longer required;
- OAF-specific configuration/status API/UI;
- tests that only assert deleted OAF behavior, replacing them with preset/target migration tests.

Do not remove traffic ApplicationID/Application/Service fields.

Do not delete unresolved legacy records without explicit migration handling.

## 4.7 Remove legacy Source-owned routing surfaces

After no production frontend/API consumer needs them:

- remove `egressId` from public target/source UX;
- remove writable old Source↔Egress API behavior;
- remove overview nesting that implies Egress owns TargetLists;
- remove unused `enabledSourcesByEgress`-style helpers;
- remove only code made dead by this architecture.

Physical DB names/columns may remain if deleting/renaming them is cosmetic and risky. Document residual storage names rather than performing unnecessary schema churn.

## 4.8 Frontend validation

Use the repository’s actual package manager and scripts.

At minimum run relevant:

```text
typecheck
lint
build
```

and existing frontend tests if configured.

Manually inspect responsive behavior for the three key flows at normal desktop width and a narrow/mobile width.

No aesthetic redesign outside this scope.

## 4.9 Full backend validation

Run the complete required suite, including:

```text
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

If full race has a known pre-existing baseline failure, document exact command/output and prove all changed packages pass race; do not silently downgrade the gate.

Run Trellis task validation.

Search for stale canonical references:

```text
TargetScopeApplications
ApplicationIDs
rb_app_
dns:application:
Source.EgressID / egress_id authority sites
```

Any remaining occurrence must be either:

- migration compatibility with a documented deletion boundary;
- legacy physical schema name only;
- historical test/fixture intentionally retained.

## 4.10 Root implementation review

Before any runtime deployment, submit full implementation report including:

- final models;
- migration behavior;
- schema changes;
- RouterOS desired mapping for all four Target/consumer combinations;
- conflict behavior;
- DNS projection limitation handling;
- OAF migration/cleanup;
- tests/commands/results;
- exact git status/diff stat;
- confirmation that production was not accessed.

Wait for root review approval.

## 4.11 Runtime acceptance on test machine only

After implementation root review APPROVED, deploy only to:

```text
10.0.0.60
```

using its isolated config/data and never production credentials/data.

Acceptance should cover at least:

1. existing Source/Egress fixture/database migration;
2. Target Library URL/manual/upload creation and refresh;
3. domain RoutingRule all subject;
4. IP RoutingRule selected subject;
5. same target/same Egress multi-rule reuse;
6. different subject/different Egress IP target allowed;
7. overlapping subject+target/different Egress blocked;
8. overlapping distinct domain physical projections surfaced, not silently applied;
9. AccessRule selected domain target;
10. AccessRule selected IP target IPv4/IPv6;
11. AccessRule all subject with trusted local scope;
12. Internet AccessRule unaffected;
13. ApplicationPreset → TargetList creation;
14. traffic attribution from preset registry;
15. managed-object cleanup when a rule/target is disabled/deleted;
16. no mutation of foreign RouterOS objects.

Do not access production `10.0.0.6`.

## 4.12 User/manual acceptance, commit, archive

After test-machine runtime acceptance, report results to the user.

Production deployment is still forbidden unless the user explicitly authorizes it and the production backup gate in `AGENTS.md` is followed.

Runnable-code commit/archive occurs only after the applicable user manual acceptance gate.

No `git add -A`; stage only task-related paths after reviewing the complete diff.

Archive the Trellis task only after final accepted commit and session/spec updates required by workflow.

---

# Slice 4 revision — Frontend/product-flow correction + ApplicationPreset catalog (4A)

This is the authoritative replacement for the earlier Slice 4 frontend
checklist. Complete the product-flow correction and catalog before treating the
worktree as ready for review.

## 4A.1 Target Library and shared selector

- show only user-managed `manual`, `url`, and `upload` TargetLists in the
  library's primary domain/IP tables;
- keep preset-backed TargetLists canonical and hidden, with ID/version/refresh/
  usage/deletion behavior intact;
- move Application catalog browsing into one shared TargetSelector used by
  RoutingRule and AccessRule;
- reconstruct application chips from hidden preset TargetList IDs when editing
  an existing rule;
- keep quick-add URL/upload/manual flows connected to the existing preview and
  version pipeline.

## 4A.2 Full bm7 catalog and requested kinds

- replace the small hardcoded preset list with a source-controlled/generated
  manifest covering valid Clash YAML paths under bm7's `rule/Clash` tree;
- keep runtime catalog access local and metadata-only; fetch a YAML only for an
  explicit preview/materialization;
- search by display name, stable ID, and aliases/keywords;
- implement `requestedKinds` as `[domain]`, `[ip]`, or both, with Domain-first
  defaults and no empty/unrequested materialization;
- test stable IDs, generated raw URLs, meaningful coverage, lazy fetching, and
  deterministic `(presetID, kind)` reuse.

## 4A.3 Restored RoutingRule wizard

Reuse the old `PolicyWizard`, `PolicyEgresses`, `TrafficIngressForm`, draft,
preview, and change-plan interaction ideas from `HEAD` without restoring old
source ownership or OAF contracts. The final four stages are:

1. Egress/interface/point-to-point/next-hop configuration and discovery,
   including IPv4/IPv6, DNS upstream, Fake DNS, route/failure mode, routing
   table, advanced settings, and RouterOutput;
2. device-global TrafficIngress plus `all`, `selected`, or `excluded` source
   scope; excluded is disabled and backend-rejected without valid ingress;
3. shared Application/TargetSelector with Domain-first tail selection;
4. readable ChangePlan preview, blockers/warnings, and explicit apply.

Remove the user-facing shared/dedicated ListMode/ListName option. Existing
Egress persistence remains the only Egress contract, and desired compilation
chooses physical grouping.

## 4A.4 AccessRule cutover

Use the same Application/TargetSelector and Domain/IP interaction in AccessRule,
while keeping AccessRule's own subject, Internet/targets, block action, and
future time-control area. Do not create a GenericRuleForm or restore OAF as a
second target type.

# Slice 4 revision — Routing desired-state compaction + RouterOS readability (4B)

## 4B.1 Conservative execution groups

Compile only physical execution compression. The group key must include
EgressID, family, route table, route/failure behavior, enabled execution
semantics, match-direction boundary, and any field that can change final
mark-routing behavior. Different family/Egress/route-table/semantics never
share. Domain and IP target matchers remain separate even when they share a
connection mark.

Ingress-bound `all` and `excluded` rules may share a final mark-routing rule
only with the TrafficIngress guard intact. Selected/source-only rules remain
dedicated unless a safe group-level source guard is proven. Never generate a
connection-mark-only final mark-routing rule.

## 4B.2 RouterOS labels and stable names

- retain exact `rb_<hash>` ownership identity and make only the suffix readable;
- include policy/target/family and Domain/IP distinction in mangle comments;
- include readable target labels in DNS static/address-list entry comments;
- use stable preset-ID slugs only for preset routing names, e.g.
  `rb_rt_<short-hash>_youtube_d` and `_ip`;
- keep custom target physical names hash-only, and sanitize/truncate all labels;
- prove that display-name renames do not alter stable ownership or custom hash
  names.

## 4B.3 No optimizer expansion

Do not eliminate duplicate logical rules, merge canonical TargetLists, add a
generic matcher union/DSL, or introduce a projection registry. Preserve
RouterOutput behavior and keep IP-family execution groups separate.

# Required reporting after every implementation slice

Use this structure:

```text
# Unified Target Policy Model — Slice N Report

## Baseline
branch / HEAD / status

## Scope completed
what changed and what explicitly did not change

## Final contracts
models / API / schema relevant to this slice

## Migration behavior
if applicable

## RouterOS semantics
if applicable

## Tests added
what invariant each proves

## Validation
exact commands and results

## Diff review
files changed / diff stat / unrelated dirty files preserved

## Production boundary
10.0.0.6 accessed? NO
10.0.0.60 deployed? NO unless explicitly part of final acceptance
commit created? NO before acceptance

## Reviewer attention
3–5 highest-risk points
```

Then stop.

# Complexity guardrail

At the end of each slice, explicitly answer:

```text
Did this slice duplicate an existing parser/downloader/version/member/reconcile mechanism?
Did it introduce an abstraction with only one consumer?
Could the same behavior be implemented with fewer persistent tables/types?
Did any temporary compatibility layer accidentally become a second authority?
Is every new long-lived model visible/explainable in the UI?
```

Any “yes” to the first, second or fourth question is a redesign signal before proceeding.

# Slice 4C — Policy UX unification & apply-domain isolation

This is the current implementation scope inside the existing task. Do not
create a new top-level task, deploy, commit, push, archive, or access either
RouterOS machine in this slice. Preserve all dirty changes from Slices 1–4B.

## 4C.0 Trace and acceptance baseline

Before code changes, document and test the current Routing save, Access save,
TargetList mutation/materialization, desired build, plan/scan/diff, apply, and
state-commit call chains. Use the existing OAF snapshot and current worktree as
the rollback boundary; do not rewrite history.

## 4C.1 Backend domain split first

- Introduce explicit routing and access desired builders over shared readers
  and helpers, with strictly owned RouterOS object namespaces.
- Make plan generation domain-scoped rather than generating a combined graph
  and guessing operation ownership afterward.
- Replace the mixed boolean commit contract with clear routing/access commit
  functions. Each function advances only its own applied state.
- Remove `policy_changes_pending` only after the separation is enforced and
  add regressions proving either domain can apply while the other is dirty.

## 4C.2 TargetList consumer-domain invalidation and promotions

Add the smallest repository helper needed to calculate
`TargetConsumerDomains(targetID)`. Every TargetList save, pending-version
save, refresh, enable/disable, delete, and preset materialization path must
use it. Promotion metadata belongs to the plan and only versions reviewed by
that plan may be promoted. Unreferenced, routing-only, access-only, and shared
TargetLists must produce the corresponding revision changes and no unrelated
ones.

## 4C.3 Access preset proposal flow

Keep ApplicationPreset selection in the Access draft/proposal. Reuse an
existing usable preset backing list directly; otherwise prepare the backing
TargetList/version in the proposal overlay and commit it atomically with the
AccessRule and Access plan. There must be no pre-save materialization that
bumps routing state or creates a self-blocking pending condition.

## 4C.4 Per-rule ingress and migration

Add a narrow RoutingRule ingress scope (`interfaceLists[]`/`interfaces[]`),
validate all/excluded/selected semantics, copy the old global ingress into
existing all/excluded rules once, and make desired routing use the rule scope.
Adapt execution-group boundaries and tests without introducing a generic
matcher abstraction.

## 4C.5 Egress reuse and copy-on-write

Implement a deterministic pure execution signature and policy-owned origin
marker with a safe additive migration defaulting existing Egress rows to
`legacy`. New policy-created Egress rows use `policy`. Reuse equivalent
Egresses, preserve IDs for unchanged edits, copy shared Egresses before a
changed edit, and clean only zero-consumer policy-owned rows.

## 4C.6 Unified Routing UI

Remove the standalone Egress and Routing Rules product sections from the main
user flow. Present one 策略路由 list and one complete policy wizard. Keep the
canonical backend models and proposal preview gate; do not expose Egress IDs
or require a second navigation step to bind an Egress.

## 4C.7 Required regressions

At minimum add tests for: equivalent Egress reuse and copy-on-write; signature
differences and display-name stability; legacy orphan safety; per-rule ingress
validation/migration/grouping; routing/access desired and applied state
isolation; routing/access drift isolation; Access preset proposal atomicity;
consumer-domain TargetList revision bumps; plan-scoped pending-version
promotion; and both domain commit functions.

## 4C.8 Slice 4C validation and stop gate

Run targeted backend/frontend tests while iterating, then:

```text
go test ./...
go test -race ./...
go vet ./...
git diff --check
python3 ./.trellis/scripts/task.py validate 09-01-unified-target-policy-model
npm run lint
npm run build
```

Run the repository's focused frontend test command if configured. Do not
deploy to `10.0.0.60` until root reviewer approval, and never access or modify
production `10.0.0.6`.

After implementation, stop with the requested Slice 4C report and wait for
root review.

## 4C.9 Root-review correction checklist

The implementation is accepted only when the following concrete regressions
remain green:

- exact Routing-only and Access-only target mutations generate and apply only
  their own domain; unreferenced target mutations generate no RouterOS job;
  generic unknown kinds do not widen to `Combined`;
- a shared target produces two explicit domain plans in stable order, with
  target-scoped pending-version promotion. A failure in the second plan leaves
  the first commit applied and the second domain pending;
- `RefreshDue` batches changed target IDs and routes each ID to its exact
  consumer domains;
- Access rules sharing one domain target produce one physical `rb_ac_*` DNS/
  address-list projection and separate rule filters. Distinct overlapping
  domain target IDs block with `access_domain_projection_ambiguous`;
- enabled Routing/Access overlapping domain projections block both plans with
  `cross_domain_dns_projection_ambiguous`, while shared IP targets do not;
- Access target projection activity is `TargetList.Enabled` plus at least one
  enabled AccessRule consumer: disabled-only targets emit no active target
  projection, while mixed consumers keep one active projection and separate
  filter states;
- Routing DNS capability validation deduplicates by `(egressID,targetID)` and
  blocks same-Egress/different-target overlap as well as different-Egress
  overlap, regardless of equal DNS context;
- Egress name is absent from the editable user form and uniqueness checks;
  existing names survive edits, and new/copy-on-write Egresses get generated
  readable names without changing the execution signature;
- Access scans classify stale `rb_ac_*`, `rbac_*`, `rbac_internet_*`,
  forwarder, and legacy-comment objects for Access cleanup only; Routing scans
  never delete them.
