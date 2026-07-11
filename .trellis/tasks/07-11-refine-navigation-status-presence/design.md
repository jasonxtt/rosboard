# Technical Design

## Architecture and Boundaries

本次修改保持现有单体 Go 服务和 React 前端边界，不新增外部服务或数据库迁移。

1. `internal/service` 负责 RouterOS 采集、终端证据归类、三态状态转换和当前告警队列。
2. `internal/model` 扩展 dashboard/terminal JSON 契约。
3. `web/src` 只消费明确的状态与告警，不在浏览器端推断设备是否在线或拼装伪告警。
4. SQLite 继续保存终端的 `state`、`lastSeen`、`onlineSince`；现有文本 state 列可直接容纳 `inactive`，无需 schema migration。

## Navigation State

前端增加两个独立展开状态：一级 `statusExpanded` 与二级 `expandedMonitorGroup`。页面状态仍由现有 `activeView`、`terminalFamily` 表达。

- 点击“状态监控”：只切换 `statusExpanded`。
- 点击带子项的二级：只切换 `expandedMonitorGroup`，并关闭同级其他组。
- 点击“线路监控”或三级叶子：更新页面状态并关闭移动端抽屉；不强制折叠其祖先。
- 页面初始化或程序化切页时，根据当前 active view 展开祖先，保证选中项可见。
- 使用 `aria-expanded`、`aria-controls` 标记折叠关系，并保留当前按钮键盘可操作性。

## Presence State Machine

采集阶段把证据拆成两类：

- strong：活动连接、路由器自身地址、reachable/permanent ARP 或 IPv6 neighbor。
- weak：bound lease、stale/complete ARP、其他仅用于识别地址/MAC/接口的邻居项。

对每个终端先构建设备身份和地址，再读取已持久化的最近强证据时间，最后统一计算状态：

```text
strong now                    -> online; lastSeen = now
no strong, now-lastSeen <= 5m -> inactive; lastSeen unchanged
no strong, older/no lastSeen  -> offline; lastSeen unchanged
```

`UpdateTerminalPresence` 负责状态边沿：进入 online 时建立新的 `onlineSince`；离开 online 进入 inactive 时结束在线区间；inactive 转 offline 不重复修改 lastSeen。在线计数严格只统计 `state == online`。

为了避免首次部署时旧逻辑刚写入的污染时间长期误报，弱证据最多只获得剩余宽限时间；它永远不能把 lastSeen 推到当前时间。`10.0.0.5` 的 fixture 覆盖 `complete=true,status=stale` 且无强证据的回归场景。

## Alert Contract and Lifecycle

新增模型：

```text
AlertEvent {
  id, level(warning|error), source, message, timestamp
}
DashboardSnapshot.alerts []AlertEvent
```

Monitor 内持有带互斥锁的有界当前事件集合（建议上限 50）。采集子项失败时通过统一 helper 上报稳定 key（来源 + 级别 + 消息/错误类别）；同 key 再次失败只刷新时间。该采集子项下一次成功时按 source/key 清除对应告警，因此面板表达“当前存在的问题”，而不是无期限历史日志。Dashboard 返回快照副本并按 timestamp 倒序。

现有 `Warnings []string` 暂时保留以兼容现有调用方和顶部全局提示；概览“当前告警”改读 `alerts`。实现时应把已有可恢复采集 warning 的产生点接入结构化 helper，避免前后端各自解释字符串。

## System Status Mapping

系统状态直接使用 dashboard 已有字段：

- 运行时间：`overview.uptime`
- RouterOS 版本：`overview.version`
- 最后采集：`overview.updatedAt`
- 活动接口：`interfaces.filter(running && !disabled).length / interfaces.length`
- 存储：`overview.storageUsedPercent`
- 数据新鲜度：当前时间减 `updatedAt`；以自动刷新/采集周期的容忍窗口判定正常或注意

CPU 和内存仍保留在顶部指标卡，不出现在系统状态列表。

## Compatibility and Rollback

- API 只新增 `alerts` 并扩展 state 枚举，旧字段不删除。
- 前端所有 state 映射、筛选、样式和详情文案必须显式支持 inactive，避免落入 offline 的默认分支。
- 回滚时可独立撤回前端导航、告警契约或 presence 状态机；没有不可逆数据库变更。

## Key Trade-offs

- 不用 Ping：减少 ICMP 被禁导致的假离线，代价是设备静默后的状态依赖 RouterOS 强证据和 5 分钟宽限。
- 告警仅保存在内存：实现小且符合“当前告警”，代价是服务重启后历史消失；持久告警历史明确不在本次范围。
- 保留 `Warnings` 兼容字段：降低改动风险，代价是短期存在两个告警表现层，后续可单独清理。
