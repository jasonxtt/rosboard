# Technical Design

## Root cause

The discovery endpoint creates a fresh `policyv2.Scanner` and rescans the
RouterOS actual state on every request. The first policy apply can therefore
change the RouterOS route set without changing React state or SQLite desired
state.

The scanner currently treats every active default route in every routing table
as physical WAN evidence. A rosboard-owned policy route such as
`0.0.0.0/0 via 10.0.0.1%lan` is an execution projection, not proof that
`lan` is an internet-facing interface. Once that route is applied,
`buildWANCandidates` adds `lan` to the WAN set and
`buildTrafficIngressCandidates` removes it from ingress candidates. This is a
post-apply self-observation feedback loop.

The scanner must retain all routes in the actual scan and fingerprint, but
must exclude only rosboard-owned policy routes from physical WAN
classification. It must not infer LAN solely from RFC1918 addresses, because
real WAN links may use private addresses.

## Scope

Modify only the policyv2 discovery candidate classification and its tests unless
the implementation proves that a small API test is required. No frontend or
proposal lifecycle changes are expected.

## Behavior

1. Read the RouterOS interface, route, list, member, address, and optional
   bridge data as the existing scanner does.
2. Resolve nested interface-list membership deterministically.
3. Mark rosboard-owned policy routes while retaining their actual route data.
4. Ignore only those marked policy routes when building physical WAN evidence.
5. Continue using managed ingress aggregates when computing interfaces that
   must not be offered as WAN candidates.
6. Filter managed aggregate lists only from the user-facing list candidates;
   retain user-owned lists and direct interface candidates according to the
   existing safety rules.
7. Return the same canonical discovery payload and preserve existing warnings.

## Test shape

Use the existing local `discoveryReader` fake. Add a before/after scan fixture
with a physical `lan` interface, a main PPPoE default route, and an after
state that adds canonical rosboard policy default routes through `lan`.
Assert `lan` remains an ingress candidate and `pppoe-out1` remains WAN.
Add an unmanaged Ethernet WAN with a private gateway to prove that the fix is
not an RFC1918 heuristic. Retain coverage for direct managed members and add
the low-cost managed `include=LAN` case to protect nested list semantics.

## Rollout and rollback

Commit and push a focused change on the existing branch. Wait for root review;
keep PR #4 Draft. After explicit approval, build a Linux amd64 binary and
replace only `/opt/rosboard-test/rosboard` on `10.0.0.60`, then restart and
verify the test service, health endpoint, affected API/assets, and logs. No
production backup or deployment is in scope.
