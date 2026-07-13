# 连接表排序筛选设计

## Header interaction

每个表头拆成两个相邻按钮：列名按钮只负责排序，筛选箭头按钮只负责打开该列筛选。不可筛选列仍使用同样的列名排序按钮，从而保持视觉节奏。

排序状态为 `ConnectionSortKey | null` 与 `asc | desc`。过滤完成后复制数组再排序，避免修改 API 数据。文本使用带 numeric 的 localeCompare，数值列直接比较。

## Anchored panel

点击筛选箭头时读取触发按钮与 `.connection-table-shell` 的 bounding rect，计算浮层 left，并按面板宽度限制在 shell 内。全局搜索按钮使用相同锚定函数，因此只有搜索面板继续靠近右侧搜索按钮。

## Clear action

新增统一 reset 函数清空 family（恢复 scope 默认）、应用/协议/线路/地址/标志/状态/全局搜索、活动浮层和排序。清理按钮使用项目 SVG Icon，不显示文字；没有活动筛选、搜索或排序时 disabled。

## Accessibility

- 列名按钮 aria-label 包含当前排序方向和下一动作。
- 筛选按钮 aria-label 维持“筛选{列名}”，活动筛选同时有 class 与 aria-pressed。
- 清理按钮 aria-label 为“清除全部筛选和排序”。
