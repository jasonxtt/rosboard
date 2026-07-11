# 按参考图重塑系统概览设计

## Visual Fidelity Contract

参考图的结构是实现合同：顶部四卡、主区域 2:1 流量/状态、底部 2:1 接口/事件，桌面首屏尽量在 900px 高度内展示主要内容。颜色、边框、字号、图标容器、表格密度和状态点均以参考图为准。

## Real-data Mapping

| Reference module | Rosboard data |
|---|---|
| CPU card | `overview.cpuLoadPercent` + `/api/load?window=1h` CPU samples |
| Memory card | current used/total + load-history memory samples |
| Online terminals | `connectedDeviceCount`, online/offline terminal breakdown + load-history count |
| Active connections | `connectionCount` + TCP/UDP/other counts aggregated from `protocols` |
| Real-time traffic | `overview.chartSamples`, upload/download rates |
| System status | dashboard freshness, RouterOS version, CPU/memory/storage thresholds, interface health |
| Interface status | first seven meaningful interfaces from `dashboard.interfaces`; full list remains on line-monitor page |
| Alerts/events | warnings plus current interface down/disabled/error/drop facts; healthy rows when no issues |

No historical timestamp is shown for facts that are only current-state observations.

## Component Changes

- Add a small internal SVG `Icon` component with a consistent 20–22px outline language.
- Replace generic `StatusTile` with `MetricCard` supporting icon tone, sparkline/progress, main value, and footer facts.
- Add `Sparkline`, `SystemStatusList`, `InterfaceSummaryTable`, and `CurrentEvents` components.
- Extend `OverviewPage` props with load samples.
- Load one-hour history while overview or load page is active; reuse the same state and endpoint.
- Move global refresh controls into the topbar; retain terminal controls where they serve list-specific needs.
- Add sidebar footer facts without changing navigation state behavior.

## Responsive Rules

- Desktop ≥1200px follows reference composition.
- 768–1199px uses two-column cards and stacks major content regions.
- Below 768px uses one-column cards and panels; interface summary shows key columns and links to the full line page.
- Do not reduce touch controls below 44px on mobile.

## Compatibility

- No backend/API/schema change.
- Vite output remains embedded in `internal/ui/dist`.
- Existing other pages retain behavior and receive only shared visual adjustments.

## Rollback

Changes remain frontend-only and can be reverted as one source/build-asset commit. The current API and stored data are untouched.
