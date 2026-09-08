# 前端全面重做 · Aurora 玻璃拟态

## Goal

在 `feat/policy-access-rebuild` 的后端功能基线上（前后端联调已验收），完全抛弃现有 UI，
以用户评审选定的「05 Aurora 玻璃拟态」风格（`design/mocks/05-aurora.html`）重写全部前端，
使后端已实现的监控功能可方便快捷查看、策略与面板设置可方便快捷配置，同时兼顾美观。

## UX 标准

**唯一权威**：`design/UX-STANDARD.md`（本分支专属规范，不写入项目级 AGENTS.md）。
任何前端改动必须遵守该文件的 token、组件、页面范式与工程约束。

## 用户已确认的决策

1. 风格：05 Aurora 玻璃拟态（深色默认 + 浅色等价变体）。
2. 明暗双主题，`data-theme` 切换，偏好持久化。
3. 技术栈不变：React 19 + TS + Vite，保留 `web` → `internal/ui/dist` 嵌入链路，后端零改动。
4. 界面文案：简体中文。
5. 页面范围不裁剪：15 个视图全部重做（仪表台/系统概览/接口/终端+详情/流量协议/策略计数/
   DHCP/路由/系统资源/负载历史/目标库/策略路由+向导/访问控制/识别设置/面板设置五区）
   + 登录与初始化向导（needs_admin/needs_login/needs_routeros）。
6. 支持移动端（<768px：底部药丸导航、单列布局、底部抽屉弹窗）。
7. 图表库从 ECharts 换为 uPlot；环形表/sparkline 手写 SVG。
8. 规范仅在 `feat/ui-rebuild` 分支内生效（`design/` + 本任务夹随分支存在）。

## Requirements

- 新前端必须完整覆盖后端 API 面（见下方 API 清单摘要），不得丢弃现有功能：
  bootstrap 阶段门、会话登录、viewer-heartbeat 快轮询模型、设备作用域 `?device=`、
  策略域 202 + job 轮询、目标库 previewId 流、计划审查（blockers/warnings/acknowledgements）、
  设备快速接入（脚本向导）与手动添加（验证令牌）双通道、归档/清理脚本、采集/识别设置、
  账号安全、维护（导出/重启/完全重置）。
- 遵守 `.trellis/spec/frontend/` 工程规范：API 边界解析 unknown、禁 any、
  设备切换重置状态、密码等敏感值不进持久存储。
- 分支与 PR：全部工作在 `feat/ui-rebuild` 分支 + 一个 Draft PR（base: feat/policy-access-rebuild）。
- 每切片完成必须 `npm --prefix web run lint && npm --prefix web run build` 全绿。

## Acceptance Criteria

- [x] 15 个视图 + 登录/初始化在新风格下功能完备（与旧前端逐项对齐）。
- [x] 明暗主题切换无写死色值；移动端 375px 无横向滚动。
- [x] echarts 依赖移除，uplot 正常工作；`web` 构建产物正确嵌入 `internal/ui/dist`。
- [x] lint/build 全绿；Go 侧 `go build ./...` 与 `go test ./...` 全绿。
- [x] 用户验收通过（2026-09-08，本地预览实例 127.0.0.1:8090，含多轮交互修复与
  「未引用预设目标库可删除」前后端改动）；合并 main 与生产部署时机由用户另行决定，
  Draft PR #7 保持未合并。

## Completion Notes（2026-09-08）

- 基座 + 4 切片并行实现全部页面；其后按用户验收反馈完成十余轮修复：Modal 轮询抢焦点
  bug、概览 hero 合并与 5:3 布局、信息卡两列 5+5、二级菜单 mega popover、表格数字对齐
  泄漏、终端列宽、「正在发生」卡片移除（方案 B）、接口全表入概览、Y 轴自适应。
- 越界但经用户批准的后端小改动：预设目标库删除保护从「一律禁止」改为「仅被引用时禁止」
  （internal/store/policy.go），store/api 测试护航；前端目标库默认折叠未引用预设。
- UX 标准继续维护在 design/UX-STANDARD.md（分支专属）。

## Slices

0. 基座：tokens/基础样式、外壳（顶栏/设备切换/告警/主题）、UI 组件库、图表封装、API 层、全部视图占位。
1. 初始化向导 + 登录 + 仪表台 + 系统概览（hero 签名页）。
2. 监控详情页群：接口(+详情)、终端(+详情/元数据编辑)、流量协议、策略计数、DHCP、路由、系统资源、负载历史。
3. 策略页群：目标库（含预览/刷新/版本）、策略路由（规则表 + 四步向导 + 计划审查 + job）、访问控制。
4. 设置页群：设备管理（双通道接入/账号/归档）、采集、识别、界面偏好、账号安全、维护；移动端打磨；空设备壳。

## API 参考（后端契约来源）

- 路由注册：`internal/api/server.go:124-428`（监控/设置/设备）、`auth.go`（会话/初始化）、
  `policy_routing.go` / `routing_rules.go`（策略路由 v2）、`access_control.go`、`target_lists.go`、
  `application_presets.go`、`provisioning.go`（快速接入）、`verification.go`。
- 载荷结构：`internal/model/types.go`（监控）、`internal/policyv2/*`（策略）、
  `internal/accesscontrol/model.go`、`internal/service/manager.go`（fleet/识别）。
- 旧前端契约参照（重写时逐字段对齐）：`web/src/lib/types.ts` 与 `web/src/features/policy/canonical.ts`。
