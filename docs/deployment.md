# 部署与更新

## systemd 部署

仓库提供了 [`deploy/rosboard.service`](../deploy/rosboard.service)。以下示例在 Linux 上将程序安装到 `/opt/rosboard`：

```bash
sudo useradd --system --home /opt/rosboard --shell /usr/sbin/nologin rosboard
sudo install -d -o rosboard -g rosboard /opt/rosboard
sudo install -o rosboard -g rosboard -m 0755 ./rosboard /opt/rosboard/rosboard
# 独立、稳定的守护副本：即使更新后的程序无法启动也能恢复
sudo install -o root -g root -m 0755 ./rosboard /opt/rosboard/rosboard-supervisor
sudo install -m 0644 deploy/rosboard.service /etc/systemd/system/rosboard.service
sudo systemctl daemon-reload
sudo systemctl enable --now rosboard
```

首次访问 `http://<服务器地址>:8080` 后按网页引导创建管理员并添加 RouterOS。服务的工作目录是 `/opt/rosboard`，因此首次保存设备时会自动创建 `/opt/rosboard/config.yaml`；不需要预先复制或编辑 YAML。

查看运行状态与日志：

```bash
systemctl status rosboard
journalctl -u rosboard -f
```

## 版本与在线更新（从 v0.2.0 开始）

在「面板设置 → 维护设置 → 版本与更新」中手动检查 GitHub 正式版本。只有更新的版本、匹配的 Linux 安装包和完整校验文件都存在时，才能确认安装；不提供后台自动检查、自动安装、重新安装或降级。页面只显示最近一次更新结果，详细错误见 `journalctl -u rosboard`。确认「完全重新初始化」时也会清除更新器保留的恢复副本。

在线安装需要上述 systemd 守护部署。守护副本启动面板子进程，下载及 SHA-256 校验期间面板继续运行；随后停止子进程，备份完整数据目录、配置和旧程序，原子替换，再验证新进程并允许其开始工作。验证前不启动采集或策略写入。启动失败时恢复旧程序及配套数据；中断的安装在下一次守护启动时先恢复。更新会短暂中断面板和采集，关闭浏览器不会取消已提交的更新。

从旧部署首次升级到 v0.2.0 时，需要先停止服务，再手动安装两个程序副本及新版 service 文件，执行 `systemctl daemon-reload` 后启动。后续在线更新仅替换 `rosboard`；`rosboard-supervisor` 保持稳定，不随在线更新替换。未来如果守护协议需要改变，会要求单独手动升级守护副本。手动运行的程序仍可检查版本，但安装按钮不可用。

- 默认更新状态位于程序旁的 `.rosboard-update/`，其中 `backup/previous/` 保留最近一次更新的完整恢复点。目录须仅服务账号可访问，备份包含敏感配置和数据库，不应提交或分享。
- 可通过 systemd 环境变量 `ROSBOARD_UPDATE_BACKUP_DIR` 指定专用备份目录；需要空间容纳完整数据和程序。数据、程序和备份路径不得相互覆盖或含符号链接。备份失败会停止安装。
- 设置 `ROSBOARD_UPDATE_DISABLED=1` 可禁用安装，仍允许检查版本。
- 下载由服务器访问官方 GitHub，支持 Go HTTP 客户端的 `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` 环境变量。不使用第三方镜像。检查失败会保留缓存信息并明确显示失败；每次检查有 30 秒冷却。
- SHA-256 校验用于检测文件损坏，信任来源是固定的官方 GitHub 仓库和 HTTPS；第一版没有额外的发布签名系统。

## 发布版本

根目录的 [`VERSION`](../VERSION) 是唯一的发布开关。普通代码提交不会创建 Release。准备下一版时，在任务分支上将 `VERSION` 改为新的正式语义版本；经过验证与验收后合并到 `main`，由 GitHub Actions 发布。不要为触发发布直接向 `main` 推送开发代码。

Actions 会运行后端测试、前端测试/lint/build/资源检查，并生成 `linux_amd64`、`linux_amd64-v3`、`linux_arm64`、`linux_armv7` 压缩包及 `sha256sums.txt`。版本号、提交号、构建时间和架构会注入可执行文件；`./rosboard version` 输出这些信息的 JSON。普通 `go build` 显示为开发构建，不能在线更新。

附件全部生成后先上传到 Draft Release，再发布正式版，避免在线更新读到未上传完整的版本。同一版本号已存在时失败，不覆盖既有 Release。`amd64-v3` 仅用于支持 x86-64-v3 的处理器；通用 x86 服务器使用 `amd64`。
