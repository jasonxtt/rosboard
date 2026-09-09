# Access control scheduled blocking

## Goal

Add recurring weekly time windows to access-control deny rules. When an enabled
rule is inside one of its configured windows, matching traffic is blocked. The
rule remains inactive outside those windows. The UI must say this explicitly as
"Block access during these times" rather than using an ambiguous "specified
time" label.

## Requirements

- Preserve the current permanent-block behavior for existing rules and for the
  `always` schedule mode.
- Support `always` and `weekly` schedule modes. A weekly schedule contains one
  or more weekday/time windows, uses minute precision, and supports multiple
  windows and cross-midnight windows.
- Use the target RouterOS device's local clock and timezone. Do not create a
  rosboard scheduler that toggles RouterOS rules at runtime.
- Persist schedules with existing access-rule revisions, audit records, device
  isolation, optimistic-concurrency checks, and access-proposal transactions.
- Keep old API clients and existing database rows compatible: an omitted
  schedule and migrated legacy rows mean `always`.
- Project schedules to both IPv4 and IPv6 RouterOS firewall filters while
  preserving managed-rule ownership and filter ordering.
- Make removing a schedule remove stale RouterOS `time` fields during
  reconciliation.
- Expose and edit the same schedule contract in Aurora and Compact UI. The two
  entry points may use different presentation CSS but must share canonical
  schedule normalization and validation.
- Keep daily usage quotas, temporary bypasses, calendar exceptions, and
  one-off dates out of this MVP.

## Acceptance Criteria

- [ ] Existing permanent access rules retain identical behavior after database
      migration and a no-op save.
- [ ] A weekly rule can be created, read, edited, and deleted through the API;
      schedule changes update rule/access revisions and audit snapshots.
- [ ] Invalid weekday, time, empty weekly schedule, equal endpoints, overlap,
      or excessive-window payloads are rejected with the existing stable API
      error shape.
- [ ] Cross-midnight windows are normalized deterministically and projected
      into separate RouterOS day/time segments.
- [ ] Target-list and internet-scope rules project correct scheduled blocking
      for IPv4 and IPv6, including disabled rules and multiple windows.
- [ ] Changing `weekly` to `always` removes any old RouterOS `time` matcher;
      drift/reconcile tests cover this regression.
- [ ] Aurora and Compact both expose the same schedule behavior and explicit
      blocking copy, including timezone context from RouterOS where available.
- [ ] Backend and frontend automated checks pass, the embedded UI build is
      regenerated and verified, and the change is reviewed by the referenced
      review conversation before any production deployment.

## Constraints

- The schedule describes blocked windows, not allowed windows.
- RouterOS remains the enforcement point; rosboard stores desired state and
  reconciles it.
- Production deployment to `10.0.0.6` is outside this implementation step and
  requires the repository's backup, health-check, and manual-acceptance gate.
