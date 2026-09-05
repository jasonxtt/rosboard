# 执行计划

按序执行，每步完成跑验证并截图自检。里程碑处 commit。

## 0. 准备

- [ ] `git checkout -b feat/ui-ikuai-restyle`（基于 `feat/policy-access-rebuild`）
- [ ] `web/vite.config.ts` 代理改指本地实例（127.0.0.1:8080），防误触生产；提交说明
- [ ] Draft PR：base = feat/policy-access-rebuild，标题注明 stacked

## 1. Token 换血（index.css）

- [ ] `:root` 色板替换（见 design.md 映射表），token 名不变
- [ ] 新增淡彩卡/蓝色 tint 变量
- [ ] 验证：`npm run build` + 任意页面截图（此时观感是"旧结构新配色"，允许中间态）

## 2. 外壳重构（App.tsx PanelApp）

- [ ] 双列导航 JSX + CSS；一级 140px / 二级 140px；二级列贴上沿
- [ ] logo+版本 tag、搜索框、设备切换器入一级列；顶栏精简
- [ ] 移动端抽屉退化
- [ ] 验证：每个菜单项点击可达对应 view（对照 inventory 外壳节）

## 3. 组件级换皮（index.css 按块重写）

- [ ] 按钮三级 / 输入选择 / tab / 分段控件 / 表格 / 徽章 / dialog / 分页 / 通知条 / 步骤条 / tooltip
- [ ] 验证：策略路由页 + 终端页截图对照设计稿

## 4. 页面级调整

- [ ] 登录页 + AdminSetup + RouterOSSetup：渐变背景白卡
- [ ] 系统概览：按设计稿终版重排；先核对 WAN 信息字段（运营商/延迟/本月数据）在 dashboard API 的存在性，缺失项报用户定夺
- [ ] 终端监控/接口监控/流量/网络服务/系统运行：样式对齐
- [ ] features/（policy 三页 + access-control + recognition）：换皮
- [ ] 面板设置：五组卡片式表单

## 5. 图表配色（lib/themeTokens.ts + RealtimeTrafficChart.tsx）

- [ ] 蓝/绿/紫/橙新色板；面积图同色 15% 渐变
- [ ] MutationObserver 主题联动保持可用

## 6. Dark mode

- [ ] `:root[data-theme="dark"]` 整组换板；逐页截图检查对比度

## 7. 总体验证（check 门禁）

- [ ] `cd web && npm run build`、`npx oxlint`、`go build ./...` 全绿
- [ ] playwright 全页面截图（登录/概览/接口/终端+详情/流量/网络服务/系统运行/目标库/策略路由+向导/访问控制/识别/设置五组/仪表台），对照 `research/rosboard-feature-inventory.md` 逐项打勾
- [ ] 与设计稿截图并排比对，用户复核观感

## 8. 部署

- [ ] 测试机 10.0.0.60 部署验证（无需备份）
- [ ] 用户确认后走生产验收门：NAS 备份（/Users/tom/nas/wyp/github/rosboard/backups/<ts>-ui-ikuai/）→ 部署 10.0.0.6 → 健康检查 → 用户手动验收

## 回滚点

- 每个里程碑 commit 可单独 revert；整体回滚 = 切回 feat/policy-access-rebuild；生产回滚用 NAS 备份恢复
