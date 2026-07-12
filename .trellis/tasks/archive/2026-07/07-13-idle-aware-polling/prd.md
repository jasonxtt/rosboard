# 浏览器无人查看时降低采集频率

## Goal

浏览器无人查看时把 RouterOS 采集降为每分钟一次；重新查看页面时立即恢复实时采集，兼顾资源节省和分钟级监控连续性。

## Background

- 当前后端无论是否有浏览器访问，都会持续执行 1 秒实时、3 秒终端和 10 秒全量采集。
- 用户允许无人查看期间所有状态只保留一分钟一次的连续性。
- API 继续只读取内存快照；浏览器请求不得直接同步执行 RouterOS REST 调用。

## Requirements

- R1：页面可见时每 10 秒发送一次轻量查看心跳；页面首次加载或从隐藏恢复可见时立即发送。
- R2：最后一个心跳超过 30 秒后，后端进入空闲模式，所有 RouterOS 数据统一每 60 秒执行一次全量采集。
- R3：空闲期间不再额外执行 1 秒实时或 3 秒终端采集，但仍保存分钟级流量、CPU、内存、终端、连接、接口、系统状态和告警。
- R4：从空闲状态收到新心跳时，调度器立即执行一次实时采集和一次全量采集，然后恢复 1/3/10 秒周期。
- R5：多个浏览器或标签页共享活动状态；任一可见页面持续心跳即可保持活跃模式。
- R6：心跳接口不得阻塞等待 RouterOS；重复心跳不得重复触发立即全量采集。
- R7：现有手动刷新、自动刷新下拉框和 API 数据结构保持兼容。

## Acceptance Criteria

- [x] AC1：无心跳超过 30 秒后，实时 `updatedAt` 不再每秒推进，并在约 60 秒周期刷新。
- [x] AC2：首次或恢复可见的页面发送心跳后，Overview 无需等待一分钟即可更新，并恢复连续秒级图表。
- [x] AC3：心跳响应快速完成，且多个连续心跳只延长活动期限，不创建采集风暴。
- [x] AC4：隐藏/关闭唯一页面后自动进入空闲模式；重新显示页面后自动恢复，无需手工操作。
- [x] AC5：Go 单元/竞态测试、前端 lint/build/audit、浏览器控制台和本地/局域网 HTTP 验证通过。

## Out of Scope

- 不提供用户可配置的空闲周期 UI。
- 不持久化查看者身份，不跨进程重启保存活动状态。
- 不使用 WebSocket 或 SSE。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
