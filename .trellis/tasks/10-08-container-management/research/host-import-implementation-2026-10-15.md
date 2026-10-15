# Host import implementation checkpoint, 2026-10-15

Status: implemented and test-machine verified; production delivery and manual
acceptance remain pending. Continues codex/container-management / Draft PR #30.

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
