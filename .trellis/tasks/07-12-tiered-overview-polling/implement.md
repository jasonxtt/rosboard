# 实施计划

1. 扩展轮询配置与校验，并补充配置测试。
2. 将 Monitor 拆出独立实时层及互斥的终端/全量层，运行 1/3/10 秒采集。
3. 新增轻量 `/api/realtime`，为采集层和 API 补充针对性测试。
4. 前端默认 1 秒读取实时数据、3 秒读取完整 dashboard，并确保请求不会重叠。
5. 更新示例/本地配置，构建嵌入式前端，重启实际服务。
6. 验证：`go test ./...`、`npm --prefix web run lint`、`npm --prefix web run build`、`npm --prefix web audit --audit-level=moderate`、浏览器网络/控制台和局域网 HTTP 200。

## Risk points

- `internal/service/monitor.go` 的快照合并必须保留慢速层刚更新的数据。
- 终端层依赖地址和接口上下文；不得使用未初始化缓存。
- 前端两个请求返回顺序可能反转，完整 dashboard 不得把更新的实时 Overview 回退成旧值。
