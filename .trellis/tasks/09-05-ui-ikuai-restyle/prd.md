# 仿 iKuai 4.0 风格前端 UI 重写

## Goal

将 rosboard 前端整体视觉风格从当前的灰白+薄荷绿极简风重写为 iKuai 4.0 的蓝白消费级风格，布局改为 iKuai 式左侧双列导航。视觉基准是已通过用户评审的静态设计稿 `preview-ikuai/`（5 页：登录/系统概览/终端监控/策略路由/面板设置）。

## Requirements

- 功能零丢失：所有 ActiveView、菜单项、按钮、表格列、排序/筛选、dialog、设置项必须保留（基线见 `research/rosboard-feature-inventory.md`）。发现疑似多余/错误的元素只报告，经用户同意才可删。
- 不得新增虚构功能：设计稿与实现中不允许出现后端/前端不存在的功能入口。
- 排版、顺序、按钮样式、背景、选框等视觉层有充分自由度，以 `preview-ikuai/` 设计稿为准。
- 保留 dark mode（现有功能），暗色 token 组同步换成新色板。
- 保留全部现有交互逻辑、状态管理、API 调用；本任务不重构 TSX 逻辑结构（除外壳导航布局必要的 JSX 调整）。
- 系统概览页采用设计稿终版结构：设备信息条 + 三张淡彩统计卡（终端数量/资源使用/活动连接）+ WAN信息 + 快捷入口 + 上下行速率 + 终端数量柱状图 + 内存/CPU 使用率。其中 WAN 信息的「运营商/本月数据使用情况/延迟」需核对后端数据，拿不到就与用户确认后去掉或替换为真实字段。
- 表格保持现有信息密度，不盲目加宽（吸取 iKuai 4.0 宽表格差评教训）。

## Acceptance Criteria

- [ ] `cd web && npm run build`（tsc -b + vite build）与 oxlint 通过；`go build ./...` 通过
- [ ] 每个页面用 playwright 截图，与 `research/rosboard-feature-inventory.md` 逐项核对功能零丢失
- [ ] 关键页面截图与 `preview-ikuai/` 设计稿观感一致（用户复核确认）
- [ ] dark mode 各页面可用、配色与新色板一致
- [ ] 部署到测试机 10.0.0.60 验证通过
- [ ] 生产 10.0.0.6 走 AGENTS.md 验收门：NAS 备份 → 部署 → 用户手动确认后才算完成

## Out of Scope

- 后端 API 改动（除非 WAN 信息字段确认需要新增，且需用户同意）
- App.tsx 单体拆分等代码结构重构
- 引入 UI 组件库（antd 等）——已决策走手写 CSS 路线
