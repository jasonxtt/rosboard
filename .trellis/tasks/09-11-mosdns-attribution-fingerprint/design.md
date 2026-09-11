# 技术设计：MosDNS 指纹归因

## 设计决策

| 决策点 | 结论 | 理由 |
|---|---|---|
| 证据源 | `dns_features`（每同步周期由观测聚合） | 观测是 TTL 敏感的瞬时证据；指纹是"该客户端最近把此 IP 解析为此域名"的长期事实，契合"大概其"定位 |
| 时效门 | 单一条件 `last_seen ≥ now-7d`（常量 `dnsFeatureMaxAge`） | TTL 约束的是客户端缓存而非映射真实性；拉模式采集下 TTL 门必然饿死证据（生产实测 0 存活） |
| 冲突仲裁 | 同 `(clientIP, answerIP)` 取 `last_seen` 最新 | 共享 CDN IP 场景以最近观测为准，与现有 newest-first 语义一致 |
| 来源分档 | `last_seen ≤ matchWindow` → `mosdns`；否则 → `mosdns-learned` | 不新增查询，仅 Resolve 时比时间戳；`match_window_minutes` 语义顺势退化为分档线 |
| 同步周期 | 默认 5 分钟 | 指纹方案下同步周期与识别率解耦；5min 只影响新域名出现延迟与实锤徽章新鲜度；增量同步每次 ~1 页，成本可忽略 |
| 首次同步 | 无水位时按 24h 时间截断停页 | 审计日志新的在前，遇超龄记录直接 break（复用水位停页写法）；解决 500 页/18min/800MB 冷启动问题 |
| 指纹清理 | SyncOnce 内删除 `last_seen < now-7d` | 指纹表当前只增不删，须随时效门一并收口 |

## 改动点

1. `internal/service/application_resolver.go`
   - `dnsEvidence` → `{domain, lastSeen}`；evidence map 从 features 构建（每 key 保留 last_seen 最新者）。
   - `validAt` 语义改为 `at - lastSeen ≤ dnsFeatureMaxAge`（7 天常量）。
   - `Resolve` 返回值增加来源字符串（`mosdns` / `mosdns-learned`），分档阈值 = `matchWindow`。
   - refresh 缓存简化为仅按 cacheDuration（30s）刷新（无时间窗查询）。
   - 新增常量 `dnsFeatureMaxAge = 7 * 24 * time.Hour`，mosdns.go 清理复用。
2. `internal/store/sqlite.go`
   - `DNSFeaturesForMatch(ctx, since)`：加 `last_seen_ns >= ?` 过滤，ORDER BY 改 `last_seen_ns DESC`（调用方仅 resolver + 测试）。
   - 新增 `PruneDNSFeatures(ctx, before)`。
3. `internal/service/mosdns.go`
   - SyncOnce：无水位时 `record.QueryTime < now-24h` 即停页（常量 `mosDNSInitialSyncLookback`）；保存观测后调用 `PruneDNSFeatures(now - dnsFeatureMaxAge)`。
4. `internal/service/monitor.go`
   - `terminalConnectionRow`：`ApplicationSource` 用 Resolve 返回的来源替代字面量 `"mosdns"`。
5. 默认值 5 分钟：`configs/config.example.yaml`、`internal/api/server.go` 识别设置保存兜底、前端 `RecognitionCard` 表单默认值。
6. 前端 `web/src/pages/ProtocolsPage.tsx`：`mosdns-learned` 徽标文案「指纹推断」；检查连接表/识别页是否需要同源文案。

## 兼容性

- 配置结构不变；`match_window_minutes` 语义变化（识别门 → 分档线），识别设置页文案顺手对齐。
- 存量 `dns_features` 数据直接可用，无需迁移；超龄指纹在首次同步后清理。
- 首次同步限深只影响无水位场景；有水位的稳态增量路径不变。

## 回滚

纯代码改动，无 schema 变更；回滚 = 换回旧二进制。旧代码读同样的表，无数据兼容问题。
