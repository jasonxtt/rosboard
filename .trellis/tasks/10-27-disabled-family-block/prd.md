# Block traffic for disabled policy egress address families

## Goal
An enabled routing policy must not leak matching traffic through main when its egress disables that traffic address family. A disabled family means DROP for that policy, rather than bypass.

## Confirmed background
On 2026-10-27, disabling the ssjy policy IPv6 family removed its IPv6 policy execution while DNS continued returning AAAA. Fresh IPv6 HTTPS connections had no policy mark or NAT and followed main toward the local PPPoE WAN; IPv4 continued through the configured proxy. No credentials, private exports, or raw production captures belong in this task.
Current projection iterates enabled families only (`internal/policyv2/routing_desired.go:245`); IPv6 IP target materialization also depends on familyEnabled. The UI exposes a family enable toggle without explaining a blocking consequence (`web/src/features/policy/ui/EgressFields.tsx:30`).

## Requirements
- R1: For an enabled policy and enabled egress, a disabled family blocks matching traffic with DROP. Apply this consistently to disabled IPv6 and disabled IPv4, including a family absent from a legacy egress shape. Preserve validation requiring an executable DNS transport family when domain forwarding needs it.
- R2: Match exactly the policy source, ingress, exclusions, target lists, keyword-domain choice and effective priority. Higher-priority winning policies retain authority; unrelated traffic is unaffected.
- R3: Preserve target materialization for blocked families, including static IP/CIDR targets and DNS-learned addresses. A/AAAA resolution remains unchanged. Domain matching retains the current DNS-learning limitation; this is not hostname inspection or AAAA suppression.
- R4: Cover forwarded traffic and, only when routerOutput is enabled, policy-associated router-originated traffic. Preserve existing policy semantics for determining which target wins for router output.
- R5: Whole-rule/whole-egress disable removes or disables its enforcement; re-enabling the family replaces blocking with normal routing. Delete, repeated apply, shared sources, and another manager/device remain safe and idempotent.
- R6: Blocking must not be bypassed by an earlier established/related accept or FastTrack. Activation must account for already-established affected connections; any targeted connection invalidation must be explicit in the preview and must not flush unrelated connections.
- R7: UI and plan preview explain the changed family-toggle meaning and expose generated block operations. Applying the new version can change existing single-family policies; that effect must be reviewable before RouterOS mutation.
- R8: Routing owns its new filter objects end to end. Access plans and cleanup must not adopt, reorder, disable or delete them.

## Acceptance criteria
1. IPv4-enabled/IPv6-disabled: matching fresh IPv6 traffic increments a managed DROP counter, has no PPPoE leak and never reaches the configured proxy; IPv4 remains successful. Reverse-family case behaves symmetrically.
2. Verify interface/list/multi-interface, IP, device and exclusion sources; unmatched source/target traffic remains unchanged. Overlapping policies follow existing priority semantics across route and block winners.
3. A and AAAA answers remain available; learnt IPv6 addresses and static IPv6 CIDRs are enforceable even without an IPv6 route foundation.
4. routerOutput false produces no router-output block; true enforces the intended target scope without breaking DNS transport or management connectivity.
5. Test single-to-dual-stack transitions, disabled policies, deletion, repeated applies, ownership isolation, and routing/access independence.
6. On the disposable test ROS, prove rule placement and handling of established/FastTracked connections before any production delivery. No unrelated connection flush is permitted.
7. Required backend/frontend checks pass; production delivery follows AGENTS.md NAS backup and manual acceptance gates.

## Out of scope
AAAA filtering, DNS resolver deployment, proxy sniffing, fixing lmy IPv6, editing VPN services, changing unrelated ROS configuration, and automatic production deployment during planning.

## Planning decisions
DROP is the requested action. Blocking follows the existing family toggle; no new bypass toggle is introduced. Whole-policy disable continues to mean no enforcement by that policy. No unresolved product questions remain.
