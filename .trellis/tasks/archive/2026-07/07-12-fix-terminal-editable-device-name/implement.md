# Implementation Plan

## Ordered Checklist

1. Store 和模型
   - 新增 `custom_name` 兼容迁移与 Terminal 字段。
   - 实现元数据原子更新、未知 ID 和持久化测试。
   - 明确 auto/custom/effective name 投影并补优先级测试。

2. Service 和 API
   - 新增 metadata 端点及输入校验。
   - 保存后局部更新 snapshot/detail/family summaries，不调用 RouterOS refresh。
   - 旧 remark 端点复用局部更新路径。
   - 增加成功、未找到、无效输入及“不触发刷新”的回归测试。

3. 编辑弹窗
   - 将 editing state 与详情选择状态分离。
   - 同一弹窗编辑设备名称和备注，展示自动识别提示和清空恢复语义。
   - 失败保留草稿，成功关闭并更新列表/详情。

4. 终端表
   - 第一列设备名称，第二列 IP/MAC。
   - 删除接口列和旧设备列，调整排序、搜索和响应式列规则。
   - 操作改为“编辑终端”。

5. 验证与交付
   - lint、TypeScript build、Go tests、Go build。
   - 真实 API 保存/恢复测试，确认无 500。
   - 浏览器持续输入跨多个轮询周期、保存关闭、刷新持久化、桌面和移动端布局验证。
   - 重启监听 `0.0.0.0:8080` 的后台服务并验证 LAN HTTP 200。

## Validation Commands

```bash
go test ./...
npm --prefix web run lint
npm --prefix web run build
go build -o ./rosboard ./cmd/rosboard
```

## Risk and Rollback Points

- `internal/store/sqlite.go`：迁移必须允许已有数据库重复启动，不删除现有名称/备注。
- `internal/service/monitor.go`：局部缓存更新必须覆盖列表、详情和 family summaries，避免保存后不同页面显示不一致。
- `web/src/App.tsx`：不能再用 selectedTerminalID 驱动编辑框，否则详情轮询会重新引入草稿覆盖。
- `web/src/index.css`：列顺序改变后必须重新核对移动端 nth-child 隐藏规则。

## Review Gate

- [x] 用户批准 PRD、design、implement 后才运行 `task.py start`。
- [x] 开发前运行 `trellis-before-dev`。
