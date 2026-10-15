# Design

The user approved the complete Compose paste-import plan on 2026-10-15.
This is a single-service form importer, not a Compose runtime.

Add a pure bounded YAML parser in internal/containers and a device-scoped,
authenticated POST /api/containers/compose/parse endpoint before snapshot reads
or mutation admission. Return ordered service candidates, a closed set of form
fields, imported field names, skipped-field explanations and manual-input notes.
Never read RouterOS, external files, environment variables or invoke executables.
Do not persist/log source text or values. Reject malformed/duplicate/multi-document
YAML, excessive structure and aliases/merge keys rather than expanding them.

Import name/image/environment, user/workdir, nonempty string startup overrides,
mem_limit/mem_reservation/cpuset and supported restart policies. Preserve mount
targets and read-only mode, but always leave ROS sources empty. Keep original
sources only as transient reference hints. Unresolved interpolation is omitted
with manual-input notes; escaped dollars become literals. Unsupported fields
are skipped, without invented equivalent settings. An explicit entrypoint with
no explicit command needs a manual note because Compose suppresses image CMD.

The shared create editor exposes a transient modal. Single service auto-selects;
multiple services require selection. Applying replaces only the closed Compose
field set and retains draft identity, ROS network/root, startup toggles/logging
and health settings. Absent imported fields return to normal defaults, avoiding
residual values on repeated imports. Resource controls enable when imported.
Cancel, source edits, unmount/device changes and stale responses cannot apply old
results. Existing container editing has no importer. Both UI CSS graphs provide
scoped responsive styling with opaque theme surfaces.

Branch codex/compose-import starts at accepted container baseline 92c35f7, whose
Draft PR #30 remains unmerged. New Draft PR targets main and documents that base
dependency; no prior PR merge is implied by this feature request.
