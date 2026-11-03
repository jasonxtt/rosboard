# Implementation plan

## Before execution
- [x] Obtain approval of the final planning summary and start the task.
- [x] Resolve an isolated branch based on main; preserve unrelated work and use Draft PR #34.
- [x] Load backend/frontend specs and implementation/check helpers. Validate routing ownership, ordering and FastTrack; reject unsupported untracked/classifier cases before apply.

## Ordered work
1. Add meaningful desired-state regressions for disabled/missing family blocking, source scopes, cross-egress priority, IP and DNS-derived targets, router output and lifecycle.
2. Implement action-aware family/target/mark projection and routing-owned filters. Extend domain selection, scan, preview, lifecycle cleanup and ordering without access-domain contamination.
3. Guarantee new-connection enforcement, including FastTrack and untracked traffic. Existing connections may expire naturally; do not clean conntrack.
4. Update canonical/compact family-toggle wording and preview warnings; explain that DNS AAAA remains unchanged and a disabled policy does not block.
5. Add reconciliation/API/UI tests for idempotence, transitions, ownership and review visibility. Update policy-routing spec in the same workstream.
6. Verify on disposable test ROS: A/AAAA answers; IPv4 success; IPv6 block counters; no WAN leak; unmatched traffic; router output; priority overlap; new connections with FastTrack present; enable/disable/delete and no-op replay.

## Validation
- Targeted tests in internal/policyv2, internal/api, internal/routeros and internal/store as affected.
- go test ./...
- go vet ./...
- Targeted race tests for changed reconciliation/apply state.
- web: npm run test; npm run lint; npm run build; npm run check:ui-build; dependency audit per current project requirements.
- Inspect complete scoped diff and staged paths for secrets/unrelated files before checkpoint commit/push. Keep one Draft PR; no merge without APPROVED.
- Production deployment only after automated and test-machine checks, NAS rollback backup, then health/API/assets verification and explicit manual acceptance. Do not archive while awaiting acceptance.

## Rollback checkpoints
Do not apply if preview cannot establish exact source/target scope or enforceable ordering. Keep failed apply visible and recover by a fresh scan. Preserve external rules and untouched connection state. Rollback removing blocks must warn that prior main-route bypass behavior returns.
