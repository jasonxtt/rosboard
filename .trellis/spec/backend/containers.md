# Native Container contracts

## Scope

Container lifecycle/image and Files directory writes default to read-only. Explicit private
per-device `container_writes` opt-in enables the native writer for an authorized
independent lab or an explicitly authorized production pilot. Public device APIs cannot enable it; connection changes clear
write authority and the pinned SSH key. Production verification/acceptance
remains separate. Simulations and development fixtures remain outside the
embedded production build.

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
- Files directory CRUD is real and separately advertised as `directoryWrites`,
  but requires the same private per-device `container_writes` opt-in as native
  writes. Enforce denial at both API admission and service entry before any
  RouterOS I/O, including recovery. Recompute the directory capability when
  decorating cached snapshots so a changed opt-in cannot leave stale controls.
  Optional real preview credentials are process-only and use the production handlers.
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

## Observed RouterOS 7.23 runtime flags

Request `downloading/extracting`, `download/extract failed`,
`starting-with-healthcheck`, `healthy`, `unhealthy`, and `memory-current` through
closed read properties. URL-encode `.proplist`, including space-bearing names.
Combined download failure wins over stopped; healthy/unhealthy implies running
only in the absence of an explicit lifecycle state. Use memory-current before
legacy memory-usage. Do not infer lifecycle state from stale healthcheck-status.
An explicitly authorized operator smoke test does not enable product lifecycle
writes or approve production delivery.


## Native write invariants

- Use `MutationClient.ContainerCommand` closed menus/fields only. No general
  REST executor, bridge/VLAN edits, interface IP, routes or firewall/NAT commands.
- Lifecycle, upload, policy and directory mutations share a device lock. Durable
  native journals load before serving requests; unknown writes hold that lock.
  Persist pending state before commands. Recover by read-back, never blind retry.
- Ownership uses exact generated device/operation markers. Adoption marks only
  its container. Deletion rechecks live references before auxiliary removals;
  old env/mount cleanup requires recorded container/update lineage.
- Upload only validated single Linux Docker-save archives, with pinned SSH,
  exclusive generated remote paths and read-back SHA-256/size verification.
- ROS 7.23.7 requires new root-dir paths. Remove an explicitly selected existing
  empty folder only after rechecking exact ID/type, children and references.
- Container removal implicitly erases root-dir. Rename the stopped unshared
  filesystem to a unique sibling before remove. Verify the new name and absence
  of the old name; Files opaque IDs can change on rename. Shared/overlapping
  root/mount references block renaming/removal. Never promise the original
  runtime path survives unchanged. Do not automatically purge retained backups.
- Project CPU indices/ranges to cpu0/cpu1 names. RouterOS cannot unset memory
  integer fields: an empty set becomes `unlimited`. When editing with blank
  memory overrides, project the freshly read global creation defaults instead;
  use `unlimited` only when that global field is absent/empty. Preserve explicit
  overrides independently for high/max. These defaults apply at save time;
  changing global defaults does not dynamically change an existing container's
  kernel limits. Read-back compares normalized units, booleans, CPU names and
  paths. Existing config values survive editor defaults.
- A blank startup override must not be sent as `set cmd=""` or
  `set entrypoint=""`: this removes the image's usable startup command.
  Omit unchanged inherited fields. Clear existing cmd/entrypoint/user/workdir
  overrides through the closed `container/unset` command with an allowlisted
  `value-name`; confirm absence of that property in fresh read-back. Native
  journals record these steps before writing, with no blind retries.
- RouterOS `.proplist` treats plus literally; encode property spaces with `%20`
  so `download/extract failed` remains visible. Public errors never include raw
  connection credentials or request payloads.

## Container-only registry DNS warming

- External DNS queries before registry imports can populate RouterOS FWD address
  lists where internal resolver requests do not. Query only the selected router,
  A and AAAA, with bounded timeouts and validated replies/TCP fallback.
- Explicit image registry hosts take precedence over private configured/detected
  registry URL. Keep registry URLs in a separate closed internal config read;
  never expose registry credentials/URLs through snapshots or public errors.
- Skip archives and non-download operations. Reuse existing journaled add steps
  for update/recovery; restore pending-download refresh after restart without
  repeating native writes. DNS refresh does not release unknown-operation locks.
- Refresh original known names using minimum address/CNAME TTL; arbitrary CNAME
  queries may select a different DNS policy. Stop workers when import ends.
- Preparation is best effort with safe optional warnings. It adds no DNS, route
  or firewall mutations and no connectivity-validation gate.
- Lab evidence shows a remaining race: a subsequent internal lookup can return a
  previously unseen address and connect before external refresh populates the
  list. Do not claim pure warming guarantees rotating registry/CDN routing or
  accept production delivery on unit tests or one successful request alone.

## Read-only transfer capability and explicit probes

SFTP capability checks may authenticate with a pinned key and list/stat/read
metadata; they must not mkdir/create/upload/rename/remove, invoke a shell write,
or call a REST mutation. A declared right or read session cannot prove upload
success. Actual authorized image upload can supply write evidence without a
pre-create dummy probe.

An optional write probe requires its own explicit administrator authorization,
the private device write opt-in and shared device gate. Persist intent before
creating exclusive server-generated paths inside a validated temporary
rosboard-owned namespace. Reject unowned/client paths and collisions, protect
archives/rootfs/mounts, and clean only exact recorded objects. Cancellation,
failures and unknown outcomes preserve cleanup intent; capability refresh never
retries a write. These are implementation/review constraints, not permission to
mutate a router during capability discovery.

## Host registry jobs and generated artifact ownership

- New registry create/update jobs capture an explicit immutable download transport
  at admission. Persist only its revision, never proxy credentials; after restart
  changed authority requires explicit recovery. Do not use environment proxies,
  implicit Docker keychains, Docker daemons or credential helper executables.
- Persist the selected manifest digest before local blob writes. Verify compressed
  sizes/hashes and expanded DiffIDs; emit single-image Docker-save archives with
  uncompressed layer members without extracting or executing them.
- Generated artifacts live under effective data_dir/container-staging/device/job.
  Record exact owned filenames before writes; reject foreign files and symlink
  ancestors during cleanup. Use exclusive SFTP job namespaces, bounded idle I/O,
  hash read-back and final rename. RouterOS SFTP missing-path status may be generic:
  prove absence via successful parent listings, never by treating errors as missing.
- Persist physical import file separately from logical registry source/digest and
  target platform. Keep old native journals recoverable without replaying an
  ambiguous add. GET endpoints never perform cleanup or transfer probes.
- Keep artifacts before first successful running state (healthy when configured),
  including stopped imports. Persist cleanup intent before exact hash-bound removal
  after successful runtime or confirmed deletion; protect runtime roots, mounts and
  other source references. Unknown cleanup holds the device gate for explicit recovery.
- Image transfer settings may save an explicitly verified connection/dedicated SSH
  identity through a read-only check. They cannot enable private container writes.

## Automatic first-use transfer identity

The user's 2026-10-15 product amendment removes manual SSH fingerprint entry and
trust prompts. An explicit authenticated create/update/upload may initialize a
missing pin: verify the saved REST credentials, observe the SSH key at that REST
host without authentication, verify credentials and SFTP metadata reads with the
exact observed key, then persist it only if endpoint/credentials/device authority
remain unchanged. This is trust on first use. Never replace an existing pin on a
network failure or key mismatch. GET/preview/capability checks never enroll keys.
Settings saves with a missing key use the same automatic read-only verification;
TestOnly must not persist it. Keep private container-write opt-in independent.

## Delete root filesystem and memory limits (2026-10-15 amendment)

User-authorized container deletion removes its exclusively bound root filesystem
instead of retaining/renaming it. Immediately before native remove, recheck exact
owned stopped identity, root safety, other container roots and all mount sources.
After native removal, confirm root absence; remove a leftover root only through
its exact recorded opaque ID/path/type and fresh no-reference checks. A same-path
replacement must fail closed. Independent mount data stays. Updates and legacy
retention journals retain their prior behavior.

Bare numeric memory values remain bytes for compatibility. Reject explicit values
below1MiB and overflow before admission, and explain units beside both inputs.
Capture bounded, credential/env-redacted RouterOS HTTP rejection details in the
durable job so deterministic add failures do not collapse into a generic message.
