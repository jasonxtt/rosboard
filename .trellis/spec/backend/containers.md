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

## Image archives and directory metadata

- `imageSource=registry` projects `remote-image`; `archive` projects `file` and
  excludes `remote-image`. New archive IDs must resolve from device-owned server
  metadata; never trust client file paths/references. Existing archive paths are
  preserved against the authoritative container snapshot.
- Read Files with a closed `.proplist=name,type,size` allowlist, never contents.
  Normalize display paths to an absolute Files root, reject dot/traversal/control
  segments, and select folders independently for rootfs and mount sources. Keep
  original RouterOS relative paths when an existing container is unchanged.
- New rootfs defaults end in `rootfs`; persistent mounts remain outside it.
  The Files virtual root and whole disks cannot serve as new rootfs directories.
- Bound archive upload size and JSON metadata; inspect without extracting or
  executing. Temporary staging is private and removed on success/failure. This
  phase accepts single-image Linux Docker-save tar only. Simulation stores only
  metadata and labels binary RouterOS transfer as simulated.
- Directory reads are real; mkdir/archive uploads remain test-only until the
  write phase. Optional real preview credentials are process-only, and the real
  preview device routes through the same production write-denial handler.
- On RouterOS 7.23.5, `/file/remove` with exact opaque IDs works where REST DELETE
  can fail. Future cleanup must use a typed command and verify owned paths/IDs.
  Binary SFTP upload support does not prove container import/start correctness.
