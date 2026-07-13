# 终端移动端与连接筛选设计

## Terminal list

默认排序键从 `device` 改为 `address`，继续复用现有 `compareTerminal` 数值 IP 比较。移动端 `.terminal-toolbar` 改为两列 CSS Grid，所有控件通过语义类名定位，不使用脆弱的 nth-child。

## Detail header

桌面沿用左右布局；手机仍让身份信息自然换行，但标题区预留右侧空间，返回按钮绝对定位到卡片右上角并保持 44px 触控区域。

## Connection table

删除 `.connection-toolbar`。连接范围首先由详情入口 `scope` 做硬限制，再应用 IP 版本列筛选，因此列筛选不能越过 IPv4/IPv6 入口边界。

可筛选列使用原生 `details/summary`：summary 是列名和筛选指示器，浮层内使用 select 或带显式 aria-label 的文本输入。生效的列筛选器显示蓝色状态。此方案具备键盘语义且无需额外弹窗状态管理。

全局搜索按钮绝对定位在 `.connection-table-shell` 右上方，与表头同高；悬浮搜索面板覆盖在表格上方，不改变布局高度。按钮使用现有 SVG Icon 系统新增 search 图标。

## Responsive behavior

- Desktop：表头筛选浮层锚定列名；全局搜索锚定表格右上角。
- Mobile：连接表继续在 `.table-scroll` 内横向滚动，页面本身不横向溢出；筛选浮层采用固定可视宽度并限制在视口内。
- 所有表头筛选 summary 和搜索按钮具有明确 aria-label/focus 状态。
