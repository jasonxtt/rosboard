# Implementation plan

1. Capture the current worktree status and add this task's context entries; do not overwrite the user's existing uncommitted changes.
2. Add backend regression tests for:
   - explicit `LAN` membership overriding route-backed WAN inference;
   - omitted/false route activity;
   - WireGuard and outbound VPN/tunnel interfaces without default routes;
   - dynamic/inbound VPN exclusion;
   - point-to-point gateway behavior.
3. Implement the smallest backend change in `internal/policyv2/discovery.go` and `internal/policyv2/gateway.go`:
   - resolve LAN members;
   - correct RouterOS boolean interpretation for routes;
   - supplement route-backed WANs with fixed VPN/tunnel candidates;
   - use only proven WANs for direct ingress suppression;
   - expand point-to-point type recognition.
4. Add the minimal frontend option-label change in `web/src/features/policy-routing/PolicyWizard.tsx` to expose unproven candidates without changing the selection or save contract.
5. Run targeted Go tests, full Go tests, vet, frontend lint, and frontend build. Inspect the diff for unrelated changes and verify no credentials or device-specific secrets were added.
6. Run local runtime checks proportional to the change. Deployment to `10.0.0.6` is not part of this implementation turn unless separately requested; if deployed later, follow the repository backup/manual-acceptance gate before committing.

## Validation completed

- `go test ./...`
- `go vet ./...`
- `npm run lint` in `web/`
- `npm run build` in `web/`
- `go build -o /tmp/rosboard-policy-discovery-check ./cmd/rosboard`

The build regenerated the embedded frontend assets. Deployment and commit remain pending the repository's remote manual-acceptance gate.
