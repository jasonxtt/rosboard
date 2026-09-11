# 配置参考

完整示例见 [`configs/config.example.yaml`](../configs/config.example.yaml)。

未传 `-config` 时，rosboard 使用当前工作目录的 `./config.yaml`；传入 `-config` 时则严格使用指定路径。两种方式都允许配置文件在首次启动时不存在，网页引导首次保存 RouterOS 设置时会自动创建它。

## 字段说明

| 字段 | 说明 |
| --- | --- |
| `listen_address` | 面板监听地址，默认 `:8080` |
| `data_dir` | SQLite 数据目录 |
| `poll_interval_seconds` | 完整 RouterOS 数据采集间隔，默认 `10` 秒 |
| `realtime_poll_interval_seconds` | 实时概览采集间隔，默认 `1` 秒 |
| `terminal_poll_interval_seconds` | 终端发现、地址与在线状态采集间隔，默认 `5` 秒；终端页当前速率由独立的 1 秒 conntrack 采集更新 |
| `sample_retention_hours` | 历史采样保留时长 |
| `allowed_cidrs` | 允许访问 `/api/*` 的客户端网段 |
| `devices[].id` | 设备稳定标识；创建后不应修改 |
| `devices[].name` | 面板中显示的设备名称 |
| `devices[].enabled` | 是否在后台持续采集该设备 |
| `devices[].routeros.*` | 每台设备的 REST 地址、账号、密码、采集接口和终端网段 |
| `devices[].protocol_analysis` | 是否对该设备启用协议 / 应用分析 |
| `devices[].mosdns.*` | MosDNS 审计日志归因：`enabled`、`base_url`、`sync_interval_minutes`（同步周期，默认 `5` 分钟）、`match_window_minutes`（实时证据窗口，默认 `30` 分钟；窗口内证据标记为「MosDNS 匹配」，更早学习到的指纹标记为「特征推断」） |

设备由面板在连接测试通过后写入配置文件；每台设备至少需要一个采集接口和一个 IPv4/IPv6 本地 CIDR。支持 `ROSBOARD_LISTEN_ADDRESS` 和 `ROSBOARD_DATA_DIR` 环境变量覆盖。自动创建与后续更新的配置文件权限均为 `0600`。

策略路由规则、访问控制规则和目标库**不写在 YAML 中**，而是保存在 SQLite 并由调和引擎下发到 RouterOS。

## 采集与页面刷新

| 层级 | 默认周期 | 控制项 | 作用 |
| --- | --- | --- | --- |
| 完整采集 | 10 秒 | `poll_interval_seconds` | 系统、接口、地址、路由、策略、终端完整快照与接口流量 |
| 概览实时采集 | 1 秒 | `realtime_poll_interval_seconds` | CPU、内存、所选 WAN 接口速率与图表采样 |
| 终端发现采集 | 5 秒 | `terminal_poll_interval_seconds` | DHCP、ARP、IPv6 Neighbor、地址/MAC 关联、在线状态、累计流量与历史 |
| 终端当前速率 | 1 秒 | 固定 | 终端页面可见时仅读取 IPv4/IPv6 conntrack，更新当前上下行速率、连接和流量分类 |

终端当前速率采集不受上述三个配置项、也不受页面「自动刷新」设置控制；终端页面离开或隐藏约 30 秒后会自动停止。页面刷新设置只控制浏览器读取后端缓存并更新显示：终端列表、终端详情、实时概览以及负载/流量历史遵从所选周期；选择停止后只停止周期读取，立即刷新仍然有效。

## 双 UI

当前提供 Compact 紧凑版和 Aurora 玻璃版，完整保留各自页面、导航和图表，共用后端、账号与设备数据。

在任一界面的「面板设置 → 界面设置 → UI 风格」选择另一套 UI，点击「保存并切换 UI」。切换会重新加载页面，请先保存其他编辑；浏览器会记住选择，首次访问默认 Aurora。明暗主题、刷新间隔和设备选择兼容共享。也可通过 `/?ui=compact` 或 `/?ui=aurora` 指定入口。
