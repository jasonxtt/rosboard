# Local architecture and baseline audit

审计日期：2026-09-01。本文只记录研究结果，不代表本轮已经修改运行时代码。

## 1. 工作区基线

- 当前分支为 `main`，HEAD 为 `d77c6d2`（`chore(task): archive 08-30-source-lists-permanent-deny`）。本地分支相对 `origin/main` ahead 48。
- 工作区存在大量未提交的 Access Control / policy-v2 变更，且这些变更是当前正式基线的一部分，不应清理或回滚。重点包括 `internal/accesscontrol/`、`internal/api/access_control.go`、`internal/policyv2/access_capability.go`、`internal/policyv2/internet_egress.go`、`internal/routeros/write_gate.go`、`internal/store/access_control.go` 及其测试，还有 `web/src/features/access-control/`。
- 已修改的 `.trellis/spec/backend/policy-routing.md`、后端 policy-v2 文件、前端 policy 页面和构建产物也属于现有工作，不在本任务第一轮变更范围内。
- 已创建独立 Trellis 规划任务 `.trellis/tasks/09-01-oaf-application-catalog/`。本轮保持 `planning`，不执行 `task.py start`，不创建提交。

## 2. 当前数据流与职责

当前应用展示链路是：

```text
RouterOS conntrack
  -> internal/service/monitor.go
  -> TerminalConnection
  -> classifyApplication(port/protocol)
  -> ApplicationResolver (optional)
  -> FeatureLibrarySynchronizer.Lookup(domain)
  -> aggregateProtocols / protocol_samples / UI
```

DNS 增强链路是：

```text
MosDNS AuditLogs
  -> MosDNSSynchronizer
  -> dns_observations + durable dns_features
  -> ApplicationResolver cache(clientIP, answerIP)
  -> V2Fly-style recognition.Library
```

关键实现位置：

| 位置 | 当前职责 | 结论 |
| --- | --- | --- |
| `internal/recognition/library.go` | 解析一套 YAML domain/full 规则，按 exact/suffix 返回显示名称，并内置应用/分类名称映射 | 这是第一套独立应用定义系统；应由 Catalog 取代，不能继续作为第二套真相源 |
| `internal/service/feature_library.go` | 每设备拉取 V2Fly `domain-list-community` 风格 YAML、缓存、刷新、Lookup | 是第二套应用定义/同步系统；不能作为新 Catalog 的后门依赖 |
| `internal/service/application_resolver.go` | 每设备把 `(clientIP, answerIP, time)` 关联到 domain，再 Lookup 显示名称 | 可改造成应用归因器，但不应再直接依赖 feature library |
| `internal/service/monitor.go` | 端口/协议分类、TerminalConnection 组装、聚合和历史采样 | 应保留可靠的 Protocol/Service 分类；应用归因只作为独立 enrichment |
| `internal/service/mosdns.go` | MosDNS 同步、原始观察保存、durable DNS feature upsert | 仅提供归因证据；不能成为 Catalog 或 Access Control 的硬依赖 |
| `internal/policyv2/desired.go` | 统一读取 egress/source/AccessRule，生成期望对象 | Application target 必须接入此图，不得另起 planner |
| `internal/accesscontrol/desired.go` | 将 internet/sources 规则和成员地址编译成 address-list/filter/jump | 保持已有目标语义；新增 applications 为同一 AccessRule 模型的加法 |
| `internal/policyv2/manager.go` | 共享 `DeviceWriteGate`、revision/hash、能力预检、scan/diff/reconcile/apply | 应用目标沿用相同事务边界、所有权和审计机制 |
| `internal/policyv2/reconcile.go` | 以 `rb_<8hex>` comment 识别受管对象，做 diff/order/reconcile | 应用对象必须有独立、稳定的 logical ID 命名空间并进入同一 reconcile |
| `internal/routeros/mutation.go` | 菜单白名单、typed CRUD、Access Control 能力 probe | 不要把尚未 probe/readback 的 matcher 当成已支持能力 |

## 3. 旧识别行为的实际问题

`Monitor.terminalConnectionRow` 当前先用 `classifyApplication` 根据端口/协议写入一个应用分类，并把 `ApplicationSource` 设为 `port`、`Estimated=true`；若 Resolver 命中 DNS，则用显示名称覆盖，来源变为 `dns`、`Estimated=false`。这同时混合了三种不同概念：传输协议、端口服务分类和具体应用。`aggregateProtocols` 又以 `connection.Application` 分组，并将 `connection.Protocol` 作为 `Kind`，所以名称和类别是统计主键的一部分。

现有 `classifyApplication` 的可靠价值应保留为服务/协议 fallback，例如 HTTP、网络通讯、文件传输、网络视频、网络下载及 TCP/UDP/ICMP。它不能被命名为应用识别，也不能推出“443 等于某品牌应用”。新模型应将以下字段分开：

- Protocol：TCP/UDP/ICMP 等连接层事实，继续复用现有字段，不新增 Transport 字段。
- Service：由端口/协议得到的粗粒度服务分类。
- Application：Catalog 中有稳定 ID 的具体应用。
- ApplicationSource / Estimated：复用现有字段；MosDNS 命中是 inferred，不新增 Confidence 模型。

现有 `TerminalConnection`、`ProtocolStat` 和 `ProtocolHistorySample` 没有稳定 Application ID、Service 字段。`Estimated=false` 当前被用于 DNS 命中，需要改为 DNS attribution 的 inferred 语义：命中时使用 `ApplicationSource="mosdns"`、`Estimated=true`。不新增 Confidence 模型；`Protocol` 继续是 transport fact，Service 独立表示粗粒度服务。

## 4. MosDNS、DNS evidence 与开关耦合

`ApplicationResolver.refresh` 当前先查限定时间范围的 `dns_observations`，再查整个 `dns_features` 表作为缺失补全。`DNSFeaturesForMatch` 没有时间边界，而 `PruneDNSObservations` 只删除原始观察、保留 durable feature。因此旧 feature 目前可能被当作实时证据无限期使用；收窄后的目标是直接移除这条 realtime fallback。

最小语义修正：

1. 实时归因只按 `query_time` 与现有 per-device evidence/match window 查询 `dns_observations`，并考虑 observation TTL；过期 observation 不参与实时归因。
2. `dns_features` 继续由 ingestion 更新，可用于统计/诊断，但完全移出实时 `(clientIP, answerIP) -> domain` attribution。
3. DNS 归因写入 `ApplicationSource="mosdns"`、`Estimated=true`，明确它是 inferred；不新增 Confidence/expiry subsystem。
4. MosDNS 不可用时，连接采集、Protocol/Service fallback、Catalog 加载和 Access Control 均继续工作；仅 DNS enrichment 缺失。

当前 `NewMonitorManager` 只有在 `device.ProtocolAnalysis` 打开时才创建 FeatureLibrary 和 ApplicationResolver，并在 settings API 中把 MosDNS/FeatureLibrary 子开关强制与该开关绑定。新设计必须解除这个创建和保存耦合：Protocol Analysis 只控制流量统计、sample、UI 和 DNS attribution worker；Catalog 是独立的 core 状态；MosDNS 是可选 attribution enrichment。

## 5. Access Control 基线与兼容边界

当前 `internal/accesscontrol/model.go` 的 `AccessRule` 只有：

```text
TargetScope = internet | sources
SourceIDs   -> policyv2.Source IDs
```

`ValidateRule` 保证 internet 不带 source、sources 至少有一个 source。`policyv2.Source` 代表用户管理的 domain/IP 集合，带版本、schedule、pending/last-good 等策略语义；它不是应用目录，也不能通过伪造 Source 行来承载应用。

当前 `BuildDesiredWithAccessOptions` 已经把 source materialization、terminal member address、internet egress 和 `accesscontrol.BuildDesired` 串在同一 desired graph 中。source list 名称由 `source-list:<managerID>:<deviceID>:<source.ID>` 稳定 hash 得到，Access Control 使用统一的 `rb_` ownership comment、revision/hash、能力预检和共享 `DeviceWriteGate`。

应用目标的兼容约束：

- 新增 `TargetScope=applications` 与 `ApplicationIDs[]`，不改变 `internet`/`sources` 的校验和 RouterOS 行为。
- `ApplicationIDs` 不写入 `SourceIDs`，不创建 fake `policyv2.Source`，不创建第二个 AccessRule API/plan/apply 系统。
- 应用目标编译结果必须并入 `policyv2.BuildDesired` 和 `accesscontrol.BuildDesired` 的同一 plan/reconcile/apply 图。
- 应用对象采用独立 logical ID 前缀（例如 `access-app:` / `catalog-app:`），但仍使用现有 `rb_` comment、managed field、能力预检和 readback。不能污染 source list 的命名空间。
- Catalog 或 RouterOS matcher 不可用时，应用规则生成 blocker/unavailable，整个该设备的 plan 不执行应用相关变更；已有 internet/sources 规则不被改写为“近似应用规则”。

## 6. 数据库、API 与前端迁移面

数据库现状：

- Access Control 有 `access_rules` 与 source 关系表；应用 ID 最小可维护的持久化方式是新增 `access_rule_applications` 关系表，沿用 source 关系的读取、保存、顺序和 revision 机制。
- `protocol_samples` 目前只保存 name/kind、connections、upload_bps、download_bps；不保存 ProtocolStat 的 upload/download bytes。首期不改表、不重写旧行；稳定 Application ID 的历史统计另立任务。
- `dns_observations` 已有 QueryTime/TTL，`dns_features` 已有 FirstSeen/LastSeen/LastTTL；保留两张表，只有前者参与实时归因，不增加 stale subsystem。
- per-device 数据库迁移已有“旧全局 DNS 无法安全归属设备则清除”的明确规则；Catalog 元数据不是 per-device policy source，不应复制到每个设备库。

配置与 API 迁移面：

- 添加进程级 `application_catalog` 配置（外部 URL/文件和 refresh），不再默认 V2Fly URL；last-good 成功快照继续服务两个消费者，失败只更新 status/lastError。
- 旧 `feature_library` 配置只作为兼容读取；V2Fly source/refresh 不再启动运行时。现有 match window 应迁移为 per-device DNS attribution/evidence window，避免把旧字段继续当作第二个应用库配置。
- 保留 `protocol_analysis`/`protocolAnalysis` 的兼容名称也可以；只改变其职责边界，不让它控制 Catalog 或 MosDNS 的可用性。settings response 可逐步增加独立 catalog 状态与 MosDNS attribution 状态。
- Access Control API/前端在同一规则表单中增加 applications target 和应用选择；sources 仍只展示用户 Sources。
- Protocol 页面应展示稳定应用名称/ID、Service、Protocol、ApplicationSource，并保留旧字段以兼容客户端。

## 7. 研究结论

推荐的最小切分是：

```text
applicationcatalog (single immutable process snapshot)
       |                         |
       v                         v
ApplicationAttributor       policyv2 application materialization helper
       |                         |
Monitor/Protocol API        accesscontrol -> existing plan/apply
```

Catalog 只拥有应用定义、支持的 domain signatures 和 snapshot status；Attributor 只增加可选的流量归因；policy-v2 通过少量 helper 将显式选中的应用域名 materialize 后交给现有 Access Control desired graph。

当前没有阻止开始实现的额外产品决策：首期 enforcement 已收窄为 domain-only + RouterOS DNS address-list，OAF 数据不 vendor，许可证问题由外部 URL/文件方案隔离。

## 8. Keep / refactor / migrate / delete matrix

| 模块 | 决定 | 实施边界 |
| --- | --- | --- |
| `internal/model` TerminalConnection/ProtocolStat/history contracts | keep + additive | 保留旧字段，只增加 stable app ID/service；复用 Protocol/ApplicationSource/Estimated；不重写历史 |
| `internal/service/monitor.go` raw collection/rates/conntrack | keep | 原始连接采集、计数、速率和关闭 Protocol Analysis 时的行为保持 |
| `classifyApplication` | refactor | 改成 Service fallback；删除“端口分类就是具体应用”的语义 |
| `internal/service/application_resolver.go` | refactor | 改成 Catalog-backed ApplicationAttributor，保留 per-device DNS evidence 隔离 |
| `internal/service/mosdns.go` | keep + decouple | 保留同步/观察保存；只作为可选 attribution evidence provider |
| `internal/store` DNS observations/features | keep + refactor query | 保留两张表；实时只查询 observations 的 window/TTL，features 只供 ingestion/统计/诊断 |
| `internal/recognition` | migrate then delete | 先迁移必要的 exact/suffix fixture/兼容读取，最终删除 hardcoded label map 和第二套库 |
| `internal/service/feature_library.go` | migrate then delete | 旧 config/cache 只兼容读取；停止 V2Fly synchronizer/default URL 后删除 runtime |
| `config.FeatureLibraryConfig` | migrate | 读取旧 match window 到 DNS evidence window；不再作为 V2Fly 应用源 |
| `config.ProtocolAnalysis` | keep + decouple | 保留兼容开关，只控制 observability，不控制 Catalog/MosDNS/Access Control |
| `AccessRule` / `RuleMember` | keep + additive | 同一规则/成员/revision 模型新增 applications scope，不新增第二套规则 |
| `access_rule_sources` / `policyv2.Source` | keep unchanged | 继续只表达用户 source；应用 ID 不进入其中 |
| `access_rule_applications` | add | 独立保存应用选择和顺序，随 AccessRule revision 管理 |
| `policyv2` desired/plan/reconcile/write gate | keep + extend | 用少量 helper 增加 app objects，复用已有 gate、ownership、readback、order |
| RouterOS `tls-host`/L7/request matcher | defer | 首期不实现；未来需要独立任务 |
| `protocol_samples` | keep unchanged | 继续使用 name/kind/connections/rates；历史不重写 |
| settings/API/UI | refactor | 分离 Catalog、Traffic Analysis、MosDNS attribution；Access Control 仍为同一页面/接口体系 |
