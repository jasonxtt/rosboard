# Native Container contracts

## Scope

The first native Container phase is read-only on RouterOS. All simulated
mutations are Go test-only handlers; they are absent from `cmd/rosboard`.
Development preview HTML/fixtures must stay outside the embedded UI build.
A future write phase requires separate RouterOS verification and acceptance.

## Boundaries

- `internal/routeros.ContainerRead` has a closed GET-only menu/property allowlist.
  Never request registry credentials, `config-json`, or arbitrary REST paths.
- `internal/containers` owns snapshot normalization and pure draft resolution.
  Explicit device scope selects one enabled, non-archived configuration. Missing
  optional menus are capability warnings; network/auth failures remain errors.
- RouterOS REST scalars are strings. Container runtime state may be boolean
  flags such as `running`/`stopped`, rather than a `status` property. Join VETH
  and bridge ports by exact interface names, never naming conventions.
- Disk selection requires a usable mounted filesystem and positive free space.
  No observed memory-usage field means unavailable telemetry, never invented zero.
- Resolve endpoints perform no RouterOS writes. Required image/network values
  never acquire network defaults. Blank image command/user/workdir overrides are
  omitted; existing values remain intact. Env values are not shell-parsed.
- VETH sharing is supported by RouterOS. New containers use dedicated VETH;
  existing shared VETH changes must be rejected. Existing/shared resource lists
  never become owned merely because their container is adopted.
- Snapshot reads coalesce per device/credential fingerprint and expire after
  five seconds. Failures are uncached. A future mutation/read-back implementation
  must explicitly bypass/invalidate this cache before confirming outcomes.
- Job identifiers, idempotency keys and locks are device-scoped. Unknown outcomes
  hold the operation lock until read-back; restoring a page restores its job.

## Verification

Use fake typed readers and local HTTP fixtures. Include required/default fields,
verbatim env values, TCP/UDP, read-only mounts, shared networking, account/device
isolation, write denial, idempotency and unknown-result recovery. Normal tests
skip the interactive preview server. Production builds must exclude its fixtures.
