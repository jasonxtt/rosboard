# Design

`internal/containers` owns typed drafts, resolved defaults, normalized snapshots and validation. `internal/routeros` provides closed GET-only container/config/VETH/bridge/disk/env/mount/NAT/log readers; no mutation interface is added. API selects exactly one enabled non-archived device from configuration, uses bounded context, and returns safe errors. Missing optional menus yield explicit capability warnings, not fabricated supported state. Empty command overrides are omitted from projected RouterOS fields.

Shared frontend feature contracts and markup are headless with respect to shell/styles. Each UI loads its own container stylesheet through its own page entry. Drafts stay local and reset with device changes. API adapters parse unknown JSON; reads cancel on unmount and jobs poll without overlapping requests. Single-page editor resolves defaults inline; incomplete fields retain data and show errors.

Simulation is a Go test-only HTTP server enabled by `ROSBOARD_CONTAINER_PREVIEW=1`. It serves only fake device container APIs to a standalone Vite development HTML entry outside the production entry graph. The same Go resolution and JSON types serve formal and simulated requests, preventing default/validation drift. Fixtures and simulated mutations never enter the runnable binary or frontend production graph. Mock mutations are device scoped, idempotent by request ID, serialized per device, with explicit recovery by read-back and no retry of uncertain creation.

Future writes must add typed allowlisted mutations, durable object ownership, device write gate and read-back reconciliation. Real image update/delete data retention is outside phase 1. Production gate remains unchanged.
