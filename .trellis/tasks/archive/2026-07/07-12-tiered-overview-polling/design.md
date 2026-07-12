# 分层刷新设计

## Architecture

后端保持“采集器主动轮询、HTTP API 只读内存快照”的边界。Monitor 使用两个调度通道：实时层独立每秒运行；终端层与慢速全量层共用互斥锁并按截止时间串行运行。这样耗时较长的 conntrack/全量轮次不会阻塞流量时间轴，同时慢速写入不会彼此重叠。

### 实时层（默认 1 秒）

- `/system/resource`：CPU、内存。
- 仅 `routeros.traffic_interfaces` 的 `monitor-traffic`：概览上传/下载。
- 更新 Overview 的实时字段和 5 分钟曲线；保存 1 秒流量/负载采样。
- RouterOS 偶发响应超过 1 秒时，图表在缺失秒位沿用最近一次有效速率，避免时间轴跳秒；下一个真实样本仍原样展示。

### 终端层（默认 3 秒）

- 并行读取地址、DHCP lease、ARP、IPv6 neighbor、IPv4/IPv6 conntrack，缩短这一高成本轮次的墙钟时间。
- 基于最近一次完整接口数据重建终端与连接详情。
- 更新终端列表、连接数、在线终端数和协议聚合。

### 慢速层（默认 10 秒）

- 沿用现有完整 refresh，负责接口、健康、告警、策略、路由和清理。
- 完成后覆盖完整一致快照；下一次实时/终端轮次只更新其负责字段。

## API and frontend

- 新增 `GET /api/realtime`，返回 Overview，保持 payload 轻量且不含 dashboard 的终端/策略/路由集合。
- React 中拆成两个计时器：默认 1 秒拉 `/api/realtime` 并合并 overview；3 秒拉 `/api/dashboard` 获取终端/连接卡片和慢速面板。
- 自动刷新下拉框控制实时接口周期，增加“1 秒刷新”并设为默认；停止刷新仍可停止自动请求。

## Configuration

- `realtime_poll_interval_seconds` 默认 1。
- `terminal_poll_interval_seconds` 默认 3。
- 保留 `poll_interval_seconds` 作为慢速全量周期，默认改为 10，兼容已有配置字段。

## Reliability and rollback

- 终端层与慢速层串行；实时层允许与它们并行，单轮失败只记录错误，下一轮继续。
- 层级更新在拿到完整结果后一次性加锁合并，API 不观察半成品。
- 慢速轮次若在执行期间出现更新的实时快照，提交时保留较新的 CPU、内存、流量、曲线和时间戳。
- 若运行负载异常，可仅调大三个配置周期，无需回退代码。
