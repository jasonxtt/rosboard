# 合并刷新控件并统一轮询行为

## Goal

把顶部栏的“立即刷新”和“自动刷新设置”收敛为一个刷新组合控件，减少重复视觉元素，同时保证立即刷新仍是一步操作、自动刷新状态清晰可见，并让所有页面数据轮询遵从同一个刷新周期。

## Requirements

- 顶部栏渲染一个分栏刷新控件：左侧按钮立即刷新，右侧按钮打开自动刷新选项菜单。
- 立即刷新按钮保持独立的键盘/屏幕阅读器语义；自动刷新按钮显示当前周期，并保留停止、1 秒、3 秒、5 秒、10 秒选项。
- 桌面端组合控件紧凑显示；移动端两个操作区域均不小于 44×44px，且不产生文档级横向溢出。
- 点击立即刷新时，即使自动刷新设置为“停止刷新”，也必须重新读取当前页面的数据。
- 选择的自动刷新周期必须同时控制仪表台、实时概览、流量/负载历史与终端详情的浏览器轮询；选择“停止刷新”后只保留首次加载与手动刷新。
- 不改变后端 API、自动刷新选项集合或现有设置页中的默认刷新配置。

## Acceptance Criteria

- [ ] 桌面与移动端顶部栏只显示一个刷新组合控件，立即刷新和自动刷新设置均可直接操作。
- [ ] 立即刷新在“停止刷新”状态下仍触发 `/api/fleet-overview`、`/api/dashboard`、当前实时/历史数据以及当前终端详情的重新读取。
- [ ] 自动刷新周期改变后，相关轮询使用新周期；“停止刷新”不会继续运行负载历史或终端详情轮询。
- [ ] `npm --prefix web run lint`、`npm --prefix web run build`、`go test ./...`、`go vet ./...`、`git diff --check` 通过。
- [ ] 在 1440、1024、768、390、375px 下完成布局与交互回归，无文档级横向溢出；移动端刷新组合控件的两个可点击区域均不小于 44px。
- [ ] 完成部署门禁：替换前备份远程二进制、配置、SQLite 数据与服务单元，验证远程 systemd、健康接口、相关 API 与嵌入前端资源，并等待用户人工验收后再提交代码。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
