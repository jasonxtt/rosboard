# Host-side registry download and RouterOS archive import proposal

Status: revised planning, incorporating the user's 2026-10-15 decisions; runnable
implementation has not started. The user selected host-side download and approved
staging under the data directory. The proxy belongs below Version and updates
in Maintenance, shares its transport with application updates, and uses one URL
input with an inline test icon and an external Confirm button. Generated image
files remain until first successful container runtime or confirmed deletion.
This continues the existing Container task, branch, and Draft PR.

## Product behavior and scope

Keep the current registry/local-archive image choices. A confirmed registry
create/update starts one durable asynchronous job: resolve the image/platform,
download and verify it on the application host, produce a single-image
Docker-save-compatible archive, transfer and verify it on the selected RouterOS,
and execute the existing guarded native import with `file`, not `remote-image`.
The browser submits immediately and observes existing progress; closing the
browser does not cancel a job. There is no separate connectivity-check wizard.

New registry jobs do not use RouterOS registry DNS warming or modify RouterOS
DNS, policy address lists, routes, or firewall rules. Container runtime networking
remains the configured VETH/bridge. Local archives keep their explicit original
workflow. Start/stop/edit/delete do not re-download images. The application host
never executes or mounts downloaded images.

## Downloader and archive format

Prefer an in-process Go registry client using a pinned, reviewed
`google/go-containerregistry` version, restricted to its read/remote primitives.
No Docker/Podman daemon, CLI, socket mount, privileged container, or host image
store is required. Do not use the implicit Docker keychain or invoke credential
helper programs. The dependency version ships with rosboard and is tested on the
project's Go toolchain and release targets.

Support Docker Hub and GHCR public images first, plus explicit HTTPS registries
using the same protocol. Parse names rather than concatenate HTTP paths. Bare
references retain the current effective registry-selection behavior using a
closed private RouterOS registry URL read; show the normalized reference before
confirmation. Never read RouterOS registry passwords. Resolve a tag once, persist
the selected manifest digest, then fetch immutable config/layer digests for this
job. Retrying or restarting the same job cannot silently select a newer tag.
Updating a container intentionally resolves its saved original reference again.

Select linux/amd64 or linux/arm64 from the TARGET RouterOS, not the application
host. Ignore attestation entries in multi-platform indexes. Validate both selected
manifest platform and config platform. ARM32 needs an exact CPU variant; never
assume that every RouterOS `arm` device supports arm/v7. Until variants are
reliably identified, fail with a clear unsupported-platform message.

Preserve config, CMD/ENTRYPOINT, environment, user, workdir, labels and health
metadata. Verify manifest/config/compressed-layer sizes and SHA-256, as well as
uncompressed layer DiffIDs. Read-only HTTP GET retries are bounded and handle
429/Retry-After; native writes remain non-retryable until read-back recovery.

Do not assume a library's `tarball.Write` is identical to traditional Docker-save:
the reviewed implementation emits compressed layer members and may include zstd.
Use the registry client to fetch verified blobs, then a narrow streaming archive
adapter emitting the conservative single-image Docker-save form with uncompressed
layer tar members. Never unpack layer members onto the host filesystem. Bound
expansion size and validate the finished tar through the existing InspectArchive.
Unsupported legacy/foreign layer formats fail explicitly rather than being omitted.
Real RouterOS import is a prerequisite to calling this format compatible.

## Shared proxy setting and credentials

Place a `Proxy settings` card directly BELOW `Version and updates` in Settings ->
Maintenance in BOTH UI variants, including the no-device settings shell. Do not
add an Image download tab or separate protocol/host/user/password fields. Render
one URL input; a compact test-icon button sits inside its right edge, and a
Confirm button sits outside the input to its right. Supply an accessible test
name/tooltip and keyboard focus. On mobile preserve this order without overflow.

Accept `http://host:port`, `https://host:port`, `socks://host:port`, `socks5://host:port`
and `socks5h://host:port`; treat `socks://` as an explicit SOCKS5 alias and normalize
it to the same remote-DNS SOCKS5 transport. Empty input + Confirm selects direct
mode. Optional credentials use standard URL userinfo, for example
`http://user:password@proxy.example:7890` or
`socks://user:password@proxy.example:7890`. Reserved characters in username/password
must be percent-encoded; do not guess unescaped separators. Validate scheme,
host/IPv6 literal, port, control characters and userinfo before saving. Reject
unsupported schemes, paths, queries and fragments with safe errors.

The test icon tests the CURRENT edited candidate without saving it; Confirm
atomically saves the URL for future requests. When the input represents a saved,
redacted credential-bearing URL and is unchanged, testing uses the saved server
configuration by revision, never its mask as a password. A changed URL must be
provided afresh; do not copy stored secrets to a newly entered endpoint or persist
a mask. GET returns redacted display data/passwordSet only, never the full secret
URL. No credentials are stored in browser persistence, history, logs or job payloads.
The test uses fixed GitHub and registry/auth endpoints with bounded requests and
reports update and image-registry results separately; Docker Hub's expected 401
challenge is not a connection failure. Testing cannot install or create anything.

One shared, explicitly configured transport factory serves application release
checks, checksum/release downloads, registry authentication, manifests and
redirected blob/CDN downloads. Keep the updater's official repository/HTTPS host
allowlist, redirect limit, checksum verification and rollback behavior unchanged.
Proxy-Authorization is confined to the proxy; it is never forwarded to origins.
Each update check/download or container job owns an immutable configuration
revision/transport. A Confirm action affects new operations without restarting
rosboard or changing a running operation mid-transfer. After process restart, a
changed transport/credential revision pauses a retained job for explicit recovery.

HTTP(S) CONNECT and SOCKS5 forward destination names to the proxy. The proxy's
own hostname may still require application-host DNS. Do not pre-resolve image
hosts through RouterOS or let HTTP_PROXY/NO_PROXY override the explicit setting.
A configured proxy failure is an error, never an automatic direct fallback.
RouterOS REST, SFTP, monitoring and unrelated network clients stay outside this
shared DOWNLOAD transport; normal certificate verification stays enabled.

Docker-packaged rosboard can use the setting for registry downloads and release
metadata checks, but keeps its existing online-self-update-disabled behavior.
The host Docker daemon's `docker pull` of the rosboard application image is an
external operation and does not inherit an application's saved proxy URL.

Persist the shared proxy configuration atomically through cfgMu/private config,
with empty input explicitly clearing it. Keep temporary-directory/quota defaults
server-side and report their effective values through existing diagnostics as
needed; the requested proxy card remains a single-input form.

Private-registry credentials remain distinct and scoped to the exact registry
host/port. First delivery focuses on public Docker Hub/GHCR and explicit HTTPS
public registries; private repositories receive an explicit authentication error.
Do not import host Docker credentials or RouterOS registry secrets implicitly.

## SFTP authority and target location

The current upload client shares the REST account, fixes SSH port 22, and pins a
host key. The managed onboarding account's policies omit SSH. Archive import must
therefore gain explicit PER-DEVICE image-transfer settings: independent SSH
username/password (optional same-account mode only when it works), port default
22, and verified SHA256 host fingerprint. The SSH hostname stays bound to the
selected RouterOS endpoint. Changing device identity clears its transfer authority.

Expose these fields under Device management -> Image transfer, protected by the
same administrator/same-origin authentication. Return passwordSet only. No account
policy changes, provisioning or trust-on-first-use occur automatically. Verify
the SFTP authority on each supported RouterOS version; do not claim RouterOS
provides a per-directory SFTP jail if it does not. Software guards limit
writes to exact generated job paths.

An independent RouterOS 7.23.7 proof confirmed `ssh,read,write,ftp` supports the
whole SFTP sequence: list/mkdir/upload/read-back/rename/remove/rmdir. Without ftp,
the subsystem could not open; without write, mkdir failed; without ssh,
authentication failed. The FTP service remained disabled. Use this verified
custom-group combination, not the default full group; no policy, sensitive,
reboot, API or REST policies are needed for the separate SFTP transfer role.
See `sftp-permission-validation-2026-10-15.md`; do not call it a proof for a
RouterOS version not yet exercised. The control account retains its existing
REST authorization separately.

Required service: `/ip/service` ssh enabled on its configured port (default
TCP/22), with the service's source restriction, user's source restriction and
input firewall allowing the application host. SFTP has no separate service
switch and does not require enabling FTP/TCP21. Container control keeps the
existing www or www-ssl REST service; classic API service ports are not required.
No production service, account or firewall changes occur while planning.

Default remote staging is `<rootfs disk>/rosboard/images/jobs/<job UUID>/image.tar`.
An optional per-device staging disk can be chosen explicitly; do not silently
switch to whichever disk has the most free space. Require a mounted usable disk,
protect built-in/system roots, and keep staging separate from rootfs/mounts. Plan
space for archive + extracted runtime + scratch reserve, and retain the old
runtime during updates. Compressed registry size is not an adequate extraction
space estimate; count bounded expanded layer bytes while constructing the archive.

Transfer first to an exclusive generated partial path, close it, read back
size/SHA-256 using SFTP, and publish the final path only after verification. This
checksum read-back entails another archive-sized transfer and needs its own
visible progress phase. Replace the existing fixed five-minute transfer deadline
with bounded stage and idle-progress timeouts. Record transfer intent and exact
paths before touching the router. Unknown/partial transfer results are read back
before reuse; never overwrite an arbitrary pre-existing path.

## Local staging and Docker deployment

Default local staging is `<effective data_dir>/container-staging/<device hash>/
<job UUID>/`, with private directory/file modes 0700/0600. Store verified compressed
blobs plus the assembled archive, using generated filenames and atomic completed
markers. Validate ownership and avoid symlink traversal when cleaning. No image
layers are extracted into a host rootfs and no image file belongs in Git or the
executable directory.

Binary deployment using `/opt/rosboard/data` therefore stages under
`/opt/rosboard/data/container-staging`. The existing Docker image has working
folder and persistent volume `/var/lib/rosboard`, default data_dir `./data`, so
its default is `/var/lib/rosboard/data/container-staging`. The supplied compose
bind mount `/opt/rosboard-docker:/var/lib/rosboard` makes the HOST location
`/opt/rosboard-docker/data/container-staging`. Any future private-config custom absolute directory must
be writable by rosboard; Docker users must bind-mount that location explicitly.
The Docker image continues to run as UID/GID 1000. A proxy on the host cannot be
addressed as 127.0.0.1 from a bridge-network container; document a reachable LAN
or configured host-gateway address. No Docker socket is needed.

Initial limits: retain the existing 1 GiB finished-archive ceiling, a default
5 GiB staging quota subject to real free space, and at least 256 MiB filesystem
reserve. Enforce actual bytes for compressed and expanded data, including other
jobs and remote retained roots. Reject before or during a bounded download rather
than filling the host disk. Start with one active host download, same-device
native writes serialized by the existing gate; monitor reads remain available.
Do not introduce a long-lived shared image cache in the first version.

## Durable jobs, logical image origin and cleanup

Add preparation/download/assembly/transfer/verification stages to the existing
native journal. Persist exact normalized source, manifest digest, target platform,
config revision, owned local/remote paths and completed step results; credentials
and registry tokens are not journal payloads. Keep the original request/signature
for idempotent replay. The prepared import step replaces only physical image
fields with the verified `file` path. Stage-specific deadlines replace the
current all-work ten-minute execution deadline.

Preserve logical registry origin even though RouterOS exposes a file import.
Bind origin metadata to exact owned container identity and operation lineage,
not an arbitrary matching name/path. List/details show original image, resolved
digest and platform; future updates pull that origin again. Adopted or manually
created archive containers acquire no invented registry origin. Legacy journals
retain their original pending native steps and recover by read-back.

Restart recovery rechecks completed blob digests and reuses verified files.
The first version restarts an incomplete blob from zero, not promised byte-range
resume. Transfer recovery verifies a completed remote archive or safely discards
only an exact owned partial path. Import outcome unknown retains the shared
write lock and archive; recover actual RouterOS state before issuing any native
command. Browser reload cannot repeat container/add.

Cancellation is immediate during host download; transfer cancellation closes SSH
and journals remaining paths for cleanup. Once import has begun, do not promise
atomic cancellation or delete an archive/rootfs being read. Expose recovery and
then the ordinary guarded delete action for confirmed failed created objects.
Downloading/uploading a replacement must not stop the old running container;
activation continues the existing guarded update/root-retention behavior.

Generated-image retention is bound to CONTAINER runtime, not merely a successful
import job. Retain local completed blobs/archive and the ROS archive while the
container is stopped awaiting its first manual start, or has import/start/health
failure awaiting a user configuration correction. There is no 24-hour expiry or
quota eviction of these retained container-associated artifacts. Quota exhaustion
must report the retained occupancy rather than delete this recovery material.

On the first fresh, verified successful runtime, remove generated local download
files and ROS tar/partials that are no longer being read by an import/recovery
operation. If an explicit health check exists, require healthy; otherwise require
running with no import/start failure. Preserve original source/digest/platform
metadata after deleting tar so future updates still work. Verify on the lab that
removing the source tar after runtime success preserves subsequent start/restart.
A manual later stop must not cause another download.

Successful create/update/start/recovery and a bounded background read-back worker
can recognize first successful runtime, including an external start. Do not write
or clean as a side effect of GET snapshots or from stale cached runtime flags.
Remote cleanup takes the same device write gate, skips pending/unknown imports,
and checkpoints exact owned paths/IDs before removing anything. A configured
stopped-but-not-started container must not be mistaken for successful runtime.

Confirmed container deletion MUST clean up its associated generated local and
remote image files even if the container never started. Include cleanup in the
durable delete operation: persist the container-remove result, then finish
associated artifact cleanup without repeating remove on recovery. If deletion
is confirmed but remote cleanup fails, expose `container deleted; image cleanup
pending` and retain the cleanup intent; do not report overall deletion/cleanup
as complete. If an archive has another live/pending consumer, unlink this
container's association and remove the physical file only after the last consumer
releases it. External deletion may be noticed by authoritative read-back, with
absence distinguished from read/auth failures.

This policy covers rosboard-generated download blobs/tars and related partials.
It does not erase mounted application data or the guarded retained runtime roots.
User-uploaded archives keep their explicit metadata and reference safeguards;
auto-generated and user-uploaded provenance cannot be conflated. Only abandoned
pre-container job artifacts without live/recoverable references may be swept.
Orphan cleanup never sweeps whole disks or arbitrary data directories. Keep
cleanup failures visible and retry/reconcile exact cleanup independently of
successful runtime, using the durable intent and read-back.

## Acceptance sequence and open gates

1. Validate registry/HTTP(S)/SOCKS5, remote DNS behavior, complete redirected
   authentication/layer traffic, shared updater release/checksum traffic, URL-userinfo
   percent encoding, candidate test vs Confirm, saved-mask handling, bad credentials,
   no direct fallback, and no secret
   responses/logs. Test multi-platform selection, digest pinning, corruption,
   bounded expansion, no space, cancellation and configuration changes.
2. Validate archive shape against real Docker Hub Alpine plus the user's intended
   image, GHCR, and a multilayer OCI image; include real extraction/start tests.
3. Exercise failures/restarts at download, transfer, import and update activation;
   prove no duplicate container/add and old runtime retention. Verify first-runtime
   cleanup, retention while stopped/failing awaiting configuration changes, deletion
   before first run, cleanup-failure recovery without duplicate remove, and source-tar
   deletion preserving subsequent start/restart. Test both maintenance variants,
   mobile layouts and Docker non-root persistent-volume/proxy addressing.
4. Run Go full build/test/vet and targeted race, frontend test/lint/build and emitted
   asset isolation checks. Inspect the public staged diff and push the same Draft PR.
5. Production delivery requires a usable verified transfer account/fingerprint,
   no active job interruption, the prescribed mounted writable NAS backup, and
   deployed service/health/API/assets checks. Manual user acceptance remains the
   separate final gate before merge/release/archive.

Open before implementation: actual RouterOS archive format compatibility,
source-tar removal after first successful runtime, SFTP authority on additional
RouterOS versions, and ARM32 variants. The independent 7.23.7 SFTP policy matrix
has passed and all permission-test fixtures were removed. Public-image
host-side authentication/manifest/blob download has already passed a read-only
independent-host proof; that proof alone does not accept RouterOS archive import.

## Primary references

- Registry client transport/platform/authentication:
  https://pkg.go.dev/github.com/google/go-containerregistry/pkg/v1/remote
- Reviewed tarball writer (compressed members, not assumed traditional save):
  https://github.com/google/go-containerregistry/blob/v0.22.1/pkg/v1/tarball/write.go
- Project-version Go proxy transport:
  https://github.com/golang/go/blob/go1.26.4/src/net/http/transport.go
- RouterOS architecture-specific archive import:
  https://help.mikrotik.com/docs/spaces/ROS/pages/84901929/Container
- Registry manifest/blob protocol:
  https://distribution.github.io/distribution/spec/api/
