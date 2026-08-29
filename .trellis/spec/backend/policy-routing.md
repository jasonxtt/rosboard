# Policy Routing

## Scenario: Reconcile domain policy routing to RouterOS

### 1. Scope / Trigger

- Trigger: changes to policy egresses, domain sources, traffic ingress, RouterOS planning, apply, enable/disable, or deletion.
- SQLite stores desired policy state and pending source versions. RouterOS remains the actual runtime state and is rescanned before every plan and apply.

### 2. Signatures

- Policy reads and writes live under `/api/policy-routing/devices/{deviceID}`.
- `POST /plans` returns normalized `create`, `patch`, `move`, and `delete` operations plus blocking issues and confirmations.
- `POST /plans/{planID}/apply` uses the authenticated panel session and the device's configured RouterOS account. It does not request the administrator password again.
- The device RouterOS account must have `write` permission. Read-only accounts are rejected with a device-management recovery path.

### 3. Contracts

- Only objects carrying the exact rosboard installation, device, and logical-object identity are mutable. Never delete by a broad comment prefix.
- Apply is idempotent: scan actual state, compare it with desired state, create or patch required objects, activate them, then remove stale managed objects last.
- A saved traffic-ingress selection is represented in RouterOS by one managed aggregate interface list. Create it only while at least one policy egress needs it; remove it after the last egress is deleted while retaining the saved selection in SQLite.
- Disable preserves the egress and its managed objects but disables only that egress's activation, DNS, route, and NAT rules. Shared lists, shared marks, and externally owned routing tables remain available to other policies.
- Delete omits that egress's owned objects from desired state. Shared managed objects are removed only after their last consumer disappears; external objects are never removed.
- Managed comments use `stable identity | readable label`. Ownership matching uses only the stable identity, while a label change produces a patch so RouterOS remains readable.
- Dedicated address-list names contain a sanitized source-name keyword and a short stable suffix. Renaming a source may replace its dedicated list name but must preserve exact ownership and cleanup.
- New egress drafts default to one shared mark list per egress, named from the egress name as `manual_<egress-name>_lab`; the name is generated and read-only. Dedicated mode remains available as an explicit opt-in, and existing egresses retain their stored list mode during edit.
- Dynamic VPN ingress is included through a selected stable RouterOS interface list. WireGuard and other fixed interfaces may be selected directly; rosboard does not configure the VPN service itself.
- Ordinary saves for an enabled source assigned to an enabled egress automatically
  generate and execute one synchronization job after the desired state is
  persisted; the source editor waits for that job before showing the active
  version.
- The policy wizard defers its intermediate source rebinding writes and
  automatically synchronizes once after all egress, ingress, and source changes
  are saved. Blocked, takeover, and drift-recovery plans retain an explicit
  confirmation flow.
- Scheduled URL refreshes batch due source updates per device and automatically
  synchronize assigned enabled sources. Unassigned sources and sources whose
  egress is disabled may remain pending until a later eligible save.

### 4. Validation & Error Matrix

- Draft revision or RouterOS fingerprint changed after preview -> reject the stale plan and require a new preview.
- Another apply is active for the same device -> reject it; different devices remain independent.
- RouterOS account lacks write permission -> block plan application and direct lifecycle actions.
- An apply mutation fails -> stop, retain desired/pending state, record the failure, and allow a later rescan and retry.
- Deleting one of multiple consumers -> preserve every shared or external dependency still in use.
- Deleting the final egress -> remove the managed traffic-ingress list and its members.

### 5. Tests Required

- Planner/reconciler: create, patch, move, delete, idempotent replay, stale revision, stale RouterOS fingerprint, and injected mutation failure.
- Ownership: legacy stable comments remain recognizable; readable-label changes patch instead of replacing; foreign and other-installation objects remain untouched.
- Lifecycle: enable/disable affects only the selected egress; deleting one shared consumer preserves dependencies; deleting the last consumer cleans managed shared objects and traffic ingress.
- Sources: URL/upload parsing, SSRF and content limits, pending-version promotion only after successful apply, readable dedicated-list names, and rename cleanup.
- Quality: `go test ./...`, targeted race tests, `go vet ./...`, frontend lint/build/audit, deployed health/API/assets checks, and user acceptance before commit.

### 6. Wrong vs Correct

#### Wrong

```go
deleteObjectsWithCommentPrefix("rosboard:")
recreateEverythingFromScratch()
```

#### Correct

```go
actual := scanRouterOS()
desired := buildDesiredState(savedPolicy)
plan := diffExactManagedObjects(actual, desired)
applyRequiredBeforeCleanup(plan)
```
