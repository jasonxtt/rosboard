# 技术设计

## 范围与约束

改动限定在 `web/src/features/policy/` 的策略向导、步骤导航、预览组件及其必要样式。采用向导组件内部 React state，不引入全局状态管理或新的测试基础设施。后端 `/plans` 与 `/plans/{id}/apply` 合约保持不变。

## 状态模型

- `activeStep`：当前展示页，范围 0–3。
- `maxUnlockedStep`：本次向导中通过“下一步”正式解锁的最远可编辑步骤；新增策略初始为 0，已有策略初始为 2，成功生成预览后为 3，单调不回退。
- `draftRevision` 及 ref：任何影响提案的本地编辑递增，用于判断计划是否对应当前草稿并防止过期响应覆盖状态。
- `planDraftRevision`：生成当前 `plan` 时的草稿版本。仅当两者相等时才渲染可应用预览。
- `generating`、预览组件上报的 `applying`：统一阻止冲突操作。

## 导航与校验

`PolicyWizardSteps` 只负责显示状态和发出点击事件，不保存第二份状态。所有标题都保留原生 button；锁定步骤使用可访问的只读文案和样式区分，表单通过 `fieldset disabled` 实际禁用，不能依赖 CSS 拦截。

父组件统一实现三个步骤校验和 `advanceFromStep`：

1. 策略与来源：规则名非空且来源范围有效。
2. 访问目标：至少一个目标列表。
3. 出口：复用现有出口校验。

步骤 4 的点击逻辑为 `validate all -> generate current proposal -> show fresh preview`。未解锁步骤 4 时只显示占位，不请求后端；已解锁且计划仍对应草稿时直接复用现有预览。

## 计划一致性与并发

生成前捕获请求版本和提案快照。生成期间禁用编辑；响应返回后再次比对版本，过期响应不得覆盖 `plan`。草稿修改只使预览失效，不删除或重置目标、预设、出口等下游选择。

## 组件边界

- `RoutingRuleWizard.tsx`：状态、校验、导航、草稿版本和预览生成。
- `components.tsx`：步骤标题的 active/done/locked 状态、ARIA 文案和忙碌禁用。
- `PolicyPlanPreview.tsx`：继续持有应用流程，通过 `onBusyChange` 向父组件报告应用中状态。
- 仅在 `fieldset disabled` 无法覆盖某个控件时才修改子选择器；不扩展到目标库、预设物化或后端路由。
