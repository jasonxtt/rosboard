# 来源级列表与永久阻断实施计划

## Assumptions

- 用户对父任务设计的批准同时批准本子任务按该设计实施。
- 本阶段不加入窗口、配额、临时放行或重置的占位实现。
- 先以测试固定迁移与防火墙契约，再最小范围修改现有 package。

## 1. Baseline And Fixtures

- [ ] 记录 git/runtime 测试基线，确认无关工作树改动。
- [ ] 为 shared/dedicated、domain/IP、IPv4/IPv6 补来源级 desired-state 失败测试。
- [ ] 补 dual migration、重启恢复、cleanup gate 和幂等 replay 测试。

验证：targeted policyv2/store tests 先证明旧实现不满足新契约。

## 2. Source-Level Materialization

- [ ] 实现稳定 `SourceListName`，让 DNS Static 与 IP CIDR 指向来源级列表。
- [ ] shared egress 生成每来源 mark-connection，同时保留共用 connection mark/mark-routing/route table。
- [ ] 持久化 migration state，保留旧 shared compatibility rule，正确处理 rename/version/disable/delete。
- [ ] 支持 access-only 来源物化与无消费者来源清理。

验证：targeted planner/reconciler/store tests、idempotent scan-plan-apply、mutation failure tests。

## 3. Access Domain And Store

- [x] 将当前单 terminal/source `Policy` 重构为逻辑 `AccessRule` + client members + source members；保留 device scope、revision、audit 和 applied state。
- [x] 第一阶段支持 `target_scope=internet|sources`，`sources` 多选、`internet` 无 source row；action 仍固定 deny。
- [x] client member 默认 auto-follow，fixed IP 作为高级方式；保存可靠 MAC anchor，并实现 resolved / temporarily_unresolved / conflicted 三态投影。
- [x] 单个 auto member 暂时不可解析时保留无冲突的最后已应用投影并 degraded，不阻断设备其他规则；确认地址转移到其他身份时移除错误投影。
- [x] 在 `Store.MergeTerminal` 事务内迁移 auto member identity，阻止删除仍被规则引用的来源。
- [x] 数据主键以 `rule_id` 为未来窗口/共享 quota 聚合边界，不再以 terminal/source pair 作为用户主实体。

验证：schema/migration rollback、device isolation、multi-client/multi-source membership、terminal merge、unresolved/reassigned identity tests。

## 4. RouterOS Permanent Deny

- [x] 保持 typed RouterOS filter read/mutation allow-list、per-device write gate 和 read-back verification。
- [x] `sources` scope 按规则物化成员地址列表，并对每个来源展开 IPv4/IPv6 双向 jump；logical rule ownership/audit 保持一个 rule。
- [x] `internet` scope 按 RouterOS 全部 routing table 的默认路由解析实际出口接口，主备接口去重后生成直接 `out-interface` / `in-interface` 双向 TCP reset、UDP/other drop 规则；不再生成 local-prefix list 或 internet jump/sub-chain。
- [x] 无法解析独立出口、默认路由只落到本地接口或活动路由引用不存在/禁用接口时阻止 internet rule，禁止退化成无接口的全局 forward drop。
- [x] auto-follow 终端投影全部可用 IPv4/IPv6 地址，忽略 interface-scoped IPv6 link-local 地址。
- [x] 把 managed jump block 移到 filter 顶部并读回验证；foreign conflict 或 unknown outcome 时停止。

验证：httptest RouterOS、multi-member/source expansion、internet preserves LAN、IPv4/IPv6、ownership、move order、unknown outcome tests。

## 5. API And Frontend

- [x] API 改为 logical rule CRUD + job state；保存自动 apply。内部 reconcile/sync 能力可以保留，但不作为常驻 UI 主操作。
- [x] 创建/编辑采用：规则名称 → 多选设备 → 整个互联网/指定网站或IP → 多选来源 → 禁止访问 → 启用。
- [x] 默认隐藏身份模式；高级设置才显示“自动跟随设备地址（推荐）/固定当前 IP”。
- [x] 主表改为“规则 / 设备 / 访问范围 / 生效条件 / 状态”；移除“永久阻断”、身份列、地址列、常驻“同步 RouterOS”和“保存并同步”文案。
- [x] failed/degraded/drift 时按上下文提供“重新应用”和具体成员异常；正常状态不要求用户理解 RouterOS reconcile。

验证：API contract tests、frontend parser/component tests、multi-select UX、desktop/mobile viewport、lint 和 production build。

## 6. Local Quality Gate

- [x] `gofmt` changed Go files。
- [x] targeted tests 与 targeted `go test -race`。
- [x] `go test ./...`、`go vet ./...`、`git diff --check`。
- [x] `npm run lint`、`npm run build`、`npm audit`。
- [x] 重建并本地验证嵌入式前端二进制。

## 7. Deployment Gate

- [x] 旧单终端/单来源 MVP 已完成一次部署基线验证与 NAS 备份；该版本不再作为最终人工验收候选。
- [x] 完成当前 logical-rule 重构后重新运行完整本地质量门，不复用旧构建结论。（`go test ./...`、targeted race、`go vet ./...`、`git diff --check`、前端 lint/build 全部通过；`internal/api` 的 -race 全量存在一个预先存在的测试辅助竞态（server_test.go restarts 计数器），与本次改动无关，改动包的 targeted race 通过。）
- [ ] 按 `AGENTS.md` 为新的最终候选重新确认/创建生产备份并部署。
- [ ] 在 `unicom`、`cmcc` 分别验证设备独立；实测多设备 + 多来源、整个互联网保留 LAN、auto member 离线降级，以及来源同时用于策略路由和访问控制。
- [ ] 等待用户人工检查新的最终候选并明确批准。

## 7.1 Session Fixes (2026-08-30 下午)

- [x] 修复新增规则空白弹窗：Go nil slice 被序列化为 null（terminal.ipv4/ipv6、policy.pinnedIpv4/v6），前端展开/.length 崩溃；后端统一输出 []（nonNilStrings / NormalizePolicy / scanAccessPolicy），前端 reload 归一化兜底；新增回归测试 TestAccessControlOverviewNeverMarshalsNullArrays。
- [x] 用户确认项目未上线无存量用户：整体移除 shared 列表迁移机制（policy_v2_source_list_migrations 表、dual/cleanup_ready/complete 状态机、迁移兼容对象、cleanup 审批 API 与相关测试）。shared 模式标记身份改为 egressID:family，与可编辑列表名解耦。list_mode/list_name 列保留。

## 8. Finish

- [ ] 新模型人工批准后更新稳定 Trellis spec。
- [ ] 只提交本子任务相关改动，记录 commit 并归档子任务。
- [ ] 返回父任务；下一阶段允许窗口继续复用同一 logical rule，多设备窗口共同生效；后续每日配额按 rule 共享池累计。
