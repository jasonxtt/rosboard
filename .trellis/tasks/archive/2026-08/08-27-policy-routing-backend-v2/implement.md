# 策略路由后端 V2 实施计划

## 1. 实施门禁

- 本文件获用户审阅批准后，才运行 `task.py start` 和修改程序。
- 前端契约优先于旧后端类型；禁止为复用旧代码重新引入被删除的状态机。
- 每阶段都以可运行的纵向结果结束，不先堆满所有模型和抽象。

## 2. Phase 1：冻结契约与最小数据模型

- 从当前可达页面提取 overview、discovery、egress/source CRUD、preview/rules、plans/apply 的请求响应 fixture。
- 标明暂不实现的前端辅助 API，防止把未接入功能误当首版需求。
- 建立 V2 Repository 和必要迁移，只承载配置、版本、规则和最近 apply 状态。
- 复用 auth、step-up、设备校验、MutationClient、安全 fetcher 和 Clash parser。

验证：Repository CRUD/revision/tombstone/version 测试；API fixture parser 测试；旧表和非策略数据不变。

## 3. Phase 2：单 IPv4 出口纵向链路

- 实现最小 Desired Builder、managed Scanner 和扁平 Diff。
- 实现 `POST /plans` 与当前 `ChangePlanView` 契约。
- 实现单设备 apply worker：写入依赖对象、激活、flush、最后清理、重扫提交。
- 完成一个 IPv4 出口、一个共享列表、一个上传来源的端到端路径。

验证：fake RouterOS 精确操作序列；foreign 对象不写；stale plan 拒绝；每个写入点失败后重试可收敛。

## 4. Phase 3：补齐当前前端能力

- IPv6、双栈、多出口、优先级和 failure mode。
- shared/dedicated 列表、Fake DNS 自动分配和冲突检查。
- URL preview、定时刷新 pending version、规则分页搜索。
- 出口/来源启停、解绑、pending deletion 和成功后的清理/提升。
- overview 活动作业与最近失败状态。

验证：按前端工作流建立 API 契约表；覆盖各功能的最小成功、冲突、删除和失败重试案例；前端 typecheck/build 通过。

## 5. Phase 4：切换、删旧与验收

- 将 runtime/API 装配切到 V2，确认旧 planner/executor/jobs/rollback 等路径无调用。
- 删除仅服务旧架构的代码和测试；不顺手重构 routeros、auth、config 或其他模块。
- 运行全量 Go、前端和 diff 检查，并进行本地运行时/视觉验证。
- 在 `10.0.0.99` 验证创建、修改、失败重试和清理，确认非受管对象无变化。
- 部署 `10.0.0.6` 前按 AGENTS.md 在 NAS 保存时间戳备份并控制最多 10 份；验证 systemd、health、API 和嵌入前端。
- 等待用户人工验收；批准后才提交、更新 Trellis 记录并归档任务。
- 将“LAN 范围”收敛为设备级“策略流量入口”：补齐 interface list/Bridge/VLAN/WireGuard/固定隧道 discovery，过滤内置列表与 WAN/Bridge 从端口；前端移除手工输入并默认选择 `LAN`。
- Desired Builder 与 Scanner 管理一个聚合 interface list 及其成员，mangle 统一引用它；验证现有 `LAN`、额外 WireGuard、现有 `VPN-LAN` include、删除成员和重复 apply 零差异。

## 6. 完成定义

- PRD 全部验收项有自动化或实机证据。
- 当前前端不需要兼容旧复杂 planner/job 行为。
- V2 失败恢复依靠幂等重试，并有故障注入测试证明。
- 旧后端实现不再进入编译产物；旧数据库表暂留只为部署回退。
- 未实现功能明确留在非目标列表，不留下半成品端点或假状态。

## 7. 生命周期与统一设备账号补充实施

- Desired Builder 区分 enabled、disabled 和 pending deletion；覆盖单策略停用、重新启用、共享列表、外部表及自动表清理测试。
- 增加启停直接应用动作；删除接口在写入 tombstone 后直接生成和启动清理任务。失败保留可重试状态，前端删除“请点击应用设置”提示。
- 保留编辑向导的差异预览；验证优先级变化能同步受管规则顺序。
- 删除独立 policy access 配置、运行时装配、API、前端类型与未引用组件，设备 RouterOS 凭据成为唯一 reader/mutation 凭据。
- 快速接入脚本增加 `write`；增加当前设备账号权限读取和策略写权限门禁。
- 重排设备编辑器：连接信息一行、账号管理一行，账号编辑复用设备保存/验证，一键更换复用 onboarding session。
- 运行 Go 全量测试/vet、前端 lint/build、diff 检查和本地运行验证，再按部署门禁备份并部署 `10.0.0.6`。

实施结果：上述生命周期、共享对象保护、统一账号、只读门禁和设备账号模块已经完成；全量 Go 测试/vet、前端 lint/build、diff 检查和浏览器运行态验证通过。Linux amd64 构建已部署到 `10.0.0.6`，NAS 回退备份为 `/Users/tom/nas/wyp/github/rosboard/backups/20260827T160621Z-policy-lifecycle-account-unification/`。systemd、`/api/health`、登录保护、嵌入资源、只读账号提示和设备管理账号模块均已复核，当前等待用户人工验收，尚未提交或归档。

## 8. 清理完整性与 RouterOS 可观测性补充

- Desired Builder 仅在存在非待删除策略时输出聚合入口 list，补充“删除一条仍保留、删除最后一条即清理”的 fake RouterOS 回归测试。
- 将受管 comment 拆为稳定身份和可读说明，Scanner 按稳定段识别，验证旧纯身份 comment 可无破坏升级、名称修改生成 patch、重复应用零差异。
- 专用标记列表名改为域名列表名称关键字加短稳定后缀；DNS static comment 明确包含域名列表名称，其余策略专属 route/mangle/NAT/DNS transport comment 包含策略名称和用途。
- 前后端新建默认值切到 dedicated；编辑既有记录不迁移其模式。
- 完成 Go/前端全量检查后重新执行 NAS 备份部署门禁，并等待用户验收。

实施结果：最后一条策略清理聚合入口、稳定身份加可读 comment、可读专用列表名称和新建默认 dedicated 已完成。回归测试覆盖共享策略保留入口、最后策略删除入口、策略改名只 patch comment、专用列表名称及 DNS static 来源说明。Go 全量测试/vet、前端 lint/build、diff 检查通过；Linux amd64 构建部署到 `10.0.0.6`，备份为 `/Users/tom/nas/wyp/github/rosboard/backups/20260827T164408Z-policy-readable-comments-cleanup/`。远端服务、健康接口、嵌入资源和新建向导默认项已验证，等待用户人工验收。
