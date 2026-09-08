# Target-list deletion finalization implementation plan

## 1. Baseline and test setup

- [x] Inspect the current target-list store/API tests and run the focused baseline.
- [x] Add a failing store regression for an applied, active-version, unreferenced
  target and verify the existing code leaves a tombstone.
- [x] Add an API regression through `/api/target-lists/{id}` using the test policy
  manager and device store.

## 2. Implement the smallest store change

- [x] Update `PolicyRepository.DeleteSource` to physically delete only when its
  pre-delete consumer domains are empty; leave the legacy domain tombstone
  branch intact.
- [x] Preserve the transaction, revision checks, preset protection, and reference
  errors.
- [x] Run `gofmt` on changed Go files.

## 3. Automated verification

- [x] Run targeted `go test` packages/tests for target-list deletion and policy
  domain apply behavior.
- [x] Run `go test ./...`, `go vet ./...`, `go build ./...`, and `git diff --check`.
- [x] Run `go test -race ./internal/store ./internal/api`; frontend lint passed
  with one pre-existing `no-useless-escape` warning in `vite.config.ts`.
- [x] Confirm the diff contains only the fix, focused regressions, and this task's
  planning artifacts; do not touch the user's unrelated worktree changes.

## 4. Test-machine delivery

- [x] Build the production-shaped Linux AMD64 binary with the repository's existing
  build process.
- [x] Before replacing anything on `10.0.0.60`, inspect its service/config/data
  paths and preserve its test-only scope; no production credentials or data may
  be used.
- [x] Deploy the binary to the test service, restart it, and verify systemd status,
  `/api/health`, target-list API response, and the embedded JS/CSS asset URLs.
- [x] If the test database contains a stuck unreferenced tombstone, retry its
  delete through the API and verify it disappears; otherwise create and delete
  a disposable custom list through the test API.

Runtime result: the existing unreferenced `pending_delete` fixture on the
test device was deleted through the authenticated API. The deployed binary
hash matched the local build, systemd stayed active, health returned `{"ok":true}`,
the target-list API returned the expected authenticated data, and the embedded
target-library JS plus index JS/CSS returned HTTP 200. The disposable test
administrator password was reset only to obtain an authenticated session for
this check; the temporary session file was removed afterward.

## 5. Finish gate

- Report the code/tests and `10.0.0.60` runtime result.
- Do not deploy to `10.0.0.6` or create a work commit in this turn; production
  deployment and the post-acceptance commit remain separate gates.

## 6. Root-review follow-up

- [x] Remove the generic automatic `policy plan is stale` retry from the
  manager; keep only the explicit shared-target follow-up apply path.
- [x] Preserve trusted auto-follow member resolutions in the access proposal
  overlay when the normalized identity anchor is unchanged.
- [x] Add the Access proposal enabled→disabled→enabled integration regression
  through exact `GeneratePlanWithOptions` / `ApplyPlanWithHash` approval.
- [x] Add Access status regressions for queued/staging/verifying, unapplied
  desired revisions, and committed equal revisions.
- [x] Record the shared TargetList activation semantics and inline selector
  behavior in the current task and backend spec.
- [x] Re-run all backend/frontend checks and deploy this final root-review
  build to `10.0.0.60`; do not touch production or create a commit.

## 7. P0 cross-manager ownership correction

- [x] Add the small shared ownership identity helper and switch policy-v2 and
  Access Control managed comments to the scoped `rbs_<scope8>_<object8>` format;
  retain old `rb_<scope8>_<object8>` and `rb_<8>` only for conservative migration.
- [x] Make foreign scoped objects and unknown unscoped legacy objects
  non-actionable; keep exact current scoped legacy migrations and narrow stale
  cleanup. Old unscoped Access objects are ambiguous and are not auto-adopted.
- [x] Add dual-manager/device isolation, coexistence/idempotency, and
  conservative legacy-migration regressions.
- [x] Add the committed Access terminal-refresh zero-operation regression.
- [x] Deploy only this correction to `10.0.0.60`, inspect the live RouterOS
  object/log behavior for at least three scheduler ticks, and wait for root
  reviewer acceptance before any commit or production action.

Runtime result: final Linux AMD64 binary `dde91b45e65a45b2aaa3efec7c576fe87586cdf32d7c7f5c1a1b0ed77966186a`
is active on the test service and `/api/health` returns `{"ok":true}`. After
the committed Access edit, device state stayed at desired/applied `55/55`
for Access and `48/48` for policy across three scheduler observations. The
RouterOS graph stayed at 709 current Access DNS static entries, one scoped
DNS forwarder, one scoped address list, and 20 total firewall filters. The
sample DNS static `.id` remained `*1D180` with the same scoped comment and
`disabled=false`; no capability-probe, bulk add/remove/change, activation, or
error log entries appeared after the final restart. No production action or
commit has been taken; root-review acceptance remains the next gate.

## 8. P1 ownership-proof correction

- [x] Make only the current scoped `rbs_<scope8>_<object8>` physical namespace
  eligible to prove commentless Access ownership.
- [x] Keep `rb_ac_`, `rbac_`, `rosboard_access_`, readable Access labels, and
  other legacy prefixes as post-ownership domain hints only.
- [x] Keep foreign scoped `rbs` objects visible but non-actionable, and keep
  old unscoped `rb_<8>` objects ambiguous without automatic adoption.
- [x] Add regressions for foreign legacy prefixes, label-only objects, current
  scoped ownership, foreign scoped ownership, and unscoped legacy ambiguity.
- [x] Re-run the full backend/frontend verification and Trellis task validation.
- [x] Deploy only this P1 correction to `10.0.0.60`; perform a real Access
  edit/enable and observe three scheduler ticks.

Runtime result: final Linux AMD64 binary
`7441a5d82605eb1185ab19fffa10994ea8100dcdeec7ea231526e7805aef487d` is active
on `rosboard-test.service` and `/api/health` returns `{"ok":true}`. The real
Access edit completed with `访问规则已保存并应用。`, and enabling it completed
with `规则已启用。`. At each of the three scheduler observations, Access and
policy were `58/58` and `48/48`, the same committed job revision was retained,
and the RouterOS graph stayed at 711 DNS static entries (709 Access entries),
one scoped DNS forwarder, one scoped address list, and 20 firewall filters.
The sample DNS static entry remained `.id=*1D445`, with the same scoped comment,
`disabled=false`, and the expected scoped `forward-to`. The RouterOS log had
zero entries after the interactive apply cutoff and no capability-probe or
mutation counts. No production action or commit has been taken.

## 9. Finish gate

- Root-review acceptance is still required before creating the work commit or
  taking any production action.
