# 完善连接表排序与筛选交互

## Goal

让连接表的排序与筛选各自清晰可控，筛选浮层就近出现，并能一键恢复默认表格状态。

## Requirements

- R1：连接详情所有列的列名文字可点击排序；首次点击升序，再次点击降序，并显示明确方向箭头。
- R2：可筛选列的筛选箭头是独立按钮，点击不触发排序；图形和触控区域比当前更大。
- R3：筛选浮层根据被点击箭头的位置计算并锚定在该列表头附近，同时限制在连接表可视区域内。
- R4：全局搜索按钮左侧增加无文字 SVG 清理按钮；具有 `aria-label`、focus 状态和无筛选时的 disabled 状态。
- R5：清理按钮清除全部列筛选、全局搜索及连接表排序，恢复当前详情入口范围内的原始顺序。
- R6：保持全部/IPv4/IPv6 scope 硬边界、手机内部横向表格滚动和页面无横向溢出。

## Acceptance Criteria

- [x] AC1：点击应用、地址、速率等任一列名可在升序/降序之间切换，筛选面板不打开。
- [x] AC2：点击独立筛选箭头只打开筛选，浮层与点击列相邻而不是固定在搜索按钮旁。
- [x] AC3：筛选箭头手机可操作区域至少 44px，桌面图标明显大于旧版。
- [x] AC4：清理按钮位于搜索按钮左侧；应用筛选、搜索和排序后点击可全部恢复，并进入 disabled。
- [x] AC5：375px 和桌面浏览器无页面横向溢出、无控制台错误；lint/build/audit、Go 测试和局域网 HTTP 200 通过。

## Out of Scope

- 不持久化连接排序/筛选状态。
- 不改变后端连接数据或 RouterOS 采集。

## Requirements

- TBD

## Acceptance Criteria

- [ ] TBD

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
