# Native Container contracts

## Scope

Container lifecycle and image writes remain disabled on RouterOS. The user's
directory-picker follow-up authorizes real Files mkdir, rename and recursive
directory removal independently. Simulated container lifecycle mutations are
Go test-only handlers; they are absent from `cmd/rosboard`.
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
- Health-check commands are executed inside the container and require tools
  present in its image. User-facing examples use a RouterOS command such as
  `curl -f http://127.0.0.1:80/`, without Dockerfile's `CMD-SHELL` marker.
  Image inheritance adds no custom probe when the image has none. A probe reports
  service health; stop/restart/notification behavior needs separate configuration.
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
verbatim env values, direct-IP networking, read-only mounts, shared networking, account/device
isolation, write denial, idempotency and unknown-result recovery. Normal tests
skip the interactive preview server. Production builds must exclude its fixtures.

## Image archives and directory metadata

- `imageSource=registry` projects `remote-image`; `archive` projects `file` and
  excludes `remote-image`. New archive IDs must resolve from device-owned server
  metadata; never trust client file paths/references. Existing archive paths are
  preserved against the authoritative container snapshot.
- Read Files with a closed `.proplist=.id,name,type,size` allowlist, never contents.
  Normalize display paths to an absolute Files root, reject dot/traversal/control
  segments, and select folders independently for rootfs and mount sources. Keep
  original RouterOS relative paths when an existing container is unchanged.
- New rootfs defaults end in `rootfs`; persistent mounts remain outside it.
  Runtime and mount directories may share an application parent; folder names
  are not fixed. The Files virtual root and whole disks cannot serve as new rootfs directories.
- Bound archive upload size and JSON metadata; inspect without extracting or
  executing. Temporary staging is private and removed on success/failure. This
  phase accepts single-image Linux Docker-save tar only. Simulation stores only
  metadata and labels binary RouterOS transfer as simulated.
- Files directory CRUD is real and separately advertised as `directoryWrites`.
  Container `writes` remains false. Archive uploads remain test-only. Optional
  real preview credentials are process-only and use the production handlers.
- On RouterOS 7.23.5, `/file/remove` with exact opaque IDs works where REST DELETE
  can fail. Future cleanup must use a typed command and verify owned paths/IDs.
  Binary SFTP upload support does not prove container import/start correctness.

## Bound directory mutations

- `POST /api/containers/directories?device=...` accepts only `mkdir`, `rename`,
  `delete`, and read-only `recover` actions. Use the typed `MutationClient`
  Files methods, never local filesystem APIs or arbitrary client REST commands.
- Preserve Unicode and spaces, reject traversal, separators in a name, control
  characters, oversized paths/names, root and disk mutations. Re-read the exact
  source type, path, opaque ID and container root/mount references before writing.
  Destructive operations require a matching expected ID; deleting requires the
  exact confirmed absolute path. Ordinary files have no mutation controls.
- The user permits nonempty deletion. Explain that all files and subdirectories
  are deleted. Protect disk/system roots and overlapping existing container
  root-dir/mount paths, including their ancestors and descendants.
- Use the policy manager's shared device write gate (a local service gate when
  no manager exists). Never retry a write. Request IDs replay confirmed outcomes;
  unknown outcomes keep the gate until explicit read-back confirms the result.
  Files GET exposes a pending request so reopening a picker can reconcile it.
- Real RouterOS 7.23.5 testing verified `/file/add`, `/file/set`, and recursive
  `/file/remove`. Renaming may change opaque IDs for the folder and descendants:
  confirmation must verify old/new paths and retained descendants, not ID equality.
- Files also return long `**opaque` IDs instead of configuration menus' `*hex`
  IDs. Validate these separately and send them only as exact JSON `numbers`
  values matched against fresh metadata; never place them in REST path segments.
- Operation replay records are in memory and expire after one hour once settled.
  An application restart does not restore unresolved directory records. This
  test-stage implementation is not a durable production mutation journal.

## Direct container access

Services are accessed at container-IP:application-port through the configured
VETH/bridge. There is no port-mapping field in drafts or snapshots. The container
reader does not allow firewall NAT menus, and simulation does not create NAT
ownership. Existing router NAT rules remain outside this feature. Stale JSON
port fields are ignored and never reappear in resolutions or snapshots.
