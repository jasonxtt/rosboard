# Implementation plan

1. 为 `MergeTerminal` 增加重复地址合并回归测试，先证明当前直接 UPDATE 会触发唯一约束。
2. 将地址移动改为 upsert + delete，并保留最大 `last_seen`。
3. 把 LAN 设备计数改为只接受 `online`，同步测试和 UI 提示。
4. 运行 Go 测试/race/vet及前端 lint/build，检查 diff。
5. 构建二进制并重启 `0.0.0.0:8080` 实时服务。
6. 在当前数据库上连续采样 Dashboard 和日志，验证更新时间、速率及连接数恢复变化，检查 loopback/LAN HTTP 200。

## Rollback

- 代码变更仅影响本地 SQLite 合并和展示统计；可回滚二进制。
- 不执行破坏性数据库清理，不影响 RouterOS。
