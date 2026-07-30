# 设计文档：DHCP 面板、路由页增强与状态监控菜单重组

- 日期：2026-07-30
- 状态：待用户审阅
- 背景：用户反馈"展示的数据有点少，DHCP、路由之类的数据还没"。本设计新增 DHCP 独立展示页、增强现有路由页，并重组"状态监控"二级菜单以容纳本次及未来的监控类型。

## 1. 目标与范围

**做：**

1. 新增 DHCP 页：DHCP server 概况、地址池利用率、lease 列表。
2. 增强路由页：补全路由条目字段，按路由表分组并汇总。
3. 重组侧边栏"状态监控"菜单：新增"网络服务"分组，"运行监控"改名"系统运行"。

**不做（YAGNI，明确排除）：**

- DHCP client（WAN 侧）状态展示——后端类型顺带补字段，前端本次不展示。
- DHCP / 路由的历史持久化（SQLite 采样表）。
- 路由页筛选/搜索、默认路由健康告警。
- 未来监控项（ARP 表、连接明细、DNS、NAT、VPN 等）只作菜单规划，不做占位菜单项，不实现。

**总体方案：** 沿用现有快照管线（方案 A）。DHCP 数据与 Routes/Policies 相同模式：全量 `refresh` 采集 → `DashboardSnapshot` 内存快照 → HTTP 端点 → 前端轮询 `/api/dashboard` 渲染。不引入新基础设施，不改轮询节奏。数据新鲜度跟随 `PollIntervalSeconds`，对 lease/路由这类低频变化数据足够。

## 2. 后端：RouterOS 客户端（internal/routeros/）

### 2.1 types.go 字段扩充

`DHCPLease`（现仅 6 个字段）增加：

| 字段 | JSON tag |
|---|---|
| ID | `.id` |
| ExpiresAfter | `expires-after` |
| LastSeen | `last-seen` |
| Dynamic | `dynamic` |
| Blocked | `blocked` |
| Disabled | `disabled` |
| ActiveAddress | `active-address` |
| ActiveMACAddress | `active-mac-address` |

`DHCPServer` 增加：`AddressPool`（`address-pool`）、`LeaseTime`（`lease-time`）。

`DHCPClient` 增加：`Address`（`address`）、`Gateway`（`gateway`）。仅补类型，本次前端不展示。

`RoutingRoute` 增加：`PrefSrc`（`pref-src`）、`Scope`（`scope`）、`TargetScope`（`target-scope`）、`ImmediateInterface`（`immediate-interface`）、`Static`（`static`）、`Connect`（`connect`）、`Dynamic`（`dynamic`）、`ECMP`（`ecmp`）、`Comment`（`comment`）。

`IPRoute`（兼容回退路径）增加：`PrefSrc`（`pref-src`）、`Static`（`static`）、`Connect`（`connect`）、`Comment`（`comment`）。

新增 `IPPool` 类型：`ID`（`.id`）、`Name`（`name`）、`Ranges`（`ranges`）、`Comment`（`comment`）。

所有字段与现有约定一致：string 类型，RouterOS REST 原样返回。

### 2.2 client.go

新增 `IPPools(ctx)` 方法，`GET /rest/ip/pool`，实现参照现有 `DHCPServers`（client.go:152）。其余端点已存在，无需改动。

## 3. 后端：投影模型（internal/model/types.go）

```go
type DHCPServerStat struct {
    Name        string `json:"name"`
    Interface   string `json:"interface"`
    AddressPool string `json:"addressPool"`
    LeaseTime   string `json:"leaseTime"`
    Disabled    bool   `json:"disabled"`
    Invalid     bool   `json:"invalid"`
}

type DHCPPoolStat struct {
    Name        string   `json:"name"`
    Ranges      string   `json:"ranges"`
    Total       int      `json:"total"`
    Used        int      `json:"used"`
    Free        int      `json:"free"`
    UsedPercent float64  `json:"usedPercent"`
    Servers     []string `json:"servers"`
}

type DHCPLeaseStat struct {
    ID           string `json:"id"`
    Address      string `json:"address"`
    MACAddress   string `json:"macAddress"`
    HostName     string `json:"hostName"`
    Comment      string `json:"comment"`
    Server       string `json:"server"`
    Status       string `json:"status"`       // bound / waiting / offered ...
    ExpiresAfter int64  `json:"expiresAfter"` // 秒；无值为 0
    LastSeen     int64  `json:"lastSeen"`     // 秒；无值为 0
    Dynamic      bool   `json:"dynamic"`
    Blocked      bool   `json:"blocked"`
    Disabled     bool   `json:"disabled"`
}

type DHCPStat struct {
    Servers []DHCPServerStat `json:"servers"`
    Pools   []DHCPPoolStat   `json:"pools"`
    Leases  []DHCPLeaseStat  `json:"leases"`
}
```

`DashboardSnapshot` 增加字段 `DHCP DHCPStat \`json:"dhcp"\``。

`RouteStat` 增加字段：`PrefSrc`、`Scope`、`TargetScope`、`ImmediateGateway`、`Protocol`、`Comment`（均 string，Scope/TargetScope 保持 RouterOS 原始字符串）。`Protocol` 为归一化值：`static` / `connected` / `dynamic`。

## 4. 后端：采集（internal/service/monitor.go）

### 4.1 refresh 流程

- 全量 `refresh`（monitor.go:848 起）已拉取 `DHCPServers`、`DHCPLeases`。新增拉取 `IPPools`。
- 失败处理沿用现有模式：采集失败时 addWarning + 保留上次快照中的对应数据；`buildCapabilities` 增加 `ip-pool` 数据源注记（参照现有可选数据源模式，monitor.go:1856）。DHCP 相关端点在设备未启用 DHCP 时返回空数组，属正常情况，不产生 warning。

### 4.2 buildDHCP

新增 `buildDHCP(servers []routeros.DHCPServer, leases []routeros.DHCPLease, pools []routeros.IPPool) model.DHCPStat`，纯函数，参照 `buildRoutes`（monitor.go:1258）模式：

- **Servers**：直接投影，`disabled`/`invalid` 解析为 bool（沿用现有 truthy 解析辅助函数）。
- **Pools**：
  - 容量解析：`ranges` 形如 `10.0.0.10-10.0.0.254`，可多段逗号分隔；也可能是单个地址。逐段解析 IPv4 起止地址计算地址数并累加。解析失败的段跳过并使 `Total` 保持已解析部分（不因个别异常段整体失败）。
  - `Used`：该池关联 server（通过 `DHCPServer.AddressPool == pool.Name`）名下 `status == "bound"` 的 lease 数。
  - `Servers`：引用该池的 server 名列表。无 server 引用的池也照常展示（Used=0）。
  - `UsedPercent`：`Total > 0` 时为 `Used/Total*100`，否则 0。
- **Leases**：直接投影。`ExpiresAfter`/`LastSeen` 用现有 RouterOS 时长字符串解析逻辑（如 `parseDuration` 类辅助函数；`1d2h3m4s` 格式）转为秒。地址优先取 `Address`，`ActiveAddress` 非空且不同（static lease 场景）时以 `ActiveAddress` 为展示地址。排序：按 IP 地址升序（数值序，非字典序）。

### 4.3 buildRoutes 增强

- 透传新字段 `PrefSrc`、`Scope`、`TargetScope`、`ImmediateGateway`（取 `immediate-gw`）、`Comment`。
- `Protocol` 归一化：`static=true → "static"`；`connect=true → "connected"`；否则（`dynamic=true` 或全无标志）→ `"dynamic"`。
- `IPRoute` 兼容回退路径同样透传其可用的新字段（`PrefSrc`、`Comment`、protocol 由 `static`/`connect` 推导），无值字段为空字符串。
- 现有字段、`CurrentMatches` 归因逻辑（routes.go 的 routeMatcher）不变。

## 5. API 层（internal/api/server.go）

- 新增 `GET /api/dhcp`：返回 `Snapshot().DHCP`，支持 `?device=<id>`（走 `scopedURL`），实现参照 `/api/routes`（server.go:251）。
- `GET /api/dashboard` 自动携带新 `dhcp` 字段，无需改动。
- `/api/routes` 响应仅增量加字段，向后兼容。
- 鉴权沿用现有 session 中间件，无变化。

## 6. 前端（web/src/）

### 6.1 类型（lib/types.ts）

- 新增 `DHCPServerStat`、`DHCPPoolStat`、`DHCPLeaseStat`、`DHCPStat` 接口，与第 3 节 JSON 字段一一对应。
- `DashboardResponse` 增加 `dhcp: DHCPStat`。
- `RouteStat` 增加 `prefSrc`、`scope`、`targetScope`、`immediateGateway`、`protocol`、`comment`。
- `ActiveView` 联合类型增加 `'dhcp'`；`landingViews`（App.tsx:193）同步加入。

### 6.2 DHCP 页（App.tsx 新增 DHCPPage 组件）

数据来源：复用现有 `/api/dashboard` 轮询结果，不新增轮询循环。页面结构自上而下：

1. **Server 概况卡片行**：每个 DHCP server 一张卡——名称、接口、地址池、lease 时长、状态（disabled/invalid 标红）。
2. **地址池利用率**：每池一行——名称、范围、已用/总量、利用率进度条（>85% 橙色、>95% 红色）、关联 server。
3. **Lease 表格**：列为地址、MAC、主机名、备注、server、状态徽章（bound 绿 / waiting|offered 灰 / blocked 红）、剩余到期时间（`format.ts` 时长格式化；0 显示 `—`）、动态/静态标记。默认按地址排序。表格上方本地搜索框，客户端过滤地址/MAC/主机名/备注。

**空态/降级**：设备未启用 DHCP server（servers 与 leases 均空）时显示空态提示"该设备未启用 DHCP Server 或接口无权限"；`ip-pool` 采集失败时池区块显示 capabilities 注记，其余区块正常。

### 6.3 路由页增强（RoutesPage，App.tsx:2134）

- **分组**：按 `table` 字段分组渲染。routing rules（`kind === 'rule'`）单独一节置顶（跨表入口）；路由组中 `main` 表置顶，其余表按名称排序。
- **组头汇总**：表名、条数、默认路由（`0.0.0.0/0` / `::/0`）的 active 状态、组内 `currentMatches` 合计。
- **新增列**：pref-src、协议来源徽章（static/connected/dynamic）、comment；scope/target-scope 作为次要信息收进悬浮提示（title 属性）。
- 现有列（目标/网关/距离/active/当前匹配连接）保留。

### 6.4 菜单重组（App.tsx:857-939）

现状问题："路由 / 分流"挂在"运行监控"下语义错位；分组维度混杂。

调整后结构：

```
系统概览
状态监控
├── 接口监控
├── 终端监控 ─ 全部 / IPv4 / IPv6
├── 流量监控 ─ 协议统计 / 策略统计
├── 网络服务 ─ DHCP / 路由 / 分流        ★ 新增分组
└── 系统运行 ─ 负载历史                   ★ "运行监控"改名，路由移出
面板设置
```

实现：`expandedMonitorGroup` 联合类型加 `'services'`；"路由 / 分流"按钮从 runtime 组移入 services 组；新增 DHCP 菜单项；"运行监控"文案改"系统运行"。页面组件、URL、现有类型不动。

分组原则（指导未来扩展，本次不实现未来项）：每组回答一个问题——接口=线路通不通；终端=谁在网上；流量=流量去哪了；网络服务=路由器提供的服务是否正常；系统运行=设备自身是否健康。未来监控类型的归宿：

| 组 | 未来可加入 |
|---|---|
| 接口监控 | 线路健康（多 WAN 检测、PPPoE/DHCP client 状态） |
| 终端监控 | ARP / ND 表、无线终端（registration table） |
| 流量监控 | 连接明细（conntrack）、队列 / QoS |
| 网络服务 | DNS、NAT / 端口映射、VPN 会话 |
| 系统运行 | 硬件健康（温度/电压/存储）、日志 / 告警 |

## 7. 测试

- `internal/service/monitor_test.go`：`buildDHCP` 单测——池 ranges 解析（单段/多段/单地址/含非法段）、used 计数（bound 过滤、server→pool 关联）、lease 时长解析与排序；`buildRoutes` 新字段透传与 protocol 归一化。
- `internal/api/server_test.go`：`/api/dhcp` 正常返回与 `?device=` 多设备 scope。
- 前端沿用现有约定（无既有组件测试框架则不新增）。

## 8. 部署与验收

遵守 AGENTS.md 门禁：改动完成后部署到 10.0.0.6（部署前对二进制/配置/SQLite 做时间戳备份），等待用户手动验收批准后 commit。实现前需阅读 `.trellis/spec/backend/monitoring-contracts.md` 及前端对应 spec。

## 9. 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/routeros/types.go` | DHCPLease/DHCPServer/DHCPClient/RoutingRoute/IPRoute 扩字段；新增 IPPool |
| `internal/routeros/client.go` | 新增 `IPPools()` |
| `internal/model/types.go` | 新增 DHCPStat 系列；RouteStat 扩字段；DashboardSnapshot 加 DHCP |
| `internal/service/monitor.go` | refresh 拉取 pools；新增 buildDHCP；buildRoutes 透传新字段 |
| `internal/api/server.go` | 新增 `/api/dhcp` |
| `web/src/lib/types.ts` | 新类型；ActiveView 加 `'dhcp'` |
| `web/src/App.tsx` | DHCPPage；RoutesPage 分组增强；菜单重组 |
| 对应 `*_test.go` | 见第 7 节 |
