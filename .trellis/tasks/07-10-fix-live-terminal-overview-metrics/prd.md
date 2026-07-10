# Fix live terminal and overview metrics

## Goal

让系统概况和终端监控直接反映当前 RouterOS 的实时状态，而不是回放快照，并补齐局域网连接设备数与 RouterOS conntrack 总连接数。

## Background

- 当前 `0.0.0.0:8080` 运行的是历史回放服务，模拟数据只提供 `10.0.0.1/24`、空连接表和零接口速率，因此 UI 中全部终端/IPv4 只显示 RouterOS 本机，速率也不可信。
- 使用用户提供的只读账号直连 RouterOS 后，REST 返回了完整 ARP、DHCP、IPv4/IPv6 conntrack 和接口 monitor 数据；现有实时聚合能产生多台 IPv4 终端。
- RouterOS 的接口 monitor 字段是 bit/s；WAN 上行应使用所选 WAN/PPPoE 接口的 TX，下行应使用 RX。

## Requirements

1. 系统概况在内存占用后增加“连接设备”和“连接数”两个状态模块。
2. “连接设备”统计当前在线或空闲的局域网终端，排除 RouterOS 本机、离线终端和已知 WAN/流量接口终端。
3. “连接数”直接统计 RouterOS 当前 IPv4 与 IPv6 conntrack 条目总和，包含能够关联终端和无法关联终端的条目。
4. 系统概况和页面右上角明确显示所选 WAN 接口的实时上/下行速率，并标明采样接口；上行使用 TX、下行使用 RX。
5. 全部终端和 IPv4 终端必须来自真实 RouterOS 数据；不得继续以回放快照作为 8080 的运行数据源。
6. 不写入或修改 RouterOS 配置。
7. 完成后构建应用，并以 `0.0.0.0:8080` 启动，允许局域网其他设备访问。

## Acceptance Criteria

- [ ] 概况卡片按“运行时间、CPU、内存、连接设备、连接数、WAN 上行、WAN 下行、在线接口”显示。
- [ ] `connectedDeviceCount` 排除 RouterOS 本机、离线及已知 WAN 终端，并有单元测试覆盖。
- [ ] `connectionCount` 等于本轮 REST 读取到的 IPv4 conntrack 数加 IPv6 conntrack 数。
- [ ] 单元测试证明 WAN TX 映射到上行、RX 映射到下行。
- [ ] 真实 8080 API 的 IPv4 终端不再只有 `10.0.0.1`，连接数与 RouterOS 实时 REST 数据量级一致。
- [ ] 真实 8080 API 的速率随所选 PPPoE/WAN 接口 monitor 变化，页面同时展示采样接口。
- [ ] 后端测试、前端构建与全量质量检查通过。
- [ ] `http://10.0.0.86:8080` 可从局域网访问并返回 200。

## Out of Scope

- 摄像头、交换机等 RouterOS 当前无法直接提供的周边设备扩展。
- 修改 RouterOS 防火墙、接口、DHCP、ARP 或账号配置。
