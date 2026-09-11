# 修复 MosDNS 识别归因证据饿死

## Goal

生产环境开启「协议分析 + MosDNS 联动」后，协议统计页从未出现任何 MosDNS 识别的应用/域名。根因已定位：识别证据有效期逻辑要求 `now < queryTime + TTL`（`internal/service/application_resolver.go` 的 `validAt`），而 MosDNS 应答 TTL 绝大多数 ≤300s（生产实测 51% ≤5s），远小于 30 分钟同步周期，导致几乎所有证据在可用前即过期。本任务按用户确认的"大概其"定位重新设计证据语义，恢复识别能力。

## Background（生产实测数据）

- unicom 设备已正确配置 `protocol_analysis: true` + mosdns enabled，审计 API 可达，首次同步已导入 63 万条观测，预设域名库 13 个预设 1178 条规则就位。
- 实测：解析器窗口内 1152 条观测，TTL 存活着 0 条；conntrack 中 26 条连接能命中证据 key，全部死于 TTL 过期。
- `dns_features` 指纹表当前只写不读（`DNSFeaturesForMatch` 仅测试消费），且无任何清理，无限增长。
- 首次同步无水位时全量翻页（500 页/10 万条），耗时约 18 分钟、内存峰值 800MB+。

## Requirements

1. 识别证据源从 `dns_observations`（TTL 硬过期）改为 `dns_features` 指纹表：
   - 唯一时效门：`last_seen` 在 7 天内（常量，暂不开放配置）；
   - 同一 `(clientIP, answerIP)` 取 `last_seen` 最新者；
   - 不设 hit_count 门槛（用户明确不追求精准率，只要"大概"）；
   - TTL 不再参与判定（保留存储，仅展示/诊断用）。
2. 来源分档：指纹 `last_seen` 在 `match_window_minutes` 内 → 来源 `mosdns`（UI 显示"MosDNS 匹配"）；更老 → 来源 `mosdns-learned`（UI 显示"指纹推断"）。`match_window_minutes` 语义退化为分档分界线，不再决定能否识别。
3. 同步周期默认值 30 → 5 分钟（example 配置、保存接口兜底默认值、前端表单默认值）；存量显式配置不迁移。
4. 首次同步限深：无水位时只回溯最近 24 小时的审计日志（稳态增量同步不受影响）。
5. 每次同步时清理 `last_seen` 超过 7 天的指纹，使 `dns_features` 有界。
6. 域名→应用预设的 `MatchDomain` 归因逻辑、ambiguous 只回填域名的行为保持不变。

## Acceptance Criteria

- [ ] `go build ./...` 与 `go test ./...` 通过（含更新后的 resolver/store 测试）
- [ ] 前端 lint / build 通过
- [ ] 解析器单测覆盖：7 天时效门、newest-per-key、实锤/推断分档、ambiguous 行为不变
- [ ] 首次同步限深单测：无水位时 24h 之前的记录触发停页；有水位时不受影响
- [ ] 协议统计页对 `mosdns-learned` 来源有独立徽标文案
- [ ] PR 以 Draft 形式提交，保持 Draft 直到验收

## Out of Scope

- 生产部署与验收（走单独的生产验收门禁流程）。
- 存量子库/主库中历史 dns_observations 数据的迁移或清理策略调整（48h 保留不变）。

## Notes

- 相关讨论与实测过程见本会话；关键结论已写入本文件与 design.md。
