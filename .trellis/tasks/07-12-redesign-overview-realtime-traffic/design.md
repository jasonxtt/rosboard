# Technical Design

## Source Mapping

参考实现：

- `/Users/tom/github/mosdns/webui-log/src/components/dashboard/RealtimeTrendChart.vue`
- `/Users/tom/github/mosdns/webui-log/src/components/dashboard/DnsOverviewCard.vue`

Rosboard 不复制 Vue 组件结构，而是在 React 中移植其图表配置和生命周期：`useRef` 持有容器与 ECharts 实例，`useEffect` 初始化/销毁，另一个 effect 用 `setOption` 更新数据，ResizeObserver 负责容器变化。

## Dependency Boundary

在 `web/package.json` 增加已修复已知 XSS 问题的 `echarts ^6.1.0`（mosdns 的 5.6 配置和 API 迁移到该版本）。使用 `echarts/core` 并只注册：

- `LineChart`
- `GridComponent`
- `TooltipComponent`
- `CanvasRenderer`

Rosboard 的可见速率说明由 React DOM 渲染，不需要 ECharts legend，因此可省略 `LegendComponent`。这样比 `import * as echarts from 'echarts'` 更接近按需加载目标。

图表组件通过 React `lazy()` / `Suspense` 独立打包，ECharts 不进入初始应用 chunk。

## Data and Units

新增 `formatBitRate(value)`：输入保持 RouterOS bits/s，不除以 8，按 1000 阶自动格式化 `bps/Kbps/Mbps/Gbps`。该函数用于：

- 标题下的当前下载/上传值
- ECharts Y 轴刻度
- Tooltip 两条系列值

时间序列映射：

```text
xAxis.data = chartSamples[].timestamp -> HH:mm:ss
下载 series = chartSamples[].downloadBps
上传 series = chartSamples[].uploadBps
```

系列顺序固定为下载（蓝）在前、上传（绿）在后，与标题信息顺序一致。

## ECharts Configuration

从 mosdns 移植的核心配置：

- Canvas renderer
- `animationDuration: 520`, update `700`, `cubicOut`
- `smooth: 0.26`, `showSymbol: false`, line width 2
- 蓝色 `#2563eb`、绿色 `#16a34a`
- 顶部约 24% 到底部约 2% 的同色透明渐变面积
- category xAxis、`boundaryGap: false`
- value yAxis、隐藏轴线/刻度、浅色横向 splitLine
- axis tooltip、深色高对比背景、虚线 axisPointer
- grid 为标签留出左/右/底部空间，但图形容器本身占满面板宽度

当 samples 为空时不初始化 Canvas，显示现有空状态。samples 从空变为非空时初始化；组件卸载或回到空状态时 dispose。

## Layout

`traffic-panel` 头部保持 flex 两端结构。左侧 `.traffic-heading-block` 内为标题和 `.traffic-live-values`，后者使用两行 grid：

```text
● 下载（100 Mbps）
● 上传（50 Mbps）
```

ECharts 容器桌面高度 280px、窄屏 220px；移除当前 `.traffic-panel .chart-svg { height: 195px }` 的特例。面板仍与右侧系统状态保持同一 grid 行，允许左侧面板因图表变高而决定该行高度，右侧面板自然拉伸，不人为填充虚假内容。

## Accessibility and Performance

- 当前值文字提供非颜色区分；图表容器带描述性 `aria-label`。
- Tooltip 只是增强，关键当前值始终可见。
- respects `prefers-reduced-motion`：禁用 ECharts 动画。
- 每次 dashboard 刷新只调用增量 `setOption`，不重复创建实例。
- ResizeObserver 和 window resize 监听在卸载时清理。

## Rollback

本次只改变前端依赖、组件和生成资源；后端 API 不变。回滚时恢复 SVG `TrafficChart`、移除 ECharts 依赖并重新构建嵌入资源即可。
