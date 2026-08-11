# 设计：合并刷新控件并统一轮询行为

## 1. 控件结构

顶部栏采用 split control：一个视觉组合容器内包含两个原生按钮。

- 左侧主操作：刷新图标，点击递增 `refreshNonce`，不打开菜单。
- 右侧设置操作：复用现有 `ChoiceMenu`，显示当前周期的短标签/桌面标签，点击打开 `menuitemradio` 菜单。
- 两个按钮不互相嵌套；各自保留 `aria-label`、键盘焦点和可见焦点环。
- 桌面端保持紧凑横向组合，移动端两个按钮分别达到 `--control-h-touch`，由组合容器承担整体视觉边界。

自动刷新触发标签改为“刷新设置，当前自动刷新为……”，避免组合后重复显示“自动刷新”。菜单选项仍使用完整的“1 秒刷新”等文本。

## 2. 轮询行为

`refreshNonce` 是一次性手动刷新信号，所有当前页面数据 effect 都应在它变化时执行首次加载。自动刷新周期只决定 interval 是否创建及其周期：

- fleet overview、dashboard、realtime overview/resource、traffic history 使用 `dashboardRefreshMs`。
- load history 使用 `dashboardRefreshMs`，停止时不创建 timer。
- terminal detail 使用 `dashboardRefreshMs`，同时加入 `refreshNonce` 依赖；不再写死 1 秒。
- viewer heartbeat、设备发现和服务端采集心跳属于辅助保活/发现，不由页面刷新组合控件控制。

这样手动刷新与自动刷新是同一条数据读取链的两种触发方式；设置为停止只停止周期触发，不阻断首次加载和手动操作。

## 3. 兼容与风险

- 保持 `panelRefreshOptions` 的 value 集合与设置页 `<select>` 不变。
- 不改 API 或服务端采集频率；只改变浏览器读取缓存的时间安排。
- 主要风险是移动端组合控件宽度和 ChoiceMenu 弹出层定位，因此验证 375/390px 的几何、scrollWidth 与菜单打开状态。
