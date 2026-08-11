# 设计：统一 UI 设计令牌与控件基类

本文档基于 `/tmp/rosboard-ui-tokens/inventory.md`（codex 出的现状清点，不入库）定死档位与每期范围。
**核心原则：档位值全部来自现状，第一至三期计算样式差异集合必须为空。**

## 0. 一个范围决策：间距不做数值比例尺

PRD 第一期原写"引入间距令牌"，清点回来后我改变了做法，理由如下。

清点显示间距实际分布是 **两套互相错开的 4px 尺**：

- 偏移 0：`4 / 8 / 12 / 16 / 20 / 24`
- 偏移 2：`6 / 10 / 14 / 18`

再加五个奇数游离值 `1 / 3 / 5 / 7 / 9 / 11`（合计约 49 处）。合起来等于"2px 网格"，不是一把比例尺。

在"零可见变化"约束下，`3px` 不能并入 `4px`、`10px` 不能并入 `12px`。若强行建 `--space-2 … --space-24` 这类**按像素值命名**的令牌，结果是给 16 个像素值改个名字：既没有消掉任何一处不一致，也无法通过改一个令牌全局调整（调用点引用的是 `--space-10`，而不是某个角色）。那是纯增指向层，负收益。

因此第一期改为：

- **按角色命名的间距令牌**，取值等于现状高频值。角色可以在后续一次性调整，且调用点读起来自解释。
- 真正的一次性值保持字面量，逐条登记在 §6 的账本里。
- 奇偶混用的收敛（`3→4`、`10→12`、`14→16` 一类）单列为**可选第五期**，需要用户明确接受微小可见位移才做，不塞进本轮。

PRD 的第一期要求与验收标准需按此同步修订（见本文末尾"PRD 同步项"）。

## 1. 交互控件高度令牌

清点：桌面上下文 43 条 + 767 媒体块 31 条 = 74 条高度规则，取值 8 种。其中 **55 条**落在四个档位上，可直接令牌化且零差异。

| 令牌 | 值 | 覆盖规则数 | 角色 |
|---|---|---|---|
| `--control-h-xs` | 28px | 3 | 表格内联小按钮（`.global-warning-list-head button` 521、`.connection-sort-button` 597、`.column-filter-button`） |
| `--control-h-sm` | 32px | 12 | **桌面默认控件高**：顶栏 select/图标钮、主题钮、分页、工具栏、告警开关、危险钮 |
| `--control-h-md` | 36px | 12 | 表单与对话框控件：搜索框、详情页 tab、设置表单、主/次按钮、密码钮、元数据字段 |
| `--control-h-touch` | 44px | 28 | 767 媒体块统一触控高（另有 `.range-pills button` 以 `height:100%` 继承 44px 父容器） |
| `--control-box` | 16px | 3 | 勾选框/单选框字形（`.checkbox-label input` 744、`.checkbox-field input` 765、`.interface-option input` 813，均带 `!important`，第三期处理） |

**保留字面值及理由**（第一期不动，写进 CSS 注释）：

| 值 | 位置 | 保留理由 |
|---|---|---|
| 1px | 825 `.theme-option > input` | 视觉隐藏的原生 input，不是尺寸档位 |
| 15px | 767 `.toolbar-toggle input` | 与 16px 差 1px 的游离值；并入 `--control-box` 是可见变化，登记待第五期 |
| 26px | 478 `.range-pills button` | 44px 药丸容器内的内联 chip，不是独立控件档位 |
| 34px | 264 `.sidebar-device-card > select`、1140 `.provisioning-mode-toggle button` | 落在 32/36 中间，两侧都差 2px；登记待第五期 |
| 38px | 279 `.monitor-page-tabs button` | 同上，差 36px 两像素 |
| 42px | 241 `.mobile-menu-button`（桌面上下文） | 移动端同元素已是 44px；并档是可见变化，登记待第五期 |
| 52 / 54 / 58 / 66 / 72 / 76px | 314 / 757 / 810 / 770 / 1074 / 823 / 1079 | 卡片式可选行（主题卡、接口卡、设备行、折叠标题），是**行高**不是控件高，属另一族节奏，本轮不纳入控件档位 |
| 64 / 92 / 112 / 230 / 280px | textarea 与 onboarding 卡片 | 内容区最小高度，与控件档位无关 |

## 2. 间距角色令牌

取值全部等于现状高频值，替换范围仅限"该值当前就用于该角色"的调用点。

| 令牌 | 值 | 现状处数 | 角色定义 |
|---|---|---|---|
| `--gap-label` | 3px | 12 | 标签与其值的纵向贴合（`.detail-summary-item`、`.device-row span`、`.interface-option > span` 等） |
| `--gap-inline` | 6px | 9 | 同一行内图标与文字、并列小元素 |
| `--gap-row` | 10px | 25 | **最高频**：列表行内、动作区、筛选面板等紧凑成组间距 |
| `--gap-section` | 14px | 17 | 卡片之间、表单行之间、区块之间的常规节奏 |
| `--gap-shell` | 18px | 6 | 页面级容器间距（`.topbar`、`.settings-panel`、`.provisioning-flow`、`.monitor-page-tabs`） |
| `--pad-pill-x` | 14px | 6 | 药丸控件默认左右内边距（工具栏/分页/toggle/danger 一族） |
| `--pad-pill-x-sm` | 12px | 4 | 紧凑药丸左右内边距（顶栏 select、主题钮、告警钮） |
| `--pad-pill-x-lg` | 18px | 2 | 主行动按钮左右内边距（`.primary-button` / `.close-button` / `.complete-setup-button` / `.full-reset-button`） |
| `--pad-cell` | 8px 10px | 4 | 表格单元格（`.overview-interface-table`、`.resource-table` 的 th/td） |
| `--select-arrow-pad` | 28px | 1 → 第二期扩为全部 select | 自绘箭头预留的右侧内边距 |

`4 / 8 / 12 / 16 / 20 / 24` 中未落入上述角色的用法保持字面量——它们本来就在 4px 网格上，加令牌无收益。
`1 / 5 / 7 / 9 / 11` 与 `40 / 42 / 44 / 48`（图标/后缀预留位）保持字面量，登记在 §6。

### 2.1 按证据补的六个角色（第一批分类之后）

初版只有上表 10 个令牌，实际分类（`/tmp/rosboard-ui-tokens/classify.md` 表 A）只能命中 47 条声明，剩下四十余条被判"角色不符"。逐条核对后确认**是令牌集漏了角色，不是分类过于保守**——上表除药丸与单元格外没有任何 padding 令牌，于是所有容器内边距无处可归。而 PRD 的目标是"改一处即生效"，`padding: 10px 14px` 抄 6 遍、`padding: 14px` 抄 5 遍正是同一个病，放过它等于第一期没做事。

| 令牌 | 值 | 处数 | 角色定义 |
|---|---|---|---|
| `--gap-stack` | 10px | ~6 | `display: grid` 的纵向堆叠（卡片/区块内部标题+字段+内容） |
| `--gap-stack-sm` | 6px | ~5 | 紧凑纵向堆叠，比 `--gap-label` 松一档 |
| `--pad-card` | 14px | 5 | 顶层卡片/面板/fieldset 内边距（`.metric-card`、`.resource-card`、`.reference-panel`、`.settings-fieldset`、`.settings-disclosure-body`） |
| `--pad-box` | 12px | 3 | 卡片内部嵌套盒（`.verification-summary`、`.connection-filter-panel`、`.scope-result-section`） |
| `--pad-bar` | 10px 14px | 3 | 横条内边距（`.data-toolbar`、`.pagination`、`.load-toolbar`） |
| `--pad-control-x` | 12px | 4 | 矩形文本控件左右内边距（`.search-input`、`.tab-button`、筛选面板 input/select、`.settings-form` 控件） |

**同值不同名是故意的。** `--gap-row` 与 `--gap-stack` 同为 10px，`--pad-pill-x-sm` / `--pad-box` / `--pad-control-x` 同为 12px。这正是角色令牌的价值：25 个无从分辨的字面 10px 变成两族可独立演进的引用。已核实 `--pad-pill-x-sm` 的 6 个位点全部带 `border-radius: var(--radius-full)`，与矩形控件族无交集。

设 3 处为纳入门槛。低于门槛的保持字面量：`.panel { padding: 18px }`（仅 1–2 处）、`.content { padding: 0 18px 22px }`（一次性）。

### 2.2 本期不收敛、进账本的真不一致

分类过程暴露出"角色相同但取值有两套"，这些是后续该收敛的目标，收敛必然产生可见位移，故只登记：

- 图标+文字同行：`--gap-inline` 是 6px，但 `.device-overview-offline`(440)、`.metric-value-row`(462)、`.theme-option`(857) 同角色用 10px。
- 卡片之间：`--gap-section` 是 14px，但 `.terminal-scope-groups`(827)、`.theme-options`(855)、移动端 summary(1057) 同角色用 10px。
- 表格单元格三套并存：`8px 10px`（已令牌化）、`.data-table thead th` 的 `10px 12px`、`.data-table tbody td` 的 `11px 12px`。
- 容器内边距两套并存：`--pad-card` 14px 与 `--pad-box` 12px 的边界靠嵌套层级判定，并非全文严格一致。

## 3. 焦点环令牌

现状：全局在 `index.css:128` 定义一次（`outline: 2px solid var(--mint); outline-offset: 2px`），另有 13 条规则重复声明，`outline-offset` 有 `0 / 2 / 3 / -2` 四种值。

| 令牌 | 值 | 语义 |
|---|---|---|
| `--focus-width` | 2px | 环宽，全局唯一 |
| `--focus-color` | `var(--mint)` | 环色，全局唯一 |
| `--focus-offset` | 2px | 默认：控件有独立边界，环画在外侧 |
| `--focus-offset-flush` | 0 | 控件紧贴一个裁切容器的边，2px 外扩会被切掉 |
| `--focus-offset-inset` | -2px | 父链上有 `overflow: hidden` 或滚动视口，环必须画在内侧 |
| `--focus-offset-clear` | 3px | 控件自带 2px 边缘指示器（tab 的底边），环要让开它 |

**订正两处原假设。** 清点 16 条 outline 声明后（证据见 `/tmp/rosboard-ui-tokens/classify.md` 表 B）：

1. `--focus-offset-clear` 原名 `--focus-offset-loose`，注释写的是"圆形控件"。全文唯一的 3px 位点是 `.monitor-page-tabs button`，它 `border: 0; border-bottom: 2px solid transparent`，active 态就是给这条底边上色——3px 是为了让开自己的指示器，与圆形无关。原注释是凭空猜的，已改。
2. 原计划"只重申默认值的直接删除"，实际**一条都删不掉**。全局规则只覆盖 `button, input, textarea, select` 的 `:focus-visible`，而取值等价的那几条各有存在理由：
   - `.connection-table-viewport`（div 滚动视口）、`.settings-disclosure > summary`——全局规则不命中这些元素类型；
   - `.metric-composition-part` 与 `.link-button` 各是一对 `:focus { outline: none }` + `:focus-visible {恢复}`，恢复那条是必需的；
   - `.provisioning-script-area textarea` 用 `:focus` 而非 `:focus-visible`，取值同但行为不同（鼠标点击也出环），改它是行为变更，不属本期。

   唯一真正冗余的是 `.connection-sort-button` / `.column-filter-button`（已确认是 `<button type="button">`，全局规则以更低特异性命中、取值全同）。但删除它无法用 cssdiff 证明惰性——工具比的是 `(媒体查询, 选择器, 属性)` 三元组，删掉必然报 `ONLY_IN_BEFORE`，"渲染不变"只能靠人工论证。为保住第一期 `ONLY_IN_BEFORE=0` 的硬门槛，**这条删除移到第三期**（那一期本就是删冗余）。

因此第一期在焦点环上的成果是"令牌化 + 语义命名准确"，不是"声明数量下降"。验收标准按此改写。

## 4. 断点写法

现状：`940 / 959 / 980 / 1164` 四个块写作 `(max-width: Npx), (max-device-width: Npx)`；`1212 / 1217`（DHCP 卡片）只写 `max-width`。且 767 断点自身散在 `980` 与 `1217` 两个块中。

**原方案是统一为"两者都有"，实际验证后撤回；第一期保持现状。**

验证推翻了“补 OR 分支零回归”的假设：headless Chrome 在 1440 / 1024 视口下仍可能满足 `max-device-width: 999px`，使 DHCP 卡片错误套用窄屏网格。DOM harness 在两种主题下均测出 3 项 DHCP 计算样式差异。反向全局删除 `max-device-width` 又可能让依赖该 OR 分支的真机丢失窄屏布局。因此第一期不改任何媒体查询条件，把整体统一移入需真机验证、允许可见变化的第五期。

**块合并是有条件的，不无脑做。** 把 `1217` 的 767 块并进 `980` 的 767 块，会让这些规则从"文件末尾"移到"981–1211 之前"。如果 981–1211 之间存在命中同元素同属性、且特异性不高于它们的规则，级联赢家就变了——这不是排版整理，是行为改动。因此：

1. 先由工具算出"合并前后每个 `(媒体查询, 选择器, 属性)` 的最终赢家"是否完全一致；
2. 并人工核对 981–1211 之间的规则与被移动规则是否存在选择器交集；
3. 只有两项都通过才执行合并；否则**只统一媒体查询写法，保留分散块**，并把合并降级为账本项。

`1212` 的 999 块并进 `959` 同理。

**实际审计结论：保留分散块与现有查询条件。** `767px` 主块之后仍有 Provisioning 与 DHCP 的基础规则；把它们各自的移动端覆盖前移进主块，会被后写的同选择器基础规则重新覆盖。反向把主块整体后移又会改变主块与中间规则的现有级联次序。`999px` 的 DHCP 块同理。给 DHCP 补 `max-device-width` 也已由多宽度 DOM harness 证明会在部分桌面环境改变网格，因此第一期不移动规则、不改查询条件。

## 5. 类型比例尺自洽

- `10px`（`1006 / 1012 / 1020` 三处，均在移动端）：新增 `--fs-nano: 10px`，并把 `:root` 注释里的 "11px floor" 改为 "11px floor for body copy; --fs-nano is reserved for dense mobile metric labels only"。选择保留 10px 而非上调到 11px，因为上调是可见变化。
- `18px` 图标尺寸（3 处）：新增 `--icon-md: 18px`。其他图标尺寸暂留字面量，待第二期顺带清点后决定是否成族。

## 6. 待第五期的账本（需用户接受可见位移才做）

以下为"要收敛就必然产生可见变化"的项，本轮**只登记不动**：

- 奇数间距 `3px`（12 处）、`5px`（8 处）、`7px`（8 处）、`9px`（8 处）、`11px`（1 处）、`1px`（3 处）→ 并入偶数网格。
- 偏移 2 与偏移 0 两套 4px 尺合流（`6→8`? `10→12`? `14→16`? `18→20`?）——需要先定哪一套是基准。
- 控件高度游离值 `15 / 26 / 34 / 38 / 42px` 并入四档。
- 卡片行高族 `52 / 54 / 58 / 66 / 72 / 76px` 是否自成一套 `--row-h-*` 档位。
- 断点统一：整体删除或补齐 `max-device-width` 均会改变一部分设备的匹配集合，需真机验证后选择现代 `max-width` 方案。
- **图标尺寸尺（第一期原计划做，已撤回）。** 全文图标尺寸有 `11 / 14 / 14 / 15 / 15 / 16 / 17 / 17 / 18 / 18 / 19 / 22 / 28 / 28 / 40px` 十五个取值，且根本不存在既有的尺。原计划加 `--icon-md: 18px`，实际只能覆盖 4 条声明，剩十余个值照旧散着——这是 §0 判定过的"按像素值命名令牌"陷阱：不消除任何不一致，只给 18px 改名，还会造成"图标已令牌化"的错觉，比不加更糟。已从 `:root` 撤掉。收敛需要动像素（`17→16`、`19→20` 之类），属可见变更。
  注：`--fs-nano: 10px` 不在此列——它给一条已成型的 9 档字号尺补地板档，3 个位点角色一致（移动端密集指标标签），保留。

## 7. 分期范围与验证

### 第一期：令牌层（本文 §1 §2 §3 §4 §5）

改动仅限 `web/src/index.css`。零 JSX 改动。预期差异集合为空。

顺序：先加 `:root` 令牌，再分四批替换（高度 → 间距角色 + 焦点环 → 字号 → 断点），每批单独 `git diff` 审阅。图标批已撤回，理由见 §6。断点批放在最后且必须单独验证，因为它移动规则位置，是本期唯一有级联风险的操作。

### 第二期：药丸基类与 select

基类设计（值全部等于现状默认组）：

```css
.pill {
  height: var(--control-h-sm);
  padding-inline: var(--pad-pill-x);
  color: var(--slate);
  background: var(--surface);
  border: 1px solid var(--hairline);
  border-radius: var(--radius-full);
  cursor: pointer;
  transition: border-color var(--motion), background var(--motion), color var(--motion);
}
```

修饰类：`.pill--md`（36px）、`.pill--xs`（28px）、`.pill--icon`（正方、`padding: 0`、`display: grid; place-items: center`）、`.pill--pad-sm` / `.pill--pad-lg`、`.pill--ghost`（透明背景、无边框）、`.pill--warn` / `.pill--danger` / `.pill--primary` / `.pill--accent`。

20 个药丸实例逐一映射到"基类 + 修饰类"，映射表由 codex 先出、我审过再动手。移动端 44px 由 `.pill` 在 767 块内一次性提升为 `--control-h-touch`，替换掉现在散在 28 条规则里的手抄值。

同期把组件规则里的标签选择器（`select` / `button` / `input`）换成显式类——这是 44px 失效那个 bug 的直接成因（`.topbar-controls select` 特异性 0-1-1 压掉 0-1-0 的工具类）。需要同步改 `web/src/App.tsx` 的 `className`。

select 外观统一：全部 `select` 加 `appearance: none` + 自绘箭头（`background-image` 的 inline SVG 或 `::after` 覆盖层，选前者以便直接用在 `select` 上），右侧内边距统一 `--select-arrow-pad`。因此所有 select 的原生箭头会替换为统一箭头，原先右留白小于 28px 的 select 会增大右留白；这些是本期 requirements 直接要求的预期差异。除 `appearance`、箭头背景属性和 `padding-right` 外，第二期关键计算样式与几何必须保持不变。

实际验证覆盖 9 个逻辑 select（顶栏桌面/移动节点分开，共 10 个物理节点）以及告警、主题、图标、时间范围、两组分页和接入方式药丸。light/dark × 1440/1024/768/390/375 共 20 个 case、10 对改前/改后比较，非 select 预期项的差异为 0；报告在 `/tmp/rosboard-ui-tokens/phase2-dom-report.md`。移动目标绝对值检查仍会报告既有的接入方式 34px 按钮，这一游离高度已在 §1/§6 登记为第五期项目，本期前后没有退化。

### 第三期：表单选择器边界

`.settings-form select, .settings-form input`（747、1083）过宽，命中了勾选框与可选卡片，逼出 10 处 `!important`。改法：给真正的表单控件加显式类（如 `.settings-input`），选择器收窄到该类，然后删除 `!important`。

### 第四期：交互统一

- 自动刷新从原生 `select` 改为与主题一致的自定义 popup（`role="menu"` + `role="menuitemradio"`，与 `App.tsx:1312-1321` 同构），取值集合（停/1s/3s/5s/10s）与生效方式不变。
- 主题选项抽成单一数据源（现 `App.tsx:82-84` 的 `panelThemeOptions` 与顶栏各写一份）与单一样式来源（`.theme-menu-option` 314-319 与 `.theme-option` 823-826 合并，尺寸差异用修饰类表达）。
- `documentElement.dataset.theme` / `style.colorScheme` 的写入从 `App.tsx:639-640` 与 `1590-1591` 两处收敛为一处。

本期**允许可见变化**，但每一项差异必须事先在此文档列明预期。

### 验证方法（每期相同）

**主证据是构建产物 CSS 的"解析值声明对比"，不是 DOM 抽样。** 第一期本质上是"把字面值换成取值相同的 `var()`"，所以只要构建后的 `dist/assets/*.css` 里每条 `(媒体查询, 选择器, 属性)` 的**解析后取值**与改动前完全一致，就没有任何元素可能渲染不同——这比抽样测若干个元素强得多，且覆盖到达不了的页面（登录后、onboarding、DHCP 等）。

工具要求（由 codex 写，产物放 `/tmp/rosboard-ui-tokens/`，不入库）：

1. 解析改动前后两份构建 CSS，按 `@media` 上下文 + 选择器归一化成 `(媒体, 选择器) → 有序声明表`。
2. 用 `:root` 与 `:root[data-theme="dark"]` 的令牌表递归展开 `var()`（含 fallback），得到解析值；空白、`0px`/`0`、颜色写法、简写/长写需归一化后比较。
3. 输出差异集合。第一至三期该集合必须只含"`:root` 新增的令牌声明"这一类，别的必须为空。
4. 断点合并批额外输出"最终赢家变化"报告（见 §4）。

**DOM 层测量作为辅证**，用于捕捉解析值相同但级联顺序变化带来的影响，以及几何回归：沿用 `08-11-fix-mobile-monitor-summary-clipping` 的手法，真实 `index.css` + 真实 DOM 结构的静态 harness，headless Chrome 取计算样式与几何。

- 宽度：1440 / 1024 / 768 / 390 / 375。
- 逐元素比对：`height`、`padding`、`margin`、`gap`、`border-radius`、`border`、`background-color`、`color`、`font-size`、`font-weight`、`outline-*`；第一至三期差异集合必须为空。
- 另测：`documentElement.scrollWidth <= clientWidth`（无横向溢出）；顶栏 `offsetHeight === scrollHeight`（不回归概览遮挡缺陷）；移动端所有可点元素 `rect.height >= 44`。
- 环境坑：macOS headless Chrome 有 ~500px 最小窗口宽度，窄屏须强制容器宽度；本机无 `timeout`；Chrome 进程会驻留，须显式 `pkill`。

## PRD 同步项

- 第一期要求"引入间距令牌与交互控件高度令牌"→ 改为"引入交互控件高度令牌与**按角色命名**的间距令牌"，并说明不建数值比例尺的理由（§0）。
- 第一期验收"交互控件高度与间距的字面值数量显著下降"→ 高度部分保留（55/74 条令牌化）；间距部分改为"高频角色间距的字面值收敛为令牌引用，其余字面值均登记在案"。
- 新增"可选第五期"说明：奇偶网格合流与游离档位并档需用户接受可见位移，不在本轮范围。
