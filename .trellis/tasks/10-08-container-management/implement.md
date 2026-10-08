# Implementation and validation

- [x] Read-only RouterOS adapter, domain normalization/default resolution, device-scoped API.
- [x] Shared contracts, flat editor/table/detail/logs and capabilities.
- [x] Independent Aurora and Compact navigation/styles.
- [x] Development-only mock endpoints and standalone preview with failure/recovery scenarios.
- [x] Focused domain/API/frontend/mock regression tests.
- [x] gofmt; go build ./...; go test ./...; go vet ./...; targeted race.
- [x] npm --prefix web test; lint; build; check:ui-build; git diff --check.
- [x] Document mapping and manual desktop/mobile theme/device-switch QA.
- [x] Inspect exact staged diff, checkpoint commit/push and one Draft PR.

Rollback: remove this feature branch/stop preview; no RouterOS writes or production replacement occurs. Keep task active for future real-write acceptance.

## Phase 1 verification checkpoint

- `go build ./...`, `go test ./...`, `go vet ./...` passed.
- Race checks passed for `internal/containers`, `internal/api`, `internal/routeros`.
- Frontend: 70 tests passed; lint passed with four pre-existing `fasttrack.test.tsx` key warnings; build and dual-UI checks passed.
- Standalone development preview entry passed a separate TypeScript check.
- Production manifest/JS excludes development preview and fixture devices; lazy container stylesheet graphs are disjoint.
- Current RouterOS 7.23.5: all 13 allowlisted GET menus succeeded. No writes were performed.
- HTTP preview through Vite verified missing-field errors, defaults, env special characters/newlines, read-only mounts, TCP/UDP, download/create/start phases, duplicate suppression, pending-task restoration, recovery and device isolation. Fixtures were reset after verification.
- Manual visual acceptance remains pending: desktop/mobile, both themes/UI variants, long names and large table. Instructions are in `web/dev/README.md`.
- No production deployment, merge, release or complete task archival. Real writes and update/delete data retention still require independent RouterOS verification.

Draft PR: https://github.com/jasonxtt/rosboard/pull/30 (implementation checkpoint `8624091`). Task status stays `in_progress`; manual visual acceptance and future real writes are pending.


## Follow-up execution

- [x] Record local-image and Files-picker scope; keep production reads only.
- [x] Add Docker-save metadata validation and device-scoped archive projection.
- [x] Add typed file metadata reads and safe directory normalization/listing.
- [x] Add shared upload/picker components with isolated styles for both UIs.
- [x] Add simulated upload/mkdir and optional real read-only preview routing.
- [x] Verify format/architecture/isolation, directory paths/collisions, selected
  fields and device-switch cancellation contracts.
- [x] Run required Go/frontend checks; verify Vite proxy integration and scoped
  independent test-device file capabilities; clean temporary RouterOS objects.
- [x] Review public staged paths/diff, commit/push same branch and update Draft PR.
- [ ] User desktop/mobile, light/dark visual review; future complete write gate.


### Follow-up checkpoint results

- Single-image Docker-save tar inspection, device-scoped archive metadata and
  `file` projection implemented; registry and archive sources are exclusive.
- Shared picker and image uploader implemented in both isolated UI styles.
  Real Files directory reads use the typed client. Upload/mkdir remain simulated.
- New rootfs defaults and separate persistent mount guidance implemented;
  existing relative RouterOS paths remain unchanged on edit.
- Frontend 73 tests passed; Go build/test/vet and scoped race checks passed;
  lint/build/dual-UI checks and development preview TypeScript check passed.
  Four existing fasttrack JSX-key lint warnings remain.
- The opt-in independent RouterOS read test passed. REST mkdir/read-back and
  SFTP archive round-trip/hash verification passed; temporary objects cleaned.
  No actual container import/start/update/delete was attempted.
- Vite-proxy runtime verification passed for real read-only snapshot/Files and
  simulated multipart upload, mkdir, projection, archive import/start and reset.
- Manual desktop/mobile/theme visual acceptance remains pending. Full task stays
  active and Draft; production deployment and complete write acceptance remain
  separate.

## Direct-IP access checkpoint

- Removed port-mapping form/list fields, Go/TypeScript contract members and
  parser/default/validation logic. The flat editor now has seven visible sections.
- Removed container NAT reads and NAT ownership creation/editing from simulation.
  No RouterOS configuration or existing firewall/NAT rules were changed.
- Regression checks reject NAT reads and prove stale client port fields cannot
  produce mappings or ownership. Both UIs retain IP/VETH and Bridge columns.
- Go build/test/vet and containers/api/routeros race checks passed. Frontend
  73 tests, lint/build/dual-UI checks passed; four existing fasttrack key warnings
  remain. Trellis context validation and git diff checks passed.
- Restarted the local preview. Vite-proxy smoke checks passed for both simulated
  device snapshots, pure direct-IP resolution, and real test-device read-only
  snapshot/Files. Both preview URLs respond. No real mutations were performed.
- Same branch/Draft PR; manual visual acceptance and real-write acceptance remain
  pending. No production deployment, merge or task completion.
