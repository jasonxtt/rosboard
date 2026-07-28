# 技术设计：终端列表与连接详情

## 数据语义

`TerminalConnection.SourceAddress/SourcePort` 和 `DestinationAddress/DestinationPort` 保存 RouterOS 原始连接方向。`orientConnection` 只负责判断连接归属、终端上传/下载方向、外网地址及路由归属，不再覆盖原始四元组。

连接 key 优先使用 RouterOS REST `.id` 并带地址族前缀，避免实时刷新时相同四元组产生 React key 冲突；缺少 `.id` 时继续使用原有四元组 key 兼容。

## 前端

- 终端列表设备列展示名称主行和 MAC 辅助行，IP 独立展示。
- 连接详情保留一份 `ConnectionTable` 列定义和表格状态实现。
- 来源 IP、来源端口分别拥有 sort/filter key。
- 表格继续调用 `formatBits` 和 `formatBytes` 展示终端视角上传/下载。
- 不新增全局页面、导航状态、轮询或终端身份列。

## 验证

- Go 单测覆盖 reply-side 终端仍保留原始 src/dst，同时上传/下载方向正确。
- 前端 lint、构建和依赖审计通过。
- 本地运行验证终端列表、连接拆列筛选和 375px 布局。
- 远端按门禁备份部署，用户负责最终视觉验收。
