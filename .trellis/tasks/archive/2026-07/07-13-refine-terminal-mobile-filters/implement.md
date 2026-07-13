# 实施计划

1. 修改终端默认排序并重构移动端工具栏类名与网格 CSS。
2. 调整详情标题卡片及手机返回按钮定位。
3. 在连接详情中建立 scope-first 数据管道和各列筛选状态。
4. 实现可复用表头筛选器、IP 版本标签和悬浮全局搜索，删除旧工具栏。
5. 构建嵌入式前端并重启服务。
6. 用 375px 与桌面浏览器验证排序、三种入口范围、筛选、搜索、排版和控制台；运行 lint/build/audit、Go tests。

## Risk points

- `connectionFamily` 是父组件状态，必须与 `scope` 共同约束，不能让全部选项泄漏到 IPv4/IPv6 入口。
- details 浮层不能撑高表头或被 `.table-scroll` 裁切；全局搜索必须保持可操作。
- 新增 IP 版本列后 empty row 的 `colSpan` 必须同步。
