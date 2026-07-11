# Switch live service to YAML config

## Goal

正式完成“取消空闲状态”的监控改动，并建立安全、真实的本机 YAML 配置，使 rosboard 以后无需对话解析或环境变量注入即可启动。

## Background

- 当前未提交业务代码是一组“取消空闲状态”的改动：后端将 RouterOS/租约/ARP/IPv6 邻居标记从 `idle` 改为 `online`，前端移除 `idle` 类型、筛选项和文案，嵌入式 JS 已重建；用户已明确要求纳入正式修改。
- `.gitignore` 另有一行 `configs/config.local.yaml`，用于保护本地真实凭据；该行与本任务目标一致。
- 已存在的 `configs/config.local.yaml` 字段完整，但凭据验证返回 RouterOS HTTP 401，文件权限为 `0644`。
- 当前 8080 没有监听进程；旧 `/tmp/run-rosboard-live.sh` 仍依赖从 Codex 会话提取密码并注入 `ROSBOARD_*`。

## Requirements

1. 终端运行状态正式收敛为 `online` / `offline` 两种：现有 RouterOS 地址、有效 DHCP 租约、完整/可达 ARP、可达 IPv6 邻居均标记在线，不再产生 `idle`。
2. 前端类型、筛选项、状态文案和状态样式移除 `idle`；“显示离线设备”按钮继续在默认在线与全部状态间切换。
3. 补齐相关测试、规范和嵌入式前端构建产物，并将这组业务改动纳入本任务提交。
4. `configs/config.local.yaml` 使用已验证的 RouterOS 地址、账号和密码，监听所有接口的 8080，并继续使用项目 `data` 目录。
5. 真实 YAML 保持 Git 忽略，文件权限设为仅当前用户可读写（`0600`）。
6. 新增固定本地启动脚本，直接执行 `rosboard -config configs/config.local.yaml`，不得解析聊天记录或注入 `ROSBOARD_*`。
7. 当前服务切换为该 YAML/脚本启动方式，并以脱敏方式验证 RouterOS 鉴权。

## Acceptance Criteria

- [x] 后端和前端源码中不再存在运行态 `idle` / “空闲”分支，测试与规范同步更新。
- [x] 默认在线列表和“显示离线设备”交互在二态模型下正常工作。
- [x] “取消空闲状态”的源码、测试、规范和构建产物纳入本任务提交。
- [x] 配置文件通过 RouterOS `/rest/system/resource` 鉴权，且不输出密码。
- [x] `configs/config.local.yaml` 权限为 `0600`，并被 Git 忽略。
- [x] 启动脚本不包含凭据或 `ROSBOARD_*` 环境变量。
- [x] 运行进程命令行包含 `-config .../configs/config.local.yaml`，父进程独立于 Codex 工具命令。
- [x] `127.0.0.1:8080` 与 `10.0.0.86:8080` 均返回 HTTP 200，Dashboard 更新时间持续前进。
- [x] 真实 YAML 不进入 Git；其余本任务业务代码、规范、构建产物、`.gitignore` 和启动脚本完整提交。

## Out of Scope

- 设置开机自启动或安装系统级服务。
