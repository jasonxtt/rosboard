# RouterOS 域名访问控制调研

日期：2026-08-30

## 结论摘要

可以做，但应拆成三个能力层级：

1. **域名阻断**：可直接落在 RouterOS。复用现有域名规则，借助 DNS Static
   将解析结果加入目的地址列表，再用按终端源地址列表与目的地址列表组合的
   IPv4/IPv6 `forward` 过滤规则阻断。
2. **固定时段**：RouterOS 防火墙原生支持 `time`/`days` 匹配，可由受管规则
   在路由器本地直接执行，不要求 rosboard 在每个边界时刻在线。
3. **每日累计时长**：RouterOS 没有“某终端访问某域名列表累计 N 分钟”的
   原生配额对象。需要从规则计数器或连接跟踪定义“正在使用”，再由 RouterOS
   Scheduler 脚本或 rosboard 状态机累计并切换阻断规则。

推荐先完成域名阻断和固定时段；累计配额单独作为第二阶段，不把未经确认的
计时语义塞进第一版。

## 现有项目复用点

### 域名列表与 RouterOS 物化

- `internal/policy` 已支持手工、上传和 URL 来源，并规范化 `DOMAIN`、
  `DOMAIN-SUFFIX`。
- `internal/policyv2/desired.go` 已把域名规则写成 `/ip dns static type=FWD`，
  使用 `address-list` 和 `match-subdomain` 将解析出的 A/AAAA 地址动态加入列表。
- 现有策略路由随后以 `dst-address-list` 做 mangle 标记。访问控制可以复用
  **规则来源和 DNS 物化语义**，但不应依赖某个策略出口是否存在。
- 未绑定出口的域名列表目前不会在 RouterOS 中物化；访问控制需要独立的
  destination-list 生命周期，或经过明确所有权设计后共享已有对象。

### RouterOS 写入边界

- `internal/routeros/policy_types.go` 的 mutation allow-list 已包含 IPv4/IPv6
  filter、address-list、DNS Static 和 `move`。
- `policyv2` 当前刻意没有把 filter 纳入其受管菜单。访问控制适合使用独立
  manager/reconciler 和独立 comment identity，避免策略路由生命周期误删过滤规则。
- 现有 reconciler 只调整受管对象之间的相对顺序。访问控制的入口 jump 必须能
  以经过扫描证明的**外部规则 ID**为锚点，放在宽泛 LAN accept、FastTrack 和
  established accept 之前；否则规则会存在但不生效。

### 终端身份和统计

- 终端以稳定 terminal ID 保存，优先由 MAC 聚合，并维护 IPv4/IPv6 地址集合。
- 默认终端轮询周期为 5 秒；RouterOS connection tracking 已提供
  `orig-bytes`、`repl-bytes`、connection mark 等数据。
- `ApplicationResolver` 已能使用每设备 MosDNS 把 client IP + answer IP 关联到域名，
  但这套识别是监控增强，不应成为阻断的唯一证据。
- SQLite 目前只累计终端总流量，没有“终端 + 域名列表 + 日期”的使用账本。

## 现网只读核验

### RouterOS 基线

| 设备 | RouterOS | 形态 | DNS | IPv4/IPv6 conntrack | FastTrack |
|---|---|---|---|---|---|
| `unicom` | 7.21.5 long-term | CHR x86_64 | remote requests 开启，未配置 DoH | 502 / 187（探测时） | 0 |
| `cmcc` | 7.21.5 long-term | CHR x86_64 | remote requests 开启，未配置 DoH | 109 / 11（探测时） | 0 |

两台设备均能读取 DNS Static `address-list` 字段、filter 计数器、Kid Control 和
Scheduler 菜单，且 device mode 允许 Scheduler。

### 终端与来源

| 设备 | 终端 | 有 MAC | 已记录 IPv4/IPv6 | DHCP | 协议分析/MosDNS |
|---|---:|---:|---:|---|---|
| `unicom` | 48 | 39 | 56 / 50 | 13 静态、8 动态 lease | 开启 |
| `cmcc` | 21 | 12 | 23 / 17 | 本机无 DHCP lease | 关闭 |

`unicom` 已有未绑定出口的 Google、Claude、GitHub、Telegram、Youtube 等域名列表，
说明访问控制必须允许直接引用“列表库”，不能要求它先绑定策略出口。

### 防火墙顺序

- `unicom` IPv4 在位置 2、IPv6 在位置 1 已有宽泛 `LAN -> accept`；随后才是
  established/related accept。
- `cmcc` IPv4/IPv6 的位置 0 就是宽泛 forward accept。
- 因此访问控制入口必须放到这些 accept 之前，并在未来出现 FastTrack 时仍保持
  在 FastTrack 之前。单纯把 drop 规则追加到链尾不会生效。

### 时间基线

- `unicom`：`Asia/Taipei`，NTP synchronized。
- `cmcc`：`Asia/Shanghai`，NTP synchronized。
- 两者当前都是 UTC+8，但时间策略仍应显示并校验 RouterOS 的实际时区，不能
  默认采用浏览器或 rosboard 主机时区。

## MikroTik 官方能力边界

### DNS Static address-list

RouterOS DNS Static 支持 `address-list` 和 `match-subdomain`。命中静态 DNS 规则后，
解析结果会以 DNS TTL 为 timeout 加入防火墙地址列表；这是现有策略路由已经使用的
官方机制。

边界：

- 只有经过该 RouterOS DNS resolver 的查询才会触发列表填充。
- 客户端自带 DoH/DoT、VPN、硬编码 IP 或已有缓存时可能绕过 DNS 物化。
- 一个 CDN IP 可同时承载允许与禁止域名，IP 级阻断可能扩大命中范围。
- TLS `tls-host` 可作为小规模补充证据，但依赖可见 SNI；分片 ClientHello、QUIC/HTTP3
  和加密 ClientHello 不能作为完整覆盖基础。

官方资料：

- [DNS](https://manual.mikrotik.com/docs/network-management/dns/)
- [Firewall filter matchers](https://manual.mikrotik.com/docs/cli-reference/ip/firewall/filter/)

### 防火墙时间与计数

- filter 规则支持 `time` 和 `days`，适合固定允许/禁止窗口。
- filter 规则暴露 `bytes`、`packets`；connection tracking 暴露每条连接的方向字节数。
- 这些数据能回答“是否有流量”和“流量多少”，不能原生回答“今天累计主动使用了多少分钟”。

官方资料：

- [Connection tracking](https://manual.mikrotik.com/docs/firewall-and-quality-of-service/connection-tracking/)
- [REST API](https://help.mikrotik.com/docs/spaces/ROS/pages/47579162/REST+API)

### Kid Control

Kid Control 提供按设备的周时间表、暂停、速率和流量字段，但 profile 对设备的整个
Internet 访问生效，不支持“只对某个域名列表累计 1 小时”。它可作为未来“整机上网
时间”功能参考，不能替代本任务的域名级策略。

官方资料：[Kid Control](https://manual.mikrotik.com/docs/network-management/kid-control/)

### Scheduler 与脚本

RouterOS Scheduler 可以周期运行脚本，因此技术上能每分钟读取规则计数并切换阻断。
但每日累计状态仍需解决重启持久化、脚本升级、计数器重置、跨午夜、夏令时/时区、
手工改动和多策略资源占用。第一版若把全部状态机写入 RouterOS script，会明显增加
故障面和回滚难度。

官方资料：[Scheduler](https://help.mikrotik.com/docs/spaces/ROS/pages/40992881/Scheduler)

## 推荐分阶段方案

### Phase 1：域名阻断

1. 新建独立 access-control 期望状态，引用 terminal ID 和 domain source ID。
2. 为每个受控终端维护精确所有权的 IPv4/IPv6 source address-list。
3. 为被引用域名来源维护 access-control 专用 destination list；复用域名解析规则，
   不依赖策略出口生命周期。
4. 在 IPv4/IPv6 forward 顶部插入一个受管 jump，进入独立 chain；规则只 drop/reject
   或 return，绝不用 accept 绕过用户现有安全规则。
5. 应用前证明 jump 位于宽泛 accept、FastTrack 和 established accept 之前；证明失败
   则 block plan。
6. 启用阻断时处理现有 conntrack，否则旧 TCP/UDP 流可能继续一段时间。

### Phase 1.5：固定时段

- 在策略子链内使用一个或多个 `time`/`days` return 窗口，窗口外落到 drop/reject。
- RouterOS 本地执行；rosboard 负责生成、读回和展示下一次状态变化。
- 应用前要求 NTP 已同步，并明确展示 RouterOS 时区。

### Phase 2：每日累计时长

推荐先采用混合状态机：

- **RouterOS**：保存专用计数规则、实际 block 规则和当前执行状态。
- **rosboard**：轮询计数器/连接，按已确认口径累计 active seconds，持久化每日账本，
  达到配额后 patch RouterOS block 状态并读回确认。
- 路由器或 rosboard 重启时，以 RouterOS 当前规则、计数器 generation 和 SQLite ledger
  进行恢复，不能把计数器回退误算为新流量。

不推荐第一版直接使用 RouterOS-only 脚本状态机；如果产品要求 rosboard 离线期间仍能
继续精确累计和触发配额，再单独设计 RouterOS 持久状态文件与脚本升级协议。

## 已确认决策

- 访问控制不是 `unicom` 专属能力。每台已纳管 RouterOS 都有独立的终端、域名来源、
  访问策略和 RouterOS 受管对象。
- 同一终端在不同 RouterOS 上出现时，不自动跨设备复制或联动策略；用户在当前设备
  上独立配置和应用。
- 有 MAC 的终端使用 MAC-backed 身份，访问控制自动同步该 RouterOS 当前观测到的
  IPv4/IPv6 地址。没有 MAC 的终端仍可配置，但使用 IP-bound 身份：创建时冻结用户
  确认的精确 IP，后续不自动跟随地址变化或终端合并。
- 第一版不强制劫持、重定向或封锁终端 DNS，不额外封锁 DoT、DoH、VPN、代理和
  硬编码 IP。访问控制只对 RouterOS DNS Static/address-list 已学习到的目的地址生效，
  并在 UI 中明确标记为可绕过的尽力控制。
- 每日配额采用网络活跃时间，不采用“首次访问后连续倒计时”。计量源是访问策略在
  RouterOS 中的双向 jump 规则计数器；MosDNS 和应用标签只用于展示，不参与扣时。
- 管理员可以临时放行到 RouterOS 本地午夜，也可以显式重置当日用量。两者都需要
  确认和审计；临时放行不修改原策略或已累计用量。
- 同一个域名或 IP 来源必须能同时服务策略路由和访问控制。当前把多个来源成员混入
  一个出口 shared address-list 的物理模型将迁移为来源级稳定列表；多个来源继续共享
  出口 connection mark 和路由表。

## Shared address-list 迁移决策

- 当前 `shared` 模式把同一出口的全部域名解析结果和 IP/CIDR 条目写入一个列表，
  适合路由但丢失来源身份，无法只阻断其中一个来源。
- RouterOS DNS Static 每条匹配只有一个 `address-list` 目标，且规则按顺序匹配，不能
  依赖重复 DNS Static 同时维护“出口聚合列表”和“来源独立列表”。
- 新模型为每个来源生成稳定列表。shared 出口下的多个来源分别匹配各自列表，但写入
  同一个出口 connection mark；每地址族只保留一个 mark-routing 规则。
- 迁移时保留旧 shared-list mark-connection 兼容规则，直到旧动态成员自然过期并通过
  读回确认为空；IP 静态成员先复制到来源列表再清理旧条目。
- 第一轮迁移不删除数据库 `list_mode`/`list_name` 字段，保证旧二进制与迁移前数据库
  一起回滚时仍有完整数据；前端在新模型验收后再移除过时设置。

## 活跃时间算法

### 采样与活跃门槛

- access-control controller 每 10 秒按设备批量读取全部受管 IPv4/IPv6 filter 规则，
  不为每条策略单独发请求。
- 每条策略使用出站和回程两个 jump 规则的 `bytes`、`packets` 增量合计。
- 一个采样窗口内增加至少 4 KiB 或 12 个包，判定为 meaningful activity。该固定门槛
  用于过滤低频 TCP keepalive、HTTP/2 ping 和少量后台探测，不在第一版暴露配置项。
- 规则首次出现、RouterOS `.id` 变化、计数器变小或设备刚恢复连接时只记录 baseline，
  本窗口不补算流量。

### 会话与累计

- 首个 meaningful sample 打开活跃会话；会话从上一个正常采样边界开始计入，单次
  最多计 10 秒。
- 后续 meaningful sample 继续累计实际采样间隔。
- 暂时没有 meaningful delta 时，保留 30 秒 idle grace，以覆盖视频分段缓冲和短暂停顿；
  超过 30 秒后会话关闭，不再累计。
- 任意一次采样间隔超过 20 秒时视为观测中断：不补算缺失区间，关闭当前活跃会话，
  恢复后重新建立 baseline。
- 这一定义衡量的是“目标地址列表上的网络活跃时间”，不是屏幕亮屏、播放器状态或
  人眼注意力。共享 CDN IP 和未经过 RouterOS DNS 的流量仍受前述能力边界影响。

### 每日边界与触发

- ledger key 为 `(device_id, access_policy_id, RouterOS local date)`；RouterOS 时区是日期
  边界的唯一来源，UI 同时展示该时区。
- 达到配额后在下一个采样内启用 RouterOS block；理论最大超额约 10 秒。
- RouterOS 重启、规则重建或手工 reset counters 不清空 SQLite 已累计时间。只要观察到
  counter generation 改变或负增量，就从新 baseline 继续。
- 跨午夜关闭前一天会话，新日期从零开始；不把午夜两侧一个采样窗口重复计入。

### 离线行为

- rosboard 停机、升级或暂时无法读取 RouterOS 时，不根据断线前状态猜测活跃时间，
  也不建立 RouterOS heartbeat fail-closed 机制。
- RouterOS 已经启用的永久阻断、时段阻断和配额阻断继续生效；尚未触发配额阻断的
  策略保持允许。
- 恢复连接后建立新 counter baseline，缺失区间记为 `unobserved` 且不计入 used seconds。
  UI 在当日用量旁展示未观测状态，避免把不完整数据显示成精确统计。

## 阻断动作官方核验

- MikroTik 官方 IPv4 和 IPv6 firewall filter CLI reference 都定义了 `action=reject`
  和 `reject-with=tcp-reset`；UDP 与其他协议使用官方 `action=drop`。
- 2026-08-30 对现网两台 RouterOS 7.21.5 long-term 执行只读
  `/console/inspect request=completion`：`unicom` 与 `cmcc` 的 IPv4、IPv6 filter
  都返回 `tcp-reset` completion。该探测只读取 CLI 语法元数据，没有创建或修改规则。
- 实现时仍需把 `reject-with` 纳入 rosboard 的受限 RouterOS 字段 allow-list，并在每台
  RouterOS 应用前执行能力证明；不能因为当前两台支持就取消设备级验证。

官方资料：

- [IPv4 firewall filter CLI reference](https://manual.mikrotik.com/docs/cli-reference/ip/firewall/filter/)
- [IPv6 firewall filter CLI reference](https://manual.mikrotik.com/docs/cli-reference/ipv6/firewall/filter/)
- [Console inspect CLI reference](https://manual.mikrotik.com/docs/cli-reference/console/)

## 固定时段语义

- 产品只提供“允许访问窗口”，不同时提供正反两套时间语义。
- 未配置窗口表示全天允许；配置一个或多个星期/时间窗口后，窗口外进入阻断。
- 跨午夜窗口由 rosboard 拆成前一天到 `23:59:59`、后一天从 `00:00:00` 开始的
  RouterOS `time`/`days` matcher；重叠和相邻窗口先规范化合并。
- 每日配额只在当前处于允许窗口时读取和累计活跃时间。窗口外即使因规则漂移观察到
  计数变化，也不能扣减当天剩余额度，而应报告执行异常。
