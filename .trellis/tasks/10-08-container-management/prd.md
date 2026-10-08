# Native container management — phase 1

User approved implementation on 2026-10-08. This phase delivers native Container reads, a complete single-page editor, and a development-only interactive simulation. Production RouterOS mutations are deferred.

## Requirements
- Arcane-inspired scoped appearance, Dockhand-style searchable/sortable container table, Portainer-inspired flat creation/edit form in both Aurora and Compact.
- All seven configuration sections visible; desktop two columns, mobile one. No wizard or configuration tabs. Optional IPv6/MAC and startup command/entrypoint/user/workdir start collapsed under separate advanced toggles, retaining values and expanding validation errors. Startup/log/restart controls appear before advanced overrides.
- Image and manual static network required: dedicated new VETH, existing bridge, IPv4/CIDR and gateway. No bridge creation, DHCP, VLAN or outbound firewall/NAT changes.
- Optional image-derived unique name; missing image tag uses latest. Largest suitable free disk supplies an independent root directory. Optional command, entrypoint, user, workdir, env, mounts and resources preserve inheritance when blank.
- New defaults: start immediately and at boot, logging enabled, restart policy no, healthcheck inheritance. Editing preserves every existing parameter.
- List/detail/options/log/default-resolution APIs isolated by explicit device. Formal service performs GET-only RouterOS reads and rejects all action endpoints.
- Simulation shares contracts, covers create/edit/start/stop/restart/update/delete/adopt, tasks and progress, failure/deduplication/unknown-result recovery and shared-object retention. Fixtures excluded from production bundles.
- Show capability limits, provenance, VETH sharing and effective defaults. Adopt explicitly; shared objects are never implicitly owned/deleted.

## Acceptance
Go build/test/vet and relevant race checks; frontend test/lint/build and dual-UI build isolation. Provide a local interactive simulation and desktop/mobile light/dark manual review steps. Keep branch and PR Draft; no production deployment, merge or full task archival before acceptance. Real writes require independent RouterOS validation later.


## Follow-up requirements: local images and Files navigation

- Let users choose a registry image or upload one Linux Docker-save `.tar`
  image, with clear format/architecture/size errors and retained form values.
- Add a small Files picker for rootfs and mount sources: navigate actual folders,
  create a child folder in simulation, and select it immediately. Show existing
  files as context and allow mount sources to select a file. No general editor,
  delete tool or shell is requested.
- Keep new rootfs separate from persistent configuration/data under a common
  per-container parent. Preserve every existing container path on edit.
- Label `root-dir` as the container runtime directory. Explicitly allow a shared
  application parent, e.g. `nginx/rootdir` and `nginx/data/config`; persistent
  mount sources remain outside the runtime directory. Folder names are not fixed.
- Access services through container-IP:application-port. Remove the port-mapping
  section and list column. No port mappings or NAT objects enter the container
  contract, reads, simulation, adoption or deletion.
- Maintain current read-only production scope. Simulate upload/mkdir clearly;
  provide optional read-only test-device browsing and independently verify file
  capabilities before a future complete write pipeline. Credentials stay private.

Acceptance includes malformed/oversized/incompatible archives, device isolation,
source selection, navigation/create/select, collisions/traversal rejection, both
UI variants and cancelled work on device switch. Keep the same Draft PR and task.

## Full-panel test deployment follow-up

- Runtime-directory input opens inline Files on click or ArrowDown and remains
  directly editable. Remove the separate runtime browse button.
- Environment delete is an accessible trash icon in the row right column.
- Keep native second-level menu entries in both full UI shells.
- Deploy the complete program on the disposable rosboard-test machine with an
  isolated config/data directory and the independent test RouterOS. Preserve the
  current real read-only container capability; simulations stay out of this build.
- Verify service/health, authentication, device scope, real snapshot/Files, draft
  resolution, write denial and the actual embedded UI assets. Production remains
  separate; no production backup/deployment, merge or complete task acceptance.

## Startup and health-check clarity follow-up

- Put create/start-on-boot, logging and restart policy before the startup advanced
  arrow. Default-collapse CMD, ENTRYPOINT, user and workdir, retaining existing
  and edited values across toggles and submission.
- Explain health checks as periodic commands inside the container that test
  whether its application responds. Make image/default inheritance, missing
  image checks, custom command dependencies, interval/timeout/failure count and
  startup preparation understandable without Docker terminology.
- Preserve health contracts and default inheritance. Do not enable real writes
  or add implicit stop/restart/notification actions.
- Update the same isolated full-panel test deployment and Draft PR.
