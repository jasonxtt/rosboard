# 重做概览实时流量图表

## Goal

让系统概览的实时流量模块在宽屏下充分利用可用空间，清晰展示当前上传/下载速率，并复用 mosdns 概览图表中已经验证过的平滑曲线、渐变面积、Tooltip 和响应式体验。

## Confirmed Evidence

- 用户要求标题下方分两行、左对齐显示实时值，例如“下载（100 Mbps）”“上传（50 Mbps）”，而不是当前同一行的静态 `下载 (bps) / 上传 (bps)`。
- 用户提供的截图显示当前图表只占面板中央较窄区域，左右有大量无效留白，图表与实时流量模块尺寸不匹配。
- Rosboard 当前 `TrafficChart` 是固定 `820×280` SVG，概览 CSS 又把图表高度压到 `195px`（`web/src/App.tsx:1111`、`web/src/index.css:170-171`）；宽屏时 SVG 保持 viewBox 比例，造成横向空间没有被图形充分使用。
- mosdns 当前主 UI 源码位于 `/Users/tom/github/mosdns/webui-log`。`RealtimeTrendChart.vue` 使用 ECharts 5.6 按需导入 `LineChart`、`GridComponent`、`LegendComponent`、`TooltipComponent` 和 `CanvasRenderer`。
- mosdns 图表实现使用 `smooth: 0.26`、`showSymbol: false`、2px 蓝/绿线、纵向透明渐变面积、axis tooltip、虚线 axisPointer、ResizeObserver 和增量 `setOption()`。
- Rosboard 的 `uploadBps/downloadBps` 来源是 RouterOS bits/s；现有 `formatBits()` 实际转换为 B/s。用户明确要求在该模块显示 Mbps，因此当前值、Y 轴和 Tooltip 应采用 bit/s 单位格式，避免标签与数值语义冲突。

## Requirements

### R1. 当前速率信息

- 实时流量标题下方按上下两行显示“下载（实时值）”和“上传（实时值）”。
- 下载保持蓝色、上传保持绿色；每行用色点和文字共同区分，不能只依赖颜色。
- 数值按 bit/s 自动选择 `bps / Kbps / Mbps / Gbps`，与 RouterOS 数据语义一致。
- 当前值优先使用 `overview.downloadBps/uploadBps`，没有有效值时显示 `0 bps`。

### R2. mosdns 图表体验移植

- 使用 ECharts Canvas 和 mosdns 相同的按需模块加载方式，不引入完整全量包入口。
- 复用平滑曲线、无常驻点、蓝绿渐变面积、横向网格、轴向 Tooltip、虚线指示线和 ResizeObserver 自适应原则。
- 图表数据仍来自 Rosboard 现有 5 分钟 `overview.chartSamples`，不改变后端采样接口。
- Tooltip 显示采样时间、下载和上传的精确 bit/s 格式值。

### R3. 面板布局

- 图表 Canvas 填满实时流量面板的有效宽度，不再因固定 SVG 比例出现左右大块空白。
- 桌面图表目标高度约 280px；移动端约 220px，并保持无页面级横向滚动。
- 标题/速率信息在左侧纵向排列，“5 分钟”范围标记保持右上角。
- 空数据时保留明确空状态；图表初始化和更新不得造成布局跳动。

## Acceptance Criteria

- [x] 标题下方显示两行实时下载/上传值，单位为 bps/Kbps/Mbps/Gbps，数据与 dashboard API 当前值一致。
- [x] ECharts 曲线填满面板宽度，桌面宽屏不再有原 SVG 的大块左右留白。
- [x] 曲线、渐变、Tooltip、网格和自适应行为与 mosdns 实现原则一致。
- [x] 连续刷新时图表增量更新且无控制台错误；无数据时显示空状态。
- [x] 375、768、1440px 下布局可用，无页面级横向溢出。
- [x] 前端 lint/build、Go 测试、嵌入式二进制构建、真实 API 和局域网浏览器验证通过。

## Out of Scope

- 不修改 RouterOS 采样频率、5 分钟窗口或后端流量方向定义。
- 不移植 mosdns 的查询数/延迟业务字段、时间段统计弹窗或系列开关。
- 不重做系统状态、接口状态或其他概览模块。
