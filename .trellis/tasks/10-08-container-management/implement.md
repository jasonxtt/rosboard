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

## Runtime directory terminology checkpoint

- Renamed the shared `root-dir` field and picker to container runtime directory.
  Storage guidance explicitly allows one application parent with separate runtime
  and persistent subdirectories, e.g. `nginx/rootdir` and `nginx/data/config`.
- Updated the existing validation message to use the same terminology. Directory
  validation and path preservation behavior are unchanged; no new mutations.
- Frontend 73 tests, lint/build/dual-UI checks, Go build/test/vet and Trellis
  context/diff checks passed. Four existing fasttrack JSX-key warnings remain.
- Vite-proxy resolution accepted the shared-parent example and preserved both
  paths; a mount source inside the runtime directory remained invalid with the
  updated message. Vite serves the new label/guidance. Preview restarted.
- Same task branch and Draft PR; production deployment and real-write acceptance
  remain pending.

## Form interactions and full-panel test deployment checkpoint

- Optional IPv6/MAC fields start collapsed under the network advanced arrow.
  Values survive collapse; validation errors expand their fields. The other
  configuration sections remain visible on one page.
- Runtime-directory input opens Files directly below it on click or ArrowDown,
  supports direct typing, and retains typed paths when the picker closes.
  Environment rows use a right-hand accessible trash icon; other row values
  remain intact after deletion. Both UI styles contain the same scoped layout.
- Frontend 76 tests, lint/build/dual-UI checks, Go build/test/vet, Trellis context
  validation and diff checks passed. The four existing fasttrack key warnings
  remain. Previous scoped race checks cover unchanged backend concurrency.
- The complete embedded program runs in a separate `rosboard-container-test`
  service on the disposable test machine, port 8080. Its private config and
  fresh SQLite data are under `/opt/rosboard-container-test`; the existing test
  service on port 80 is preserved. Only the independent test RouterOS is used.
- Runtime checks passed for administrator setup/login, supervised setup restart,
  device isolation, actual read-only container snapshot and Files directory
  listings, effective defaults and validation. Container actions, upload and
  mkdir requests return `container_read_only` without enabling real writes.
- Both full UI entries respond; nine embedded HTML/JS/CSS assets match the local
  build byte-for-byte. Existing second-level entries are under
  **Host settings → Container management** in both shells. Simulation fixtures
  remain excluded from the deployed program.
- User desktop/mobile and light/dark review of the full-panel deployment is
  pending. The task and PR stay active/Draft; no production deployment, merge or
  complete container-write acceptance occurs at this checkpoint.

## Startup advanced settings and health-check clarity checkpoint

- Start-after-create, start-on-boot, logging and restart policy now precede a
  default-collapsed startup advanced toggle. CMD/ENTRYPOINT/user/workdir values
  survive collapse/reopen and submit, including existing overrides.
- Health-check labels and hints explain application response probing, image
  inheritance and images without a check, custom commands inside the container,
  interval/timeout, consecutive failures and startup preparation. The command
  example uses RouterOS syntax without Dockerfile's `CMD-SHELL` marker.
- Go changes are messages only: validation and default-resolution wording now
  match the UI. The health inheritance/override projection contract is unchanged.
  Stop/restart/notification settings are not implicitly enabled.
- Frontend 78 tests, lint/build/dual-UI checks, Go build/test/vet, Trellis context
  and diff checks passed; four existing fasttrack key warnings remain.
- Updated the isolated full-panel test service and verified login, actual
  snapshot/Files, default/inherited/custom health projection, all four startup
  overrides, write denial, and both UI entries/embedded assets. No real container
  operations or production delivery were performed.
- Same task branch and Draft PR. User desktop/mobile/theme visual review and
  future real-write acceptance remain pending.
