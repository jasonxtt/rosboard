# 执行计划

1. 核对 GitHub 分支 `feat/policy-access-rebuild` 最新 HEAD、当前 helper、Access Control member evaluation 和既有回归测试。
2. 在 `internal/policyv2/routing_desired.go` 增加地址族级来源解析结果，分离“解析地址”与“物化地址列表”，并让 selected/excluded/all 按各自安全语义决定 IPv6 激活。
3. 在现有 policy desired 测试中先添加可复现用例，再实现最小修改，验证 IPv6 foundation、来源地址列表、mangle 激活和 warning。
4. 运行定向 Go 测试，再运行 `go test -count=1 ./internal/policyv2/...`、`go test -count=1 ./internal/api/...`、`go test ./...`、`go vet ./...`、`go build ./...`、Web lint/build/audit、`git diff --check` 和 `Trellis validate`。
5. 只暂存本任务涉及的 tracked 文件，检查 staged diff、路径和敏感信息，创建 focused commit，推送同一分支。
6. 将最新完整 HEAD 交给项目审核对话 root review；若 `CHANGES REQUIRED`，按逐条意见修改、验证、提交、推送并再次审核，直到 `APPROVED`。
7. 只有 root review `APPROVED` 后构建 Linux amd64，部署到 `10.0.0.60`，检查 service/health/embedded assets，再交由用户实际验证；不部署生产机。

回滚点：实现提交前保留工作区现状；测试机仅替换 `/opt/rosboard-test/rosboard`，如需回滚使用部署前的测试机二进制备份。生产 acceptance gate 不适用于本次测试机部署。
