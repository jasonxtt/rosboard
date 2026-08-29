# RouterOS 域名策略路由实施计划

> **旧实施计划已暂停（2026-08-27）**：尚未完成的后端阶段不再继续；后续按子任务 `08-27-policy-routing-backend-v2/implement.md` 执行。

## 1. 状态与执行门禁

- Phase 6: APPROVED。
- Phase 7: IMPLEMENTED — user review completed。
- Phase 8: IMPLEMENTED — awaiting user review。
- 需求：[prd.md](./prd.md)
- 设计：[design.md](./design.md)
- 当前 Trellis task 必须继续保持 `planning`，直到用户审阅三份文档并明确允许 `task.py start`。
- 不得因为批准本计划而自动连接或写入 `10.0.0.99`；实机写入需要执行阶段单独授权。
- 程序变更不得在 `10.0.0.6` 人工验收前 commit。

## 2. 完成定义

只有同时满足以下条件才算实现完成：

1. PRD 的 AC-01 至 AC-23 有测试或明确人工证据。
2. Go 单元、集成、race、vet，前端 lint/build/audit 均通过。
3. 本地运行时和 embedded frontend 验证通过。
4. 获得授权后，`10.0.0.99` 分阶段实机用例通过，且实验对象可完整清理或按用户选择保留。
5. 部署前 NAS 备份与 `10.0.0.6` systemd/API/assets 验证通过。
6. 用户手工检查部署实例并明确批准。
7. 只在批准后 commit、记录 Trellis session 并归档 task。

## 3. 实施纪律

- 每一阶段先写失败测试或可复现 fixture，再写最小实现。
- 只修改该阶段列出的边界；不顺带重构监控、认证或 UI。
- 每次修改配置字段、API payload 或 RouterOS wire field 前先 `rg` 搜索现有定义和消费者。
- 所有 RouterOS 变更测试默认使用 fake HTTP server；真实设备不是普通单元测试依赖。
- 任何密码、Basic Auth header、上传 YAML 正文和敏感 export 不进入测试失败输出。
- 每阶段结束运行目标测试和 `git diff --check`；发现前一阶段回归立即停止推进。
- `npm --prefix web run build` 生成 `internal/ui/dist`；不得手改生成文件。

## 4. 阶段计划

### Phase 0：启动前复核与上下文

目标：满足 Trellis 规划门禁，不修改 runnable program。

- [ ] 用户审阅 `prd.md`、`design.md`、`implement.md`。
- [ ] 记录用户对规划的修改并重新运行文档校验。
- [ ] 为实现和检查 context manifest 加入相关 backend/frontend spec、三份 RouterOS 参考方案和官方契约。
- [ ] 检查当前 worktree；记录并保护用户已有 `AGENTS.md` 等无关修改。
- [ ] 获得明确“启动开发”后才执行 `python3 ./.trellis/scripts/task.py start 08-20-policy-routing`。

验证：

```bash
python3 ./.trellis/scripts/task.py validate 08-20-policy-routing
git status --short
```

门禁 G0：未获用户启动许可，不进入 Phase 1。

### Phase 1：领域骨架、配置与安装身份

对应：FR-2、FR-3、FR-15；AC-02、AC-03、AC-17。

先写测试：

- [ ] Config：旧 YAML 无策略字段仍可加载；策略凭据 round-trip；空密码更新保留；API 投影不含密码。
- [ ] Config：启用策略凭据但配置权限非 `0600` 时失败，错误不含秘密。
- [ ] Store：安装 manager UUID 并发首次创建只产生一个稳定值，重启后不变。
- [ ] Provisioning：策略脚本权限精确为 `read,write,test,api,rest-api` 且不含 `policy`，与监控账号使用不同前缀/group。
- [ ] Auth：step-up 正确/错误/限速/session username 变化路径，不新建 session。

实现：

- [ ] 扩展 `config.DeviceConfig`，新增独立 `PolicyAccess` 和 `ManagedPolicyAccount`。
- [ ] 原子保存、权限检查、passwordSet/cleanupAvailable projection。
- [ ] 根 SQLite 增加 installation meta 迁移和稳定 UUID API。
- [ ] 为策略 provisioning 抽取可参数化的安全账号生成逻辑，但不得改变现有监控脚本输出。
- [ ] 在 `auth.Service` 增加单次 step-up 校验。
- [ ] 建立 `internal/policy/types.go`、Manager 空实现和 disabled-by-default 装配；此阶段不产生 RouterOS 写入。

验证：

```bash
gofmt -w internal/config internal/store internal/auth internal/api internal/policy cmd/rosboard
go test ./internal/config ./internal/store ./internal/auth ./internal/api ./internal/policy ./cmd/rosboard
go test -race ./internal/config ./internal/store ./internal/auth ./internal/api
go vet ./...
git diff --check
```

回滚点 R1：删除新增策略配置/表迁移/空 manager 装配；现有监控行为和 YAML 必须仍通过原测试。

### Phase 2：策略 SQLite Repository

对应：FR-5、FR-12 至 FR-15；AC-06、AC-13 至 AC-20。

先写测试：

- [ ] 每个策略表均无法跨 device ID 读取、更新、轮换或删除。
- [ ] active source version 只在单事务 commit 后切换；失败 pending version 不改变 active。
- [ ] shared materialized rule 多来源引用增删正确，最后引用才删除。
- [ ] 10 个成功历史版本轮换保护 active/last-good；失败版本不保存正文。
- [ ] 删除来源清除 raw/domain body、保留审计元数据。
- [ ] job/step write-ahead 状态、cancel flag、唯一 active job 和重启查询正确。

实现：

- [ ] 在 `internal/store/policy.go` 建立 design.md 定义的设备级 schema 和事务迁移。
- [ ] 在 `internal/policy/repository.go` 定义仅业务所需接口，避免 API 依赖 SQL。
- [ ] 实现 egress/source/version/rule/materialized/object/plan/job/backup/audit repository。
- [ ] 原始 YAML 与大快照在事务外 gzip；Store 只处理已准备的 bytes。
- [ ] 所有 cursor pagination 使用稳定 `(created_at,id)` 或 `(domain,type)` cursor，不使用大 offset。

验证：

```bash
go test ./internal/store ./internal/policy
go test -race ./internal/store ./internal/policy
go test ./...
git diff --check
```

回滚点 R2：使用迁移前数据库副本恢复；不得尝试让旧二进制读取新 schema 后继续写。

### Phase 3：URL fetcher、上传和 Clash parser

对应：FR-6、FR-13；AC-04、AC-05、AC-13、AC-18、AC-19。

先写测试：

- [ ] GitHub blob→raw、普通 HTTPS、304、ETag/Last-Modified。
- [ ] 拒绝 HTTP、userinfo、fragment、私网/回环/ULA/link-local/multicast/reserved、混合 DNS 答案和每跳恶意 redirect。
- [ ] 模拟检查后 DNS rebinding，断言实际 dial 仍使用已验证固定 IP且 SNI/Host 正确。
- [ ] 5 MiB+1、15 秒、>5 redirect、二进制/MIME/UTF-8 失败。
- [ ] YAML root/payload 类型、DOMAIN、DOMAIN-SUFFIX、trailing dot、case、IDNA、duplicate、invalid、unsupported、zero valid、20,001 条。
- [ ] multipart 上限、随机临时文件、任意文件名路径穿越和所有退出路径清理。

实现：

- [ ] `source_fetcher.go` 专用 resolver/transport/client，不使用环境 proxy。
- [ ] 严格 GitHub URL normalizer 和逐跳 pinning。
- [ ] `clash_parser.go` node/标量/规则限制与有限错误样本。
- [ ] 上传 preview 服务和 gzip/hash/version persistence。
- [ ] URL/manual schedule metadata；此阶段不写 RouterOS。

验证：

```bash
go test ./internal/policy -run 'Fetcher|URL|Clash|Upload|Source'
go test -race ./internal/policy
go test ./...
git diff --check
```

回滚点 R3：来源 preview 可以关闭而不影响 Monitor；数据库中没有 active RouterOS mapping。

### Phase 4：RouterOS mutation client 与 wire contracts

对应：FR-3、FR-8 至 FR-10、FR-14；AC-03、AC-08、AC-10、AC-15。

先写 fake server 测试：

- [ ] GET/PUT/PATCH/DELETE/POST path、JSON body、Basic Auth、不含任意 path 注入。
- [ ] create/patch 解析 `.id`，DELETE 空 body 成功，404/400 detail 有界。
- [ ] allowlisted move、DNS flush、print、export、DNS settings set；不伪造 `/ip/dns/static/resolve`，代表性域名解析留给后续 Verifier probe abstraction；非白名单 command/path 拒绝。
- [ ] context timeout、网络错误、429/5xx 有限重试，4xx 不重试。
- [ ] 日志/错误不会输出 Authorization 或 password。

实现：

- [ ] `internal/routeros/mutation.go`，menu/command 强类型白名单。
- [ ] `policy_types.go` 只加入 Scanner/Executor 真正使用字段。
- [ ] 支持 `.proplist` 查询和 RouterOS 字符串布尔/数字正规化。
- [ ] move adapter 与 export reader 使用接口隔离，便于不同版本集成测试。
- [ ] 不提供 `/rest/execute` 用户脚本入口。

验证：

```bash
go test ./internal/routeros -run 'Mutation|Move|Export|HTTP'
go test -race ./internal/routeros
go test ./...
git diff --check
```

回滚点 R4：mutation client 尚未被 API 暴露，删除装配即可恢复纯只读运行。

### Phase 5：Scanner、能力矩阵和拓扑证明

对应：FR-3、FR-4、FR-8 至 FR-11；AC-03、AC-07 至 AC-12。

先写纯函数 fixture：

- [ ] 7.17/7.18/7.21.5/7.22.x 版本解析和 capability field probe 组合。
- [ ] 静态 next-hop@main、DHCP、PPPoE、WireGuard、IPv4/IPv6、默认 WAN。
- [ ] strict table、main fallback、existing failover、ECMP、recursive loop、inactive、blackhole、dynamic protocol、non-main VRF。
- [ ] LAN interface list 证据、用户确认范围、WAN 误选阻止。
- [ ] NAT safe reuse/missing/indeterminate，GUA IPv6、WG ULA masquerade。
- [ ] PCC/PBR insertion、routing rule 提示、FastTrack bypass、无法定位 blocker。
- [ ] DNS Static exact/suffix/regex/FWD/A/AAAA/CNAME/NXDOMAIN 冲突。

实现：

- [ ] Scanner 批量读取 design.md 菜单并产出 immutable actual snapshot。
- [ ] Capability Matrix 按版本诊断、字段/API probe 最终裁决。
- [ ] WAN、route table、LAN、NAT、firewall、FastTrack analyzers 均为纯函数。
- [ ] actual canonical fingerprint 排除 counters 等非结构字段。
- [ ] Scanner 识别 owned/reused/foreign/manual candidate 和 field-level drift。

验证：

```bash
go test ./internal/policy -run 'Scanner|Capability|Topology|Route|NAT|Firewall|FastTrack|Conflict|Fingerprint'
go test -race ./internal/policy
go test ./internal/service ./internal/routeros
go test ./...
git diff --check
```

回滚点 R5：Scanner 是只读组件，可保留用于诊断；不生成 apply 能力。

### Phase 6：Materializer 与不可变 Planner

对应：FR-4 至 FR-12、FR-15；AC-06 至 AC-14、AC-17、AC-20。

先写测试：

- [ ] shared/dedicated desired set、引用计数、source disable/delete/move、exact/suffix 分离。
- [ ] 跨出口 containment blocker、同出口 redundancy、IP overlap warning、priority order。
- [ ] 完整初次计划、非结构 domain delta、source migration、disable/delete、strict/fallback、IPv6 partial blocker。
- [ ] owned/reused/field increment compensation、rule anchor、adoption 与 foreign owner。
- [ ] plan determinism、hash、expiry、desired revision、actual fingerprint 和 acknowledgement codes。
- [ ] scheduled plan 发现结构变化时转 pending review。

实现：

- [ ] `materializer.go` 和 `conflicts.go`。
- [ ] `planner.go` 生成有序 operations、verification 和 compensation。
- [ ] 计划写 SQLite 后不可修改；修改 draft 必须生成新 plan。
- [ ] 资源估算、>10,000 warning、20,000/50,000 cap、>50% shrink review。
- [ ] fake alias 从受控 documentation address pool 分配，并扫描所有冲突后持久化；IPv4/IPv6 分开验证。

验证：

```bash
go test ./internal/policy -run 'Material|Conflict|Plan|Alias|Priority|Adoption'
go test -race ./internal/policy
go test ./...
git diff --check
```

回滚点 R6：Planner 只产生预览，不执行；可以在 UI/API 暂不暴露 apply。

### Phase 7：Backup、Executor、Verifier 与 rollback

对应：FR-8 至 FR-16；AC-08 至 AC-17、AC-20、AC-21。

先写故障注入测试：

- [x] backup export/object snapshot/hash/权限/10 份轮换和受保护 backup。
- [x] 每个 executor phase 在第 N 步失败，已完成步骤严格逆序补偿。
- [x] crash 位于 `prepared → RouterOS applied → journal applied` 的 prepared journal 可由 startup reconcile 区分 before/after/unknown。
- [x] create/patch/move/disable/delete/shared-field increment 使用 immutable compensation 描述；foreign/reused drift fail-closed。
- [x] bounded DNS worker 已接入 Executor；仅独立 DNS Static create/delete 进入 worker，structural/move/activation 保持串行，初始并发 1、连续成功批次后逐步升至上限 4，failure/unknown 停止后续 dispatch，active request 不被主动取消；本阶段未实现基于 RouterOS latency、error-rate 或 free-memory signal 的降并发。
- [x] apply 前 fingerprint/desired identity stale 返回，无 RouterOS 写入。
- [x] route 使用独立 `/routing/route` runtime semantic reader；DNS Static/A/AAAA/order/counter/NAT/FastTrack 使用 Planner 冻结的 typed verifier；单样本缺 AAAA 不直接失败，但全样本无 AAAA 为 uncovered/indeterminate 且不可 commit。
- [x] rollback failure 将 job 置为 `rollback_failed`，保护 backup，startup reconcile 不自动重放或 full import。
- [x] Repository=nil 在 backup 和 RouterOS mutation 之前 fail closed；forward `prepared` 与 rollback `compensation_prepared` 均先于对应写入持久化。
- [x] compensation reconciliation 明确区分 applied/not_applied/unknown；not_applied 保留 `compensation_not_applied` 并进入 `needs_decision`，不伪造 rolled_back。
- [x] rollback_failed 写入 durable device mutation pause；manual/scheduled 新 job 被拒绝，read-only reconciliation 不受影响，重启不清除 pause。
- [x] materialized reference deltas 与 committed/committed_partial terminal job state 通过单一 SQLite transaction 提交；final commit failure 不盲目 rollback 已验证的 RouterOS。
- [x] family dependency DAG 冻结 cross-family atomic group；只有完成并 semantic-verified 的 family checkpoint 才可保留，retained reference deltas 与 group 一致。
- [x] AnchorAfter + NeighborID 使用最终 `[anchor,target,neighbor]` relation 验证，并覆盖外部插入 drift。
- [x] failure-injection matrix 覆盖 repository gate、compensation crash window、runtime route/DNS/counter、pause、SQLite final commit、interleaved family 和 AnchorAfter。
- [x] partial family 的 unknown create/patch 优先进入 `needs_decision`，保留 prepared evidence，不进入 `committed_partial`。
- [x] Planner 冻结真实 DNS cache-flush operation、create→enable semantic identity、representative samples、mark/DNAT counter identity 和 runtime route normalization。
- [x] owned operation/compensation 使用完整 instance/device/object/type identity；runtime mapping 持久化同一 frozen identity。
- [x] pause latch 使用原子 CreateJob/ClaimJob gate；普通 device-state update 不清除 latch，恢复必须调用独立 recovery method。
- [x] verified execution-group checkpoint 持久化 plan/hash/sequence/evidence，restart 后先 read-only revalidate 再允许 family retention。
- [x] terminal SQLite transaction 同时处理 references、source active/last-good、final mappings、audit 和 job terminal state。
- [x] DNS worker failure-stop/unknown-stop 覆盖串行与并发批次；known-applied sibling 保留可补偿证据，unknown prepared row 不重放。
- [x] ExecutionGroup 冻结 prerequisite/postcondition/semantic barrier；DNS family checkpoint 等待 frozen cache flush 与语义验证完成。
- [x] partial retention 从已验证 family root 反向计算依赖闭包；shared Consumers 由 Planner dependency DAG 反向传播，失败 family 专属对象不保留。
- [x] compensation 与 restart reconciliation 在 applied/not_applied 判断前验证完整 ownership identity；foreign comment 不写、不重绑定。
- [x] SourceActivation 冻结 group/required-groups/egress identity；计划裁剪和 committed_partial 只提交 retained closure 对应版本。
- [x] restart reconciliation 重新读取 durable group checkpoint、applied journal 与 semantic read-back；显式 ResumeJobAfterReconciliation 释放旧 claim，禁止自动重放。
- [x] source migration 从 scanner/mapping evidence 冻结 DNS Static ownership comment；identity 不完整时以 migration_ownership_unproven 阻止执行。
- [x] DNS TCP/UDP mark/DNAT descriptors 使用 policy:dns-output/dns-dnat logical identity、完整 wire fields、runtime .id 与 typed counter arrays。
- [x] 提供只读 ProductionDNSProbe/NewProductionVerifier，代表样本、address-list、flush evidence 和 typed counters 均来自 frozen plan/read-only reader。
- [x] fallback route descriptor 由真实 desired route fields 直接冻结；支持 gateway normalization、family/active/interface、fallback table 与 strict blackhole 语义。
- [x] partial terminal result 在同一 SQLite transaction 中持久化 configured/blocked family metadata，不能被上层误读为全量 configured。
- [x] cancellation 使用独立 sentinel；无 mutation 时进入 `cancelled_before_write`，有 durable applied journal 时全量定向 rollback，绝不走 family retention。
- [x] failed execution-group checkpoint 在 restart/reconcile 中保持 `failed` 状态；仅在 plan/hash、rollback journal 和 RouterOS compensation read-back 全部成立时 hydrate 并继续独立 group。
- [x] Desired + Materialization dual-stack 路径保持 DNS Static shared scope，单独冻结 IPv4 transport、A/AAAA CoverageFamilies 和跨 family transport dependency；IPv4 transport failure 不会伪造 IPv6 configured。
- [x] production-shaped dual-stack schedule 冻结为 transport → shared materialized DNS → IPv4 business → IPv4 flush/checkpoint → IPv6 business → IPv6 flush/checkpoint；Executor 对未达 verified barrier 的依赖 fail-closed，不运行时重排。
- [x] ExecutionGroup 冻结显式 role（business family / DNS transport prerequisite / shared prerequisite / postcondition）；transport 不进入 business partial result，IPv6 仅保存 schedule-after，不把 IPv4 business 当 semantic prerequisite。
- [x] production-shaped Executor runtime 覆盖 IPv4 business failure、IPv4 transport failure、双业务失败清理 prerequisite、IPv6 business failure保留 IPv4，以及 verified+failed outcome invariant。

实现：

- [x] `backup.go` 写 `${DataDir}/policy-backups`，安全路径/权限/hash/轮换。
- [x] `jobs.go` 设备级 durable runner、状态机、cancel、startup reconcile。
- [x] `executor.go` write-ahead journal、staging、order、activate、flush、verify、commit。
- [x] `rollback.go` 针对本 job 的补偿；共享字段只撤回增量。
- [x] `verifier.go` 读回和健康状态。
- [x] 增量更新和结构迁移沿 plan phase 分开执行；structural operation 不会由 executor 补写。

验证：

```bash
go test ./internal/policy -run 'Backup|Executor|Journal|Rollback|Reconcile|Verify|Cancel|Job|DNSWorker'
go test -race ./internal/policy ./internal/store
go test ./...
go vet ./...
git diff --check
```

回滚点 R7：功能仍由 policy access/manager 开关控制；如 executor 不可靠，保持 API preview-only，不部署写入口。

### Phase 8：策略 API 与安全门禁

对应：FR-1、FR-2、FR-6、FR-12 至 FR-17；AC-01、AC-02、AC-04、AC-05、AC-13 至 AC-21。

先写 API 测试：

- [ ] 全部端点 method、auth、CIDR、same-origin、device unknown/disabled/archived。
- [ ] overview/discovery/egress/source/rules/jobs/audit 空 shape 不为 null。
- [ ] preview→plan→apply；stale、expired、replay、blocker、ack 缺失、step-up 失败均不写。
- [ ] 需要 step-up 与不需要的 domain delta 矩阵。
- [ ] upload 413、source upstream 502、topology 422、job conflict 409。
- [ ] backup download 不能跨 device/job，Content-Disposition 安全。
- [ ] 所有 settings/response/log fixtures 不含 password/raw Authorization。

实现：

- [ ] `internal/api/policy_routing.go` 按 design.md 路由。
- [ ] Server 使用一个完整依赖构造器，保留旧构造器兼容测试。
- [ ] 将 request session actor、remote IP 和 plan hash 传入 step-up/audit。
- [ ] 长写请求返回 202 job，不阻塞 HTTP handler 等待 RouterOS。
- [ ] policy manager unavailable 时页面查询仍返回可解释的 setup state。

验证：

```bash
go test ./internal/api -run 'Policy|StepUp|Upload|Plan|Job|Backup'
go test -race ./internal/api ./internal/policy
go test ./...
go vet ./...
git diff --check
```

回滚点 R8：API 可通过构建配置禁用；现有 API 路由和 bootstrap 不受影响。

### Phase 9：前端“主机设置 / 策略路由”

对应：FR-1、FR-17；AC-01、AC-06、AC-18、AC-21。

先确定可验证状态：

- [ ] API normalization 覆盖缺失数组/对象、错误 code、stale device response。
- [ ] device 切换 abort 请求并清空 draft/上传/密码/job selection。
- [ ] wizard 在 blocker、warning、ack、step-up、202 job 各状态有明确转移。
- [ ] rules 使用 cursor pagination，不把 50,000 条载入浏览器。

实现：

- [ ] `App.tsx` 仅新增 `hostSettingsExpanded`、ActiveView、sidebar/topbar 接线和 feature render。
- [ ] 建立 `web/src/features/policy-routing/` types/api/hooks/components。
- [ ] Access onboarding、overview、egress/source CRUD、URL/upload preview、wizard、ChangePlan、job、drift、adoption 页面。
- [ ] active job 1 秒轮询，隐藏页面降频/停止，恢复可见立即刷新。
- [ ] 管理员密码只存在最终表单 local state，提交后清空；禁止 localStorage/sessionStorage。
- [ ] CSS 使用现有 tokens、mobile drawer、44px touch target、内部表格滚动和无 document overflow。
- [ ] 更新前端共享类型；不在多个组件重复 cast 同一 payload。

确定性验证：

```bash
npm --prefix web run lint
npm --prefix web run build
npm --prefix web audit --audit-level=high
go build -o ./rosboard ./cmd/rosboard
go test ./...
git diff --check
```

用户手工视觉清单（本地 embedded build）：

- [ ] 1440、1024、768、375px 打开/关闭主机设置菜单，无横向页面溢出。
- [ ] 未配置账号、权限不足、版本阻止、正常 overview、drift、foreign owner、active job 各状态可读。
- [ ] URL 与上传向导、差异分组、风险确认、移动端表格/任务进度可操作。
- [ ] light/dark、键盘 focus、ARIA expanded/tab/menu、danger action 正确。

回滚点 R9：移除 feature render/nav 即可隐藏 UI；backend 不自动运行未启用设备策略。

### Phase 10：Scheduler、健康、接管与完整生命周期

对应：FR-12、FR-15、FR-16；AC-13、AC-14、AC-16、AC-17、AC-21。

先写测试：

- [ ] next-run 持久化、稳定 jitter、manual 优先、304 no job、同设备串行、跨设备不阻塞。
- [ ] 结构变化、>50% 缩减、cap、drift、inactive route、foreign owner 均 pending review。
- [ ] health configured/degraded/action-required 与 manual healthy 降级。
- [ ] exact manual adoption、partial/mixed 拒绝、reuse vs adopt、release、foreign takeover。
- [ ] restart 后 queued/running/rollback job 对账决策。

实现：

- [ ] 启动 scheduler/health checker，使用设备/source hash 错峰。
- [ ] adoption/discovery 与 release/forced takeover plan。
- [ ] UI 补齐 pending review、resume/rollback、manual healthy 与 audit/download。
- [ ] 删除/停用/迁移、最后来源、出口删除、account cleanup 全生命周期。

验证：

```bash
go test ./internal/policy ./internal/api ./internal/store
go test -race ./internal/policy ./internal/api ./internal/store ./internal/service
go test ./...
go vet ./...
npm --prefix web run lint
npm --prefix web run build
npm --prefix web audit --audit-level=high
git diff --check
```

回滚点 R10：关闭所有 policy access 后 scheduler 不创建任务，Monitor 继续正常。

### Phase 11：全量本地验证与安全审计

对应：全部 AC 的非实机部分。

- [ ] `gofmt` 仅作用修改的 Go 文件。
- [ ] 运行全部测试、race、vet、frontend lint/build/audit。
- [ ] 搜索秘密、任意 RouterOS execute/path、password projection、未分页规则和 TODO/TBD。
- [ ] 用 fake RouterOS 做 50,000 条规模、故障注入、取消、restart reconciliation。
- [ ] 启动本地 embedded build；验证 `/api/health`、bootstrap、现有 dashboard、策略 disabled/setup/read-only 状态。
- [ ] 确认监控 `updatedAt` 在策略 preview/large fake job期间继续推进。
- [ ] 准备 `10.0.0.99` 只读 discovery 报告和拟写计划，但不写设备。

命令：

```bash
gofmt -w <changed-go-files>
go test ./...
go test -race ./internal/...
go vet ./...
npm --prefix web run lint
npm --prefix web run build
npm --prefix web audit --audit-level=high
go build -o ./rosboard ./cmd/rosboard
zsh -n scripts/run-local.sh
git diff --check
python3 ./.trellis/scripts/task.py validate 08-20-policy-routing
```

门禁 G1：向用户提交只读 discovery、backup 位置、计划和预计影响；取得明确许可后才进入 Phase 12。

### Phase 12：`10.0.0.99` 分阶段实机验收

禁止把凭据写进命令历史、文件、测试 fixture、环境导出或日志。使用交互式/内存输入并在步骤后清理。

#### 12.1 只读与权限

- [ ] 验证 identity、版本、free-memory、现有 WAN/LAN、route、DNS、firewall、NAT、FastTrack。
- [ ] 生成/执行专用策略账号初始化，验证权限矩阵后不再使用高权限账号。
- [ ] 备份 RouterOS export 和 affected snapshot，下载并校验 hash。

#### 12.2 小规模行为

- [ ] 使用小型上传 YAML，创建一个 IPv4 strict 出口；验证 table/route、Fake DNS、FWD、address-list、mangle、NAT、顺序和清理。
- [ ] 启用 IPv6，验证同名列表和独立 family 状态；没有 AAAA 的样本换用明确双栈域名。
- [ ] 用户从 LAN 客户端确认实际出口并标记 healthy；rosboard 不调用公共 IP API。

#### 12.3 生命周期与故障

- [ ] 两来源共享列表、重复规则引用、删除一个来源保留共享条目。
- [ ] 专用列表与优先级、跨出口域名冲突 blocker。
- [ ] URL refresh、304、>50% 缩减 pending review、本地 YAML replacement。
- [ ] 人工制造一个可恢复 drift，验证暂停与恢复/停止管理。
- [ ] 在安全步骤注入失败/取消，验证 targeted rollback；不测试会导致管理失联的破坏性故障。
- [ ] 删除来源/出口，只清理自有对象并保留复用对象。

#### 12.4 扩展规模

- [ ] 在 RouterOS free-memory 和用户允许范围内逐级增加规则；不要求实机达到 50,000 才算通过。
- [ ] 验证 >10,000 警告和任务进度可在 fake scale 证明，实机规模由资源检查决定。

实机结束：

- [ ] 生成最终对象清单与 health 报告。
- [ ] 询问用户保留实验策略还是清理所有 rosboard 自有对象。
- [ ] 若清理，读回证明无自有对象残留；策略账号按用户选择保留或提供清理脚本。

门禁 G2：实机结果获用户确认后，才准备部署 `10.0.0.6`。

### Phase 13：部署 `10.0.0.6` 与人工验收

严格执行仓库 acceptance gate：

1. [ ] 确认 `/Users/tom/nas/wyp/github/rosboard/backups/` 已挂载且可写。
2. [ ] 列出备份目录并验证仅匹配时间戳目录；创建第 11 份前只删除最旧一份。
3. [ ] 在 NAS 新建 `<timestamp>-policy-routing/`，备份远端现有 binary、配置、SQLite data 和 service unit。
4. [ ] 校验备份文件大小/hash，可用于恢复；不得在 `10.0.0.6` 创建开发 rollback backup。
5. [ ] 部署新 binary/assets/config migration，重启 systemd。
6. [ ] 验证 service active、`/api/health`、bootstrap、现有 dashboard contracts、策略 API、embedded JS/CSS hash 和监控 polling。
7. [ ] 向用户提供 URL、桌面/移动检查步骤和已知风险，等待用户人工检查。

门禁 G3：用户未明确批准，不得 commit、归档任务或删除部署前备份。

### Phase 14：批准后收尾

- [ ] 根据实现中形成的稳定契约更新相关 `.trellis/spec`，不把临时实现细节写成规范。
- [ ] 再运行全量确定性检查和 `git diff --check`。
- [ ] 检查 diff 只包含任务相关文件与用户已知生成 assets；保护用户原有无关修改。
- [ ] 创建有意图的 commit；记录 hash。
- [ ] 使用 Trellis finish/archive/session 流程记录结果。
- [ ] 不主动 push 或创建 PR，除非用户另行要求。

## 5. 验收追踪矩阵

| PRD 验收 | 主要实施阶段 |
|---|---|
| AC-01 导航与设备隔离 | 8、9、11 |
| AC-02 凭据与最小权限 | 1、8、12 |
| AC-03 版本能力 | 1、5、12 |
| AC-04 URL/解析安全 | 3、8 |
| AC-05 上传 YAML | 3、8、9 |
| AC-06 shared/dedicated | 2、6、9、12 |
| AC-07 IPv4/IPv6 | 5、6、7、12 |
| AC-08 route 补齐 | 5、6、7、12 |
| AC-09 strict/fallback | 5、6、12 |
| AC-10 DNS/mangle/NAT/FastTrack | 4 至 7、12 |
| AC-11 防火墙风险 | 5、6、8、12 |
| AC-12 冲突/优先级 | 5、6、9、12 |
| AC-13 同步/last-good | 2、3、6、10、12 |
| AC-14 drift | 5、7、10、12 |
| AC-15 rollback | 4、7、11、12 |
| AC-16 durable jobs | 2、7、10、11 |
| AC-17 adoption/multi-instance | 1、5、6、10 |
| AC-18 scale/UI pagination | 2、3、6、7、9、11 |
| AC-19 history retention | 2、3、10 |
| AC-20 lifecycle cleanup | 2、6、7、10、12 |
| AC-21 verification/healthy | 7、9、10、12 |
| AC-22 `10.0.0.99` | 12 |
| AC-23 `10.0.0.6` gate | 13、14 |

## 6. 停止条件

任一情况出现时停止当前阶段并报告，不用更强权限或更宽范围重试：

- 实际 RouterOS 字段/行为与设计不符且会改变安全语义。
- 不能证明对象所有权、route 最终出口或 firewall 安全插入位置。
- NAS 未挂载/不可写、备份不完整或 hash 不符。
- RouterOS backup/export 含疑似敏感信息或无法清理远端临时文件。
- 删除、rollback 或目标文件操作失败。
- 实机出现管理连接不稳定、路由泄漏、开放解析器风险或 free-memory 危险下降。
- 工作树无关修改与任务文件发生不可分离冲突。
- 用户撤回实机、部署或范围授权。
