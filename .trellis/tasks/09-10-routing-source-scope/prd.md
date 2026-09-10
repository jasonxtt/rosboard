# Routing source scope and TrafficIngress decoupling — PRD v0.1

Status: planning only; revised after root-review design Gate. This document is based on fixed baseline
`5a3a2197c1940a0bc72c2778614a8e7da0b9e356` on branch
`codex/routing-source-scope`.

## 1. Problem statement

Policy routing currently treats RouterOS topology inference as a prerequisite
for configuring a routing rule. The scanner filters `/interface` and
`/interface/list` into `TrafficIngressCandidate` values. The API and wizard
then accept only those candidates, and the planner validates the selected
scope again. A false negative in that inference can therefore make an
otherwise valid source IP, device, interface, or interface-list impossible to
configure.

The product semantic is simpler:

`SourceScope -> TargetListIDs -> EgressID`

An interface is one possible source expression, not the definition of every
source. Topology discovery must explain and recommend; it must not decide
whether a valid RouterOS matcher may be used.

## 2. Current behavior found in the repository

The authoritative routing model is `internal/policyv2.RoutingRule`:

- `Subject` is the shared `internal/subject.Subject` alias with modes
  `all`, `selected`, and `excluded`.
- `Ingress` is a per-rule `TrafficIngressScope` containing interface-list and
  interface names.
- `TargetListIDs`, `EgressID`, `Priority`, `Enabled`, and `Revision` are
  unchanged routing concerns.
- `NormalizeRoutingRule` requires `Ingress` for `all` and `excluded`, and
  deliberately discards `Ingress` for `selected`.

The same `Subject` type is used by Access Control, but Access rules do not use
`TrafficIngress`. Routing-specific source data must not be added to the shared
subject type.

Persistence currently stores:

- a device-global `policy_v2_device_state.lan_scope_json` payload, exposed as
  `trafficIngress`;
- per-rule `ingress_interface_lists_json` and `ingress_interfaces_json`;
- per-rule subject members and prefixes in child tables;
- the old source-to-egress relation, migrated into authoritative routing rules.

The one-time migration copies the former global scope into `all`/`excluded`
rules. `selected` rules are intentionally source-only. Once the authority
marker is `v1`, a rule's per-rule ingress is canonical and the global value is
only a compatibility fallback for old payloads.

The authoritative planner in `internal/policyv2/routing_desired.go` currently
materializes:

- selected subjects as per-family managed `src-address-list` matchers;
- all subjects as a managed aggregate interface-list plus
  `in-interface-list`;
- excluded subjects as that interface-list plus a negated subject address-list;
- RouterOS `prerouting` connection marking followed by routing marking;
- optional `output` marking for the separate egress `RouterOutput` behavior.

There is no direct `in-interface` materialization for a rule source today.
Global/legacy desired building also requires a non-empty global ingress when an
enabled egress exists. `ValidateTrafficIngress` reads live interfaces and
interface-lists, but it additionally rejects disabled, dynamic, and egress
WAN interfaces. Those role checks are currently coupled to business admission.

`RoutingRuleConflicts` compares subject overlap and IP target overlap. It does
not compare ingress or source-kind overlap. Domain projections keep their
existing device-global Priority arbitration.

The scanner already reads the raw RouterOS interfaces, interface-lists,
members, bridge ports, addresses, routes, DHCP clients, and PPPoE evidence. It
also produces the formal `IngressDecision` trace consumed by Doctor. Its
`TrafficIngressCandidate` result is an inference layer, not a complete fact
list.

## 3. Product decision

The first implementation must make these source expressions independently
usable:

1. Device — an identified rosboard terminal, resolved through the existing
   `Subject` / identity / AnchorMAC / pinned/last-IP mechanisms.
2. IP — one or more IPv4 or IPv6 host/prefix values supplied by the user.
3. Interface — a real RouterOS interface, materialized as `in-interface`.
4. InterfaceList — a real RouterOS interface-list, materialized as
   `in-interface-list`.
5. All — no source matcher.

The explicit `All` expression is reserved in the model but is not part of the
first apply-capable slice. A source-free `prerouting` rule would match every
forwarded non-local target flow, while the current output-chain behavior is
egress-wide and can include router-originated traffic when `RouterOutput` is
enabled. Until a reviewed safety contract covers forwarded traffic,
router-originated traffic, management traffic, policy-owned marks, and loop
prevention, `All` must produce an explicit blocker rather than silently
falling back to a candidate or to the global TrafficIngress.

The built-in RouterOS interface-list `all` is treated as equivalent to the
deferred `All` source for this safety boundary. It must remain visible in the
factual selector list, but the first release must block plan/apply with a
stable safety reason rather than using it as a bypass around the deferred
`All` gate.

Existing legacy rules remain supported without automatic semantic conversion.
The first release carries two compatibility forms:

- a new typed `sourceScope` discriminator for new simple source expressions;
- current `Subject` + per-rule `Ingress` for legacy and compound-compatible
  rules.

This is a compatibility seam, not a generic boolean expression language.

For any rule that already has `sourceScope`, the canonical source authority
must never silently split between a modern client and an old client. An old
client that omits `sourceScope` may update non-source fields only when the
legacy `Subject`/`Ingress` projection it submits is semantically identical to
the canonical source. If it attempts to change source-related legacy fields,
the server must reject the write with a stable 409/422-style source-mismatch /
unsupported-client error. It must not silently preserve the old canonical
source while reporting success, and it must not guess a reverse migration.

## 4. Proposed source model

Add a routing-only discriminated source selector, tentatively named
`RoutingSourceScope` and exposed as `RoutingRule.sourceScope`.

The selector must not duplicate the full shared `Subject` or its identity
children inside `source_scope_json`. Existing Subject storage already owns
terminal identity, AnchorMAC, pinned/last IPs, members, and prefixes. The new
selector only declares the routing source kind and the fields unique to that
kind.

Conceptually:

```go
type RoutingSourceKind string

const (
    RoutingSourceDevice        RoutingSourceKind = "device"
    RoutingSourceIP            RoutingSourceKind = "ip"
    RoutingSourceInterface     RoutingSourceKind = "interface"
    RoutingSourceInterfaceList RoutingSourceKind = "interface-list"
    RoutingSourceAll           RoutingSourceKind = "all"
)

type RoutingSourceScope struct {
    Kind RoutingSourceKind `json:"kind"`
    Name string            `json:"name,omitempty"` // interface/list only
}
```

The exact Go names remain an implementation detail, but the authority rules
are fixed:

| Kind | Canonical payload | RouterOS source matcher |
| --- | --- | --- |
| `device` | existing `RoutingRule.Subject` identity/member child tables | per-family owned `src-address-list`; no ingress matcher |
| `ip` | existing `RoutingRule.Subject` prefix child tables, normalized as explicit user IP/CIDR source | per-family owned source address-list; no ingress matcher |
| `interface` | `sourceScope.name` | `in-interface=<name>` |
| `interface-list` | `sourceScope.name` | `in-interface-list=<name>` |
| `all` | none | deferred until the safety gate is approved |

For `device`, the Subject payload must represent the existing selected-device
identity semantics. For `ip`, the Subject payload is reused only as the
canonical persistence for explicit prefixes; it must not trigger device
identity resolution. The `sourceScope.kind` discriminator tells the compiler
which interpretation is authoritative.

Do not serialize another `Subject`, AnchorMAC set, pinned IP set, or prefix
collection into `source_scope_json`. There must be one canonical copy of
device/IP source payload.

The existing `Subject` and `Ingress` fields remain in the wire/storage model
during compatibility. For interface/list rules, `Subject + Ingress` may carry a
deterministic legacy projection, but that projection is not source authority.
Legacy `SubjectModeExcluded` remains a compound-compatible legacy form:

`legacy ingress AND NOT selected subject addresses`

It must not be rewritten into a single new source kind.

The source model does not express arbitrary AND/OR. Combining Device + Interface
or IP + Interface into a new generic expression is out of scope.

## 5. TrafficIngress new role

`TrafficIngress` becomes `Ingress Analysis` / `Interface Recommendation`.
It may continue to provide:

- inferred LAN/client ingress candidates;
- WAN, PPPoE parent, bridge-slave, disabled, dynamic, WireGuard, tunnel, and
  physical hints;
- recommended ordering, warnings, and rationale;
- Doctor Decision Trace evidence.

It may not hide a real RouterOS interface or interface-list from the source
selector and may not reject a typed source solely because it is not a
candidate, is marked as WAN, is disabled, or is dynamic. Such conditions are
warnings when the RouterOS matcher is still valid. Missing/renamed objects,
unsupported matchers, and proven semantic conflicts remain blockers.

The old `/traffic-ingress` and `/lan-scope` endpoints remain compatibility
surfaces. They must not become the authority for new `sourceScope` rules.

## 6. API impact

Keep the existing discovery response and `trafficIngress` field for old
clients and Doctor compatibility. Add a factual source-selector read surface,
preferably a sibling endpoint such as:

`GET /api/policy-routing/source-selectors`

The endpoint must expose RouterOS facts separately from rosboard inference:

- `interfaces`: every real RouterOS interface returned by the factual read;
- `interfaceLists`: every real RouterOS interface-list returned by the factual
  read;
- factual fields such as name, type/kind, running, disabled, and dynamic;
- optional `roleHints`, `warnings`, `recommended`, and rationale fields;
- separate fact/inference availability and warnings;
- snapshot identity/fingerprint information where available.

The critical availability rule is:

**fact availability must not depend on the complete TrafficIngress topology
analysis succeeding.**

For example, if `/interface` and `/interface/list` succeed but `/ip/route`,
bridge-port, DHCP, PPPoE, or another inference dependency fails, the API must
still return the successfully read interfaces/lists. It may mark recommendation
or role analysis as partial/unavailable and attach warnings, but it must not
erase the factual selector list or make a valid interface unselectable.

The implementation may reuse the same read-through snapshot/cache so the same
RouterOS menu is not read twice, but it must not call an all-or-nothing Scanner
path whose unrelated required failure suppresses already available facts.

The endpoint must not issue RouterOS mutation and must not be the apply
authority. Apply uses a separate fresh source preflight.

A new rule payload carries `routingRule.sourceScope`. Legacy payloads
containing `subject`, `ingress`, or top-level `trafficIngress` continue to be
accepted at the compatibility boundary under the canonical-write rules in
section 7.

The old `/traffic-ingress` and `/lan-scope` endpoints remain compatibility
surfaces. They must not become the authority for new `sourceScope` rules.

## 7. Persistence and model impact

Use an additive migration only. Add a nullable/defaulted source selector
payload such as `source_scope_json` (or an equivalent versioned column/table)
to `policy_v2_routing_rules`; do not rewrite existing rows during the first
migration.

`source_scope_json` stores the source discriminator and interface/list name
where applicable. It must not contain a second serialized copy of the shared
Subject, AnchorMAC, pinned/last IPs, member identities, or explicit IP-prefix
payload that is already canonical in existing Subject child storage.

Read behavior:

1. If canonical `sourceScope` is present, normalize its kind.
2. For `device` / `ip`, read the canonical payload from the existing Subject
   and child tables according to that kind.
3. For `interface` / `interface-list`, read the canonical name from
   `sourceScope`.
4. If no `sourceScope` is present, read the existing legacy
   `Subject + per-rule Ingress` representation unchanged.

Write behavior for a modern payload:

- normalize `sourceScope` first;
- write `sourceScope` plus the kind-specific canonical payload in one SQLite
  transaction;
- for `device` / `ip`, reuse existing Subject/child-table persistence rather
  than duplicating it into JSON;
- for `interface` / `interface-list`, write a deterministic legacy projection
  only for compatibility;
- proposal clone/hash, desired hash, revision and CAS identity must include the
  source kind plus the canonical kind-specific payload;
- global `TrafficIngress` must not be rewritten by saving a canonical source
  rule.

Write behavior for an old payload against an already canonical rule:

- derive the expected legacy projection from the current canonical source;
- if the incoming legacy source fields are absent or semantically equal to that
  projection, preserve the canonical `sourceScope` and allow non-source
  updates;
- if the incoming `Subject` / `Ingress` would change the effective source,
  reject with a stable conflict/validation error;
- do not silently overwrite canonical source, silently ignore the source edit,
  or automatically reverse-migrate the rule.

Legacy edits to rules with no `sourceScope` remain legacy.

No migration may reinterpret a legacy excluded rule as a positive device rule
or drop an existing ingress condition. Existing rules must remain editable and
apply-equivalent after upgrade.

## 8. Planner and materializer impact

Introduce a routing-only source compiler that normalizes a rule into a
family-aware matcher while preserving the current desired-state lifecycle:

- preview-only planning;
- deterministic logical IDs and plan identity;
- fresh preflight on plan and again after the apply CAS/gate boundary;
- runtime RouterOS `.id` binding;
- actual fingerprint and stale-plan detection;
- owned/foreign object protection;
- restart-safe checkpoints;
- staged activation, verification, and existing compensation/recovery;
- IPv4/IPv6 family isolation;
- Access-first cross-domain ordering.

Materialization requirements:

- Device: reuse current terminal resolution and source address-list generation;
  do not require any TrafficIngress candidate.
- IP: interpret the canonical Subject prefix payload as explicit user IP/CIDR
  source, parse/canonicalize with `netip`, emit only the matching family, and
  never add an inferred interface matcher.
- Interface: put the requested name directly in the mangle connection/routing
  matchers as `in-interface`; do not create the ingress aggregate just to
  represent it.
- InterfaceList: put the requested list directly in the matchers as
  `in-interface-list`; do not replace it with inferred candidates.
- Legacy all/excluded: retain the current managed aggregate and exact matcher
  semantics until a separately reviewed migration changes them.
- `output` rules remain separate from `prerouting`; no source interface field
  may leak into router-originated output rules.

`RoutingSourceKind=all` is a stable blocker in the first release. The built-in
RouterOS interface-list `all` is also a stable blocker because it aliases the
same deferred broad-source semantics. It remains visible as a RouterOS fact;
the blocker is a safety rule, not a recommendation/candidate decision.

Fresh preflight must re-read the authoritative object type after final
plan/CAS checks and before the first RouterOS write:

- Interface source: exact name must exist in `/interface`.
- InterfaceList source: exact name must exist in `/interface/list`.
- Missing, renamed, or wrong-kind source is a blocker.
- Built-in interface-list `all` is a stable
  `equivalent_to_deferred_all_source`-style blocker.
- Disabled, dynamic, WAN-role, bridge, tunnel, or recommendation metadata is a
  warning only when RouterOS can legally use the matcher.
- Device/IP source validates its canonical Subject payload and family without
  consulting TrafficIngress candidates.

No apply write is allowed after a source preflight blocker. Discovery metadata
is advisory and must never be substituted for the requested source.

## 9. Conflict semantics

Keep current winner/blocker semantics. Do not build a topology solver.

- Proved overlap of same-family IP/CIDR source ranges plus overlapping IP
  targets and different egresses remains a blocker.
- Proved identical interface selectors or identical interface-list selectors
  enter the same existing overlap rule; different egresses cannot silently
  compete for the same known source/target space.
- Domain target overlap continues to use existing Priority projection
  semantics: different effective priorities warn and order the winner first;
  equal priority blocks.
- Interface versus interface-list, interface versus IP, and IP versus
  dynamic/peer topology are not solved from hints. If the target/effective
  egress combination could overlap, emit an explicit indeterminate-overlap
  warning and retain deterministic Priority/order behavior; do not claim the
  sources are disjoint.
- Same-egress overlap remains allowed as today.

For example, `10.0.0.0/24` versus `10.0.0.10` is a proven source overlap. An
`Interface(wg1)` versus `IP(10.0.0.10)` pair is not proven by the interface's
own address or by a candidate hint; it is warning/unknown rather than a
topology-derived blocker. No generic expression AST is introduced.

## 10. Compatibility and migration

The existing legacy migration remains the source of truth for old rows:

- old source-to-egress rows become `SubjectModeAll` rules;
- the former global scope is copied into all/excluded rules once;
- selected rules remain source-only;
- old `SubjectModeExcluded` keeps ingress plus negated subject addresses.

Do not silently map legacy `all + ingress` to new `all`, because the former
means “all addresses within the persisted ingress constraint” and the latter
means “all forwarded sources” (and may be unsafe). If an old rule has both
members/prefixes and ingress semantics, keep the legacy representation.

The global device `trafficIngress` remains readable and writable for old
clients, but no new source-scope plan may depend on it. After routing authority
is established, changing that global value must not rewrite a canonical rule or
block a source-only new rule.

## 11. UI flow

Replace the wizard's “策略入口” gate with a “来源范围” step:

1. choose Device, IP/CIDR, Interface, or InterfaceList;
2. show the corresponding typed editor;
3. load factual interfaces/lists from the source-selector API;
4. sort recommended items first, but show every successfully read RouterOS
   fact even when recommendation analysis is partial or unavailable;
5. display role warnings/reasons without disabling otherwise valid selectors;
6. show the built-in interface-list `all` as a real RouterOS list but mark it
   safety-blocked because it is equivalent to the deferred All source;
7. show an explicit deferred explanation for the first-class `All` source;
8. plan and apply through the existing preview and job flow.

A non-recommended bridge/WAN/disabled/dynamic selector must not be hidden or
blocked merely because of its role hint. The `all` interface-list exception is
blocked by the explicit broad-source safety boundary, not by recommendation.

Editing a legacy rule must show its effective compound semantics and preserve
it unless the user explicitly chooses a new simple source kind.

If an old client edits a canonical rule, the server-side compatibility rule in
section 7 remains authoritative: a non-source update is allowed only when the
legacy source projection is unchanged; a source change must fail explicitly.

Access Control pages and shared terminal selectors retain their current
meaning.

## 12. Doctor impact

Keep Doctor, Deep, export, and `IngressDecision` trace contracts unchanged in
this design phase. Follow-up wording should say that only WireGuard is
currently *recommended* and may indicate topology-analysis trouble; it must
not imply that policy routing is unavailable. The deep topology finding is
already non-overall-affecting; no Doctor rewrite is included in the first
implementation slice.

## 13. Security and fail-closed requirements

The refactor must not weaken:

- no automatic RouterOS mutation during discovery;
- planner preview-only semantics;
- fresh apply preflight;
- runtime `.id` binding;
- CAS/revision and deterministic plan identity;
- stale fingerprint detection;
- owned versus foreign object protection;
- restart-safe checkpoints and compensation/recovery;
- IPv4/IPv6 isolation;
- Access-first cross-domain ordering;
- Routing-to-Routing conflict semantics.

Source selector names are untrusted input. They must be normalized, typed, and
re-read from RouterOS before write. Discovery metadata is advisory only.

Fact retrieval and inference retrieval have different failure boundaries:
failure of route/bridge/WAN/recommendation analysis must not suppress already
successful `/interface` or `/interface/list` facts. Conversely, fact absence
or fresh preflight mismatch must fail closed before the first RouterOS write.

A failed preflight must not silently substitute a candidate, an aggregate list,
or the global scope.

Canonical source authority must also fail closed on incompatible old-client
writes. Once a rule has `sourceScope`, a legacy source mutation that differs
from the canonical projection must return an explicit conflict/validation
error rather than creating split authority.

The first release must reject both the first-class `All` source and the
built-in interface-list `all` alias until the dedicated broad-source safety
gate is approved.

## 14. Acceptance tests

The implementation is accepted only when all of the following are covered by
focused unit/API/planner/apply tests:

- **A — false-negative discovery:** discovery recommends/exposes only `wg1`;
  a new `IP(10.0.0.0/24) -> YouTube -> WAN2` rule can plan/apply with no
  TrafficIngress candidate and no `in-interface` matcher.
- **B — interface:** `Interface(wg1)` materializes `in-interface=wg1`.
- **C — interface-list:** `InterfaceList(LAN)` materializes
  `in-interface-list=LAN`.
- **D — facts versus recommendation:** real `bridge1` remains visible even if
  analysis marks it WAN/non-recommended; selection is saveable and only shows
  a warning.
- **E — fresh deletion:** selecting `wg1`, deleting it before apply, and
  applying causes a fresh-preflight blocker with zero erroneous rule writes.
- **F — legacy upgrade:** old rules reload, edit, plan, and apply with
  semantically equivalent matchers; no silent subject/ingress change occurs.
- **G — IPv4:** `10.0.0.0/24` produces IPv4 source matching only.
- **H — IPv6:** `fd86::/64` produces IPv6 source matching only and does not
  enter the IPv4 desired graph.
- **I — canonical authority versus old client:** create a canonical Interface
  rule, then submit an old-client payload with no `sourceScope`.
  - unchanged legacy projection + non-source edit is accepted and preserves
    canonical source;
  - changed legacy `Subject`/`Ingress` source is rejected with a stable
    conflict/validation error;
  - no silent source drift occurs.
- **J — fact availability despite inference failure:** `/interface` and
  `/interface/list` succeed while route/bridge/other analysis fails. The
  source-selector API still returns all successful facts and marks inference
  partial/unavailable instead of returning an empty selector list.
- **K — built-in list `all`:** it is present in factual interface-list output,
  is not hidden as a candidate failure, but plan/apply is blocked with a stable
  broad-source safety reason until the All safety gate is approved.
- **L — single canonical device/IP payload:** source persistence/proposal
  round-trip proves that device identity, AnchorMAC, pinned/last IPs and IP
  prefixes are not duplicated inside `source_scope_json`; existing Subject
  stores remain the single payload authority.

Additional required regressions cover proposal clone/hash, CAS, actual
fingerprint drift, runtime IDs, restart checkpoints, partial apply recovery,
foreign-object protection, conflict cases, RouterOutput separation, and
AccessRule/Subject behavior.

## 15. Non-goals

- arbitrary source boolean expressions or a generic rule AST;
- topology inference or a WireGuard peer/address solver;
- automatic RouterOS mutation during discovery;
- changing target-list, egress, DNS Priority, or Access semantics;
- redesigning Doctor in this task;
- production deployment, database backup, merge, or release acceptance;
- enabling `All` before its explicit forwarded/output/management safety gate.

## 16. Rollout and review gates

Implementation is split into four slices described in `design.md` and
`implement.md`. Each slice keeps the same branch and must pass focused tests
before the next slice.

Root review is required after each slice. Slice 1 may start only after this
design revision is approved, including these fixed boundaries:

1. `sourceScope` does not serialize a second Subject/device/IP payload;
2. canonical rules reject conflicting old-client source edits instead of
   creating split authority;
3. factual interface/list availability is independent from unrelated topology
   inference success;
4. built-in interface-list `all` is visible as a fact but blocked by the same
   deferred broad-source safety gate as first-class `All`.

The dedicated `All` safety gate remains outside the four implementation slices.

