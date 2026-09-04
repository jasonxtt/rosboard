# Implementation plan

1. Add regression tests for NAT exclusion, field clearing, and desired-order
   moves; verify each test fails against the current implementation.
2. Remove NAT from the policy desired/managed boundary while preserving stored
   draft compatibility.
3. Implement order-aware diff and move execution using the existing RouterOS
   mutation interface; keep cleanup and dependency ordering deterministic.
4. Make managed-field comparison clear removed fields.
5. Make compact plan presentation complete and add discovery warnings/error
   handling without changing the physical-LAN candidate policy.
6. Run targeted and full Go tests, race tests, vet, frontend lint/build, and
   diff checks. Inspect the final diff for unrelated changes.
7. Perform local runtime/visual checks and report the manual policy-page checks
   required before any deployment. Do not deploy or commit before the user
   approves the local result.

## Execution record

- Implemented the policy reconciliation, discovery, and plan fixes with
  regression coverage; also included the accepted device ordering and policy
  UI updates in the integrated worktree change.
- `go test ./...`, `go vet ./...`, frontend TypeScript checks, lint, build, and
  `git diff --check` passed. Lint retains one pre-existing Vite escape warning.
- Deployed the verified Linux amd64 build to `10.0.0.6` after creating the NAS
  backup at `20260829T034100Z-combined-three-tasks`; service, health, protected
  API responses, and embedded assets were verified.
- User approved the deployed result. The implementation commit is
  `df2be77`.
