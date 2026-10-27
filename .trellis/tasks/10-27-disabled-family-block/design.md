# Design

## Execution contract
Separate an egress address-family action (route or block) from DNS transport selection. Derive both IPv4 and IPv6 policy actions; enabled families retain the current route foundation, disabled/missing families create blocking execution without requiring a gateway, route table or NAT. Disabled rules/egresses must not create active blocking.

Preserve the existing normalized source compiler, target projection and deterministic priority winner. Include route/block action in execution-group identity so sharing cannot merge different outcomes. Classify matching flows through managed connection marks; use dedicated routing-owned filter DROP rules for blocked winners. Do not use independent unordered per-target drops that could override a higher-priority routing winner. Router-output matching follows its existing distinct aggregation and mark contract, with no ingress restriction added to router-originated traffic.

## Targets and DNS
Keep current FWD/forwarder/alias transport behavior. Build static target members for all enforceable families, including blocked families, and retain DNS-derived address-list population. No AAAA or NXDOMAIN override. Exclude local destinations consistently and prevent accidental blocking of policy DNS transport itself.

## Filter ownership and ordering
Use stable canonical routing logical IDs and exact scoped ownership. Extend routing desired/scan/domain-selection/diff/apply/stale cleanup to include these specific filter identities. Existing access filter identities remain access-owned. Review reconcile.go filter ordering and FastTrack capability handling before selecting final anchors. Block filters must precede established accept/FastTrack paths that can bypass them, while respecting foreign rule ownership. Never edit foreign rules except through the established explicit FastTrack recovery contract.

## Existing connections
New-connection-only marking is insufficient when the user disables a family on established traffic. Implementation must inspect the existing apply/connection handling before coding: preserve priority and account for FastTrack. Prefer exact, previewed invalidation of affected policy connections plus correct filter positioning; if safe identification is impossible, surface a concrete blocker/recovery action instead of silently claiming protection. No broad conntrack flush. The disposable ROS scenario is a required technical validation gate.

## Compatibility and presentation
Reuse Egress.families without a schema change if feasible. Missing-family rows follow disabled-family blocking. Update canonical and compact UI wording wherever the switch/preview is exposed. This intentionally changes existing single-family policy behavior; surface a warning and exact changes in a fresh plan rather than silently mutating routers on app upgrade.

## Rollout and rollback
Use one independent work branch and Draft PR targeting main after checking current workstream/base differences. Do not put this change onto the unrelated container-management workstream. Validate on 10.0.0.60, then use production NAS backup and user acceptance before delivery acceptance or merge. A rollback must explicitly show the removed blocks and the resulting prior bypass behavior; preserve external NAT and access rules.
