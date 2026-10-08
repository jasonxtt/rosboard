# Design

`internal/containers` owns typed drafts, resolved defaults, normalized snapshots and validation. `internal/routeros` provides closed GET-only container/config/VETH/bridge/disk/env/mount/log readers; no mutation interface is added. API selects exactly one enabled non-archived device from configuration, uses bounded context, and returns safe errors. Missing optional menus yield explicit capability warnings, not fabricated supported state. Empty command overrides are omitted from projected RouterOS fields.

Shared frontend feature contracts and markup are headless with respect to shell/styles. Each UI loads its own container stylesheet through its own page entry. Drafts stay local and reset with device changes. API adapters parse unknown JSON; reads cancel on unmount and jobs poll without overlapping requests. Single-page editor resolves defaults inline; incomplete fields retain data and show errors.

Simulation is a Go test-only HTTP server enabled by `ROSBOARD_CONTAINER_PREVIEW=1`. By default it serves only fake device container APIs to a standalone Vite development HTML entry outside the production entry graph. The same Go resolution and JSON types serve formal and simulated requests, preventing default/validation drift. Fixtures and simulated mutations never enter the runnable binary or frontend production graph. Mock mutations are device scoped, idempotent by request ID, serialized per device, with explicit recovery by read-back and no retry of uncertain creation.

Future writes must add typed allowlisted mutations, durable object ownership, device write gate and read-back reconciliation. Real image update/delete data retention is outside phase 1. Production gate remains unchanged.

## Follow-up: local image archives and storage navigation

The user requested a local image upload option, a small RouterOS Files directory
picker with mkdir. Services use direct container-IP access without port mappings.
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
