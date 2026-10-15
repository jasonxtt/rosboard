# Onboarding account image-transfer authority audit — 2026-10-15

## Current source evidence

- `internal/api/provisioning.go`, provisioningScript: both group add/set branches
  assign `read,write,test,api,rest-api` only. SSH and FTP group policies are absent.
- `internal/api/provisioning_test.go`, TestProvisioningScriptContent: asserts that
  baseline and explicitly forbids ssh and ftp. This is an intentional existing
  contract that needs a coordinated change, not an incidental script typo.
- `docs/routeros-access.md` and `docs/security.md` describe the same five rights.
- `internal/routeros/container_upload.go` currently authenticates SFTP using the
  device connection user/password, fixed port 22 and a private pinned host key.
- Generated account metadata records username/group. Existing legacy fallback
  helpers can infer names from prefixes; that inference alone must NOT authorize
  a privileged existing-group upgrade.
- The independent SFTP matrix established ssh/read/write/ftp as a working transfer
  combination on RouterOS 7.23.7, with FTP service disabled. See the separate proof.

Conclusion: the existing generated connection account cannot perform this task's
SFTP image transfer. The user explicitly includes fixing onboarding in this plan.

## Required work within the same task/branch/PR

1. New scripts use exact `read,write,test,api,rest-api,ssh,ftp` in BOTH create and
   existing-group branches, with no policy/sensitive/reboot or unrelated rights.
   Keep explicit tests for the full allowed set and disallowed permissions.
2. Explain the added transfer authority in onboarding preview/help, and update
   access/security docs when the runnable implementation lands. Generated account
   credentials can be reused server-side; a dedicated transfer account remains an
   optional override. Never expose secrets or enable container_writes implicitly.
3. Existing accounts need an administrator-run, identity-bound upgrade script;
   software upgrade alone does not change router-side rights. Preserve username,
   password and device configuration. Do not add policy just to self-upgrade.
4. Require fresh recorded account/group identity and exclusive intended membership.
   Mismatched/shared/foreign or prefix-inferred-only legacy groups cannot be
   automatically upgraded. Reject unsafe names or quote them correctly in scripts.
   Recheck permission and actual SFTP capabilities after the administrator applies.
5. SSH service/port, source restrictions, reachability and host-key verification
   remain distinct prerequisites. Generated scripts do not open services, change
   firewall rules or silently pin SSH keys. Unavailable transfer is an optional
   capability warning for monitoring-only onboarding, not a blanket device failure.
6. Exercise new generated roles, repeated guarded upgrades, password preservation,
   permission denial, SSH-disabled state, independent devices, credential-source
   switching and host-key/verification invalidation. Preserve read/policy behavior
   and the private per-device container-write gate. Verify on the disposable lab
   before any production delivery; no router-side mutation is part of this audit.

Status: planning updated; provisioning code/tests/docs have not yet been changed.
