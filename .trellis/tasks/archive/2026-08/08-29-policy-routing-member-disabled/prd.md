# 修复策略路由自动同步与对账默认字段

## Goal

修复策略路由应用计划在对账已有 RouterOS 对象时，因 RouterOS 返回的默认字段被错误清空而失败的问题，并恢复普通保存即同步 RouterOS 的交互语义，确保新增出口和域名规则能够完整写入。

## Requirements

- 保持现有策略路由的期望对象和 RouterOS 所有权语义不变。
- 对已有受管对象生成 patch 时，不得发送未出现在该对象期望字段中的 RouterOS 默认字段空值；删除操作仍可正常清理受管对象。
- 不改变下一跳网关、路由表、DNS、mangle 或用户现有 RouterOS 对象的其他行为。
- 保留旧字段清理能力，增加回归测试覆盖 interface-list member 和 DNS forwarder 返回 `disabled=false`、而期望对象未声明该字段的场景，并确认不会生成非法字段 patch。
- 新建或编辑已归属启用策略的域名列表，保存后自动生成并执行同步任务，不再要求用户再次点击“应用”。
- 策略向导保存出口、流量入口和域名列表后只执行一次自动同步；向导分步保存不得触发中间态的重复并发同步。
- 自动同步仅在普通保存链路执行；阻断项、人工接管、漂移恢复等需要人工判断的计划仍保留显式确认入口。
- 自动刷新到已归属启用策略的 URL 域名列表时自动同步；未分配或所属策略停用时保留 pending，待下一次普通保存或启用策略时同步。

## Acceptance Criteria

- [x] 针对已有流量入口成员和 DNS forwarder 生成计划时，不再产生 `disabled: ""`（或等价非法值）的 patch。
- [x] 已归属启用策略的域名列表保存接口返回同步任务，任务成功后 source 版本直接提升为 active，前端不显示“已引用（待应用）”。
- [x] 普通策略向导保存成功后自动同步并关闭，不再展示必须点击的“预览并应用”步骤；阻断计划仍可解释并处理。
- [x] 已归属 URL 的定时刷新能够自动同步，未分配/停用策略的 URL 刷新仍安全保留 pending。
- [x] 既有对账、字段清理和应用操作测试保持通过。
- [x] `go test ./...`、`go vet ./...`、前端 lint/build 和 `git diff --check` 通过。
- [x] 本地构建/运行检查完成；已按项目门禁完成 net 部署备份、远端服务/API/嵌入资源验证，并经用户手动验收后提交代码。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
