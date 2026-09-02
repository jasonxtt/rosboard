# Unified Target Library and Policy Rule Model — Technical Design

## 1. Design summary

The target architecture is intentionally small:

```text
                    ┌─────────────────────┐
                    │    Target Library   │
                    │ domain / ip content │
                    └──────────┬──────────┘
                               │ TargetList IDs
                 ┌─────────────┴─────────────┐
                 │                           │
        ┌────────▼────────┐         ┌────────▼────────┐
        │   RoutingRule   │         │    AccessRule   │
        │ Subject+Targets │         │ Subject+Targets │
        │    + Egress     │         │  / Internet     │
        └────────┬────────┘         └────────┬────────┘
                 │                           │
        mangle/routing-mark             filter deny
                 │                           │
                 └──────────┬────────────────┘
                            │
                   RouterOS desired graph
```

Long-lived user concepts:

- `TargetList`
- `RoutingRule`
- `AccessRule`
- `Egress`

Shared implementation concepts are kept narrow:

- Target content/version storage;
- pure Subject normalization/resolution helpers;
- Target selector / Subject selector frontend components;
- RouterOS desired/reconcile infrastructure.

There is no GenericPolicyRule, matcher AST, provider system or generic compiler.

## 1.1 Preview boundary for the RoutingRule wizard

The wizard's intermediate state is a small in-memory `PolicyProposal`, not a
partially saved canonical graph. The proposal may contain an Egress edit, the
device TrafficIngress selection, the RoutingRule edit, and the content needed
for newly selected preset TargetLists. Plan generation builds desired state
through a read-only repository overlay, so preview does not write rows,
pending versions, or `desired_revision`.

The generated plan records the pre-proposal desired revision, a proposal hash,
the resulting desired hash, and revisions of referenced canonical Egress and
TargetList dependencies. Applying the plan requires the exact plan hash and
rechecks all of those values. Only then does the repository commit the whole
proposal and one desired-revision bump in a single SQLite transaction; the
existing RouterOS apply job receives the same reviewed desired graph. Back or
Close simply discards the in-memory proposal. A scheduled refresh or another
normal GenerateAndApply sees only the canonical graph, and any concurrent
canonical revision makes the reviewed proposal stale rather than applying it.

## 2. Canonical TargetList model

Create a shared business model, preferably under a small package such as `internal/targetlist`, while reusing the existing policy source persistence and `internal/policy` content pipeline.

Conceptual model:

```go
type TargetList struct {
    ID                string
    Name              string
    Kind              string // domain | ip
    SourceType        string // url | upload | manual | preset
    PresetID          string // only sourceType=preset
    URL               string // URL/preset resolved rule URL
    Schedule          string
    Enabled           bool
    ActiveVersionID   string
    PendingVersionID  string
    LastGoodVersionID string
    ETag              string
    LastModified      string
    NextRunAt         time.Time
    Revision          int64
    PendingDeletion   bool // migration/reconcile lifecycle, not ownership
    Applied           bool // migration/reconcile lifecycle
    Versions          []Version
    Counts            map[string]int
}
```

`Enabled` is retained during this refactor because existing disabled Source semantics must not be lost. It is a global TargetList availability switch: disabled content is not projected for any consumer. Consumer-specific on/off remains on RoutingRule/AccessRule.

`PendingDeletion` and `Applied` are not new product concepts. They may stay internally while current two-phase desired/apply lifecycle is reused, then can be simplified only in a later focused cleanup.

### Invariants

- Canonical `TargetList` has **no EgressID**.
- `Kind` remains immutable after creation.
- `SourceType` remains immutable after creation except an explicit migration operation.
- Domain and IP content never coexist in one TargetList.
- `TargetList.ID` remains stable through migration.
- Version/rule identities remain stable through migration.
- A TargetList may have zero, one or many RoutingRule and AccessRule consumers.

## 3. Persistence evolution: reuse, do not rebuild

### 3.1 Keep existing content tables

Do not create parallel `target_versions` / `target_rules` tables.

Reuse:

```text
policy_v2_sources
policy_v2_source_versions
policy_v2_source_rules
```

Physical names may remain legacy names during this task. They are storage implementation detail; renaming them does not improve the product model and would enlarge rollback risk.

Add only storage required by new source semantics, for example:

```text
policy_v2_sources.preset_id TEXT NOT NULL DEFAULT ''
```

The existing `egress_id` column remains temporarily during Slice 1 and is explicitly legacy-only. Canonical TargetList store/API methods do not expose it.

### 3.2 Canonical repository methods

Introduce canonical TargetList operations over the same rows, e.g.:

```text
ListTargetLists
GetTargetList
SaveTargetList
DeleteTargetList
SavePendingTargetVersion
SaveTargetRefresh
ListTargetVersions
ListTargetRules
```

Implementation should reuse the existing SQL bodies where possible rather than maintain Source and Target implementations in parallel.

Legacy Source endpoints may use a thin adapter during Slice 1. The canonical code path owns validation/versioning.

### 3.3 Version semantics

Keep current active/pending/last-good semantics during this architecture migration.

Reason:

- current desired/apply already stages pending content and promotes it after successful reconciliation;
- changing content activation and rule architecture simultaneously would expand failure/rollback scope;
- library-only targets can expose pending content/rules in the UI just as current unassigned sources do.

A later task may simplify version promotion independently if the product requires “active content even with no RouterOS consumer.” It is not required for this refactor.

## 4. Temporary legacy Egress association

During TargetList foundation only:

```text
policy_v2_sources.egress_id
```

remains the legacy routing association so current routing behavior continues while the new Target Library API is introduced.

Rules:

- canonical TargetList responses omit `egressId`;
- legacy `/api/policy-routing/sources` may still expose it during Slice 1;
- no new business logic may use it outside the legacy routing adapter;
- no new generic association table is created merely to hide this column;
- Slice 2 migrates this association to RoutingRule and then stops treating the column as authoritative.

This seam must be visibly marked in code as temporary and removed from authority in the RoutingRule cutover.

## 5. RoutingRule model and persistence

### 5.1 Model

```go
type RoutingRule struct {
    ID            string
    Name          string
    Subject       Subject
    TargetListIDs []string
    EgressID      string
    Priority      int
    Enabled       bool
    Revision      int64
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

The action is implicit: route through `EgressID`.

### 5.2 Tables

Policy V2 uses one SQLite database per RouterOS device, so RoutingRule tables follow existing policy-v2 device scoping and do not add redundant `device_id` columns.

Recommended minimal tables:

```text
policy_v2_routing_rules
- id PK
- name
- egress_id
- subject_mode          // all | selected
- priority
- enabled
- revision
- created_at
- updated_at

policy_v2_routing_rule_targets
- rule_id
- target_id
- position
- PK(rule_id, target_id)

policy_v2_routing_rule_members
- rule_id
- terminal_id
- binding
- anchor_mac
- pinned_ipv4_json
- pinned_ipv6_json
- last_ipv4_json
- last_ipv6_json
- PK(rule_id, terminal_id)

policy_v2_routing_rule_prefixes
- rule_id
- prefix
- position
- PK(rule_id, prefix)
```

Do not create a polymorphic `policy_subjects(owner_type, owner_id, ...)` table. RoutingRule and AccessRule persistence stays separate and auditable.

### 5.3 Egress after split

Canonical Egress continues to own real exit mechanics:

- interface/gateway;
- route table;
- DNS upstream and Fake DNS transport;
- IPv4/IPv6 family settings;
- failure/route mode;
- NAT;
- router-output behavior;
- enabled state.

The following existing fields stop being user-facing routing-policy ownership once RoutingRule is authoritative:

- Egress priority → RoutingRule priority;
- list mode/list name → no longer define target grouping.

Physical legacy columns can remain until a later schema-cleanup task if removing them creates unnecessary migration risk.

## 6. Existing Egress/Source → RoutingRule migration

### 6.1 Migration unit

Create **one migrated RoutingRule per existing Egress that has at least one associated Source**.

This is the closest semantic representation of current behavior, where one Egress owns a group of destination Sources and all TrafficIngress clients match those destinations.

For each eligible Egress:

```text
ID             = deterministic migration ID derived from Egress ID
Name           = readable name derived from Egress name
Subject.Mode   = all
TargetListIDs  = every non-pending-deleted Source with legacy egress_id = Egress.ID
EgressID       = Egress.ID
Priority       = Egress.Priority
Enabled        = true
```

The Egress's own `Enabled` state remains authoritative for whether the physical exit can activate. Setting migrated `RoutingRule.Enabled=true` preserves the current expectation that re-enabling a disabled Egress restores its configured target selection.

Disabled TargetLists remain referenced but are not materialized until re-enabled, preserving existing disabled Source behavior.

Unassigned Sources become library-only TargetLists.

An Egress with no associated Source creates no RoutingRule.

### 6.2 Idempotency and authority marker

Add one small policy schema metadata marker, e.g.:

```text
policy_v2_schema_meta
key = routing_rules_authoritative
value = v1
```

Migration runs transactionally:

1. create RoutingRule tables if absent;
2. insert deterministic migrated rules and target relations;
3. preserve all TargetList/version rows in place;
4. set the authority marker;
5. clear legacy `policy_v2_sources.egress_id` values after equivalent RoutingRule relations exist;
6. bump desired revision so the new graph is reconciled;
7. commit.

On failure, the transaction rolls back and the old association remains authoritative.

After the marker exists, startup never recreates rules from `egress_id`; RoutingRule is canonical.

Rollback to an older binary after this migration requires restoring the pre-migration SQLite database together with the old binary. Do not attempt a lossy in-place downgrade.

### 6.3 Pending deletion / in-flight state

Current delete flows already clear a Source's Egress association before pending deletion, so pending-deleted Sources are not migrated into RoutingRule targets.

Pending-deleted Egresses do not produce migrated rules.

A prior process apply job may be persisted in non-terminal state after a restart, but schema migration must not infer RouterOS completion from that job. The normal desired/rescan/apply path remains responsible for runtime convergence after migration.

## 7. Subject model: shared semantics, separate rule ownership

Create one narrow pure helper layer once RoutingRule becomes the second consumer. It may live in a package such as `internal/subject` and contains only normalization/resolution types and functions, not policy actions.

Conceptual shared payload:

```go
type Subject struct {
    Mode     string // all | selected | excluded (routing only)
    Members  []Member
    Prefixes []string
}

type Member struct {
    TerminalID string
    Binding    string // auto | fixed
    AnchorMAC  string // internal
    PinnedIPv4 []string
    PinnedIPv6 []string
    LastIPv4   []string // internal
    LastIPv6   []string // internal
}
```

AccessControl can keep its persisted `RuleMember{RuleID,...}` row shape and map to/from the shared member semantics. RoutingRule keeps separate `rule_id` persistence. This avoids a polymorphic rule-owner framework while still centralizing the non-trivial address normalization and auto-follow behavior.

### Manual prefixes

- exact IPv4 normalizes to `/32` internally;
- exact IPv6 normalizes to `/128` internally;
- CIDR is masked/canonicalized with `netip`;
- link-local IPv6 remains excluded from auto-follow forwarded matching as in current Access Control;
- malformed or mixed-family inputs fail validation, not best-effort coercion.

### `all`

For RoutingRule, `all` means all forwarded traffic entering the configured Policy TrafficIngress. It does not create a giant source address-list.

For AccessRule, `all` is a required first-version subject. It is projected from the device's already trusted local `TerminalScope` prefixes (`Scope.PrefixesForFamily`) into the rule subject list, so target/internet filters still match only client-side local addresses. If a requested address family has no trustworthy local scope, that family is blocked/degraded explicitly instead of widening to every forwarded source. It must not be implemented as fake terminal rows or an unconstrained forward-chain rule.

## 8. Routing desired-state projection

### 8.1 Subject projection

For RoutingRule, `all` is bounded by the selected TrafficIngress and emits
`in-interface-list=<policy ingress>` without a source list. `selected` is
source-only: it emits a per-rule subject list and does not add the ingress
matcher to the first classification rule merely because an ingress exists.
`excluded` is ingress-bound and emits the ingress list plus a negated
per-rule exclusion list. It is invalid without a valid TrafficIngress.

For selected or excluded RoutingRule subjects, create one stable per-rule
subject address-list per family from resolved members and manual prefixes.

Routing mangle match becomes conceptually:

```text
all:       in-interface-list = policy-ingress
           AND dst-address-list = target-projection
selected:  src-address-list = rule-subject
           AND dst-address-list = target-projection
excluded:  in-interface-list = policy-ingress
           AND src-address-list = !rule-subject
           AND dst-address-list = target-projection
→ mark connection / routing
```

The final `mark-routing` execution rule always retains a safe direction/source
guard. In particular, a selected/source-only rule may stay dedicated when a
shared group-level guard cannot be proven; a connection-mark-only
`mark-routing` rule is never generated.

### 8.2 Destination projection identities

Logical TargetList content is shared; physical destination lists are keyed by execution context.

#### IP Target + RoutingRule

Use a deterministic routing projection keyed by:

```text
(device, egress, target)
```

The list contains the target's IP-CIDR/IP-CIDR6 rules appropriate to the enabled family.

Multiple RoutingRules using the same TargetList and same Egress share this destination projection.

#### Domain Target + AccessRule

Use an access-context projection keyed by:

```text
(device, access, target)
```

It uses the Access Control DNS forwarder/context and feeds the existing filter desired builder.

#### IP Target + AccessRule

Use the same access-context target identity with static IPv4/IPv6 address-list rows.

### 8.3 Domain Target + RoutingRule — RouterOS DNS constraint

RouterOS `/ip/dns/static` is a device-global ordered rule list. A static FWD entry has `forward-to`, `address-list` and `match-subdomain`, but no client/source matcher. Therefore rosboard cannot safely promise two independent DNS resolutions for the same domain pattern based only on RoutingRule Subject.

This is separate from logical policy conflict detection.

V1 rule:

- normalize enabled RoutingRule references by the physical `(device, egress, target)` key before validating projection capability;
- same domain target + same Egress can share one physical projection normally, including when several rules reference it;
- different target IDs on the same Egress with exact/suffix-overlapping domain content must return `domain_projection_context_ambiguous`, even when DNS contexts happen to match;
- different Egresses with overlapping domain projections must return the same capability blocker; this remains separate from logical RoutingRule conflict detection;
- IP targets are not affected by this DNS-specific limitation.

Do not solve this by adding duplicate same-name DNS Static FWD rows and relying on order. Do not add a DNS proxy subsystem in this task.

A future focused design may support a shared/common domain-resolution mode if the user explicitly wants the trade-off; it is not hidden inside this refactor.

### 8.4 RouterOutput

`Egress.RouterOutput` remains an Egress capability, not a fake Subject.

For router-originated output, use the union of enabled RoutingRule target projections pointing to that Egress. End-device Subject constraints do not apply to router-originated traffic.

This preserves existing router-output semantics without inventing a “router terminal.”

## 9. AccessRule migration and canonical model

Canonical AccessRule becomes:

```go
type AccessRule struct {
    ID            string
    Name          string
    Subject       Subject
    TargetScope   string // internet | targets
    TargetListIDs []string
    Enabled       bool
    Revision      int64
    ...
}
```

Action remains implicit deny/block.

### 9.1 Existing sources rules

Migrate:

```text
targetScope=sources
sourceIds=[A,B]
```

into:

```text
targetScope=targets
targetListIds=[A,B]
```

IDs do not change. `access_rule_sources` can remain the physical relation table during this task and be interpreted as TargetList IDs. A table rename is cleanup, not correctness.

### 9.2 Internet rules

Remain first-class and unchanged.

### 9.3 OAF application rules

Before removing OAF support:

1. inspect every `TargetScopeApplications` rule;
2. resolve each referenced OAF application from the frozen/current catalog data available to that database/runtime;
3. create a deterministic migration domain TargetList from the supported domain signatures;
4. replace the application relation with TargetList relation(s);
5. if an application ID cannot be resolved, preserve the legacy rule as disabled/degraded with an explicit migration issue; never widen it or silently delete it;
6. remove the OAF relation/runtime only after no canonical rule depends on it.

Because OAF was never deployed to production, this path mainly protects development/test state and makes migration behavior principled without creating long-term OAF compatibility.

## 10. Target deletion and usage

A TargetList cannot be permanently deleted while referenced by any canonical RoutingRule or AccessRule.

Expose computed usage counts in Target Library UI/API:

```text
routingRuleCount
accessRuleCount
```

Deletion error is one canonical `target_in_use` result with enough usage metadata for UI to link to the referencing rules.

Do not create per-consumer duplicate target rows to avoid this constraint.

## 11. RoutingRule conflict engine

Conflict evaluation is deliberately narrow.

A blocker requires:

```text
ruleA.Enabled
AND ruleB.Enabled
AND ruleA.EgressID != ruleB.EgressID
AND SubjectOverlap(A,B)
AND TargetOverlap(A,B)
```

### 11.1 Subject overlap

Implement only:

- `all` overlaps anything;
- same TerminalID overlaps;
- manual/exact prefixes overlap using `netip.Prefix.Overlaps` / containment;
- terminal vs manual prefix uses current or last trusted resolved addresses when available;
- unresolved terminal evidence produces a specific indeterminate warning when overlap cannot be proven, rather than guessing.

### 11.2 Target overlap

Fast path:

- same TargetList ID → overlap.

For different TargetLists of same kind:

- domain: exact==exact, exact inside suffix, suffix equal/nested;
- IP: CIDR overlap via `netip`;
- domain vs IP: no declarative overlap blocker.

Run subject overlap first so expensive target-content comparison is avoided for disjoint clients.

Priority determines deterministic RouterOS ordering; it does not legalize contradictory overlapping different-Egress rules.

Do not create a general set algebra engine.

## 12. ApplicationPreset design

### 12.1 Registry

Use a source-controlled manifest/generated catalog:

```go
type ApplicationPreset struct {
    ID       string
    Name     string
    Category string
    RuleURL  string
}
```

The checked-in manifest is generated during development from the valid YAML
paths under the selected bm7 Clash tree and covers the tree rather than a
short hand-picked list. Runtime only reads the generated metadata; it never
calls the GitHub API or downloads the whole catalog at startup. Categories and
aliases are optional metadata, with `其他` as fallback.

No provider interface, repository crawler or dynamic catalog discovery.

Use one explicit raw base URL for v1. Each manifest entry stores a stable
preset ID, display name, category, relative Clash YAML path, and optional
aliases/search keywords. A small helper constructs the raw URL; full URL
strings are not duplicated. The user-selected
`iZuoShou/bm7_ios_rule_script` mirror is the runtime source; upstream
Blackmatrix7 is provenance/reference only.

Do not vendor the whole upstream rule catalog.

### 12.2 Preset preview and creation

Flow:

```text
preset metadata
→ existing URL fetcher
→ existing Clash parser
→ supported-rule filtering
→ split by domain/IP kind
→ preview counts/samples
→ create/reuse ordinary TargetList(s)
```

Use deterministic uniqueness per device:

```text
(presetID, kind)
```

so selecting YouTube repeatedly does not create duplicate preset-backed library lists.

Preset-backed TargetLists persist `preset_id` and the resolved URL for
refresh/audit. The URL is read-only in UI. Preview/materialization accepts
`requestedKinds`, whose legal values are `domain`, `ip`, or both. It creates
or reuses only the requested kinds; existing unrequested backing lists are
never implicitly selected.

Unsupported rule types remain in ignored counts only.

## 13. Traffic attribution after OAF

Keep the useful evidence half of `ApplicationResolver`:

```text
MosDNS observation
(clientIP, answerIP, domain, queryTime, TTL)
→ recent valid domain evidence
```

Replace OAF `Catalog.LookupDomain` with a lightweight domain registry built
from supported domain rules of the generated ApplicationPreset manifest.

Properties:

- `ApplicationID` is the preset ID (e.g. `youtube`), not `oaf:<number>`;
- `Application` is preset display name;
- `Service` / matched domain evidence remains available;
- exact/suffix matching is deterministic;
- ambiguous ownership returns no application attribution;
- attribution registry is read-only metadata/cache and is not an enforcement model.

## 14. API design and staged compatibility

### 14.1 Canonical Target Library API

Use a top-level shared resource, while preserving the project's existing `?device=` addressing style to minimize routing churn:

```text
GET    /api/target-lists?device={id}
POST   /api/target-lists?device={id}
GET    /api/target-lists/{id}?device={id}
PUT    /api/target-lists/{id}?device={id}
DELETE /api/target-lists/{id}?device={id}&revision=...
GET    /api/target-lists/{id}/rules?device={id}
POST   /api/target-lists/{id}/refresh?device={id}
POST   /api/target-lists/url/preview?device={id}
POST   /api/target-lists/upload/preview?device={id}
POST   /api/target-lists/manual/preview?device={id}
```

Response model never contains canonical `egressId`.

### 14.2 Application presets

```text
GET  /api/application-presets
POST /api/application-presets/{id}/preview?device={id}
```

Preview returns domain/IP counts separately plus ignored rule counts.

`POST /api/application-presets/{id}/target-lists` accepts
`requestedKinds`. A preset with both supported kinds defaults to
`[domain]`; a domainless preset defaults to `[ip]`. A missing requested kind
is a validation error and does not create an empty TargetList.

### 14.3 RoutingRule API

Extend the existing policy-routing resource rather than introduce a second routing service:

```text
GET    /api/policy-routing/rules?device={id}
POST   /api/policy-routing/rules?device={id}
GET    /api/policy-routing/rules/{id}?device={id}
PUT    /api/policy-routing/rules/{id}?device={id}
DELETE /api/policy-routing/rules/{id}?device={id}&revision=...
```

Existing plan/apply remains the only RouterOS mutation pipeline.

### 14.4 AccessRule API

Keep existing Access Control route family. Canonical payload changes to `subject`, `targetScope=internet|targets`, `targetListIds`.

Temporary old field decoding is allowed only during the migration slice. New frontend always writes canonical fields.

### 14.5 No dual writable authority

The backend model becomes authoritative before the final frontend cutover, but two writable representations of the same relationship are never kept in parallel:

- Slice 1 may keep legacy Source routing authority while adding canonical Target Library.
- Slice 2 makes RoutingRule authoritative; legacy Source→Egress mutation stops being writable authority at that point.
- Slice 3 makes canonical AccessRule targets/subjects authoritative; legacy ApplicationIDs/source-shaped writes stop being canonical authority at that point.
- Slice 4 switches the production frontend to the already-approved canonical APIs and removes obsolete UI/API surfaces.

Because no runtime deployment occurs between backend slices, the checked-out legacy frontend may be temporarily unable to edit a relationship whose old write contract has been retired. Prefer that short development-only incompatibility over a complex write-through compatibility layer or dual authority.

## 15. Frontend component boundaries

After backend contracts are stable:

- extract a reusable `TargetSelector` used by RoutingRule and AccessRule forms;
- extract a reusable `SubjectSelector` used by RoutingRule and AccessRule forms;
- reuse existing policy source preview/rules UI inside Target Library rather than copy it;
- keep `RoutingRuleWizard` and `AccessRuleWizard` separate;
- RoutingRule reuses the old four-stage Egress/discovery → ingress/source →
  target → preview/apply interaction; Egress persistence remains canonical,
  and the user-facing list-mode choice is removed.
- Target Library hides preset-backed rows from ordinary user-managed lists,
  while rule reconstruction can still resolve their IDs to application chips.

Do not create one configurable GenericRuleForm.

## 16. Migration / rollout sequence

### Stage A — Target foundation

- canonical TargetList model/API over existing storage;
- legacy `egress_id` remains routing compatibility only;
- no RouterOS behavior change;
- Target Library UI can be introduced without moving routing authority yet.

### Stage B — RoutingRule authority

- create RoutingRule tables and narrow shared Subject helpers;
- transactionally migrate existing Egress/Source associations;
- desired builder uses RoutingRule instead of `Source.EgressID`;
- legacy egress/source write relationship stops being authoritative;
- do not deploy this backend-only intermediate state.

### Stage C — AccessRule, presets and attribution backend

- AccessRule uses TargetList IDs and shared subject semantics;
- existing RuleMember behavior is preserved through shared helpers;
- add ApplicationPreset registry/preview and preset-backed TargetLists;
- migrate resolvable OAF application rules;
- traffic attribution uses preset-domain registry;
- do not deploy this backend-only intermediate state.

### Stage D — Slice 4A frontend/product-flow correction + catalog

- production frontend switches to user-managed Target Library, shared
  Application/TargetSelector, restored RoutingRule wizard, and canonical
  AccessRule flows;
- generated bm7 catalog, lazy preset preview/materialization, and
  Domain-first requested-kind selection are complete;
- remove obsolete OAF enforcement/catalog UI only after the canonical target
  migration no longer depends on it;
- remove legacy Source-owned API/UI fields that no longer have a consumer;
- keep physical legacy DB columns/table names if removing them provides no
  correctness benefit.

### Stage E — Slice 4B routing desired-state compaction + readability

- compile logical RoutingRules into conservative execution groups. The group
  key is `(EgressID, address family, route table, route/failure semantics,
  enabled execution semantics, match-direction boundary, and any other field
  that changes mark-routing behavior)`;
- share connection marks/final mark-routing only for proven-equivalent
  ingress-bound or excluded rules; selected/source-only rules remain dedicated
  unless a safe group-level source guard exists;
- keep Domain and IP target projections as separate matchers, including when
  they share one execution group;
- retain `rb_<hash>` ownership identity and add sanitized readable labels to
  comments. Preset routing lists use a stable preset-ID slug plus short hash
  and kind suffix; custom list names remain hash-only;
- do not eliminate duplicate logical rules.

## 17. Rollback boundaries

- Before any runnable implementation, preserve the approved OAF working-tree snapshot outside the repo as already required.
- Before any SQLite authority migration, tests must prove migration transaction rollback.
- Once `routing_rules_authoritative=v1` has been written in a real database, rollback requires the paired pre-migration database plus old binary; old binary + migrated DB is not supported.
- RouterOS runtime changes continue through existing full desired scan/plan/apply/verification. No new direct mutation path is introduced.
- Production deployment remains gated by local tests → root implementation review → `10.0.0.60` runtime acceptance → user approval → production authorization/backup rules.

## 18. Test matrix

### Target foundation

- existing domain/IP Source row reads as canonical TargetList with same ID/content/version state;
- URL/manual/upload preview and version creation unchanged;
- ETag/Last-Modified refresh unchanged;
- canonical API omits Egress ownership;
- deletion blocks when either RoutingRule or AccessRule references target;
- legacy Source compatibility is exact while Slice 1 is active.

### Routing migration

- one existing Egress with N Sources → one deterministic all-subject RoutingRule with N targets;
- multiple Egresses migrate independently;
- unassigned TargetList remains library-only;
- disabled Egress and disabled TargetList preserve behavior;
- pending-deleted objects are not resurrected;
- migration replay creates no duplicate rules/relations;
- migration transaction failure leaves old association/marker unchanged;
- desired objects before/after migration are semantically equivalent for migrated all-subject policy;
- old `egress_id` no longer drives desired state after authority marker.

### Subject / conflict

- all vs selected overlap;
- same/different terminal IDs;
- IPv4/IPv6 exact and CIDR overlap;
- terminal resolved/last-known addresses vs manual prefix;
- unresolved identity warning;
- same target + disjoint subject + different Egress is not logical conflict;
- overlapping subject + overlapping target + different Egress is blocker;
- same Egress overlap is allowed;
- domain projection context ambiguity is reported separately from logical conflict.

### Access migration

- sources → targets preserves IDs/order;
- internet remains unchanged;
- domain/IP AccessRule desired behavior remains equivalent;
- auto/fixed member behavior and last-known projection remain equivalent;
- OAF application migration converts resolvable records and preserves unresolved records as explicit degraded state.

### Application presets

- generated registry deterministic and meaningfully covers the bm7 Clash tree;
- domain/IP split uses existing parser;
- unsupported matcher counts visible and ignored;
- repeat preset selection reuses `(presetID,kind)` target;
- URL refresh retains last-good behavior;
- no ApplicationIDs enforcement relation is created.

### Attribution

- recent MosDNS evidence maps supported domain to preset application;
- stale/expired evidence does not attribute;
- ambiguous domain does not guess;
- device scopes remain isolated;
- protocol-analysis-off behavior remains compatible.

### Full validation

- targeted package tests per slice;
- `go test ./...`;
- focused race first, then full `go test -race ./...` before final acceptance if practical within current project baseline;
- `go vet ./...`;
- frontend typecheck/lint/build/audit when frontend is touched;
- `git diff --check`;
- no production RouterOS access during planning or ordinary implementation review.

## 19. Explicitly rejected designs

Rejected:

- new TargetList + TargetVersion + TargetRule database copied from Source tables;
- GenericPolicyRule with action route/deny;
- generic Subject/Matcher AST;
- provider/plugin framework for application rule repositories;
- runtime crawling the whole BM7 repository or downloading its catalog;
- `kind=mixed` TargetList;
- one physical RouterOS list forced across routing/access/different Egress DNS contexts;
- persistent generic projection/refcount registry before a real need exists;
- duplicate same-domain RouterOS FWD rows used as fake source-aware DNS routing;
- silently dropping OAF application rules during cleanup.

## 20. Slice 4 revision record

This section is the authoritative correction for the implementation already in
the worktree. It supersedes the earlier three-step RoutingRule sketch and the
earlier small hardcoded-registry wording above.

### 20.1 Slice 4A — Frontend/product-flow correction + ApplicationPreset catalog

The Target Library is “my target lists”: only `manual`, `url`, and `upload`
rows appear in its primary domain/IP tables. `sourceType=preset` rows retain
stable IDs, versions, refresh, usage references, and deletion protection, but
are hidden backing materializations. Application catalog browsing and
selection live in the shared rule TargetSelector. Selecting an application
returns ordinary TargetList IDs and uses the user-facing confirmation “应用
规则已准备好”.

The preset catalog is a checked-in/generated manifest of valid bm7 Clash YAML
paths. Runtime does not enumerate GitHub or fetch all YAMLs. Each entry stores
stable ID, display name, category, relative path, and optional aliases; a fixed
raw base constructs the URL. Preview/materialization fetches only the selected
entry and uses the existing parser. `requestedKinds` is the sole selector for
materialization: `[domain]`, `[ip]`, or `[domain,ip]`; default is Domain when
available, otherwise IP. Existing backing lists are reused, and an
unrequested kind is never added because it already exists.

The RoutingRule UI is the old four-step wizard adapted to canonical models:

1. Egress configuration and interface/point-to-point/next-hop discovery;
2. TrafficIngress plus `all`, `selected`, or `excluded` source scope;
3. shared TargetSelector with application Domain/IP tail control;
4. ChangePlan-style readable preview, then explicit apply.

AccessRule uses the same application selector and Domain-first behavior but
keeps its own subject and Internet/targets semantics.

### 20.2 Slice 4B — Routing desired-state compaction + RouterOS readability

Execution groups are physical-only. A group key includes Egress, family, route
table, route/failure behavior, enabled execution semantics, match-direction
boundary, and every other field that changes the final mark-routing behavior.
Different families, Egresses, route tables, or unproven execution semantics
cannot share. Domain and IP remain separate target matchers even when their
connection mark is shared.

Ingress-bound `all` and `excluded` rules may share a final execution rule only
when it retains `in-interface-list=<policy ingress>`. Selected/source-only
rules may use a dedicated mark pair; sharing is allowed only with a proven
source/direction guard. A final rule with only `connection-mark` and no
direction/source guard is invalid. No duplicate logical-rule elimination is
performed.

Every RouterOS comment keeps its stable `rb_<hash>` prefix. The suffix is a
sanitized/truncated readable label containing policy/target/family as useful.
Preset routing address-list names use `rb_rt_<short-hash>_<stable-preset-slug>_d`
or `_ip`; custom TargetLists remain `rb_rt_<hash>`. The stable preset ID, not
the mutable display name, determines the slug, so display renames do not
rename physical identity. Subject lists keep their existing stable namespace.

### 20.3 Slice 4C — Policy UX unification & apply-domain isolation

The final Routing product surface is a single policy list and a complete
policy wizard. The UI/application aggregate is represented by the existing
`RoutingRule + Egress + TargetList` records; no second persistent
`RoutingPolicy` entity is added. Egress remains the reusable execution
configuration and is hidden as a top-level user workflow. The wizard drafts
source/ingress, targets, and egress mechanics together, then commits the
reviewed bundle through the existing proposal preview gate.

#### Egress lifecycle

Define a pure `EgressExecutionSignature` from fields that change execution:
enabled address families, interface/point-to-point/next-hop/gateway, route
mode and explicit route-table semantics, DNS upstream/Fake DNS transport,
failure mode, NAT, RouterOutput, and any other effective execution field.
Exclude display name, revision, applied/pending-deletion flags, legacy list
mode/name/priority, and automatically allocated aliases. A user-explicit
alias is included when it affects execution.

New policy saves first reuse an equivalent Egress by signature, otherwise
create a policy-owned Egress. An unchanged edit retains its Egress ID. A
changed edit reuses an existing equivalent Egress when available; otherwise
it updates the current Egress only when its consumer count is one and its
origin is safe for policy mutation. A shared current Egress is copied before
the RoutingRule is rebound. Only `origin=policy` and zero-consumer Egresses
are eligible for cleanup; legacy rows remain protected even at refcount zero.

#### Per-rule ingress

RoutingRule owns a narrow ingress scope with `interfaceLists[]` and
`interfaces[]`; no generic matcher DSL is introduced. `all` and `excluded`
require a valid ingress and compile to an ingress guard, with excluded also
using a negated subject list. `selected` may omit ingress and compiles to
source-only matching. The old device-global TrafficIngress is a migration
input/default only: existing all/excluded rules receive a deterministic copy;
selected/source-only rules remain source-only. Desired routing compilation
uses `RoutingRule.Ingress` thereafter.

Execution groups include normalized ingress scope in their boundary. All and
excluded rules may share only when ingress, Egress execution, family, route
table, failure/route behavior, and mark-routing semantics match. Different
ingress scopes cannot share a final guarded rule. Selected rules remain
dedicated unless a safe source guard is proven.

#### Explicit apply domains

The repository exposes separate domain operations equivalent to:

```go
BuildRoutingDesired(...)
BuildAccessDesired(...)
CommitRoutingApply(...)
CommitAccessApply(...)
```

Each plan carries a domain, scans and diffs only objects owned by that domain,
and records only the TargetList pending versions it actually reviewed and
used. Routing owns `rb_rt_*`, routing projections, mangle, routes, routing
tables, NAT, execution groups, and RouterOutput routing objects. Access owns
`rb_ac_*`, access projections, filter/jump/anchor rules, and internet-blocking
interface lists. A shared RouterOS menu does not merge ownership; only a
specific proven global capability conflict may block both domains.

Routing success advances only `policy_v2_device_state`; Access success
advances only `access_control_state`. There is no boolean mixed commit
contract. An Access apply is valid while routing desired/applied revisions
differ, and a Routing apply is valid while Access has pending changes.

TargetList revision invalidation derives consumer domains from RoutingRule and
AccessRule references. Library-only writes do not bump either state; routing-
only/access-only/shared references bump routing/access/both respectively.
Preset materialization for Access is held in the Access proposal overlay and
promoted atomically with the AccessRule and its reviewed pending versions.

#### Root-review invariants

The current plan resolver must use the target ID's exact consumer set for
normal target operations. It must not discover a domain by scanning every
source and must never return `PolicyDomainCombined` for normal runtime work;
only an explicit legacy compatibility request may ask for `Combined`.
Unknown operation kinds use the Routing-compatible path rather than widening
the plan. An exact shared target request is rejected unless the caller uses
the explicit two-domain operation.

The shared-target operation builds and applies Routing then Access as two
independent plans in deterministic order. They do not share actual snapshots,
diffs, commits, or promotions. A first-domain success is durable even when a
second-domain apply fails, and the failed domain retains its pending state.
RefreshDue first batches changed target IDs and per-domain flags, then scopes
each generated plan to those IDs.

Access target DNS/list projections are keyed by `(device, TargetListID)`, not
by rule. The one physical `rb_ac_*` projection is referenced by every matching
Access filter, while filters remain rule-specific. Distinct Access target IDs
whose domain contents exact/suffix-overlap receive the
`access_domain_projection_ambiguous` capability blocker. Enabled overlapping
Routing and Access domain projections receive
`cross_domain_dns_projection_ambiguous` in both plans. These blockers are
domain-scoped and never justify a `Combined` plan; IP-only overlap is allowed.

Access target activity is the conjunction of an enabled TargetList and at
least one enabled AccessRule consumer. A disabled-only consumer set emits no
active target projection; a mixed set keeps the one projection active and
leaves each rule filter independently enabled or disabled.

The Routing projection blocker compares distinct `(egressID, targetID)`
projections, not rule pairs and not only DNS context keys. Same Egress plus
same Target is deduplicated and allowed; same Egress plus different
non-overlapping targets is allowed; any distinct overlapping domain
projections block, while IP-only projections remain allowed.

RouterOS ownership classification must recognize current and legacy Access
namespaces (`rb_ac_*`, `rbac_*`, `rbac_internet_*`, Access forwarders, and
legacy Access comments). Only an Access scan may classify those objects as
stale Access ownership.
