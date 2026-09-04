# OAF Application Catalog and unified application recognition/access control

## Goal

在 rosboard 中建立唯一的 `ApplicationCatalog`，将应用定义、稳定应用 ID、安全的 domain signatures、来源/版本和加载状态集中管理，并让两个消费者使用同一份目录：

1. Traffic Attribution / Observability：把连接和 DNS 证据归因到稳定 application ID，或明确显示 unknown/service fallback。
2. Access Control / Enforcement：在同一现有 policy-v2 desired/plan/reconcile/apply 图中，以 application target 作为 AccessRule 的加法目标。

Catalog 不是 `policyv2.Source`，应用 ID 不是 `sourceIds`，也不新增第二套 AccessRule 系统。OAF 只作为 metadata/signature source；rosboard 不实现 OAF DPI/runtime。

## Requirements

### R1. Single catalog and stable identity

- 进程内只有一个 Catalog owner；不再同时维护 V2Fly domain list、`internal/recognition` hardcoded label database 和另一套应用定义。
- application ID 必须稳定且与 display name 解耦。OAF numeric ID 使用 provider namespace（例如 `oaf:<id>`）；自有条目使用明确的自有 namespace。
- 首期 Application contract 只需要 ID、display name、可选 category 和安全的 domain signatures；icon 延后，不下载、不 vendor、不做 asset pipeline。
- provenance、version、loadedAt、lastSuccess、counts、lastError 和可选 checksum 属于 Catalog snapshot，不复制到每个 Application/Signature。
- Catalog 缺失时 Observability 显示 unknown；application AccessRule 无可用 snapshot 时 unavailable/blocker。刷新失败时保留已解析的 last-good snapshot。

### R2. Attribution semantics

- 现有 `TerminalConnection.Protocol` 继续表示 transport fact；新增真正缺失的 `ApplicationID` 和 `Service`，不复制 Transport 字段。
- 端口/协议只提供 Service fallback；443 不得直接变成某个品牌应用。
- MosDNS observation 是 attribution enrichment，不是 Catalog、连接采集或 Access Control 的硬依赖。
- DNS 命中时使用现有 `ApplicationSource="mosdns"`、`Estimated=true`、`MatchedDomain`；明确这是 inferred attribution，不新增复杂 Confidence model，也不把 `service` 当作 ApplicationSource。
- realtime attribution 第一版只查询 `dns_observations`，受 query time、现有 evidence/match window 和必要 TTL 约束；`dns_features` 不参与实时 application attribution。
- Protocol Analysis 开关只控制 observability aggregation/samples/UI 和 attribution worker，不控制 Catalog load、RouterOS policy 或 Access Control。

### R3. OAF boundary and provenance

- rosboard 只实现 OAF `feature.cfg` loader；首期提取稳定 ID、display name 和安全独立的 domain signature。
- loader 可以理解并拒绝/跳过 request、dict、search 或其他组合条件，避免把不完整 signature 错当成 domain-only；不建立 generic matcher framework。
- 不把 OAF feature database、icon 包或 runtime vendor 到仓库，不把 OAF 数据编译进 binary；Catalog 数据通过配置的外部 URL/文件获取。
- provenance、version、license/README 说明和可选 checksum 记录在 Catalog snapshot；不在 runtime 建立 license 状态机。
- 不实现 DPI、packet capture、MITM、proxy、kernel module、OAF runtime 或 HTTP/L7 payload parser。

### R4. AccessRule additive application target

- 新增 `TargetScope=applications` 和 `ApplicationIDs[]`（具体 API 命名可兼容现有字段风格）。
- `internet`、`sources` 的既有校验、source IDs、source list materialization 和 RouterOS 行为不变。
- `applications` 不得携带 `SourceIDs`，也不得创建 fake `policyv2.Source`。
- 应用目标使用现有 AccessRule revision、member binding、desired graph、`DeviceWriteGate`、`rb_` ownership 和 reconcile；不另建 planner/apply 系统或 generic compiler framework。
- 第一阶段只承诺安全独立的 domain-only signatures 的 RouterOS DNS address-list projection；不支持的 signature 不生成近似规则。

### R5. Compatibility and migration

- 旧 V2Fly `feature_library` 配置可被读取/迁移，但不再启动 V2Fly synchronizer，也不默认请求 V2Fly URL；旧 feature cache 不作为 Catalog 输入。
- `dns_observations` 和 `dns_features` 数据继续保留；只有前者参与 realtime attribution。旧 protocol samples 不改 schema、不重写。
- Access application selection 通过独立关系存储（`access_rule_applications`），不污染 `access_rule_sources` 或 Source 数据。
- 旧客户端继续读取已有 display name/protocol 字段；首期 realtime API/UI 增加 application ID 和 service，不扩大历史数据库范围。

### R6. Settings/API/UI

- 设置页独立显示 Catalog status、Traffic Analysis 和可选 MosDNS attribution；文案明确“MosDNS 只增强流量统计中的应用归因，不影响应用访问控制”。
- Catalog status 不应伪装成 per-device Source；每设备只显示该设备的可用性/归因状态。
- Protocol page 能区分 application、service、protocol 和 `ApplicationSource`，并能显示 unknown/legacy history。
- Access Control 表单在现有规则体系中增加 applications 目标和 Catalog-backed application picker；sources 仍只列用户 Sources。

## Non-goals

- 不实现 OAF/OpenWrt runtime、DPI、抓包、代理、MITM、kernel module 或通用 HTTP/L7 payload parser。
- 不用应用 ID 取代 `policyv2.Source`，不把应用规则伪装成 source rule。
- 不在本任务中重写历史显示名，不修改 `protocol_samples` schema，不追求概率评分模型，不引入第二套 policy engine。
- 不支持 `tls-host`、request、L7 dict、search matcher 或 DPI；它们只保留为 loader 的 unsupported/deferred 输入。

## Acceptance Criteria

### Catalog and attribution

- 同一个有效 Catalog 被 Monitor/Protocol API 与 AccessRule application path 使用；代码搜索不存在运行时 V2Fly `domain-list-community` 拉取路径或第二个应用 label map。
- 首次有效 refresh 前 Catalog 为 unavailable；之后 refresh 下载/解析失败时保留 last-good，并在 status 显示 lastError。连接采集和 source/internet policy 始终可用。
- application ID 不因 display name 改名而变化；旧 protocol history 继续按已有字段读取，不重写。
- 仅有端口 443 的连接显示 protocol/service fallback，不被标成任意具体应用；DNS 命中使用 `ApplicationSource=mosdns`、`Estimated=true` 和 `MatchedDomain`，并标注为 inferred。
- realtime attribution 只读取受 query time/window/TTL 限制的 `dns_observations`；`dns_features` 不会进入该路径。
- domain lookup 在 exact 优先、最长 suffix 后，如果最高具体程度存在多个不同 Application，则返回 ambiguous/unknown，不按 ApplicationID 猜选。

### Access Control and RouterOS

- internet/sources 现有测试和行为保持通过；application target 的 source IDs 为空且 application relation 独立存储。
- domain-only 应用目标进入同一 policy-v2 plan/reconcile/apply，使用独立 application ownership，支持双栈和 drift/readback 测试。
- 含不可丢弃 request/dict/search/其他组合条件的 signature 不进入 enforcement domain set；不使用 port/domain 近似替代。
- 应用 list name 只基于 manager/device/applicationID；domain/DNS object identity 只基于 applicationID、matcher type 和 normalized domain。Catalog version refresh 不造成 unchanged object churn。
- 应用规则与成员绑定、rule revision、write gate、现有 capability/readback 和 Access Control order 一致；不新增 FastTrack subsystem。

### Settings and migration

- Protocol Analysis off 不再阻止 Catalog load、MosDNS status 或 Access Control；仍按旧契约跳过 observability aggregation/samples。
- 旧 config/db 能加载；没有 destructive history rewrite；旧 source/internet 规则可继续计划和应用。
- `protocol_samples` schema 保持不变；首期只修正 realtime TerminalConnection/Protocol API/UI 语义。
- API/UI 能分别表达 Catalog status、Traffic Analysis、MosDNS attribution 和 application target。

## Planned implementation order

详细步骤见 `design.md` 与 `implement.md`。本轮只完成 research/design，待用户审阅后才开始 task implementation。
