# Validation and delivery — 2026-10-15

Implementation commit: a1d63554c7e5f280fab6a3e2041e70332644a156.
Draft PR: https://github.com/jasonxtt/rosboard/pull/32.
Baseline dependency: accepted container code in still-open PR #30; no merge of
that work or this feature is authorized by delivery alone.

## Automated and source review

- Full Go tests, build and vet passed. Parser/API coverage includes bounded YAML,
  duplicate/multi-document/alias rejection, safe diagnostics, interpolation,
  supported/unsupported fields, environment forms, mount forms, memory units,
  CPU bounds, authentication and device scope. The pure API test fails on any
  attempted RouterOS reader call.
- All 105 frontend tests passed, including the four new parser-adapter/form/modal
  cases covering single/multiple service selection, manual source paths,
  replacement without ROS scope changes, resources, cancellation, source edits,
  device changes and stale responses.
- Frontend lint passed with only four existing fasttrack test key warnings;
  TypeScript/Vite build and emitted dual-UI/CSS isolation checks passed.
- Complete feature source and staged paths were reviewed; no known credentials,
  private addresses, key/token material or unrelated files were included.
  Published Git tree matches the local validated tree. GitHub docker CI passed
  for the implementation revision. CLI credentials were unavailable, so GitHub
  connector Git objects/ref operations published the exact tree; the local
  branch fast-forwarded to that commit without history rewriting.

## Disposable test-machine / independent RouterOS verification

- Deployed dev-compose-1015 with the isolated lab-only configuration/data.
  Service health and all 78 embedded distributable files matched the build.
  An unauthenticated live parse request returned 401.
- An authenticated full MSM Compose sample produced the supported fields, five
  expected skips and an empty ROS mount source. Duplicate keys were rejected.
- Selected an independent name, VETH/IP, root and config directory; completed
  resolve, host manifest/layer download and verification, Docker-save packaging,
  SFTP upload/hash read-back, native import and start. New MSM served HTTP 200.
- Confirmed first-successful-runtime generated archive cleanup. Deleted the
  fixture container/root and explicitly deleted its test mount data/parent.
  The user's original MSM remained healthy with its original ID and root.
- Temporary verification sessions were removed. No production RouterOS or
  production SQLite configuration/data was used by the test service.

## Production delivery and pending acceptance

- Confirmed no unsettled production jobs and a writable CIFS NAS before stopping
  the service. A consistent timestamped backup, 20261015T165122Z-compose-import,
  includes old binary/supervisor, configuration, all SQLite/data, updater state
  and systemd unit. The complete 1,702,932,480-byte archive was read and SHA-256
  verified before replacing the binary. A post-delivery retention audit found
  that the initial matcher excluded older labels containing version dots, so
  eleven timestamped directories existed briefly. Only the oldest timestamped
  backup was removed; a broad timestamp-prefix recount confirms ten remain and
  the new backup is present. Future retention must not constrain label syntax.
- Production runs the same dev-compose-1015 binary. Service and health passed;
  all 78 distributable assets match. Config, unit and supervisor match backup.
- Authenticated pure parse and container snapshot read-back passed for both
  enabled device scopes. The original lab MSM remained healthy. Production
  verification requested no RouterOS mutation; its temporary session was removed.
- User manual visual acceptance remains pending. Inspect Aurora and Compact,
  desktop and 375px/mobile: create -> import -> paste -> parse -> select service
  -> inspect skips/manual fields -> apply. Verify readable dialog controls,
  no page overflow, retained network/root, blank mount sources and cancellation.
  Repeat import and theme switching. No main merge, release or task archive.
