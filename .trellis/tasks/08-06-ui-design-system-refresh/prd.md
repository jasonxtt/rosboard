# 落地 Mintlify 设计系统：前端视觉层重构

## Goal

把面板的视觉层从"没有体系的通用后台配色"重做为一套被真正贯彻的 token 体系，参照
[Mintlify DESIGN.md](https://github.com/VoltAgent/awesome-design-md/blob/main/design-md/mintlify/DESIGN.md)：
近黑 ink 主色 + 薄荷 `#00d4a4` 仅作强调、hairline 描边的扁平卡片、全圆角胶囊按钮、
Inter + Geist Mono 的字体分工、负字距大字号。

**不改变任何功能、数据流、API、类型或页面结构。**

## Problem

审计 `web/src/index.css`（1323 行）后确认，"土"来自三类系统性问题，而非个别页面的样式瑕疵：

1. **调色板没有体系。** token 声明了但没有被贯彻：标题黑有 6 种写法
   （`#0f172a` `#152033` `#172033` `#101828` `#27364b` `#1e293b`），正文灰有 10 种
   （`#334155` `#445168` `#455468` `#475569` `#52637a` `#64748b` `#718096` `#7890aa`
   `#8491a4` `#7b8798`），状态色 tint 约 40 个散落字面量。
2. **层级混乱。** 圆角有 9 种 px 字面量（`2/3/4/5/6/7/9/10`，`5px` 用了 13 次），
   而 `--radius-lg` 声明了却从未被使用；`--shadow-panel` 只被消费 3 次，另有 24 处
   硬编码 box-shadow，`.metric-card` / `.reference-panel` / `.fleet-summary` /
   `.fleet-overview-list` 各自手搓近乎相同的 `0 1px 3px` / `0 2px 8px` 变体。
3. **字体没有分工。** 字号 19 种，`11px` + `12px` 占全部规则的 54%，还有 3 处 `9px`
   已不可读；"大数字"这一个角色有 4 种互不相同的处理（`25px` / `23px` / `28px` /
   `clamp(22px,2vw,28px)`）。字体栈把 **PingFang 放在 Inter 之前**，所以拉丁字母和
   数字全部落到 PingFang 上，密集表格里的数字既不等宽也不精致；全站无 mono 分工。

## Decisions

| 决定 | 选择 |
| --- | --- |
| 字体 | 自托管 Inter + Geist Mono 的 latin/latin-ext 子集 woff2（可变字重），中文回落 PingFang SC |
| 侧栏 | 深藏青 → 浅色 + 薄荷高亮 |
| 主题 | 砍掉 glass，只保留 light + dark（指南未给暗色板，按其 4 个锚点推导） |
| 范围 | CSS 全量重做视觉层 + 少量 markup 微调，**不拆分 App.tsx** |

## Requirements

### 字体资产

- `web/src/assets/fonts/` 放 4 个可变字重 woff2（**不是** `public/`：`vite.config.ts`
  的 `base: './'` 会让 `public/` 资源在 `dist/assets/*.css` 里的相对 `url()` 解析错；
  放 `src/assets/` 由 Vite 指纹化并重写 URL）。产物随 `internal/ui/dist` 内嵌进二进制。
- `@font-face` 带 `font-display: swap`、`font-weight: 400 700`（Inter）/ `400 600`
  （Geist Mono）区间、以及与 Google 一致的 `unicode-range`，确保中文永不触发字体下载。
- 附 OFL 1.1 许可证文本（两者均为 OFL）。
- `--font-sans` 中 **Inter 必须排在 PingFang SC 之前**；`--font-mono` 同时替换
  `index.css:718` 和 `:1256` 两处逐字重复的 mono 栈。

### Token 体系（重写 `:root`）

- 表面：`--canvas: #fafafa`（页面）/ `--surface: #ffffff`（卡片）/
  `--surface-soft: #f7f7f7` / `--surface-code: #1c1c1e`；描边
  `--hairline: #e5e5e5` / `--hairline-soft: #ededed`。卡片靠**对比而非阴影**浮起。
- 文字 6 档（指南原值，收敛现有 16 种）：`--ink #0a0a0a` `--charcoal #1c1c1e`
  `--slate #3a3a3c` `--steel #5a5a5c` `--stone #888888` `--muted #a8a8aa`。
- 强调：`--primary #0a0a0a` / `--on-primary #ffffff`；`--mint #00d4a4`
  `--mint-deep #00b48a` `--mint-tint rgba(0,212,164,.10)`。薄荷**只**用于强调型 CTA、
  选中态指示、输入框聚焦边框；绝不用于正文或大面积填充。
- 状态色用指南许可的四个附加强调色 + stone：`--status-ok #1ba673`
  `--status-warn #c37d0d` `--status-error #d45656` `--status-info #3772cf`
  `--status-idle #888888`。约 40 个 tint 字面量改为
  `color-mix(in srgb, var(--status-x) 12%, var(--surface))` 推导。
- 圆角 6 档：`4 / 6 / 8 / 12 / 16 / 9999`。**所有按钮**用 `--radius-full`。
- 层级 4 档：`--elev-0: none`（配 hairline，绝大多数卡片）/ `--elev-1` / `--elev-2`
  （弹层）/ `--elev-3`（模态、移动抽屉）/ `--elev-mint`（极少量强调）。
- 排版 11 个 token，下限从 9px 抬到 11px；`--stat-lg`（28px/-1px/tabular-nums）
  统一替换 4 种大数字写法。
- 动效：`--motion: 160ms ease` / `--motion-slow: 200ms ease`，替换现有
  `.15/.16/.18/.22s`。保留现有 `prefers-reduced-motion` 块。

### 组件（类名不变）

- 侧栏浅色（`--sidebar-width` 190 → 216px）：`--surface` 底 + 右 hairline，
  `.menu-item.active` 用 `--surface-soft` + `--ink` + 600 + **左 2px 薄荷竖条**；
  删掉蓝色渐变和 `0 8px 22px` 光晕。
- 按钮全部 `--radius-full` 胶囊：`.primary-button` ink 黑底白字；
  `.complete-setup-button` 薄荷底黑字（强调 CTA）；`.toolbar-button` / `.close-button`
  / `select` hairline 描边；`.icon-button` 32px 圆形；`.danger-button` 红字 + hairline；
  `.link-button` 用 `--status-info`（薄荷禁止用于文字）；tab 类走 segmented-tab
  （active = ink + 2px ink 下边框）；pill 类走 pill-tab（active = ink 底白字）。
- 输入框统一 36px + `--radius-md` + hairline；**聚焦态改为 `2px solid var(--mint)`**，
  全局 `:focus-visible` 同步。
- 表格：`thead th` = `--surface-soft` + `--text-micro-upper` + `--stone`；`tbody td` =
  `--text-body-sm` + `--slate` + `--hairline-soft` 行线；行 hover 改 `--surface-soft`。
  **IP / MAC / 字节数 / 速率 / 时长 / 端口 / 百分比等数值列一律 `--font-mono` +
  tabular-nums。**
- 卡片（`.panel` `.metric-card` `.fleet-summary` `.status-tile` `.reference-panel`
  `.resource-card`）统一 surface + 1px hairline + `--radius-lg` + `--elev-0`；
  删掉 `.status-tile::after` 的三色渐变条。`.metric-*` 4 个色调重映射为
  `--ink` / `--status-info` / `--mint-deep` / `--status-warn`（原 purple `#7c3aed`
  不在指南许可范围内）。

### 顺带修掉的真实缺陷

- `--metric-secondary` / `--metric-tertiary` 只在 `.metric-purple` / `.metric-orange`
  上声明（`index.css:312-313`），`.metric-blue` / `.metric-green` 缺失 → 这两种卡片里的
  `.metric-part-1` / `-2` 解析为空值。补齐 4 个色调的完整三段色。
- fleet 环形图 `inactive/other` 在 TSX 里是 `#f59e0b`（`App.tsx:2331`），CSS 图例文字
  用 `#b45309`（`index.css:269`）—— 同一状态两种橙色。统一到 `--status-warn`。
- `--resource-soft` 为死属性（声明未消费），删除。
- `.chart-svg` / `.chart-axis-label` 在 JSX 中零引用（已被 ECharts 取代），删除。
  `.polyline` / `.chart-area` / `.upload-area` / `.download-area` **逐个 grep 确认后**
  再删，不盲删。

### Dark 主题（推导）

指南明确"无已发布暗色板"，但给了 4 个锚点（`canvas-dark #0a0a0a`、
`hairline-dark #1f1f1f`、`on-dark #ffffff`、`on-dark-muted #b3b3b3`）。按同一精神补齐：
`--surface #141414` / `--surface-soft #1a1a1a` / `--hairline-soft #191919` /
`--charcoal #ededed` / `--slate #d4d4d4` / `--stone #8a8a8a` / `--muted #6a6a6a`；
`--primary #ffffff` + `--on-primary #0a0a0a`（指南：深色带上用白胶囊）；薄荷不变
（本就为深色设计）；状态色提亮保证近黑上的对比（ok `#2fc48f` · warn `#e0a44a` ·
error `#f08585` · info `#6b9be8`）。视觉层 token 化后，现有 dark 块 97 行逐选择器覆盖
（`792-888`）绝大部分可直接删除，末尾散落的 dark 规则（`1308-1312`）一并合并。

### TSX / HTML

- `App.tsx` 删 glass 3 处：`:55` `PanelTheme` 类型、`:78-82` `panelThemeOptions`、
  `:217` 持久化校验（已存 glass 的用户自动回落 light）。
- `RealtimeTrafficChart.tsx` 现硬编码 12 个颜色值且 `chartThemeOption()` 只对 dark 生效
  → 改为用 `getComputedStyle` 读 CSS 自定义属性，挂进已有的 `data-theme`
  `MutationObserver`（`:143-146`）。下行 → `--status-info`，上行 → `--mint-deep`，
  tooltip 改 `--surface-code` 底。
- 抽 `statusPalette()` 助手（同样走 `getComputedStyle`，按主题记忆化），供流量图与
  fleet 环形图（`App.tsx:2329-2346`）共用，消掉 `:2331` 的硬编码色表。
- `web/index.html` 的 `<meta name="theme-color">` `#071d38` → `#ffffff`，
  加 `media="(prefers-color-scheme: dark)"` 的 `#0a0a0a` 变体。

## Non-Goals

- 不拆分 `App.tsx`（3459 行确实该拆，但与视觉重构混在一起会让 review 和回滚都困难，
  留作独立后续任务）。
- 不改数据流、API、类型或页面结构。
- 不新增前端依赖（不引入 Tailwind / UI 库）。
- 不改响应式断点结构（仅在其中替换 token）。

## Acceptance Criteria

- [ ] `npm --prefix web run lint` 通过
- [ ] `npm --prefix web run build` 通过；4 个 woff2 被指纹化输出到
      `internal/ui/dist/assets/`；产物字体体积增量 < 250KB（实测 188KB）
- [ ] `index.css` 中不再有裸的近黑/灰字面量、裸圆角 px、裸 box-shadow、裸字号
      （状态色 tint 走 color-mix，几何值除外）
- [ ] `--radius-lg` 被真实消费；`--shadow-panel` 与 glass 块不再存在
- [ ] `.metric-blue` / `.metric-green` 的 `--metric-secondary` / `--metric-tertiary`
      有值，`.metric-part-1` / `-2` 能解析
- [ ] fleet 环形图与 CSS 图例的 inactive 色一致
- [ ] 流量图与环形图在 light / dark 切换时颜色随 token 变化（不再有硬编码色值）
- [ ] `.trellis/spec/frontend/component-guidelines.md` 记录的行为契约未被破坏
      （接口物理/逻辑分页与 `在线`/`Down`/`已禁用` 标签、fleet 顶栏唯一搜索框位置、
      `headingless-topbar` 紧凑间距、终端监控默认 `online` 过滤与 `在线状态` 表头
      筛选气泡、终端身份列的名称 + MAC 两行）
- [ ] 用户按交付的清单在 light / dark × 1440/1024/768/390 完成人工视觉验收并批准

## Verification

按 `.trellis/spec/frontend/quality-guidelines.md`（该规范**明确禁止**默认用浏览器自动化
做视觉验收）：

1. `npm --prefix web run lint`（oxlint）
2. `npm --prefix web run build`（`tsc -b && vite build`）
3. 检查产物体积增量与字体指纹化结果
4. **视觉验收交给用户本人**，交付：`npm --prefix web run dev` 地址、视口
   （1440 / 1024 / 768 / 390）、逐页清单（仪表台 / 系统概览 / 接口 / 终端 /
   连接详情 / 流量 / 网络服务 / 系统运行 / 设置各子页 / 登录 / 初始化向导 / 快速接入）、
   light 与 dark 两套主题、已知风险点（sticky 表头、移动端抽屉、conic-gradient 环形图、
   ECharts 主题跟随）

## Deployment Gate

`AGENTS.md` 规定：凡改动可运行程序，须先部署到 `10.0.0.6`（保留二进制/配置/SQLite 的
时间戳备份）、验证 systemd 服务与 health 端点、等用户人工检查并明确批准后，才能创建提交。
本地 lint + build 通过后停下交给用户；用户确认后再走部署流程，批准后再提交。
