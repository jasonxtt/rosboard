# Combined native UI and configuration import delivery — 2026-10-16

## Authorized scope and build

The user explicitly requested compiling the container-management UI revisions
and configuration imports together and replacing production for manual inspection.
This supersedes the test-only delivery state in the earlier validation records.

The deployed source revision is d350d76528b52b965f7f2fb03c98f913df2c7493 on
codex/compose-import / Draft PR #32. Its ancestry includes the container baseline
92c35f7 from PR #30, Compose import a1d6355, native UI alignment 3aad5fc and Docker
run import 624f27b. The changes after 624f27b are documentation only. No additional
runtime changes or unrelated planning PR were included.

Rebuilt the embedded frontend and static linux-amd64 binary as
**dev-containers-1016**. The frontend build reproduced the checked-in assets with
no diff; emitted bootstrap/CSS isolation checks passed. The implementation's full
Go tests/vet/build and all 108 frontend tests remain passing; lint has only the
four pre-existing FastTrack JSX-key warnings. GitHub docker CI passed at d350d76.

## Same-binary test verification

- Replaced only the isolated disposable test service binary; its configuration
  contains only the independent lab device, with no production credentials/data.
- No unsettled native jobs existed before replacement. Health, both UI entry
  points and hashes of all 78 distributable embedded assets matched the build.
- Unauthenticated parser access returned 401. Authenticated Compose and the exact
  requested ddns-go Docker run example returned the expected fields and skips.
  ROS mount sources remained empty. Container read-back passed and the existing
  MSM remained healthy. The temporary verification session was removed.
- Earlier isolated real MSM and ddns-go create/download/import/start/cleanup/delete
  results remain documented in validation.md and docker-run-validation-2026-10-16.md.

## Consistent production backup and replacement

- Confirmed that the NAS backup location is mounted as writable CIFS. Retention
  removed only the oldest timestamped backup directory before creating the next;
  the resulting retained directory count is 10.
- An initial preflight mistook the updater's stage field for state and stopped
  before service shutdown or replacement. Corrected the local deployment helper
  after confirming stage=succeeded; removed only its empty backup directory.
- Verified no unsettled native/updater job, uploaded and hashed the candidate,
  then stopped the service for a consistent backup. Streamed the old binary,
  supervisor, configuration, complete data directory including SQLite, updater
  state and systemd unit directly to the NAS. No development rollback backup was
  stored on the production host.
- Backup label: 20261016T073718Z-containers-ui-import. The private production.tar
  is 1,686,804,480 bytes; all archive members were fully read before replacement.
  SHA256: d03953961194161258784135730243de3f6df006c0b5b291cb82a4782dc10776.
  Private contents and the manifest remain on the NAS, outside Git.
- Atomically replaced the binary and started the existing systemd service. It is
  active/running, Result=success, ExecMainStatus=0. The installed binary hash
  matches the exact test-verified candidate, and health reports dev-containers-1016.

## Production verification and acceptance state

- Both UI entry points and all 78 distributable asset hashes passed over HTTP.
- The unauthenticated parser boundary returned 401. For both enabled device
  scopes, authenticated Compose/Docker run parsing and native container reads
  passed. The temporary verification session was removed in finally cleanup.
- Configuration, supervisor and service unit match the NAS backup byte for byte.
  No container or RouterOS mutation was requested by deployment verification.
- Manual visual acceptance remains pending. Inspect Aurora and Compact,
  desktop/mobile and light/dark. Create container -> Import configuration opens
  the Compose / Docker run importer. Check the container-management revisions and
  the import preview/application, including skipped host networking and manual
  ROS mount source selection.
- Keep task in_progress and PR Draft. No main merge, release, task completion or
  archive follows from this delivery; fresh user acceptance and root APPROVED
  are still required.
