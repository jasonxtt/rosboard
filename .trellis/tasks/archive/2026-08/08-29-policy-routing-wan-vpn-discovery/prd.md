# 修正策略路由 WAN、LAN 与 VPN 出口发现

## Goal

完善策略路由向导的 RouterOS 出口发现：明确配置为 LAN 的接口不能因为默认路由或自定义路由表而被误报为 WAN；WireGuard 和 RouterOS 支持的固定 VPN/隧道客户端接口应能作为策略出口候选。

## Requirements

- 以明确的 `LAN` RouterOS interface list 及其递归解析后的成员作为 LAN 角色证据。接口名称不能单独决定角色，不能用简单的 `lan` 字符串匹配替代拓扑证据。
- RouterOS REST 路由中省略的 `active` 字段必须按非活动处理；禁用或非活动默认路由不得成为“已验证 WAN”依据。
- WAN 发现应保留现有基于默认路由的普通物理/VLAN/Bridge 出口发现，并保留跨 routing table 的真实出口识别能力；但明确 LAN 成员不能出现在 WAN 候选中。
- 正在运行且未禁用的固定出口接口应补充为候选：PPPoE、WireGuard、L2TP/SSTP/OpenVPN/PPTP 客户端以及 GRE/IPIP/EOIP/VXLAN/ZeroTier 等固定隧道。没有活动默认路由的候选必须标记为未验证，不得伪造网关。
- 动态或明显的入站 VPN 服务端接口（例如 `l2tp-in`）不得作为自动 WAN 候选。
- WireGuard、PPP/VPN 和固定隧道候选必须按点到点接口处理，使无 IP 下一跳时可以使用接口作为 RouterOS 路由 gateway；普通接口仍要求明确网关。
- UI 必须能区分已验证和未验证的 WAN 候选，并保留当前的人工确认/网关填写流程。
- 策略流量入口仍可显示明确 LAN 成员；选择 `LAN` interface list 后，子接口应显示为已覆盖，生成规则继续使用受管聚合 interface list。
- 不修改 RouterOS 现有 WAN、VPN、interface list、防火墙或路由配置；本任务只修改 rosboard 发现和草稿校验逻辑。

## Acceptance Criteria

- [ ] 对含有 `LAN -> lan`、且 `lan` 同时出现在非活动 `main` 默认路由或活动自定义表默认路由中的 RouterOS 快照，WAN 候选不包含 `lan`，策略流量入口能识别 `LAN` 及其 `lan` 成员。
- [ ] 省略 `active` 的 RouterOS 默认路由被视为非活动；活动默认路由仍能正确生成 WAN 候选和网关证据。
- [ ] 无默认路由但运行中的 WireGuard、L2TP/SSTP/OpenVPN/PPTP/PPPoE 或固定隧道客户端出现在 WAN 候选中，`Proven=false`，并在 UI 中显示未验证状态。
- [ ] 动态 `l2tp-in`、禁用接口和明显入站服务端接口不出现在补充的 VPN WAN 候选中。
- [ ] WireGuard/VPN/固定隧道候选在前端不强制填写 IP 网关，后端 desired state 使用接口名作为点到点 route gateway；普通接口的网关校验行为不回退。
- [ ] 现有 discovery、gateway、policy routing 后端测试通过；新增回归测试覆盖 LAN 优先级、非活动路由、VPN 候选和点到点网关判断。
- [ ] `go test ./...`、`go vet ./...`、`npm --prefix web run lint`、`npm --prefix web run build` 通过；不覆盖或撤销用户已有工作区改动。

## Constraints

- 不能通过接口名称 `lan`/`wan` 做唯一角色判断；未有明确 LAN 证据时，复杂双角色拓扑保留为用户可确认的候选或使用下一跳模式。
- WireGuard 接口本身没有内置“客户端/服务端”角色标记；没有活动默认路由时作为未验证候选展示，由用户确认其 peer、Allowed Address、路由及加密套接字回程路径。
- 现场 RouterOS 只读快照仅用于诊断，不得把账号、密码或设备专属秘密写入代码、任务文档、测试或日志。
