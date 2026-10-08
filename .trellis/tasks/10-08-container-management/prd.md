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
