# Design

## Configuration

使用现有 `config.Load(path)` 原生 YAML 入口。真实配置固定为 `configs/config.local.yaml`，保留 `configs/config.example.yaml` 作为公开模板。环境变量覆盖能力仍存在，但本地启动脚本不设置任何 `ROSBOARD_*`。

## Secret handling

真实配置只保存在被忽略的本机文件中，权限 `0600`。验证仅输出 HTTP 状态、字段是否存在和文件权限，不打印密码。

## Startup

新增 `scripts/run-local.sh`，从脚本位置解析项目根目录，并 `exec` 已构建二进制及绝对 YAML 路径。当前运行通过 macOS 用户会话启动该脚本后 `nohup/disown`，避免 Codex 命令退出时回收进程。

## Terminal state model

监控输出收敛为二态：发现当前 RouterOS 地址、DHCP、ARP/邻居可达证据或活动连接时为 `online`，没有当前证据并超出保留条件时为 `offline`。前端联合类型和筛选项与此契约一致，删除未使用的 `.state-idle` 样式。

## Commit boundary

暂存“取消空闲状态”的 service/frontend/test/spec/dist 改动，以及本任务新建的脚本、`.gitignore` 保护规则和 Trellis 文档。真实 YAML 始终保持 ignored/untracked。
