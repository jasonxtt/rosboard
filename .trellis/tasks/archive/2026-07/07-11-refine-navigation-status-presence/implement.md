# Implementation Plan

## Ordered Checklist

1. 在线状态模型与回归测试
   - 为 strong/weak evidence 和 5 分钟状态转换补充表驱动测试。
   - 扩展终端 state 为 online/inactive/offline，确保弱证据不推进 lastSeen。
   - 校验 onlineSince 和在线计数只受 online 边沿影响。

2. 结构化当前告警
   - 在 model 中增加 `AlertEvent` 和 dashboard `alerts`。
   - 在 monitor 中实现并发安全、有界、去重且可恢复清除的当前事件集合。
   - 将现有采集 warning/error 产生点接入 helper，保留 `Warnings` 兼容输出。
   - 增加队列容量、去重、恢复清除和排序测试。

3. 前端类型和概览内容
   - 扩展 TypeScript API 类型和 inactive 状态格式化/筛选/样式。
   - 当前告警改读结构化 alerts，删除 `buildCurrentEvents()` 的健康填充逻辑。
   - 系统状态替换为运行时间、版本、最后采集、活动接口、存储和新鲜度。

4. 分级折叠菜单
   - 增加一级/二级展开状态及祖先同步逻辑。
   - 实现同级手风琴、叶子切页、移动端关闭抽屉和 ARIA 状态。
   - 调整最少量 CSS，使二级/三级层级清晰且不恢复此前过高标题行。

5. 集成与真实环境验证
   - 构建前端并嵌入 Go UI 产物。
   - 启动服务后检查 dashboard JSON、10.0.0.5 状态、在线统计。
   - 用浏览器逐项验证菜单、概览、空告警/有告警、终端筛选和移动端。

## Validation Commands

```bash
go test ./...
cd web && npm run lint
cd web && npm run build
```

运行服务后补充：

```bash
curl -fsS http://127.0.0.1:8080/api/dashboard
curl -fsS http://127.0.0.1:8080/
```

最终通过浏览器对 LAN 地址进行桌面和窄屏截图比对，并确认 `10.0.0.5` 在无强证据超过 5 分钟后为 offline。

## Risk and Rollback Points

- `internal/service/monitor.go`：在线状态和采集告警集中，先以测试锁定行为再改实现。
- `internal/store/sqlite.go`：不得通过 schema 迁移扩大范围；只调整已有 presence 边沿语义。
- `web/src/App.tsx`：导航、概览、筛选集中在单文件，按功能小步修改，避免顺带重构。
- `internal/ui/dist`：仅由前端 build 更新，不手工编辑生成文件。

## Pre-start Checks

- [x] PRD、design、implement 三份文档经用户审核。
- [x] `prd.md` 无未决问题且每条验收标准可验证。
- [x] 开始编码前运行 `trellis-before-dev` 读取本项目规范。
- [x] 保留用户已有非本任务 worktree 修改，不覆盖、不清理。
