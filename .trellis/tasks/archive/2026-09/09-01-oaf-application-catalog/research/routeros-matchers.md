# RouterOS matcher and enforcement research

审计日期：2026-09-01。依据 MikroTik 官方文档：

- [Common Firewall Matchers and Actions](https://help.mikrotik.com/docs/spaces/ROS/pages/250708064/Common%2BFirewall%2BMatchers%2Band%2BActions)
- [Filter](https://help.mikrotik.com/docs/spaces/ROS/pages/48660574/Filter)
- [Firewall](https://help.mikrotik.com/docs/spaces/ROS/pages/250708066/Firewall)
- [Connection tracking](https://help.mikrotik.com/docs/spaces/ROS/pages/130220087/Connection%2Btracking)
- [Address-lists](https://help.mikrotik.com/docs/spaces/ROS/pages/130220135/Address-lists)
- [REST API](https://help.mikrotik.com/docs/spaces/ROS/pages/47579162/REST%20API)

## 1. 关键语义

- firewall/filter 按规则自上而下处理；普通终止 action 会停止当前链，`jump`/`return` 可组织自定义链。
- `tls-host` 基于 HTTPS TLS SNI，使用 GLOB 通配符；如果 TLS handshake 被拆在多个 TCP packet 中，不能匹配 hostname。它不是通用 HTTPS/DPI matcher，也不能识别 QUIC/UDP。
- `src-address-list`/`dst-address-list` 是地址集合匹配；address-list 可用于 filter/mangle/NAT。DNS/static address-list materialization 可以把解析出的地址放入受管列表，但 IP/CDN 归属是近似且随 DNS 变化的。
- port matcher 支持整数/范围，只作用于 TCP/UDP。`protocol`、`dst-port` 等字段可表达 RouterOS filter 条件，但不代表应用身份。
- FastTrack 会绕过 firewall、connection tracking 等处理；已有 foreign FastTrack 规则可能使后续 filter/tls-host 规则不生效。rosboard 目前不能安全重写外部 FastTrack policy。
- IPv4/IPv6 filter 是分开的菜单；任何应用目标投影都必须明确 family、地址解析和失败状态。

## 2. Matcher matrix

| OAF matcher/metadata | 归因支持 | RouterOS 可表达性 | 当前 rosboard 支持 | 所需代码/风险 | 第一阶段决定 |
| --- | --- | --- | --- | --- | --- |
| `proto` + `sport/dport` | 可作为 Protocol/Service 事实；单独不足以确定 app | TCP/UDP protocol/port filter 可表达整数和范围 | policy-v2 有通用 protocol/port fields，monitor 也读取 conntrack 端口；没有 app projection | 直接用于 app rule 会误伤共享端口 | 保留为 loader 可识别的输入和 Service fallback；不作为具体 Application identity 的默认 enforcement |
| `host` domain，精确/后缀 | DNS observation 可作 inferred app attribution | 可间接用 RouterOS DNS/static address-list；直接 SNI 另见 tls-host | policy-v2 已有 source 的 DNS/address-list materialization，Catalog 尚无 | 需 app-owned list、DNS upstream、IPv4/IPv6、CDN overlap 和 readback | 第一阶段唯一 app enforcement subset：仅能安全独立解释的 domain-only signature，能力不满足则 blocker |
| `host` -> `tls-host` | DNS 归因可用，但不是同一证据 | `tls-host` 支持 TLS SNI glob，存在 fragmented-handshake false negative | `managedRouterFields` 未含 tls-host；当前没有 typed capability/readback contract | 未来需单独 allowlist/probe/order/FastTrack 方案 | deferred；第一阶段不承诺 |
| `url` / `request` | 当前没有 HTTP payload 证据 | 没有可接受的通用 request matcher；Layer7 会引入 payload/DPI 语义 | 不支持 | 不能用 domain/port 近似替代，避免误控 | unsupported |
| `dict` L7 hex | 只有 OAF DPI/L7 运行时或 payload parser 才能执行 | RouterOS 某些版本有 Layer7 能力，但当前 contract 未验证且不等于 OAF dict | 无 typed field、能力验证和运行时 | 规则大小/性能/语义差异；会超出 rosboard 边界 | unsupported |
| `search str` | 当前没有对应 DNS/conntrack 证据 | 无稳定通用 matcher contract | 不支持 | 不能静默降级 | unsupported |
| icon/name/category | 不参与连接匹配 | 不适用 | UI metadata 尚无 Catalog；icon 首期 deferred | 仅需最小 catalog metadata/status | category optional；icon deferred |
| rosboard `policyv2.Source` address-list | 不是 OAF matcher；是用户策略输入 | filter 的 source/destination address-list 可表达 | 已有 source list + access filter/jump/reconcile | 必须保持 source ownership 与 app ownership 分离 | keep as existing source path; never encode app ID as SourceID |

## 3. 最小可行 enforcement projection

首个可执行子集只接受 Catalog 中“完整且可解释、可以独立作为 domain 匹配的 signature”。现有 `policyv2` 组装代码为每个 `(device, applicationID, address family)` 生成独立的 application-owned address list，并用现有 policy-v2/accesscontrol desired graph 把 AccessRule member 的地址与这些 target list 连接起来。list name 只基于 `manager/device/applicationID`；domain/DNS object logical ID 只基于 `applicationID + matcher type + normalized domain`。

如果一个 Application 同时包含安全 domain signature 和含 request/L7/search 等不可丢弃条件的 signature，只控制安全 domain 部分，并在 Catalog/status 中说明这是已知域名的尽力控制，不声称完整 DPI blocking。

Catalog version/revision 不进入 list name、managed comment 或 logical object identity。refresh 后未变的 domain object 不重建；新 domain create、删除 domain cleanup，display name 变化只更新 readable label/comment。

该路径需要 RouterOS 自己的 DNS/static forwarder 或等价已验证能力，但不依赖 MosDNS；MosDNS 只增强流量归因。如果 RouterOS DNS forwarder、IPv4/IPv6 materialization、规则菜单或 ownership/readback 任一项不满足，则应用规则产生明确 blocker/unavailable，现有 sources/internet 规则保持原语义。

这不是把 OAF 的 port/L7 规则“翻译成一个看起来相似的规则”。没有安全独立 domain-only signature 的应用不会被伪装成已支持。共享 CDN、同一 domain 属于多个应用、DNS 变更和 IPv4/IPv6 差异是该地址列表方案的已知限制。

## 4. `tls-host` 后续准入条件

`tls-host` 不能只加到一个 fields map 就宣布支持。至少需要：

1. RouterOS version/capability probe 与明确的 `/ip/firewall/filter`、`/ipv6/firewall/filter` readback contract。
2. `tls-host` 纳入 managed fields、创建/patch/list 的安全白名单，并能在 scan/diff 中区分 rosboard-owned 与 foreign rule。
3. glob 与 OAF host 规则的确定性转换；处理 TCP-only、fragmented handshake、TLS 版本和不含 SNI 的连接。
4. 与现有 Access Control chain/jump order 的测试，以及外部 FastTrack 规则的行为研究。
5. fake RouterOS、IPv4/IPv6、reconcile drift、删除/更新和升级回滚测试。

在这些条件达成之前，`tls-host` 状态只能是 deferred/unsupported，不可出现在“已生效应用控制”列表中。本任务的 domain-only 路径只复用现有 Access Control rule ordering，不新增 FastTrack scanner、mutation 或 policy manager。
