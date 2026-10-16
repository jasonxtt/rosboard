# Onboarding transfer-role implementation — 2026-10-15

Continues the existing container-management workstream, branch and Draft PR #30.
This checkpoint implements the onboarding authority plan; host-side registry
preparation, shared proxy and generated-image lifecycle remain pending.

## Delivered behavior

- Newly generated roles use exactly `read,write,test,api,rest-api,ssh,ftp`.
  Repeated matching new-account scripts contain no user/group set command and
  preserve passwords. Old/foreign/shared collisions are refused rather than
  adopted or repaired. Creation and repeat paths read back role and membership.
- `GET /api/devices/{id}/account-transfer` requires explicit saved generated
  account metadata, matching connection username and fresh user/group IDs,
  router identity, enabled state, ownership marker and exclusive membership.
  Prefix fallback used by older cleanup helpers is not upgrade authorization.
- The administrator script binds the freshly read identities, checks live
  membership/ownership/policy again, upgrades exact-old only, makes exact-new a
  write-free no-op, and rejects every other role. Read-back failure stops with
  an explicit unknown-outcome message. Guards are optimistic, not atomic CAS.
- Go and RouterOS parsers compare effective sets, understand known disabled
  flags, and reject duplicates, contradictions and unknown/invalid tokens.
- Shared account UI in Aurora and Compact generates/displays the script and
  offers fresh read-only verification after administrator execution. Onboarding
  and replacement help explain the extra authority and independent SSH/key/write
  prerequisites. Preview warns without blocking monitoring-only onboarding.
- Default transfer credentials remain the saved connection account. Private
  optional `container_transfer` overrides credentials/key/port as a complete
  source; `container_ssh_port` supports non-default SSH with the connection
  account. Changing connection credentials invalidates transfer/key/write opt-in;
  unrelated saves preserve them. Clearing the account clears that authority.
- SFTP discovery authenticates with the pinned key and reads directory metadata.
  It never calls the uploader or a shell and never reports proven write success.
  There is no optional write-probe endpoint; its temporary-object/cleanup/recovery
  requirements remain applicable if such an action is implemented later.

## Validation

- `go test ./...`, `go build ./...`, `go vet ./...`: passed.
- Race checks for `internal/routeros`, `internal/containers`, `internal/api`: passed.
- Frontend 95 tests, lint, production build and dual-UI graph checks: passed.
  Four pre-existing fasttrack JSX-key warnings remain.
- Added regressions cover exact old/new/reordered/disabled/extra/missing/partial/
  invalid policies, missing/foreign/shared metadata, legacy prefix refusal,
  device and credential-source isolation, key/connection invalidation, auth/list
  denial, disabled SSH and cancellation. Instrumented SFTP server handlers assert
  zero write/shell requests for discovery success and failure.
- Frontend tests verify both control styles, GET-only requests, script-as-text,
  refusal of unproven write claims, and stale-request abortion on device switch.
- Explicit opt-in `TestManagedRoleIndependentLab` passed on independent RouterOS
  7.23.7 at `10.0.2.5`. It executed generated provisioning and upgrade scripts,
  confirmed externally changed passwords survive repeat provisioning and upgrade,
  used the generated account itself for REST metadata and pinned SFTP reads, and
  rejected stale-policy, shared-membership and ownership drift. Actual RouterOS
  parser tests include reordered/disabled/duplicate/contradictory/unknown tokens.
  Temporary generated users/groups were removed. Existing accounts were retained.
- SSH exec treats multi-line input differently from Terminal paste; the opt-in
  harness removes comment lines and flattens the outer block into one command.
  Two early harness fixtures were identified through exact IDs and router logs,
  then removed after checking ownership and exclusive membership.
- RouterOS positive-only policy assignments can retain omitted flag values.
  Lab fixture resets explicitly include disabled flags. Product scripts rely on
  exact live policy guards and read-back, not successful assignment alone.

## Test deployment and remaining acceptance

The existing isolated service on `10.0.0.60:8080` now runs
`dev-onboarding-1015`. Service/health and both UI entry URLs passed. All 75 embedded
asset files match the local build by SHA-256. The new API denies unauthenticated
requests with 401. No production service, configuration or credentials changed.

User visual review: open `http://10.0.0.60:8080/?ui=aurora` and
`http://10.0.0.60:8080/?ui=compact`; inspect connection-account settings and expand
**镜像传输权限与旧账号升级**. Check desktop/mobile widths and light/dark themes.
Generated accounts show the guarded script; the existing manually configured lab
account is correctly refused for an automatic managed-group upgrade. New-device
script help must explain SSH/FTP authority and preserve monitoring-only access.
Only execute an upgrade on the exact router named by its freshly checked account.

No production delivery, root APPROVED, merge or task completion/archive is claimed.
