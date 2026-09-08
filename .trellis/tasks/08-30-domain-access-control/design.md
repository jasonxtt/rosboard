# RouterOS 域名访问控制技术设计

## 1. 状态与边界

- 状态：用户已于 2026-08-30 批准设计，按三个顺序子任务实施。
- 父任务只维护总体需求与集成审查；可运行程序和 RouterOS 配置由子任务逐阶段修改。
- 需求来源：[prd.md](./prd.md)。调研依据：[routeros-domain-access-control.md](./research/routeros-domain-access-control.md)。

目标：

1. 每台 RouterOS 独立配置“终端 + 域名来源”的访问策略。
2. 同一来源可同时用于策略路由和访问控制。
3. RouterOS 负责实际过滤、固定时段和计数器；rosboard 负责期望状态、协调、每日账本和审计。
4. 先交付永久阻断，再交付允许窗口，最后交付每日活跃配额。

非目标：

- 不劫持 DNS，不封锁 DoH/DoT/VPN/代理，不声称不可绕过。
- 不做 TLS 解密、HTTPS 说明页、进程级应用识别或屏幕使用时间统计。
- 不自动跨 RouterOS 复制策略，不把同一 MAC 在不同设备间合并。
- 不在第一版引入域名/IP 之外的新来源类型；现有 `kind=domain` 和 `kind=ip` 都可作为访问控制目标。

## 2. 组件边界

```text
Browser
  └─ /api/access-control/devices/{deviceID}/*
       ├─ auth / same-origin / device validation
       └─ accesscontrol.Manager
            ├─ AccessRepository ─ device SQLite
            ├─ TerminalResolver ─ MonitorManager current snapshot
            ├─ SourceReader ───── PolicyRepository source/rules
            ├─ Reconciler ─────── RouterOS allowlisted mutation client
            └─ UsageController ── filter counters + daily ledger

policyv2.Manager
  └─ source-level address-list materialization + policy routing

routeros.DeviceWriteGate
  └─ serialize policy-routing and access-control writes per RouterOS device
```

新增 `internal/accesscontrol`，只拥有访问策略、终端地址投影、filter 期望状态和用量算法。
来源解析仍在 `internal/policy`，来源/版本仍在 `internal/policyv2` + `internal/store/policy.go`，
RouterOS REST 细节仍在 `internal/routeros`。

## 3. 来源级 RouterOS 列表

### 3.1 稳定名称

每个来源使用稳定且可读的列表名：

```text
rb_src_<sanitized-source-name>_<short-source-id>
```

名称只由 device identity + source ID 决定；来源改名只更新可读 comment，不替换稳定后缀。
`policyv2.SourceListName` 是策略路由和访问控制唯一共享的命名函数。

### 3.2 域名来源

- 每条 DNS Static 的 `address-list` 指向来源级列表。
- 来源绑定并启用策略出口时，继续使用该出口的 named forwarder/Fake DNS 路径。
- 来源只被访问控制引用或其出口停用时，使用设备级 access forwarder；其上游从当前
  RouterOS `/ip/dns servers` 与 `dynamic-servers` 的可证明有效值生成。没有可用上游时阻止应用。
- 来源未被策略路由或访问控制引用时，不物化 RouterOS DNS 对象。

### 3.3 IP 来源

- 每条 IPv4/IPv6 CIDR 直接写入来源级地址列表。
- 同一来源后续开放给访问控制时无需再次迁移或复制。

### 3.4 shared 出口的新含义

- shared 不再表示“所有来源共用一个 address-list”。
- shared 出口中，每个来源各有一条 `mark-connection dst-address-list=<source-list>`，但
  全部写入同一个 egress/family connection mark。
- 每个 egress/family 只有一条 `mark-routing connection-mark=<egress-mark>`。
- dedicated 旧数据第一轮保留其现有每来源 mark，避免无必要的连接标记迁移；验收后
  再决定是否统一 schema。当前任务不删除旧字段。

## 4. Shared 列表在线迁移

迁移按设备和出口执行：

1. 创建来源级 IP 静态成员和新的 source-list mark-connection 规则，均写入现有 shared connection mark。
2. 保留旧 shared-list mark-connection 兼容规则。
3. patch DNS Static 的 `address-list` 到来源级列表，然后 flush RouterOS DNS cache。
4. 新查询填充来源级动态列表；旧 shared 动态成员在 TTL 内仍由兼容规则继续路由。
5. 扫描旧 shared 列表。静态 IP 成员确认已复制后删除；动态成员未清空前保留兼容规则。
6. 旧列表为空且远端人工验收通过后，后续 cleanup apply 删除兼容规则。

迁移状态保存在 `policy_v2_device_state` 的新字段/JSON 中，至少包含
`none|dual|cleanup_ready|complete`，防止重启后直接跳过兼容阶段。旧数据库字段不删除，
回滚必须恢复部署前 NAS 备份中的二进制、配置和对应 SQLite。

## 5. 访问规则模型

用户面对的是一条逻辑 `AccessRule`，而不是“一个终端 × 一个来源”的 RouterOS 展开项：

```text
AccessRule
  id
  name
  clients[]                    # 一个或多个成员
  target_scope = internet | sources
  source_ids[]                 # 仅 sources；一个或多个
  mode = deny | limited
  daily_quota_seconds?         # limited 可选，规则组共享
  windows[]                    # limited 可选；空表示全天
  enabled
  revision

AccessRuleClient
  rule_id
  terminal_id
  binding = auto | fixed
  anchor_mac?                  # auto 的稳定身份锚点
  fixed_ipv4[] / fixed_ipv6[]  # 仅 fixed
```

- 一条规则至少包含一个 client；`sources` 至少包含一个 source，`internet` 的 `source_ids` 必须为空。
- `internet` 是一级访问范围语义，不能通过特殊 source ID、`0.0.0.0/0` 或 `::/0` 伪装成普通来源。
- `deny` 表示禁止命中范围；`limited` 至少配置窗口或配额之一，空窗口表示全天允许。
- 多终端规则的每日配额是一个共享池，ledger 以 `(device_id, rule_id, RouterOS local_date)` 为键；各成员有效活跃秒数相加进入同一池，两台同时活跃一分钟消耗两分钟额度。
- 来源被任一规则引用时不能删除；来源版本更新后列表名稳定，访问 filter 无需换目标身份。
- 不同来源运行时 IP 交集仍遵循“任一命中阻断即阻断”；共享 CDN/IP 可能扩大命中范围，UI 必须继续明确能力边界。
- RouterOS 可以按成员、来源、方向展开多个内部对象，但这些对象不是用户可独立编辑的访问规则。

## 6. 终端身份投影

### Auto-follow（默认）

- UI 只表达“自动跟随设备地址（推荐）”，不要求用户理解 MAC 模式；底层保存 terminal ID 与可靠 MAC anchor。
- 当前 Monitor snapshot 能解析时，Reconciler 精确同步该成员的 IPv4/IPv6。
- 成员暂时离线或快照暂不可解析时，不再让整个设备 fail-closed：该成员进入 `degraded/unresolved`，其他规则与成员继续 reconcile。
- 对 unresolved 成员采用三态处理：`resolved` 精确 reconcile；`temporarily_unresolved` 保留最后一次已成功应用且没有相反身份事实的投影，不主动删；`conflicted/reassigned` 在确认旧地址已属于其他身份时移除错误投影，等待新地址重新解析。
- 新建 auto-follow 成员必须至少有一次可靠身份和地址解析，不能从完全未知状态创建一个不可执行规则。
- `Store.MergeTerminal` 在同一事务迁移 auto-follow 成员的 terminal ID，并保持 MAC anchor / logical membership 不变。

### Fixed IP（高级）

- 无可靠设备身份或用户明确选择高级模式时保存精确 IPv4/IPv6；后续终端合并或地址变化不自动迁移。
- 无有效 fixed IP 时阻止启用。

终端成员列表名仍使用稳定 hash；RouterOS comment identity 统一使用 `rb_<8位小写十六进制哈希>`，访问控制扫描通过 desired identity 映射区分对象，不再引入 `ra_` 或长的实例/设备复合前缀。

## 7. RouterOS filter 结构

逻辑规则由 desired builder 编译为 RouterOS 内部对象。

### `target_scope=sources`

每个成员与目标来源按 IPv4/IPv6 生成双向 jump：

```text
forward src-list=<member> dst-list=<source> action=jump jump-target=<rule-chain>
forward src-list=<source> dst-list=<member> action=jump jump-target=<rule-chain>
```

多个 client × 多个 source 只是在执行层展开，用户仍看到一条规则。

### `target_scope=internet`

“互联网”定义为当前 RouterOS 终端本地网络范围之外的转发流量。rosboard 复用 Monitor 已计算的 `TerminalScope.Prefixes` 构建受管 local-prefix 列表；该范围本身已经排除已识别 WAN/tunnel 前缀。每个成员生成出站/回程入口，先允许本地范围返回原 forward，再对非本地流量执行规则动作：

```text
forward src-list=<member> action=jump jump-target=<rule-out-chain>
rule-out-chain dst-address-list=<local-prefixes> action=return
rule-out-chain ... deny/limited actions ...

forward dst-list=<member> action=jump jump-target=<rule-in-chain>
rule-in-chain src-address-list=<local-prefixes> action=return
rule-in-chain ... deny/limited actions ...
```

这样“禁止整个互联网”默认不阻止 NAS、打印机和其他已识别本地网段访问，也不依赖一个恰好叫 `WAN` 的接口列表。若本地范围不可证明，不能退化为无条件 drop。

所有受管 jump 都必须位于宽泛 LAN accept、established/related accept 和 FastTrack 之前；允许状态只 `return`，绝不 `accept`。

规则子链顺序：

1. 临时放行成员命中 -> `return`。
2. `limited` 且共享配额未耗尽：一个或多个 `time`/`days` -> `return`；空窗口使用无时间 matcher 的 return。
3. TCP -> `reject reject-with=tcp-reset`。
4. UDP -> `drop`。
5. 其他协议 -> `drop`。

`deny` 没有普通 return。达到共享配额时整条逻辑规则的普通 return 一起关闭。规则顺序通过 RouterOS `move` 读回验证；无法解释的 chain/comment 冲突或 move 失败时停止。

## 8. 时间、配额与人工干预

### 固定窗口

- RouterOS 本地 `time-zone-name` 和 NTP 状态是唯一时间基线。
- 跨午夜窗口拆分，相邻/重叠窗口合并。
- NTP 未同步或时区不可读时，带窗口策略阻止应用；永久 deny 仍可使用。

### 活跃配额

- 每设备每 10 秒各批量读取一次 IPv4/IPv6 filter，不按策略单独请求。
- 双向 delta 合计达到 4 KiB 或 12 packets 为 meaningful activity。
- 30 秒 idle grace；采样间隔超过 20 秒关闭会话且不补算。
- ledger key：`(device_id, rule_id, RouterOS local_date)`；成员/方向 counter 分别采样后将有效活跃秒数累计到同一个规则账本。
- 计数回退、RouterOS `.id` 变化或重启只重建 baseline。
- 达到限额后最多超出一个正常采样周期；必要时删除匹配 conntrack，处理未来 FastTrack 存量连接。

### 临时放行

- 为该策略写入 terminal override address-list entries，timeout 精确到 RouterOS 本地午夜。
- 子链前两条规则分别匹配出站 `src-address-list` 和回程 `dst-address-list` 并 return；
  即使 rosboard 离线，RouterOS timeout 也会结束放行。
- RouterOS 重启导致 dynamic override 提前消失是安全降级，不延长放行。
- 临时放行期间冻结配额，不把超额访问写成大于 quota 的 used seconds。

### 重置今日用量

- SQLite 事务把当天 used seconds 置零、关闭会话、保留 `reset_count` 并写审计。
- 事务成功后在 device write gate 内恢复普通 return，并读回验证。

## 9. SQLite

设备数据库新增五张表，全部包含 `device_id`：

| 表 | 关键字段 |
|---|---|
| `access_rules` | rule/name/target-scope/mode/quota/enabled/revision/timestamps |
| `access_rule_clients` | `(device_id, rule_id, member_id)`、terminal/binding/MAC anchor/fixed addresses |
| `access_rule_sources` | `(device_id, rule_id, source_id)`；仅 `sources` scope |
| `access_rule_windows` | `(device_id, rule_id, weekday, start_second, end_second)` |
| `access_usage_daily` | `(device_id, rule_id, local_date)`、共享 used seconds、unobserved、reset count、updated at |
| `access_counter_state` | rule/member/source-or-internet/family/direction/router ID/bytes/packets/observed at/session state |
| `access_audit` | actor/action/policy/before JSON/after JSON/timestamp/result |

策略与窗口保存、当日重置、终端身份 merge 都使用事务。大型来源规则不复制到访问控制表。

## 10. API 与前端

API：

```text
GET    /api/access-control/devices/{deviceID}
POST   /api/access-control/devices/{deviceID}/policies
PUT    /api/access-control/devices/{deviceID}/policies/{policyID}
DELETE /api/access-control/devices/{deviceID}/policies/{policyID}
POST   /api/access-control/devices/{deviceID}/policies/{policyID}/override
POST   /api/access-control/devices/{deviceID}/policies/{policyID}/usage/reset
POST   /api/access-control/devices/{deviceID}/sync
GET    /api/access-control/devices/{deviceID}/jobs/{jobID}
```

结构变更保存 desired state 后生成一个设备级 sync job；同设备写入串行，不同设备并行。
响应区分 desired、applied、degraded、blocked 和 unobserved。

前端新增 `web/src/features/access-control/`：

- 当前 RouterOS 设备内的逻辑规则列表，主列展示规则名、设备数量、访问范围、生效条件和状态；身份/IP 细节进入详情而非主表。
- 创建/编辑抽屉：规则名、多选设备、`整个互联网 | 指定网站/IP`、多选来源、禁止访问或限制访问、允许窗口、共享每日配额。
- 默认不暴露 MAC/IP；高级设置才允许将单个成员切为固定 IP。
- 主操作只保留“新增规则”；保存即触发自动 apply。常驻“同步 RouterOS”和“保存并同步”文案移除，只有 failed/degraded/drift 时显示“重新应用”。
- 状态列：当前允许/阻断、原因、共享今日用量、RouterOS 时区、未观测/成员 degraded 标记。
- 行操作：启停、临时放行至午夜、重置今日用量、删除。
- 不展示无法兑现的“应用识别准确率”或“防绕过”文案。

## 11. 并发与失败

- 新增共享 `routeros.DeviceWriteGate`，policy-routing apply、access sync、quota toggle 和
  override/reset 都必须获取同一 device lock。
- `MutationOutcomeUnknownError` 停止后续写入，重新扫描确定状态，不盲目重试。
- rosboard 离线：已有 RouterOS filter/time 继续；未观测时段不扣配额。
- RouterOS 离线：desired 保留，job degraded；恢复后重新扫描和 reconcile。
- 来源无 active/pending version、DNS upstream 不可证明、终端无地址、时区/NTP 不可用、
  filter 顺序不能置顶或 foreign chain 冲突均返回 blocker。

## 12. 分阶段交付

1. **来源级列表迁移 + 永久阻断**：先验证来源可同时用于路由和控制。
2. **允许访问窗口**：只增加 RouterOS 原生 time/days 行为。
3. **每日活跃配额 + 人工干预**：最后增加后台计量和 ledger。

每一阶段都是独立 Trellis 子任务、独立部署和独立人工验收。前一阶段通过远端验收并
提交后才能开始下一阶段。

## 13. 官方依据

- [RouterOS DNS](https://manual.mikrotik.com/docs/network-management/dns/)
- [IPv4 firewall filter](https://manual.mikrotik.com/docs/cli-reference/ip/firewall/filter/)
- [IPv6 firewall filter](https://manual.mikrotik.com/docs/cli-reference/ipv6/firewall/filter/)
- [Policy Routing](https://manual.mikrotik.com/docs/network-management/routing/policy-routing/)
- [Connection tracking](https://manual.mikrotik.com/docs/firewall-and-quality-of-service/connection-tracking/)
- [REST API](https://help.mikrotik.com/docs/spaces/ROS/pages/47579162/REST+API)
