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

Production rollout and the user's original failed-task memory correction remain
pending. Correction must be backed up, limited to the exact failed non-pending add
and confirmed128M values, preserving all completed steps/artifacts. Leave execution
to the user's Continue action; do not create a duplicate task/container.
Task and PR remain in_progress / Draft, pending manual/root acceptance.

Checkpoint implementation published as cd280c59b007ed6473530208ae515c03e62c4f14
(local2d29748, identical tree). Final test service runs dev-root-cleanup-1015;
health and all79 embedded file hashes pass. A read-only dry run confirms the
original failed record still has exactly one unfinished, non-pending container/add,
with both limits128, all completed objects/artifacts preserved and no target ID.
Prepared an atomic, identity/state-bound repair of only those two remaining step
fields to128M plus explanatory task status; preserve original request/signature.
No retry or native create/delete is part of that maintenance repair.

Automatic approval review rejected the combined production replacement/restart
and persisted-task repair before execution, requiring explicit user approval for
this new version and journal adjustment. A concrete approval request lists backup,
short restart, deletion behavior and exact memory correction. No new production
backup, binary change or journal edit was performed; production remains
 dev-auto-transfer-1015 and the original failed task remains unchanged for now.
