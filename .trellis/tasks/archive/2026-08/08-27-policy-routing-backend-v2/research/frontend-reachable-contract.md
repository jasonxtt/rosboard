# 当前策略路由前端可达契约

提取日期：2026-08-27。来源是 `web/src/features/policy-routing/PolicyRoutingPage.tsx`、其直接渲染组件、`hooks.ts` 和 `api.ts`。本文件只用于 V2 实施时区分“当前页面需要”与“历史 API 仍存在”。

## 当前主流程需要

| 前端行为 | API |
|---|---|
| 页面状态与轮询 | `GET /overview` |
| WAN/LAN 选择 | `GET /discovery` |
| LAN 范围 | `PUT /lan-scope` |
| 出口配置 | egress GET/POST/PUT/DELETE |
| 来源配置 | source GET/POST/PUT/DELETE |
| 来源内容 | URL/upload preview、`GET /sources/{id}/rules` |
| 应用预览 | `POST /plans` |
| 确认应用 | `POST /plans/{id}/apply` |
| 运行提示 | overview 的 `activeJobs`，以及 apply 响应 `jobId` |

`ChangePlanView` 需要计划顶层 ID、时间、revision、actual fingerprint、hash、summary、operations、blockers、warnings、acknowledgements 和 requiresStepUp。内部不需要复刻旧 ChangePlan，只需由 API DTO 填充这些字段。

## 保留的设置能力

`PolicyAccessCard.tsx` 当前没有由 `PolicyRoutingPage` 直接渲染，但策略运行时仍依赖 access/setup 状态。V2 复用现有 access、provisioning 和 cleanup 实现，不在本任务重写其安全逻辑。

## 当前不可达或无调用

- `PolicyTakeoverModal` 未被页面引用。
- `PolicyAuditDialog` 未被页面引用。
- `fetchJob/cancelJob/resumeJob/rollbackJob` 没有组件调用。
- takeover preview、adoption preview、audit、backup download 不在当前页面主流程。
- drift panel 只有 overview 主动报告 drift/paused 才出现；V2 首版不报告自动恢复入口。

这些能力不是 V2 首版验收项。不得因为 `api.ts` 或 `types.ts` 中还存在函数/类型，就自动扩大后端范围。
