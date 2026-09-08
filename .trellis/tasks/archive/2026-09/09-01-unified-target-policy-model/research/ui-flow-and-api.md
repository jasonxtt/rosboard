# UI flow and API needs

## Product rule

This refactor starts from the user mental model, not from database tables:

```text
Who
+
What destination
+
Action
```

The UI therefore exposes three real concepts only where users can understand them:

- Target Library — reusable domain/IP destination lists
- RoutingRule — who + targets + egress
- AccessRule — who + targets/internet + block

Egress remains an advanced routing resource describing how traffic exits; it no longer owns destination lists.

## 1. Target Library

### Page structure

```text
目标列表

[域名列表] [IP 列表]

搜索...                         + 新建目标列表

名称             来源       内容          更新状态      使用情况
OpenAI Domains   URL        28 个域名      正常          2 条规则
Telegram IPs     手动       6 个网段       正常          1 条规则
```

The page is device-scoped because RouterOS projection and existing policy/access state are device-scoped, even though the conceptual list is shared between RoutingRule and AccessRule inside that device. Preset-backed rows are intentionally hidden from these primary “my lists” tables; the Application catalog is opened from a rule TargetSelector instead.

### Create flow

The first choice is source, not consumer:

```text
新建目标列表

来源
[应用] [URL] [上传] [手动添加]
```

For URL/upload/manual, kind is explicit and immutable after creation:

```text
类型
● 域名列表
○ IP 列表
```

The current source preview UX should be reused:

- parse before save
- show valid count
- show ignored counts
- show sample rows
- show fetch/upload errors
- save only from a valid preview when content changed

### Application preset path

Application is a creation shortcut, not a distinct enforcement target:

```text
选择应用
搜索应用...

视频
☐ YouTube
☐ Netflix

通讯
☐ Telegram
```

After choosing one application:

```text
YouTube
179 个域名 · 3 个 IP 网段

☑ 使用域名
☐ 使用 IP 网段

IP 网段可能包含共享基础设施，可能扩大匹配范围。
```

If both kinds are selected, create/reuse two ordinary TargetLists:

```text
YouTube Domains   kind=domain sourceType=preset
YouTube IPs       kind=ip     sourceType=preset
```

Do not introduce `kind=mixed`.

## 2. Shared Target selector

RoutingRule and AccessRule should reuse the same target-selection interaction, not necessarily one generic Rule form.

The selector has three sections:

```text
访问什么

应用
[搜索应用...]  YouTube  Telegram  Netflix ...

我的域名列表
☐ OpenAI Domains
☐ Work SaaS

我的 IP 列表
☐ Company Networks
☐ Telegram IPs

+ URL   + 上传   + 手动添加
```

Selecting an ApplicationPreset resolves to one or two normal TargetList IDs. Quick-add actions create a TargetList in the library and immediately select it.

The rule payload stores only TargetList IDs. It never stores `applicationIds` as a separate enforcement contract.

Application cards have one selectable body and an independent kind control:

```text
YouTube                         域名 ▾
```

Selecting the body toggles the application. The kind menu offers `Domain`,
`IP`, and `Domain+IP`; it cannot leave a selected application with no kind.
When both kinds exist, Domain is selected initially and IP is opt-in. If the
preview has no domains, IP is the initial kind. Existing rules reconstruct the
card and kind state from their hidden preset-backed TargetList IDs.

The selector's “my domain lists” and “my IP lists” sections filter out
`sourceType=preset`; preset-backed rows remain available only for ID/name
reconstruction and are never duplicated as ordinary library choices.

## 3. Shared Subject selector

The visible choices are deliberately small:

```text
谁的流量

● 全部设备
○ 指定设备 / IP / CIDR
```

When selected scope is used:

```text
设备
[搜索设备...]
☑ Living Room TV
☑ Child iPad

手动地址
+ 10.0.0.25
+ 10.0.20.0/24
+ 2001:db8:100::/64
```

Device selection should preserve the current Access Control behavior:

- auto-follow uses stable terminal/MAC identity when available
- fixed exact IP remains available as an advanced choice
- last confirmed address projection survives temporary absence until contradictory identity evidence exists

Manual IP and CIDR are independent of terminal identity. The backend may normalize exact IP to `/32` or `/128`, but the UI should preserve a simple exact-IP presentation when appropriate.

Do not expose a general boolean matcher builder.

## 4. RoutingRule create/edit flow

Restore the mature step-by-step wizard and adapt its existing discovery and
preview components to the canonical RoutingRule/TargetList model. Do not use a
single page that mixes exit mechanics, source scope, and target selection.

### Step 1 — 出口配置

Reuse the old Egress draft/discovery flow for existing RouterOS resources:

```text
WAN/interface discovery · physical/point-to-point interface · next-hop
IPv4/IPv6 enable/config · automatic gateway discovery · manual gateway
advanced settings · DNS upstream · Fake DNS Alias
failure/route mode · routing table · RouterOutput
```

Only discovered interfaces, point-to-point links, or explicit next-hop values
are selectable. The flow does not create RouterOS interfaces and does not
introduce a second Egress persistence model. The old user-facing shared versus
dedicated list-mode setting is removed; desired-state compilation decides
physical grouping.

### Step 2 — 流量入口 + 来源

Reuse `TrafficIngressForm` discovery for interface-list, bridge, VLAN,
WireGuard, VPN/tunnel, and physical interfaces. The source scope is directly
bounded by the selected ingress and offers:

```text
全部入口流量   → in-interface-list=<policy ingress>
指定源 IP/CIDR  → src-address-list=<rule subject>
排除源 IP/CIDR  → in-interface-list=<policy ingress>
                  + src-address-list=!<excluded subject>
```

“排除源 IP/CIDR” is disabled without a valid TrafficIngress and is rejected
by the backend as well. The specified-source mode does not add an ingress
matcher merely because an ingress was configured; its final mark-routing rule
must retain a source/direction guard for return-flow safety.

### Step 3 — 访问目标

Use the shared TargetSelector. At least one target is required. Applications
default to Domain, with explicit IP opt-in, and the selector shows only
user-managed domain/IP lists in its ordinary list sections.

### Step 4 — 预览并应用

Before saving/applying, show a ChangePlan-style preview containing:

```text
策略名称 · IPv4/IPv6 · interface/point-to-point/next-hop
TrafficIngress · source mode · included/excluded addresses
application names + each Domain/IP choice · custom TargetLists
DNS upstream · route/failure mode · RouterOutput
blockers/warnings · final RouterOS create/patch/move/delete plan
```

Only an explicit apply from this preview persists/applies the complete change.

The preview boundary is a proposal, not an early save. The wizard sends the
draft Egress/TrafficIngress/RoutingRule and any lazily previewed preset
selection to `POST /api/policy-routing/plans`. The server projects that
proposal through a read-only overlay and returns a plan hash, base desired
revision, desired hash, and dependency snapshot. Back/Close leaves SQLite
unchanged; scheduled source refresh and other GenerateAndApply calls can only
read the previous canonical graph. `POST /plans/{id}/apply` must carry the
exact plan hash and pass the revision/dependency checks before one SQLite
transaction commits the proposal and starts the reviewed apply job.

## 5. AccessRule create/edit flow

Reuse the same first two concepts, but keep AccessRule as its own product entity.

### Step 1 — 谁的流量

Shared Subject selector.

### Step 2 — 阻止什么

```text
● 整个互联网
○ 指定目标
```

For specified targets, show the shared Target selector.

### Step 3 — 确认

Action is fixed:

```text
阻断
```

Do not add `action=route|deny` to a generic Rule type.

The existing time-control area can remain a later AccessRule-specific extension; it is not part of TargetList/RoutingRule abstraction.

AccessRule does not inherit RoutingRule's TrafficIngress or excluded-source
semantics. It keeps its own Subject and Internet/target enforcement contract,
but uses exactly the same Application card and Domain-first kind interaction.

## 6. Rule list pages

### Routing rules

```text
策略路由

名称        谁               目标                  出口        状态
儿童视频    Child iPad       YouTube / Netflix     WAN-Xray    已启用
工作设备    Work Mac         Company SaaS          WAN2        已启用
```

### Access rules

```text
访问控制

名称        谁               阻止                  状态
儿童睡眠    Child iPad       Internet              已启用
禁用短视频  Child iPad       TikTok / YouTube      已启用
```

The user should never have to infer policy rules from an Egress card containing owned sources.

## 7. Minimum canonical API contracts

The exact route names are a technical design concern, but the UI requires the following resources.

### TargetList

A shared, device-scoped endpoint should exist outside the policy-routing-owned `/sources` contract. Recommended canonical route:

```text
/api/target-lists/devices/{deviceID}
```

Minimum operations:

```text
GET    /target-lists
POST   /target-lists
GET    /target-lists/{id}
PUT    /target-lists/{id}
DELETE /target-lists/{id}?revision=...
GET    /target-lists/{id}/rules
POST   /target-lists/{id}/refresh
POST   /target-lists/url/preview
POST   /target-lists/upload/preview
POST   /target-lists/manual/preview
```

ApplicationPreset selection may use:

```text
GET  /application-presets
POST /application-presets/{id}/preview
```

or an equivalent small endpoint. The client should not crawl GitHub directories itself.

### RoutingRule

Recommended resource:

```text
/api/policy-routing/devices/{deviceID}/rules
```

The current policy API uses a different device-addressing style; implementation may preserve existing routing prefix conventions if changing the top-level shape would create unnecessary churn. The important contract is a first-class RoutingRule resource rather than source ownership inside Egress.

### AccessRule

The existing AccessRule endpoint remains. Its canonical payload changes from:

```text
targetScope=sources + sourceIds[]
targetScope=applications + applicationIds[]
```

to:

```text
targetScope=targets + targetListIds[]
targetScope=internet
```

Compatibility fields may be accepted temporarily during migration, but the new UI must use the canonical target contract.

## 8. API object fields required by UI

### TargetList

```text
id
name
kind = domain | ip
sourceType = url | upload | manual | preset
sourceRef/presetId when applicable
url when applicable
schedule
status
activeVersionId
lastGoodVersionId
etag / lastModified when applicable
nextRunAt
revision
counts
versions summary
usage summary (routing/access rule count)
```

`egressId` is not part of the canonical TargetList response.

Legacy `enabled`, `pendingDeletion` and `applied` fields may remain internal during migration; the final UI should not use them to express consumer ownership.

### RoutingRule

```text
id
name
subject
targetListIds[]
egressId
priority
enabled
revision
status
```

### AccessRule

```text
id
name
subject
targetScope = internet | targets
targetListIds[]
enabled
revision
status
```

## 9. UI migration rule

Do not build the final frontend before the backend model exists.

Implementation order:

```text
finalize these interaction contracts
→ implement canonical backend and migration
→ keep old frontend on compatibility APIs while backend changes
→ switch frontend to Target Library / RoutingRule / new AccessRule contracts
→ remove old source-owned UX
```

This is UI-first design but backend-first implementation.
