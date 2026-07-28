# 首次安装初始化与多 ROS 设备配置：实施计划

## Success criteria

实施完成的定义是：PRD 的 AC1–AC18 全部有自动化或运行时证据；全新 data/config 环境能完成管理员初始化、登录、RouterOS 测试与多设备保存或跳过；认证和设置接口不泄露密码；本地与远程部署验证通过；用户在 10.0.0.6 手工验收后才允许提交。

## Ordered implementation

### 1. 建立认证与初始化持久化

- 在 internal/store 增加 admin_account、auth_sessions、app_state 的幂等 schema 和最小查询/事务方法。
- 新建 internal/auth，集中用户名/密码校验、Argon2id 参数化 hash/verify、会话 token、七天滚动续期和全会话撤销。
- 实现进程内登录限速器、Argon2id 并发上限及确定性时钟/随机源注入，便于测试。
- 为唯一管理员并发创建、密码不 trim、会话 hash/过期/续期/撤销、初始化状态写入补齐单元测试。
- 验证：go test -race ./internal/store ./internal/auth。

### 2. 重构 API 启动边界与认证中间件

- Server 接入 auth service；保持 allowed_cidrs 为第一道检查。
- 增加 bootstrap、setup admin、login/logout、原子 account credentials、setup complete API。
- 为写请求增加同源校验，为 auth cookie 设置 HttpOnly、SameSite、Path、Max-Age 和条件 Secure。
- 明确公开路由白名单，其他 API 默认要求有效会话。
- 覆盖 phase 矩阵、401/409、并发注册、限速、Cookie、退出/改密撤销和 allowed CIDR 顺序。
- 验证：go test -race ./internal/api。

### 3. 实现 RouterOS 能力验证

- 在 internal/routeros 增加共享 Verify 服务、必需/可选 probe 分类、安全错误分类和重定向限制。
- 调整 Monitor full refresh 使用同一必需/可选分类；可选能力失败进入 warnings，不阻断快照。
- 在 API 增加 test-connection、15 分钟一次性验证票据、连接指纹和票据消费。
- 使用 httptest RouterOS 假服务覆盖认证失败、必需权限失败、可选端点警告、超时、TLS/格式错误、票据过期、字段变化和重放。
- 验证：go test -race ./internal/routeros ./internal/service ./internal/api。

### 4. 收紧设备保存合同

- 统一新增/编辑请求规范化：已有设备空密码保留、新设备密码必填。
- 新增/连接字段变化要求有效票据；非连接字段编辑免票据。
- 后端执行 endpoint 去重（含 archived）、接口非空/存在、所选接口 monitor-traffic 权限、CIDR canonicalize/去重/非空。
- 把 config.Save 收紧为同目录 0600 临时文件 + 原子 rename，失败保留旧 YAML；覆盖写入失败和临时文件清理。
- 保证所有远端验证成功后才写 YAML；初始化“保存设备”不重启并刷新多设备列表，仅设备编辑页“完成设置”或选择页的跳过/进入面板动作标记 onboarding complete，存在已保存设备时进入面板才重启采集服务。
- 删除 settings/device 响应中的密码原文，前端合同只保留 passwordSet。
- 覆盖新增、编辑、重复 endpoint、归档冲突、无效接口/CIDR、票据消费、YAML 失败、原子替换和完成标记失败后的恢复。
- 验证：go test -race ./internal/config ./internal/api。

### 5. 实现本机密码重置命令

- 把 cmd/rosboard/main.go 拆为默认 server 路径和 admin reset-password 子命令，不改变现有 rosboard -config 调用。
- 使用 golang.org/x/term 无回显读取两次密码；复用 auth 验证/哈希；禁止命令行明文密码。
- 覆盖无管理员、非 TTY、确认不一致、成功重置及会话撤销。
- 验证：go test ./cmd/rosboard；交互式本地 smoke test 确认终端不回显。

### 6. 重构前端启动状态机

- 在 web/src/lib/types.ts 定义 bootstrap/auth/test-result 的唯一类型合同。
- App 最外层改为 bootstrap router；移除以 dashboard/error 推断初始化的旧路径。
- 新增管理员创建、登录和 RouterOS onboarding 页面。
- 401 时清除内存敏感状态并回到 bootstrap；密码与 verification token 不写 localStorage/sessionStorage。
- 验证：npm run lint、npm run build。

### 7. 复用设备编辑器并完成空面板

- 把初始化与设置页设备编辑收敛到同一个 Connection → Verified → Collection → Save 状态机。
- 测试前锁定接口/CIDR；连接字段变化清除票据；显示身份、warning、接口和 CIDR 候选。
- 已有设备密码默认为空并提示留空保留。
- 增加紧凑单列账号安全页、独立退出入口及无设备 shell/监控空状态。
- 保持设备切换、禁用、归档、恢复、永久删除和响应式布局。
- 运行 build 后确认 internal/ui/dist 嵌入资源已更新。
- 验证：npm run lint、npm run build、npm audit --audit-level=high。

### 8. 文档和配置合同

- 更新 README 的首次安装、管理员登录、本机重置密码、allowed_cidrs 抢注风险、设备测试流程和 HTTPS 建议。
- 更新 configs/config.example.yaml，使全新安装示例不预置假设备，同时保留明确的监听、data dir、轮询和 allowed CIDRs。
- 修正 README 中“仅支持单设备”等过时描述。
- 最终阶段通过 trellis-update-spec 更新 backend runtime configuration/database 与 frontend component guideline，记录新的认证、凭据投影和设备验证合同。

### 9. 完全重新初始化

- 在维护设置新增独立警示区域和实心警示色按钮；只使用一次确定/取消确认，不要求输入文字。
- 新增受认证的 full-reset API，删除配置文件，并用一个 SQLite 事务清空管理员、会话、初始化状态和全部监控表。
- 清除验证票据与 session cookie；受 supervisor 管理时关闭旧数据库连接并重启，前端清理 Rosboard 浏览器状态后等待首次初始化页恢复。
- 覆盖未确认不变更、确认后配置文件消失、全部数据库状态清空以及 bootstrap 回到 needs_admin。

## Automated quality gate

从仓库根目录执行：

    gofmt -w <本任务修改的 Go 文件>
    go test ./...
    go test -race ./internal/store ./internal/auth ./internal/routeros ./internal/service ./internal/api
    go vet ./...
    cd web && npm run lint
    cd web && npm run build
    cd web && npm audit --audit-level=high
    zsh -n scripts/run-local.sh
    git diff --check

如果新增 internal/auth 的包路径与最终实现不同，race 命令按实际包调整，但不得缩小覆盖。

## Local runtime and visual verification

使用临时 config/data 路径启动全新实例，不能复用当前 data/rosboard.db：

1. 未初始化桌面与 375px：创建管理员页面、键盘焦点、密码确认和错误布局。
2. 并发/刷新：创建后刷新进入 RouterOS 步骤，退出后登录仍回到该步骤。
3. RouterOS failure matrix：错误 IP、错误密码、缺少必需权限、可选 endpoint 缺失。
4. 成功 test：身份、warnings、接口和 canonical CIDR 候选正确。
5. 修改连接字段使票据失效；空接口/CIDR、坏 CIDR、重复目标均不能保存。
6. “保存设备”后服务不重启，设备立即出现在初始化列表且可继续添加；“完成设置”与保存按钮等尺寸但使用不同背景色，可直接保存尚未单独保存的当前设备并完成初始化，只重启一次且恢复后直接显示 Dashboard 和设备切换。
7. 第二台设备添加、归档、恢复和删除不影响第一台。
8. 全新实例选择跳过，完整 shell 与所有监控空状态可用，之后仍可添加设备。
9. 设置与导出 payload、浏览器 storage、控制台和服务日志不含明文密码。
10. 账号与密码原子保存、全会话撤销、独立退出、七天过期（用测试时钟）和 CLI reset 行为符合合同。

使用浏览器开发工具检查无 console error、无失败资源、无 document-level overflow，并确认桌面与 375px 的触控目标和表单布局。

## Deployment acceptance gate

程序改动必须遵循仓库 AGENTS.md，且不得在手工验收前提交：

1. 构建最终前端嵌入资源和 Go binary。
2. 在 10.0.0.6 上解析实际 binary、config、SQLite 路径。
3. 创建同一时间戳的备份目录，停止服务后完整备份旧 binary、config 和 SQLite（包括 SQLite sidecar 文件，如存在），记录权限与 owner。
4. 部署已本地验证的 binary/静态资源，保留目标 config/data 路径和权限，启动 systemd。
5. 验证 systemd active、journal 无凭据、/api/health、公开 bootstrap、未认证 401、登录、受保护 API、设备 API、RouterOS test/save 合同和当前嵌入 JS/CSS 资产。
6. 验证至少两个轮询周期 updatedAt 前进，并检查设备切换及受影响页面。
7. 请用户在部署实例手工检查初始化/登录/设备管理/空状态；等待用户明确批准。
8. 只有批准后才运行最终 Trellis check、更新 spec、创建工作提交、记录 session 并归档任务。

## Rollback

- 远程验收失败时停止服务，成套恢复同一时间戳的 binary、config、SQLite 及 sidecar，再启动并验证 health/systemd。
- 不允许只回滚 binary 而保留新数据库或新前端资产。
- 本地实现回滚点按步骤 1–7 的边界审查；不使用破坏用户现有改动的 git reset/checkout。

## Risky files and review focus

- internal/store/sqlite.go：schema 与监控数据必须互不破坏。
- internal/api/server.go：默认拒绝、公开路由白名单、凭据投影、并发 config save。
- internal/routeros/client.go / verification：Basic Auth 不泄漏，redirect 不跨 host。
- internal/service/monitor.go：可选能力降级不得掩盖必需采集失败。
- web/src/App.tsx：现文件较大，只做与状态机和共享设备编辑直接相关的抽取，不顺带重构监控页面。
- cmd/rosboard/main.go：保持 systemd 现有启动参数兼容。
- internal/ui/dist：必须由 web build 生成，不手改 bundle。

## Final review checklist

- [ ] PRD AC1–AC18 逐项有证据。
- [ ] 没有浏览器可见的 RouterOS 明文密码响应。
- [ ] 没有通过前端绕过 test ticket 或服务端字段校验的路径。
- [ ] allowed CIDR、认证、初始化 phase 的执行顺序一致。
- [ ] 全新初始化和跳过两条主路径均可恢复。
- [ ] 多设备数据隔离回归通过。
- [ ] 本地视觉/运行时、远程部署和用户手工验收完成。
- [ ] 用户批准前没有 commit 或 task archive。
