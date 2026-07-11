# 优化监控导航、状态和在线判定

## Goal

优化 Rosboard 的分级导航、概览状态/告警语义和终端在线判定，使菜单层级可控、系统信息不重复、告警来源真实可解释，并避免把只有陈旧发现记录的关机设备统计为在线。

## Background

- 用户要求状态监控采用三级折叠菜单：一级“状态监控”展开二级；二级“线路、终端、流量、运行监控”按需展开；只有终端、流量、运行监控展示三级叶子项。
- 当前“当前告警”不是日志 error 列表。`web/src/App.tsx:528` 的 `buildCurrentEvents()` 混合 dashboard warnings、接口状态、阈值与用于填充版面的健康事实，名称和内容语义不准确。
- 当前系统状态重复展示了顶部已有的 CPU、内存；`internal/model/types.go:5-20` 已提供运行时间、版本、存储和更新时间等可复用数据。
- 实时面板把 `10.0.0.5`（`BC:24:11:94:45:CA`）标记为 online，但它无 DHCP lease、无 IPv4/IPv6 conntrack，本机连续 ping 100% 丢包，仅存在 ARP `complete=true,status=stale`。
- 根因位于 `internal/service/monitor.go:534-535`：只要 ARP `complete=true` 就推进 online 和 lastSeen，陈旧 ARP 缓存因此被当成当前在线证据。

## Requirements

### R1. 分级折叠菜单

- “系统概览”保持独立一级叶子菜单。
- “状态监控”作为一级可展开菜单；展开后显示“线路监控、终端监控、流量监控、运行监控”四个二级菜单。
- “线路监控”是二级叶子，点击后直接切换页面。
- “终端监控”展开“全部终端、IPv4、IPv6”；“流量监控”展开“协议统计、策略统计”；“运行监控”展开“负载历史、路由 / 分流”。
- 一级和二级的展开状态与当前页面状态分离；点击非叶子菜单只展开/收起，不切页。点击叶子菜单才切页。
- 同级二级菜单采用手风琴行为，同时最多展开一个带三级项的二级菜单；当前页面所属层级首次进入时自动保持可见。

### R2. 真实结构化告警

- “当前告警”只展示采集过程中真实产生的 warning/error，不再拼入接口正常、系统正常等健康事实，也不通过解析日志字符串生成。
- 后端在采集告警产生处记录结构化事件，字段至少包含 `id`、`level`、`source`、`message`、`timestamp`。
- 事件保存在有容量上限的进程内队列中，按时间倒序随 dashboard API 返回；服务重启后清空是可接受行为。
- 相同来源、级别和消息的连续采集失败应去重/更新时间，避免每个刷新周期重复刷屏；恢复后不再作为当前告警展示。
- 没有当前告警时显示一个明确空状态。

### R3. 不重复的系统状态

- 系统状态移除 CPU、内存，使用已有数据展示：RouterOS 运行时间、版本、最后成功采集时间、活动接口数、存储使用率和数据新鲜度。
- 数据新鲜度由当前时间与 `overview.updatedAt` 的差值计算；超过合理采集窗口时显示注意状态。
- 缺失值显示 `-`，不得用虚构健康数据补位。

### R4. 三态在线判定

- 终端状态统一为 `online`（在线）、`inactive`（近期未活跃）、`offline`（离线）。
- 强在线证据包括：活动 IPv4/IPv6 conntrack、RouterOS 自身地址、`reachable/permanent` ARP、`reachable/permanent` IPv6 neighbor。
- `bound` DHCP lease、`complete` 但 `stale` 的 ARP、其他非 reachable 的邻居记录仅是弱发现证据，不能单独更新 lastSeen 或 onlineSince。
- 有强证据时立即 online 并更新 lastSeen；强证据消失后的 5 分钟宽限期内为 inactive；超过 5 分钟为 offline。
- inactive 不计入在线终端数量，在线时长停止增长；再次获得强证据时开始新的在线区间。
- Ping 不作为判定源，因为终端可能禁止 ICMP；它只用于人工诊断验证。

## Acceptance Criteria

- [x] AC1：一级/二级菜单按点击展开，三级只在所属二级展开时出现；非叶子不切页，叶子切页，桌面和移动端行为一致。（R1）
- [x] AC2：告警列表只展示真实结构化 warning/error，包含时间、来源和级别；重复失败不刷屏，恢复后消失，无告警时只有空状态。（R2）
- [x] AC3：系统状态不再重复 CPU/内存，并展示运行时间、版本、最后采集、活动接口、存储、新鲜度；缺失数据不伪造。（R3）
- [x] AC4：仅有 stale ARP、无 lease/connection/reachable evidence 的 `10.0.0.5` 不标记 online，不更新 lastSeen；宽限期后为 offline。（R4）
- [x] AC5：强证据出现、消失不足 5 分钟、消失超过 5 分钟分别得到 online、inactive、offline，并正确维护在线计数和 onlineSince。（R4）
- [x] AC6：前端 lint/build、Go 单元测试、真实 RouterOS API 和浏览器视觉/交互验证全部通过。（R1-R4）

## Out of Scope

- 不读取或全文解析 RouterOS/systemd 应用日志来推断告警。
- 不持久化告警历史，不增加告警确认、通知或筛选系统。
- 不以 Ping 扫描整个局域网，也不引入额外探针服务。
- 不重做本次需求之外的页面视觉风格。
