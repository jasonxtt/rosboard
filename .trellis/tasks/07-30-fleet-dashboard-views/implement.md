# Fleet dashboard implementation plan

1. Define and test the service-owned fleet projection from cached monitor
   snapshots, including enabled/archived filtering, state/count classification,
   stale/unavailable entries, and no-monitor safety.
   - Verify with targeted Go unit tests.
2. Add the authenticated `GET /api/fleet-overview` route and API tests for
   response shape, authentication, and cache-only behavior.
   - Verify with targeted `internal/api` tests.
3. Add frontend types, persisted list/icon preference, fleet polling, and the
   sidebar/navigation split between fleet dashboard and existing device
   overview.
   - Verify production TypeScript build and lint.
4. Implement the list view first: summary tiles, controls, row ordering,
   unavailable state, selection handoff, responsive layout.
   - Verify lint/build and manual desktop/mobile review after deployment.
5. Implement icon view over the same filtered projection and controls.
   - Verify view-switch persistence, equivalent visible data, and responsive
   layout after deployment.
6. Run the complete automated suite, build embedded frontend assets, deploy to
   `10.0.0.6` with timestamped binary/config/SQLite backups, and verify the
   remote systemd service, health endpoint, fleet API, device API contracts,
   and embedded assets.
7. Ask the user to inspect the deployed list and icon views. Do not commit or
   archive the task until the user explicitly approves manual acceptance.
