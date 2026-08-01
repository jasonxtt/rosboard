# 资源监控实施计划

## 1. 建立数据契约

- 扩展 RouterOS 类型和 client，读取 `/system/resource`、`/system/resource/cpu`、`/system/resource/irq`、`/system/resource/hardware` 的只读字段。
- 在模型与 API 快照中增加完整的 `systemResource` 映射，包含顶层资源、逐核 CPU、IRQ 和硬件详情。
- 将逐核 CPU 纳入 realtime，将 IRQ/硬件/低频字段纳入 full refresh，并保持资源字段与更新较新的快照合并。
- 验证：Go 单元测试覆盖完整字段映射、空详情快照和较新 realtime 数据的资源字段保留。

## 2. 实现前端资源监控

- 扩展 `ActiveView`、导航分组、landing view 和相关活动状态分支。
- 扩展资源监控页面，展示 CPU、逐核 CPU、内存、存储、系统信息、IRQ 和硬件设备详情。
- 使用四列窄卡片网格；CPU 与系统信息同一行，卡片允许跨列并自然增高，移动端降为单列。
- 复用现有格式化、卡片、设备选择、刷新和错误状态组件；详情请求仍由 Monitor 统一采集并进入现有 dashboard/realtime 快照。
- 对空值、零总量和旧 API 缺少 `systemResource` 做安全展示。
- 验证：TypeScript 构建、lint，并通过源代码检查确认资源页使用现有 API 数据。

## 3. 回归验证

- 运行 `gofmt`、`go test ./...`、`go test -race ./internal/service ./internal/api`。
- 运行 `npm --prefix web run lint`、`npm --prefix web run build` 和项目要求的依赖审计。
- 构建嵌入式 Linux amd64 程序并启动本地实例，确认 `/api/health`、dashboard/realtime 契约和嵌入前端资源。
- 按 375px 和 1440px 检查导航、卡片布局、无横向溢出、空值和刷新状态；浏览器检查只作为本地视觉验证，不代替用户验收。

## 4. 远端部署验收

- 只读确认 `10.0.0.6` 当前服务、路径和健康状态。
- 停止服务前，以同一时间戳备份现有二进制、配置和 SQLite 数据。
- 安装已验证的 Linux amd64 构建，启动 `rosboard.service`。
- 验证 systemd active、`/api/health`、资源字段、设备作用域、更新时间推进和嵌入式资源加载。
- 如果启动或 API 验证失败，使用该时间戳备份恢复旧二进制/配置/数据。
- 等待用户手动检查资源监控页面并明确批准；在批准前不创建提交。

## 5. 完成与回收

- 根据检查结果修复问题并重复质量验证。
- 若发现具有通用价值的新约定，再更新对应 Trellis spec；无新增约定则不扩写无关规范。
- 用户验收通过后提交代码，记录任务和会话，并按 Trellis 流程归档。

## 执行记录

- 已通过 `go test ./...`、`go test -race ./internal/service ./internal/api`、`npm --prefix web run lint`、`npm --prefix web run build` 和 `npm --prefix web audit --audit-level=high`。
- 已使用临时实例验证 `/api/health`、嵌入式 HTML、JS 和 CSS 均返回 200。
- 已部署 Linux amd64 构建，校验和为 `996022f06af6f57538374bd20abddb56cacb1c5fc43be990fefb0bd363038e64`。
- 远端备份目录：`/opt/rosboard/backups/20260801-134818-system-resource-monitor`。
- 远端 `rosboard.service` 当前 active，健康接口返回 200，嵌入式资源包含“资源监控”；认证保护下的 dashboard/realtime 字段等待用户登录后手动验收。
- 本次详情扩展已通过本地模拟 RouterOS 验证：顶层资源、逐核 CPU、IRQ、硬件详情均进入 `/api/dashboard`；1440px 下 CPU/系统信息同排，375px 下单列且无横向溢出。
- 本次远端备份目录：`/opt/rosboard/backups/20260801-144526-system-resource-detail`；部署二进制校验和：`f6e571b2651e9ed98752342b998b92c1231046e7831082fec5372a45b2c2ecc3`。
- 本次部署后远端服务 active、`/api/health` 返回 200、`/api/bootstrap` 显示已完成初始化，未认证访问 `/api/realtime` 返回预期 401；等待用户登录后的资源页面手动验收。
