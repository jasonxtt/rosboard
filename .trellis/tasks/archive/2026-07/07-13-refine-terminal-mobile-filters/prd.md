# 优化终端监控移动端与连接表筛选

## Goal

让终端列表和详情在手机上更紧凑、可读，并把连接筛选完全收进表头，消除占空间的独立工具栏。

## Requirements

- R1：终端列表默认按 `IP / MAC` 中的 IP 数值升序排序；IPv4 必须按四段数值比较，`10.0.0.10` 位于 `10.0.0.8` 之后。
- R2：手机端终端工具栏使用固定四行两列网格：搜索占满首行，状态/接口、非在线切换/数量、刷新周期/立即刷新分别对齐，消除不规则空白。
- R3：终端详情“返回”改为标题卡片右上角紧凑按钮，手机端不单独占行。
- R4：连接详情删除现有 family tabs、协议 select、搜索框组成的整块工具栏，不保留新的表格上方一行。
- R5：连接表第一列为 IP 版本；“全部终端”范围可显示/筛选 IPv4 与 IPv6，“IPv4”范围只能显示 IPv4，“IPv6”范围只能显示 IPv6。
- R6：IP 版本、应用、协议、出口线路、本地地址、目的地址、标志和连接状态通过对应列名的 Excel 式弹出筛选操作。
- R7：全局模糊搜索作为表头同高的右上角放大镜按钮，点击显示悬浮搜索框，不新增持久工具栏或表格数据列。
- R8：全局搜索覆盖应用、协议、线路、本地/目标/外网地址、端口和标记；列筛选和全局搜索组合生效。

## Acceptance Criteria

- [x] AC1：首次打开终端监控时，表头显示 `IP / MAC ↑`，前几行 IPv4 数值严格升序。
- [x] AC2：375px 手机端终端筛选区无错位、无页面横向溢出，触控控件最小高度 44px。
- [x] AC3：手机详情标题与返回按钮共用一张标题卡片且返回位于右上角。
- [x] AC4：连接详情中旧工具栏完全消失，表格第一列显示 IPv4/IPv6 标签，列筛选和悬浮全局搜索均可操作。
- [x] AC5：从全部/IPv4/IPv6 三个入口进入详情时，连接行严格遵守对应 IP 范围。
- [x] AC6：桌面和手机浏览器验证、前端 lint/build/audit、Go 测试及局域网 HTTP 200 通过。

## Out of Scope

- 不修改 RouterOS 数据采集、终端识别或连接归属逻辑。
- 不持久化筛选状态到后端或 URL。

## Requirements

- TBD

## Acceptance Criteria

- [ ] TBD

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
