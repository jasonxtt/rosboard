# OAF Application Catalog and unified application recognition/access control

## 1. Design decisions

### 1.1 One catalog, two consumers

新增一个小的 `internal/applicationcatalog` 核心包，持有进程级、只读的 Catalog。它负责：

- 从配置的外部 URL/文件读取 OAF 数据；
- 提取稳定应用 ID、display name 和安全的 domain signatures；
- 按 exact/suffix 进行 `LookupDomain`；
- 暴露 snapshot status。

它不负责设备、RouterOS、MosDNS、AccessRule、策略 revision 或 UI 状态。应用目录加载一次并由所有设备共享；设备差异只在 RouterOS capability projection、DNS evidence 和 terminal facts。

建议的最小核心形状（概念 contract，不是本轮代码）：

```text
Catalog
  Snapshot{version, source, provenance, applications, domainIndex, loadedAt, lastError}
  LookupDomain(domain) -> Application + normalized signature
  Get(applicationID) -> Application
  Status() -> CatalogStatus

Application
  ID, Name, Category?
  DomainSignatures[]
```

Catalog 在成功读取并完整解析后整体替换；下游不会看到半加载数据。OAF 的 request/dict/search 等首期不用的字段只需被识别为不可安全降级，不建立 generic matcher executor。

### 1.2 Stable IDs and domain matching

ID 采用 namespace：上游 OAF 条目为 `oaf:<numeric-id>`，项目自有条目使用 `rosboard:<stable-slug-or-id>`。显示名称变更不改变 ID。不同 provider 的相同 numeric ID 不相互覆盖。

domain index 保留 exact/suffix 两种规则；匹配先 exact，再选择最长 suffix。如果最高具体程度仍有多个不同 Application，则返回 ambiguous，不按 priority 或 ApplicationID 猜选。只对安全独立的 domain signature 做 DNS attribution。带 request/L7/search 或其他共同条件的 signature 不得被降级为 host-only。

### 1.3 Source and provenance policy

配置增加进程级 `application_catalog` source/refresh/status 入口。没有 source 或进程从未获得有效快照时 Catalog status 为 unavailable；不再默认 V2Fly URL。缓存启动读取成功后可成为当前 snapshot。

每次成功加载都整体替换当前 last-good；下载失败、内容损坏或新版本解析失败只写入 lastError，保留当前 snapshot。snapshot status 记录 source、version、provenance、loadedAt/lastSuccess、application/domain count 和可选 checksum；不建立 snapshot age enforcement policy 或 license 状态机。

在 OAF feature archive 的再分发和派生边界确认前，仓库不包含 OAF `feature.cfg`、名称库或 icon 包，也不把 OAF 数据编译进 binary。Catalog 数据从配置的外部 URL/文件获取。

### 1.4 Attribution contract

把当前 `ApplicationResolver` 改造成 `ApplicationAttributor`（可先保留文件名，先切换依赖）：

```text
Terminal facts + (clientIP, answerIP, at)
  -> recent dns_observations within evidence window/TTL
  -> Catalog.LookupDomain(domain)
  -> application ID/name + matched domain + ApplicationSource/Estimated
```

建议结果字段：

```text
ApplicationID
Application        // current display snapshot, compatibility/UI
MatchedDomain
ApplicationSource  // mosdns when the domain attribution is inferred
Estimated          // true for inferred DNS attribution
```

`TerminalConnection` 和 `ProtocolStat` 只增加真正缺失的 `ApplicationID`、`Service`；既有 `Application`、`MatchedDomain`、`ApplicationSource`、`Protocol` 和 `Estimated` 继续复用。DNS attribution 明确是 inferred，因此命中时 `ApplicationSource="mosdns"`、`Estimated=true`；不新增 Confidence enum，也不使用 `ApplicationSource="service"`。新 UI/API 将 Service 与 Application 分开展示。

`classifyApplication` 重命名/收敛为 `classifyService`，继续依据端口/协议提供 HTTPS、HTTP、DNS、SSH、unknown 等 fallback。只要没有 Catalog domain evidence，就不能填写具体 ApplicationID；443 只产生 protocol/service 信息。Service 不是 ApplicationSource。

### 1.5 Evidence freshness

实时 Application attribution 第一版只读取 `dns_observations`：按 QueryTime 限制在现有 per-device evidence/match window 内，并使用必要的 TTL 约束。`dns_features` 继续由 MosDNS ingestion 更新，可供统计/诊断，但完全不参与实时 `(clientIP, answerIP) -> domain -> application` attribution。MosDNS 没有运行、同步失败或没有 recent observation 时，应用为 unknown，Service/Protocol 仍正常。

第一版优先复用现有 `FeatureLibrary.MatchWindowMinutes` 迁移出来的 per-device evidence window，不新增 stale authority、confidence 或 expiry subsystem。

### 1.6 AccessRule additive model

在 `internal/accesscontrol/model.go` 增加：

```text
TargetScopeApplications = "applications"
ApplicationIDs []string
```

校验保持互斥：

```text
internet      -> SourceIDs empty, ApplicationIDs empty
sources       -> SourceIDs non-empty, ApplicationIDs empty
applications  -> ApplicationIDs non-empty, SourceIDs empty
```

持久化新增 `access_rule_applications(rule_id, application_id, position)`，仿照现有 source relation；不把 app ID 写入 `access_rule_sources`，不建立 fake `policyv2.Source`。API 的 rule payload 以 application IDs 读写，Catalog 负责验证 ID 是否存在/可用。

### 1.7 Existing policy graph reuse

应用目标不能另起应用防火墙服务。最小数据流为：

```text
policyv2.BuildDesiredWithAccessOptions
  -> ApplicationIDs
  -> Catalog lookup + safe domain rules
  -> policyv2 application-owned DNS/address-list materialization
  -> ApplicationList[applicationID] = stable list name
  -> accesscontrol.BuildDesired
  -> existing plan/reconcile/apply + DeviceWriteGate
```

这里只需要少量纯 helper，直接放在现有 `policyv2`/`accesscontrol` 组装代码附近；不新增第三个 policy package、compiler interface hierarchy、provider registry 或第二个 planner。

## 2. RouterOS projection chosen for first slice

第一阶段只支持 domain-only signature 的地址列表投影：

1. 对每个 selected application 建立稳定的 application-owned target list，RouterOS list name 只基于 `manager/device/applicationID`。
2. 通过已验证的 RouterOS DNS/static forwarding mechanism 将安全独立的 domain 精确/后缀匹配的解析地址 materialize 到 target list；使用 IPv4/IPv6 分开的 objects。domain/DNS object logical ID 只基于 `applicationID + matcher type + normalized domain`。
3. 让现有 Access Control member binding/filter/jump 逻辑引用这些 target lists；应用目标仍是 AccessRule 的 target scope，不能被看成 source list。
4. 所有 objects 进入现有 desired/reconcile，comment/field equality/order/readback 与现有 Access Control 一致。

Catalog version/revision 只进入 status/provenance/debug，不进入 list name、managed comment 或 logical object identity。Catalog refresh 后未变的 domain object 保持原 identity；新 domain create、删除 domain cleanup，display name 变化只更新 readable label/comment。

这个方案明确有边界：共享 CDN/IP 可能带来同 IP 多应用归属，DNS 变更有刷新延迟，IPv6 可能与 IPv4 不同；状态必须暴露这些限制。没有 RouterOS DNS upstream/materialization 或能力验证时生成 blocker，不把 MosDNS 的结果直接当作 Access Control 地址，也不让 MosDNS 变成 enforcement 依赖。

`tls-host`、request、dict、search 和 DPI 暂不实现。端口/协议 matcher 即便 RouterOS 能表达，也只作为 protocol/service 信息，不作为具体 app enforcement 的默认降级。FastTrack 的特殊处理留给未来真正实现 `tls-host` 时的任务；domain-only 只复用当前 Access Control 的 rule ordering contract，不新增 FastTrack subsystem。

## 3. Settings, API, and UI split

保持现有 `protocol_analysis`/`protocolAnalysis` 兼容字段，改变其边界而不做无必要的字段重命名：

- Protocol Analysis：控制 aggregation、protocol samples、flow categories、Protocol 页面和 attribution worker 的 observability 输出；关闭时保留原始连接采集、计数、Protocol/Service facts 和 Access Control。
- Application Catalog：进程级 status/source/version/counts；不受 per-device Protocol Analysis 开关控制。
- MosDNS：per-device 可选 enrichment；开关和运行状态独立于 Protocol Analysis。页面明确其只增强流量统计的应用归因，不影响应用访问控制。

旧 `feature_library` 的 source/refresh 配置只读迁移，不再创建 synchronizer。其 match window 迁移为 MosDNS/DNS attribution evidence window；没有必要让旧 V2Fly 名称继续出现在新运行时 status。

Protocol API/实时 TerminalConnection 只增加 `ApplicationID`、`Service`，继续复用 `Protocol`、`Application`、`MatchedDomain`、`ApplicationSource` 和 `Estimated`。`protocol_samples` schema 不在本任务修改，历史继续按已有 name/kind/connections/rate 读取，不重写。Access API 保持同一 rule endpoint，新增 target scope/application IDs。

## 4. Failure and safety rules

| 情况 | Observability | Access Control |
| --- | --- | --- |
| 进程从未取得有效 Catalog | unknown/service fallback，status unavailable | applications target unavailable；internet/sources 不受影响 |
| Catalog refresh 下载/解析失败 | 继续使用当前 last-good，并显示 lastError | 继续使用当前 last-good；没有 last-good 时 applications unavailable |
| Catalog corrupt/签名无法解析 | 不加载该 snapshot | blocker，不复用未知 matcher |
| MosDNS disabled/down | 无 DNS attribution；采集和 service fallback 正常 | 不影响任何 target |
| Protocol Analysis disabled | 不聚合/不写 sample/UI enrichment，raw facts 保留 | 不影响 applications/sources/internet |
| selected app 无 domain-only supported signature | 显示 catalog unsupported | blocker；不降级为 port/tls-host/L7 |
| RouterOS domain materialization/readback 不满足 | 可显示设备 projection unsupported | blocker；不改写 foreign rule |
| 旧 config/db | 兼容读取、增量迁移 | 旧 sources/internet 继续工作，历史不破坏 |

## 5. Compatibility with dirty Access Control work

本设计直接复用当前 Access Control 基线：

- 不改变 source 的版本、schedule、pending/last-good 和 source list hash 逻辑。
- 不改变 internet egress discovery 或 source/member address resolution。
- 不改变 `rb_<8hex>` ownership comment、access revision、capability verifier、write gate、plan hash、order move 和 foreign rule 保护。
- 只增加 `applications` 分支及 catalog-owned objects；应用 blocker 不应导致已有 source/internet object 被“修正”为 app 近似物。
- 现有 Access Control 测试必须在每个 slice 保持通过；应用测试增加独立 fixture，明确检查 `SourceIDs` 为空、list/object identity 不冲突和旧规则结果不变。

## 6. Review boundary

首期 enforcement 已确定为 domain-only + RouterOS DNS address-list。OAF feature database、icon 和运行时均不 vendor，Catalog 只从配置的外部 URL/文件加载；许可证研究仍保留在 research 文档，但不建立 runtime license 状态机，也不阻止开始实现 loader。

`tls-host`、request、dict、search 和 DPI 继续 deferred。除用户对本文 planning artifacts 的 review 外，目前没有阻止 `task.py start` 的技术或产品 blocker。
