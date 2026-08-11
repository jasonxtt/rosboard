# 执行计划：合并刷新控件并统一轮询行为

## 实现

- [x] 调整顶部栏 JSX，使用一个刷新组合容器包住立即刷新按钮与自动刷新 `ChoiceMenu`。
- [x] 调整刷新控件 CSS，合并视觉边界并保持桌面紧凑、移动端 44px 触控区域。
- [x] 修正实时概览、负载历史和终端详情 effect 的停止/周期/手动刷新逻辑。
- [x] 更新 README 与前端组件规范中对刷新行为和控件结构的说明。

## 验证

- [x] 运行前端 lint/build、Go test/vet、git diff --check。
- [x] 使用静态/浏览器 harness 验证 1440/1024/768/390/375px，无横向溢出，组合控件可点击区域达标。
- [x] 本地启动服务，验证 `/api/health`、bootstrap、相关 API 与嵌入 CSS/JS 资源。
- [x] 部署到 `10.0.0.6` 前备份二进制、配置、SQLite 数据和 systemd 服务单元。
- [x] 验证远程服务、健康接口、相关 API 与嵌入资源。
- [x] 用户人工验收通过。
- [ ] 提交代码并归档 Trellis 任务、更新 GitHub draft PR。

## 当前暂停点

2026-08-12 已部署到 `10.0.0.6`，备份目录为
`/opt/rosboard/backups/rosboard-refresh-control-20260812-004552/`。
远程 systemd、`/api/health`、bootstrap、未认证 API 状态和嵌入 CSS/JS
资源均已验证；用户已人工检查组合刷新按钮并确认通过，进入提交与发布收尾。
