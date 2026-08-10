# 修复移动端终端监控指标被列表遮挡

## Goal

修复移动端进入 `终端监控` 后，顶部六项概览（设备 / 连接 / ↑ / ↓ / 累↑ / 累↓）被终端列表遮挡或裁掉的问题，使 `全部`、`IPv4`、`IPv6` 三个页签都能完整看到标签和数值。

## Background

`.terminal-list-content` 是固定高度（`100dvh`）的纵向 flex 容器，并且 `overflow: hidden`。容器内 `header.topbar` 是可收缩的 flex 项，而 `.terminal-list-panel` 是 `flex: 1 1 auto` 且 flex-basis 取内容高度（整张表格 + 分页）。移动端规则把 `.topbar:not(.detail-topbar)` 的 `min-height` 归零后，顶部栏失去了收缩下限：列表越长，顶部栏被压缩得越多，溢出的行既被容器 `overflow: hidden` 裁掉，又被 `position: relative` 的列表面板覆盖，因此表现为“`全部` / `IPv4` 完全看不到概览，`IPv6` 只剩标签行、数值被 `设备名称` 表头行遮住”。

## Requirements

- 移动端 `终端监控` 的顶部栏在任何页签、任何列表行数下都保持自身内容所需的高度，不被列表面板压缩或覆盖。
- 六项概览的标签与数值在 `全部`、`IPv4`、`IPv6` 三个页签下均完整可读；数值不得被表头或列表行遮挡。
- 终端列表面板承担全部剩余高度并保留内部滚动，列表区域不出现新的裁切、双滚动条或文档级水平溢出。
- 同类的固定高度滚动容器（终端连接详情页 `.connection-detail-content`）采用同一约束，避免同一成因在详情页复现。
- 桌面端与中等宽度布局的顶部栏高度、概览排布和列表行为保持不变。
- 使用最小范围的 CSS 修改，不改动数据逻辑、组件结构或交互。

## Acceptance Criteria

- [x] 375px 与 390px 宽度下，`终端监控` 的 `全部`、`IPv4`、`IPv6` 三个页签都能看到完整的六项概览（标签 + 数值）。
- [x] 概览数值的底边不超过顶部栏底边，也不进入列表面板区域（以布局测量为准，不只依赖肉眼判断）。修复后 390px：`header.h == header.scrollHeight == 165`，`summaryValueBottom 95 <= panelTop 165`。
- [x] 列表行数变化（每页 10 / 20 / 50）不影响顶部栏高度。
- [x] 终端列表在移动端仍可垂直滚动，页面无文档级水平溢出（内容宽 351 / 366 实测 `docOverflow False`，六项数值均未裁切）。
- [x] 终端连接详情页在移动端顶部栏同样不被内容压缩（同一条规则覆盖 `.connection-detail-content`）。
- [x] 桌面端（1440px）终端监控顶部栏与列表布局与修复前一致（`header.h == scrollHeight == 96`，自然高度不超过 62px 下限语义，规则在桌面端为空操作）。
- [x] `npm --prefix web run lint`、`npm --prefix web run build`、`go test ./...`、`go vet ./...`、`git diff --check` 通过。
- [x] 本地运行验证通过：`/healthz` 200，`index.html` 指向 `assets/index-BVbBuc5w.css`，该资源含本次规则。
- [x] 已按部署门禁部署到 `10.0.0.6`：备份 `/opt/rosboard/backups/<ts>-mobile-summary/`（二进制 + config.yaml + systemd 单元 + 停服后的 `rosboard.db`），换新后 `systemctl is-active` 为 `active`，`/healthz` 200，远程 CSS 哈希与本地构建一致（`29e4e645…`）且含本次规则，日志无异常。
- [ ] 等待用户在部署实例上人工验收确认后再提交代码。

## Notes

- 成因是 commit `eb893ed`（统一移动端顶部栏）把移动端顶部栏 `min-height` 归零后暴露出来的 flex 收缩问题，不是概览组件本身的问题。
- 复现与验证使用 `/tmp/rosboard-mobile-repro/` 下的静态 harness（复制 `web/src/index.css` + 终端监控真实 DOM 结构 + headless Chrome 截图与几何测量），避免依赖真实设备数据。
