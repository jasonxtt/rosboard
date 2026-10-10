# First authorized live Container smoke test

The user authorized a simple real container on the independent lab RouterOS,
after accepting the current UI provisionally. The lab runs RouterOS 7.23.7
long-term on x86_64 with Container mode enabled. Credentials, actual network
addresses, device IDs and private operation receipts stay outside the repository.

## Verified operations

- Resolve a dedicated Nginx Alpine draft with the deployed rosboard API, then
  apply its Container fields to the isolated lab. Dedicated VETH/bridge-port
  objects were created with an identifying smoke-test comment.
- Registry creation was accepted, but the router failed direct Docker Hub
  access and its fallback registry DNS resolution. This is a network/download
  failure, not successful image installation. Read logs before proceeding.
- Download the official linux/amd64 image on the disposable rosboard test host
  using skopeo, produce a single-image Docker archive, and transfer it through
  SFTP. Re-read the remote archive and compare its SHA-256 with the source.
- Remove only the exact failed smoke container after matching its name, ID,
  interface, root path and comment; preserve its dedicated VETH and bridge port.
  Import the verified archive using `file` instead of `remote-image`.
- RouterOS accepted the resolved absolute `/disk/application/rootdir` path.
  Import finished with `stopped=true`; explicit start launched the inherited
  Nginx entrypoint and command. A localhost HTTP probe returned 200 and healthy.
- Explicit stop exited gracefully with code zero. Start and native restart
  retained the root path and returned to healthy, with repeated HTTP 200 logs.
- `/container/start`, `/container/stop`, and `/container/remove` accepted
  `numbers` with the exact object ID. On this version, `/container/restart`
  rejected `numbers` and accepted singular `number`. Do not guess one argument
  shape for every lifecycle command.

## Read-model fixes revealed by real data

- Download flags are `downloading/extracting` and `download/extract failed`.
  Failure takes precedence over a stopped flag. URL-encode the `.proplist`
  parameter because a requested property contains a space.
- A healthy container may return `healthy=true` without `running=true`.
  `starting-with-healthcheck` maps to starting; healthy/unhealthy maps to running
  only after explicit stopped/error/transitional flags have been considered.
- Per-container memory is `memory-current`; retain `memory-usage` as an older
  fallback without inventing telemetry when neither property is present.

## Boundary and remaining validation

This was an operator-controlled live smoke test, not a production Container
writer acceptance. The panel Container lifecycle endpoints still fail closed;
real Files directory operations remain separate. The running smoke container
and its owned artifacts remain in the lab for user inspection.

The selected existing bridge originally had no LAN member. Internal HTTP health
is proven, while direct LAN access requires a network preparation decision.
Existing LAN membership/address migration was requested from the user and has
not been applied without an answer. No bridge VLAN, NAT, or production device
changes are part of this test.

Reference: https://manual.mikrotik.com/docs/containers/
