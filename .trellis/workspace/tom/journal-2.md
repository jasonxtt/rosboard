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


## Session 64: 完成 UI 设计令牌第三期

**Date**: 2026-08-11
**Task**: 完成 UI 设计令牌第三期
**Branch**: `main`

### Summary

为设置表单文本、数字、密码和 URL 控件增加 settings-input，收窄通用表单选择器并移除表单区域 !important；保留复选框、接口卡片和主题卡片的等值局部样式。通过前端 lint/build、Go test/vet、静态视觉回归和本地运行时验证，按门禁部署到 10.0.0.6 并备份 /opt/rosboard/backups/rosboard-ui-phase3-20260811-223755/，远端服务/API/嵌入 CSS 校验通过，用户人工验收通过。第四期未开始。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `d7508b1` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 65: 完成 UI 设计令牌第四期

**Date**: 2026-08-12
**Task**: 完成 UI 设计令牌第四期
**Branch**: `main`

### Summary

完成主题与自动刷新 ChoiceMenu 统一、手机端仪表台四控件满行布局和 PC 立即刷新圆形按钮；通过 lint/build、Go test/vet、响应式视觉回归、本地运行与远端部署验收，用户已通过。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `f9c2377` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 66: 合并刷新控件并统一轮询行为

**Date**: 2026-08-12
**Task**: 合并刷新控件并统一轮询行为
**Branch**: `agent/unify-ui-design-tokens`

### Summary

将立即刷新与自动刷新设置合并为分栏刷新控件；修正停止刷新时的手动刷新、实时/历史与终端详情轮询行为。完成本地 lint/build、Go test/vet、响应式 harness、浏览器运行态验证和 10.0.0.6 远程部署验收；用户已人工验收通过。备份：/opt/rosboard/backups/rosboard-refresh-control-20260812-004552/。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `05a15fc` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 67: Cmcc policy wizard discovery-unavailable: 只读诊断 + 前端 fail-closed 修复

**Date**: 2026-08-24
**Task**: Cmcc policy wizard discovery-unavailable: 只读诊断 + 前端 fail-closed 修复
**Branch**: `main`

### Summary

net Cmcc 新建策略 503 policy_discovery_unavailable 根因=RouterOS 上策略管理账号缺失（全 401，/rest/user 无该用户）；确认两条「代理」草稿仅在每设备 SQLite 为 desired-state，无 RouterOS 变更；前端向导改为 discovery 不可用 fail closed（禁用保存/先校验再落盘），WAN/LAN 提供明确 select+手动兜底；lint/build/go build 通过，未部署未提交。

### Main Changes

# Session: Cmcc policy wizard discovery-unavailable 诊断与前端 fail-closed 修复

Date: 2026-08-24 · Branch: main (dirty WIP) · 只读诊断 + 前端修复，未部署未提交

## 背景
用户操作 net 实例（10.0.0.6）Cmcc device（UUID 1d0eaf78-ab44-4ee8-b7de-375b2950d711）新建策略时：
向导接口/LAN 无法选择只能输入；保存草稿并生成预览返回 503 policy_discovery_unavailable；
页面出现两条「代理」出口草稿（一条 IPv4→pppoe-out1 绑定 Youtube，一条无域名列表）。

## 只读诊断结论（证据链）
1) 服务与二进制：rosboard.service active (running)，主进程 /opt/rosboard/rosboard -config /opt/rosboard/config.yaml，
   binary sha256 e3b923576daef74ae3fc79b514477d89b1f9fa920a80179adbbb2a5ae268ef09（约 23MB，2026-08-24 11:55 更换并重启）。
   Cmcc policy_access: enabled=true，username=rosboard_policy_42d62299e25a3200（密码已设置，不回显）。
2) 发现失败根因：用 config 中 policy_access 凭据直连 RouterOS 10.0.0.99 全部 GET 401
   （/rest/system/resource、/interface、/ip/route、/ip/firewall/*、/routing/route、/ipv6/firewall/* 等均 401）；
   同一 RouterOS 上 monitor 账号 rosboard_5f6602373b081907 相同 GET 全部 200；
   /rest/user 只读列表（monitor 账号，.proplist=name,group,disabled）只有 admin(disabled)、wyp、rosboard_5f66…、codex 四用户，
   无任何 rosboard_policy_42d62299e25a3200 用户，也无 rosboard_policy_g_* 组。
   → 直接根因：策略管理账号在 RouterOS 上不存在（被删除/从未真正执行 Winbox 脚本/设备被替换），
   config 里仍保存着该账号，fresh discovery（scanner 用 policy 账号扫描全部只读菜单）必然失败。
   恢复路径（运维，未执行）：UI「策略访问」重新生成账号脚本 → Winbox 执行 → 保存账号，之后 discovery 恢复。
3) 向导退化逻辑：EgressFields/WanInterfaceField 与向导 LAN 字段本来就是 input+datalist（候选来自 discovery）；
   discovery 不可用时 datalist 为空 → 只能手输，属既有设计，不是新 bug，但与 fail-closed 需求冲突。
4) saveAndPlan 部分保存语义：向导第 6 步顺序为 saveLanScope → saveEgress（不传 id，每次都是新 UUID 一行）→
   saveSource（归属/新建）→ createPlan。createPlan 在 freshness 不可用时返回 503，
   但前序草稿已落盘：Cmcc 每设备库
   /opt/rosboard/data/devices/1d0eaf78-…-3968e142b4203a4b.db（只读 sqlite 核对）：
   - 出口 9d56ef75-cafa-4cc0-9623-bbd5e28c402f「代理」dedicated/manual_proxy_lab dns 1.1.1.1 strict enabled=1 revision=2
     IPv4→pppoe-out1 gw 10.0.2.1；已绑定来源 Youtube(939152d8, url, enabled, rev2)
   - 出口 fc3ba825-6db4-42d0-a05b-a45c93a772ac「代理」同样式 enabled=0 revision=2，无来源绑定
   - policy_device_state: ownership=unconfigured, last_scan_at=0, health=degraded, drift=drifted
   - policy_plans/jobs 均为空（无任何 RouterOS 变更/备份/apply；两条都是 desired-state 草稿）
   - audit（按时间升序）：access.provision → source.save(Youtube) → lan_scope.update →
     egress.save 9d56ef75 → source.save Youtube→9d56 → egress.save fc3ba825 → egress.save fc3ba825 → egress.save 9d56ef75
     （全部来自 10.0.0.86）
5) 两条出口成因：每次向导 saveAndPlan 的 saveEgress 都不传 id → 服务端新建 UUID；
   两次向导尝试（t≈36s 间隔）产生两条草稿；之后对两条草稿各又编辑保存一次（revision 均到 2）。
   即：用户多次重试 + 向导每次创建新出口 id + discovery 失败导致 plan 永不生成 = 多条半成品草稿。
   处理建议（未执行）：恢复账号后由用户在 UI 中自行停用/删除多余草稿（本次不动）；不要在未恢复前反复点保存。
6) 未见 policy runtime/journal error 文本（服务日志只有启动与 monitor 取消扫描一行）；device config 绑定正常。

## 前端修复（仅 web，未部署未提交，未删改现有两条草稿）
文件：
- web/src/features/policy-routing/PolicyWizard.tsx
  - 新增 discoveryAvailable=discovery?.available；step5（差异与应用）在 discovery 为 null 或不 available 时 fail closed：
    stepError 返回明确文案使「保存草稿并生成差异预览」禁用，并显示错误墙 Notice「策略管理账号无效或 RouterOS 发现不可用」；
  - saveAndPlan 入口最先 fail closed：discovery 不可用时不保存 LAN/egress/source，直接报错返回（杜绝半成品/重复出口）；
  - LAN 字段：discovery 可用时改为明确 <select>（候选 + 显式「手动输入列表名…」兜底），不可用时仅保留输入框；
- web/src/features/policy-routing/PolicyEgresses.tsx
  - WanInterfaceField：discovery 可用时改为明确 <select>（含手工输入兜底），不可用时维持输入框；
域名列表独立保存、最终应用单次密码流程、单一归属语义均未改动。

验证：web npm run lint（oxlint）无警告；npm run build（tsc -b && vite build）通过；
go build ./... 通过（前端产物已嵌入 internal/ui/dist）。无前端单测脚本；后端行为未改动。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 68: Cmcc 域名列表语义改造 + 共享标记列表删除 bug 修复（本地实现）

**Date**: 2026-08-24
**Task**: Cmcc 域名列表语义改造 + 共享标记列表删除 bug 修复（本地实现）
**Branch**: `main`

### Summary

移除域名列表启停概念（物化资格=egressId+父策略有效状态，保存规范化 enabled=true）；未分配可删/已引用 409 fail-closed；删除 shared_list_in_use 门禁修复共用 manual_proxy_lab 无法删出口问题；编辑器改为保存并预览→确认保存两步流。go test/vet/race、npm lint/build/audit、gofmt、git diff --check 全绿。未部署未提交。

### Main Changes

# Session: Cmcc 域名列表语义改造 + 共享标记列表删除 bug 修复（本地实现，未部署未提交）

Date: 2026-08-24 · 本地代码修改与验证，未部署、未 commit、未触碰 RouterOS/remote config/SQLite、未改 AGENTS.md。

## 需求实现
1) 域名列表移除「启用/停用」概念：
   - 前端：删除编辑器「启用该列表」checkbox、列表表格「停用/启用」操作与 ToggleSourceEnabled 组件（改为 DeleteSourceDialog）；
     状态列改为派生语义「未分配（不参与应用）/已引用（待应用/已应用版本）/策略已停用（暂不参与应用）/待删除（需应用设置后完成）」。
   - 后端：savePolicySource 保存时把 source.Enabled 规范化为 true（wire/db 兼容，不再作为用户开关）；
     materializer 物化资格改为「egressId 非空 + 父 egress enabled 且未 pending-delete」，删除 source.Enabled skip；
     scheduler/health/source_counts 同步移除 enabled 分支（未分配 source 仍独立刷新版本，但永不物化/不参与 enqueue 计数）。
2) 域名列表删除：未分配可删、已引用拒绝（后端 409 source_referenced_by_egress，fail-closed）、保留两阶段 pending-delete/审计/版本安全与 revision 门禁；pending-delete 展示清晰状态。
3) 修复共享标记列表阻塞出口删除：删除 deletePolicyEgress 的 shared_list_in_use 门禁（共享列表是 DNS Static 动态内容，
   由 source→egress 引用决定，非独立 RouterOS 对象；删除无 source 的出口不可能移除共享列表内容；egress_referenced_by_sources 独立门禁保留）。
   回归：两出口共用 manual_proxy_lab 可各自删除；A 有 source 仍被 409 阻止；materializer 证明待删除出口规则清除、存活出口共享列表规则保留。
4) 新建/编辑保存交互：主按钮「保存并预览」首屏可点（基本字段有效时），点击自动 preview → 展示 PreviewResult → 主按钮变「确认保存」
   （带 previewId 保存）；preview 失败停表单可重试；编辑输入变化后 generation+signal 使旧 preview 失效重新预览；纯元数据修改直接保存不预览；
   notModified/无 previewId 保持 fail-closed；保留次要「重新预览」按钮。

## 修改文件
后端：internal/api/policy_lifecycle.go、internal/api/policy_routing.go、internal/policy/materializer.go、
     internal/policy/scheduler.go、internal/policy/health.go、internal/policy/source_counts.go
测试：internal/api/phase10_test.go、internal/policy/materializer_test.go、internal/store/policy_test.go
前端：web/src/features/policy-routing/{types.ts,api.ts,PolicySources.tsx,PolicySourcesPage.tsx,PolicyWizard.tsx,format.ts}

## 验证（真实结果）
- gofmt -l：无输出；go vet ./...：通过
- go test ./... -count=1：全包 ok（cmd/rosboard, api, auth, config, mosdns, policy, recognition, routeros, service, store）
- go test -race ./internal/{api,policy,store}/：ok
- npm --prefix web run lint：0 error/0 warning；npm --prefix web run build（tsc -b && vite build）：通过
- npm --prefix web run audit --audit-level=high：found 0 vulnerabilities
- git diff --check：通过；changed files trailing whitespace：无

## 未解决边界
- 未做 RouterOS 真实 plan 应用回归（无 fresh discovery fixture 只能到 materializer/API 层）；
  已静态证明 planner 从不生成 ip/firewall/address-list 操作，共享列表为 DNS Static 动态内容。
- Codex 派发时提到的向导「新列表内联创建」仍保留 step3 显式预览步骤（编辑器已按需求改造）；若需向导同步「保存后自动预览」可后续迭代。
- 网络侧行为未变更；部署与提交留待 Lead 验收。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 69: Pi 第二轮修复：Lead review 阻塞项（共享列表/planner/health 边界/向导自动预览/UX）

**Date**: 2026-08-24
**Task**: Pi 第二轮修复：Lead review 阻塞项（共享列表/planner/health 边界/向导自动预览/UX）
**Branch**: `main`

### Summary

A:前端出口删除去 shared blocker、移除 isPreviewMock 禁门；B:planner 移除 shared_address_list_identity_collision；C:新增 SourceMaterializationEligible 统一过滤 health/counts/enqueue；D:向导新建列表改为保存并预览→确认保存；E:编辑器按钮/文案与删除对话框修正。go test/race/vet、npm lint/build/audit、gofmt、git diff --check 全绿。未部署未提交。

### Main Changes

# Pi 修复汇报（第二轮）：Lead review 阻塞项修复（本地实现，未部署未提交）

## A. 前端出口删除（PolicyEgresses.tsx / api.ts）
- egressDeleteBlockers 只保留非 pending-delete source 引用阻止；删除「shared 列表影响其他出口」与「sharedWith 被共用」两个 blocker。
- DeleteEgressDialog 移除 isPreviewMock() 禁门，真实 UI 直接调用 deleteEgress（DELETE /egresses/{id}，Phase 10 已存在）；清理 api.ts 过时 mock-only 注释。
- 对话框新增信息性 Notice：「共享标记列表仍被其他出口使用，将保留」列出共享方，明确不会删除/停用共享列表。

## B. 计划器共享列表复用（planner.go / planner_test.go）
- desiredIdentityIssues 移除 shared_address_list_identity_collision 生成（shared 同名列表可被多出口复用），保留 routing_table / dns_forwarder / dedicated 真实 collision 门禁。
- 测试更新：同名 shared list 两个出口 + pending-delete/存活出口共用 manual_proxy_lab 均断言无该 blocker；routing table 冲突仍 blocked。
- Materializer 级回归（上轮）证明待删除出口规则清除、存活出口共享列表规则保留。

## C. 无引用不参与应用的 health/计数/调度边界（eligibility.go 统一规则）
- 新增 SourceMaterializationEligible(egresses, source)：egressId 非空 + 父 egress 存在且 enabled 且非 pending-delete。
- health.SourceFailureEvidenceFromRepository：未分配/父缺失/禁用/待删除的 source 不参与故障证据（连续失败不降级）。
- source_counts.SourceCountChanges：同样过滤（不参与 shrink/resource）。
- scheduler.tryEnqueueScheduledPlan：父 egress 无效时不 enqueue（仍可刷新存版本）。
- 测试：health 未分配/禁用/待删除/缺失父均不降级 + enabled 对照；store 计数禁用/待删除父不计数 + 对照；scheduler 父禁用/待删除不 enqueue 且对照 enqueue。

## D. 向导内联新建列表（PolicyWizard.tsx）
- step3 预览不再阻塞向导；step6 按钮在有新建未预览时为「保存并预览」：首次点击自动 preview（generation+signal 门禁保留），成功后在 step6 展示 PreviewResult，按钮变「保存草稿并生成差异预览」，二次点击才进入保存链（LAN/出口/列表含 previewId）；预览失败/零规则/无 previewId 均 fail-closed 不写任何草稿；向导 discovery fail-closed 与第六步一次密码流程不变，单归属约束不变。

## E. 编辑器 UX
- PolicySources.tsx 主按钮 previewing 时 disabled 且显示「正在解析…」，避免重复 preview 请求。
- PolicySourcesPage 空态文案改为「点击保存后自动解析预览，确认后保存」。
- DeleteSourceDialog 删除误导的「审计中取消」说明，保留真实的两阶段删除说明。

## 修改文件
后端：internal/policy/{eligibility.go(新),planner.go,planner_test.go,health.go,source_counts.go,scheduler.go,phase10_test.go} internal/store/policy_test.go
前端：web/src/features/policy-routing/{PolicyEgresses.tsx,api.ts,PolicyWizard.tsx,PolicySources.tsx,PolicySourcesPage.tsx}
（上轮已改文件本轮未回退）

## 验证（真实结果）
- gofmt -l：无输出（eligibility.go 经 gofmt -w）
- go test ./... -count=1：全包 ok
- go test -race ./internal/{api,policy,store}/：ok
- go vet ./...：通过
- npm run lint（oxlint）：0 warning；npm run build（tsc -b && vite build）：通过
- npm audit --audit-level=high：0 vulnerabilities
- git diff --check：通过；changed files trailing whitespace：无

## 未解决边界
- 未连 RouterOS、未部署、未提交；plan 应用级回归仍受 trace fixture 限制（planner 级已覆盖无 blocker + materializer 级证明共享列表保留）。
- 「删除出口」确认后仍走两阶段（pending-delete→计划应用），真实 RouterOS 清理需在验收环境生成计划核对。


### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 70: Pi 第三轮修复：Lead review 6 项真实问题（discovery 接通/门禁解耦/loader 统一过滤/identity 去 Enabled/删除带 revision/预览竞态）

**Date**: 2026-08-24
**Task**: Pi 第三轮修复：Lead review 6 项真实问题（discovery 接通/门禁解耦/loader 统一过滤/identity 去 Enabled/删除带 revision/预览竞态）
**Branch**: `main`

### Summary

1)策略设置页加载 discovery 并传 PolicyEgressSection；2)域名列表 CRUD 改由设备启用/归档决定只读，runtime 未就绪仅提示；3)materialization_loader 统一 SourceMaterializationEligible（缺失父也排除），保留 direct Materialize 输入校验；4)desired identity 移除 Enabled；5)deleteSource/deleteEgress 带 revision；6)预览 finally 按 gen/abort 门禁清理。新增回归全绿：go test ./...、-race、go vet、gofmt、npm lint/build/audit、git diff --check。未部署未提交。

### Main Changes

# Pi 第三轮修复汇报（Lead review 发现 6 项真实问题，全部修复 + 针对性回归）

## 1) 前端 policy 页 discovery 未接通（PolicyRoutingPage.tsx）
- 根因：`usePolicyDiscovery(props.deviceID, adoptionOpen)` 只在接管对话框打开时加载；PolicyEgressSection 恒传 `discovery={null}`。
- 修复：改为 `usePolicyDiscovery(props.deviceID, props.section === 'settings' || adoptionOpen)`，并把 `discovery={discovery.discovery}` 传给 PolicyEgressSection。hook 在 `active` 翻转为 true 时重新取数、离开或设备切换时 cleanup abort，无旧 discovery 残留；向导沿用自身 discovery（需要重试与 fail-closed 门禁），已用注释说明选择，避免不必要的重复请求。

## 2) 域名列表 CRUD 与策略运行时门禁解耦（PolicySourcesPage.tsx）
- 根因：`readOnly = !overview.access.enabled || overview.setup.state !== 'ready'` 把账号/runtime 未就绪错误放大为列表不可编辑。后端 save/preview/delete source 只依赖设备 store（resolvePolicyDevice 仅校验 device enabled/archived）。
- 修复：`hardReadOnly = !overview.device.enabled || overview.device.archived`（设备未启用/已归档才只读）；新增 `policyUnavailable` 提示“策略应用暂不可用：列表仍可保存（未分配草稿）与版本刷新，分配并应用后才参与 RouterOS 物化”。未放开出口/LAN/计划/应用设置任何门禁；未分配 egressId 为空时保存、编辑器不发送假 egressId（后端既有 API 测试覆盖）。

## 3) materialization_loader 统一过滤（materialization_loader.go + phase10_test.go）
- 根因：loader 只过滤空 egressId/pending-delete，source 指向缺失父 egress 会进入 Materialize 报 unknown egress，与第 2 轮 SourceMaterializationEligible 声明不一致。
- 修复：`LoadPolicyMaterialization` 改用 `source.PendingDelete || !SourceMaterializationEligible(desired.Egresses, source)`，未分配/父缺失/父禁用/父待删除一律不进入 DNS static 物化；direct Materialize 对未知父 egress 的报错保留为 fail-closed 输入校验（有注释说明，不吞错误）。
- 回归：新增 `TestLoadPolicyMaterializationEligibilityRegression`——有效父产生规则；未分配/禁用/待删除/缺失父均 0 CreateRules/0 KeepRules 且无 RouterOS create；direct Materialize 未知父仍报错。为此给 phase10Repository 补充 ManagerInstanceID/ListSourceRules/ListMaterializedRules/ListMaterializedReferences 与按 SourceID 过滤的 ListSourceVersions。

## 4) desired identity 移除 source.Enabled（repository_identity.go + phase10_test.go）
- 根因：sourceIdentity 包含 Enabled，历史 enabled=false 保存一次名称/计划/归属就制造与 RouterOS 无关的 identity/plan 失效。
- 修复：sourceIdentity 删除 Enabled 字段（遗留字段不再参与 desired hash/revision）。
- 回归：`TestDesiredSourceIdentityIgnoresEnabledFlag`——同一 source 仅 Enabled true/false 时 revision/hash 完全一致；改归属/递增 Revision/置 pending-delete 时 hash（及相应 revision）变化。

## 5) 删除请求带 revision stale 门禁（api.ts + 两处对话框）
- 根因：deleteSource/deleteEgress 不带 `?revision`，queryRevision=0 → 无条件标记。
- 修复：两个 API 函数新增 revision 参数并发 `?revision=<current>`；DeleteSourceDialog 传 source.revision、DeleteEgressDialog 传 egress.revision；后端既有 stale revision 语义保留（未降低门禁）。

## 6) 预览异步竞态（PolicySources.tsx / PolicyWizard.tsx）
- 根因：runPreview/runNewListPreview 的 finally 无条件 setPreviewing/setBusy，旧请求迟到写状态会清掉新请求 busy。
- 修复：finally 仅在 `!signal.aborted && gen === previewGenRef.current` 时清理；输入变化时 invalidatePreview 同步复位 previewing/busy 防止“卡在解析中”；saveAndPlan/regeneratePlan/submit 的 finally 增加 `if (!signal.aborted)` 门禁。已全量核对 await 后所有 setState（错误路径原有 gen/abort 门禁保留），「保存并预览→确认保存」流程不变。

## 修改文件
- 前端：web/src/features/policy-routing/{PolicyRoutingPage.tsx, PolicySourcesPage.tsx, PolicySources.tsx, PolicyEgresses.tsx, PolicyWizard.tsx, api.ts}
- 后端：internal/policy/{repository_identity.go, materialization_loader.go, phase10_test.go}

## 验证（真实结果）
- gofmt -w 涉及文件后 `gofmt -l internal`：无输出
- `go test ./... -count=1`：全包 ok（cmd/rosboard, api, auth, config, mosdns, policy, recognition, routeros, service, store）
- `go test -count=1 -race ./internal/policy ./internal/api ./internal/store`：ok
- `go vet ./...`：通过
- `npm --prefix web run lint`：0 error / 0 warning；`npm --prefix web run build`：通过；`npm --prefix web run audit --audit-level=high`：0 vulnerabilities
- `git diff --check`：通过；changed files trailing whitespace：无

## 未解决边界
- 未部署、未 commit、未连接/写 RouterOS、未改 AGENTS.md/远程 config/SQLite。
- discovery 可用性依赖真实 RouterOS（Cmcc 账号缺失时 WAN/LAN 候选仍为空，属预期 fail-closed；页面/向导会显示“策略管理账号无效或 RouterOS 发现不可用”提示）。
- 计划/应用级共享列表保留仍以 planner+materializer 级回归覆盖，RouterOS 真实验收时建议人工生成删除方案差异核对。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 71: Pi 第四轮修复：unmount finally setState 门禁 + 向导打开时页面 discovery 暂停

**Date**: 2026-08-24
**Task**: Pi 第四轮修复：unmount finally setState 门禁 + 向导打开时页面 discovery 暂停
**Branch**: `main`

### Summary

阻塞1：PolicySources/PolicyEgresses(3处)/PolicyReview(2处)/PolicyRoutingPage/PolicyAccessCard/ChangePlanView 全部 finally 的 setState 增加 !signal.aborted 门禁（脚本扫描 14 处 finally 均 gated，密码清理语义在非 abort 场景保持）；阻塞2：页面 usePolicyDiscovery active 改为 (settings && !wizardOpen)||adoptionOpen，向导打开时暂停页面发现避免重复请求。契约静态复核保持：Eligible 四路统一、identity 无 Enabled、delete 带 revision、列表 CRUD 门禁解耦、无启停概念/无 shared_list_in_use。lint/build/audit、go test ./...、-race、vet、gofmt、diff --check 全绿。未部署未提交。

### Main Changes

# Pi 第四轮修复汇报（Lead review 2 项阻塞 + 契约静态复核，全部通过）

## 阻塞 1：unmount/stale 后 finally 仍 setState（已逐一修复）
按 useMutationScope 语义统一：每个 finally 的 React state setter 必须在 `!signal.aborted` 时才执行；abort（组件卸载/设备切换）后不写旧组件/父组件 state；abort 场景不做密码清理（组件已销毁），非 abort 场景维持原密码清理语义。
- PolicySources.tsx：DeleteSourceDialog.submit finally → `if (!signal.aborted) setBusy(false)`
- PolicyEgresses.tsx：PolicyEgressEditor.submit、ToggleEgressEnabled.submit、DeleteEgressDialog.submit 三个 finally → `if (!signal.aborted) setBusy(false)`
- PolicyReview.tsx：PolicyDriftCard.createDriftPlan、PolicyAdoptionDialog.preview 两个 finally → `if (!signal.aborted) setBusy(false)`
- PolicyRoutingPage.tsx：generateStructuralPlan finally → `if (!signal.aborted) setPlanBusy(false)`（regenerateDriftPlan 本身无 finally setState，其 setError/setPlanView 已有 abort 门禁）
- PolicyAccessCard.tsx：run 的 finally → `if (!signal.aborted) { setSessionPassword(''); setManualPassword(''); setManualAdminPassword(''); setBusy(false) }`
- ChangePlanView.tsx：submit 的 finally → `if (!signal.aborted) { setAdminPassword(''); setSubmitting(false) }`
- 静态回归证据（无测试框架，用脚本对 feature 目录全部 finally 块扫描 setState）：14 处含 React setter 的 finally 全部已带 `signal.aborted`/generation 门禁；components.tsx 的 finally 仅做 DOM cleanup（textarea.remove/focus），无 setState。
- 全量核对 await 后 setState/父回调：所有 catch 与成功分支原有的 abort 门禁保留（props.onSaved/onClose/onDone/onChanged/onApplied 均在 if (signal.aborted) return 之后）。

## 阻塞 2：向导打开时页面 discovery 重复请求（PolicyRoutingPage.tsx）
- 根因：`usePolicyDiscovery(deviceID, section==='settings' || adoptionOpen)` 在向导打开时仍保持 active，同时 PolicyWizard 自己也 usePolicyDiscovery(deviceID, true) → 双重取数，与注释“不重复”矛盾。
- 修复：active 改为 `(props.section === 'settings' && !wizardOpen) || adoptionOpen`——向导打开时页面 discovery 暂停（hook cleanup abort 在途请求）并让出给向导自己的 discovery/retry/fail-closed；关闭向导或回到设置页时 active 重新为 true，hook 重新加载（先置 loading 再取数）。接管对话框继续复用页面 discovery。
- 注释同步更新说明请求取消/重载行为。依赖数组已含 wizardOpen（组件每次渲染计算 active，hook deps 为 [deviceID, active, nonce]，切换正确触发 effect 重跑与 abort）。

## 上一轮契约静态复核（全部保持）
- SourceMaterializationEligible 统一覆盖 health/source_counts/scheduler/materialization_loader：未分配、父缺失、disabled、pending-delete 一律不参与物化/健康/计数/enqueue；direct Materialize 对未知父仍 fail-closed（有注释明确是输入校验）。
- source desired identity 不含 Enabled（sourceIdentity 已删字段；有回归测试 TestDesiredSourceIdentityIgnoresEnabledFlag）。
- deleteSource/deleteEgress 继续发送 `?revision=<current>`。
- 域名列表 runtime 未就绪时 CRUD 仍可用（hardReadOnly 只由设备 enabled/archived 决定），仅显示“策略应用暂不可用”提示；未恢复启停概念；未恢复 shared_list_in_use 删除 blocker（生产代码 grep 无 shared_list_in_use / shared_address_list_identity_collision）。

## 修改文件（本轮）
web/src/features/policy-routing/{PolicySources.tsx, PolicyEgresses.tsx, PolicyReview.tsx, PolicyRoutingPage.tsx, PolicyAccessCard.tsx, ChangePlanView.tsx}
（后端无改动）

## 验证（真实结果）
- gofmt -l internal：无输出（本轮无 Go 改动）
- go test ./... -count=1：全包 ok（cmd/rosboard, api, auth, config, mosdns, policy, recognition, routeros, service, store）
- go test -count=1 -race ./internal/policy ./internal/api ./internal/store：ok
- go vet ./...：通过
- npm --prefix web run lint：0 error / 0 warning
- npm --prefix web run build：通过
- npm --prefix web run audit --audit-level=high：0 vulnerabilities
- git diff --check：通过；changed files trailing whitespace：无

## 未解决边界
- 未部署、未 commit、未连接/修改 RouterOS 或远程 config/SQLite、未改 AGENTS.md。
- discovery 页内暂停依赖 hook 的 active 切换（effect 重跑 + abort），无测试框架仅静态/类型复核；真实 UI 行为建议验收时人工确认向导开合期间的请求数（预期向导内 1 次、页面暂停）。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 72: Phase 12 修订实现：域名列表立即删除/账号维护流/刷新体验/LAN 候选多选/显式下一跳/策略状态

**Date**: 2026-08-24
**Task**: Phase 12 修订实现：域名列表立即删除/账号维护流/刷新体验/LAN 候选多选/显式下一跳/策略状态
**Branch**: `main`

### Summary

P1 未分配 source 立即删除（revision CAS+审计，无 pending 假态）；P2 账号新增/编辑/删除流（cleanup 脚本端点，不执行 /user）；P3 账号确认后即时反馈+短轮询；P4 discovery LAN 候选（list/bridge/真实接口+证据，内置排除）+向导多选入组合列表；P5 显式下一跳（planner 跳过 WAN 证明、p2p 自动、API IP/族校验、前端警告与路径）；P6 egress applied 证据与待应用/已应用标签；P7 DNS 冻结遵守；P8 全绿灯。完整的 go test ./.../race/vet/gofmt、npm lint/build/audit、git diff --check 通过。未部署未提交未触 RouterOS。边界已如实列出。

### Main Changes

# Rosboard Phase 12 修订实现汇报（Pi 执行 agent，本地实现，未部署未提交）

## 实现范围（对照用户逐项确认需求）
### 一、域名列表删除（已完成）
- 后端 `deletePolicySource` 改为 Phase 12 立即删除契约：未分配（egressId 空）且**无已应用物化规则**的 source 直接物理删除（新 store `DeleteSourceWithRevision`：原子事务、device 隔离、revision CAS、审计），**不再产生 pending-delete 假状态**；未分配但仍有 materialized 规则（曾应用后未应用清理）→ 409 `source_has_applied_rules`（先应用清理计划再删，防 RouterOS 孤儿）；被策略引用 → 409 `source_referenced_by_egress`（原有，未绕过后端门禁）。
- store 复用原有 `DeleteSource` 安全语义（引用级联 + 无引用规则清理 + 审计同事务），抽公共 tx 实现。
- 前端：删除只弹小型确认框（未分配=立即删除说明，pending 遗留=待删除说明），成功/失败反馈、键盘/焦点沿用 Modal 契约；表格操作按 pending/引用/未引用三态控制。
- 回归：store `TestPolicySourceImmediateDeleteWithRevision`（CAS/隔离/已分配拒绝/审计）；API `TestPolicySourceDeleteIntentAndRevisionGuard` 改断言立即删除+gone+audit；`TestPolicyUndeleteEndpointsCancelDeleteIntent` 改为 egress 两阶段+未分配删除后 undelete 404；`TestPolicySourceDeleteRejectsAppliedRules`（materialized 规则 409）。
- 边界：被引用删除按钮为禁用+title 提示，引用策略名称显示在表格「归属」列；删除确认框不单独回显策略名。

### 二、策略管理账号（已完成）
- 新增路径保持：脚本路径（生成最小权限脚本→Winbox 执行→确认）+ 手动路径（Verify + WriteProbe，`verifyPolicyCredentials` 既有）。
- 编辑当前账号：`更换账号` 预填用户名；手动保存先验证新凭据/写能力成功后才 `saveSettings` 替换，失败不破坏旧配置；密码不落日志/响应/localStorage。
- 删除当前账号：新端点 `POST /api/policy-routing/access/cleanup`（只读，无 step-up）返回当前账号清理信息——托管账号 → 清理脚本（`provisioningCleanupScript` 复用，含组占用保护说明），**rosboard 不执行 /user 删除**；手动账号 → `managed:false` 明确“仅清除本地连接信息，RouterOS 用户不被删除”。确认动作复用既有 `PUT /access {enabled:false}`（step-up + 审计 access.update + 运行时吊销）。
- 前端 PolicyAccessCard 重写：overview / script / manual / remove 四态；删除流程（脚本复制按钮只复制脚本不泄密；手动路径说明）；编辑预填；未延长 mutation allowlist（未向 /user 加任何写路径）。
- 回归：`TestPolicyAccessCleanupForDeviceContract`（托管/手动/未配置）、`TestPolicyAccessCleanupEndpointAndEgressAppliedEvidence`（端点 + 审计）。

### 三、账号确认后的刷新体验（已完成）
- 脚本确认/手动验证成功后立即显示“账号已连接，正在后台刷新策略数据…”；随后 ~5 秒内短周期（1s×5，可取消、signal 保护）轮询 overview；超时态显示“账号已保存，后台刷新仍在进行，可稍后手动刷新”+「立即刷新」入口；已成功账号不会因轮询未完成显示为失败。所有 await/finally 用 mutation signal 门禁（含清理脚本加载/删除确认）。

### 四、LAN 范围候选和多选（已完成主体，见边界）
- 后端：新增 `interface/bridge/port` 允许读菜单（`ReadMenuBridgePort` + proplist + alwaysReadMenus），扫描进 `TopologyInput.BridgePorts`；discovery `lan` 投影重做为三类候选：自定义 interface list（kind=list，排除内置）、bridge（kind=bridge，含 bridge-port 成员、L3 地址证据、reason）、真实接口（kind=interface，ether 等非桥/非点对点/非 vlan，含地址证据）；内置 all/dynamic/none/static 移到 `builtins` 诊断数组，不再作为候选。
- 前端向导 LAN 步改为分组多选：列表单选用已确认成员；bridge/接口多选（选择 bridge 即整体纳入其端口）；选择值/地址/原因证据展示；手动输入兜底保留且“未探测”不伪造候选；保存映射：多选原始候选 → rosboard 托管组合列表 `policy-lan` 写入成员（进 planner 的 in-interface-list 入口匹配）；选列表则复用其成员。
- 回归：`TestPolicyDiscoveryProjectionLANCandidates`（列表/bridge/端口/地址/内置诊断/点对点排除/容器 bridge 独立可见）、`TestPolicyListBridgePortMenu`（只读菜单路径）。
- 边界：当前 planner 契约为**单一有效入口列表**；多选被并集到一个有效列表（复用选中列表或托管 `policy-lan`），**多个独立 interface-list 作为多路独立入口过滤器未建模**（照实现安全约束，未伪造）。容器 bridge 与普通 bridge 均单独可见，由用户结合 reason/端口证据判断。组合列表的 planner 级回归未新增（列表/成员创建路径由既有测试覆盖）。

### 五、显式下一跳网关（已完成）
- 不新增出口类型；每地址族可选网关：留空=自动（点对点接口自动走接口路由；普通 WAN 走发现默认路径；无法证明时给出可操作错误含地址族与修复指引）；填写=显式网关静态默认路由（跳过 WAN 证明/topology_proof_missing/wan_source_unknown，仅校验接口存在——无 Phase5 证明且发现里有接口证据时）。
- 点对点（pppoe/wireguard）空 gateway 不再触发 `desired_route_incomplete`（`snapshotInterfaceIsPointToPoint` 自动识别）；typed（pppoe/dhcp）策略不被 gateway 字段覆盖。
- API `savePolicyEgress` 网关校验：必须是单 IP、地址族匹配（IPv4/IPv6），否则 400 `invalid_gateway`。
- 前端：高级区网关输入提示更新 + 显式下一跳警告（旁路由回程/环路风险，非阻塞）；向导确认步显示“策略接口 → 下一跳 → RouterOS 主路由”路径线。
- 回归：`TestBuildChangePlanExplicitNextHopGateway`（LAN 接口+显式网关不产生 wan_source_unknown/topology_proof_missing/desired_route_incomplete 且生成 gateway=...@main 路由；pppoe 空网关生成接口路由；非点对点空网关仍 blocker）；`TestPolicyEgressGatewayValidation`（API 校验）。
- 边界：WAN 接口下拉候选仍为发现的 proven WAN；非 WAN（如对接旁路由的 LAN 接口）通过既有手动输入兜底选择（planner 已不拦截），发现接口全量列表未加入该下拉（记为待办，UI 不算阻断）。

### 六、阻止计划与策略状态（已完成主体）
- 后端 overview 增加 egress `applied` 证据：仅当该 egress 拥有 materialized RouterOS 规则（终端提交事务才持久化）才为 true；desired=enabled 但从未应用 → false。
- 前端出口状态改为派生：待删除 / 已应用 / 待应用 / 已停用（期望），不再把 desired enabled 当“已启用/已生效”。
- 向导最终应用成功已回调关闭流程并刷新 overview（既有）；blocked/cancel 保留已保存草稿并可重新编辑/预览。
- 回归：`TestPolicyAccessCleanupEndpointAndEgressAppliedEvidence` 验证 draft egress applied=false、写入 materialized 规则后 applied=true。
- 边界：per-job/blocker→egress 的直接映射未实现（有 blocker 时状态仍显示待应用而非“应用被阻止”细节；计划错误在 ChangePlanView 中呈现）。

### 七、DNS 范围冻结（遵守）
未做任何 DoH/DoT/TProxy/全流量/旁路由 DNS 逻辑改动。

### 八、UI/UX（遵守）
沿用现有壳层/semantic tokens/Modal/Field/Notice/StatusBadge/CopyButton，无 emoji、无裸 hex、无新视觉体系；44px 触控与焦点沿用现有控件；全 feature 的 finally setState 均有 signal 门禁（含本轮新代码）。

## 修改文件
- 后端：internal/store/policy.go（DeleteSourceWithRevision + tx 抽取）、internal/policy/repository.go、internal/api/policy_lifecycle.go、internal/api/policy_routing.go（access/cleanup 端点、applied 投影、网关校验、discovery builtins）、internal/api/policy_review.go（LAN 候选投影）、internal/api/provisioning.go（policyAccessCleanupForDevice）、internal/policy/{intents.go,topology.go,scanner.go}、internal/routeros/policy_types.go（ReadMenuBridgePort）
- 前端：web/src/features/policy-routing/{PolicyAccessCard.tsx（重写）,PolicyWizard.tsx,PolicyEgresses.tsx,PolicySources.tsx,PolicySourcesPage.tsx,types.ts,api.ts}
- 测试：internal/api/{phase10_test.go,phase12_source_delete_test.go,phase12_gateway_test.go,phase12_account_status_test.go,phase12_lan_candidates_test.go}、internal/store/policy_test.go、internal/policy/{planner_test.go,materializer_test.go,phase10_test.go,eligibility.go(既有)}、internal/routeros/policy_read_test.go

## 验证（真实结果）
- gofmt -l：无输出
- go test ./... -count=1：全包 ok（cmd/rosboard, api, auth, config, mosdns, policy, recognition, routeros, service, store）
- go test -count=1 -race ./internal/{policy,store,api,auth,config,routeros,service} ./cmd/rosboard：ok
- go vet ./...：通过
- npm --prefix web run lint：0 error / 0 warning；npm --prefix web run build：通过；npm --prefix web audit --audit-level=high：0 vulnerabilities
- git diff --check：通过；changed files trailing whitespace：无

## 阻塞与安全边界（未伪造）
1. LAN 多路独立 ingress（多个不同 interface-list 各自生效）与当前 planner 单列表契约冲突，按安全约束实现为“并集到一个有效列表”，未绕过；需新 planner 模型方支持多列表（Phase 13 范围）。
2. 容器/普通 bridge 只能由用户结合证据判断（无 RouterOS 侧“container”语义可靠标记，未猜测）。
3. WAN 接口下拉未包含全部发现接口（手动输入可用）；发现接口全量进下拉列为后续小迭代。
4. 未部署、未 commit、未连接/修改 RouterOS 或远程 config/SQLite、未改 AGENTS.md、未清理 10.0.0.99 遗留 probe（本轮未连接 10.0.0.99）。
5. 组合列表/向导多选→计划应用未做真机端到端验证（预留用户验收）。
6. 有 blocker 时 UI 仍显示“待应用”而非逐 egress“应用被阻止”（错误细节在 ChangePlanView 呈现）。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 73: Phase 12 review 2 修复：立即删除覆盖遗留 pending/TOCTOU/applied 一致性/LAN 候选可理解性/清理 ack/成功反馈

**Date**: 2026-08-24
**Task**: Phase 12 review 2 修复：立即删除覆盖遗留 pending/TOCTOU/applied 一致性/LAN 候选可理解性/清理 ack/成功反馈
**Branch**: `main`

### Summary

1)未分配立即删除不再被旧 pending-delete 卡死（API+store 回归）；2)store 事务内重查 materialized refs（ErrSourceHasAppliedRules 哨兵，API 409，TOCTOU 防护）；3)applied=当前期望物化无 delta（EgressAppliedEvidence，改期望/分离即 false）；4)LAN 候选排除 lo/loopback，UI 改“策略接口”，组合列表 policy-lan planner 级回归（列表+成员+in-interface-list 引用）；5)托管清理确认加 checkbox ack；6)成功消息升级 Notice 且立即显示。go test ./.../race/vet/gofmt、npm lint/build/audit、git diff --check 全绿。未部署未提交未触 RouterOS。

### Main Changes

# Phase 12 修订 · Lead review 阻塞项修复汇报（第二轮，Pi 执行，未部署未提交）

## 1) 未分配立即删除覆盖遗留 pending-delete 状态（已完成）
- 根因：API `deletePolicySource` 先遇 `PendingDelete` 返回 `source_pending_delete`，旧未分配待删除记录无法立即删除。
- 修复：API 只保留「已分配 → 409 source_referenced_by_egress」前置；未分配一律走 store 物理删除，store 内不再因 pending_delete 拒绝（注释说明：无 RouterOS 对象的未分配行其 pending 标记无意义）。
- 回归：`TestPolicyDeleteSourceLegacyPendingDeleteUnblocksImmediateDelete`（store）、`TestPolicySourceDeleteLegacyPendingDeleteEndpoint`（API：先置 pending-delete 再 DELETE → 200 deleted，行消失）。

## 2) store 事务内 TOCTOU 防护（已完成）
- 根因：API 先 `ListMaterializedReferences` 再 store；并发 apply 在检查后插 ref 会被物理删除而无清理计划。
- 修复：`DeleteSourceWithRevision` 在同一事务内（device/source/revision 作用域）重新 `COUNT(policy_materialized_refs)`，>0 时返回新哨兵 `policy.ErrSourceHasAppliedRules`，API 映射 409 `source_has_applied_rules`；**不再依赖 API 外层预检查**（API 侧 refs 预检已删除）。设备隔离与 revision CAS 保留。
- 回归：`TestPolicyDeleteSourceWithRevisionTOCTOUGuard`（直接 repository 调用带 refs 也拒绝，source+refs 保留）；`TestPolicySourceDeleteRejectsAppliedRules`（API 409）仍通过。

## 3) applied 证据与当前期望一致（已完成）
- 根因：旧逻辑按 egressID 计数 materialized rule，改期望但计划 blocked/cancelled 仍显示已应用。
- 修复：新增 `policy.EgressAppliedEvidence`（`internal/policy/applied.go`）：基于 `LoadPolicyMaterialization` 当前期望 delta——egress 有 Create/Update/Delete delta（或期望无物化内容）即非 applied；修改期望/分离 source（旧 rule 行仍在）立即置 false；pending-delete/禁用 egress 不参与。overview 接入（managerInstanceID 由 store 提供；失败保守置 false）。
- 回归：`TestPolicyOverviewEgressAppliedRequiresCurrentDesired`——种子终端提交一致的 rule（含确定性 ownership comment 与 AddressList 等 wire 全字段）→ applied=true；detach source 后旧 rule 行仍在 → applied=false。

## 4) LAN / 策略接口候选可理解性（已完成）
- 4a loopback：LAN direct candidates 排除 loopback 类型与 `lo` 名称；测试加入 `lo` 断言不出现。
- 4b UI 文案：WAN 字段改名「策略接口」，hint 说明显式下一跳可选连接旁路由的 LAN/桥接口（候选下拉展示名称+类型/运行状态，手动输入兜底保留；显式下一跳旁路由回环警告保留）。
- 4c 组合列表 planner 回归：`TestBuildChangePlanCompositePolicyLANIngress`——多原始选择（bridge+ether）无列表选择时生成**一个**托管入口列表 `policy:lan-list:policy-lan` + 每个选择的 `policy:lan-member` 操作，且 mangle mark-connection/mark-routing 操作 `in-interface-list=policy-lan` 真被策略入口引用；不会变成多个独立策略。

## 5) 账号清理的人为确认门槛（已完成）
- 托管账号清理确认增加明确 checkbox「我已在 RouterOS 手工执行上述清理脚本（rosboard 不会代替执行 /user 删除）」；未勾选（且 cleaned managed 时）确认按钮 disabled；确认仍走 step-up `PUT /access {enabled:false}` 仅清除 rosboard 保存凭据。手动/非 managed 路径不要求 ack、只清除本地凭据、明确说明 RouterOS 用户不删除。服务端边界保持：cleanup 端点只返回脚本，不执行 /user（未扩展 mutation allowlist）。
- 静态核对：PolicyAccessCard remove 分支的按钮 disabled 条件含 `cleanup?.managed && !cleanupAcked`；进入 remove 时 ack/密码重置。

## 6) 账号连接成功反馈醒目且快速（已完成）
- run 成功即（不等待轮询）设置消息「账号已连接，正在后台刷新策略数据…」；随后 ≤5s（1s×5）可取消轮询；超时文案「账号已保存，后台刷新仍在进行，可稍后手动刷新」；消息升级为 `Notice`（good/neutral）醒目区块 +「立即刷新」入口；真实定时路径：beginRefreshFeedback 在成功回调后立即 setMessage，首 tick 1s 后才调 onChanged，5 tick 后停在最终态；abort（卸载/设备切换）即停。
- 手动/非 managed 移除路径文案保持不变（只清本地凭据）。

## 修改文件（本轮）
- 后端：`internal/policy/{applied.go(新),repository.go}`、`internal/store/policy.go`、`internal/api/{policy_lifecycle.go,policy_routing.go,policy_review.go}`、测试 `internal/api/phase12_{source_delete,account_status,lan_candidates}_test.go`、`internal/store/policy_test.go`、`internal/policy/planner_test.go`
- 前端：`web/src/features/policy-routing/{PolicyAccessCard.tsx,PolicyEgresses.tsx}`（文案/ack/Notice）

## 验证（真实结果）
- gofmt -l：无输出；go vet ./...：通过
- go test ./... -count=1：全 10 包 ok
- go test -count=1 -race ./internal/{policy,store,api,auth,config,routeros,service} ./cmd/rosboard：全 ok
- npm --prefix web run lint：0 error/0 warning；build：通过；audit --audit-level=high：0 vulnerabilities
- git diff --check：通过；changed files trailing whitespace：无

## 遗留边界（如实）
1. LAN 多路独立 interface-list 作为独立入口过滤器仍与 planner 单入口契约冲突（并集实现，未伪装）；`policy-lan` 为 rosboard 托管组合列表名，若 RouterOS 已存在同名且非 rosboard 拥有，planner 现有 lan_interface_list_foreign/ambiguous 门禁会阻止（安全保留）。
2. WAN/策略接口下拉仍以发现 proven 接口为主；非 WAN 接口手动输入可用（显式下一跳路径已不拦截）。
3. blocked 计划时 UI 状态为「待应用」（详细 blocker 在 ChangePlanView 呈现），未做逐出口「应用被阻止」文案。
4. 未部署、未 commit、未连接/写 RouterOS、未改 AGENTS.md、未清理 10.0.0.99 遗留 probe（未连接）。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 74: Phase 12 review2 阻塞修复（第三轮）：legacy pending 前端删除/applied 全量期望/投影准确性/清理门禁/托管保留/stale 回调

**Date**: 2026-08-24
**Task**: Phase 12 review2 阻塞修复（第三轮）：legacy pending 前端删除/applied 全量期望/投影准确性/清理门禁/托管保留/stale 回调
**Branch**: `main`

### Summary

A)未分配含 legacy pending 前端可立即删除+文案；B)新增 policy_commit_state 终端提交绑定，applied=当前完整 desired identity 比对（仅 LAN scope 变化即 false，DNS 行保留不误报）；C)投影 running 用真实证据、wans/lo 排除、direct 未证明标注；D)cleanup 未加载禁用确认+重试；E)同名编辑保留 ManagedAccount（API 回归）；F)then 回调 signal 门禁+删除反馈文案。全门禁绿：go test ./...、-race、vet、gofmt、npm lint/build/audit、git diff --check。未部署未提交未触 RouterOS。

### Main Changes

# Phase 12 review2 阻塞修复汇报（第三轮，Pi 执行，未部署未提交）

## A. legacy pending-delete 前端可立即删除（已完成，仅前端）
- PolicySourcesPage：删除按钮条件改为「未分配（含 legacy pendingDeletion）→ 可删；已分配 → 禁用+title」；派生状态：未分配+legacy pending → “未分配（可立即删除）”，已分配+pending → “待删除（需应用设置后完成）”。
- DeleteSourceDialog 文案改为：未分配 → “立即删除 rosboard 定义与历史版本，不写 RouterOS、不要求应用设置；若仍有曾应用规则后端会明确拒绝并要求先应用清理计划”；已分配 → 明确被阻止文案。不再保留误导性“等待应用清理计划”。
- 静态核对：rows 删除按钮与 dialog 文案同步；后端测试（store/API）保留。

## B. applied 覆盖完整策略期望（已完成，核心契约变更）
- 新增 durable 终端提交绑定 `policy_commit_state(device_id, desired_revision, desired_hash, committed_at)`：仅 SUCCESSFUL terminal commit（committed/committed_partial，带业务验证）在**同一事务**内写该记录（`PreparePolicyExecutionCommit` 扩展字段 `CommittedDesiredRevision/Hash/CommittedAt`，executor 从 plan 填）。rollback/cancelled/partial-无验证 不写。
- `EgressAppliedEvidence` 重写：取 committed 记录，与**当前完整 desired identity**（`RepositoryDesiredIdentity`，覆盖策略接口/下一跳/路由表/优先级/断线/NAT/RouterOS output/LAN scope/source 绑定/删除意图）逐 revision+hash 比对；不一致/记录缺失/错误一律 false（待应用，fail closed）。DNS 物化行存在与否不再是判定依据。
- 回归：`TestPolicyOverviewEgressAppliedRequiresCurrentDesired`——种子 committed=当前完整 hash → true；**仅改 LAN scope（不动 DNS 行）→ false；恢复同 hash → true**；detach source（旧 DNS 行保留）→ false。store 执行提交测试同步补齐 committed 字段（4 处 commit 字面量 + phase7Repository 记录语义）。

## C. LAN/策略接口投影准确性（已完成）
- bridge/direct 候选 `running` 改为真实 interface Running/Disabled 证据（不再写死 true）；测试含 running=false 断言。
- wans（策略接口）候选排除 loopback/lo（`isLoopbackCandidate`），保留 ether/pppoe/bridge；测试断言 lo 不出现且合法候选保留。
- direct 接口 reason 改为“直接接口（未证明 LAN/WAN，请结合地址与拓扑确认）”（+有 L3 地址说明），不伪造为 LAN。

## D. 账号清理 fail-closed 等待 cleanup 结果（已完成）
- PolicyAccessCard 增加 `cleanupLoaded`：fetchPolicyAccessCleanup 未成功前（含失败重试态）「确认移除账号」disabled；提供「重试获取清理信息」与「返回」；managed 仍需 checkbox ack；manual 路径 loaded 后即可确认（只清本地凭据）。失败经 ErrorNotice 展示。

## E. 托管账号同名编辑保留托管身份（已完成，后端）
- servePolicyAccess enabled 分支：新用户名 == 现有 ManagedAccount.Username 且验证成功 → **保留 ManagedAccount（组名不变）**；换名/改为普通账号才清 managed。
- 回归：`TestPolicyAccessManagedIdentityPreservedOnSameNameEdit`（同名改密走 Verify+WriteProbe 成功 → ManagedAccount 保留；换名 → 清除）。

## F. 账号操作 stale callback 与删除反馈（已完成）
- completeSession/saveManual/removeAccount 的 `.then` 回调增加 signal.aborted/代际门禁（run 前捕获独立 signal，aborted 不 setState/不触发父回调）。
- 删除成功文案改为「账号已移除，正在刷新策略数据…」（不再显示“账号已连接”）；刷新仍 ≤5s 可取消 + 超时「可稍后手动刷新」+「立即刷新」；beginRefreshFeedback 首消息参数化。

## 修改文件（本轮）
- 后端：`internal/policy/{repository.go(CommittedDesiredState+接口), executor.go, applied.go(重写)}`、`internal/store/policy.go(policy_commit_state+提交写+读写方法)`、`internal/api/{policy_routing.go(managed 保留, overview 接入不变), policy_review.go(投影准确性)}`；测试 `internal/store/policy_test.go`、`internal/policy/{phase7_test.go, planner_test.go}`、`internal/api/phase12_{account_status,lan_candidates}_test.go`
- 前端：`web/src/features/policy-routing/{PolicySourcesPage.tsx, PolicySources.tsx, PolicyAccessCard.tsx}`

## 验证（真实结果）
- gofmt -l：无输出；go vet ./...：通过
- go test ./... -count=1：全 10 包 ok
- go test -count=1 -race ./internal/{policy,store,api,auth,config,routeros,service} ./cmd/rosboard：全 ok
- npm --prefix web run lint：0/0；build：通过；audit：0 vulnerabilities
- git diff --check：通过；trailing whitespace：无

## 遗留边界（如实）
1. applied 证据按设备级完整 desired hash 判定；per-egress 仅按 enabled/非 pending 过滤（与设备一致视为已应用）。首次部署前无 committed 记录 → 全部“待应用”（fail closed）。
2. LAN 多独立 interface-list 入口仍未建模（并集到单一有效列表）；`policy-lan` 冲突仍由 planner 现有 foreign/ambiguous 门禁保护。
3. 策略接口下拉仍以 proven 发现候选为主；非 WAN 手动输入可用。
4. 未部署、未 commit、未连接/写 RouterOS、未改 AGENTS.md、未清理 10.0.0.99 遗留 probe（未连接）。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 75: Phase 12 review3 最后阻塞修复（第四轮）：partial 不写 applied 证据/删除标题动态/遗留 pending 编辑死路

**Date**: 2026-08-24
**Task**: Phase 12 review3 最后阻塞修复（第四轮）：partial 不写 applied 证据/删除标题动态/遗留 pending 编辑死路
**Branch**: `main`

### Summary

1)仅完整 JobStateCommitted 在事务内写 policy_commit_state，committed_partial/rollback/cancel 不写不覆盖；新增 TestPolicyCommitStateNotOverwrittenByPartial（partial 后记录仍为上次 full，全新设备无记录）；2)Notice 标题按操作动态（连接/移除/状态已更新）；3)legacy pending 未分配编辑按钮 disabled+说明，删除对话框注释清理为立即删除语义。全门禁绿：go test ./...、-race、vet、gofmt、npm lint/build/audit、git diff --check。未部署未提交未触 RouterOS。

### Main Changes

# Phase 12 review3 最后阻塞修复汇报（第四轮，Pi 执行，未部署未提交）

## 1) committed_partial 不再写入全局“已应用”证据（已完成，后端 + 回归）
- 根因：`CommitPolicyExecution` 对 committed 与 committed_partial 都 upsert `policy_commit_state`；`EgressAppliedEvidence` 按设备级完整 hash 比对后把所有 enabled/non-pending egress 置 true → 一个 family 成功另一个失败（committed_partial）时失败出口也会显示“已应用”。
- 修复：**仅 `JobStateCommitted`（全部计划成功且通过现有终态验证）在同一事务写 `policy_commit_state`**；`committed_partial`（以及 rollback/cancelled）不写也不覆盖。overview 因此继续按**上一次完整 commit（或无记录 → 全“待应用”）**判定，绝不把 partial 当全设备 applied。
- 回归：`TestPolicyCommitStateNotOverwrittenByPartial`——先完整 commit(hash-full) → 记录存在；再 committed_partial(hash-partial，一成功组一失败组) → **记录仍为 hash-full（未被覆盖）**；全新设备无记录（present=false）。
- 说明：per-egress partial 失败集合未持久化，按最小安全语义以全局记录为准（partial 不提升任何出口状态），符合 fail-closed。

## 2) 删除账号后 Notice 标题动态（已完成，前端）
- 根因：message 文案已改但 Notice title 固定“账号已连接”，删除后标题错误。
- 修复：新增 `statusTitle` 状态，按操作类型设置——新增/编辑成功 → “账号已连接”；删除成功 → “账号已移除”；缺省兜底“账号状态已更新”。render 由 `statusTitle || '账号状态已更新'` 提供。
- 静态核对：completeSession/saveManual 传 '账号已连接'，removeAccount 传 '账号已移除'。

## 3) legacy unassigned pending-delete 编辑入口避免死路（已完成，前端）
- 根因：未分配 legacy pendingDeletion 可删但仍显示可编辑；后端 save source 对 pending-delete 返回 source_pending_delete，编辑器必然保存失败。
- 修复：表格“编辑”按钮在 `source.pendingDeletion` 时 **disabled**，title 明确“该记录已标记待删除，无法编辑；请先直接删除（未分配）或应用清理计划（已分配）”；未分配仍可立即删除。
- 同步清理 `DeleteSourceDialog.submit` 上过时的“两阶段生命周期意图”注释（改为立即删除 + 后端 source_has_applied_rules fail-closed 说明）。

## 修改文件（本轮）
- 后端：`internal/store/policy.go`（partial 不写 evidence 条件）；测试 `internal/store/policy_test.go`（新回归）
- 前端：`web/src/features/policy-routing/{PolicyAccessCard.tsx(标题动态), PolicySourcesPage.tsx(编辑禁用), PolicySources.tsx(注释清理)}`

## 验证（真实结果）
- gofmt -l：无输出；go vet ./...：通过
- go test ./... -count=1：全 10 包 ok
- go test -count=1 -race ./internal/{policy,store,api,auth,config,routeros,service} ./cmd/rosboard：全 ok
- npm --prefix web run lint：0/0；build 通过；audit：0 vulnerabilities
- git diff --check：通过；changed files trailing whitespace：无

## 遗留边界（如实）
1. per-egress partial 成功集合未持久化：committed_partial 不提升任何出口为 applied（全局记录不变），如需“部分出口已应用”的精确逐项展示需持久化成功 family/egress 集合（本轮按最小安全范围未做）。
2. LAN 多独立 interface-list 入口仍未建模（并集单列表实现，`policy-lan` 冲突由 planner 门禁保护）。
3. 未部署、未 commit、未连接/写 RouterOS、未改 AGENTS.md、未清理 10.0.0.99 遗留 probe（未连接）。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 76: Phase 12 DNS Static 能力探针实现：358 条 dns_static_capability blocker 根因修复

**Date**: 2026-08-24
**Task**: Phase 12 DNS Static 能力探针实现：358 条 dns_static_capability blocker 根因修复
**Branch**: `main`

### Summary

根因=scanner unknown + capabilityFromProbe 仅 supported 放行 + validateIntentCapability 每 intent 追加 blocker（179 规则=358 blocker）。实现：inert 唯一 /ip/dns/static FWD 探针（create-readback-delete-confirm-absent，fail-closed，ambiguous delete 不重试并携带唯一身份）；CapabilityMatrixProvider 单飞缓存（键=device identity+结构 fingerprint，失败不缓存为 supported，账号/runtime 生命周期失效）；预览注入证据、apply 复核、overview 只读缓存展示；DNSStatic 能力定义去 regexp 对齐实际 wire；前端同 code+reason 聚合成影响 N 个操作可展开。测试：探针生命周期/失败/既有对象不动、并发单飞/指纹重探/失败缓存、179 规则 358→0、apply 门禁、纯函数聚合。全门禁绿。未部署未 commit 未触 RouterOS。

### Main Changes

# Rosboard Phase 12：DNS Static 能力探针实现汇报（Pi，本地实现，未部署/未 commit/未连 RouterOS）

## 根因（与 Lead 定位一致）
- scanner `capabilityProbesFromSnapshot` 将 DNSStatic 等 APIStatus 置 unknown；`capabilityFromProbe` 仅 APIStatus=supported 才 support；planner `validateIntentCapability` 因而对每个 /ip/dns/static intent 追加 `dns_static_capability` blocker —— 179 条规则 = create+enable 358 个 intent = 358 条 blocker（实测现象）。
- 生产装配只有只读 Scanner + MutationClient，无生产能力 API probe；测试 fixture 才手工填 supported。

## 实现
### 1) 能力探针（internal/policy/capability_probe.go）
- `DNSCapabilityProber` 仅用现有 allowlist `MutationClient` surface（ip/dns/static 菜单），无任意 REST path、无 /execute、无用户管理。
- 每次运行生成唯一 token（6 字节随机 hex）：name=`rb-cap-<token>`、address-list=`rb-cap-list-<token>`、comment=`rosboard capability probe <token>`；forward-to=`192.0.2.53`（TEST-NET-1，disabled=yes 永不解析，不触发真实域名查询、不产生 address-list 条目）。
- 生命周期（fail-closed）：create(PUT) → 读回 SAME object（Get）逐项验证 type/forward-to/address-list/match-subdomain/disabled/comment/.id/name → delete → confirm-absent（独立 List + name filter 检查 .id/name）；任一步失败即 unknown（绝不 supported）；delete 返回 ambiguous 时不重试（伪造测试证明仅 1 次 DELETE），`CapabilityProbeError` 携带唯一 probe 身份（name/address-list/.id/stage）供审计/人工清理；只操作本 probe 对象，不触碰既有对象（测试覆盖）。
- 探针使用已验证的策略管理账号，不需要管理员密码。

### 2) 能力证据生命周期（internal/policy/capability_matrix.go + manager.go）
- `CapabilityMatrixProvider`：mutex 单飞串行（并发预览只跑一次探针，测试证明）；缓存键 = DeviceIdentityFromSnapshot + 结构 fingerprint（不含 capabilities，指纹不变）；fingerprint/设备变化重探；失败以 unknown 缓存（绝不以 supported 缓存，重复同 key 不再探，测试证明）；provider 生命周期=设备 runtime/账号（DisableDevice 删除、runtime 重建重建）→ 账号更换/runtime 重建自动失效。
- Manager 新增 `RegisterCapabilityProber` / `DNSStaticCapability`（带缓存/probe）/ `DNSStaticCapabilityCached`（overview 只读展示，不跑探针）；cmd/rosboard/policy_runtime.go 装配每设备 prober。
- 注入：servePolicyPlan 在 loadPolicyMaterialization 且 DesiredRules>0 时调用（探针可缓存/串行），仅当 supported 时把有限 evidence 注入本次 snapshot 的 Capabilities.Entries（指纹不受影响，plan 冻结 capability 状态）；普通 overview/discovery 不探针（仅叠加缓存显示）。
- apply 复核：servePolicyPlanApply 在身份+指纹校验后，`policy.PlanNeedsDNSCapability`（dns/static 变更操作或冻结 capability）→ 重新取证据（缓存命中同指纹），非 supported 则 409 `policy_capability_unverified`；fixture/no-entry 回退仅在快照有 supported 证据或快照完全无该条目时沿用计划冻结证据（生产扫描恒为 unknown → 生产仍强制探针）。

### 3) 能力契约诚实化（internal/policy/capabilities.go）
- DNSStatic 所需字段改为本项目实际 wire：`type,forward-to,address-list,match-subdomain,disabled,comment`（去掉未使用的 `regexp`，注释说明 regexp 属未来能力，绝不用过宽成功放行）；测试 fixture 同步更新（缺字段→unknown 用 address-list 缺失断言）。
- 剩余能力门禁（本就为 unknown 且本轮不实现探针，真实列明）：`named_forwarder`（命名 DNS 转发器）、`move_order`（mangle/filter/nat/routing-rule 有序变更）、`dhcp_default_route_tables`、`ipv6_family`。普通单出口共享列表方案（无 DNS forwarder、无 mangle 重排、IPv4、非 DHCP 回填）不触碰这些门禁——planner gating 回归证明 minimal fixture 仅出现 dns_static_capability 一类 blocker；若启用 IPv6/输出 mangle/DHCP 自动回填/命名转发器，仍会 fail-closed 阻塞直至实现对应安全探针（不在本轮范围）。

### 4) 前端聚合（ChangePlanView.tsx + planIssues.ts 纯函数）
- 同 code+reason 重复 blocker 聚合成一条根因行“影响 N 个操作”，可展开对象列表（logical IDs 去重）；单条保持逐条。
- 可操作中文：dns_static_capability → “正在验证/验证失败：…最小权限探针…重新生成预览…与 WAN/LAN/域名内容无关”；named_forwarder/dhcp_route_table_capability 给出“本轮未实现探针”诚实说明；不提示用户改 WAN/LAN。
- 能力验证后计划不再生成 dns_static_capability blocker（179 条规则=358 ops，0 blocker）；overview 能力矩阵在缓存 supported 时显示“dns_static_address_list: supported（已验证）”，不探针。

## 测试（真实结果，全部通过）
- `TestCapabilityProberEndToEndLifecycle`（create→readback→delete→confirm-absent，各一次）
- `TestCapabilityProberExistingObjectUntouched`（既有对象不动）
- `TestCapabilityProberFailuresFailClosed`（create 403→unknown；delete 500→unknown 且仅 1 次 DELETE 不重试；confirm-absent 检测残留→unknown）
- `TestCapabilityMatrixProviderSerializesConcurrentPreviewsAndCachesByKey`（4 并发=1 探针；fingerprint 变化重探=2；失败缓存为 unknown 且不重探）
- `TestBuildChangePlanDNSStaticCapabilityGating`（179 规则：unknown=358 个 dns_static_capability blocker；supported=0 blocker 且 358 个 dns static ops 保留）
- `TestPlanNeedsDNSCapability`（路由表 op 不触发；dns/static op 与冻结能力触发）
- capabilities 定义测试更新（缺字段→unknown）

## 验证命令（真实结果）
- `gofmt -l internal cmd`：无输出；`go vet ./...`：通过
- `go test -count=1 ./...`：全 10 包 ok
- `go test -count=1 -race ./internal/{policy,store,api,auth,config,routeros,service} ./cmd/rosboard`：ok
- `npm --prefix web run lint`：0/0；`build`：通过（tsc+vite）；`audit --audit-level=high`：0 vulnerabilities
- `git diff --check`：通过；changed files trailing whitespace：无

## 改动文件
- 后端：internal/policy/{capability_probe.go(新),capability_matrix.go(新),capability_probe_test.go(新),capability_matrix_test.go(新),manager.go,capabilities.go}；internal/api/policy_routing.go；cmd/rosboard/policy_runtime.go
- 前端：web/src/features/policy-routing/{ChangePlanView.tsx,planIssues.ts(新)}

## 未解决边界 / 剩余能力门禁（如实）
1. named_forwarder、move_order、dhcp_default_route_tables、ipv6_family 仍为 unknown 门禁，本轮未实现对应安全探针；普通单出口 IPv4 共享列表方案不受影响，启用上述功能会 fail-closed 阻塞（需后续同框架探针，不改能力定义）。
2. 探针结果只在预览/apply 路径产生（overview 只读缓存展示）；若首次预览失败，需重新生成预览触发重探。
3. 未部署、未 commit、未连接/写入 RouterOS、未改 AGENTS.md；10.0.0.99 未连接、无 probe。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 77: Phase 12 DNS 能力探针 fail-closed + 删除生命周期（解绑即删/解绑 tombstone/egress 自动解绑）

**Date**: 2026-08-24
**Task**: Phase 12 DNS 能力探针 fail-closed + 删除生命周期（解绑即删/解绑 tombstone/egress 自动解绑）
**Branch**: `main`

### Summary

DNS 探针：create 成功后任何 readback/confirm 失败都按确切 .id 清理一次（不重试，双阶段错误+身份）；ambiguous create 只读精确 name 查找仅删唯一匹配；缓存仅 supported（失败/取消不缓存可重试），并发单飞，取消不毒化；apply nil snapshot 防 panic；预览触发扩大至 Delete-only。删除生命周期：source 删除同事务自动解绑→无 refs 立即物理删/有 refs tombstone；egress 删除自动解绑全部 source 保留未分配+pending，不再 egress_referenced_by_sources；生命周期 metadata 清理仅限完整 committed（修 partial 误删隐患）。前端按钮/文案同步。回归覆盖原子性/rollback/CAS/TOCTOU/审计/共享列表/179→358/delete-only 触发。全门禁绿。未部署未 commit 未连 RouterOS。

### Main Changes

# Phase 12：DNS 能力探针 fail-closed 修复 + 删除生命周期 实现汇报（Pi，未部署/未 commit/未连 RouterOS）

## 一、DNS Static 探针 fail-closed 且不遗留孤儿（internal/policy/capability_probe.go）
- Create 成功且拿到 RouterOS `.id` 后，**任何后续失败（readback HTTP 错误/字段篡改/confirm-absent）都会执行一次针对该确切 `.id` 的清理删除**（`cleanupByID`，单次不重试）；`CapabilityProbeError` 同时携带原始失败阶段与清理失败阶段（含 probe name/address-list/id），失败结果 unknown。
- Create 返回错误（可能 ambiguous）：用唯一 probe name 做**只读精确查找**（name filter + 应用侧二次比对），仅当唯一匹配才删除该对象并返回 unknown；无匹配/多匹配 → fail closed（多匹配绝不清除），保留身份供人工清理；绝不按宽泛条件删用户对象，也绝不触碰既有对象。
- 成功路径不变：create → 同对象完整字段 readback（type/forward-to/address-list/match-subdomain/disabled/comment/.id/name）→ literal id delete → 独立 list confirm absent；不扩大菜单、无 /execute。
- 回归：`TestCapabilityProberFailuresFailClosed`（readback HTTP 500 → unknown+清理 1 次+无残留；**readRewrite 注入 address-list 篡改** → unknown+清理+无残留；delete 失败 → unknown+仅 1 次 DELETE+对象明确残留）；`TestCapabilityProberAmbiguousCreateCleansUniqueMatchOnly`（create 500 但对象已建 → 按唯一 name 清理，foreign 对象不动；无收获/多匹配 → fail closed）；既有 EndToEnd/Existing-touch 保持。

## 二、能力缓存与请求取消（internal/policy/capability_matrix.go）
- **只缓存 supported**（键=device identity + 结构 fingerprint）：unknown/unsupported/取消/临时网络失败**不落缓存**，下一次显式预览会重探（“重新生成预览”语义真实有效）。
- 并发单飞：in-flight channel —— 同一 key 的并发调用共享一次探针结果；支持后写入缓存；失败只回传当次调用者，后续显式预览重试。
- 回归：并发 4 路 = 1 探针；fingerprint 变化重探；失败不缓存（同 key 两次调用=两次探针）且**从不 supported**；**ctx canceled 不毒化缓存**（随后健康预览成功重探）。
- apply 复核：`identity.ActualSnapshot == nil` → 503 `policy_discovery_unavailable`（不再解引用 panic）；再进入 DNS 能力复核（含缓存命中/变化重探 + 快照 supported 回退仅限 fixture 无条目场景，生产扫描恒 unknown → 生产强制探针）。

## 三、预览触发范围（internal/api/policy_routing.go）
- 触发条件由 `len(DesiredRules)>0` 扩大为 `materializationHasDNSStatic`（Desired/Create/Update/Delete/Keep 任一非空）——**删除最后一个来源/策略时仅 DeleteRules 也会触发探针**。
- 回归：`TestMaterializationHasDNSStaticTrigger`（delete-only 触发、空/nil 不触发）；planner gating `TestBuildChangePlanDNSStaticCapabilityGating` 保持（179 规则 unknown=358 blocker / supported=0 blocker 且 358 ops 保留）。

## 四、删除生命周期（用户已确认语义，本轮实现）
- **删除域名列表**：`DetachAndDeletePolicySource`（store 原子事务）：先在同一事务自动解除 egressId 绑定（revision+1 防并发）；随后**无已应用 materialized refs → 立即物理删除 + audit(deleted)**；**有 refs → 保留 unbound + pending-delete tombstone + audit(pending)**，由 cleanup plan 生成、**仅完整 JobStateCommitted 终端事务物理删除**；失败/cancel/rollback/restart/committed_partial 均不提前删元数据（store 生命周期清理已改为仅 committed）。revision CAS、设备隔离、TOCTOU（refs 计数在事务内）、审计原子性保持。
- **删除策略路由**：`DetachSourcesAndMarkEgressPendingDelete`（store 原子事务）：收集并**自动解除全部非 pending 绑定 source（egress_id=NULL、revision+1、每 source 一条 source.unassign 审计）**，域名列表保留为未分配；策略标记 pending-delete + egress 审计；**不再返回 egress_referenced_by_sources**；RouterOS 清理与最终 metadata 删除走 immutable cleanup plan + 完整 commit 终端事务（保留 backup/ownership/fail-closed）。
- 修复既有隐患：`Commits` 中源/出口 metadata 清理原在 committed_partial 也会执行——**改为仅 JobStateCommitted**（partial/cancel/rollback/restart 保留元数据）。
- 回归：`TestPolicySourceDetachAndDeleteAtomics`（assigned+无 refs 立即删+egress 保留；stale CAS；跨设备拒绝；refs→tombstone 保留；audit 失败全回滚）；`TestPolicyEgressDetachSourcesAndMarkPendingDeleteAtomics`（2 source 原子解绑+保留+审计；double-delete fail；audit rollback；每 source audit 2 条）；API：`TestPolicyEgressDeleteTwoPhaseGuards`（带 source 删除 200、source 未分配保留、double 409）、`TestPolicyEgressDeleteSharedListBetweenEgresses`（含 source 删 A、B 共享列表不再阻止、解绑后可删 source）、`TestPolicySourceDeleteIntentAndRevisionGuard`（assigned 立即删+egress 保留+audit）、`TestPolicySourceDeleteRejectsAppliedRules`（refs→200 pendingCleanup tombstone）。
- 前端：PolicySourcesPage 删除按钮 = 仅 pending（待清理）禁用，assigned/unassigned 均可点；DeleteSourceDialog 文案“先自动解绑；有已应用对象进入待清理需应用设置清理；否则立即删除”；DeleteEgressDialog 无阻止、展示将解绑的列表（保留为未分配）、共享列表仅提示、确认可用。

## 改动文件
- 后端：internal/policy/{capability_probe.go,capability_matrix.go,capability_probe_test.go,capability_matrix_test.go,capabilities.go,manager.go}；internal/api/{policy_routing.go,policy_lifecycle.go,phase12_capability_trigger_test.go,phase10_test.go,phase12_source_delete_test.go}；internal/store/{policy.go,policy_test.go}；cmd/rosboard/policy_runtime.go
- 前端：web/src/features/policy-routing/{PolicyEgresses.tsx,PolicySourcesPage.tsx,PolicySources.tsx,ChangePlanView.tsx,planIssues.ts(上轮)}

## 验证（真实结果）
- gofmt -l internal cmd：无输出；go vet ./...：通过
- go test ./... -count=1：全 10 包 ok
- go test -count=1 -race ./internal/{policy,store,api,auth,config,routeros,service} ./cmd/rosboard：ok
- npm lint：0/0；build：通过；audit：0 vulnerabilities
- git diff --check：通过；trailing whitespace：无

## 未解决边界 / 剩余门禁（如实）
1. named_forwarder / move_order / dhcp_default_route_tables / ipv6_family 仍为 unknown 门禁、本轮未实现对应安全探针（普通单出口 IPv4 共享列表方案不触碰；启用 IPv6/输出 mangle/DHCP 回填等会 fail-closed）。
2. egress “无已应用对象立即删”分支**故意未实现**：仅凭无 materialized refs 无法证明 family-only 出口的 RouterOS 对象不存在（router-objects/表/路由不属 materialization），采用统一的 tombstone→cleanup plan→完整 commit 删除以杜绝孤儿（安全优先，符合“可安全完成”选择权）。
3. 未部署、未 commit、未连接/写入 RouterOS（10.0.0.99 未连接、无 probe）、未改 AGENTS.md、保留 dirty worktree。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 78: Phase 12 review 续阻塞（第六轮）：egress 解绑覆盖 pending/前端按钮一致性/probe cleanup 独立 ctx/并发单飞按 key/token 精确清理

**Date**: 2026-08-24
**Task**: Phase 12 review 续阻塞（第六轮）：egress 解绑覆盖 pending/前端按钮一致性/probe cleanup 独立 ctx/并发单飞按 key/token 精确清理
**Branch**: `main`

### Summary

1)DetachSourcesAndMarkEgressPendingDelete 去掉 pending_delete 过滤，同一事务解绑全部绑定 source（含 pending，pending 状态保持）+每 source audit，完整 committed cleanup 不再因残留绑定失败（回归 TestPolicyEgressDetachUnbindsPendingSourcesToo）；2)前端：删除按钮仅 assigned+pending disabled，未分配含 legacy pending 可删；egress 待删除行编辑/启停/删除 disabled；3)Prove 清理全部走 Background+15s 独立 ctx，请求取消后仍尝试一次 DELETE 且成功清理（新回归）；4)CapabilityMatrixProvider 改按 key in-flight map + 等待者 select ctx.Done，取消等待者不影响 owner，A/B/A 并发回归总 create=2；清理未用 helper；5)cleanupByName 要求 name+comment token+address-list 全匹配，0/多/空 id fail closed（同名无 token 保留、双 token 匹配双保留回归）。全门禁绿：gofmt/vet、go test ./...、-race 全 ok、npm lint/build/audit、git diff --check。未部署未 commit 未触 RouterOS。

### Main Changes

# Phase 12 review 续阻塞修复（第六轮，Pi，未部署/未 commit/未连 RouterOS）

## 1) egress 删除解绑必须覆盖已 pending 的 source（关键生命周期 bug）✅
- 根因：`DetachSourcesAndMarkEgressPendingDelete` 的 SELECT/UPDATE 带 `AND pending_delete = 0`；存在 pending_delete=1 且仍有 egress_id 的 source 时不会被解绑，CommitPolicyExecution 的 egress cleanup 统计到残留绑定而清理必失败。
- 修复：SELECT/UPDATE 去掉 pending_delete 过滤（同一事务解除**全部** egress_id=该策略的 source，egress_id=NULL、revision+1、每 source 一条 `source.unassign` 审计）；**source 原 pending 状态保持不变**（只改 egress_id）。
- 回归：`TestPolicyEgressDetachUnbindsPendingSourcesToo`——normal+pending(source 已 MarkSourcePendingDelete) 两个绑定 source 删除 egress 后：both egress_id 为空、pending source 仍 pending；随后**完整 committed lifecycle commit（DeleteEgresses）成功**（不再因残留绑定失败）并物理删除 egress 元数据；`TestPolicyEgressDetachSourcesAndMarkPendingDeleteAtomics` 保持（2 source 原子解绑+审计+rollback/double-delete）。

## 2) 前端删除按钮与状态一致性 ✅
- PolicySourcesPage：删除按钮 disabled 仅限 `egressId && pendingDeletion`（遗留的“已绑定 pending”待清理态，标题提示：完成策略删除/应用清理计划）；**未分配（含 legacy pending）与已绑定但未 pending 的列表始终可点**（不再出现“状态写可立即删除但按钮 disabled”矛盾）；后端同事务自动解绑 + 立即删/待清理语义由 store 保证。DeleteSourceDialog 文案同步（自动解绑；有已应用对象→待清理需应用设置；否则立即删除）。
- PolicyEgresses 表格：`egress.pendingDeletion` 行的编辑/启停/删除全部 disabled + 明确标题（“已进入待删除/待清理；请应用设置完成清理”），避免误操作与二次删除。

## 3) capability probe cleanup 使用独立 bounded context ✅
- 根因：Prove 用请求 ctx；create 成功后请求被取消会导致 cleanupByID/cleanupByName 拿到已取消 ctx → 清理失败并遗留 probe。
- 修复：所有 cleanup/delete/confirm-absent/ambiguos-lookup 都在 `cleanupContext()`（`context.Background()` + 15s timeout）上执行；创建/读回仍用请求 ctx（请求取消即中止探针主体）；错误保留 double-stage（原始失败 + cleanup 失败）。
- 回归：`TestCapabilityProberCanceledRequestStillCleansUp`——create 成功、GET 阻塞、cancel 请求 ctx、释放后 readback 失败 → **DELETE 仍被尝试 1 次且成功清理**（deleteCalls==1、objects 空）；delete ambiguous 不重试保持（FailuresFailClosed 已有 1 次断言）。

## 4) CapabilityMatrixProvider 并发 ✅
- 等待者 `select { <-running.done, <-ctx.Done() }`：取消等待者立即返回 unknown 且**不影响 owner/其他等待者**；owner 完成仍写 supported cache。
- **按 key 的 in-flight map**（不再是单槽）：key A 探针运行时 key B 独立起探针，第三个 A 加入 A 的 in-flight——A 永远单飞。
- context canceled 不写 supported cache（仅 success 入缓存）。
- 回归：`TestCapabilityMatrixProviderABASingleFlightPerKey`（阻塞 A → 起 B 与 A2 → 释放：总 create=2（A 一次、B 一次）、A1/A2 均 supported）；`TestCapabilityMatrixProviderCanceledWaiterDoesNotAffectOwner`（取消等待者秒退 unknown 含 “wait canceled”，owner 完成 supported、create==1）。
- 清理未使用的 `errCapabilityContextCanceled/capabilityProbeCanceledReason`（上一轮遗留）。

## 5) ambiguous create 精确清理加强 ✅
- `cleanupByName` 现在要求 **name 精确相等 + comment 含 probe token + address-list 与 probe 完全一致** 才视为该 probe 的唯一匹配；id 为空 fail closed；0/多匹配均不删除（身份进错误供人工清理）。
- 回归：`TestCapabilityProberCleanupRequiresTokenAndAddressListMatch`——同名但无 token/address-list 不符的 impostor 被保留（仅真 probe 被清理）；两个完全相同的 token 匹配 → fail closed，两个对象都保留并报告 “2 exact token matches”；`TestCapabilityProberAmbiguousCreateCleansUniqueMatchOnly` 保持（唯一 token 匹配被清理、foreign 不动）。

## 改动文件（本轮）
- 后端：internal/store/{policy.go, policy_test.go}；internal/policy/{capability_probe.go, capability_matrix.go, capability_probe_test.go, capability_matrix_test.go}
- 前端：web/src/features/policy-routing/{PolicySourcesPage.tsx, PolicyEgresses.tsx}
（沿用此前轮次的 DNS 探针/删除生命周期/聚合等改动）

## 验证（真实结果）
- gofmt -l internal cmd：无输出；go vet ./...：通过
- go test ./... -count=1：全 10 包 ok
- go test -count=1 -race ./internal/{policy,store,api,auth,config,routeros,service} ./cmd/rosboard：全 ok
- npm lint：0/0；build：通过；audit：0 vulnerabilities
- git diff --check：通过；trailing whitespace：无

## 未解决边界（如实）
1. named_forwarder / move_order / dhcp_default_route_tables / ipv6_family 仍为 unknown 门禁、本轮未实现探针（普通单出口 IPv4 共享列表不触碰；启用相关功能会 fail-closed）。
2. egress “无已应用对象立即删”分支仍未实现（统一 tombstone→cleanup plan→完整 commit，避免 family-only 出口 RouterOS 对象孤儿）。
3. 未部署、未 commit、未连接/写入 RouterOS（10.0.0.99 未连接无 probe）、未改 AGENTS.md；dirty worktree 保留。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 79: Phase 12 review（第七轮·前端域名列表语义）：诚实状态文案 + 已绑定可持续删

**Date**: 2026-08-24
**Task**: Phase 12 review（第七轮·前端域名列表语义）：诚实状态文案 + 已绑定可持续删
**Branch**: `main`

### Summary

sourceStatus：pendingDeletion 一律'待清理（需应用设置）'（含未分配+已绑定两态），不再出现'可立即删除'误报；删除按钮去掉 pending-disabled 分支，所有非只读 source 可点击（含已绑定+pending），后端同事务自动解绑、无 refs 立即删/有 refs 待清理；DeleteSourceDialog 对 pendingDeletion 明确'继续解绑/仍待清理需应用设置/不直接写 RouterOS'。静态核对：'可立即删除'仅注释、setDeleting 单入口、deleteSource 带 revision。lint/build/audit 通过；后端相关回归（egress detach pending-source、probe cleanup、capability A/B/A、source/egress 原子解绑）与 go test ./... 全绿；vet/gofmt/diff --check 通过。未部署未 commit 未连 RouterOS。

### Main Changes

# Phase 12 review（第七轮 · 前端域名列表语义）修复汇报（Pi，未部署/未 commit/未连 RouterOS）

## 根因
PolicySourcesPage.tsx 两处与后端能力/user 需求不一致：
1. `sourceStatus` 对「pendingDeletion && egressId 为空」显示“未分配（可立即删除）”，而后端 `DetachAndDeletePolicySource` 在仍有 `policy_materialized_refs` 时会解绑后**保留 pending tombstone**（需应用设置清理），删除按钮再次点击也不会立即物理删除——UI 误报“可立即删除”。
2. 删除按钮对「egressId && pendingDeletion」渲染 disabled + “先删策略”提示，与用户明确需求（已绑定也可单独删除、后端已支持事务内自动解绑）不一致。

## 修复（仅前端两个必须文件）
- `PolicySourcesPage.tsx`
  - `sourceStatus` 改成诚实文案：pendingDeletion 一律“待清理”——`egressId && pendingDeletion` → “待清理（仍绑定策略，需应用设置）”；`egressId 为空 && pendingDeletion` → “未分配（待清理，需应用设置）”（不再出现“可立即删除”）；未分配非 pending → “未分配（不参与应用）”。不引入 N+1 查询、不猜测 hasAppliedRules（保持简单安全）。
  - 删除按钮：去掉 pendingDeletion-disabled 分支，**所有非只读 source 均可点**（含已绑定 + pendingDeletion）；后端同事务自动解绑、无 refs 立即删/有 refs 保留待清理（不在 UI 伪造立即成功）。
- `PolicySources.tsx`（DeleteSourceDialog）
  - pendingDeletion 文案明确：本次继续自动解除任何残余绑定；仍有已应用引用则保持待清理、需随后应用设置（不直接写 RouterOS）；无引用才物理删除。保持单次确认弹窗、`deleteSource(id, revision)` 不变。

## 静态核对（无前端测试框架，以 grep/构建验证）
- `grep 可立即删除`：仅剩代码注释（“绝不宣称可立即删除”），无任何用户可见文案；✓
- `setDeleting(source)` 仅 1 处点击入口；页面内 `source.pendingDeletion ? (` 分支数=0（删除不再按 pending 禁用）；✓
- `deleteSource(props.deviceID, props.source.id, props.source.revision, signal)` 保持（带 revision 门禁）；✓

## 测试与结果
- `npm --prefix web run lint`：0/0 ✓；`npm --prefix web run build`（tsc+vite）通过 ✓；`npm audit --audit-level=high`：0 vulnerabilities ✓
- 后端回归保持通过：`go test ./internal/policy ./internal/store ./internal/api -count=1 -run "TestCapability|TestPolicyEgressDetach|TestPolicySourceDetach|TestPolicySourceDelete|TestPolicyEgressDelete|TestPolicySourceImmediate|TestPolicyDeleteSource"` → ok（含 egress detach pending-source、probe cleanup、capability A/B/A、source/egress 原子解绑等）；`go test ./... -count=1` 全 10 包 ok ✓
- `go vet ./...` 通过；`gofmt -l internal cmd` 无输出；`git diff --check` 通过；changed files 无 trailing whitespace ✓

## 改动文件（本轮）
- web/src/features/policy-routing/PolicySourcesPage.tsx
- web/src/features/policy-routing/PolicySources.tsx

## 边界（如实）
- 未引入 hasAppliedRules/pendingCleanup 的服务器投影：为诚实起见统一显示“待清理”，不猜测可立即删除（简单、安全优先）。
- 删除仍是后端门禁驱动的两相语义（立即删 / 待清理），UI 不伪造成功。
- 未部署、未 commit、未连接/写入 RouterOS（10.0.0.99 未连接）、未改 AGENTS.md、保留 dirty worktree。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 80: Phase 12 review 第七轮跟进：移除 JSX 内裸注释泄漏 + 存量注释准确化

**Date**: 2026-08-24
**Task**: Phase 12 review 第七轮跟进：移除 JSX 内裸注释泄漏 + 存量注释准确化
**Branch**: `main`

### Summary

Lead 复核发现第七轮 PolicySourcesPage.tsx 删除按钮前 '// 所有非只读 source…' 裸注释位于 JSX children 被编译为 JSXText 进 DOM（dist 出现内部说明）。修复：整段删除该裸注释（按钮语义不变）；rg 扫描 feature 全部 tsx 确认仅此一处落在 JSX children（其余均在 TS 函数体内不渲染）；PolicySources.tsx DeleteSourceDialog 上方旧注释'未分配列表的删除是立即删除'改为准确实现注释（有已应用引用时转待清理需应用设置）。验证：npm lint/build 通过，dist 中 rg 该泄漏串及其它内部注释串均无匹配（仅剩刻意前端文案），go test ./... 全绿、vet/gofmt/diff --check/npm audit 通过。未部署未 commit 未连 RouterOS。

### Main Changes

# Phase 12 review（第七轮跟进）修复汇报：JSX 内裸注释泄漏回归（Pi，未部署/未 commit/未连 RouterOS）

## 根因（Lead 发现，属实）
第七轮在 PolicySourcesPage.tsx 删除按钮前新增的三行 `// 所有非只读 source 都可发起删除…` 直接写在 JSX children 内；TSX 编译把 `// …` 当作 **JSXText** 渲染进 DOM——构建产物 `internal/ui/dist/assets/index-*.js` 中出现该内部实现说明字符串，用户页面可见（可见 UI 回归）。

## 修复
1. **删除 JSX 内裸注释**：`PolicySourcesPage.tsx` 删除按钮上方那段 `// 所有非只读 source…`（三行）整段移除，按钮保持可点（语义不变：所有非只读 source 可发起删除，后端同事务自动解绑、无引用立即删/有引用转待清理）。
2. **同步排查本轮/历史 JSX 内裸 `//` 或 `/*` 文本**：用 `rg -n "^\s{6,}// "` 扫描 feature 全部 .tsx——唯一落在 JSX children 的即上述三行（其余全部位于 TS 函数体内语句之间，不渲染）；已修复。
3. **存量注释准确性**：`PolicySources.tsx` DeleteSourceDialog 上方旧注释“未分配列表的删除是立即删除”已不准确（存在已应用物化引用时后端保留 pending tombstone 需应用设置清理）——改为准确实现注释（描述 DetachAndDeletePolicySource 的解绑/立即删/待清理语义），未改业务语义。

## 验证（真实结果）
- `npm --prefix web run lint`：0/0
- `npm --prefix web run build`（tsc+vite）：通过
- **构建产物检查**：`rg -o "所有非只读 source 都可发起删除" internal/ui/dist/assets/*.js` → 无匹配（泄漏已清除）；另扫描 `未分配列表的删除是立即删除` / `Fail closed` / `不伪造立即成功` → 均无；dist 中仅剩的用户可见 hint（如“只在本次向导内存中使用…”、“预览已就绪；点击…继续”）为**刻意设计的前端文案字符串**，非内部注释（已人工核对上下文）。
- `go test ./... -count=1`：全 10 包 ok
- `go vet ./...`：通过；`gofmt -l internal cmd`：无输出
- `npm --prefix web run audit --audit-level=high`：0 vulnerabilities
- `git diff --check`：通过；changed files 无 trailing whitespace

## 改动文件
- `web/src/features/policy-routing/PolicySourcesPage.tsx`（移除 JSX 内裸注释）
- `web/src/features/policy-routing/PolicySources.tsx`（DeleteSourceDialog 上方注释改为与后端语义一致的准确实现注释）

## 边界
- 未部署、未 commit、未连接/写入 RouterOS（10.0.0.99 未连接）、未改 AGENTS.md、保留 dirty worktree；前端构建产物为本地生成（dist 未部署）。

### Git Commits

(No commits - planning session)

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 81: 设备管理界面重构与 net 部署验收

**Date**: 2026-08-28
**Task**: 设备管理界面重构与 net 部署验收
**Branch**: `main`

### Summary

移除设备管理页底部运行信息卡片，完成设备接入状态、连接测试与可直接保存流程的前端及后端改造；构建并部署 Linux amd64 版本到 net (10.0.0.6:8080)，完成服务、健康接口、API 与嵌入资源验证，用户已明确验收通过。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `6082ad0` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 82: 修复策略弹窗焦点抢占并调整出口表格列

**Date**: 2026-08-28
**Task**: 修复策略弹窗焦点抢占并调整出口表格列
**Branch**: `main`

### Summary

定位 PolicyModal 焦点 effect 依赖 onClose，父组件轮询重渲染导致输入时焦点被抢回关闭按钮；改为仅挂载时初始化焦点、onClose 走 ref。出口策略表格新增策略接口/下一跳网关/地址族列并按用户指定顺序排列。前端 lint+build、go vet+全量测试通过；两次部署 10.0.0.6（备份 20260828T072541Z-policy-modal-focus-fix、20260828T074816Z-policy-egress-table-columns、20260828T080228Z-policy-egress-address-family-column），期间一次因漏设 GOOS=linux 部署了错误二进制导致服务短暂 Exec format error，已立即修复重部，用户验收通过后按 588d0ee（网关发现/DNS缓存预警，此前已上线）与 a64e99a 分两笔提交。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `a64e99a` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 83: 策略路由 WAN/LAN/VPN 出口发现修复与 net 部署

**Date**: 2026-08-29
**Task**: 策略路由 WAN/LAN/VPN 出口发现修复与 net 部署
**Branch**: `main`

### Summary

完成 LAN 优先的 WAN 发现、WireGuard 与 RouterOS VPN/隧道出口候选、点到点网关处理和未验证状态提示；通过全量测试与远端健康/API/嵌入资源校验，部署到 net 后经用户手动验收通过。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `a255e65` | (see git log) |
| `382e677` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 84: Refine policy wizard navigation workflow

**Date**: 2026-08-29
**Task**: Refine policy wizard navigation workflow
**Branch**: `main`

### Summary

Reordered policy wizard steps, enabled direct step navigation, added edit-only save-and-preview action, and deployed the verified frontend bundle to net.

### Main Changes

- Reordered the wizard to egress/address family, traffic ingress, domain lists, and preview/apply.
- Made all wizard step headings directly clickable.
- Added an edit-only “保存并应用” shortcut that saves the draft, generates a plan, and opens preview.
- Kept new-policy creation on the normal Next flow without the shortcut.
- Verified the frontend lint/build, Linux amd64 build, remote health endpoint, and embedded asset markers.


### Git Commits

| Hash | Message |
|------|---------|
| `e3dad84` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 85: 策略路由自动同步与 RouterOS 对账修复

**Date**: 2026-08-29
**Task**: 策略路由自动同步与 RouterOS 对账修复
**Branch**: `main`

### Summary

修复已绑定启用策略的域名列表保存后仍停留在 pending、必须二次应用的问题：保存和定时刷新自动同步，策略向导合并为一次同步，并清理普通流程待应用文案；同时修复 RouterOS 默认 disabled 字段导致的非法空值 patch。已通过 Go 全量测试/race/vet、前端 lint/build/audit，备份并部署 10.0.0.6，完成服务/API/嵌入资源验证及用户手动验收。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `c53efae` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 86: Commit and deploy integrated policy/device updates

**Date**: 2026-08-29
**Task**: Commit and deploy integrated policy/device updates
**Branch**: `main`

### Summary

Deployed and user-accepted the integrated policy routing, naming, and device ordering changes; committed implementation and archived the review-fixes task.

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `df2be77` | (see git log) |
| `2e03894` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 87: 手动域名列表来源

**Date**: 2026-08-29
**Task**: 手动域名列表来源
**Branch**: `main`

### Summary

新增手动域名列表来源，支持 Clash DOMAIN/DOMAIN-SUFFIX 与 mosdns plain/domain:/full: 格式；已有来源编辑时锁定来源类型，后端拒绝类型变更；通过 Go 全量测试、vet、前端 lint/build、本地运行检查，并部署到 10.0.0.6 完成服务、健康接口、嵌入资源和手工验收。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `e0bd6e2` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 88: 识别设置全量按设备化（MosDNS/协议分析/特征库）

**Date**: 2026-08-30
**Task**: 识别设置全量按设备化（MosDNS/协议分析/特征库）
**Branch**: `main`

### Summary

识别设置全部按设备独立并部署验收通过

### Main Changes

按用户确认的方案把识别设置（协议分析总开关、MosDNS 对接、协议特征库）从进程级全局配置改为全部按 RouterOS 设备独立：

- config：三项设置全部下沉到 devices[]（protocol_analysis/feature_library/mosdns），移除全局段、ROSBOARD_MOSDNS_*/ROSBOARD_FEATURE_LIBRARY_* 环境变量与全部识别迁移逻辑；Config.ProtocolAnalysis 保留为 yaml:"-" 运行时载体以零侵入传入 Monitor。
- store：DNS 表 + mosdns_state 进每个设备库（物理隔离）；版本化一次性清空旧全局 DNS 数据与遗留水位（dns_scope_migrated="2"）；purgeDNSData 接入 PurgeDevice 与 ResetAll（含未打开的设备库文件）；ForDevice 子库打开失败返回 nil 不再回退 owner 库。
- service：每设备独立 MosDNS 同步器、特征库同步器（防碰撞独立缓存文件）与应用归因器；设备级 RecognitionStatus/MosDNSStatus；同步器初始化失败进入状态 LastError。
- api：识别设置按设备保存（关总开关强制关子项、只动列出的设备）；设备编辑不携带识别字段则保留；/api/protocols 按设备门控且回退与 MonitorForDevices 一致；/api/recognition|mosdns|mosdns/observations 需 deviceId。
- web：识别设置成为 主机设置 > 识别设置 二级子菜单（策略路由下方），页面跟随顶部设备切换器，只展示/保存当前设备；protocols 页面门控按选中设备且等 settings 加载后再重定向。

过程：外部 agent review 发现 8 个问题（5 P1 + 3 P2）全部修复；一次部署事故（marker 唯一约束导致约 3 分钟重启循环）当场修复并补回归测试。部署全程按门禁执行（NAS 时间戳备份 20260829T154153Z-per-device-mosdns + iteration1-4 留档，干净基线构建避开并行会话的策略路由 WIP），用户已在 10.0.0.6 人工验收通过。


### Git Commits

| Hash | Message |
|------|---------|
| `c808cb0` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete
