# Docker run import validation — 2026-10-16

Implementation: 624f27b42e3cb9b442f64cb4f1c4feb428a2eb86, same branch and Draft
PR #32. This amendment extends configuration import and preserves the prior
native-UI alignment; it does not add Docker execution or a new runtime.

## Behavior

The shared Create -> Import configuration entry auto-detects Compose or a single
Docker run command. Existing Compose clients/path remain compatible; optional
format metadata identifies the syntax. Quoting, escaped dollars/newlines,
short/long/equal options, repeated scalar/env flags and mount forms are handled
without Shell/file/environment evaluation. Known unsupported options are listed;
unknown arity, Shell compounds and substitutions are rejected safely. Post-image
argv requires manual CMD entry instead of a lossy join.

The requested ddns-go example imports image/name/restart=always and target /root.
The source remains empty with the original Docker path as a reference. Detach
uses the panel's normal background/startup behavior. Host networking is skipped
with an explicit reminder to choose ROS bridge/IP/gateway; no NAT or routing is
created by parsing. Literal single-quoted/escaped passwords retain dollar signs.

## Automated validation

- Full Go tests, vet and build passed. Six Docker run table/contract cases cover
  the exact command, resources, quotes, escapes/CRLF, repeated fields, variables,
  static host-source hints, option arity, rejected compounds and safe errors.
- Existing no-RouterOS-I/O API tests include a valid Docker command. Compose
  parsing/auth/device isolation tests remain passing.
- All 108 frontend tests passed, including Docker syntax display, automatic
  single-container selection, unchanged submitted source and no create action.
  Existing Compose cancellation/stale/device/scope-preservation cases pass.
- Frontend TypeScript/Vite build, lint and emitted dual-UI/CSS isolation checks
  passed. Only the four existing FastTrack JSX-key warnings remain.
- GitHub docker CI passed at implementation revision 624f27b.
- Reviewed the complete staged source/artifact paths and secret patterns before
  publication. GitHub connector published the exact checked tree; local Git
  fast-forwarded without rewriting shared history.

## Test delivery and actual RouterOS validation

- Only the disposable isolated test service was replaced with dev-docker-run-1016.
  Its configuration contains only the independent lab device. No unsettled jobs
  existed before replacement. Config, supervisor and service-unit hashes stayed
  unchanged; candidate hash/health and all 78 distributable assets passed.
- The exact requested command parsed through the live authenticated API into
  the expected fields and two explained skips. Changed the fixture name, selected
  an unused independent IP/VETH, a unique root, and a manually chosen unique
  config source. No provider credentials or domain update configuration was used.
- Resolve, host registry download/digest/layer verification, Docker-save/SFTP
  import and native start all passed. ddns-go served HTTP 200 on port 9876 at its
  independent container IP. First-successful-runtime image cleanup was confirmed.
- Native delete removed the fixture/root. Explicit deletion removed only its
  test mount data and parent. The original MSM retained its ID/root and remained
  healthy. The temporary verification session was deleted.

## Pending acceptance

Production remains dev-compose-1015, unchanged by this amendment; the UI alignment
and Docker run import await user inspection on the test instance. Check both UI
variants, light/dark and desktop/mobile: Create -> Import configuration -> paste
the ddns-go command -> Parse -> inspect host-network/manual-source notes -> Apply.
Verify ROS network/root survive and the source remains blank; cancel/repeat
import as well. Keep task in_progress and PR Draft. No production replacement,
main merge, release or archive is authorized by these test results alone.
