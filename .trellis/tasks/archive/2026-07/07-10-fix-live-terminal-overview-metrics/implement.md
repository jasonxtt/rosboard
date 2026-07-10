# Implementation plan

1. 扩展 Overview 数据模型和刷新聚合，增加 LAN 设备数及 conntrack 总数。
2. 为 LAN 设备过滤及 WAN TX/RX 方向增加后端单元测试。
3. 更新前端类型、八宫格概况布局、WAN 速率标签与采样接口提示。
4. 运行 Go 测试、前端测试/构建及项目质量检查。
5. 构建生产二进制，停止 8080 回放，使用真实 RouterOS 数据启动 `0.0.0.0:8080`。
6. 对照实时 RouterOS REST 与 rosboard API 验证 IPv4 终端数、conntrack 总数、WAN 速率和局域网 HTTP 200。

## Rollback points

- 数据模型新增字段向后兼容；前端可随二进制一起回滚。
- 运行切换只影响本机 rosboard 进程，不修改 RouterOS。
