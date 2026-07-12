# Implementation Plan

## Ordered Checklist

1. 添加 ECharts 依赖并实现 React 图表组件
   - 按需注册 line/grid/tooltip/canvas。
   - 移植 mosdns 的平滑线、渐变、Tooltip、坐标轴和 ResizeObserver。
   - 处理空数据、增量更新、dispose 和 reduced-motion。

2. 统一实时流量单位
   - 新增 bit/s 格式化函数及测试/可验证样例。
   - 当前值、Y 轴和 Tooltip 使用同一格式化逻辑。

3. 重排实时流量面板
   - 标题下方改为上下两行实时下载/上传。
   - 图表桌面约 280px、移动约 220px并填满宽度。
   - 删除旧 SVG 图表和只针对它的 CSS。

4. 构建与浏览器验证
   - lint、TypeScript build、Go tests、Go build。
   - 重启 `0.0.0.0:8080` 服务。
   - 对照 dashboard API 验证当前值，检查 Tooltip、连续更新和控制台。
   - 验证 375/768/1440px 截图与页面横向溢出。

## Validation Commands

```bash
npm --prefix web run lint
npm --prefix web run build
go test ./...
go build -o ./rosboard ./cmd/rosboard
```

## Risk and Rollback Points

- `web/package.json` / lockfile：确保只新增必要 ECharts 依赖。
- `web/src/App.tsx`：实例不能在每 5 秒刷新时重建。
- `web/src/index.css`：移除 SVG 高度覆盖后检查概览 grid 和移动断点。
- `internal/ui/dist`：必须通过 Vite build 生成，不手工修改。

## Review Gate

- [x] 用户批准 PRD、design、implement 后才开始编码。
- [x] 编码前运行 `trellis-before-dev`。
