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

- Every new RouterOS rule/object comment uses `rb_<8位小写十六进制哈希>` and may append ` | readable label`. The hash is derived deterministically from the stable logical-object identity; the label is display-only. Do not introduce long installation/device/logical composite prefixes in new comments.
- Ownership is always matched by the complete stable `rb_<8>` identity, never by the `rb_` prefix. A stale canonical identity may only appear as an explicit operation in a generated plan; existing legacy comment formats are read only for exact migration and are never deleted by a broad prefix.
- Apply is idempotent: scan actual state, compare it with desired state, create or patch required objects, activate them, then remove stale managed objects last.
- A saved traffic-ingress selection is represented in RouterOS by one managed aggregate interface list. Create it only while at least one policy egress needs it; remove it after the last egress is deleted while retaining the saved selection in SQLite.
- Disable preserves the egress and its managed objects but disables only that egress's activation, DNS, route, and NAT rules. Shared lists, shared marks, and externally owned routing tables remain available to other policies.
- Delete omits that egress's owned objects from desired state. Shared managed objects are removed only after their last consumer disappears; external objects are never removed.
- Managed comments use `rb_<8位小写十六进制哈希> | readable label`. Ownership matching uses only the exact stable identity from the desired-object mapping, while a label change produces a patch so RouterOS remains readable. Policy routing and access control share this comment convention.
- An `internet` access rule is expanded per address family and per RouterOS default-route egress interface into direct `forward` filters (`src-address-list + out-interface` and `dst-address-list + in-interface`). When a family has multiple egresses, they share one managed RouterOS interface list and the filters use `out-interface-list` / `in-interface-list`, so interface count does not multiply filter rules. All non-disabled default routes across routing tables are considered, including standby routes; PPPoE and other known WAN/tunnel interfaces take precedence over stale local-scope evidence. If a family cannot be proven, the plan blocks without an unconditional drop and returns scanned interface candidates for explicit multi-selection; the backend revalidates selected interfaces before applying.
- Auto-follow access members project every currently observed usable address. IPv6 link-local addresses are excluded because they are interface-scoped and cannot be matched reliably in a forwarded address-list rule.
- `Egress.ListMode` and `Egress.ListName` remain only as legacy migration/storage compatibility. They are not the canonical RoutingRule target-grouping authority and are not exposed as new user configuration; the desired-state Execution Group projection determines safe physical sharing automatically.
- Custom canonical routing TargetLists use hash-stable physical names such as `rb_rt_<hash>`. Preset-backed routing TargetLists may use a stable preset-ID slug for readability, such as `rb_rt_<hash>_<preset>_d` or `_ip`; mutable display-name changes must not change ownership identity.
- Dynamic VPN ingress is included through a selected stable RouterOS interface list. WireGuard and other fixed interfaces may be selected directly; rosboard does not configure the VPN service itself.
- Ordinary saves for an enabled source assigned to an enabled egress automatically
  generate and execute one synchronization job after the desired state is
  persisted; the source editor waits for that job before showing the active
  version.
- The RoutingRule wizard keeps intermediate edits in a read-only proposal
  overlay. Plan generation does not write Egress, TrafficIngress, TargetList,
  RoutingRule, or `desired_revision`; only the exact reviewed plan hash may
  promote the proposal. Approval checks the base desired revision, proposal
  and desired hashes, and referenced Egress/TargetList revisions, then commits
  the bundle and one desired-revision bump atomically before starting the
  RouterOS job. Back/Close and unrelated source refreshes cannot apply the
  unapproved draft.
- Scheduled URL refreshes batch due source updates per device and automatically
  synchronize assigned enabled sources. Unassigned sources and sources whose
  egress is disabled may remain pending until a later eligible save.

### 4. Validation & Error Matrix

- Draft revision or RouterOS fingerprint changed after preview -> reject the stale plan and require a new preview.
- A proposal plan without its exact plan hash, with a changed dependency, or
  with a changed canonical desired revision -> reject it without committing
  the draft.
- Another apply is active for the same device -> reject it; different devices remain independent.
- RouterOS account lacks write permission -> block plan application and direct lifecycle actions.
- An apply mutation fails -> stop, retain desired/pending state, record the failure, and allow a later rescan and retry.
- Deleting one of multiple consumers -> preserve every shared or external dependency still in use.
- Deleting the final egress -> remove the managed traffic-ingress list and its members.

### 5. Tests Required

- Planner/reconciler: create, patch, move, delete, idempotent replay, stale revision, stale RouterOS fingerprint, and injected mutation failure.
- Ownership: legacy stable comments remain recognizable; readable-label changes patch instead of replacing; foreign and other-installation objects remain untouched.
- Lifecycle: enable/disable affects only the selected egress; deleting one shared consumer preserves dependencies; deleting the last consumer cleans managed shared objects and traffic ingress.
- Sources: URL/upload parsing, SSRF and content limits, pending-version promotion only after successful apply, stable preset-list names, hash-stable custom-list names, and rename cleanup.
- Proposal boundary: preview leaves canonical rows and revisions unchanged;
  another GenerateAndApply sees the old graph; approval commits the reviewed
  proposal exactly once and applies that graph.
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

## 7. Slice 4C policy UX and apply-domain boundaries

The Routing product surface is one policy list and a complete policy wizard.
Egress remains an internal reusable execution configuration; it is not a
separate first-level user workflow. The existing `RoutingRule + Egress +
TargetList` aggregate is sufficient, and no duplicate persistent
`RoutingPolicy` entity should be introduced.

RoutingRule owns a narrow per-rule ingress scope. `all` and `excluded` require
an ingress guard; `selected` is source-only and may omit ingress. Existing
device-global TrafficIngress is migration/default compatibility only and must
not remain a second writable runtime authority. Execution groups must include
normalized ingress scope in their grouping boundary.

Routing and Access are separate execution domains. Implementers must use
domain facts, not an operation classifier over a combined desired graph:

- Routing desired/plan/scan/diff owns `rb_rt_*`, routing target projections,
  mangle, routes, routing tables, NAT, execution groups, and routing
  RouterOutput objects.
- Access desired/plan/scan/diff owns `rb_ac_*`, access target projections,
  filter/jump/anchor rules, and internet-blocking interface lists.

`CommitRoutingApply` may advance only policy routing applied revision/hash and
`CommitAccessApply` may advance only access-control applied state. A routing
drift must not enter an access plan, and access pending state must not block a
routing plan. A cross-domain blocker is valid only for a concrete, named
RouterOS global capability conflict; `policy_changes_pending` is not such a
capability and must not be used.

TargetList is shared input, not routing-owned state. Compute its consumer
domains from canonical RoutingRule and AccessRule references. A mutation to an
unreferenced list bumps neither domain; routing-only, access-only, and shared
lists bump routing, access, and both respectively. Plans record the exact
pending TargetList versions they reviewed, and apply promotes only those
versions.

Access ApplicationPreset selection remains inside the Access proposal until
the AccessRule, backing TargetList/version rows, desired graph, and reviewed
apply are committed as one domain-scoped flow. It must not pre-materialize a
shared TargetList and accidentally bump routing state.

### 8. Root-review corrections for Slice 4C

Normal target operations must resolve the exact consumer domain from the
canonical target references. They must not scan unrelated sources or return a
combined plan. `Combined` is an explicit legacy compatibility mode only;
unknown operation kinds use the Routing-compatible path. An exact shared
target operation must use two explicit domain applies, with Routing and Access
plans kept separate and applied in stable order.

Each shared-target domain apply owns its own RouterOS snapshot, desired graph,
diff, commit, and pending-version promotion. If the second apply fails, the
first successful domain remains applied and the second remains pending.
Refresh batches changed target IDs and domain flags before creating these
scoped plans.

Access domain projection identity is one physical `rb_ac_*` target projection
per `(device, TargetListID)`; Access filters remain rule-specific. Distinct
Access target IDs with exact/suffix-overlapping enabled domains must block with
`access_domain_projection_ambiguous`. Enabled overlapping Routing and Access
domain projections must block both domain plans with
`cross_domain_dns_projection_ambiguous`; never widen either blocker into a
`Combined` plan. Shared IP targets remain valid.

Display Egress names are non-authoritative: no required/unique UI field and no
execution-signature input. Preserve an existing name during edits and generate
a readable internal name for new or copied policy Egresses. Access ownership
classification must include `rb_ac_*`, `rbac_*`, `rbac_internet_*`, forwarder,
and legacy Access comment namespaces, while Routing cleanup remains unable to
remove Access objects.

Routing DNS projection capability is evaluated on distinct physical
`(device, egressID, targetID)` projections. Enabled rules sharing the same
Egress and Target share one projection and are allowed. Exact/suffix-overlapping
domain content across different target IDs must block with
`domain_projection_context_ambiguous`, including when the Egress DNS contexts
are equal; overlapping projections across different Egresses use the same
blocker. IP-only projections do not receive this DNS blocker.

Access target projection activity is `TargetList.Enabled` and at least one
enabled AccessRule consumer. A target referenced only by disabled rules emits
no active `rb_ac_*` DNS/address-list projection, while a mixed enabled/disabled
consumer set keeps one shared active projection and preserves each rule
filter's independent enabled state. This activity rule is shared by desired
state and Access/cross-domain projection validators.
