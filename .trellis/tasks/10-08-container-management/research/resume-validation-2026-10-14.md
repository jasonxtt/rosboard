# Container validation resumed — 2026-10-14

## Scope

The user requested continuation of the existing container workstream, prioritizing
remaining validation and fixes. Restored `codex/container-management` at PR #30
HEAD `e3b30ab`, kept the same active task and Draft PR, and used only the existing
independent lab service and RouterOS 7.23.7 x86_64. The user logged into the test
panel for browser verification. Production was not accessed or deployed.

## Defects fixed

1. Native edit with blank memory overrides sent empty integer values. The typed
   adapter converted those to unlimited, despite the editor promising current
   global defaults. A live 128M/256M test reproduced kernel values `max`/`max`.
   Native edit now projects the fresh global defaults independently for high/max,
   falling back to unlimited only for absent/empty globals. The final build
   verified 192M/384M as kernel values 201326592/402653184, with HTTP 200 after
   automatic restart. Original global settings were restored and read back.
2. Native edit sent blank cmd/entrypoint/user/workdir strings. In particular,
   blank CMD/ENTRYPOINT removed the usable image startup command. An unchanged
   inherited value is now omitted; existing overrides are cleared using the
   closed container/unset command. Field absence confirms the result. The live
   panel cleared all four overrides, restored Nginx's image defaults, and
   returned HTTP 200. A stopped disposable fixture was explicitly repaired
   during diagnosis; original containers were not edited.
3. The shared table/logs displayed numeric byte counts without units. Numeric
   telemetry now uses the shared formatter; readable unit strings and unavailable
   values retain their meaning. Progress action names are localized.
4. Aurora's device name and action controls widened the document to 434px at a
   390px viewport. The mobile topnav now wraps. Final light/dark screenshots and
   DOM bounds showed no document overflow; desktop retained its original row.
5. Task metadata incorrectly described native writes/uploads as simulation-only.
   Updated it to reflect private per-device opt-in and distinct native/Files
   durability boundaries.

## Automated evidence

- Go 1.26.4: full tests, vet and static build passed after the final changes.
- Race: containers, routeros and api passed with the final native unset changes.
- Native regressions cover finite/unlimited/missing global defaults, mixed and
  explicit memory overrides, automatic restart, four default-restoring unsets,
  uncertain acknowledgements without retransmission, and exact request replay.
- Typed transport tests cover the four allowlisted unset fields and reject
  foreign menus, unrelated properties, invalid values and extra fields.
- Node 24.19.0: 90 frontend tests passed; lint exited zero with only four existing
  fasttrack JSX-key warnings. Production build, emitted dual bootstrap/CSS graph
  checks, container CSS isolation and simulation exclusion passed.
- Trellis context validation and diff checks passed.

The new Linux environment required temporary Go/C compiler/npm setup. Initial
frontend setup hit incomplete binary downloads; a clean cached npm ci completed
before the final standard tests/lint/build. Tooling and credentials stayed out
of the repository.

## Runtime evidence

- Existing isolated test service remained authenticated and device scoped.
  Final version: dev-container-validation-1014-r3. All 78 embedded files matched
  the local build byte-for-byte; private config/data and supervisor were retained.
- Both UI entries loaded. Aurora desktop/mobile and light/dark list layouts were
  inspected. Compact mobile light/dark list and seven-section form were checked.
  Native create/recovery used Compact; resource/startup edits used Aurora.
  Browser checks are engineering evidence, not the user's final visual approval.
- Docker Hub produced an SSL failure and its RouterOS fallback timed out. GHCR
  responded normally. Used the [official Nginx package](https://github.com/nginx/docker-nginx-unprivileged/pkgs/container/nginx-unprivileged)
  at ghcr.io/nginx/nginx-unprivileged:stable-alpine on a dedicated disposable
  fixture with its own network/root. Registry download, extraction, native start
  and direct container HTTP 200 passed. The slow lab download took about eleven
  minutes and was not reported as complete before extraction finished.
- Restarted the test service during registry extraction. The durable journal
  restored the pending task and device lock, denied other writes, and resumed
  by explicit read-back without adding another container.
- Startup argument experiments produced real application exit logs, retained
  the unknown-task lock, and were explicitly corrected/read back. A single-arg
  sleep override was used for the final default-restoration test. Raw displayed
  image command text must not be assumed to be shell argument syntax.
- Global finite limits were changed only temporarily and restored in a finally
  path. Existing global changes do not dynamically rewrite an existing kernel
  cgroup; inheritance is applied at creation/edit time.
- The original Nginx continued returning HTTP 200. The pre-existing failed
  registry fixture and all its data were preserved.

Registry update/cutover passed through Aurora. The original service returned
HTTP 200 during the slow download. The import wait reached its conservative
unknown-result threshold; explicit recovery continued the same task without
adding another replacement. After extraction, the new container replaced the
old ID and returned HTTP 200. The old runtime was renamed and a seeded marker's
exact contents survived actual removal of the old container.

Deleted only the disposable replacement through the panel. Its new runtime was
also renamed and its separately seeded marker survived actual container removal.
The fixture and its dedicated VETH/bridge port are absent; the two original
containers remain, and the original Nginx still returns HTTP 200. Both retained
runtime directories remain on the lab for inspection, without automatic purging.
Global high/max were verified back at unlimited/unlimited after cleanup.

## Remaining acceptance boundary

Files mutation replay records remain in memory; destructive Files restart
recovery is not accepted. Other RouterOS versions/architectures, disk exhaustion,
router reboot during import/cutover, and full restoration from retained runtime
storage remain outside this checkpoint. No production deployment, APPROVED,
merge, release, or task archival is implied.
