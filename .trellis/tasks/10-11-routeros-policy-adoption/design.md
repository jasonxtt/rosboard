# Design: whole-scope RouterOS adoption

## Chosen approach

Read existing RouterOS objects, reconstruct a supported policy snapshot, preview current-generator replacements, and perform a bounded handover. Reuse RoutingRule, Egress, TargetList, AccessRule, the typed RouterOS client, and existing plan/job machinery. Do not weaken ordinary foreign-ownership checks.

## Discovery and parsing

Add a focused adoption module under internal/policyv2. Scan relevant menus already covered by reconciliation, plus reference evidence needed to prove that cleanup is safe. Identify candidate scopes from canonical comments; group objects by scope and connect them through connection marks, route tables, address lists, DNS forwarders, interface lists, and filter-chain references.

Readable labels provide display names only. They do not prove ownership or encode reversible logical IDs. Already managed local objects are not candidates. Conflicting local desired state blocks adoption rather than being overwritten.

Reconstruct routing source selectors, manual IP/domain targets, egress, route mode, DNS transport, activation, and effective ordering. Reconstruct access source prefixes, targets/internet scope, activation, and weekly time windows. Prefer equivalent independent rules if the original logical grouping cannot be proved. Do not guess original UUIDs or merge based on names.

Read domains from DNS Static; never turn transient resolved addresses into permanent domain targets. Freeze terminal address evidence as IP/CIDR selectors. Preserve normal external interfaces/tables as references. Reject dependencies whose ownership, meaning, or safe treatment is ambiguous, including shared objects used outside the selected scope when replacement cannot preserve those users.

Compile the reconstructed graph using existing generators. Compare supported semantic structure after normalizing generated identifiers, defaults, runtime-only fields, and references. Preserve effective order relative to relevant outside rules; block cases the current generator cannot represent. Every effective field and every selected owned object must be explained. Unsupported structures block the whole scope, with specific reasons.

## Interfaces and storage

Under /api/policy-routing/devices/{deviceID}:

- GET /adoptions: read-only candidate scan with scope, counts, warnings, blockers, and pending/completed adoption status.
- POST /adoptions/plans: select a scope and generate a read-only adoption preview. Return the existing plan envelope with plan ID/hash and exact operations.
- POST /adoptions/{id}/retry: re-read an interrupted adoption and create a new preview of its remaining work; never mutate RouterOS directly.

Reuse existing plans/{planID}/apply and jobs endpoints for confirmation and progress. The backend resolves object membership from its own scan; clients cannot supply arbitrary cleanup IDs. Apply requires the exact preview hash and the old-manager-stopped acknowledgement.

Add one small device-local adoption record holding source scope, selected static object snapshots, new policy IDs, approved intent, stage/progress, and last error. Persist imported policy rows and this record in one SQLite transaction after approval, using existing validators and transaction helpers. Keep routing and access applied revisions separate. This is a retry journal, not a second policy model or a long-lived identity alias system.

Use stable new IDs from the approved proposal through all retries. A restart must retain pending status and must not silently launch mutations. Preserve the record after completion for outcome/idempotency reporting; ordinary reconciliation no longer consumes its mapping.

## Execution and exact cleanup authority

1. Under the existing device write gate, revalidate local revisions and the relevant RouterOS static configuration, references, and order against preview. Dynamic DNS members and counters are not configuration drift.
2. Atomically save the imported desired graph and adoption intent. Block ordinary policy mutations and automatic routing/access/source synchronization for this device while the adoption is unfinished; monitoring continues.
3. Stage new dependencies and inactive replacement rules; read back completeness. Preserve routing/access execution-domain boundaries and validate cross-domain DNS precedence.
4. Perform only the approved activation/deactivation and placement steps, preserving disabled policies. A short transition is accepted; do not advertise atomic RouterOS transactions or zero downtime.
5. Verify replacements, then clean old objects by exact menu/ID plus expected static fields from the journal. Delete shared foundations last, after reference checks. Never delete by scope/prefix alone.
6. Verify final state, advance the respective applied revisions, mark adoption complete, and release the ordinary synchronization hold.

Do not add old foreign objects to ordinary owned/stale matching. Adoption's reviewed operation list is the narrow exception that authorizes their replacement and cleanup.

## Failure and retry

Before cutover, failure leaves old rules active and retains any staged new IDs. During cutover or cleanup, stop at the recorded phase and report actual progress; never continue cleanup after a failed prerequisite. Persist step intent before the corresponding RouterOS mutation, then read back on retry to resolve whether a timed-out mutation occurred.

Retry compares actual state with original snapshots and already verified replacement state, reuses imported IDs, and returns a fresh plan requiring review. An unexpected external change blocks retry with the affected object identified. Missing old objects are acceptable only when journal/read-back evidence establishes that the planned deletion already occurred. A failed adoption retains its synchronization hold; retry is the supported completion path, not an unreviewed automatic rollback.

FastTrack remains foreign configuration: reuse existing compatibility analysis and preview its required adjustments. Do not invent old-manager rollback snapshots; use the adoption-time state as the baseline. The previous manager must be stopped by the operator. No distributed controller coordination is added.

## UI

Add adoption entry points to both policy UIs. Reuse preview/job components and existing API parsing conventions. Flow: scan groups -> preview one complete group -> acknowledge old manager stopped -> apply -> existing policy list. Show missing-metadata snapshot limitations before confirmation. On failure expose recheck/retry and the blocked-sync state. Do not add a second policy editor.

## Compatibility and delivery

New tables are additive; existing policy types and normal ownership behavior remain stable. Update the ownership specification to document the explicit reviewed adoption exception when implementation lands. Deliver within one work branch/Draft PR. This preparation checkpoint changes only task documents; no migrations or application changes are performed now.
