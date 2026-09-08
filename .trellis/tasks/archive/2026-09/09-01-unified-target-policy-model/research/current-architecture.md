# Current architecture inventory

## Scope

Read-only inventory for the unified target / rule-model refactor. This records the current code paths that must be reused or migrated; it is not a new implementation contract by itself.

## 1. Current policy source model

`internal/policyv2/model.go` defines:

```go
type Source struct {
    ID                string
    EgressID          string
    Type              string
    Kind              string
    Name              string
    URL               string
    Schedule          string
    Enabled           bool
    ActiveVersionID   string
    PendingVersionID  string
    LastGoodVersionID string
    ETag              string
    LastModified      string
    NextRunAt         time.Time
    Revision          int64
    PendingDeletion   bool
    Applied           bool
    Versions          []SourceVersion
    Counts            map[string]int
}
```

Content kinds are already exactly the two logical Target Library kinds required by the new product model:

- `domain`
- `ip`

Existing source types are:

- `url`
- `upload`
- `manual`

The only missing source type for the target-library product model is `preset` / application preset.

## 2. Persistence that should be reused

`internal/store/policy.go` already owns a complete versioned content store:

- `policy_v2_sources`
- `policy_v2_source_versions`
- `policy_v2_source_rules`

Important existing state that must survive migration:

- stable source ID
- type / kind / name / URL / schedule
- `active_version_id`
- `pending_version_id`
- `last_good_version_id`
- ETag / Last-Modified
- next refresh time
- revision
- version SHA256 / compressed payload / state / error / HTTP status
- parsed rule rows
- counts and diff metadata

The tables are not intrinsically unusable for TargetList. The main product coupling is the `policy_v2_sources.egress_id` column and the code that interprets it as ownership.

A physical table rename is not required to establish the new business model and would create needless migration risk.

## 3. Egress / Source coupling sites

The principal coupling points are:

1. `Source.EgressID` in `internal/policyv2/model.go`.
2. `PolicyRepository.SaveSource` validates a non-empty `EgressID` against `policy_v2_egresses`.
3. `DeleteEgress` clears `policy_v2_sources.egress_id`.
4. `ListSources(ctx, egressID)` supports egress-filtered reads.
5. `BuildDesiredWithAccessOptions` groups enabled sources with `enabledSourcesByEgress`.
6. DNS forwarders, DNS static rules, IP address-list entries and mangle activation are generated from those per-egress source groups.
7. `SourceAutoApplyEligible` / manager logic consults `Source.EgressID` when deciding whether a pending source can be applied.
8. The policy overview API embeds source summaries under each Egress by `src.EgressID`.
9. The policy frontend exposes `egressId` as part of `PolicySource`.

Therefore the correct migration target is not a blind rename. The refactor must first make target data independent, then move the routing association into RoutingRule.

## 4. Existing parser / fetch / preview pipeline

The policy API already exposes three preview flows:

- `/sources/url/preview`
- `/sources/upload/preview`
- `/sources/manual/preview`

They reuse `internal/policy` parsing/fetching code and feed a short-lived preview cache. Saving content creates a new `SourceVersion` and parsed `SourceRule` rows.

Existing supported rules relevant to the new architecture:

- `DOMAIN`
- `DOMAIN-SUFFIX`
- `IP-CIDR`
- `IP-CIDR6`

Existing safeguards / useful behavior include:

- URL fetch limits and validation
- upload handling
- manual parsing
- Clash YAML parsing
- kind-specific domain/IP parsing
- preview samples and ignored counts
- source rule limits
- ETag / Last-Modified
- scheduled refresh
- active / pending / last-good version lifecycle

This pipeline must be reused. Creating a second Target downloader/parser/version subsystem would be a regression in architecture quality.

## 5. Current RouterOS projection

`internal/policyv2/desired.go` currently builds one combined policy/access desired graph.

### Routing domain source

For an enabled domain source assigned to an Egress:

```text
source
→ egress-specific DNS forwarder / transport
→ /ip/dns/static
→ source address-list
→ mangle matching that address-list
→ egress routing table
```

### Routing IP source

For an enabled IP source assigned to an Egress:

```text
source rules
→ IPv4/IPv6 firewall address-list rows
→ mangle matching that address-list
→ egress routing table
```

### Access-control domain/IP source

Access Control currently references the same Source ID. The desired builder can materialize a source even if its routing Egress is disabled or unavailable, and then `accesscontrol.BuildDesired` consumes the source list.

This proves that a domain/IP list already has value independent of routing; the current type simply does not express that product truth cleanly.

## 6. Current physical list sharing

`SourceListName(managerID, deviceID, source)` produces one source-level RouterOS address-list identity. The same physical list can currently be used by both routing and access control.

The new design must not rely on this sharing because domain resolution context may differ:

- routing needs the selected Egress DNS upstream / Fake DNS transport;
- access control uses the access-control DNS context.

Logical target data can remain shared while physical RouterOS projections become consumer-context specific.

No persistent projection/ref-count framework is required: the existing desired/reconcile graph can derive stable object identities and clean stale managed objects from the complete desired state.

## 7. AccessRule and subject capabilities

`internal/accesscontrol/model.go` currently defines:

```text
AccessRule
- id
- name
- targetScope = internet | sources | applications
- sourceIds[]
- applicationIds[]
- enabled
- revision
```

and:

```text
RuleMember
- ruleId
- terminalId
- binding = auto | fixed
- stable MAC anchor for auto-follow
- pinned IPv4 / IPv6
- last confirmed IPv4 / IPv6
```

Useful existing subject behavior:

- terminal identity
- auto-follow by stable MAC anchor
- fixed exact IPv4 / IPv6
- last-known projection
- conflict handling when identity facts change
- per-rule member persistence and revision flow

Missing for the new common Subject selector:

- `all` subject
- manual IP/CIDR independent of a Terminal
- RoutingRule consumer

The minimal shared extraction should occur only when RoutingRule becomes the second real consumer. Do not build a general matcher/subject framework during TargetList foundation work.

## 8. AccessRule persistence

Current tables include:

- `access_rules`
- `access_rule_sources`
- `access_rule_applications`
- `access_rule_members`
- `access_control_state`
- `access_audit`

`access_rule_sources` already stores ordered references to Source IDs and can be semantically migrated to TargetList IDs without copying target content.

The current OAF application relation is additive and separable from source references.

## 9. Current OAF implementation to retire later

The approved-but-not-production OAF work currently adds:

- `internal/applicationcatalog`
- `ApplicationIDs` and `TargetScopeApplications`
- `dns:application:*` desired objects
- `rb_app_*` address lists
- application catalog runtime/configuration
- `ApplicationResolver` using MosDNS observations plus `Catalog.LookupDomain`

The useful idea to preserve is small and generic:

```text
recent MosDNS (client IP, answer IP, domain)
+ domain → application registry
→ traffic attribution
```

The OAF feature.cfg parser/runtime and independent AccessRule application enforcement are not needed in the new model.

## 10. UI inventory

Current policy routing exposes Source as a policy-routing-owned concept and includes `egressId` in `PolicySource`.

The source editor already has solid UX for:

- domain vs IP kind
- URL/upload/manual source type
- preview before save
- rule-count / ignored-rule feedback
- rule inspection
- refresh status

These pieces should be moved/reused in a shared Target Library rather than reimplemented.

Current Access Control already has rule + member UI and OAF application selection. Its application picker should not survive as a separate enforcement target type; the useful search/category interaction can be reused for ApplicationPreset creation/selection.

## 11. Architectural conclusion from inventory

The smallest safe path is:

1. Reinterpret the existing source/version/rule machinery as TargetList content storage.
2. Keep `policy_v2_sources.egress_id` only as a temporary legacy routing association while new Target APIs become canonical.
3. Introduce RoutingRule and migrate that association in the next slice.
4. Change AccessRule source references to TargetList references without copying target data.
5. Add a small ApplicationPreset registry that feeds the same target parser/version pipeline.
6. Replace OAF application enforcement and catalog runtime only after equivalent target-based behavior exists.

This avoids a duplicate data plane and makes the final architecture simpler than the OAF implementation.
