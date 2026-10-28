# Delete rootfs and diagnose memory rejection, 2026-10-15

User requests that deleting a container delete its rootdir rather than retain a
renamed sibling, and asks why the newest create failed.

Diagnosis: the latest job8d3516db completed host download/hash verification and
published its SFTP archive. All preceding network/mount steps succeeded; final
container/add failed before creating a container. Request fields memory-high and
memory-max were both the bare string128 (bytes). Independent RouterOS reproduces
exact HTTP400 `failure: memory-max < 1MB`;128M is accepted. User confirms both
limits were intended to be128MiB, and asks the distinction between high/max.
Explained high reclaim/throttling versus max hard/OOM boundary using Linux docs.
No production task replay was performed during diagnosis.

Implementation:

- Delete no longer schedules root retention/rename. Before native remove, re-read
  exact owned stopped identity and reject unsafe/shared/mounted root overlaps.
  Confirm root absence afterward; only exact recorded ID/path/type may authorize
  leftover root cleanup. Same-path replacement is retained and blocks completion.
  Independent mounted data remains. Updates and legacy journals retain old behavior.
- Validate explicit memory quantities at least1MiB, including overflow, before
  admission; preserve bare-number byte semantics and explain128M beside inputs.
- Persist bounded RouterOS rejection details in failed jobs, redacting connection
  credentials and environment values instead of always showing a generic failure.

Verification: full Go tests/vet,100 frontend tests, lint/build/dual-UI isolation.
Added ownership/shared/mount/replacement, memory-unit and error-redaction tests;
legacy pending root-rename recovery still passes. Actual independent RouterOS
reproduces128-byte rejection and accepts128M. Full real host-image lab passes
DockerHub/GHCR create/update/start/restart, artifact cleanup and delete: rootfs
removed without a retained sibling while a real independent mount-source folder
remains. Disposable lab fixtures cleaned; production runtimes untouched.

Production delivery completed after the user explicitly approved deployment and
correction of the original failed task. Automatic approval review had previously
required this version-specific authorization; no production mutation preceded it.

Implementation checkpoint: cd280c59b007ed6473530208ae515c03e62c4f14.
Both test and production now run dev-root-cleanup-1015; health and all 79 embedded
asset hashes pass. Before replacement, the complete production backup was written
to the mounted writable NAS under 20261015T081835Z-root-cleanup. The archive was
fully read and its SHA256 verified; retention remains at most 10 timestamped
backups. Configuration and service unit match the backup after deployment.

While the service was stopped, an atomic identity/state-bound repair changed only
the exact failed job's unfinished container/add memory-high/max fields to 128M
and its explanatory phase/error. Original request/signature, completed steps and
published image artifact were preserved. No retry, container creation or deletion
was performed. The in-app browser visibly shows 72%, the corrected-memory
explanation and the enabled Continue unfinished steps button. User execution and
manual acceptance remain pending; task stays in_progress and PR stays Draft.
