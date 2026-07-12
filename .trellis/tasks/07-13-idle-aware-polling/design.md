# 空闲感知采集设计

## Activity model

Monitor 保存 `activeUntil`，任何可见页面通过 `POST /api/viewer-heartbeat` 将其延长到当前时间后 30 秒。只有从空闲切换到活跃时才向实时和后台调度器各发送一次非阻塞唤醒信号；普通续期不触发额外采集。

前端每 10 秒心跳一次，并监听 `visibilitychange`：仅 `document.visibilityState === 'visible'` 时发送。关闭、切后台或网络中断都无需卸载请求，TTL 会自然失效。

## Scheduler behavior

- 活跃实时调度器：1 秒运行 `refreshRealtime`。
- 活跃后台调度器：3 秒运行 `refreshTerminals`，10 秒运行 `refresh`，两者继续由 `refreshMu` 互斥。
- 空闲：实时调度器不采集；后台调度器每 60 秒只运行完整 `refresh`，由一次全量轮次覆盖所有分钟级数据。
- 唤醒：实时调度器立即 `refreshRealtime`；后台调度器立即 `refresh`，完成后重新建立 3/10 秒截止时间。

## API contract

`POST /api/viewer-heartbeat` 只更新内存活动期限并返回 `{ activeUntil }`。它不等待或直接调用 RouterOS，调度器收到唤醒信号后异步采集。

## Concurrency and failure

- 活动期限由独立 RWMutex 保护；唤醒 channel 容量为 1，避免重复信号堆积。
- 心跳停止不是错误，不产生告警。
- 心跳恢复时若慢速轮次正在执行，后台唤醒保留在 channel 中，由互斥调度器在安全时机处理。
- RouterOS 采集失败沿用现有最后有效快照和告警规则。
