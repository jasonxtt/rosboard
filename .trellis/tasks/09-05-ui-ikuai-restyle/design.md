# 技术设计：仿 iKuai 4.0 UI 重写

## 视觉权威来源

1. `preview-ikuai/` 静态设计稿（用户已评审通过）——布局与观感的最终依据
2. `research/ikuai-40-design-tokens.md`——iKuai 4.0 实测色值与组件规格
3. `research/rosboard-feature-inventory.md`——功能零丢失核对基线

## 总体策略

**保 className 契约、换 token、按组件块改样式；结构改动只发生在外壳导航。**

- 现有体系：零 UI 依赖，全部手写 CSS（`web/src/index.css` 1907 行，~60 个 CSS 变量 token），TSX 只用语义化 className。这套契约保留。
- 不引入任何新依赖（无 Tailwind / antd）。构建产物体积基本不变。

## Token 换血（index.css `:root`）

token 名称不变，值替换为 iKuai 实测值：

- 主强调：`--mint #00d4a4` 系 → 蓝 `#4794EB` 系（`--focus-color`、active、链接、主按钮底）；新增 `--primary-blue: #4794EB`、`--primary-blue-hover: #3A83D4`、`--primary-blue-tint: #F0F9FF`
- 主按钮：黑底 pill → 实心蓝圆角 6；`--radius-full` 在按钮上的使用改为 6px（token 保留，按钮规则改引用 `--radius-sm`）
- 表面：`--canvas #fafafa` → `#F5F7FA`；卡片白 + 1px `#EAEEF2` 边 + 极轻阴影（替代现有阴影体系观感）
- 文字梯度：`#0a0a0a/#1c1c1e/#3a3a3c...` → `#333/#666/#999` 系
- 表格头底 `#F9F9F9`；分隔线 `#F0F0F0`
- 淡彩统计卡：新增 `--tint-blue: #F0F6FD`、`--tint-purple: #F5F4FD`、`--tint-orange: #FFF8EC`
- 图表色（`lib/themeTokens.ts` 同步）：蓝 `#4794EB` / 绿 `#7FD38D` / 紫 `#A5A0F8` / 橙 `#F5A623`
- 语义色：ok `#22C55E`、warn `#F5A623`、error `#D45656`
- dark mode（`:root[data-theme="dark"]`）：整组同步换板，主蓝可略提亮保证对比度

## 外壳重构（App.tsx PanelApp，约 1333-1530 行）

- 侧边栏单柱 → 双列：一级列（140px，logo+版本 tag / 搜索框 / 设备切换器 / 五个一级项 icon+文字）+ 二级列（140px，当前组的子菜单，紧贴列上沿）。仪表台、系统概览无子菜单时二级列收起。
- 菜单结构与文案一字不动（真实结构见 inventory），仅改呈现形态。
- 顶栏精简：保留页面标题 + 页内 tab（终端地址族等）+ 刷新控制 + 主题 + 搜索入口，按设计稿摆位。
- 移动端：`<=767px` 退化为单列抽屉（现有 sidebarOpen 逻辑保留），二级项内联展开。

## 组件级重写（只动 CSS，不动 JSX 结构）

按钮三级（实心蓝/白底描边/蓝色文字链接）、表格（表头 #F9F9F9、行分隔 #f0f0f0、上行蓝下行绿、蓝色操作链接、排序 ↕ 符号）、页内 tab（蓝字+2px 下划线）、分段控件（灰底容器+蓝底激活块）、输入/选择（浅填充 #EEF2F8 或白底细边）、状态徽章（绿/灰/红点+字）、dialog（维持弹窗形态仅换皮，不改成整页表单）、分页条、tooltip、通知条、步骤条（策略向导）。

## 页面级调整（JSX 结构小改）

- **登录/初始化页**：渐变背景 + 居中白卡布局（结构调整）
- **系统概览**：按设计稿终版重排 grid（设备信息条、淡彩统计卡×3、WAN信息+快捷入口左列、速率图+柱状图+内存CPU 右列）。WAN 信息的运营商/延迟/本月数据先核对 dashboard API；不存在则与用户确认替代方案。
- **终端监控**：列与真实一致（设计稿已对齐）；样式换新
- **features/**（policy、policy-routing、access-control）：共享组件同步换皮，向导步骤条、变更清单、状态徽章按设计稿

## 兼容与回滚

- 分支：`feat/ui-ikuai-restyle`（基于 `feat/policy-access-rebuild`），Draft PR 先以 `feat/policy-access-rebuild` 为 base（父 PR #4 未合并），父合并后 retarget `main`
- 每个里程碑（token/外壳/组件/页面/图表/dark）单独 commit + 截图验证
- 回滚 = 分支级；生产回滚走 AGENTS.md NAS 备份
- 开发期将 `web/vite.config.ts` 的 `/api` 代理从生产 10.0.0.6 改指本地实例，避免误触生产
