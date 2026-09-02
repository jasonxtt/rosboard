# RouterOS domain projection constraint

## Question

Can rosboard safely materialize the same domain TargetList into different RouterOS DNS projections for two subject-disjoint RoutingRules that use different Egress DNS contexts?

Example:

```text
iPad + YouTube → WAN2
TV   + YouTube → WAN3
```

Logically the two rules do not conflict because their Subjects are disjoint. The execution question is whether RouterOS DNS Static can select a different FWD/address-list projection based on the requesting client.

## Official RouterOS behavior

MikroTik documents `/ip/dns/static` as one ordered device-level list. Entries are checked in order. Static FWD entries expose fields including:

- `name` / `regexp`;
- `type=FWD`;
- `forward-to`;
- `address-list`;
- `match-subdomain`;
- `disabled`.

The documented DNS Static matcher does **not** include client/source address, interface, source address-list, or another policy-subject selector.

`address-list` means that addresses learned when a DNS request matches the static entry are dynamically added to the named firewall address-list for the DNS TTL.

Official reference:

- https://help.mikrotik.com/docs/spaces/ROS/pages/37748767/DNS

MikroTik also documents firewall address-lists as the normal IP-address grouping consumed by mangle/filter/NAT:

- https://help.mikrotik.com/docs/spaces/ROS/pages/130220135/Address-lists

## Consequence

The user-level conflict rule remains:

```text
Subject overlap
AND Target overlap
AND different Egress
→ logical routing conflict
```

Therefore two subject-disjoint rules are **not** a logical policy conflict.

However, if the same/overlapping domain content requires different physical DNS/address-list projections, RouterOS DNS Static cannot select which FWD rule/address-list projection to use from the RoutingRule Subject alone. The safety unit is the physical `(device, egress, target)` projection, not the resolver context; equal DNS context does not make two different physical projections safe.

It would be unsafe to create duplicate same-domain FWD entries and assume both independent projections are populated correctly; order would become part of behavior and there is no source-aware selector proving the intended mapping.

## V1 decision

Keep this as a separate RouterOS capability/projection blocker:

```text
domain_projection_context_ambiguous
```

V1 behavior:

- same domain target + same Egress: supported; share one `(device, egress, target)` projection;
- same Egress + different domain target IDs whose content overlaps: block, even when the DNS context is equal;
- different Egress + overlapping domain projections: block; this remains separate from logical subject/target conflict detection;
- IP targets do not have this DNS-specific blocker.

Do not add a DNS proxy, client-specific DNS interception, generic resolver subsystem, or duplicate-order workaround as part of this refactor.

If later product requirements demand allowing the disjoint-subject/same-domain/different-Egress case, design a separate explicit common-resolution or source-aware DNS solution and expose its trade-off instead of hiding it in TargetList/RoutingRule architecture.

## Slice 4C apply-domain implications

The DNS limitation is also relevant when Routing and Access consume the same
logical domain data. They are separate RouterOS consumers and must not be
collapsed into a `Combined` desired graph merely because a physical menu is
shared. Routing and Access each need an independently owned projection and
apply plan.

Within Access, the safe projection key is `(device, TargetListID)`, not
`(ruleID, TargetListID)`. Multiple AccessRules may therefore reuse one
`rb_ac_*` DNS/address-list projection while retaining separate rule filters.
The effective Access target activity is `TargetList.Enabled` together with at
least one enabled AccessRule consumer. A target referenced only by disabled
rules has no active DNS/address-list projection; a mixed enabled/disabled
consumer set keeps the one canonical projection active while each filter keeps
its rule state. Different active Access target IDs with exact/suffix-overlapping
domain content are ambiguous and must block with
`access_domain_projection_ambiguous`.

When enabled Routing and Access target projections overlap in domain content,
the conflict is a concrete cross-domain RouterOS capability blocker. Emit
`cross_domain_dns_projection_ambiguous` in both the Routing and Access plans,
without creating a Combined plan. Shared IP projections do not need this DNS
blocker.

These blockers do not change the logical subject-overlap rule: disjoint
subjects can remain logically valid, while the plan still refuses an unsafe
source-unaware DNS materialization. A later common-resolution or source-aware
DNS design remains out of scope for this refactor.

## Slice 4C final physical-projection rule

The Routing validator first deduplicates enabled references by the physical
key `(egressID, targetID)`. Five RoutingRules using the same key therefore
produce one DNS/address-list projection. It then compares distinct physical
projections for exact/suffix domain overlap:

| Routing projections | Domain content | Result |
| --- | --- | --- |
| same Egress + same Target | any | share; no projection blocker |
| same Egress + different Targets | non-overlapping | allowed |
| same Egress + different Targets | exact/suffix-overlapping | `domain_projection_context_ambiguous` blocker |
| different Egresses + different Targets | overlapping, even equal DNS context | `domain_projection_context_ambiguous` blocker |
| any physical keys | IP-only | no DNS projection blocker |

The existing blocker name is retained for compatibility, but its reason is
now about multiple distinct physical RouterOS projections rather than merely
different resolver contexts.
