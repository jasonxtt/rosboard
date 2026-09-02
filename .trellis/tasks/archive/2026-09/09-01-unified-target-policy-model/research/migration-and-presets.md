# Migration and ApplicationPreset research

## 1. Migration goal

The migration must preserve existing desired behavior while changing ownership semantics.

Old model:

```text
Egress
  └── Source (domain or IP)
```

New model:

```text
TargetList
   ├── RoutingRule ──→ Egress
   └── AccessRule  ──→ block
```

The TargetList identity and content history should remain stable. The routing association moves; the content does not.

## 2. Existing Source → TargetList mapping

The preferred first-step migration is semantic/in-place, not copy-and-rebuild.

| Existing field/state | New meaning | Migration |
|---|---|---|
| `id` | TargetList ID | preserve exactly |
| `name` | TargetList name | preserve |
| `kind=domain/ip` | TargetList kind | preserve |
| `type=url/upload/manual` | sourceType | preserve |
| `url` | source URL | preserve |
| `schedule` | refresh schedule | preserve |
| `active_version_id` | active version | preserve |
| `pending_version_id` | pending version | preserve |
| `last_good_version_id` | last-good version | preserve |
| ETag / Last-Modified | HTTP refresh metadata | preserve |
| next_run_at | refresh scheduling | preserve |
| revision | optimistic concurrency | preserve |
| source versions | TargetList content versions | reuse rows |
| source rules | TargetList parsed rules | reuse rows |
| `egress_id` | legacy routing association only | keep temporarily, then migrate to RoutingRule |
| `enabled` | global list availability during compatibility | preserve initially; rule enablement becomes consumer-specific |
| `pending_delete` / `applied` | legacy reconcile lifecycle | preserve only until all consumers use canonical rule graph |

The physical table names `policy_v2_sources`, `policy_v2_source_versions`, and `policy_v2_source_rules` may remain during the refactor. Renaming tables provides no product value and would create a larger migration/rollback surface.

## 3. Temporary Egress compatibility seam

During the TargetList foundation slice, the old `egress_id` column remains populated so the current desired builder and old frontend continue to work.

Canonical TargetList APIs must not expose `egressId`.

Legacy Source APIs may continue to expose it temporarily.

This seam is explicitly temporary:

```text
TEMPORARY COMPATIBILITY
policy_v2_sources.egress_id
→ interpreted only by legacy Source / old routing code
→ removed from authority when RoutingRule migration commits
```

Do not create a second generic mapping table merely to hide this column for one slice.

## 4. Existing Egress/Source → RoutingRule migration

Current routing applies all selected TrafficIngress traffic to destination lists owned by an Egress. Therefore the closest semantic conversion is **one migrated RoutingRule per existing Egress**, not one rule per source.

For each non-deleted Egress with one or more associated sources:

```text
RoutingRule
- deterministic migrated ID derived from Egress ID
- name derived from Egress name
- subject = all
- targetListIds = all sources whose legacy egress_id == this Egress ID
- egressId = existing Egress ID
- priority = existing Egress priority
- enabled = existing Egress routing state, with target availability still respected
```

Why one rule per Egress:

- it matches current user intent: one egress currently owns a group of sources;
- it preserves shared connection-mark/routing-table behavior;
- it creates fewer user-visible rules;
- it leaves the user free to split subjects/targets later.

Unassigned Sources become library-only TargetLists.

An Egress with no Source remains an Egress and produces no migrated RoutingRule.

### Disabled source behavior

Existing `Source.Enabled=false` must not be silently lost. During migration, the TargetList retains its availability flag, and a migrated RoutingRule may retain the target reference. A disabled TargetList is not materialized until re-enabled.

After the new rule model is stable, product UX should prefer editing rule target membership or rule enablement for consumer-specific control; the TargetList-level availability switch can remain an advanced/global library state only if still useful.

### Egress fields after migration

The following remain genuinely Egress-owned:

- interface/gateway
- route table
- DNS upstream
- Fake DNS transport
- IPv4/IPv6 family configuration
- failure/route mode
- NAT
- router-output behavior

The following are legacy policy-shape fields and should stop driving the new rule model:

- priority (moves to RoutingRule)
- list mode / list name as user-facing source grouping controls

Physical columns may remain until a later low-risk schema cleanup.

### RouterOutput compatibility

Existing `Egress.RouterOutput` has no end-device Subject. Preserve behavior by interpreting it as:

> also apply the Egress's enabled RoutingRule target set to router-originated output traffic.

Do not invent a fake “router device” Subject.

## 5. AccessRule migration

### `sources`

Old:

```text
targetScope=sources
sourceIds=[A,B]
```

New:

```text
targetScope=targets
targetListIds=[A,B]
```

IDs are unchanged, so the relation can be migrated without copying target content. The existing `access_rule_sources` table may be physically retained and reinterpreted during staged migration; a table rename is optional cleanup, not a blocker.

### `internet`

Remain first-class and unchanged. It must not become a special TargetList containing `0.0.0.0/0` or `::/0`.

### `applications` from OAF

Do not silently drop old OAF application rules.

The OAF implementation was never deployed to production, but the migration should still be deterministic for development/test databases:

1. Before removing OAF catalog support, inspect every `TargetScopeApplications` rule.
2. For each referenced OAF application that exists in the current catalog snapshot, materialize its supported domain signatures into a normal domain TargetList with a deterministic migration identity.
3. Replace the AccessRule application relation with the resulting TargetList ID(s).
4. If an OAF application ID cannot be resolved, do not delete it. Keep the legacy rule disabled/degraded and surface an explicit migration issue until the operator replaces or deletes it.
5. Only remove `ApplicationIDs`, `access_rule_applications`, `dns:application:*`, and `rb_app_*` behavior after no canonical rule depends on them.

This path is intentionally narrow and exists only to avoid hidden data loss. It is not a permanent OAF compatibility layer.

## 6. Subject migration / reuse

Existing AccessRule members are already user identities and should remain stable.

The new shared Subject selector requires:

```text
scope = all | selected
selected terminal members
manual IPv4/IPv6 exact IP or CIDR
```

Recommended minimal implementation when RoutingRule becomes the second consumer:

- extract the existing terminal member normalization/resolution helpers into a small shared subject helper package or equivalent shared code;
- keep AccessRule and RoutingRule as separate rule models and separate persistence;
- store manual exact IP/CIDR as canonical prefixes (`/32` or `/128` for exact IP) separate from terminal members;
- do not create a generic expression/matcher AST.

`all` means forwarded traffic entering the configured policy `TrafficIngress` scope. It does not include router-originated output traffic.

## 7. Minimal conflict semantics

A RoutingRule conflict exists only when all are true:

```text
subjects overlap
AND
targets overlap
AND
egress IDs differ
```

Minimum subject overlap:

- `all` overlaps everything;
- same terminal ID overlaps;
- exact/manual prefixes overlap using `netip` containment/overlap;
- terminal vs manual prefix uses current/last trusted terminal address projection when available;
- unresolved terminal/manual overlap may be an explicit warning/indeterminate case rather than guessed.

Minimum target overlap:

- same TargetList ID overlaps;
- domain lists: exact==exact, exact under suffix, suffix equal/nested;
- IP lists: CIDR overlap using `netip`;
- domain vs static IP list is not treated as a deterministic declarative overlap.

Priority does not make a contradictory different-Egress overlap safe; such a case remains a conflict.

No generic set algebra engine is required.

## 8. RouterOS target projection contexts

Logical target content is shared. Physical address lists are derived by consumer context.

### Domain target + RoutingRule

```text
TargetList(domain)
+ Egress DNS context
→ route projection list keyed by (device, egress, target)
→ /ip/dns/static using egress forwarder/transport
→ mangle activation for RoutingRule
```

### IP target + RoutingRule

```text
TargetList(ip)
+ Egress
→ route projection list keyed by (device, egress, target)
→ /ip/firewall/address-list and /ipv6/firewall/address-list
→ mangle activation for RoutingRule
```

### Domain target + AccessRule

```text
TargetList(domain)
→ access projection list keyed by (device, access-context, target)
→ /ip/dns/static using access DNS forwarder
→ access filter
```

### IP target + AccessRule

```text
TargetList(ip)
→ access projection list keyed by (device, access-context, target)
→ IPv4/IPv6 firewall address-list
→ access filter
```

Different RoutingRules using the same target and the same Egress may share the `(egress,target)` destination projection because the DNS context is identical. Routing and Access projections are not forced to share.

No persistent projection registry/ref-count table is needed in the first version; deterministic logical IDs in the complete desired graph provide lifecycle ownership.

## 9. ApplicationPreset source research

The user-selected reference repository `iZuoShou/bm7_ios_rule_script` is a public fork of `blackmatrix7/ios_rule_script` and contains the Clash rule tree under `rule/Clash/`.

Upstream Blackmatrix7 publishes one application directory/file pattern such as:

```text
rule/Clash/YouTube/YouTube.yaml
rule/Clash/Netflix/Netflix.yaml
rule/Clash/Telegram/Telegram.yaml
```

Observed upstream examples confirm the existing rosboard parser boundary is appropriate:

- YouTube advertises 179 `DOMAIN-SUFFIX` rules, 2 `IP-CIDR`, 1 `IP-CIDR6`, plus a `DOMAIN-KEYWORD` entry that rosboard should ignore in v1.
- Netflix contains ordinary `DOMAIN`/`DOMAIN-SUFFIX` entries plus a large IP-CIDR set and unsupported keyword/process rules. This is a concrete reason to make IP-range inclusion an explicit UI choice with a warning.
- Telegram upstream has historically included rule types such as IP-ASN in some variants; unsupported matcher types should be counted/ignored rather than approximated.

Upstream repository metadata reports GPL-2.0 and also carries additional usage/disclaimer text. First-version rosboard should therefore **not vendor or republish the rule contents**. Keep a source-controlled/generated metadata manifest of valid application YAML paths in rosboard, and download only the selected YAML at runtime through the existing fetcher.

## 10. ApplicationPreset registry decision (superseded by Slice 4 revision)

The original research recommendation was a small hardcoded registry. The
authoritative Slice 4 decision supersedes that recommendation: use a
source-controlled/generated manifest covering valid YAML paths under the bm7
`rule/Clash` tree. This is generation-time input only; runtime must not use a
GitHub directory crawler, remote catalog parser, or whole-catalog download.

Example shape:

```go
type ApplicationPreset struct {
    ID       string
    Name     string
    Category string
    Aliases  []string
    RulePath string
    RuleURL  string
}
```

The implementation may add display-only category, alias, and relative-path
metadata needed by the shared selector.

Recommended first-version source policy:

- use one explicit repository base for all presets (default to the user-selected `iZuoShou/bm7_ios_rule_script` mirror);
- keep the Blackmatrix7 upstream URL in code comments/research metadata, not as an automatic runtime fallback;
- store relative rule paths and derive exact rule URLs with one helper from a
  fixed path/base;
- generate the manifest from the valid tree during development, rather than
  discovering directories dynamically at runtime;
- keep curated categories/aliases as optional metadata, with a generic
  fallback category.

A preset ID such as `youtube` is stable rosboard metadata. It is not an enforcement identity and is not stored in AccessRule/RoutingRule target arrays.

## 11. Preset → TargetList behavior

Selecting a preset runs the same content pipeline as URL sources:

```text
ApplicationPreset
→ existing URL fetcher
→ existing Clash parser
→ supported-rule split by kind
→ preview
→ ordinary TargetList version(s)
```

For a YAML containing both domains and IP ranges:

- “use domains” creates/reuses a `kind=domain` preset-backed TargetList;
- “use IP ranges” creates/reuses a `kind=ip` preset-backed TargetList;
- selecting both yields two TargetList IDs;
- no `kind=mixed`.

Preset-backed lists should persist a stable `presetId` and the resolved source URL needed for audit/refresh. The URL is read-only in the UI.

A deterministic uniqueness rule such as `(device, presetId, kind)` should prevent repeated selection of YouTube from creating duplicate library lists.

## 12. Traffic attribution without OAF

The existing `ApplicationResolver` already separates DNS evidence collection from domain→application lookup.

Keep the evidence logic:

```text
MosDNS observation
(client IP, answer IP, domain, TTL, query time)
→ recent valid evidence
```

Replace only the OAF catalog lookup with a lightweight preset-domain registry
built from supported domain rules of the generated ApplicationPreset manifest.

Attribution output remains:

```text
ApplicationID
Application
Service/domain evidence
```

Ambiguous domain ownership remains “no application attribution” rather than a guess.

The registry used for attribution may cache parsed preset domain data, but it must not become a second enforcement data model; enforcement continues to reference TargetList IDs.

## References

- User-selected Clash rule mirror: https://github.com/iZuoShou/bm7_ios_rule_script/tree/master/rule/Clash
- Blackmatrix7 upstream Clash tree: https://github.com/blackmatrix7/ios_rule_script/tree/master/rule/Clash
- Blackmatrix7 upstream repository/license/provenance: https://github.com/blackmatrix7/ios_rule_script
- YouTube rule example: https://github.com/blackmatrix7/ios_rule_script/tree/master/rule/Clash/YouTube
- Netflix rule example: https://github.com/blackmatrix7/ios_rule_script/tree/master/rule/Clash/Netflix

The counts cited above are research snapshots, not stable API contracts. Implementation tests should use controlled fixtures rather than asserting live upstream counts.
