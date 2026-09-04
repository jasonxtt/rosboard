# 手动目标列表编辑内容回填

## Goal

修复目标列表编辑器的内容回填缺失：用户保存手动域名列表后再次打开编辑，必须能看到并继续编辑已经保存的域名规则。

## Requirements

- 编辑已有手动目标列表时，按目标 ID 读取当前已经保存的内容，并回填到内容文本框。
- 目标列表摘要接口保持轻量；完整内容只在打开编辑器时按需读取。
- 回填内容需要保证域名规则的语义完整、顺序稳定，可重新预览和保存；不要求保留原始空格、空行或注释。
- 读取的内容必须来自当前已提交版本；不能读取未保存的 preview 或其他临时 proposal 数据。没有 active 版本时，允许使用当前唯一的 pending 版本作为新建但尚未应用目标列表的可编辑内容。
- 内容读取期间禁止预览和保存；读取失败时必须保留错误状态，不能把空文本当成用户清空后继续保存。
- 仅修改名称等元数据时，不要求重新生成内容版本，已有内容必须保持不变。
- 已有手动内容被修改后，必须先“预览并校验”并使用对应的 preview ID 保存；不能无提示地忽略文本修改。
- 新建目标列表仍从空文本框开始；URL、上传和应用预设的既有行为保持不变。
- GET 详情及内容读取只能读数据，不得增加 revision、创建 version、promotion、policy plan 或 apply。
- 保持 `proposal → preview → apply`、TargetList canonical/API 边界，不修改向导导航、计划预览或无关策略功能。

## Acceptance Criteria

- [ ] 新建手动域名列表，粘贴多条域名并保存；再次编辑时文本框显示全部已保存规则。
- [ ] 编辑回填内容后新增、删除域名，重新预览并保存；再次编辑结果与修改一致。
- [ ] 只修改目标列表名称并保存，内容不丢失且不产生新的内容版本。
- [ ] 内容详情读取失败或仍在加载时，不能通过空文本保存覆盖原内容。
- [ ] 未保存的新 preview 不会被详情读取返回。
- [ ] 手动 IP 列表复用相同读取逻辑并能完成语义 round-trip（若共享实现无需额外分支）。
- [ ] 目标列表 summary/routing context 不携带完整内容，详情接口按需返回可编辑内容。
- [ ] 前端 lint/build/audit、后端测试/vet、Trellis 校验和 Git diff 检查通过。
- [ ] 通过 root reviewer 明确报告 `APPROVED` 后，部署到 `10.0.0.60`；部署只作为用户人工验收前的测试机交付，不视为生产验收或合并授权。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
