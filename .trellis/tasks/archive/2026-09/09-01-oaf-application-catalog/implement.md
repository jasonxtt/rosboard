# OAF Application Catalog and unified application recognition/access control

本文是用户 review 通过后执行的最小实现顺序。当前任务仍处于 planning；本轮不执行这些代码步骤。

## Slice 1 — Catalog core

**Scope**：建立一个进程级 `ApplicationCatalog`、OAF loader、domain lookup 和 last-good snapshot。

**Expected files/packages**：`internal/applicationcatalog/`、`internal/config/`、manager/bootstrap/status API；不提前改 policy engine。

**Behavior**：

- Application 只包含 stable ID、display name、可选 category 和安全独立的 domain signatures。
- OAF loader 能识别 ID/name/host 等字段；含 request、dict、search 或其他必须共同满足的条件时，不摘 host 伪装成 domain-only。
- exact 优先；否则使用最长 suffix；同等具体程度命中多个不同 Application 时返回 ambiguous/unknown，不按 ApplicationID 猜选。
- 外部 URL/文件是 Catalog 数据源；成功完整解析后替换当前 Catalog，失败只记录 lastError 并保留 last-good。首次成功前为 unavailable。
- status 只记录 source、version、loadedAt/lastSuccess、application/domain count、lastError 和必要时 checksum。icon 不下载、不 vendor、不进入 binary。

**Migration**：无运行时数据库迁移；旧 V2Fly 配置暂不在本 slice 启动。

**Tests / acceptance**：解析、ID/name 解耦、domain normalization、exact/suffix、ambiguous、坏输入、last-good refresh、无 snapshot status；没有 device 或 MosDNS 时 Catalog 也能加载和报告状态。

**Dependency**：planning review 完成后即可开始。

## Slice 2 — Attribution migration

**Scope**：将实时应用归因切换到 Catalog，并把端口分类明确为 Service。

**Expected files/packages**：`internal/service/application_resolver.go`、`internal/service/monitor.go`、`internal/model/types.go` 及对应测试。

**Behavior**：

- `ApplicationResolver` 改为 Catalog-backed attribution；实时只查询 `dns_observations`，受 query time、现有 evidence/match window 和必要 TTL 限制。
- `dns_features` 继续由 MosDNS ingestion 更新，可用于统计/诊断，但从 realtime `(clientIP, answerIP) -> domain -> application` 路径移除。
- 只新增真正缺失的 `ApplicationID`、`Service`；复用现有 `Protocol`、`Application`、`MatchedDomain`、`ApplicationSource` 和 `Estimated`。
- DNS 命中使用 `ApplicationSource="mosdns"`、`Estimated=true`，明确是 inferred；不新增 Confidence 模型，不使用 `ApplicationSource="service"`。
- `classifyApplication` 收敛/重命名为 `classifyService`；443 只能得到 Protocol/Service fallback，不能得到品牌应用。

**Migration**：不改 `protocol_samples` schema；不重写历史。

**Tests / acceptance**：domain hit、ambiguous/unknown、443 fallback、TTL/window boundary、过期 observation、MosDNS disabled/down、multi-device isolation、Protocol Analysis off、旧 API projection。

**Dependency**：Slice 1。

## Slice 3 — Retire V2Fly and separate settings

**Scope**：移除旧应用定义运行时，解除 Protocol Analysis、Catalog 和 MosDNS 的错误耦合。

**Expected files/packages**：`internal/recognition/`、`internal/service/feature_library.go`、`internal/service/manager.go`、`internal/config/config.go`、`internal/api/server.go`、`web/src/lib/types.ts`、`web/src/App.tsx`。

**Behavior**：

- 停止 V2Fly synchronizer、默认 V2Fly URL 和 hardcoded application label map 的运行时路径。
- 旧 `feature_library` 配置/cache 只兼容读取或迁移，不再作为 Catalog 输入；必要的旧 match window 迁移为 DNS evidence window。
- Catalog 是进程级状态，不受 Protocol Analysis 开关控制；MosDNS 是可选 per-device attribution enrichment，也不受该开关强制关闭。
- Protocol Analysis 仍只控制 aggregation、samples、flow categories、Protocol UI 和 attribution worker；旧 raw connection/Protocol/Service/Access Control 行为保持。
- 设置/API/UI 分开展示 Catalog status、Traffic Analysis、MosDNS attribution，并明确 MosDNS 不影响 Access Control。

**Migration**：保留旧 config/db 可加载；不引入新的历史表或 license runtime state machine。

**Tests / acceptance**：旧 config load、旧 cache 不被执行、无 Catalog 启动、refresh failure 保留 last-good、settings round-trip、analysis on/off、MosDNS independent、完整代码搜索确认无 V2Fly runtime/default URL。

**Dependency**：Slices 1–2。

## Slice 4 — Additive AccessRule applications

**Scope**：在现有 AccessRule 中增加 `applications + ApplicationIDs[]`。

**Expected files/packages**：`internal/accesscontrol/model.go`、repository/store schema、`internal/api/access_control.go`、`web/src/features/access-control/`。

**Behavior**：

- 保持 `internet` 和 `sources + SourceIDs[]` 完全不变。
- 新增独立 `access_rule_applications(rule_id, application_id, position)` relation；ApplicationID 不写入 source relation，也不创建 fake `policyv2.Source`。
- 复用现有 rule/member/revision/API/页面；应用选择只引用 Catalog 中的 stable ID。
- 校验三个 scope 互斥，应用 scope 不允许 SourceIDs。

**Migration**：新增关系表并做 additive migration；旧 AccessRule 行原样可读。

**Tests / acceptance**：scope validation、DB round-trip/order/revision、旧 rule compatibility、invalid/empty application IDs、UI source/app separation；现有 internet/sources 测试不变且通过。

**Dependency**：Slice 1（Catalog ID/status）和现有 Access Control baseline；不依赖 MosDNS。

## Slice 5 — Domain application enforcement

**Scope**：把安全 domain-only 应用目标接入现有 policy-v2/accesscontrol desired graph。

**Expected files/packages**：`internal/policyv2/desired.go`、`internal/accesscontrol/desired.go`、现有 RouterOS policy/reconcile tests；只使用少量纯 helper，不建立 compiler framework。

**Behavior**：

```text
ApplicationIDs
  -> Catalog lookup
  -> safe independent domain rules
  -> policyv2 application-owned DNS/address-list objects
  -> ApplicationList[applicationID]
  -> existing accesscontrol.BuildDesired
  -> existing plan/reconcile/apply and DeviceWriteGate
```

- Application list name 稳定基于 `manager/device/applicationID`。
- domain/DNS object logical ID 稳定基于 `applicationID + matcher type + normalized domain`。
- Catalog version/revision 只用于 status/provenance/debug，不参与 list name、managed comment 或 logical identity；refresh 只新增/删除/更新实际变化的 domain objects。
- 含不可丢弃 request/dict/search/其他组合条件的 signature 不进入 domain set；同一 Application 可只控制其安全 domain 部分，但不得声称完整 DPI blocking。
- 复用现有 member projection、`rb_` ownership、AccessRule ordering、capability/readback、plan/reconcile/apply 和 write gate。不新增 FastTrack subsystem。
- 不生成 port/domain 近似来替代 unsupported matcher；不实现 `tls-host`、request、L7、search 或 DPI。

**Migration**：不创建 Source；只增加 application-owned desired objects。

**Tests / acceptance**：exact/suffix domain、multiple selected apps、ambiguous attribution 与 explicit AccessRule selection 的区别、IPv4/IPv6、Catalog refresh no-churn、新增/删除 domain cleanup、unsupported signature、Catalog unavailable、foreign objects、drift/readback、existing source/internet regression。

**Dependency**：Slices 1 and 4；复用 Slice 3 的 settings/status。

## Slice 6 — Migration and regression closure

**Scope**：收口兼容性、边界测试和旧代码删除。

**Expected files/packages**：受前述 slices 实际 diff 影响的 config/store/API/UI/spec 文件；不扩大到新的历史分析系统。

**Behavior / migration**：

- `protocol_samples` schema 保持现状：只使用 name/kind/connections/upload_bps/download_bps；不增加 application/service/transport/source/confidence 列，不重写旧历史。
- `dns_observations`/`dns_features` 数据继续保留，但只有 observations 参与 realtime attribution。
- 完成旧 config、per-device DB、Catalog last-good cache、MosDNS disabled/down、Catalog unavailable、IPv4/IPv6 和 domain overlap 的兼容行为。
- 删除只由本任务遗留的 `recognition`/FeatureLibrary runtime wiring；不删除无关 dirty Access Control 或既有 dead code。

**Tests / acceptance**：完整 Go tests、frontend checks/build、migration tests、fake RouterOS desired/reconcile tests、multi-device runtime smoke；再次搜索 V2Fly runtime、fake Source/application IDs、重复 AccessRule planner 和错误的 `dns_features` realtime fallback。

**Dependency**：Slices 1–5 全部完成。

## Deferred outside this task

- OAF feature database、icons 和 OAF runtime 不 vendor、不编译进 binary。
- `tls-host`、request、L7 dict、search string、DPI 和 FastTrack 专项系统另立任务。
- 稳定 Application ID 的历史统计/schema migration 另立任务。
