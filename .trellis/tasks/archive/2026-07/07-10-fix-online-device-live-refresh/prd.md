# Fix online device count and live refresh

## Goal

系统概况的“连接设备”只显示当前在线的 LAN 终端，并恢复所有实时监控数据的持续刷新。

## Background

- 前端 `/api/dashboard` 定时器仍按 5 秒请求，浏览器刷新链路正常。
- 运行中的 API `updatedAt` 固定在 `2026-07-10T10:14:32Z`，连续请求的 CPU、连接数和速率完全不变。
- 后端日志每 5 秒报告 `move terminal addresses: UNIQUE constraint failed: terminal_addresses.terminal_id, terminal_addresses.family, terminal_addresses.address`。
- 数据库中两个 IPv6 地址同时属于临时 `addr:` 终端和已经关联 MAC 的 `mac:` 终端；`MergeTerminal` 直接更新 `terminal_id`，目标已存在同一 `(terminal_id, family, address)` 时整轮刷新回滚。

## Requirements

1. `connectedDeviceCount` 仅统计 `state == online` 的 LAN 终端，继续排除 RouterOS 本机、WAN/流量接口和离线终端；`idle` 不再计入。
2. 合并临时地址终端到 MAC/RouterOS 终端时，同地址已存在不得导致唯一约束失败。
3. 冲突地址合并后只保留一条目标终端记录，并保留两条记录中较新的 `last_seen`。
4. 修复必须自动消化当前数据库中的重复归属，不要求手工删除历史数据库。
5. RouterOS 保持只读，不修改其配置。
6. 完成后重新构建并启动 `0.0.0.0:8080`，局域网可访问。

## Acceptance Criteria

- [x] 单元测试覆盖在线/空闲/离线、RouterOS、WAN 和接口未知终端的在线设备计数。
- [x] 存储层测试复现“源终端和目标终端已有同一地址”，合并成功、地址唯一且 `last_seen` 取最大值。
- [x] 当前 28 MB 数据库无需手工清理即可完成刷新，日志不再出现 `move terminal addresses` 唯一约束错误。
- [x] `/api/dashboard.overview.updatedAt` 至少跨两个轮询周期持续变化。
- [x] CPU、连接数或 WAN 速率等实时字段随 RouterOS 快照更新，不再固定为旧值。
- [x] 前端“连接设备”提示明确为仅在线 LAN 终端。
- [x] Go 测试/race/vet、前端 lint/build 通过。
- [x] `127.0.0.1:8080` 与 `10.0.0.86:8080` 均返回 HTTP 200。

## Out of Scope

- 改变在线、空闲、离线三种终端状态本身的判定规则。
- 修改 RouterOS 配置或清空 rosboard 历史数据。
