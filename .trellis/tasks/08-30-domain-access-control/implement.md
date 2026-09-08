# RouterOS 域名访问控制实施计划

## 执行原则

- 父任务保持 planning；用户已于 2026-08-30 批准设计，现按顺序创建并实施三个子任务。
- 每个子任务都执行完整的自动检查、远端备份、部署验证、人工验收和验收后提交。
- 不在一个未提交工作树中连续叠加三个阶段。
- 所有 RouterOS 写入只通过 `internal/routeros` allow-list 和精确 ownership comment。

## 子任务 1：来源级列表与永久阻断

### 1. 基线与迁移测试

- [ ] 为 shared/dedicated 域名和 IP 来源补齐当前 desired-state fixture。
- [ ] 增加来源级列表命名、同出口共享 connection mark、每来源 mark-connection 的目标 fixture。
- [ ] 覆盖 shared 动态列表 dual migration、IP 静态成员先复制、重启恢复和 cleanup gate。
- [ ] 验证未被引用来源不物化，访问控制引用的未绑定来源使用 access forwarder。

验证：目标测试先失败，现有 `go test ./...` 保持基线。

### 2. 来源级列表实现

- [ ] 增加稳定 `SourceListName`，修改 domain/IP materialization。
- [ ] shared egress 改为每来源 mark-connection + 每 egress/family mark-routing。
- [ ] 保留旧 shared mark-connection 兼容对象和迁移状态，不删除旧 schema 字段。
- [ ] 来源删除、禁用、重命名和版本提升保持精确所有权与 pending promotion 契约。

验证：policyv2/store targeted tests、idempotent replay、live fake RouterOS apply。

### 3. Access core persistence/domain

- [ ] 以 logical `AccessRule` 为用户主实体，增加 client/source membership、repository interface 和纯 validation/decision 函数。
- [ ] 表结构覆盖 device isolation、rule revision/audit、multi-client/multi-source membership 和 terminal merge，并为后续 rule-level shared quota 保持稳定主键边界。
- [ ] 默认 auto-follow device address；fixed IP 仅用于无可靠身份或高级设置。成员暂时 unresolved 只 degraded，不得全设备 fail-closed；确认地址身份冲突时移除错误投影。
- [ ] 阻止删除仍被访问规则引用的来源。

验证：store migration/isolation/rollback、membership、terminal merge、unresolved/reassigned identity tests。

### 4. RouterOS permanent deny

- [ ] 扩展 RouterOS read/mutation allow-list：source/destination match、jump、jump-target、time/days、reject-with 和所需 counter fields。
- [ ] 新增共享 per-device write gate，并让 policyv2 和 accesscontrol 共用。
- [ ] `sources` scope 按 client × source 在执行层展开双向 forward jump；`internet` scope 复用 TerminalScope local prefixes，先 return 本地流量再阻断非本地流量。
- [ ] 构建 TCP reset/UDP+other drop 子链，把受管 jump 块移动到 filter 顶部并读回验证；旧的 `ra_` 兼容标记发生归属冲突时 block，未知 `rb_<8>` 不得按前缀删除。
- [ ] TerminalScope 不可证明时阻止/降级 internet rule，不得把 LAN 一并 drop；对未来 FastTrack 设计 conntrack cleanup 接口。

验证：httptest RouterOS、multi-member/source、internet-preserves-LAN、ownership、move anchor、unknown outcome、IPv4/IPv6 tests。

### 5. API 与 UI

- [ ] 增加 overview、logical rule CRUD、job API 和稳定错误码；保存自动 apply，reconcile/sync 只保留故障恢复用途。
- [ ] 新增访问控制页面：规则名、多选设备、`整个互联网 | 指定网站/IP`、多选来源、禁止访问和启用状态。
- [ ] 主表显示规则、设备、访问范围、生效条件、状态；隐藏身份/IP 实现细节，fixed IP 仅在高级设置。
- [ ] 移除“永久阻断”“保存并同步”和常驻“同步 RouterOS”主操作；failed/degraded/drift 时才显示“重新应用”。
- [ ] 来源选择支持 domain 与 IP，允许选择未绑定或已绑定来源，并展示各自的能力边界。

验证：API contract tests、frontend parser tests、multi-select UX、lint/build、桌面/移动端视觉检查。

### 6. 子任务 1 验收

- [ ] `gofmt`、`go test ./...`、targeted `go test -race`、`go vet ./...`、`git diff --check`。
- [ ] `npm run lint`、`npm run build`、`npm audit`，重建嵌入式 Go 二进制。
- [ ] 确认 `/Users/tom/nas/wyp/github/rosboard/backups/` 已挂载可写，按 AGENTS.md 保留最多 10 份。
- [ ] 备份 `10.0.0.6` 当前 binary/config/SQLite/service unit 到时间戳 NAS 目录。
- [ ] 部署后验证 systemd、health、access API、policy-routing API、前端 assets。
- [ ] 在 `unicom` 和 `cmcc` 各创建测试规则，验证设备独立、IPv4/IPv6、多设备+多来源和整个互联网阻断且 LAN 保留。
- [ ] 验证 auto-follow 成员离线/恢复不阻断其他规则应用；验证一个来源同时参与策略路由和访问控制，策略出口不改变。
- [ ] 等待用户人工验收；通过后才提交并归档子任务 1。

## 子任务 2：允许访问窗口

### 1. 后端

- [ ] 增加 windows 表 CRUD、规范化、重叠合并和跨午夜拆分。
- [ ] capability scan 读取 RouterOS 时区、NTP 和 IPv4/IPv6 time matcher。
- [ ] limited 逻辑规则生成 time/days return；deny 不依赖时间能力，多成员共同使用同一窗口。
- [ ] 计算当前状态与下一次切换时间，API 使用 RouterOS 时区。

### 2. 前端

- [ ] 增加星期/时间窗口编辑器，空窗口明确表示全天允许。
- [ ] 展示 RouterOS 时区、当前状态和下次允许/阻断时间。
- [ ] 不提供“禁止窗口”第二套反向语义。

### 3. 验收

- [ ] 单日、多日、重叠、跨午夜、时区/NTP blocker tests。
- [ ] 两台 RouterOS 独立窗口验证；修正 `unicom` 与 `cmcc` 时区差异的显示和执行。
- [ ] 重复完整 NAS 备份、部署、远端 API/assets 检查和用户人工验收。
- [ ] 验收后提交并归档子任务 2。

## 子任务 3：每日活跃配额与人工干预

### 1. 计量状态机

- [ ] 实现 10 秒批量 counter poll、generation baseline、4 KiB/12 packets 门槛。
- [ ] 实现 30 秒 idle grace、20 秒 gap cutoff、跨午夜不重复计时。
- [ ] 各 member/direction/source-or-internet counter 独立建立 baseline；有效活跃秒数累加到 `(device, rule, local_date)` 共享 daily ledger。两台成员同时活跃一分钟合计消耗两分钟共享额度。
- [ ] 写入 shared daily ledger/unobserved/reset count；RouterOS 重启和 counter reset 不清空 ledger。
- [ ] 同设备 controller 单实例，关闭/重启无 goroutine 泄漏或重复扣时。

### 2. 配额执行

- [ ] quota reached 时在 write gate 内禁用普通 return，读回并记录 applied state。
- [ ] 新日期或 reset 时恢复 return；离线缺口不补算。
- [ ] 必要时清理匹配 conntrack，确保已有/未来 FastTrack 连接不能继续绕过新 block。

### 3. 人工干预

- [ ] 临时放行使用 RouterOS address-list timeout 到本地午夜，记录 audit。
- [ ] 重置今日用量使用 SQLite 事务，再同步 RouterOS 状态。
- [ ] 临时放行期间冻结配额；RouterOS 重启导致提前结束，不延长。

### 4. 前端

- [ ] 展示今日用量/额度、活跃/空闲、未观测、阻断原因和本地重置时间。
- [ ] 增加临时放行、重置确认和操作结果。

### 5. 验收

- [ ] fake-clock deterministic tests、race tests、重启/断线/counter rollback/午夜 tests。
- [ ] 在测试来源上缩短 quota 完成远端端到端验证，确认最大触发延迟不超过一个采样周期。
- [ ] 验证 rosboard 离线期间固定时段和已有 block 继续、未观测时间不扣除。
- [ ] 重复完整 NAS 备份、部署、远端 API/assets 检查和用户人工验收。
- [ ] 验收后提交并归档子任务 3；再完成父任务集成审查。

## 最终集成检查

- [ ] policy-routing、access-control 和 monitor 同设备写/读并发无死锁或交叉删除。
- [ ] 同一来源同时路由与控制，改名、刷新、禁用、删除和失败恢复均符合契约。
- [ ] 30 设备规模下 counter poll 与 filter scan 有明确请求预算，不影响现有监控刷新。
- [ ] 更新相关 Trellis spec，记录来源级列表、access ownership、计量算法和部署门禁。
