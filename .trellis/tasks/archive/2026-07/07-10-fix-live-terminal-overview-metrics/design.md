# Design

## Data contract

在 `model.Overview` 增加：

- `connectedDeviceCount`: 当前可见的 LAN 在线/空闲终端数。
- `connectionCount`: 当前 IPv4 与 IPv6 conntrack 原始条目总数。

保持现有 Dashboard API 结构，前端只消费新增字段，不增加新接口。

## Backend aggregation

监控刷新仍以一次 RouterOS REST 轮询为原子快照：

1. 使用现有 ARP、DHCP、邻居和 conntrack 数据构建终端。
2. 对终端结果做 LAN 设备计数：排除 `routeros:self`、`offline`、明确的 WAN/所选流量接口；接口未知但有活动证据的终端保留，避免漏计。
3. 使用 `len(ipv4Connections) + len(ipv6Connections)` 作为总连接数，避免终端关联规则导致漏算。
4. 使用已选流量接口 monitor 的 TX 汇总为 WAN 上行、RX 汇总为 WAN 下行。

## Frontend layout

概况区由 6 项扩展为 8 项；两个新模块紧跟内存。速率标签改为“WAN 上行速率/下行速率”，详情显示 `TX/RX · 采样接口`。顶部状态条同样使用 WAN 命名，消除“全接口总速率”的歧义。

## Runtime rollout

构建新的二进制和前端资源后，停止 8080 回放进程，以用户提供的 RouterOS 只读凭据启动真实服务并监听 `0.0.0.0:8080`。凭据仅作为本机运行时环境使用，不写入版本库。回滚时可停止真实进程并重新启动旧服务，不涉及 RouterOS 变更。
