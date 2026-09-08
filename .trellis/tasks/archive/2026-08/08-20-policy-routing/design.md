# RouterOS 域名策略路由技术设计

> **历史设计（2026-08-27 起不再作为后端实施依据）**：V2 后端采用精简幂等协调设计，见子任务 `08-27-policy-routing-backend-v2/design.md`。

## 1. 状态与设计原则

- 状态：草案，供实现前审阅。
- 需求来源：[prd.md](./prd.md)。
- 本设计不授权写入 `10.0.0.99`，也不启动 Trellis 任务执行阶段。

设计原则：

1. **监控与写管理隔离**：现有 `MonitorManager`、监控账号和只读 API 不承担策略写入。
2. **计划先于执行**：任何 RouterOS 变更都先产生不可变计划；执行前再次验证实际状态指纹。
3. **期望状态与实际状态分离**：SQLite 保存期望状态和 journal，RouterOS 每次操作前重新扫描。
4. **对象级所有权**：评论标识、逻辑 ID、RouterOS `.id` 和字段基线共同证明所有权；仅名称相同不足以证明。
5. **逐步补偿而非伪事务**：RouterOS REST 不提供跨对象事务，因此采用 write-ahead journal、分阶段启用和反向补偿。
6. **设备级串行、跨设备并行**：同一设备只有一个写任务；其他设备监控与策略任务不受阻塞。
7. **复用现有结构但不继续扩大单文件**：后端沿用 `config/routeros/store/api/service` 边界；前端策略功能拆入独立 feature 目录，不继续把完整流程堆入 `App.tsx`。

## 2. 现有架构约束

| 现状 | 设计影响 |
|---|---|
| `internal/service/MonitorManager` 为每台设备维护独立只读 Monitor | 新增独立 `policy.Manager`，不能向 Monitor 注入写方法 |
| 每个非默认设备已有独立 SQLite，连接数为 1 | 策略数据继续进入对应设备数据库；事务短小，下载和 RouterOS I/O 不占用数据库事务 |
| 全局管理员、session 和 app state 位于根 SQLite | 安装级 manager instance ID 保存到根库 |
| RouterOS 客户端当前封装 GET 和少量只读 POST | 在同包增加受限通用 REST mutation 层，但仅 Policy Manager 持有写凭据客户端 |
| `DeviceConfig.RouterOS` 保存监控凭据，`ManagedAccount` 表示现有监控账号 | 增加独立 `PolicyAccess`/`ManagedPolicyAccount`，不得复用或升级监控账号 |
| API 使用 `?device=<id>` 和统一认证、CIDR、same-origin 门禁 | 策略 API 沿用相同设备选择和安全前置处理 |
| React 页面和导航主要集中在 `App.tsx` | 只在 `App.tsx` 接入一级菜单、路由状态和 feature shell；向导、列表、任务组件独立实现 |
| Vite 构建产物嵌入 Go 二进制 | 前端验收必须重建 Go 二进制并确认新 asset hash |

## 3. 总体架构

```text
Browser
  └─ /api/policy-routing/*?device=<id>
       ├─ API auth / same-origin / step-up
       └─ policy.Manager
            ├─ Repository ── device SQLite
            ├─ SourceService
            │    ├─ SSRF-safe HTTPS fetcher
            │    └─ Clash YAML parser
            ├─ Scanner ───── policy RouterOS REST client (read)
            ├─ Planner ───── desired + actual → immutable ChangePlan
            ├─ JobRunner
            │    ├─ BackupWriter
            │    ├─ Executor ─ policy RouterOS REST client (write)
            │    ├─ Verifier
            │    └─ Compensator
            └─ Scheduler / DriftChecker

Existing MonitorManager ─── monitoring RouterOS REST client (read-only)
```

### 3.1 新增包与文件边界

```text
internal/policy/
  manager.go             设备注册、scheduler、runner 生命周期
  types.go               领域枚举和 API-safe DTO
  repository.go          Store 接口，不依赖 SQL 实现
  source_fetcher.go      URL 规范化和 SSRF-safe 下载
  clash_parser.go        YAML 解析与域名规范化
  materializer.go        来源规则 → 唯一 RouterOS 规则/引用
  scanner.go             RouterOS 实际配置快照
  capabilities.go        版本和字段能力矩阵
  topology.go            WAN/LAN/路由/NAT 可证明性分析
  conflicts.go           域名、DNS Static、列表和所有权冲突
  planner.go             不可变变更计划
  ownership.go           comment 编解码与指纹
  jobs.go                持久化设备级队列和恢复
  executor.go            分阶段写入
  rollback.go            反向补偿
  verifier.go            读回、顺序、DNS、路由和计数验证
  adoption.go            手工配置发现与接管
  backup.go              export、对象快照、保留策略

internal/routeros/
  mutation.go            PUT/PATCH/DELETE/command/move 的受限 REST 方法
  policy_types.go        仅策略所需 RouterOS wire types

internal/store/
  policy.go              策略表迁移和 Repository 实现

internal/api/
  policy_routing.go      策略 API、上传、下载、错误映射

web/src/features/policy-routing/
  api.ts                 唯一 fetch/normalization 边界
  types.ts               前端策略 DTO
  PolicyRoutingPage.tsx  页面 shell
  PolicyOverview.tsx
  PolicyWizard.tsx
  SourceEditor.tsx
  SourceRulesTable.tsx
  ChangePlanView.tsx
  JobProgress.tsx
  DriftResolution.tsx
  AdoptionWizard.tsx
```

不建立单次使用的通用框架；上述文件可在实现时按实际体量合并，但职责边界不得反向混入 Monitor 或 `App.tsx`。

### 3.2 进程装配

- `cmd/rosboard/main.go` 在根 Store、MonitorManager 之后创建 `policy.Manager`，并使用同一进程 context 启停。
- API Server 增加 policy manager 依赖。现有测试构造器继续代理到一个完整依赖构造器，避免大面积修改测试。
- Policy Manager 为每个启用且未归档、具有策略凭据的设备创建一个 device controller。
- controller 持有独立 policy REST client、设备 Store、一个 durable runner 和带稳定错峰的 scheduler。
- 未配置策略凭据的设备仍可打开页面并执行只读能力说明，但不能执行策略扫描中需要写账号权限的探测或任何变更。

## 4. 配置与秘密边界

### 4.1 YAML 模型

在 `DeviceConfig` 下新增独立字段，复用监控端点但不复用凭据：

```go
type PolicyAccessConfig struct {
    Enabled        bool   `yaml:"enabled"`
    Username       string `yaml:"username"`
    Password       string `yaml:"password"`
    ManagedAccount *ManagedRouterOSAccount `yaml:"managed_account,omitempty"`
}
```

- RouterOS base URL 始终来自同一设备的 `RouterOS.BaseURL`，避免写账号指向另一台设备。
- 策略账号使用单独前缀，例如 `rosboard_policy_<suffix>` 和 `rosboard_policy_g_<suffix>`。
- 初始化脚本仅设置 `read,write,test,api,rest-api`，不修改现有监控账号或 group。
- 策略凭据 API 只投影 `enabled`、`username`、`passwordSet`、`managed` 和 `cleanupAvailable`。
- 空密码更新表示保留已存密码，与现有设备设置契约一致。
- `config.Save` 原子替换并设置 `0600`。如果启用策略凭据的现有配置文件带 group/other 权限，启动或保存必须失败并指出权限，不得继续带秘密运行。

### 4.2 临时初始化凭据

- 产品默认流程沿用短期、内存态 provisioning session：生成脚本，用户在 RouterOS 执行后由 rosboard 验证并保存专用账号。
- 实验设备临时高权限账号仅可在明确授权的验收步骤中进入内存，不通过 UI 持久化。
- provisioning session 只存哈希索引，15 分钟失效，成功保存后消费；日志中不得输出用户名与密码组合、Basic Auth header 或请求体。

### 4.3 管理员 step-up

- 在 `auth.Service` 增加 `VerifyStepUp(ctx, remoteIP, sessionUsername, password)`。
- 校验 session 对应的当前管理员用户名和 Argon2id 密码，复用登录的并发槽和 IP+用户名限速，但不创建新 session。
- API 不签发可重复使用的 step-up token；密码只与 `planID + planHash` 的单次 apply 请求一起提交。
- 校验通过后立即清空请求 DTO 中的密码引用；日志和错误只报告成功/失败，不记录输入。

## 5. 领域模型与 SQLite

### 5.1 安装级数据

根数据库新增安装元数据：

| 表 | 关键字段 | 用途 |
|---|---|---|
| `installation_meta` | `key`, `value` | 持久保存随机 UUID `manager_instance_id` |

首次启动原子生成 UUID；迁移 rosboard 主机时必须连同根数据库迁移该 ID。

### 5.2 设备级表

所有策略表进入 `Store.OpenDevice(deviceID)` 对应数据库，并显式包含 `device_id` 以延续现有隔离契约。

| 表 | 主要字段与约束 |
|---|---|
| `policy_device_state` | `device_id` PK、LAN scope JSON、ownership state、last scan/health、drift state |
| `policy_egresses` | UUID、name、priority、list mode/name、DNS upstream/fake alias、strict/fallback、router-output flag、enabled、revision |
| `policy_egress_families` | `(egress_id,family)` PK、enabled、WAN interface/gateway、route table、route mode、NAT mode |
| `policy_sources` | UUID、egress、type URL/upload、name、URL、schedule、enabled、active/last-good version、ETag/Last-Modified、next run、revision |
| `policy_source_versions` | UUID、source、SHA-256、compressed raw YAML、counts、diff summary、state、created time |
| `policy_source_rules` | `(version_id,rule_type,domain)` PK；规范化 exact/suffix 规则 |
| `policy_materialized_rules` | UUID、egress、list、rule type、domain、RouterOS `.id`、desired hash；共享模式唯一键 |
| `policy_materialized_refs` | `(materialized_id,source_id,version_id)` PK；引用关系 |
| `policy_router_objects` | logical ID、menu/family、`.id`、ownership owned/reused、identity key、desired/actual hash、managed fields、baseline JSON、last seen |
| `policy_plans` | UUID、kind、desired revision/hash、actual fingerprint、risk、step-up requirement、immutable JSON、expiry/state |
| `policy_jobs` | UUID、plan、state、phase、progress、cancel flag、error、created/started/finished |
| `policy_job_steps` | `(job_id,seq)`、action、target、before/after、inverse、`.id`、status、attempt/error |
| `policy_backups` | UUID、job、filesystem path、hash、created、protected state |
| `policy_audit` | actor、action、logical object、plan/job/version、result、summary、timestamp |

### 5.3 数据存储规则

- 枚举、时间和 JSON payload 在 Store 边界统一编码/解码；API 与前端不能读取数据库 schema。
- `policy_sources.active_version_id` 只有在 RouterOS 验证成功后更新；pending version 失败时不改变 active pointer。
- 原始 YAML 和大型解析结果在数据库事务之外压缩/解析，再用短事务写入。
- 每个成功来源保留最多 10 个非活动完整版本，另保护 active 和 last-good；轮换与来源提交在同一短事务中决定。
- 失败来源只写元数据，不写 raw body。
- 删除来源先完成 RouterOS job，再在事务中删除正文和规则，审计记录只保存计数/哈希。
- 数据库 schema 迁移可重复执行且在事务中完成；旧二进制回滚必须与迁移前数据库一起恢复。

## 6. 所有权与实际状态指纹

### 6.1 comment 格式

所有支持 comment 的自有对象使用可解析、版本化格式：

```text
rosboard:v1;i=<instance-uuid>;d=<device-hash>;o=<object-uuid>;t=<kind>;n=<short-name>
```

- 决策依赖完整 UUID，不依赖截断展示名。
- comment 超长时只省略 `n`，不能省略版本、实例、设备、对象和类型。
- 每次扫描先按已知 `.id` 读取，再按 comment 重定位；`.id` 变化但 comment 唯一时更新映射。
- 名称相同、comment 缺失或只包含旧手工说明的对象均不能自动视为自有。

### 6.2 无 comment 或共享字段

`/ip dns` 单例、DHCP Client `default-route-tables`、DHCP network DNS 等不能通过新增独立对象表达的修改，使用字段增量所有权：

- 保存对象 identity key、被管理字段、修改前值、rosboard 加入成员和应用后值。
- 删除时只撤回记录的成员或字段，并要求当前值仍能与基线+rosboard 增量解释。
- 当前值无法解释时进入 drift，不盲目恢复旧整对象。

### 6.3 指纹

- Scanner 对相关菜单生成 canonical JSON：字段名排序、布尔/数字/列表正规化，并保留有序规则的相对顺序。
- `actualFingerprint = SHA-256(canonical snapshot)`；计划保存生成时的指纹。
- apply 获取设备锁后重新扫描；指纹或 policy revision 不一致返回 `409 stale_plan`，必须重新预览。
- 只影响无关监控字段或计数器的变化不进入结构指纹。

## 7. 来源下载、上传与解析

### 7.1 SSRF-safe HTTPS fetcher

URL 来源使用专用 `http.Client`，不复用 RouterOS client：

1. 只接受 `https`，拒绝 userinfo、fragment、空 host 和异常端口。
2. 对 GitHub `github.com/<owner>/<repo>/blob/<ref>/<path>` 做严格 host/path 识别后转换为 `raw.githubusercontent.com`；普通 URL 不做猜测性重写。
3. 每一跳通过自定义 resolver 解析全部地址；任一结果属于 loopback、RFC1918、ULA、link-local、multicast、unspecified、benchmark、documentation 或其他非公网范围时整跳拒绝，防止混合 DNS 答案绕过。
4. 自定义 `DialContext` 使用已经验证并固定的 IP 建连，同时保留原 hostname 作为 HTTP Host 和 TLS SNI，避免检查后再次解析造成 DNS rebinding。
5. 禁用环境代理；重定向手工处理且最多 5 次，每跳重复完整校验。
6. 请求总 context 为 15 秒；读取 `5 MiB + 1 byte`，多一字节即失败。
7. 接受明确文本/YAML及 GitHub raw 常见 `text/plain`；拒绝压缩炸弹式异常响应、二进制内容和无法解码的 UTF-8。
8. 只在 2xx 且内容校验完成后保存 ETag/Last-Modified；304 直接生成 no-op 结果。

LAN URL、任意 host 文件路径和带认证 URL没有绕过入口。

### 7.2 上传边界

- API 使用 multipart，Server 先通过 `MaxBytesReader` 限制请求，再写入内存或 `DataDir/tmp` 下权限 `0600` 的随机临时文件。
- 不采用用户文件名作为路径；文件名只作为净化后的展示元数据。
- parse/save 成功或失败后都关闭并删除临时文件。
- 原始内容只在通过文件大小、UTF-8 与 YAML 结构校验后压缩保存。

### 7.3 Clash parser

- 使用 `yaml.v3` 解码单个 document；根必须是 mapping，`payload` 必须是 sequence of scalar strings。
- 对 node 数量、标量长度和总规则数设上限，避免在 5 MiB 内构造病态 YAML。
- 规则按第一个逗号拆分并去空格；只接受恰当的 `DOMAIN,<domain>` 和 `DOMAIN-SUFFIX,<domain>`。
- 域名 lowercase、去单个末尾点，并通过 IDNA Lookup profile 转 ASCII 后验证 label/总长度；通配符、空 label、IP literal 和控制字符拒绝。
- exact/suffix 在各自语义内去重；不同语义保持两条。
- parser 返回有效规则、逐类型 ignored counts、有限数量的示例错误和内容 hash，不把全部错误文本塞进 API。
- 有效规则为零或超过 20,000 时整个版本失败。

## 8. Scanner、能力与拓扑分析

### 8.1 扫描菜单

按启用地址族和预检场景批量读取：

- `/system/resource`、`/interface`、地址、interface list/member。
- `/routing/table`、`/routing/route` 只读有效路由视图，以及 `/ip/route`、`/ipv6/route` 可配置对象。
- `/ip/dhcp-client`、`/ipv6/dhcp-client`、PPPoE/WireGuard/相关接口状态。
- `/ip/dns`、`/ip/dns/forwarders`、`/ip/dns/static`、DNS cache。
- IPv4/IPv6 firewall mangle、nat、filter、address-list；routing rules 和 routing settings。
- DHCP server network 及 LAN/DNS 相关对象。

读取采用必要 `.proplist`（RouterOS 支持时）减少 50,000 DNS Static 设备上的响应体。任一必需菜单不可读即阻止相应能力；可选菜单失败只在确实不影响证明时降级为 warning。

### 8.2 能力矩阵

能力不是只按版本字符串决定：

| 能力 | 版本门槛 | 运行时证明 |
|---|---:|---|
| named forwarder | 7.17 | forwarders 菜单与字段可读、可在实验对象上验证写权限 |
| DNS Static address-list | 7.17 方案要求 | `type=FWD`、`forward-to`、`address-list`、`match-subdomain` 字段存在 |
| DHCP `default-route-tables` | 7.18 | DHCP client 返回该字段且 PATCH 能力探测通过 |
| IPv6 family | 分能力 | IPv6 route/firewall/NAT 菜单和目标接口/地址可证明 |
| move/order | 分能力 | 对 rosboard 临时 disabled 规则执行 move 后读回验证 |

版本用于提前给出诊断；字段/API probe 是最终判断。probe 只能创建立即删除、disabled、带自有 comment 的实验对象，且不得在普通只读页面自动执行。

### 8.3 WAN 证明

- 复用现有 `service` 中纯 topology/route attribution 思路，但 Policy topology 是独立纯函数，不读取 Monitor snapshot 作为写入依据。
- 每个候选输出地址族、接口、route source、gateway、immediate gateway、table、distance、active、ECMP、递归链和最终物理出口证据。
- 静态 next-hop 必须能经 `main` 解析；点到点接口必须 running 且非 disabled。
- DHCP 只在 client bound、接口匹配、字段能力符合时自动追加表。
- strict 复用表要求所有可转发等价最优默认路径最终均到目标 WAN；blackhole 不视为转发出口。
- 任意递归环、缺少必要输入、非 main VRF、动态协议控制或无法唯一归因均产生 blocker，而不是猜测。

### 8.4 LAN、防火墙和 NAT 证明

- LAN 候选复用现有 terminal-scope/interface-list 证据，但用户确认后的 policy LAN scope 独立持久化。
- NAT coverage analyzer 检查 chain、action、src/dst scope、out-interface/list、routing mark、顺序和 disabled 状态；仅“看起来是 masquerade”不足以复用。
- Firewall analyzer 生成 `safe`、`missing_coverage`、`indeterminate` 三态，并列出支持结论的规则 `.id`。
- FastTrack analyzer 判断策略 connection mark 是否可能命中 fasttrack；不能证明排除时规划自有 bypass，无法安全定位则 block。

## 9. 物化、冲突与计划生成

### 9.1 规则物化

输入为所有启用来源的 active/pending 规则集合：

- 共享模式 key：`(egressID, listName, ruleType, domain)`。
- 专用模式 key 额外包含 `sourceID`。
- Materializer 计算 desired set、source refs、create/update/delete/no-op；只在 job 成功时交换 active refs。
- comment 展示来源名只是辅助，引用真相在 SQLite。

### 9.2 域名与 RouterOS 冲突

- exact/exact 相同、suffix/suffix 相同或 exact 被另一出口 suffix 覆盖均是 blocker。
- 同出口重复只产生 redundancy 信息。
- 用户 DNS Static 使用确定的 exact/suffix 规则比较；无法静态证明的 regex 只要可能覆盖候选域名即 block，并展示样本。
- address-list、forwarder、routing mark、table 和 comment ownership 冲突逐对象报告。
- 运行时 address-list IP 交集是 warning，Planner 按出口 priority 生成 mangle 顺序。

### 9.3 ChangePlan

`ChangePlan` 是不可变 JSON，至少包含：

```text
planID, deviceID, kind, createdAt, expiresAt
desiredRevision, desiredHash, actualFingerprint
capabilities, blockers, warnings, acknowledgements
summary counts and resource estimate
ordered operations[] {seq, phase, action, menu, logicalID,
                      routerID, ownership, before, after,
                      anchor, verification, compensation}
```

- 交互计划默认 15 分钟失效；失效、revision 或 actual fingerprint 变化必须重新生成。
- blocker 非空不能 apply。
- 防火墙高风险例外、fallback、复用用户列表、接管、强制接管等 acknowledgement 作为明确 code 写入计划；apply 必须逐项回传。
- 计划 hash 覆盖全部操作和确认项；step-up 与该 hash 绑定。
- 定时同步只能生成 `domain_delta` 计划，不能隐式产生 route/NAT/mangle/ownership 等结构操作；发现结构缺失转为 pending review。

## 10. RouterOS REST mutation 层

### 10.1 允许的原语

新增客户端方法只接受预定义 menu 常量，禁止 API 层传入任意 RouterOS path：

- `List/Get(menu, query/proplist)`
- `Create(menu, fields)` → HTTP PUT
- `Patch(menu, id, changedFields)` → HTTP PATCH
- `Delete(menu, id)` → HTTP DELETE
- `Command(menu, command, fields)` → HTTP POST，仅允许 `move`、DNS cache flush、print/export 和 DNS settings set 等白名单命令；代表性域名解析由后续 Verifier 的 DNS probe abstraction 负责，不假定存在 `/ip/dns/static/resolve` REST command
- `SetDNSSettings(fields)` → 仅固定为 `POST /rest/ip/dns/set`，当前只允许 `allow-remote-requests` 字段，不开放通用 `set` path

不提供可由用户输入脚本文本的 `/rest/execute`。所有字段通过 JSON 编码；RouterOS comment/name/domain 不进入 CLI 字符串。

### 10.2 HTTP 行为

- policy client 继承监控端点和 TLS 设置，使用独立 Basic Auth。
- 每请求 context deadline；返回状态、受限 RouterOS message/detail 和 path，但不得包含认证或完整敏感 body。
- DELETE 成功可能为空 body，client 不强制 JSON decode。
- create/patch 读回完整对象并提取 `.id`。
- 429/5xx/网络错误可在当前 job step 内有限指数退避；4xx 配置错误不自动重试。
- 大规则集使用小型自适应 worker pool（初始 1，最多 4），仅并行互不依赖的 DNS Static create/delete；结构对象、顺序和补偿保持串行。
- 每个并行 item 仍有独立 journal seq 和状态；RouterOS 延迟、错误率或 free-memory 恶化时降回单并发。

### 10.3 顺序管理

- 计划使用稳定 `.id` anchor 和相邻自有对象，不保存易漂移的数字索引。
- 创建 firewall rule 时先 `disabled=yes`，再通过白名单 `move` 放到目标 anchor 前，读回全部相关顺序验证后才启用。
- RouterOS 版本的 move 参数差异由 mutation adapter 封装，并在 `10.0.0.99` integration test 确认；无法可靠 move 时结构计划阻止。

## 11. 执行、提交和回滚

### 11.1 状态机

```text
queued → reconciling → backing_up → staging → ordering → activating
       → flushing_cache → verifying → committed
                      ↘ rolling_back → rolled_back
                                      ↘ rollback_failed
```

终态：`committed`、`rolled_back`、`rollback_failed`、`cancelled_before_write`。

### 11.2 write-ahead journal

每一步在 RouterOS 调用前写入 `prepared` journal，包含 before、after 和 compensation；调用成功后写 `.id` 与 `applied`。进程在两次写之间崩溃时，重启 reconciliation 通过 before/after/ownership 判断该步实际状态，不能直接重放。

### 11.3 安全 staging 顺序

初次/结构应用：

1. 获取设备 runner 所有权并再次扫描、校验 plan。
2. 生成并校验 export + affected-object snapshot。
3. 新建 table、routes、interface list/member、forwarder 等基础对象；可 disabled 的对象先 disabled。
4. 建立 DNS output mark/DNAT、NAT、防火墙保护、FastTrack bypass，定位并验证顺序。
5. 分批创建 disabled DNS Static。
6. 按依赖启用 route/plumbing、DNS Static，最后启用 LAN business mangle。
7. flush DNS cache并执行验证。
8. SQLite 短事务提交 active version、object mappings、revision、audit。

域名增量：先创建新增规则，验证后停用/删除移除规则，再 flush 并执行后续 Verifier 的 DNS probe。来源迁移到另一出口属于结构操作：旧规则停用、受控清理可证明自有的动态列表、启用新规则、重新探测，接受短暂不可用但不允许同时错误出口泄漏。

### 11.4 删除和补偿

- 删除优先先 disable/tombstone，验证行为已移除后再物理删除；这样失败时可重新 enable。
- create 的补偿是删除该 `.id`；patch 只恢复被改字段；move 恢复原相邻 anchor；delete 的补偿按 snapshot 重建并恢复位置，更新新 `.id`。
- 对共享字段只撤回 rosboard 增量；当前值 drift 时停止补偿并进入人工恢复。
- rollback 逆序执行已 `applied` 步骤，每步再次 journal 和验证。
- rollback 失败后不尝试完整 import，不扩大删除范围，策略与 scheduler 全部暂停。

### 11.5 DNS cache 与动态 address-list

- 总是使用 `/ip/dns/cache/flush` 白名单命令，不清 conntrack。
- rosboard 独占且无 drift 的 address-list 可删除其动态条目以加速重新填充。
- 复用列表或存在用户条目时不得全量清理，只允许 TTL 自然过期，并在 job 结果中报告过渡窗口。

## 12. 备份设计

- 运行时策略备份位于 `${DataDir}/policy-backups/<safe-device-id>/<timestamp>-<job-id>/`，与部署前 NAS 二进制回滚备份是两个概念。
- 目录 `0700`、文件 `0600`；包含 `export.rsc`、`objects.json.gz`、`manifest.json`。
- export 由无 `sensitive` 权限的 policy account生成，并再次执行敏感键模式检查；发现疑似秘密即拒绝保存/继续。
- 首选 REST export 直接响应；若设备版本只能生成临时 RouterOS 文件，则使用随机自有文件名，下载 hash 校验后删除远端临时文件。远端清理失败时 job 阻止并明确报告，不扩大文件删除。
- manifest 保存 RouterOS identity/version、actual fingerprint、plan/job hash、每个文件 SHA-256 和对象数量。
- 新备份完整且 hash 校验成功后才轮换；保留最近 10 个，活动/rollback_failed job 的备份受保护。

## 13. API 设计

所有端点沿用当前 authenticated、allowed-CIDR、same-origin 门禁和 `?device=<id>`；响应中的空集合必须为 `[]`/`{}` 而非 `null`。

### 13.1 查询

| 方法与路径 | 返回 |
|---|---|
| `GET /api/policy-routing/overview` | access、capability、LAN scope、health/drift、egresses、sources、active/pending jobs |
| `GET /api/policy-routing/discovery` | 只读 WAN、LAN、existing policy/adoption candidates |
| `GET /api/policy-routing/egresses/{id}` | 出口详情、family 状态、owned/reused objects |
| `GET /api/policy-routing/sources/{id}` | 来源、版本、同步与计数，不默认返回全量域名 |
| `GET /api/policy-routing/sources/{id}/rules?cursor=&limit=&query=&type=` | 分页域名 |
| `GET /api/policy-routing/jobs/{id}` | job 阶段、计数、journal summary、错误和恢复选择 |
| `GET /api/policy-routing/audit?cursor=&limit=` | 审计列表 |
| `GET /api/policy-routing/backups/{id}/download` | 经权限检查的备份下载 |

### 13.2 provisioning 与配置草稿

| 方法与路径 | 用途 |
|---|---|
| `POST /api/policy-routing/access/sessions` | 生成短期专用账号脚本 |
| `POST /api/policy-routing/access/sessions/{id}/complete` | 验证、保存 policy credentials、安排一次 restart |
| `PUT /api/policy-routing/access` | 验证并保存用户现有写账号 |
| `PUT /api/policy-routing/lan-scope` | 保存用户确认的设备级 LAN scope 草稿/计划 |
| `POST /api/policy-routing/egresses`、`PUT .../{id}` | 保存 egress draft，不直接写 RouterOS |
| `POST /api/policy-routing/sources/url/preview` | 安全下载和解析 URL |
| `POST /api/policy-routing/sources/upload/preview` | 上传并解析 YAML |
| `POST /api/policy-routing/sources`、`PUT .../{id}` | 保存来源 pending version/draft |

### 13.3 计划和任务

| 方法与路径 | 用途 |
|---|---|
| `POST /api/policy-routing/plans` | 从 draft + actual 生成不可变预览 |
| `POST /api/policy-routing/plans/{id}/apply` | acknowledgements + 可选 admin password；返回 `202 job` |
| `POST /api/policy-routing/jobs/{id}/cancel` | 设置 cancel request |
| `POST /api/policy-routing/jobs/{id}/resume` | 重启对账后的显式继续 |
| `POST /api/policy-routing/jobs/{id}/rollback` | 显式回滚 |
| `POST /api/policy-routing/drift/plan` | 生成恢复期望或停止管理计划 |
| `POST /api/policy-routing/adoption/preview` | 现有配置严格对比 |
| `POST /api/policy-routing/takeover/preview` | 强制接管影响预览 |

### 13.4 API 错误契约

沿用现有 `writeAPIError`，并为策略 API增加稳定 code 与 details：

```json
{
  "code": "stale_plan",
  "message": "RouterOS configuration changed; generate a new preview.",
  "details": { "objectIds": [], "blockers": [], "retryable": false }
}
```

主要状态：400 输入错误、401 session/step-up 失败、403 same-origin/风险确认缺失、404 设备或对象、409 stale/drift/job conflict、413 上传过大、422 topology/conflict blocker、502 RouterOS/来源上游、503 设备或 manager 不可用。

## 14. Scheduler、健康与进程恢复

### 14.1 设备 runner

- 每个 controller 一个持久队列 worker；数据库是 queue 真相，内存 channel 只用于 wake-up。
- 数据库约束和 controller mutex 共同保证每设备最多一个 active mutation job。
- manual job 优先于尚未开始的 scheduled job；已开始 job 不抢占。
- 其他设备各自有 runner，MonitorManager 不获取 policy runner lock。

### 14.2 来源计划

- `next_run_at` 持久化；scheduler 使用稳定 device/source ID hash 做时间抖动，避免全部来源同时请求 GitHub/RouterOS。
- scheduled refresh 先 fetch/parse/persist pending version，再等待设备队列。
- 304/no change 只更新检查时间和审计，不创建 RouterOS job。
- >50% 缩减、容量超限、结构变化、drift、inactive route 或 foreign owner 产生 pending review，不自动 apply。

### 14.3 启动 reconciliation

启动时对 `queued/running/rolling_back` job 分类：

- `queued` 且计划未过期：重新入队。
- `running`：读取 journal 与 RouterOS，逐步判定 before/after/unknown，标记 `needs_decision`。
- `rolling_back`：同样对账已补偿步骤，要求显式继续 rollback。
- RouterOS 不可达时保留状态并退避，不把“不知道”标记为失败完成。

### 14.4 健康检查

- 默认错峰读取受管对象、family route active、DNS plumbing 与 source last-success；不进行第三方公网 IP请求。
- 结构 fingerprint 漂移、路由 inactive、连续来源失败分别生成明确状态，不能自动重写或切 WAN。
- `configured` 由自动验证得到；`healthy` 是用户在 LAN 客户端独立验证后的本地确认，实际结构恶化时自动降级。

## 15. 接管与实例迁移

### 15.1 手工配置接管

1. Scanner 只读发现 forwarder、DNS Static comment 分组、address-list、mangle、NAT、routes 和 table。
2. 用户提供来源 URL/上传 YAML、出口映射和候选组。
3. Parser/Materializer 与现状做 exact set 比较；missing、extra、semantic mismatch 任一非零则 DNS 组不可接管。
4. 基础对象逐一选择 `reuse` 或在完整关系可证明时 `adopt`；没有默认全选。
5. plan 在备份后只改所有权 comment/映射和必要规范化字段；行为变化单独列出。

### 15.2 rosboard 实例迁移

- 旧实例“释放管理”只改变 SQLite ownership state，不删除 RouterOS 行为；可选择将 comment 标为 released。
- 新实例通过 adoption 取得所有权。
- foreign instance 默认 blocker。
- forced takeover 要求确认旧实例停止、完整扫描/备份、逐对象预览、风险 acknowledgement 和 step-up；不尝试读取或合并旧实例数据库状态。

## 16. 前端信息架构

### 16.1 导航接入

- `ActiveView` 增加 `policy-routing`，新增独立 `hostSettingsExpanded`。
- Sidebar 一级菜单为“主机设置”，二级菜单目前仅“策略路由”；不能放入“状态监控”或“面板设置”。
- 顶栏显示“策略路由”，并保留全局设备选择器；页面内不重复设备选择。
- 空设备、设备未启用写账号、版本不支持、foreign owner、drift 和 active job 都有独立空/阻塞态。

### 16.2 页面结构

```text
策略路由
  ├─ 状态条：权限 / 能力 / LAN / drift / pending review
  ├─ 出口策略卡片或表格
  │    └─ family、WAN、route、DNS、列表、优先级、健康
  ├─ 域名来源表格
  │    └─ 类型、出口、规则数、更新时间、last-good、状态
  └─ 后台任务与审计入口
```

- 创建/编辑使用页面内分步向导，不使用一个超长表单。
- `ChangePlanView` 按“复用、创建、修改、移动、删除、风险”分组，并始终展示 ownership 和 rollback。
- 超过 10,000 条的计划不在浏览器渲染每条 operation，只展示聚合并提供分页/下载明细。
- 规则表 server-side cursor pagination；搜索/筛选变化重置第一页。
- 所有破坏性操作使用明确 danger action；管理员密码仅在最后确认步骤输入且不进入 React 持久状态或 localStorage。

### 16.3 状态管理与 API 边界

- 策略 feature 使用组件本地 draft 与轻量自定义 hooks；不为单页引入全局状态库。
- `api.ts` 是唯一 JSON normalization/error decoding 边界；组件不得重复 `as` cast API payload。
- server state 通过 device ID、resource ID、revision 关联；切换设备时 abort 旧请求并清空 draft/job 订阅。
- job 进度使用短轮询（active job 1 秒、idle overview 较慢）；页面隐藏时停止密集轮询，返回可见时立即刷新。
- upload、密码、plan acknowledgements 不写 localStorage/sessionStorage。

### 16.4 响应式与可访问性

- 低于 1200px 使用现有 off-canvas sidebar，低于 768px 单列布局、交互目标至少 44px。
- 大规则表在内部滚动，不制造 document 水平溢出；移动端保留来源详情和操作入口。
- 向导 step、job phase、风险和健康均用文本/图标，不只靠颜色。
- tabs、menus、expanded、dialogs/stepper 使用正确 ARIA；键盘焦点清晰，text-like button 保留 `:focus-visible`。
- 颜色、surface、radius 和 focus 使用现有 semantic CSS tokens。

## 17. 测试边界

### 17.1 纯单元测试

- URL/SSRF IP 分类、重定向、DNS rebinding pinning、限制和 GitHub URL。
- Clash YAML、IDNA、exact/suffix、无效/超限/重复规则。
- materializer 引用计数、跨出口冲突、优先级和共享/专用模式。
- capability version parsing、route topology、DHCP/PPPoE/WG、strict/fallback、NAT/FastTrack/firewall analyzer。
- ownership comment、canonical fingerprint、plan determinism、compensation generation。

### 17.2 Store/API 测试

- schema migration、设备隔离、active pointer 原子提交、版本轮换、审计正文删除。
- plan stale/replay/expiry、step-up 限速、same-origin、password-free projections。
- multipart 上限与临时文件清理、分页 shape、空集合非 null。
- 同设备 job 串行、跨设备并行、restart reconciliation 和 cancel。

### 17.3 RouterOS fake server/integration

- HTTP method/path/body、empty DELETE、RouterOS error detail、move、export、DNS flush。
- executor 每阶段故障注入并验证逆序补偿和 journal recovery。
- 50,000 条规模用 fake server 验证内存、分页、bounded concurrency 和取消，不要求向实机写满 50,000 条。

### 17.4 前端

- TypeScript build、oxlint、API normalization 和关键组件测试（如项目引入/已有测试设施时）。
- 用户手工检查 375、768、1024、1440px：菜单、向导、diff、表格、任务、错误、dark/light、无文档级横向溢出。
- 不默认使用浏览器自动化替代用户视觉验收；若用户明确授权再执行浏览器检查。

## 18. 实机与部署

### 18.1 `10.0.0.99`

实机测试分为独立授权步骤：只读 discovery → 小 YAML、单出口 IPv4 → IPv6 → shared dedup → update/delete/rollback → scheduled refresh。每次写入前保存策略 backup，不修改 WAN access、不 reboot/reset、不持久化临时高权限账号。

### 18.2 `10.0.0.6`

程序部署前必须按 `AGENTS.md`：确认 NAS 可写，在 `/Users/tom/nas/wyp/github/rosboard/backups/<timestamp>-<label>/` 保存现有 binary、config、SQLite、service unit，并最多保留 10 份。部署后验证 systemd、health、策略 API 与 embedded asset hash，等待用户人工批准后才允许 commit。

## 19. 关键取舍与拒绝方案

| 方案 | 结论 | 原因 |
|---|---|---|
| 直接把写方法加入 Monitor | 拒绝 | 权限边界混淆，慢写任务会影响监控锁和 cadence |
| SQLite 事务包住 RouterOS 写入 | 拒绝 | 外部系统不能参与 SQLite 原子事务，长事务会阻塞设备数据库 |
| 只靠 comment/name 找对象 | 拒绝 | 用户可复制名称或修改 comment，无法证明字段基线和 `.id` |
| 每来源重复写相同 DNS Static | 拒绝 | 产生重复转发记录并破坏引用删除语义 |
| 用 `/rest/execute` 拼接大脚本 | 拒绝 | 扩大命令注入和权限面，难以逐对象 journal/rollback |
| 所有 50,000 条串行一次完成 | 拒绝 | 实机耗时不可控；采用 bounded concurrency、进度、取消和资源门禁 |
| 写前只看版本号 | 拒绝 | RouterOS backport/字段权限差异存在，必须 capability probe |
| 自动强制接管或自动覆盖 drift | 拒绝 | 无法可靠保护用户复杂配置 |

## 20. 参考契约

- [RouterOS REST API](https://help.mikrotik.com/docs/spaces/ROS/pages/47579162/REST%2BAPI)：GET/PUT/PATCH/DELETE/POST 映射、单资源创建、move/export 命令。
- [RouterOS DNS](https://help.mikrotik.com/docs/spaces/ROS/pages/37748767/DNS)：named forwarder、FWD、address-list、match-subdomain、TTL 行为。
- [RouterOS Policy Routing](https://help.mikrotik.com/docs/spaces/ROS/pages/59965508/Policy%20Routing)：FIB table、gateway@main、mangle 优先级。
- [RouterOS DHCP](https://help.mikrotik.com/docs/spaces/ROS/pages/24805500/DHCP)：`default-route-tables` 与 DHCP/DHCPv6 字段。
- [RouterOS User](https://help.mikrotik.com/docs/spaces/ROS/pages/8978504/User)：`write` 不包含用户管理，`policy` 为用户管理权限。
