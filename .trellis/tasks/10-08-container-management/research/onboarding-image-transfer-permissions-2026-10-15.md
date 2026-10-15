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

1. New groups use exact `read,write,test,api,rest-api,ssh,ftp`. All existing-group
   branches use the strict transition table below; they do not unconditionally
   assign or repair a policy. Keep exact-role and disallowed-transition tests.
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
   Recheck exact policy and SFTP READ capabilities after administrator application.
   Capability detection never attempts a write to prove the right works.
5. SSH service/port, source restrictions, reachability and host-key verification
   remain distinct prerequisites. Generated scripts do not open services, change
   firewall rules or silently pin SSH keys. Unavailable transfer is an optional
   capability warning for monitoring-only onboarding, not a blanket device failure.
6. Exercise new generated roles, repeated guarded upgrades, password preservation,
   permission denial, SSH-disabled state, independent devices, credential-source
   switching and host-key/verification invalidation. Preserve read/policy behavior
   and the private per-device container-write gate. Verify on the disposable lab
   before any production delivery; no router-side mutation is part of this audit.

## User hard constraints A/B

A. After the independent ownership/membership/identity guards pass, only the
following existing-group transitions are allowed:

| Fresh effective rights | Result |
| --- | --- |
| read,write,test,api,rest-api (exact-old) | exact-new after explicit admin execution |
| read,write,test,api,rest-api,ssh,ftp (exact-new) | no-op; no group/user/password write |
| any other set, missing/unreadable/invalid policy | reject; preserve original state |

Policy order is irrelevant, but membership must be exact. Parse known disabled
`!` entries correctly; unknown/contradictory data fails closed. Extra rights,
missing baseline rights, partial ssh/ftp, full and custom roles cannot be accepted
by a subset test, stripped or repaired. Recheck the live role inside the generated
script immediately before a write; recheck exact-new afterward. A script issued
before an external edit must not authorize overwriting that edit. Keep optimistic
read-back limits explicit; divergence/unknown outcomes stop normal execution.

B. Capability/verification/preview/GET checks are read-only, including SFTP:
metadata reads, host-key verification, auth, list/stat and read-only access only.
No implicit mkdir/upload/rename/remove or shell/REST mutations. Declared rights
and read connectivity must not be reported as proven write success.

An optional write probe is a SEPARATE explicit administrator action requiring
write-probe authorization, private container_writes, a bound verified identity/key
and the shared device gate. Record intent before writes. Generate an exclusive
random path inside a validated temporary rosboard-owned namespace, reject foreign
paths/collisions and limit cleanup to that recorded object. Any failure/unknown
outcome retains cleanup intent and is not retried by later detection. Actual
explicitly authorized image upload may prove writes without a dummy capability
probe. Previous disposable permission-matrix writes were explicitly authorized
lab experiments; they do not justify product-side implicit probes.

Required regressions: every transition row including added/missing/partial rights,
invalid policy, reordered/disabled tokens, script execution after role drift;
zero mutation calls for all capability paths; denied probes without authorization/
write opt-in; temporary-path containment, exclusive creation, gate serialization,
exact cleanup and cancellation/unknown-result recovery. No source code changes
are asserted by this updated requirement.

Status: planning updated; provisioning code/tests/docs have not yet been changed.
