# Design

## Root cause boundary

数据源和前端定时器均正常；失败发生在一次监控快照写入 SQLite 的终端身份合并阶段。`Monitor.refresh` 只在所有持久化工作成功后替换内存快照，因此一次合并错误会让整个 Dashboard 保留上次成功结果。

## Terminal-address merge

将 `MergeTerminal` 的直接主键更新替换为事务内两步操作：

1. 把源终端地址插入目标终端；目标键冲突时更新 `last_seen = max(existing, incoming)`。
2. 插入成功后删除源终端地址。

终端累计量、connection state 和终端记录继续在同一事务处理；任何步骤失败仍整体回滚。该设计既能修复当前数据库，也能处理以后地址身份从 `addr:` 升级为 `mac:` 的正常竞争。

## Online-device metric

沿用现有 WAN/RouterOS 过滤，只把状态条件从“不是 offline”收紧为“必须 online”。接口未知但状态为 online 的终端仍保留，避免邻居接口元数据缺失造成漏计。

## Runtime rollout

构建嵌入式前端和 Go 二进制后，重启当前后台服务。验证 API 更新时间连续前进、日志无合并错误，并检查 LAN HTTP 200。无需迁移或清空数据库。
