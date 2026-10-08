# Native container management — phase 1

User approved implementation on 2026-10-08. This phase delivers native Container reads, a complete single-page editor, and a development-only interactive simulation. Production RouterOS mutations are deferred.

## Requirements
- Arcane-inspired scoped appearance, Dockhand-style searchable/sortable container table, Portainer-inspired flat creation/edit form in both Aurora and Compact.
- All eight configuration sections visible; desktop two columns, mobile one. No wizard or configuration tabs.
- Image and manual static network required: dedicated new VETH, existing bridge, IPv4/CIDR and gateway. No bridge creation, DHCP, VLAN or outbound firewall/NAT changes.
- Optional image-derived unique name; missing image tag uses latest. Largest suitable free disk supplies an independent root directory. Optional command, entrypoint, user, workdir, env, mounts, ports and resources preserve inheritance when blank.
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
- Rename published ports to port mappings. Explain router-IP:port to
  container-IP:port and that direct container access permits leaving it empty.
- Maintain current read-only production scope. Simulate upload/mkdir clearly;
  provide optional read-only test-device browsing and independently verify file
  capabilities before a future complete write pipeline. Credentials stay private.

Acceptance includes malformed/oversized/incompatible archives, device isolation,
source selection, navigation/create/select, collisions/traversal rejection, both
UI variants and cancelled work on device switch. Keep the same Draft PR and task.
