# 来源级列表与永久域名访问阻断

## Goal

在每台受管 RouterOS 上独立配置低心智负担的访问阻断规则：一条逻辑规则可选择多个终端，并阻断整个互联网或多个域名/IP来源；同时保持来源级 RouterOS 地址列表能力，使同一来源可以同时被策略路由和访问控制引用。

## Requirements

- 每个 domain/IP 来源必须拥有稳定的 RouterOS address-list 名称；来源改名不能改变稳定身份。
- shared 出口继续共享 connection mark、routing mark 和路由表，但每个来源使用自己的 destination address-list 和 mark-connection 规则。
- 现有 shared 地址列表必须经过可恢复的 dual migration；旧动态成员清空且人工验收前不得删除兼容规则。
- 域名来源由 RouterOS 官方 DNS Static `address-list` 能力填充；IP 来源把 IPv4/IPv6 CIDR 直接物化到来源级列表。
- 同一来源可以只用于策略路由、只用于访问控制，或同时用于两者；未被任何功能引用的来源不在 RouterOS 上物化。
- 一条逻辑访问规则支持当前 RouterOS 的一个或多个终端；不同 RouterOS 的规则、终端和应用状态互不传播。
- 访问范围支持 `internet` 与 `sources`：`internet` 默认保留 rosboard TerminalScope 已识别的本地网络访问；`sources` 支持选择一个或多个 domain/IP 来源。
- `internet` 是一级规则语义，不通过特殊 source ID 或用户可见的 `0.0.0.0/0`、`::/0` 来源模拟。
- 默认按设备自动跟随地址：有可靠 MAC 的成员跟随当前 IPv4/IPv6；固定 IP 仅用于无可靠身份或高级设置。
- 自动跟随成员暂时不可解析时不阻塞本设备其他规则同步；保持最后已证明投影并标记 degraded，发现旧地址已归属其他身份时移除该错误投影。
- 永久阻断必须覆盖双向 IPv4/IPv6 forward 流量。TCP 使用 RouterOS 官方 `reject-with=tcp-reset`，UDP 和其他协议使用 `drop`。
- 受管 jump 必须位于现有宽泛 accept、established/related accept 和 FastTrack 之前；访问控制链只允许 `return`、`reject` 或 `drop`，不得用 `accept` 绕过现有防火墙。
- rosboard 只通过 RouterOS typed allow-list 和精确 ownership comment 写入；冲突、未知 mutation outcome 或无法验证规则顺序时停止应用。
- 提供设备级访问控制概览、逻辑规则 CRUD、自动应用状态和对应前端操作界面。
- 新增规则 UI 使用“规则名称 → 多选设备 → 整个互联网/指定网站或IP → 多选来源 → 禁止访问”的流程；默认隐藏 MAC/IP 身份实现细节。
- 页面与弹窗不再使用“永久阻断”“保存并同步”作为一级文案；主表不展示身份/地址列，常驻“同步 RouterOS”按钮移除，只在应用异常时提供“重新应用”。
- 不劫持 DNS，不封锁 DoH/DoT/VPN/代理，不承诺不可绕过；界面必须明确这一边界。
- 本子任务不实现允许时间窗口、每日活跃时长配额、临时放行或当日用量重置；但本阶段的数据模型必须以逻辑 rule 为聚合单位，为后续“多成员共享一个配额池”留出正确主键边界，不能继续以单 terminal/source policy 作为用户规则主实体。

## Acceptance Criteria

- [ ] shared/dedicated、domain/IP、IPv4/IPv6 的来源级 desired state 与迁移测试通过，重复 apply 幂等。
- [ ] 旧 shared 动态列表在 dual migration 期间继续路由；重启后可恢复迁移状态，未人工验收前不会提前 cleanup。
- [ ] 同一来源同时用于策略路由和访问控制时，既有策略出口、connection mark 和 route table 保持不变。
- [ ] `unicom` 与 `cmcc` 可分别创建访问规则，任一设备的保存或自动应用不会修改另一设备。
- [ ] 一条规则可选择多个终端，并分别验证 `internet` 与多 source 阻断；用户界面始终只显示一条逻辑规则而不是 Cartesian product 展开项。
- [ ] 自动跟随与 fixed IP 成员地址投影符合要求；单个 auto 成员离线不会全局阻断同步，来源删除在仍被规则引用时被拒绝。
- [ ] IPv4/IPv6 filter jump 位于 forward 顶部受管块；TCP reset、UDP/other drop 和双向命中均通过读回及测试验证。
- [ ] API 合约、前端 lint/build、Go 全量测试、targeted race、vet 和 diff check 通过。
- [ ] 部署前按 `AGENTS.md` 将现有 binary/config/SQLite/service unit 备份到 NAS，并将备份目录保持在最多 10 份。
- [ ] 部署到 `10.0.0.6` 后 systemd、health、相关 API、嵌入式前端和两台 RouterOS 实际行为验证通过。
- [ ] 用户完成远端人工验收并明确批准后，才允许提交和归档本子任务。

## Notes

- Parent requirement set: `../08-30-domain-access-control/prd.md`.
- Approved parent design: `../08-30-domain-access-control/design.md`.
- RouterOS research: `../08-30-domain-access-control/research/routeros-domain-access-control.md`.
