# RouterOS IP/CIDR 策略路由调研

调研日期：2026-08-29

## 结论

IP/CIDR 目标分流无需建立与域名分流不同的 routing-rule 模型。现有 rosboard 已通过 firewall mangle 的 `mark-routing` 选择 RouterOS v7 routing table，因此 IP 列表只需要直接物化到对应 firewall address-list，再复用已有 `dst-address-list` mangle。

## 官方依据

1. RouterOS Policy Routing
   - https://help.mikrotik.com/docs/spaces/ROS/pages/59965508/Policy%2BRouting
   - RouterOS 支持 routing tables、routing rules、firewall mangle marking。
   - 官方说明 mangle 提供更丰富的匹配控制，并提醒无必要不要同时混用 mangle 和 routing rules；两者同时使用时 mangle 优先级更高。

2. RouterOS Mangle
   - https://help.mikrotik.com/docs/spaces/ROS/pages/48660587/Mangle
   - `mark-routing` 专用于 policy routing。
   - RouterOS v7 的 `new-routing-mark` 必须对应已创建的 routing table。

3. RouterOS Advanced Firewall
   - https://help.mikrotik.com/docs/spaces/ROS/pages/328513/Building%2BAdvanced%2BFirewall
   - 官方示例直接使用 `/ip firewall address-list` 保存 IPv4 CIDR。
   - 官方示例直接使用 `/ipv6 firewall address-list` 保存 IPv6 CIDR。

4. RouterOS v6→v7 policy routing 示例
   - https://help.mikrotik.com/docs/spaces/ROS/pages/30474256/Moving%20from%20ROSv6%20to%20v7%20with%20examples
   - 明确展示 routing table 预创建及 mangle `mark-routing` 的使用。
   - 提醒 mangle 路由标记应排除 local 目标；rosboard 当前 desired builder 已使用 `dst-address-type=!local`。

## 对 rosboard 的直接含义

当前业务 mangle 已匹配：

```text
dst-address-list=<list>
action=mark-connection
→ connection-mark
action=mark-routing
→ new-routing-mark=<table>
```

因此：

```text
Domain source
  → DNS Static FWD
  → RouterOS 动态 address-list

IP source
  → RouterOS 静态 address-list entry

两者
  → 同一个现有 business mangle / route table
```

shared 模式下可使用相同 list；dedicated 模式下每 source 使用独立 list。IP-only 策略无需 DNS forwarder、Fake DNS alias、DNS DNAT 或 DNS output routing mark。
