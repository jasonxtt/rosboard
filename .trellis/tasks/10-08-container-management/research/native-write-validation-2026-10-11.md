# Native Container lab write validation — 2026-10-11

## Scope and result

Native lifecycle writes were exercised through the complete Compact panel on the
independent disposable lab, RouterOS 7.23.7 long-term x86_64. Only that lab device
was enabled in private server configuration. No production router, program,
configuration or data was accessed or replaced in this phase.

Local-image lifecycle tests pass after fixes. Registry-image download cannot be
marked passed: the lab's Docker Hub and RouterOS fallback registry connections
timed out. The resulting error, real logs, restart/read-back recovery and explicit
cleanup were verified. Therefore this is a lab validation checkpoint, not a full
production acceptance or an unrestricted production writer release.

## Real browser/runtime coverage

| Operation | Evidence |
| --- | --- |
| Required fields | Missing image/static network prevented submit; draft retained |
| Local upload | Official BusyBox Docker-save archive uploaded through panel; pinned SFTP transfer and read-back size/SHA-256 passed |
| Archive reuse | Previously validated upload selected from the new archive picker; no repeated upload |
| Create | Dedicated VETH and existing bridge port; image imported into independent root; direct HTTP 200 |
| Startup overrides | BusyBox httpd entrypoint/CMD, user/workdir and logging read back correctly |
| Environment | Spaces, quotes, shell-looking text and Chinese values preserved verbatim; no shell interpolation |
| Mount | Read-only host directory mounted at /www; response body matched source file |
| Resources | CPU index 0 projected to cpu0; 32M/64M read back as bytes; disabling limits restored unlimited |
| Health | Custom wget check produced healthy state/logs; inherited mode cleared overrides on edit |
| Stop/start/restart | Real state changes and HTTP response after start/restart verified |
| Edit | Static address changed, custom values changed, resource/health overrides cleared and boot flag disabled; original root and mounted data preserved |
| Update | New rootfs/container staged, cutover completed; direct HTTP 200 and mount data preserved |
| Runtime retention | Old runtime renamed to a unique retained sibling; seeded file survived actual old-container removal |
| Delete | Replacement runtime retained with its BusyBox binary; source archive and mount data survived; dedicated VETH/bridge port removed |
| Adoption | Explicitly adopted existing Nginx and a disposable stopped shared-network fixture; auxiliary ownership unchanged |
| Shared network | Deleting the adopted fixture preserved shared VETH and bridge port; Nginx stayed healthy with HTTP 200 |
| Failure/recovery | Invalid CPU and memory/root parameters diagnosed and fixed; known-failed continuation and unknown state across service restart recovered without duplicate container creation |
| Registry failure | Real download/extract failure and timeout logs displayed; read-back recovery did not retry add; failed container and its dedicated network cleaned through UI |
| Deployment | Isolated service active, authentication/ready phase/device scope passed; nine embedded HTML/JS/CSS hashes matched local build; both UI entries responded |

Directory mkdir/select was also exercised through the runtime picker. Real
mkdir/rename/recursive-delete/protected-path tests are recorded in the earlier
Files validation checkpoint; their destructive restart durability limitation
remains separate from the new native lifecycle journal.

## Bugs found and fixed

1. Valid Skopeo Docker-save archives include safe internal legacy links. Inspect
   their targets without extracting; reject escaping, missing or cyclic targets.
2. ROS CPU lists require cpuN names. Empty explicit memory values are invalid;
   clear limits with unlimited and compare normalized read-back values.
3. ROS rejects a pre-existing root directory even when empty. Recheck and remove
   an explicitly selected empty folder immediately before import. Never reuse a
   nonempty/in-use root.
4. Boolean read-back is true/false rather than submitted yes/no; normalization
   avoids false unknown results after successful commands.
5. Container/remove automatically removes root-dir. Rename the stopped,
   unshared filesystem before remove; actually verify retained content. The UI
   now explains the changed retained path. Updates use the same guarded rule.
6. Files rename changes opaque IDs; retention recovery confirms new/old paths
   rather than requiring the old opaque ID after rename.
7. RouterOS proplist treats plus literally. Encode spaces as %20 to read the
   native download/extract failed flag and stop waiting on an already failed job.
8. Clear stale selected logs after successful update/delete so removed container
   IDs do not cause irrelevant log errors.

## Automated evidence

Go build/test/vet and targeted containers/routeros/api race checks pass.
Frontend: 90 tests pass; lint has only four pre-existing fasttrack JSX-key
warnings; build and both UI bootstrap/asset isolation checks pass. Added native
regressions cover request replay/busy gates, normalized fields, unknown-write
read-back without retries, private journal restart/locking, changed opaque IDs,
shared-root refusal, update/delete runtime retention, adoption/shared ownership
and clearing private write authority after connection changes.

Private credentials, configuration, journals, archives, scripts, topology and
screenshots remain outside this public repository. The original Nginx container
was left running for user testing. Disposable lifecycle/shared/failed test
containers were removed. Retained runtime backups and persistent test data remain
on the lab for inspection; they are intentionally not automatically purged.

## Remaining acceptance

- Successful remote registry download/update awaits a reachable registry from
  the test RouterOS. Local archive import/update passed independently.
- Full user acceptance of both UIs/themes/mobile is still user-led. Actual write
  browser coverage here used Compact; shared component and both build checks
  do not imply Aurora visual approval.
- Resource inheritance was tested with unlimited global settings; alternative
  global-limit combinations require separate device verification.
- Retention behavior is verified on 7.23.7 only. Other versions/architectures,
  disk exhaustion, host reboot during import/cutover, and restoration from a
  retained rootfs are not accepted by this checkpoint.
- Separate Files operation records remain in memory. Do not represent native
  lifecycle durability as acceptance of destructive Files restart recovery.
- The same task remains active and PR Draft. No production deployment, release,
  merge, APPROVED, or full task completion is implied.
