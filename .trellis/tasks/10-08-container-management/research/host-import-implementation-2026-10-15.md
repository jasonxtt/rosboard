# Host import implementation checkpoint, 2026-10-15

Status: implemented, test-machine verified and delivered to production; manual
acceptance remains pending. Continues codex/container-management / Draft PR #30.

Implemented the in-process public-registry downloader pinned to
`go-containerregistry v0.22.1`, target-platform selection (amd64/arm64, ARM32
rejected), digest pinning, compressed/config/layer verification, traditional
Docker-save output without extraction, private data-directory staging and quotas.
Jobs capture their admission transport, journal before writes, use verified pinned
SFTP/exclusive namespace/read-back/rename, then the existing native `file` import.
Logical image origin/digest/platform survive updates and restart recovery. Legacy
native journals remain recoverable. Cancellation is limited to preparation before
native writes. Cleanup retains pre-runtime artifacts and uses durable intent,
exact ownership and shared-reference/root/mount checks after successful runtime
(or healthy runtime when configured), or confirmed deletion.

Shared proxy settings serve updates and image downloads in both Maintenance UIs,
including the no-device shell: one URL input, inline test icon, exterior Confirm.
HTTP/HTTPS/SOCKS aliases, URL credentials, masked revision reuse, candidate tests
without saving, explicit direct mode and no automatic direct fallback are covered.
Per-device transfer settings support verified connection/dedicated identities and
ports without granting container-write authority. No optional write probe exists.

Validation:

- Full Go tests/vet, targeted race tests; 97 frontend tests, lint (four existing
  FastTrack key warnings), production frontend build and dual-UI isolation checks.
- Independent RouterOS 7.23.7: Docker Hub Alpine and GHCR multilayer create/import,
  start/update; source tar cleanup followed by stop/start/restart; stopped retention;
  confirmed-delete cleanup; durable logical origin restoration.
- Coordinator process killed during actual import, then restored: exactly one
  container/add, successful read-back recovery and durable cleanup.
- Debian12 LXC 10.0.0.60: real HTTP, HTTPS CONNECT and SOCKS remote-DNS proxy chains
  through registry/auth/blob/CDN; official updater metadata/checksum/package redirect,
  ELF/version checks. Tests did not install an update.
- Docker loads the generated tar; Alpine runtime image UID/GID1000 downloads to a
  mounted writable data directory with mode0600, without an installed Docker CLI.
- Full application deployed to isolated test service dev-host-images-1015. Both UI
  entry routes respond, health/systemd active, all 79 embedded files hash-verified.
  Protected APIs reject unauthenticated requests. Package tests cover authenticated
  proxy/transfer/native contracts. No production credentials/data used in the lab.

Production NAS writability was confirmed outside the restricted sandbox by an
fsynced temporary-file write and removal. The actual CIFS mount is read/write.
Production delivery must still back up consistently before replacing the binary,
verify runtime/contracts/assets, and wait for explicit user manual acceptance.
Do not merge, release or archive this task at this checkpoint.

## Production delivery, awaiting manual acceptance

Delivered `dev-host-images-1015` to 10.0.0.6 on 2026-10-15. The binary records local
implementation checkpoint `fd1038f0236ef76eccc1f6e450fbe13757c75ab6`; its tree is
identical to published commit `ebf888d6e50879a4b7d9c6dd1188b7c00ce6a347` on Draft
PR #30 (GitHub API publication creates its own commit metadata).

Verified consistent NAS backups (service stopped during archive creation):

- `/home/tom/nas/github/rosboard/backups/20261015T033212Z-host-images-gate/`
  preserves the previous `dev-container-gate-1014` deployment, binary/supervisor,
  config, all SQLite/data files, updater state and service unit; archive fully read
  and SHA256 verified before replacement.
- `/home/tom/nas/github/rosboard/backups/20261015T033650Z-host-images-metadata/`
  preserves the intermediate same-code build before correcting its version label.
  An overlapping local build had overwritten the candidate's version metadata;
  a separately named candidate was rebuilt, checked on the test machine and deployed.

The existing timestamped backup retention was applied before each backup (max10).
No rollback backups were stored on production. Final production service/health is
active at `dev-host-images-1015`; all 79 embedded files match local hashes and both
UI entry routes respond. Config, service unit and native job journals are unchanged
from the original backup. A closed read-only RouterOS container-list request passes
for the enabled production device and finds its existing container. New API routes
return the expected 401 without authentication; authenticated contract coverage is
provided by package tests and the isolated Docker application check.

The production device has no saved SSH host fingerprint. New host image jobs will
require the user to independently verify and save it in image-transfer settings;
this delivery did not silently grant key/write authority or change device roles.

Additional non-root Docker application verification passed: empty-volume admin
setup, authenticated proxy save and real connectivity test, restart persistence,
masked credential response and explicit empty/direct save. Test-only proxy service,
private TLS key, downloaded fixtures and temporary Docker app/image were removed;
the isolated test service remains available at 10.0.0.60:8080. Linux amd64/arm64/
arm-v7 builds pass (ARM32 target image download remains explicitly unsupported).

User manual UI/production acceptance and root APPROVED are still pending. Keep
this task in_progress and PR Draft. No merge, release, finish or archive occurred.
