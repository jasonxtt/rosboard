# Implementation plan

1. 脱敏审计现有 YAML、未提交 diff、运行状态和旧包装器。
2. 完成二态终端模型：更新剩余测试、CSS、规范并重建嵌入式前端。
3. 修正本地 YAML 为已验证凭据，设置 `0600`，验证 Git ignore。
4. 新增无环境变量的 `scripts/run-local.sh` 并设置可执行权限。
5. 用 YAML 启动新二进制，验证进程参数、RouterOS API、Dashboard 与 LAN HTTP 200。
6. 运行全量测试并暂存本任务所有正式改动，确认真实 YAML 不进入 staged diff。

## Rollback

- 停止 YAML 启动进程即可；不修改 RouterOS。
- 旧 `/tmp` 包装器保留但不再使用，必要时仍可人工回退。
