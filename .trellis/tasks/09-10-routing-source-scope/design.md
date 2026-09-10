# Routing source scope and TrafficIngress decoupling — design

Revision: root-review P1 closure; planning only.

## Design baseline and archaeology record

Baseline: `5a3a2197c1940a0bc72c2778614a8e7da0b9e356`.

The observed data flow is:

```text
RouterOS reads
  -> policyv2.Scanner
     -> Discovery.TrafficIngress (inferred candidates)
     -> IngressDecision trace (Doctor evidence)
  -> API discovery / traffic-ingress normalization
  -> wizard TrafficIngressScope
  -> RoutingRule.Ingress / DeviceState.lan_scope_json
  -> BuildDesired / ValidateTrafficIngress
  -> managed aggregate interface-list
  -> mangle prerouting in-interface-list + target matcher
```

The parallel source-only path is:

```text
RoutingRule.Subject selected
  -> existing terminal identity and resolved IP evidence
  -> per-family owned src-address-list
  -> mangle prerouting src-address-list + target matcher
```

The first path is the false-negative coupling. The second path already proves
that a routing rule can be materialized without TrafficIngress, but it is not a
typed source selector and its UI/API still calls the broader concept a
`Subject`.

## Coupling inventory

| Location | Current coupling | Required treatment |
| --- | --- | --- |
| `internal/policyv2/routing_rule.go` | all/excluded require `Ingress`; selected discards it | preserve for legacy; add routing-only typed source path |
| `internal/policyv2/traffic_ingress.go` | normalizer classifies by candidate set; WAN/disabled/dynamic are blockers | split factual source validation from recommendation validation |
| `internal/policyv2/desired.go` | legacy/non-authoritative enabled egress requires global scope; aggregate list is built | remove dependency for new canonical source rules; retain compatibility path |
| `internal/policyv2/routing_desired.go` | all/excluded always use managed aggregate; no direct interface matcher | compile new interface/list kinds directly; keep legacy aggregate |
| `internal/store/routing_rule.go` | two ingress JSON columns; migration copies global scope; save fills missing scope from global | additive source payload; no reinterpretation of old rows |
| `internal/store/policy_proposal.go` | top-level proposal `TrafficIngress` is normalized against candidates; rule save uses it as fallback | new source scope is inside rule; top-level field remains old-client fallback only |
| `internal/api/policy_routing.go` | discovery returns candidates; proposal and traffic-ingress PUT normalize against candidates | add fact selector API; typed source preflight does not use candidates |
| Aurora/compact wizard and source helpers | all/excluded require a non-empty ingress and render only candidates | replace with typed source editor; leave Access selectors alone |
| Doctor deep report | only-WireGuard finding is worded as candidate result | retain trace; revise wording later to recommendation language |
| `internal/policyv2/routing_rule.go` + Access Control | shared Subject alias | do not put source kind or interface data on Subject |

No current code path was found that requires `TrafficIngress` for a selected
subject once it reaches the authoritative routing builder. The hard blockers
are the normalization/API/discovery seams and the legacy global desired path,
not the device identity resolver.

## Proposed canonical model

The compatibility-safe model is an optional routing-only typed selector, but
the selector must not serialize another full `Subject` payload.

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

Canonical payload ownership is deliberately split by source kind without
duplicating data:

| New source | Canonical source payload | Compatibility projection |
| --- | --- | --- |
| device | `sourceScope.kind=device` + existing `RoutingRule.Subject` identity/member child tables | existing selected Subject; empty ingress |
| ip | `sourceScope.kind=ip` + existing Subject prefix child tables interpreted as explicit prefixes | selected Subject prefix projection; empty ingress |
| interface | `sourceScope.kind=interface,name=<x>` | all Subject + one `Ingress.Interfaces` name |
| interface-list | `sourceScope.kind=interface-list,name=<x>` | all Subject + one `Ingress.InterfaceLists` name |
| all | discriminator only | none; apply deferred |

`source_scope_json` must not contain a nested Subject, AnchorMAC, pinned/last IP,
terminal members, or a second copy of IP prefixes. The existing Subject and
identity/prefix stores remain the single canonical payload for Device/IP.

`RoutingRule.Subject` and `RoutingRule.Ingress` remain in the struct as
compatibility fields. For interface/list rules the legacy projection is
derived, not authoritative. A legacy `excluded` rule is not converted to a new
simple source.

### Canonical write authority

Once a rule has `sourceScope`, a write that omits `sourceScope` is treated as
an old-client compatibility write:

1. derive the expected legacy `Subject`/`Ingress` projection from the current
   canonical source;
2. if the incoming legacy source fields are omitted or semantically identical,
   preserve the canonical source and allow non-source edits;
3. if they differ in effective source semantics, reject with a stable
   409/422-style conflict/validation error;
4. never silently ignore the source edit, silently change the canonical source,
   or guess a reverse migration.

A modern payload that includes `sourceScope` updates the canonical source and
its kind-specific payload in one transaction.

This prevents a modern rule from entering a split-brain state where
`sourceScope` says `wg1` but an old `Ingress` projection says `bridge1`.

The model remains a typed seam, not a generic expression tree.

## Source compiler and desired objects

Add a routing-only compiler between rule normalization and
`buildRoutingMangleFamily`. Its output should be a small internal matcher,
not a public AST:

```text
family matcher:
  sourceAddressList?  // device or IP
  inInterface?        // interface
  inInterfaceList?    // interface-list or legacy aggregate
  excludedAddressList?// legacy excluded only
  boundary            // selected, direct-interface, direct-list, legacy, ...
```

Compilation rules:

1. Device calls the existing terminal evidence and address-list projection.
   It must never call candidate normalization.
2. IP parses every value with `netip`, masks CIDRs, turns a host into a
   host-prefix, rejects mixed/invalid family payloads only at the relevant
   family, and emits an owned per-rule/per-family address-list. A managed
   address-list is preferred over duplicating `src-address` fields in every
   target matcher because it matches the existing Device path and gives one
   stable ownership identity. Its mangle semantics are equivalent to
   `src-address=<prefix>` and must not add an ingress field.
3. Interface uses the exact requested RouterOS name in `in-interface` on the
   connection-mark rule and the corresponding routing-mark rule.
4. InterfaceList uses the exact requested list name in `in-interface-list` on
   both rules. It does not create a rosboard aggregate or member rows.
5. Legacy all/excluded continues to use the existing aggregate list, preserving
   current object identity and cleanup order.
6. All is rejected with a stable blocker until its safety design is approved.

The execution-group key must include the normalized source matcher. A direct
interface and a same-named interface-list must never share a mark merely
because their display name is equal. Reuse is allowed only when the complete
family matcher, target projection, table, and enabled state are equal.

Logical IDs for new source address lists and mangle rules must be stable across
source value edits. The values belong in desired fields and the plan hash, not
in the identity used for runtime `.id` binding. Existing `ScanManaged` comment
identity, foreign-object checks, structural field allowlist, actual fingerprint,
staged activation, and verification remain the authority.

## IPv4/IPv6 and RouterOutput boundaries

Device and IP sources are split by the existing family-specific address-list
menus. An IPv4 source never creates an IPv6 source object and vice versa.
Interface and interface-list matchers are family-neutral inputs but are emitted
once in each enabled mangle family, exactly like current target matching.

The source matcher applies only to the forwarded `prerouting` pair. The
existing `RouterOutput` output pair remains a separate egress-wide decision and
must not receive `in-interface`/`in-interface-list`. This is a key reason the
new `all` value cannot be implemented by simply deleting the current source
field: it needs an explicit policy for output traffic and management/control
traffic.

## Fresh preflight

Replace the candidate-based validation branch with a typed source preflight.
It should be callable from the existing `appendRoutingValidation` seam and run
during plan generation, cached-plan apply checks, and the final apply path
after CAS/gate acquisition.

For a typed interface:

- read live `/interface`;
- require exact requested name;
- missing/renamed/wrong kind is a blocker;
- disabled, dynamic, WAN-role, bridge, tunnel, and recommendation data are
  warnings unless RouterOS cannot legally use the matcher.

For a typed interface-list:

- read live `/interface/list`;
- require exact requested name;
- do not reject merely because the list is inferred as WAN, LAN, dynamic, or
  not recommended;
- treat the built-in list `all` as a stable safety blocker because
  `in-interface-list=all` aliases the deferred first-class `All` source;
- keep `all` visible in factual discovery despite that blocker.

For Device/IP:

- validate the canonical existing Subject payload according to
  `sourceScope.kind`;
- do not require a TrafficIngress candidate;
- Device uses the existing identity/AnchorMAC/resolution lifecycle;
- IP uses explicit canonical prefixes and family validation.

The existing `ValidateTrafficIngress` function may remain as a compatibility
wrapper while strict candidate/role checks are removed from new source
admission. Legacy rows retain their exact aggregate materialization.

The final source preflight must complete before the first RouterOS create/patch.
No inferred candidate may be substituted for a failed requested source.

## Discovery API and scanner reuse

Do not add a second independent topology read, but do not make factual source
selection depend on the all-or-nothing success of the existing full Scanner
path either.

Use a read-through evidence/cache layer and separate two projections:

```text
Fact projection:
  /interface
  /interface/list
  -> selector facts

Inference projection:
  routes / bridge ports / members / DHCP / PPPoE / addresses / existing
  TrafficIngress analysis
  -> role hints / recommendation / reasons
```

The fact projection owns selector availability. If `/interface` succeeds,
those interfaces remain returnable even when `/ip/route` or another topology
dependency fails. If `/interface/list` succeeds, those lists remain returnable
under the same condition.

The inference projection may be `available`, `partial`, or `unavailable`.
Failures become warnings/availability metadata; they do not erase facts.

The implementation may reuse one read-through snapshot so a menu already read
for facts is not read again by inference. It must not call a Scanner method
whose unrelated required failure discards successfully collected fact objects.

The source-selector endpoint should serialize:

```text
facts:
  interface name/type/running/disabled/dynamic
  interface-list name and minimal factual metadata

inference:
  roleHints/warnings/recommended/reasons
  availability: available|partial|unavailable

snapshot:
  captured/fingerprint information where available
```

`TrafficIngressCandidate` and `IngressDecision` remain inference/Doctor
contracts. A rejected candidate must never be interpreted as “the RouterOS
interface does not exist.”

The built-in interface-list `all` is included in facts. It may carry a
`safetyBlocked`/reason hint for UI clarity, but the authoritative blocker is
enforced again by planner/apply preflight.

## Conflict model

Normalize source overlap in a small comparator used by the existing
`RoutingRuleConflicts` path:

- IP/IP uses family-aware prefix overlap.
- Device/device uses existing terminal ID and resolved-address evidence.
- Interface/interface and list/list with identical selectors are proven
  overlap.
- Interface/list, interface/IP, list/IP, and dynamic peer relationships are
  indeterminate unless an exact fact-based equality is available. Do not solve
  routing topology or infer client address ownership.

Only the existing applicable target/effect rules decide severity. Known
overlap across different egresses with overlapping IP targets blocks as today.
Domain projection overlap keeps Priority warnings/blockers. Indeterminate
source overlap produces a warning with explicit language; it is not silently
treated as disjoint. Same egress remains allowed.

## Proposal, CAS, and apply lifecycle

`PolicyProposal` already carries the whole `RoutingRule`, so the typed source
selector lives inside that rule rather than as another global field. Proposal
clone/hash must preserve:

- `sourceScope.kind`;
- interface/list name when applicable;
- the existing canonical Subject/identity/prefix payload for Device/IP;
- AnchorMAC, pinned/last IPs, target versions and current revisions.

It must not add a second nested Subject copy to `source_scope_json`.

The top-level `PolicyProposal.TrafficIngress` stays only for old clients and
for an explicit global ingress edit with no canonical routing-source change.
New source-rule preparation must not scan `Discovery.TrafficIngress` or rewrite
global device state.

### Old-client writes against canonical rules

If a stored rule has `sourceScope` but an incoming old-client payload omits it:

- compare the incoming legacy source projection with the projection derived
  from the current canonical source;
- equal/omitted source projection permits non-source edits and preserves
  `sourceScope`;
- any source-semantic mismatch fails with a stable conflict/validation error
  before revision commit;
- no success response may hide an ignored source edit.

All accepted source changes participate in normal proposal hash, desired hash,
revision/CAS and stale-plan checks.

At apply time the source preflight happens before the first create/patch. If an
interface/list disappears after plan generation, the job fails closed. Existing
staged apply/checkpoint/restart behavior remains responsible for recoverable
partial state; no special rollback shortcut may bypass ownership or
verification.

## Doctor and Access boundaries

Doctor's deep scanner and `IngressDecision` trace are formal baseline outputs.
The design does not change their schemas or exports. A later UI wording slice
can change “only WireGuard candidate” to “only WireGuard recommended” while
keeping the finding non-blocking for routing.

Access Control continues to use the shared Subject only for client identity.
No `TrafficIngress`, source kind, interface selector, or routing source
compiler may enter `internal/accesscontrol` semantics. Cross-domain Access
before Routing ordering and target projection checks remain untouched.

## Compatibility matrix

| Input | Read | Plan/apply |
| --- | --- | --- |
| old selected Subject | yes | existing source-only address-list semantics |
| old all + per-rule Ingress | yes | existing aggregate-list semantics |
| old excluded + Ingress + Subject | yes | existing aggregate + negated address-list semantics |
| new Device | typed discriminator + existing Subject identity payload | source address-list, no ingress dependency |
| new IP | typed discriminator + existing Subject prefix payload | family-isolated source address-list, no ingress dependency |
| new Interface | typed source first | direct `in-interface`, fresh existence check |
| new InterfaceList | typed source first | direct `in-interface-list`, fresh existence check |
| new InterfaceList(`all`) | visible/readable fact | stable blocker until All safety gate |
| new All | typed source first | explicit blocker until safety gate |
| old-client non-source edit of canonical rule | projection compared | allowed only if legacy source projection is unchanged |
| old-client source edit of canonical rule | projection mismatch | explicit 409/422-style rejection; no silent drift |

The global `trafficIngress` value remains compatibility state for old clients.
It is not an authority for any canonical typed source rule.

## Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Canonical source and legacy projection split authority | canonical-write rule: old-client source mismatch is rejected, non-source edits require equivalent projection |
| Device/IP payload duplicated inside `source_scope_json` | discriminator/name only; existing Subject and identity/prefix stores remain the single payload authority |
| Fact selector disappears when unrelated topology analysis fails | fact projection availability is independent from route/bridge/WAN inference; inference degrades to partial/unavailable |
| Built-in interface-list `all` bypasses deferred All gate | show as factual list but enforce stable broad-source safety blocker in plan/apply |
| New direct matcher gets grouped with legacy aggregate | source-kind-aware execution-group key and logical IDs |
| Discovery snapshot becomes apply authority | final live preflight after CAS/gate, fail closed |
| `All` routes router/control-plane traffic unexpectedly | defer it; no silent fallback |
| Dynamic/WAN inference continues to block selection | fact API plus warning-only role metadata |
| Cross-kind overlap is over- or under-reported | only prove exact/simple overlap; warn on unknown |
| Access behavior changes through shared Subject | keep all routing source semantics in policyv2; do not modify Access interpretation |
| Legacy rule silently changes | no automatic conversion; exact legacy compiler remains |
| Partial apply leaves new source objects | existing ownership comments, checkpoints, verification, and next-plan recovery |

