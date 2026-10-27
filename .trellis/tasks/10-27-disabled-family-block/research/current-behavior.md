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

## Isolated execution verification

- Test CHR 7.23.7: a fresh IPv6 router-output connection and injected forwarded connection hit the scoped DROP counters; an unrelated destination did not. Test-only addresses, routes, interface and filter objects were removed after use.
- A separate CHR under QEMU used an independent application configuration and database. With an IPv4 FastTrack rule and an established/related accept present, IPv4 blocked-family traffic hit the managed DROP before those rules; an unrelated target did not. An active raw notrack rule blocked preview and was removed after the test.
- On that CHR, source exclusion, family re-enable, whole-egress disable, rule deletion and no-op replay were exercised. A real RouterOS matcher transition exposed rejection of an empty interface patch; a typed, allowlisted unset with staged disable fixed it and the transition was re-tested.
- A local DNS fixture returned A and AAAA to a real DNS client. Both addresses were learned into family-specific lists; the learned IPv6 target hit DROP and a non-member did not. The enabled IPv4 family marked and routed a fresh connection, whose return packet was observed in connection tracking; a test-only NAT and ARP entry were removed afterward. No connection tracking was cleared.
- The test router's unrelated IPv4 mangle classifiers correctly trigger the conservative conflict preflight for an IPv4 block. The independently instantiated CHR allows the reverse-family packet test without modifying those foreign rules.
- The isolated CHR also exercised overlapping route/block targets in both priority orders: the higher-priority route incremented its mangle counters without incrementing DROP; swapping priorities caused the next new connection to increment DROP. Replays converged to zero operations.
- An Access rule was materialized on the isolated CHR while Routing block filters existed. The routing DROP remained ahead of Access jumps and the Access replay had zero operations. Access ordering now preserves that scoped Routing prefix and fails closed on a non-contiguous prefix rather than moving Routing-owned rules.
- The UI build and automated component tests passed. Interactive visual QA remains unverified because the browser origin approval gate rejected access to the local test endpoint; do not work around that gate.

## Root-review P1 correction

- RouterOS executes `mangle/forward` between prerouting classification and the forward filter. An active `mark-connection` there can overwrite the managed winner mark; a forward jump can reach a custom chain that does the same.
- The preflight now checks forward-chain mark-connection and jump actions for the blocked family. It still permits forward packet QoS, MSS and other actions that do not rewrite connection marks. It rechecks immediately before activation.
- On the disposable RouterOS 7.23.7, an active forward mark rewrite blocked the preview, the apply endpoint rejected it, and no managed block filter was created. A forward jump to a custom mark-writing chain also blocked preview. A forward packet-mark rule and a disabled connection-mark rule remained allowed. After removing the conflicting rules, apply committed; matching IPv6 packets incremented DROP from 0 to 2 while an unmatched destination left it unchanged. All temporary objects were removed.
- A separate audit of the original `main` lockfile found three high-severity dependency findings (Vite, source-map-js and undici). The dependency lock refresh is intentional to pass the required audit, not an incidental install; the updated lock reports zero vulnerabilities.
