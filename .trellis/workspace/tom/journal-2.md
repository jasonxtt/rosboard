# Journal - tom (Part 2)

> Continuation from `journal-1.md` (archived at ~2000 lines)
> Started: 2026-08-07

---



## Session 59: Disable recognition defaults for v0.1.0

**Date**: 2026-08-07
**Task**: Disable recognition defaults for v0.1.0
**Branch**: `main`

### Summary

Defaulted MosDNS and feature-library recognition switches to off, left the MosDNS address blank until the operator enters a plain address such as 10.0.0.3, kept the feature-library URL preconfigured, and added migration for legacy default-enabled configs. Verified tests/build, deployed and manually accepted on 10.0.0.6, then republished v0.1.0.

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `1d5df0c` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 60: Mobile dashboard topbar layout fix

**Date**: 2026-08-10
**Task**: Mobile dashboard topbar layout fix
**Branch**: `main`

### Summary

Normalized the mobile monitor topbar into one shared control contract, stopped summary-row overlap at phone widths, targeted safari12 for CSS output, and served index.html with no-cache so phones stop pinning stale asset hashes. Accepted directly by the user instead of a fresh 10.0.0.6 deploy cycle; task archived with follow-ups deferred.

### Main Changes

### Main Changes

- Shared one mobile control contract across the monitor topbars so theme, manual
  refresh, and refresh-period controls stop overlapping at narrow widths, while
  desktop labels and behavior stay unchanged.
- Kept summary rows from overflowing on phones; hid the redundant status count
  and swapped in the compact refresh-period select below 767px.
- Set `cssTarget: 'safari12'` in `web/vite.config.ts` so the legacy-compatible
  media queries survive the CSS build for older iOS Safari.
- Served `index.html` with `Cache-Control: no-cache` in `internal/api/server.go`
  so mobile browsers stop pinning a stale document that references asset hashes
  which no longer exist after a rebuild.

### Testing

- `npm --prefix web run build` (tsc -b clean; asset hashes reproduced the
  committed `internal/ui/dist/` byte-for-byte, confirming dist matched source)
- `go build ./...`
- `go vet ./...`
- `go test ./...` — all packages ok

### Notes

Acceptance deviated from the AGENTS.md deployment gate: the user accepted the
mobile work directly rather than through a fresh deploy-and-inspect cycle on
10.0.0.6, and asked to archive the task with follow-up fixes deferred.

Process defect found while archiving — worth knowing before the next archive:
`task.py archive` auto-commits with `run_git(["commit", "-m", msg])` at
`.trellis/scripts/common/task_store.py:603`, with **no pathspec**. The narrow
scoping promised by `safe_archive_paths_to_add` only governs what the script
`git add`s; anything already staged is swept into the `chore(task): archive`
commit. Here that pulled `internal/api/server.go`, part of `web/src/index.css`,
and `web/vite.config.ts` into a commit labeled as an archive. Recovered with
`git reset --soft HEAD~1` (unpushed) and re-split into two scoped commits.
Prefer `task.py archive --no-commit` whenever the working tree has staged code.


### Git Commits

| Hash | Message |
|------|---------|
| `eb893ed` | (see git log) |
| `de13b21` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 61: 协议分析总开关：配置迁移、分析层门控与部署验收

**Date**: 2026-08-11
**Task**: 协议分析总开关：配置迁移、分析层门控与部署验收
**Branch**: `main`

### Summary

新增 protocol_analysis 总开关，关闭后跳过端口分类、域名反查、协议聚合与样本落库，连接抓取与终端速率不受影响；新装默认关、老配置迁移为开。

### Main Changes

### Main Changes

- Added a `protocol_analysis.enabled` master switch that gates the whole analysis
  layer: port-based `classifyApplication`, the MosDNS + feature-library reverse
  lookup, `aggregateProtocols`, `terminalFlowCategories`, and the per-poll
  `SaveProtocolSamples` write. The synchronizers and `ApplicationResolver` are not
  even constructed when it is off.
- Deliberately left connection fetching untouched. `FirewallConnectionsV4/V6` is
  load-bearing for per-terminal `CurrentUploadBps`/`CurrentDownloadBps`,
  `ConnectionCount`, `FamilyStats`/`FamilySummaries`, `ConnectionProtocolCounts`
  and the connection detail table, so the switch saves analysis work, not fetch
  work. The core regression test asserts the exact rate numbers with analysis off.
- Fresh installs default to off; an existing config file with no
  `protocol_analysis` section migrates to on so upgrades keep current behavior.
  Migration uses a **key-existence** probe (`map[string]yaml.Node`) rather than a
  pointer probe, so `protocol_analysis: null` counts as present and an explicit
  user choice is never overwritten.
- Frontend hides the protocol view, its tab and the terminal flow-distribution
  panel when off, with redirects for stale `localStorage` views and detail tabs.

### Testing

- `go build ./...`, `go vet ./...`, `go test -count=1 ./...` — all packages ok
- `npm --prefix web run lint` (oxlint, no output) and `npm --prefix web run build`
  (`tsc -b` clean; rebuild reproduced the committed asset hashes byte-for-byte)
- Deployed to `10.0.0.6`; backup at
  `/opt/rosboard/backups/rosboard-protocol-switch-20260810-235125/` (binary,
  config, systemd unit, plus `rosboard.db` copied while stopped so the wal was
  already checkpointed). systemd active, health 200, asset hashes and byte sizes
  matched the local build. Migration verified live: the remote config had no
  `protocol_analysis` section and gained `enabled: true` plus
  `protocol_analysis_migrated: true` on restart. User approved in the browser.

### Notes

Two-agent split worked well here: Claude wrote the PRD and gated, codex wrote
design.md and implemented. codex's design listed 6 conflicts with the PRD; five
were correct and four of those were genuine PRD errors — most usefully that the
"protocol stats sidebar button" the PRD told it to hide does not exist (line 1175
is the single 流量监控 parent; the protocols/policies choice lives only in the top
`monitorTabs`). Worth remembering: **a design review that finds my own spec wrong
four times is the review earning its cost**, so keep writing PRDs precise enough
to be falsifiable.

Conflict #3 was wrong — it claimed `terminalConnectionRow` lacked a resolver nil
guard, but `ApplicationResolver.Resolve` guards `r == nil` at
`application_resolver.go:38-41` and calling a pointer method on a nil pointer is
legal Go. Rejected it explicitly so it would not restructure code to fix a
non-bug. Verifying each claim against source rather than accepting the summary is
what caught this.

Reviewing codex's output also caught one out-of-scope change I reverted myself:
it had changed the `/api/mosdns` **status** endpoint's `Enabled` from
`cfg.MosDNS.Configured()` to the bare stored flag. The settings endpoint should
report stored values (so a form round-trip cannot silently drop them) while the
status endpoint reports effective state; that divergence is intentional.

Archive used `task.py archive --no-commit` per the convention recorded last
session, then committed the archive separately — the pathspec-less auto-commit at
`.trellis/scripts/common/task_store.py:603` did not get a chance to sweep staged
code this time.

Not verified: "no new `protocol_samples` rows after disabling" has unit-test
evidence only. `sqlite3` is not installed on `10.0.0.6` and installing packages on
the user's box was out of scope, so there is no live row-count comparison.


### Git Commits

| Hash | Message |
|------|---------|
| `8128c5d` | (see git log) |
| `6e87fc2` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 62: 修复移动端终端监控概览遮挡与自动刷新圆钮规格

**Date**: 2026-08-11
**Task**: 修复移动端终端监控概览遮挡与自动刷新圆钮规格
**Branch**: `main`

### Summary

移动端顶部栏在固定高度 flex 壳里被列表面板压缩导致概览被裁切/遮挡；自动刷新控件 44px 尺寸被更高优先级选择器压掉。两处均为 CSS 修复，已部署 10.0.0.6 并人工验收，发布 v0.1.1。

### Main Changes

## 背景

移动端 `终端监控` 顶部六项概览（设备 / 连接 / ↑ / ↓ / 累↑ / 累↓）在 `全部`、`IPv4` 完全看不到，`IPv6` 只剩标签行、数值被 `设备名称` 表头遮住。随后又发现刷新按钮旁的自动刷新时间控件与刷新圆钮大小不一致。

## 成因（两处都靠几何测量定位，不靠猜）

1. **概览被遮挡**：`.terminal-list-content` 是 `100dvh` + `overflow: hidden` 的纵向 flex 壳，`.terminal-list-panel` 是 `flex: 1 1 auto` 且 flex-basis 取整表内容高度。commit `eb893ed` 把移动端 `.topbar:not(.detail-topbar)` 的 `min-height` 归零后，顶部栏成了唯一可收缩项：列表行越多，顶部栏被压得越扁，溢出的概览行既被壳 `overflow: hidden` 裁掉，又被 `position: relative` 的列表面板盖住。所以行多的 `全部` / `IPv4` 整条概览消失，行少的 `IPv6` 只丢数值行。测量：改前 390px 下 `header.h 85 / scrollHeight 157`，概览数值底边 95 落在 `panelTop 85` 之下。
2. **自动刷新控件尺寸不符**：`.refresh-period-select-mobile`（0-1-0）里的 `height: 44px` 被 `.topbar-controls select`（0-1-1，一个类 + 一个标签）的 `height: 32px` 压掉——那条 44px 一直是死代码。实测改前刷新按钮 44×44 y113、自动刷新 44×32 y119，且 `appearance: auto`（iOS 会走原生 select 外观）。

## 改动（均为 `web/src/index.css`）

1. `.terminal-list-content > *:not(.terminal-list-panel), .connection-detail-content > *:not(.detail-page-connections) { flex: 0 0 auto; }` —— 固定高度滚动壳里只有滚动面板可收缩。
2. 把 44px 尺寸挪进本来就够优先级的 `.topbar-controls .refresh-period-select-mobile`，并从共享分组里摘掉那条死代码；加 `appearance: none` 去原生外观、`text-align-last: center` 保证 WebKit 文字居中（构建按 `cssTarget: safari12` 自动补 `-webkit-appearance`）。

## 验证

复现用 `/tmp/rosboard-mobile-repro/` 与 `/tmp/rb-measure/` 静态 harness（复制真实 `index.css` + 真实 DOM 结构 + headless Chrome 几何测量）。改后：390px `header.h == scrollHeight == 165`、概览数值底边 95 ≤ `panelTop 165`；内容宽 351 / 366 下 `docOverflow False`、六项数值均未裁切；两个圆钮同为 44×44 且 y 一致，操作行 `scrollWidth == clientWidth == 327`；1440px `h == scrollHeight == 96`（桌面端为空操作）。lint / build / `go test ./...` / `go vet ./...` / `git diff --check` 全过；本地起服务确认 `/healthz` 200 且嵌入资源含新规则。两次均按门禁部署 `10.0.0.6`（时间戳备份二进制 + config + systemd 单元 + 停服后的 `rosboard.db`），远程二进制与 CSS 哈希对齐本地构建，由 tom 在手机上人工验收通过后才提交。

## 经验

- headless Chrome 在 macOS 有 ~500px 最小窗口宽度，`--window-size=390,844` 实际 `innerWidth 500`；测窄屏要强制 `.content` 宽度而不是靠窗口尺寸。本机没有 `timeout` 命令，且 Chrome 进程会驻留，需显式 `pkill`。
- 移动端“看不见”的布局问题优先怀疑固定高度 flex 壳的收缩分配，其次才是组件本身；“尺寸没生效”优先算选择器优先级，两者都能用一次几何测量证实或推翻。
- 已把两条结论写进 `.trellis/spec/frontend/component-guidelines.md`（新增“固定高度滚动壳里只有滚动面板可收缩”不变量，并订正移动端概览“两行三列”的过期描述）。

## 发布

`VERSION` 0.1.0 → 0.1.1，README 当前版本号同步，推送 `main` 由 GitHub Actions 出 `v0.1.1` Release。


### Git Commits

| Hash | Message |
|------|---------|
| `8605a89` | (see git log) |
| `7a61b15` | (see git log) |
| `c097d37` | (see git log) |
| `191cf94` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 63: UI design tokens phases 1-2

**Date**: 2026-08-11
**Task**: UI design tokens phases 1-2
**Branch**: `main`

### Summary

Completed, deployed, and manually accepted phases 1-2. Paused before phase 3; remaining work is form-selector cleanup and refresh/theme interaction unification.

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `574498a` | (see git log) |
| `41c57ba` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete
