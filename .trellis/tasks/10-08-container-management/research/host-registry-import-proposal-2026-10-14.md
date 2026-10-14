# Host-side registry download and RouterOS archive import proposal

Status: proposed design for user review; runnable implementation has not started.
The user selected host-side download and asked for a complete proposal covering
proxy configuration, temporary storage, and both binary/Docker deployments.
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

## Settings and credentials

Add one global `Image download` section to BOTH settings variants, after collection
settings. It exists even with no selected device; it configures the rosboard
application's downloads rather than a router's policy or a browser preference.
Use shared form/API behavior and UI-specific styling.

Fields: direct / HTTP proxy / HTTPS proxy / SOCKS5; host, port, optional username,
write-only password; temporary-directory location; occupied/free space and quota;
optional bounded proxy test, and cleanup of unused rosboard-generated files.
The proxy test uses fixed registry/auth endpoints and does not create a container.
Advanced exact-host bypass may support a configured private registry; otherwise
proxy failure stops the download and never silently switches to direct mode.

HTTP(S) CONNECT and SOCKS5 forward destination domain names to the proxy. Proxy
host resolution itself may still use the application's resolver. The same
immutable per-job transport applies to registry authentication, manifests and
redirected blob/CDN requests. Do not pre-resolve image domains through RouterOS.
Do not inherit HTTP_PROXY/NO_PROXY in a way that overrides explicit settings.
RouterOS REST, SFTP, monitoring and unrelated network clients remain outside this
proxy. Normal certificate verification stays enabled.

Persist global settings atomically through the existing private configuration
and cfgMu. GET returns passwordSet, never password or a credential-bearing URL;
blank password preserves, and an explicit clear flag deletes. Saved settings take
effect for new jobs without restarting the application. Existing jobs keep an
immutable configuration revision; after process restart, a changed transport or
credential revision pauses the job for recovery rather than silently changing
its network path. Directory changes are rejected while any job still owns files
there. Never log tokens, signed CDN query strings or proxy credentials.

Private-registry credentials are distinct from proxy credentials and scoped to
an exact registry host/port. First delivery may support public registries only,
with explicit authentication errors for private repositories; private credential
management must not be improvised by reading the host Docker config or RouterOS
secrets. If included in the delivery, place write-only credentials in this same
section and validate trusted auth-realm hosts before sending credentials.

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
least privileges for SFTP upload/read/remove on the independent lab; do not claim
RouterOS provides a per-directory SFTP jail if it does not. Software guards limit
writes to exact generated job paths.

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
`/opt/rosboard-docker/data/container-staging`. A custom absolute directory must
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

Successful jobs delete local generated blobs/archive when no stage needs them.
Delete ROS-generated tar only after confirming import is terminal and verifying
on the lab that start/restart and application restart do not need the source tar.
If the router requires it, retain it explicitly and show occupancy; never conceal
this as cleanup success. User-uploaded archives have a different origin/retention
policy and are not automatically swept. Failed generated artifacts expire after
24 hours only when no active/unknown/recoverable job or container requires them.
A cleanup failure is a visible warning and does not rewrite successful import as
failed. Startup and an explicit cleanup button sweep only recorded owned paths,
never whole disks, arbitrary data directories, user mounts or retained runtimes.

## Acceptance sequence and open gates

1. Validate registry/HTTP(S)/SOCKS5, remote DNS behavior, complete redirected
   authentication/layer traffic, bad credentials, no direct fallback, and no secret
   responses/logs. Test multi-platform selection, digest pinning, corruption,
   bounded expansion, no space, cancellation and configuration changes.
2. Validate archive shape against real Docker Hub Alpine plus the user's intended
   image, GHCR, and a multilayer OCI image; include real extraction/start tests.
3. Exercise failures/restarts at download, transfer, import and update activation;
   prove no duplicate container/add and old runtime retention. Verify source-tar
   cleanup does not break subsequent start/restart. Test both settings variants,
   mobile layouts and Docker non-root persistent-volume/proxy addressing.
4. Run Go full build/test/vet and targeted race, frontend test/lint/build and emitted
   asset isolation checks. Inspect the public staged diff and push the same Draft PR.
5. Production delivery requires a usable verified transfer account/fingerprint,
   no active job interruption, the prescribed mounted writable NAS backup, and
   deployed service/health/API/assets checks. Manual user acceptance remains the
   separate final gate before merge/release/archive.

Open before implementation: actual RouterOS archive format compatibility,
source-tar lifetime, least SFTP authority, and ARM32 variants. Public-image
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
