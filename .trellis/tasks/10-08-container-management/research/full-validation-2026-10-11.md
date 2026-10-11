# Container phase validation — 2026-10-11

## Result and boundary

The current phase's automated checks and deployed lab API verification passed.
No new product defect was found. This result covers native reads, draft
resolution, real Files directory operations and test-only lifecycle simulation.
It does not approve a production lifecycle writer or a production deployment.

The source checkpoint before this validation was `1e275c0`; the deployed lab
program remains `fac9de4`, with the same runnable source and embedded assets.
Later intervening commits were research documentation only. This checkpoint
adds two frontend regression cases, without changing runnable code or assets.
No production build, service, configuration or data was replaced.

## Automated evidence

| Check | Result |
| --- | --- |
| `go build ./...` | Passed |
| `go test ./...` | Passed |
| `go vet ./...` | Passed |
| `go test -race ./internal/containers ./internal/routeros ./internal/api` | Passed |
| `npm --prefix web test` | 90 passed, zero failures/skips; 28 container-file cases |
| `npm --prefix web run lint` | Exit zero; four existing jsx-key warnings in unrelated fasttrack tests |
| `npm --prefix web run build` | Passed |
| `npm --prefix web run check:ui-build` | Both runtime CSS graphs passed; preview fixtures excluded |
| `git diff --check` | Passed |

The added cases exercise the shared page through both Aurora and Compact
entries with 80 containers. They verify the page limit and final-page controls,
page reset after numeric CPU/memory sort, search by IP, an empty result, status
filtering combined with sorting, and retention of the full long image reference.

Existing tests cover flat form sections, required errors and retained values,
shared VETH locks, device-switch cancellation, hidden advanced fields, resource
and health inheritance, local archive validation/projection, directory picker
keyboard/inline behavior, exact recursive-delete confirmation, and recovery.
Backend simulation covers lifecycle/adoption, replay/conflicting request IDs,
failed/unknown jobs, read-back recovery and preservation of shared objects.
These simulation results must not be reported as real RouterOS lifecycle writes.

## Deployed lab evidence

- Independent real snapshot/Files read contracts passed on RouterOS 7.23.7.
  The same GET-only contract passed on the current production RouterOS 7.23.5;
  production credentials stayed process-only and no mutation was performed.
- The deployed test service was active. Administrator authentication, ready
  bootstrap state, device selection, options, resolution, logs and Files reads
  worked. Nine embedded HTML/JS/CSS hashes matched a fresh local build, with
  both UI query entries responding successfully.
- Real directory tests created a unique disposable subtree, replayed creation
  without duplication, added a Unicode child, renamed while preserving that
  child, and recursively deleted the nonempty subtree. Re-read confirmed the
  temporary paths were gone. Disk/root protection, changed-ID rejection and
  traversal rejection passed. The running container's root was marked protected.
- Unauthenticated reads returned 401; cross-origin resolution/directory requests
  returned 403; authenticated snapshots used `Cache-Control: no-store`.
  Missing/unknown devices, nonexistent containers and invalid/missing Files paths
  returned the expected errors. POST lifecycle/upload/recovery and PUT/PATCH/
  DELETE container attempts all returned `container_read_only` without enabling
  an unsupported write flow.
- The real smoke container remained running with positive memory telemetry,
  archive image source, preserved root path and custom health configuration.
  Its logs contained HTTP 200 responses. Direct access from both the Mac and
  independent test host was already verified after the network repair.

Private scripts, runtime configuration, receipts, topology and logs remain
outside the public repository. Only the disposable test router received Files
writes in this validation.

## Production trial scope and remaining acceptance

The current build can be trialed for container list/detail/log/Files reads and
configuration preview. Container creation, edits, start/stop/restart, image
upload/update, adoption and deletion through the panel remain disabled on real
routers. Implement and independently verify that writer before claiming a full
native management release.

Real directory mutations have an additional known limitation: unresolved
operation records and write-gate reconciliation are in memory, and do not
survive an application restart. Continue destructive directory trials on the
independent lab; a durable journal/restart recovery needs its own acceptance
before production destructive use is approved.

Visual acceptance remains user-led. At 1440px and 390px, review both complete
UIs in light/dark themes: native card colours, readable long-image rows, seven
flat sections, advanced/resource/health disclosure controls, directory picker,
env delete alignment, and device changes with a form/log view open. Component
and asset tests are evidence of behavior/packaging, not browser visual approval.

The task and its existing PR remain unfinished/Draft. No `APPROVED`, merge,
production delivery, or Trellis completion/archive is implied by this report.
