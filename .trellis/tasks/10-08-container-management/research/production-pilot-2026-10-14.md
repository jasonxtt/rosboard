# Production pilot delivery — 2026-10-14

The user authorized replacing production rosboard for manual container creation
testing. No production RouterOS mutations were performed by the assistant.

## Directory write gate

Files POST now requires the same private per-device `container_writes` opt-in
as lifecycle/image writes. Service admission independently rejects all actions,
including recover, before any RouterOS I/O. Cached snapshots recompute the
directory capability from current opt-in. Regression tests cover API/service
denial, cached capability changes, and opt-in CRUD/replay/device isolation.

## Validation

- Go full tests, build and vet passed; containers/api/routeros race passed.
- Frontend 90 tests, lint and emitted dual-UI checks passed; four existing
  fasttrack JSX-key lint warnings remain. Existing embedded assets are unchanged.
- Production read-only snapshot/Files integration passed on ROS 7.23.5
  long-term x86_64. No write requests were used to test account permissions.
- The disposable test service was replaced first; health and actual panel
  loading passed with `dev-container-gate-1014`.

## Production delivery

A writable local SMB NAS mount was verified before backup. The production
service was stopped for a consistent archive of binary, retained supervisor,
configuration, the complete data directory (including every device SQLite
database), updater state and systemd unit. The archive was streamed directly
to NAS, completely read and hashed before replacement. Ten timestamped backups
remain after removing only the oldest directory. No new rollback backups were
stored on production. Private backups and credentials are outside Git.

The executable was replaced, retaining the supervisor and all data. Only the
primary production device gained `container_writes: true`; configuration
comparison confirmed no other semantic change. The existing REST account has
read/write authority. Its SSH permission remains disabled, so registry-image
creation is available but local archive upload is not enabled by this delivery.
No RouterOS account permissions were modified.

Verification passed for systemd active/running, health version, binary hash,
79 embedded asset hashes, unauthenticated API denial, an existing authenticated
Aurora session, actual container listing, creation form/default resolution and
Files browsing. The existing container identity/configuration/runtime state
matched the before-deployment read-only snapshot. No create/update/delete was
submitted on production. The production list is open for user testing.

## Acceptance boundary

This is an authorized pilot delivery awaiting user manual acceptance; it is
not APPROVED and the PR remains Draft. Destructive Files restart recovery,
disk exhaustion, router reboot during import/cutover and complete retained
rootfs restoration remain unaccepted. No merge, release or task archival.
