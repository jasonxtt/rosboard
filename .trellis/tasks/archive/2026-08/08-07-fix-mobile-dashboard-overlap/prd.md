# 修复移动端仪表盘布局重叠

## Goal

修复仪表盘在移动端和窄布局下的顶部排版及指标卡折线图重叠问题，并统一终端监控、接口监控、流量监控、网络服务、系统运行和面板设置的移动端顶部层级与全局操作控件。

## Requirements

- 调整顶部栏的响应式布局，使仪表盘标题不会被搜索框、主题/刷新控件挤压或覆盖；移动端控制项应换到独立行并保持可操作。
- 调整指标卡移动端的主内容网格，使数值区域和迷你折线图使用独立网格列，折线图不得覆盖图标、数值或辅助文字。
- 移动端仪表台的搜索、主题、刷新和自动刷新控件保持在同一行，并让搜索框在窄屏自适应收缩。
- 移动端系统概览的时间范围、主题、刷新和自动刷新控件保持在同一行，时间按钮与选择器在窄屏仍可读且不造成页面水平溢出。
- 移动端终端监控固定为三行：菜单与 `全部 / IPv4 / IPv6`页签、六项概览、搜索与全局操作。六项概览必须在同一行，不换行且不水平滚动。
- 移动端接口监控固定为三行：菜单与 `物理接口 / 逻辑接口 / 系统接口`页签、六项接口概览、系统状态与全局操作。六项概览必须在同一行。
- 移动端流量监控、网络服务和系统运行使用统一的页签行与全局操作行；面板设置使用统一的标题行与全局操作行。无概览数据的页面不增加空白第三行。
- 全局操作行的告警/正常状态、主题、立即刷新和自动刷新保持同一行。主题、立即刷新和刷新周期的高度、边框、圆角、图标视觉尺寸和按压状态必须一致；图标型控件使用 `44×44px` 触控区。
- 移动端主题按钮仅显示调色板图标，刷新周期使用 `1s / 3s / 5s / 10s / 停` 紧凑文案；保持现有菜单与选项功能。
- 正常的“最后更新”不在移动端单独占位；刷新失败或数据过期仍必须有明确、可读的状态提示。
- 兼容旧版 iOS Safari/嵌入式 WebView：设备宽度判定异常时仍进入移动布局，避免侧栏与主内容并排挤压；避免浏览器继续使用旧版入口 HTML。
- 保持桌面端现有布局、主题样式和控件功能不变，不引入新的交互或数据逻辑。
- 遵循现有前端规范，使用最小范围的 CSS/组件修改。

## Acceptance Criteria

- [ ] 在移动端宽度（至少 375px 和用户反馈的手机宽度）顶部标题完整可读，控制项不覆盖标题且页面无水平溢出。
- [ ] 在移动端指标卡中，CPU、内存、在线终端和活动连接的数值、图标、迷你折线图互不重叠；进度条/构成条与底部统计仍保持原有层次。
- [ ] 在 375px 和用户反馈的手机宽度下，终端监控为三行；六项概览单行可读，搜索和操作单行可用，且无文档级水平溢出。
- [ ] 在 375px 和用户反馈的手机宽度下，接口监控为三行；六项概览单行可读，状态和操作单行对齐。
- [ ] 流量监控、网络服务、系统运行和面板设置的移动端全局操作保持单行，同类控件高度和视觉重量一致。
- [ ] 主题、立即刷新和刷新周期的触控区至少 44px，间距至少 8px，焦点、按压、菜单和选择器交互保持可用。
- [ ] 移动端系统概览的时间范围、主题、刷新和自动刷新在同一行，按钮/选择器内容可读。
- [ ] 在桌面端和中等宽度下，仪表盘顶部搜索框/控制项与标题不发生覆盖，原有控件仍可用。
- [ ] 旧版 iOS/嵌入式 WebView 加载时，仪表台不再出现侧栏挤压主内容、标题逐字竖排或摘要卡变窄的问题。
- [x] `npm --prefix web run lint`、`npm --prefix web run build`、`go test ./...`、`go vet ./...` 和 `git diff --check` 在新方案实现后通过。
- [x] 本地运行检查通过，健康接口、引导接口和嵌入式 HTML/CSS/JS 资源可访问，CSS 包含本次移动端布局规则。
- [x] 新方案已按部署门禁重新部署到 `10.0.0.6`；替换前保留二进制、配置、SQLite 数据和服务单元的时间戳备份，并完成远程服务、健康接口、受影响 API 和嵌入式资源验证。
- [ ] 提供部署实例的人工视觉验收步骤，等待用户确认后再提交代码。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.

## Implementation Notes

- Mobile overview/fleet topbars use a single-column grid so the heading gets its own row and the control cluster cannot squeeze or cover it.
- Mobile metric cards keep the existing three-column layout, but the value row spans the icon and value columns so the sparkline occupies the third column instead of overlapping the value.
- Mobile fleet controls now use one no-wrap row with a flexible search input; overview controls use compact time buttons and a 44px refresh target in the same row.
- The old-iOS failure was traced to Vite 8/Lightning CSS rewriting `max-width` media queries into range syntax such as `width<=767px`, which older Safari does not parse. `cssTarget: 'safari12'` keeps legacy media-query syntax, while `max-device-width` provides a device-width fallback.
- The root document uses iOS text-size and overflow safeguards, and the server marks `index.html`/SPA fallback responses `Cache-Control: no-cache` so old browsers revalidate the asset manifest.
- Mobile overview keeps a normal text heading; the time-range segmented control is 44px high to match the theme and refresh controls.
- Mobile terminal and interface topbars keep all six summary values in one six-column row. Terminal search and global actions share the following row without horizontal scrolling.
- Mobile traffic, network-service, system-runtime, and settings topbars reuse the same 44px theme, refresh, refresh-period, and status presentation without adding an empty summary row.
- Remote backups: `/opt/rosboard/backups/20260807-173535-mobile-dashboard-overlap/`, `/opt/rosboard/backups/20260808-111119-mobile-topbar-controls/`, `/opt/rosboard/backups/20260808-1133-ios-compat/`, `/opt/rosboard/backups/20260808-1138-overview-title-button/`, `/opt/rosboard/backups/20260808-1142-overview-control-height/`, `/opt/rosboard/backups/20260808-1158-release-v0.1.1-preview/`, `/opt/rosboard/backups/20260808-1235-terminal-search-inline/`, `/opt/rosboard/backups/20260808-1252-terminal-controls-inline/`, and `/opt/rosboard/backups/20260808-153756-mobile-header/`.
- The approved mobile-header candidate is deployed at `http://10.0.0.6:8080/`; manual visual acceptance is pending before commit or publication.
