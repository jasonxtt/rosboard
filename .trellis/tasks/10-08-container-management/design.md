# Design

`internal/containers` owns typed drafts, resolved defaults, normalized snapshots and validation. `internal/routeros` provides closed GET-only container/config/VETH/bridge/disk/env/mount/log readers; no mutation interface is added. API selects exactly one enabled non-archived device from configuration, uses bounded context, and returns safe errors. Missing optional menus yield explicit capability warnings, not fabricated supported state. Empty command overrides are omitted from projected RouterOS fields.

Shared frontend feature contracts and markup are headless with respect to shell/styles. Each UI loads its own container stylesheet through its own page entry. Drafts stay local and reset with device changes. API adapters parse unknown JSON; reads cancel on unmount and jobs poll without overlapping requests. Single-page editor resolves defaults inline; incomplete fields retain data and show errors.

Simulation is a Go test-only HTTP server enabled by `ROSBOARD_CONTAINER_PREVIEW=1`. By default it serves only fake device container APIs to a standalone Vite development HTML entry outside the production entry graph. The same Go resolution and JSON types serve formal and simulated requests, preventing default/validation drift. Fixtures and simulated mutations never enter the runnable binary or frontend production graph. Mock mutations are device scoped, idempotent by request ID, serialized per device, with explicit recovery by read-back and no retry of uncertain creation.

Future writes must add typed allowlisted mutations, durable object ownership, device write gate and read-back reconciliation. Real image update/delete data retention is outside phase 1. Production gate remains unchanged.

## Real directory-picker follow-up

The latest user attachment explicitly expands directory handling to real RouterOS
mkdir/rename/remove. This exception does not enable container/image operations.
Keep the existing directory route, with a typed action request, separate directory
capability, strict RouterOS path helpers and exact metadata IDs. Directory calls
use the existing MutationClient transport/no-retry policy and the shared device
write gate. Verify effects by metadata read-back, including uncertain outcomes;
never route a RouterOS path into local filesystem I/O.

The input wrapper, picker navigation/operations hook, inline name editor and
folder menu share the existing UI-native CSS. Opening uses the initial input
path, with recoverable root fallback. Inline creation reserves no temporary
RouterOS object until a valid final name is submitted. Rename updates matching
draft paths, while server protection prevents changing disk/system roots and
directories referenced by existing container roots/mounts. Directory reads and
write errors remain local to the picker; outstanding actions cannot be repeated
blindly. Health/resource details are conditionally visible while their local
values survive toggles; disabled resource overrides are omitted from resolution.

Startup behavior/logging/restart remain visible before an independent, collapsed
advanced group for CMD/ENTRYPOINT/user/workdir. Hidden overrides stay in the
parent draft and are submitted unchanged; field errors expand the group. Health
controls retain the existing inheritance/override contract and visible section,
with plain-language explanations of commands inside the container and timing.
Custom examples use actual RouterOS command syntax, without Dockerfile markers.

## Follow-up: local image archives and storage navigation

The user requested a local image upload option, a small RouterOS Files directory
picker with inline mkdir, rename and confirmed recursive deletion. Services use direct container-IP access without port mappings.
Continue on the same branch and Draft PR. New defaults use a per-container `rootfs` directory; mounted
configuration/data directories are separate siblings. Preserve existing paths.

This checkpoint retains the approved read-only production boundary. Directory
listing reads actual RouterOS file metadata (never contents). Archive upload and
mkdir work in the test-only simulator; real upload/mkdir handlers remain disabled
in this phase. Scoped test-device checks verify capability, while the complete
write pipeline and acceptance remain separate. The supplied
credentials are transient and must never appear in task artifacts or commits.

Accept Docker-save single-image `.tar` archives; validate metadata, OS/architecture
and device scope before using RouterOS `file`, mutually exclusive with
`remote-image`. Do not extract uploaded archives. Bound upload and JSON sizes;
keep staging in temporary private storage and remove it after inspection. The
simulator retains metadata only, clearly labels simulated transfer, and does not
claim that bytes reached RouterOS.

The directory picker navigates folders, shows files as read-only context, creates
a single child folder in simulation, and selects either `root-dir` or a mount
source. It is not a general file editor, delete tool, or shell. Existing mount
sources may also name files. Services use container-IP:application-port over
VETH and bridge connectivity. Keep all seven sections visible. Do not read,
generate, adopt or delete NAT rules; existing routes/firewall govern reachability.

Verify malformed/oversized archives, incompatible architecture, archive device
isolation, image-source projection, path traversal, mkdir collisions, directory
navigation and selected paths, abort on device switch, and both UI builds.


## Authorized native lab write phase — 2026-10-11

The user authorized complete Container write testing through the deployed lab
panel against its preconfigured independent RouterOS only. Production RouterOS
access, production deployment and final acceptance remain excluded. This phase
supersedes the earlier lifecycle read-only implementation boundary for devices
explicitly enabled through private server configuration.

- Closed typed native writes cover create/edit/start/stop/restart/update/delete
  and explicit adoption. Default devices remain read-only; public device edits
  cannot enable native writes and connection changes clear the opt-in/pinned key.
- A private durable per-device journal records exact ownership, request replay,
  pending writes and progress. Mutations are never blindly retried; unknown
  results retain the shared device lock and recover by reading first. Startup
  restores unresolved locks before serving policy/Files writes.
- Docker-save uploads use pinned SSH/SFTP, exclusive generated paths and remote
  size/SHA-256 verification. Valid legacy archive internal links are accepted
  without extracting; unsafe/missing links remain rejected. Already verified
  uploads can be selected again without transferring the archive again.
- New containers have dedicated VETH plus bridge port. No bridge settings,
  addresses on existing interfaces, routes, NAT or firewall rules are changed.
  Edits preserve the container root/image. Existing/shared network changes are
  refused; adoption marks only the container, preserving auxiliary ownership.
- Updates import into a new rootfs, stop the old container, start the replacement,
  preserve the old rootfs and remove the old container. Mount sources remain
  unchanged. Removal cleans only journal-linked owned objects after rechecking
  shared references. Failed imports leave their created objects visible for
  explicit cleanup; they are not silently rolled back.
- RouterOS implicitly deletes root-dir on container removal. Preserve a stopped
  rootfs by renaming its `container store` to a unique sibling with `-retained-`
  before removal. Guard exact container identity, ownership, stopped status,
  source metadata and all overlapping root/mount references. Shared/overlapping
  roots block removal. Show the actual retained path; backups accumulate until
  explicitly cleaned by the operator. This behavior was verified on the lab
  version only and must be verified separately for another version.

See `research/native-write-validation-2026-10-11.md` for actual results and
remaining limits. Keep the task active and the same PR Draft; no APPROVED,
production delivery, merge or archival is implied.
