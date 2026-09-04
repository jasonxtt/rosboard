# Implementation Plan

1. Inspect the current branch, policyv2 discovery code, ownership identity
   helpers, existing discovery tests, and the test-machine RouterOS state.
2. Reproduce the candidate classification with a focused local regression
   fixture before changing behavior. Verify the fixture represents the actual
   policy-generated custom-table route topology observed on the test router.
3. Implement the smallest scanner-only correction: read route comments,
   preserve all routes in discovery, and exclude only rosboard-owned policy
   routes from WAN classification. Update adjacent tests and run `gofmt`.
4. Run targeted discovery tests, API tests, the full Go test/vet/build gates,
   frontend lint/build/audit, `git diff --check`, and Trellis validation.
5. Inspect exact staged paths and diff for secrets or unrelated files; create a
   focused commit and push `feat/policy-access-rebuild`.
6. Send the complete pushed HEAD to the project reviewer and wait for explicit
   root review. If changes are required, apply only the review instructions,
   repeat validation, and push another focused checkpoint.
7. After `APPROVED`, build/deploy to `10.0.0.60`, verify service/API/assets, and
   record the result. Leave production and PR merge untouched pending user
   acceptance.

## Validation commands

```bash
go test -count=1 ./internal/policyv2/...
go test -count=1 ./internal/api/...
go test ./...
go vet ./...
go build ./...
npm --prefix web run lint
npm --prefix web run build
npm --prefix web audit --audit-level=high
git diff --check
python3 ./.trellis/scripts/task.py validate 09-05-routing-ingress-rediscovery
```

## Implementation checkpoint

- Added route-comment reads for IPv4 and IPv6 default-route discovery.
- Preserved route data and marked rosboard-owned routes internally so only
  those routes are excluded from physical WAN evidence.
- Added before/after coverage for the managed LAN policy route, an unmanaged
  private-address WAN, and a managed aggregate that includes a user LAN list.
- Targeted policy discovery tests, package tests, API tests, full Go tests,
  vet, build, frontend lint/build/audit, diff checks, and Trellis validation
  pass. Frontend lint retains the existing `vite.config.ts` unnecessary
  escape warning.
