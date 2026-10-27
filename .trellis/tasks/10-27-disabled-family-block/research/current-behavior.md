# Evidence and implementation risks

- Runtime investigation: policy IPv6 disabled, DNS upstream remains an IPv4 address, both A/AAAA still forwarded. Fresh IPv4 succeeded; fresh IPv6 was unmarked, not NATed, no replies; active main IPv6 default was local PPPoE. No private raw outputs retained.
- internal/policyv2/routing_desired.go: target IP projection filters on familyEnabled; buildRoutingMangleFamily already compiles source matchers and execution groups; reorderRoutingConnectionObjects provides deterministic connection-before-routing order. Extend winner semantics rather than adding unconditional filters.
- internal/policyv2/routing_fasttrack.go and routing_fasttrack_state.go: foreign FastTrack has an established inspection/recovery mechanism. Adding a DROP below FastTrack is not sufficient.
- internal/policyv2/reconcile.go includes filter menus, but .trellis/spec/backend/policy-routing.md separates Routing and Access execution ownership. New routing filters require explicit domain ownership/classification updates.
- web/src/features/policy/ui/EgressFields.tsx contains the canonical family editors. Check compact wizard surfaces too.
- Technical validation still required during implementation: exact filter anchor ordering, new-connection FastTrack interaction and untracked traffic. Product behavior is settled; unsupported enforcement must block application rather than silently leak.
- The current branch is codex/container-management, with unrelated untracked artifacts. No product changes were made during planning. Determine main/base dependencies before creating the feature work branch.

## Execution preflight (2026-10-27)

- The isolated work branch is based on remote `main` at `0d8c67059bb9f9650f1b698cd6621f43c5dc6e0c`. The original checkout and unrelated container work were not modified.
- The existing compiler marks only `connection-state=new` flows. The user approved a new-connection-only guarantee: existing connections may expire naturally and no connection cleanup is in scope. FastTrack and untracked new traffic still require verification.
- The disposable host `rosboard-test` is Debian and has running rosboard test services; it is not itself a RouterOS device. The user identified the separate RouterOS device configured as “测试” for this task. Read-only checks confirmed that its REST endpoint is reachable from the test host, and identified it as a CHR running RouterOS 7.23.7. Use an independent test configuration and data store; do not copy the production configuration or database.
- The original checkout's Git metadata is read-only under this session's filesystem policy. Implementation is isolated in `/private/tmp/rosboard-disabled-family` instead.
