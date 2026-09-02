# Unified Target Library and Policy Rule Model

## Goal

把 rosboard 当前“Egress 拥有 Source”和“OAF Application 作为独立 AccessRule target”的产品模型，重构为更直接的用户心智模型：

```text
谁的流量
+
访问什么
+
执行什么动作
```

用户只需要理解三个核心业务概念：

- **目标列表（Target Library）**：可复用的域名列表和 IP 列表；
- **策略路由规则（RoutingRule）**：谁的流量访问哪些目标时走哪个出口；
- **访问控制规则（AccessRule）**：谁的流量访问整个互联网或哪些目标时被阻断。

Egress 只描述出口本身，不再拥有目标列表；Application 只作为创建/选择 TargetList 的 preset 来源，不再成为独立 enforcement 类型。

本次重构必须保留现有用户数据、现有 Source 版本/刷新能力、现有 Access Control 设备跟随能力和当前 RouterOS 行为，并使最终代码明显比 OAF 方案更简单。

## Product Principles

- **用户交互模型优先于后端抽象。** 每个长期存在的业务实体都必须能在 UI 中被用户直接理解。
- **UI 先行设计，后端先行实现。** 先锁定 Target Library、RoutingRule、AccessRule 的交互和数据需求，再实现 backend，最后正式切换 frontend。
- **Target data shared, RouterOS projection may be consumer-specific.** 逻辑目标数据共享，不强迫 RoutingRule 和 AccessRule 共用同一物理 RouterOS address-list。
- **RoutingRule 和 AccessRule 保持独立。** 可以共享 Target selector 和 Subject selector，但不合并为 GenericPolicyRule。
- **最小实现。** 优先复用现有 parser、fetcher、preview、version、refresh、RuleMember 和 desired/reconcile 能力，不为未来可能需求提前建设通用框架。

## Slice 4 revision (authoritative)

Slice 4 is split into two implementation tracks inside this task. This is a
design correction after manual UI review, not a new task and not Slice 5.

### Slice 4A — Frontend/product-flow correction + ApplicationPreset catalog

- Target Library shows only user-managed `manual`, `url`, and `upload` lists.
  Preset-backed TargetLists remain canonical, versioned, refreshable and
  referenceable, but are hidden materializations rather than library rows.
- Application selection happens inside the shared RoutingRule/AccessRule
  TargetSelector. A selected application is labeled “应用规则已准备好” (or
  equivalent), never “已加入目标库”. Existing rules reconstruct their
  application chips from hidden preset TargetList IDs.
- Application presets are sourced from a source-controlled manifest/generated
  catalog covering the valid YAML files under the bm7 Clash tree. Runtime
  reads metadata only; selected previews fetch one YAML lazily through the
  existing URL/Clash parser path.
- Application selection defaults to Domain when available, otherwise IP. The
  legal states are Domain, IP, and Domain+IP; an application cannot remain
  selected with neither kind.
- RoutingRule restores the mature four-stage wizard: (1) exit configuration
  and discovery, (2) TrafficIngress plus all/selected/excluded source scope,
  (3) target/application selection, and (4) readable preview followed by
  apply. Egress persistence remains the existing canonical contract.
- AccessRule uses the same Application/TargetSelector and Domain-first
  behavior while retaining its own subject, Internet target, and time-control
  semantics.

### Slice 4B — Routing desired-state compaction + RouterOS readability

- RoutingRule remains the logical entity; compaction only groups equivalent
  physical connection-mark/final mark-routing execution.
- The execution-group key is conservative: Egress, address family, route
  table, route/failure behavior, enabled execution semantics, match-direction
  boundary, and any other field that changes mark-routing behavior. IPv4/IPv6,
  different Egresses, different route tables, and unproven semantics never
  share a group.
- Ingress-bound and excluded rules may share a group when their final
  mark-routing rule retains the TrafficIngress guard. Selected/source-only
  rules stay dedicated unless an equally safe group-level guard is proven;
  correctness beats fewer Winbox rows.
- `rb_<hash>` ownership identity never changes. Human-readable comments add
  sanitized target/policy/family labels, and preset routing address-list names
  add a stable preset-ID slug without deriving identity from a mutable display
  name. Custom list physical names remain hash-only.
- Duplicate logical-rule elimination, a generic matcher DSL, OAF runtime
  enforcement, and source-owned Egress UX remain out of scope.

### Slice 4C — Policy UX unification & apply-domain isolation

This is the authoritative revision for the next implementation slice. It
stays inside this task and does not create another top-level Trellis task.

- The user-facing Routing page has one first-class concept, **策略路由**.
  It lists complete policies with source, targets, egress, and status. Egress
  is an internal/reusable execution configuration; it is not a separate
  first-level section and its ID is never shown as ordinary UI.
- **新增策略** opens one complete four-step wizard: policy/source, shared
  target selector, egress execution configuration, and readable preview/apply.
  The user submits one policy bundle instead of first creating an Egress and
  then binding a RoutingRule. Existing Egress/RoutingRule/TargetList storage
  remains canonical; no duplicate `RoutingPolicy` table is introduced.
- Egress reuse is deterministic and copy-on-write. Equivalent execution
  signatures reuse one policy-owned Egress; unchanged edits retain the ID;
  changed edits rebind to an equivalent Egress or mutate only an unshared
  policy-owned Egress. Shared Egresses are copied before mutation. Automatic
  cleanup may delete only `origin=policy` Egresses with zero consumers;
  legacy/unproven Egresses are retained.
- Traffic ingress is RoutingRule semantics, not Egress state. `all` and
  `excluded` require ingress; `selected` is source-only and may omit ingress.
  Existing global TrafficIngress is migrated deterministically into each
  applicable all/excluded rule and is no longer the writable runtime authority.
- Routing and Access are independent apply domains. Each has its own desired
  builder, plan/scan/diff scope, pending-version promotions, and applied state.
  Routing operations cannot include Access objects or advance Access state;
  Access operations cannot include routing objects or advance routing state.
- TargetList content remains shared data, but revision invalidation is based
  on actual consumers: unreferenced changes bump neither domain, routing-only
  changes bump routing, access-only changes bump access, and shared changes
  bump both. Access preset selection is part of an Access proposal and is not
  materialized as a standalone shared write before the Access plan.
- The global `policy_changes_pending` blocker is removed only after the domain
  boundaries above are real. A cross-domain blocker is allowed only for a
  concrete RouterOS capability conflict, never merely because the other
  domain has pending state.

#### Slice 4C root-review acceptance corrections

- Normal TargetList save, delete, refresh, enable/disable, and runtime target
  mutation resolve the exact consumer domain from the target's RoutingRule and
  AccessRule references. They must not scan all sources and synthesize a
  `Combined` plan. An unknown operation kind is Routing-compatible by default;
  `Combined` is retained only for an explicit legacy compatibility request.
- A target referenced by both domains is reconciled as two explicit,
  independently scoped plans in stable order. Each plan owns its own scan,
  diff, commit, pending-version promotion, and failure state. If the second
  domain fails, the first successful domain remains applied and the second
  remains pending.
- Refresh batches changed target IDs and domain flags before planning, so one
  refresh cycle cannot accidentally apply unrelated targets or domains.
- Access DNS/list projection identity is `(device, targetListID)`: multiple
  AccessRules share one physical `rb_ac_*` projection and keep rule-specific
  filters. Different Access target IDs with exact/suffix-overlapping domains
  produce `access_domain_projection_ambiguous`; overlapping enabled Routing and
  Access domain projections produce
  `cross_domain_dns_projection_ambiguous` in both domain plans. Neither case
  is represented by a `Combined` plan; shared IP targets remain allowed.
- Egress display names are not required, unique, or part of execution
  identity. Existing edits preserve their current name; new or copy-on-write
  policy Egresses receive a readable generated internal name.
- Stale Access ownership recognizes the `rb_ac_*`, `rbac_*`,
  `rbac_internet_*`, forwarder, and legacy Access comment namespaces. Access
  cleanup may remove these stale objects; Routing cleanup never does.

## Requirements

### 1. Target Library 是一等用户对象

- 每台 RouterOS 设备拥有自己的 Target Library；同一设备内的 RoutingRule 和 AccessRule 可以引用同一 TargetList。
- Target Library 在 UI 中明确分为域名列表和 IP 列表。
- TargetList 至少包含用户可理解的名称、类型 `domain | ip`、来源类型、当前内容数量、当前版本/刷新状态和使用情况。
- TargetList 不属于某个 Egress；用户创建目标列表时不需要先选择出口。
- TargetList ID 在迁移中必须保持稳定，现有 Source 内容不得要求用户重新创建。

### 2. TargetList 统一支持四种来源

- 支持 Application preset、URL、YAML/TXT 上传、手动添加。
- URL、上传、手动添加必须继续复用当前已经验证的解析和 preview 体验：保存内容前先解析预览，展示有效规则数、忽略规则数量、部分规则样本和解析/下载错误；内容变化时才生成新版本。
- URL 来源继续支持当前刷新能力，包括 ETag、Last-Modified、定时刷新、active version 和 last-good version。
- 不重新实现第二套 downloader/parser/version framework。

### 3. Domain 与 IP 保持两个简单 kind

- TargetList 的内容类型只允许 `domain` 和 `ip`。
- 第一版不引入 `mixed` kind。
- 一个 Application preset 同时包含域名和 IP 时，分别形成/引用普通 domain TargetList 与 IP TargetList。
- IP 范围可能覆盖共享基础设施，UI 必须明确提示潜在扩大匹配范围，并允许用户只选择域名部分。

### 4. Application 是 TargetList preset，而不是规则 target 类型

- Application picker 的结果最终必须解析成普通 TargetList ID。
- RoutingRule 和 AccessRule 只引用 TargetList ID；不新增或继续依赖独立的 ApplicationIDs enforcement contract。
- 第一版 ApplicationPreset 使用源代码管理/生成的完整 bm7 Clash YAML manifest；运行时不自动发现或下载整个上游规则仓库。
- ApplicationPreset YAML 只使用当前 RouterOS 有明确等价语义的 `DOMAIN`、`DOMAIN-SUFFIX`、`IP-CIDR`、`IP-CIDR6`。
- 第一版明确忽略不能安全等价执行的 `DOMAIN-KEYWORD`、`PROCESS-NAME`、`IP-ASN` 和其它未支持 matcher。
- 被忽略规则需要在 preview 中可见其计数，但不能被近似成更宽泛的 RouterOS matcher。

### 5. RoutingRule 成为策略路由的一等规则

- Egress 只描述出口配置，例如 interface/gateway、route table、DNS upstream/Fake DNS transport、IPv4/IPv6、failure/route mode、NAT、router-output 行为。
- RoutingRule 至少表达：

  ```text
  ID
  Name
  Subject
  TargetListIDs[]
  EgressID
  Priority
  Enabled
  Revision
  ```

- 同一个 Egress 可以被多条 RoutingRule 引用。
- 同一个 TargetList 可以被多个 RoutingRule 引用。
- 用户不再通过“把 Source 放进某个 Egress”来表达策略路由。

### 6. AccessRule 使用相同 Target 概念

- AccessRule 继续是一等业务规则，动作固定为 Block。
- AccessRule target scope 最终只需要 `internet` 和 `targets`。
- `internet` 保持独立语义，不允许伪装成 `0.0.0.0/0`、`::/0` 或特殊 TargetList。
- `targets` 引用一个或多个 TargetList ID。
- 当前 Access Control 已支持的 domain/IP Source enforcement 必须继续工作，不重新实现另一套 IP enforcement。
- 时间窗口、每日配额等 AccessRule 专属能力不进入 RoutingRule 或 GenericPolicyRule 抽象。

### 7. Subject selector 在 RoutingRule / AccessRule 中统一用户体验

- 用户可选择“全部设备”或“指定设备 / IP / CIDR”。
- `全部设备` 表示进入当前 Policy TrafficIngress 的转发流量，不等同于路由器本机 output。
- 指定设备应最大化复用当前 Access Control RuleMember 能力：stable terminal identity、MAC anchor、auto-follow current IPv4/IPv6、fixed exact IP、last confirmed projection、identity conflict handling。
- 允许手动输入 IPv4、IPv6、IPv4 CIDR、IPv6 CIDR。
- 不提供通用布尔 matcher builder 或表达式 DSL。

### 8. TrafficIngress 保持设备级全局配置

- 现有 TrafficIngress 继续定义哪些 RouterOS ingress interface/interface-list 的流量进入策略系统。
- RoutingRule 的 Subject 在 TrafficIngress 范围内进一步缩小“谁的流量”。
- AccessRule 的设备选择继续使用其现有转发过滤语义，不把 TrafficIngress 强行变成 AccessRule 的用户概念。

### 9. RouterOS projection 按 consumer context 派生

必须明确支持：

```text
domain TargetList + RoutingRule
IP TargetList     + RoutingRule
domain TargetList + AccessRule
IP TargetList     + AccessRule
```

- 同一 TargetList 的逻辑内容共享。
- RoutingRule domain projection 可以使用目标 Egress 的 DNS context。
- AccessRule domain projection 可以使用 Access Control 自己的 DNS context。
- 不要求 RoutingRule 与 AccessRule 共用同一个物理 address-list。
- 不要求不同 Egress 对同一 TargetList 共用同一个物理 address-list。
- RouterOS 物理对象必须继续使用稳定、精确的 rosboard ownership identity，并由完整 desired state 管理生命周期。

### 10. RoutingRule 冲突只在真实重叠时成立

以下两个规则不能仅因为 Target 相同就判断冲突：

```text
iPad + YouTube → WAN2
TV   + YouTube → WAN3
```

不同 Egress 的 RoutingRule 只有同时满足 `Subject overlap AND Target overlap AND Egress 不同` 才是冲突。

第一版只实现真实需要的最小 overlap：all、terminal identity、exact IP/CIDR、domain exact/suffix、IP CIDR overlap；不建设通用集合推理引擎。

### 11. 现有用户数据必须无损迁移

Existing Source → TargetList 至少保留：ID、Name、Kind、URL/upload/manual 来源语义、refresh schedule、revision、active/pending/last-good version、ETag/Last-Modified、next refresh time、parsed rules、version history、counts/diff/error metadata。

不允许要求用户“重新创建列表”。

### 12. Existing Egress/Source 策略必须迁成 RoutingRule

- 迁移后现有策略路由的有效行为不能因为 Source 与 Egress 解耦而消失。
- 迁移必须确定性、可重复执行，不产生重复规则。
- 未分配到 Egress 的 Source 迁移后应成为普通 library-only TargetList，而不是被删除。
- Existing disabled Source、disabled Egress、pending/active versions 等状态必须有明确迁移语义，不能静默丢失。

### 13. Existing AccessRule 必须迁移

- `sources + SourceIDs[]` 必须迁为 `targets + TargetListIDs[]`，并保持引用对象 identity。
- `internet` 语义保持不变。
- 当前 OAF `applications + ApplicationIDs[]` 不允许静默删除：能确定转换为普通 TargetList 的开发/test 数据应确定性转换；无法转换的 legacy rule 必须明确 degraded/disabled 并提示人工处理，而不是扩大匹配或丢规则。

### 14. OAF 不进入生产，但在替代完成前不提前删除

- 当前已经通过 test runtime acceptance 的 OAF implementation 保留为可恢复快照/历史实现。
- 新架构完全替代 application enforcement 与 attribution 之前，不直接删除其代码和数据。
- 最终应退出 OAF `TargetScopeApplications` enforcement、`ApplicationIDs` relation、`dns:application:*`、`rb_app_*`、feature.cfg catalog parser/runtime 和只为 OAF enforcement 服务的配置/API/UI。
- 最终代码复杂度应低于当前 OAF 实现。

### 15. Traffic attribution 保留 Application 信息

- Realtime/traffic views 中的 `ApplicationID`、`Application`、`Service/domain` 信息可以继续存在。
- Application attribution 可以继续基于 MosDNS recent observation 的 domain evidence。
- domain → application 映射改由轻量 ApplicationPreset/domain registry 提供，不要求 OAF Catalog。
- 一个 domain 同时属于多个应用时不猜测 attribution。

### 16. UI flows

#### Target Library

用户有独立页面管理：

```text
目标列表
├── 域名列表
└── IP 列表
```

并可从 Application / URL / Upload / Manual 创建。

#### RoutingRule

推荐三步：

```text
1. 谁的流量
2. 访问什么
3. 怎么走
```

最后选择 Egress 并显示可读摘要。

#### AccessRule

推荐三步：

```text
1. 谁的流量
2. 阻止什么（Internet / TargetLists）
3. 确认
```

RoutingRule 与 AccessRule 共享 Target selector 与 Subject selector 的交互逻辑，但页面/规则实体保持独立。

## Non-Goals

本任务明确不做：GenericPolicyRule、provider/plugin framework、generic matcher engine、policy DSL、generic compiler、通用集合推理系统、动态爬取/同步整个 Application catalog、为未知未来 consumer 建 projection registry、新写一套 downloader/parser/version framework、把 AccessRule 时间/配额抽象到 RoutingRule、用 OAF request/dict/search/DPI 特征做 RouterOS enforcement、在 planning 阶段访问 production `10.0.0.6`、在 planning approval 前修改 runnable production code。

## Acceptance Criteria

### Planning gate

- [x] 完成当前 Source/Egress/version/parser/AccessRule/OAF 真实代码 inventory。
- [x] 完成 Target Library、RoutingRule、AccessRule 和共享 selector 的 UI flow 草图。
- [x] 明确 Target data shared / RouterOS projection consumer-specific 原则。
- [x] 明确现有 Source 内容/版本能力必须原地复用，而不是复制第二套 subsystem。
- [x] 明确 Existing Source/Egress、AccessRule 和 OAF application rule 的迁移要求。
- [x] 明确 ApplicationPreset 使用生成的 bm7 Clash manifest 并复用现有 Clash parser。
- [ ] `design.md` 经 root review APPROVED。
- [ ] `implement.md` 经 root review APPROVED。

### Implementation acceptance

- [ ] TargetList 成为 canonical business/API concept，canonical contract 不含 Egress ownership。
- [ ] Existing Source ID、版本、rules、refresh metadata 在迁移后保持可用且无需重建。
- [ ] Egress 与 RoutingRule 分离；现有 Egress/Source 行为确定性迁移成 RoutingRule。
- [ ] RoutingRule 支持 all / selected subject、domain/IP targets、Egress、priority、enabled。
- [ ] RoutingRule conflict 只在 Subject + Target 真重叠且 Egress 不同时阻断。
- [ ] AccessRule 使用 `internet | targets`，不再以 ApplicationIDs 作为 enforcement target。
- [ ] Existing RuleMember device auto/fixed behavior 被复用，不出现第二套终端跟踪实现。
- [ ] AccessRule domain/IP enforcement 继续通过现有 RouterOS desired/reconcile 路径工作。
- [ ] ApplicationPreset 可以把支持的 BM7 Clash 规则变成普通 domain/IP TargetList。
- [ ] Unsupported preset matcher 被明确忽略并可见，不做不安全近似。
- [ ] Traffic attribution 不再依赖 OAF feature catalog，同时保留 ApplicationID/Application/Service 输出。
- [ ] OAF enforcement/runtime leftovers 在替代完成后安全退出，无法迁移的 legacy application rule 不被静默删除。
- [ ] 最终 frontend 展示 Target Library、RoutingRule 和新的 AccessRule 用户模型，不再要求用户把 Source 绑定到 Egress。
- [ ] 所有 migration/reconciliation 路径 restart-safe / idempotent。
- [ ] targeted tests、`go test ./...`、race、`go vet ./...`、frontend checks（涉及 frontend 时）和 `git diff --check` 通过。
- [ ] implementation root review 通过后才允许部署到 `10.0.0.60` 做 runtime acceptance。
- [ ] 未经用户明确授权不访问 production `10.0.0.6`。
- [ ] 用户 runtime/manual acceptance 后才 commit/archive runnable program changes。

## Notes

Research artifacts:

- `research/current-architecture.md`
- `research/ui-flow-and-api.md`
- `research/migration-and-presets.md`

旧 OAF task `.trellis/tasks/09-01-oaf-application-catalog/` 不是本任务的 implementation context，不在本任务 planning 阶段修改或归档。
