> Replanned 2026-09-07. This is a preservation checklist, not a completed runtime test report. The historical inventory below is retained as a baseline; the source audit and additions clarify current implementation. No item is approved for deletion. Locations are repository-relative; symbols are preferred over unstable line numbers.

# Feature preservation inventory

用途：UI 重写前后逐页核对。来源：web/src/App.tsx、web/src/features/**、web/src/lib/types.ts 实际代码（feat/policy-access-rebuild 分支）。重写完成时必须逐项核对仍在。

## 启动流程页

- AdminSetupPage（首次初始化管理员）、LoginPage（用户名+密码+显示密码眼睛钮）、RouterOSSetupPage（添加 RouterOS 设备引导）

## 外壳

- 一级菜单：仪表台 / 系统概览 / 状态监控▸ / 主机设置▸ / 面板设置▸
- 状态监控子项：接口监控 / 终端监控 / 流量监控 / 网络服务 / 系统运行
- 主机设置子项：目标库 / 策略路由 / 访问控制 / 识别设置
- 面板设置子项：设备管理 / 采集设置 / 界面设置 / 账号安全 / 维护设置
- 设备切换器（多设备）、顶栏搜索（终端过滤）、主题切换（明亮/深色，localStorage）、刷新频率控制（停止/1s/3s/5s/10s）、移动端汉堡菜单

## 页面与关键控件

- **仪表台 fleet**：多设备卡片总览（流量/运行时间/状态），点击进设备
- **系统概览 overview**：4 张指标卡（CPU/内存/在线终端/活动连接，各带 sparkline+构成+平均/峰值）；实时流量大图（上传/下载实时值）；系统状态列表（运行时间/RouterOS 版本/最后成功采集/活动接口/存储使用率/数据新鲜度，各带正常/注意状态）；接口状态表（接口名称/类型/状态/链路/接收速率/发送速率/接收流量/发送流量，前 7）；当前告警（级别图标+来源·消息+时间，严重/警告计数）
- **接口监控 interfaces**：物理/逻辑/系统分类 tab；表格 12 列（接口/类型/关联接口/地址·MAC/状态/上行速率/下行速率/累计上行/累计下行/MTU/掉线次数/错误·丢包）；点接口名开详情面板（含速率趋势图）
- **终端监控 terminals**：地址族 tab（全部/IPv4/IPv6）；表格列 = 设备名称（+MAC 次行）/IP（+次行）/连接数/上行速率/下行速率/累计上行/累计下行/在线状态（表头 ▾ 筛选：在线/全部/含离线）/在线时长/备注/操作（详情/编辑）；全部表头可排序 ↕；分页 = 每页 10/20/50 + 上一页/下一页 x/y；状态徽章 在线/近期未活跃/离线
- **终端详情**：基本信息、连接列表（多列可排序+列头筛选）、实时速率图；编辑备注/自定义名 dialog
- **流量监控**：protocols（协议统计，可开关协议分析）/ policies（策略统计）视图
- **网络服务**：dhcp / routes 视图
- **系统运行**：resource（资源监控）/ load（负载历史）视图
- **目标库 target-library**（features/policy TargetLibraryPage）
- **策略路由 policy-routing**（features/policy RoutingRulesPage + Wizard + PlanPreview）：
  - 列表：名称（含 Priority N 次行）/来源/目标/出口/状态（已启用/待应用/已停用/出口缺失/出口已停用/出口待删除 6 态徽章）/操作（停用·启用/编辑/删除）
  - 新增策略按钮、PolicyNotice 通知条
  - 向导 4 步：策略与来源 / 访问目标 / 出口 / 预览并应用；第 4 步含本次配置六项、计划信息六项、变更清单分组（含 seq/family 徽章/动作徽章/查看更多）、警告块、应用前确认 checkbox（必选徽章）、返回修改/确认并应用
- **访问控制 access-control**（features/access-control AccessControlPage）
- **识别设置 recognition**
- **面板设置 settings**（每组独立保存，无全局保存）：
  - 设备管理：基础连接参数（设备名称*/IP 或主机名*/协议·端口*）+ 认证凭据（REST 用户名*/REST 密码，留空保持现有）+ 保存设备/取消
  - 采集设置：完整/实时/终端采集间隔（秒）+ 采样保留时间（小时）+ 保存并重启采集
  - 界面设置：默认自动刷新 / 默认打开页面（14 视图）/ 默认终端范围（全部/IPv4/IPv6）/ 主题（明亮/深色）+ 保存界面设置
  - 账号安全：管理员用户名*/密码（至少 4 字符）*/再次输入* + 保存账号和密码 + 退出登录
  - 维护设置：导出全部设备脱敏设置 / 重置界面偏好 / 重启面板服务 / 已归档设备（RouterOS 清理脚本/恢复/永久清除）/ 完全重新初始化（危险区）

## 已确认不存在、不得新增虚构的功能

- 终端新增/导出按钮、表格 checkbox 批量选、在线/离线分段开关（真实实现在表头 ▾ 筛选）、新增无数据支撑的搜索能力（现有终端搜索可移动到页内，目标库已有独立搜索）
- 登录页记住密码/忘记密码/用户协议
- 概览页下联 AP/无线有线拆分、自定义快捷入口
- 面板设置「列表每页条数」、全局保存按钮、改密码需验证当前密码


## Verification ledger for the next implementer

For every ID record the final component/location, exercised scenario and result during implementation. All outcomes are **pending**. A page being present does not verify its conditional controls. The list below scopes the review; enumerate individual fields/handlers when entering each phase rather than treating this document as proof of exhaustive runtime coverage.

| ID | Source anchor | Preserve and exercise | Planned destination |
|---|---|---|---|
| AUTH-01 | `web/src/App.tsx`: AdminSetupPage, LoginPage | First admin setup, credentials, password visibility, validation, submitting and auth error | Restyled auth forms |
| AUTH-02 | App.tsx: RouterOSSetupPage, EmptyDevicePanel | First-device setup, empty-device navigation and existing settings/maintenance routes | Onboarding and consistent empty shell |
| NAV-01 | App.tsx: PanelApp | All five primary groups and every subview listed above, correct active state and group landing | Compact navigation |
| NAV-02 | App.tsx: PanelApp | Multi-device switch, device identity, theme persistence, refresh stop/1/3/5/10s, mobile open/close | Switcher/topbar/drawer |
| FLEET-01 | App.tsx: fleet view | Device summaries, status, query and open-device action | Fleet cards and local toolbar |
| OVER-01 | App.tsx: OverviewPage | CPU/memory/terminal/connection metrics, sparklines, composition, averages and peaks | Grouped banner and metric charts |
| OVER-02 | App.tsx: OverviewPage, SystemStatusList | WAN aggregates, interface addresses/link info, uptime/version, last success, active interfaces, storage, freshness | Information rail and status area |
| OVER-03 | App.tsx: OverviewPage | Upload/download series, interface table, all nine quick links, alerts with severity/source/time/counts | Monitoring area plus compact quick links |
| MON-01 | App.tsx: interfaces view | Physical/logical/system tabs, all existing table fields, interface details and trend | Styled table/detail |
| MON-02 | App.tsx: terminals view | IPv4/IPv6/all, query, supported header sorts, online header filter, page size 10/20/50 and previous/next | Terminal toolbar/table/footer |
| MON-03 | App.tsx: terminal detail/editor | Identity/IP/MAC, connections with sorts/filters, live chart, custom name/note edit, cancel/save/errors | Detail and editor |
| MON-04 | App.tsx: traffic/services/system views | Protocol analysis switch/statistics, policy statistics, DHCP/routes, resources/load history, real time ranges | Existing views restyled |
| TARGET-01 | `web/src/features/policy/TargetLibraryPage.tsx` | All/domain/IP tabs, name/ID/URL search, create/edit/delete confirmation, URL-only refresh, usage-based deletion disable | Target toolbar/table |
| TARGET-02 | TargetLibraryPage.tsx: TargetListModal | Name/kind/manual/URL/upload, file input, schedule, content preview, counts, manual-content load/retry, dirty-preview gating and job completion | Sectioned modal |
| TARGET-03 | TargetLibraryPage.tsx | Standby/applied/pending/cleanup states, versions/counts, routing/access usage, preset exclusion, preparation/errors/notices | Table and contextual feedback |
| ROUTE-01 | `web/src/features/policy/RoutingRulesPage.tsx` | Add/edit/enable/disable/delete, priority/source/target/gateway and every enabled/pending/missing/disabled/deleting gateway state | Routing list |
| ROUTE-02 | `web/src/features/policy/RoutingRuleWizard.tsx` | Four steps, real editable/jump/locked navigation, cancel/back/next, draft and validation state | Restyled existing wizard |
| ROUTE-03 | `web/src/features/policy/Selectors.tsx` and wizard | All/selected subjects, terminal selection, manual prefixes and binding modes, target selection, inline creation/application preset flows, gateway selection and warnings | Existing selectors; restore binding helper |
| ROUTE-04 | `web/src/features/policy/PolicyPlanPreview.tsx` | Configuration summary, six metadata fields, grouped operations/details, seq/family/actions, blockers, warnings, pending review, required acknowledgements, return/apply and busy/result states | Preview body; optional metadata disclosure only |
| ACCESS-01 | `web/src/features/access-control/AccessControlPage.tsx` | Boundary explanation, create/edit/toggle/delete confirmation, job phase, status/issues, disabled-device and busy restrictions | Access list |
| ACCESS-02 | AccessControlPage.tsx: AccessRuleModal | Name, all/selected/manual subjects, internet versus targets, inline target/preset creation, validation, cancel/save, always-on timing and existing unavailable scheduling explanation | Sectioned access form |
| SETTINGS-01 | App.tsx: device settings | All connection/authentication/range/advanced fields, password-blank semantics, existing add/edit/enable/archive actions and feedback | Device sections; enumerate fields from current editor before edits |
| SETTINGS-02 | App.tsx: collection settings | Full/live/terminal intervals, retention, independent save/restart collection and validation | Collection form |
| SETTINGS-03 | App.tsx: appearance settings | Default refresh/page/terminal range/theme and independent save/persistence | Appearance form |
| SETTINGS-04 | App.tsx: account settings | Username/password/confirmation, validation, save and logout | Account form |
| SETTINGS-05 | App.tsx: maintenance | Sanitized export, preference reset, service restart, archived-device cleanup script/restore/permanent purge, full reinitialize and confirmations | Maintenance and visible danger section |
| RECOG-01 | App.tsx: recognition view and referenced components | Existing recognition fields, actions and feedback; enumerate current controls before styling | Recognition page |
| STATE-01 | All above | Initial loading, no data, stale data, failed reload with previous data, long values, disabled/pending and error recovery | Shared visual treatment preserving local behavior |

## Demo adaptation rules

The preserved preview illustrates extra controls and simplified forms. Preserve real capabilities instead of copying those literals. Do not invent terminal export/add, batch selection, remembered login, forgotten password, wireless/AP data, custom shortcuts, WAN per-interface selection or test-connection actions solely because a sample resembles them. Keep existing unavailable scheduling copy in access control unless the user approves its removal. Target-library search already exists; moving terminal search into its page is a layout change, not adding a new query capability.
