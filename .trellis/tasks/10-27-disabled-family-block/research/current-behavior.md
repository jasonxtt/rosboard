# Evidence and implementation risks

- Runtime investigation: policy IPv6 disabled, DNS upstream remains an IPv4 address, both A/AAAA still forwarded. Fresh IPv4 succeeded; fresh IPv6 was unmarked, not NATed, no replies; active main IPv6 default was local PPPoE. No private raw outputs retained.
- internal/policyv2/routing_desired.go: target IP projection filters on familyEnabled; buildRoutingMangleFamily already compiles source matchers and execution groups; reorderRoutingConnectionObjects provides deterministic connection-before-routing order. Extend winner semantics rather than adding unconditional filters.
- internal/policyv2/routing_fasttrack.go and routing_fasttrack_state.go: foreign FastTrack has an established inspection/recovery mechanism. Adding a DROP below FastTrack is not sufficient.
- internal/policyv2/reconcile.go includes filter menus, but .trellis/spec/backend/policy-routing.md separates Routing and Access execution ownership. New routing filters require explicit domain ownership/classification updates.
- web/src/features/policy/ui/EgressFields.tsx contains the canonical family editors. Check compact wizard surfaces too.
- Technical validation still required during implementation: exact filter anchor ordering and selective stale/FastTracked connection treatment. Product behavior is settled; unsupported enforcement must block application rather than silently leak.
- The current branch is codex/container-management, with unrelated untracked artifacts. No product changes were made during planning. Determine main/base dependencies before creating the feature work branch.

## Execution preflight (2026-10-27)

- The isolated work branch is based on remote `main` at `0d8c67059bb9f9650f1b698cd6621f43c5dc6e0c`. The original checkout and unrelated container work were not modified.
- The existing compiler marks only `connection-state=new` flows. Existing connection records exposed by `FirewallConnection` include addresses and marks but no ingress-interface field; source and destination alone therefore do not prove that an interface-scoped policy won. Selective invalidation must not guess that scope or remove unrelated connections. FastTracked flows need a separate proven treatment.
- The disposable host `rosboard-test` is Debian and has running rosboard test services. The preflight did not find a QEMU executable or `/dev/kvm`; it is not itself a RouterOS test device. A disposable RouterOS endpoint and access are still needed for the required packet, placement and FastTrack verification.
- The original checkout's Git metadata is read-only under this session's filesystem policy. Implementation is isolated in `/private/tmp/rosboard-disabled-family` instead.
