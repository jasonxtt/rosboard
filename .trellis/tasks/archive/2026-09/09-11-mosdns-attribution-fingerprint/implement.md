# 执行计划

1. [ ] store：`DNSFeaturesForMatch(ctx, since)` 加过滤 + 排序改 last_seen 优先；新增 `PruneDNSFeatures`
2. [ ] resolver：指纹证据 + 7 天时效 + newest-per-key + 来源分档；常量 `dnsFeatureMaxAge`
3. [ ] mosdns sync：首次 24h 限深 + 指纹清理调用
4. [ ] monitor：`ApplicationSource` 使用 Resolve 返回值
5. [ ] 默认值 5 分钟三处（example yaml / server.go / RecognitionCard）+ 识别页文案对齐
6. [ ] 前端 ProtocolsPage 徽标 `mosdns-learned` → 「指纹推断」
7. [ ] 更新/新增测试：`application_resolver_test.go`、`dns_test.go`、mosdns sync 测试
8. [ ] 验证：`go build ./...`；`go test ./...`；`cd web && npm run lint（如有）&& npm run build`
9. [ ] 提交、推送、`gh pr create --draft`

验证命令：
- `go build ./... && go test ./...`
- `cd web && npm run build`

提交计划（单 commit）：`fix(recognition): 指纹化 MosDNS 归因证据，修复 TTL 饿死`

回滚点：本任务无 schema/数据变更，回滚即还原代码。
