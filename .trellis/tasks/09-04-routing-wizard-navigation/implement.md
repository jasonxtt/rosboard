# 实施计划

- [x] 重新检查策略向导、步骤导航、预览组件和样式的现状，确认现有用户改动不被覆盖。
- [x] 在 `RoutingRuleWizard` 中加入 active/unlocked/revision/plan-fresh 状态，集中封装草稿变更、三步校验、前进和步骤 4 预览生成。
- [x] 将后续步骤渲染为可查看但实际禁用的表单；实现已解锁步骤之间的自由跳转和失效预览刷新。
- [x] 更新 `PolicyWizardSteps` 的可访问锁定/完成状态及忙碌禁用表现。
- [x] 让 `PolicyPlanPreview` 上报应用忙碌状态，阻止应用期间关闭和导航。
- [x] 补充最小必要样式，避免无关 UI 重构。
- [x] 运行 lint、build、diff check；检查变更路径、敏感信息和 staged diff。
- [x] 在当前工作分支提交并推送 checkpoint，向项目审核者汇报实现和验证结果。
- [x] 根据审核意见继续修改并重复验证，直到审核者明确报告 `APPROVED`。
