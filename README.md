# rosboard

面向 RouterOS 的轻量级只读监控面板。rosboard 将系统资源、接口状态、IPv4/IPv6 终端、连接跟踪、策略计数器和路由状态集中到一个适合局域网部署的 Web 界面中。

> 当前项目定位：多 RouterOS 设备、局域网部署、Linux 优先。rosboard 不会修改 RouterOS 配置；面板自身的连接与采集设置会写入本地 YAML 配置文件。

## 主要功能

- 系统概览：CPU、内存、存储、运行时间、实时流量和在线终端数
- 接口监控：物理与逻辑接口状态、地址、速率、累计流量及历史趋势
- 统一终端视图：关联 DHCP、ARP、IPv6 Neighbor 与 Firewall Connection 数据
- IPv4 / IPv6 分域查看：终端状态、连接、协议、流量与本地历史记录
- 策略与路由：只读展示 Simple Queue、Queue Tree、Mangle、Routing Rule 和路由表状态
- 首次初始化：先创建唯一管理员，再测试并添加零台或多台 RouterOS 设备
- 面板设置：设备管理、采集周期、账号安全、界面偏好与脱敏配置导出
- 本地持久化：使用 SQLite 保存采样数据、终端累计信息、名称和备注
- 响应式界面：支持桌面和移动端浏览

## 技术架构

| 层级 | 技术 |
| --- | --- |
| 后端 | Go、`net/http`、RouterOS REST API |
| 数据 | SQLite（`modernc.org/sqlite`，无需 CGO） |
| 前端 | React、TypeScript、Vite、ECharts |
| 交付 | 前端静态资源嵌入 Go 二进制，单进程运行 |

```text
Browser → rosboard HTTP/API → RouterOS REST API
                    └──────→ SQLite
```

## 环境要求

- Go 1.26.4（以 `go.mod` 为准）
- Node.js 与 npm（用于构建前端）
- 已启用且可从部署主机访问的 RouterOS REST API
- 一个遵循最小权限原则、能够读取所需 RouterOS 资源并调用接口流量监控的账号

## 快速接入 RouterOS

rosboard 提供"快速接入"方式简化 RouterOS 设备添加流程。

1. 用户只需填写**设备名称**和 **RouterOS IP/主机名**。
2. 默认通过 **HTTP/80** 连接（协议和端口位于"高级设置"中，默认收起）。
3. 后端自动生成：
   - 随机 RouterOS 用户名（`rosboard_<16位hex>`）
   - 随机 RouterOS 用户组名（`rosboard_g_<16位hex>`）
   - 32 位随机强密码
   - 一段可复制、可重复执行的 RouterOS 脚本
4. 将脚本粘贴到 RouterOS Terminal 执行，脚本会创建只读专用账号。
5. 回到 rosboard 点击"我已执行脚本，开始接入"，后端自动完成验证、识别和保存。

### 权限与安全

- 生成的 RouterOS 专用账号权限固定为 `read,test,api,rest-api`。
- `api` 用于兼容部分 RouterOS 版本中 REST 的内部 API 登录通道；账号仍不具备 `write`、`policy`、`sensitive` 等配置修改权限。
- rosboard 日常为只读，不主动修改 RouterOS 配置。
- 账号和密码仅保存在本地 `0600` 权限的 config.yaml 中，设置接口和导出不返回密码。
- 脚本不会自动启用 www/www-ssl，也不会修改防火墙、证书或其他 RouterOS 设置。
- 接入会话 15 分钟后在 rosboard 端失效，需重新生成。已粘贴到 RouterOS 创建的账号不会自动删除。
- 归档由快速接入添加的设备后，rosboard 会显示一段不含密码的 RouterOS 清理脚本；该脚本删除专用用户，并且只在专用组没有其他用户时删除该组。清理前应确认不再恢复该设备。

### 版本要求

- 快速接入要求 **RouterOS 7** 或更高版本。
- HTTP REST 要求 **RouterOS 7.9** 或更高版本。
- 如果 www 未启用，用户需在 RouterOS 的 IP → Services 中自行启用，或改用 HTTPS。

### HTTP/HTTPS

默认通过 HTTP（明文）连接。HTTPS 可在高级设置中选择，但自签证书必须被 rosboard 主机信任；当前客户端不会擅自忽略 TLS 校验。HTTP Basic Auth 明文传输凭据，只应在可信局域网使用。

## 快速开始

1. 安装并构建前端：

   ```bash
   cd web
   npm ci
   npm run build
   cd ..
   ```

2. 直接启动后端：

   ```bash
   go run ./cmd/rosboard
   ```

3. 打开 `http://127.0.0.1:8080`，先创建管理员账号，再按引导测试并添加 RouterOS。首次保存设备时，程序会在当前工作目录自动创建权限为 `0600` 的 `config.yaml`。RouterOS 步骤可以跳过，稍后从设备管理添加。

如需将配置放在指定位置，启动时传入路径即可；文件同样可以不存在：

```bash
go run ./cmd/rosboard -config /etc/rosboard/config.yaml
```

## 配置说明

完整示例见 [`configs/config.example.yaml`](configs/config.example.yaml)。未传 `-config` 时，rosboard 使用当前工作目录的 `./config.yaml`；传入 `-config` 时则严格使用指定路径。两种方式都允许配置文件在首次启动时不存在，网页引导首次保存 RouterOS 设置时会自动创建它。

| 字段 | 说明 |
| --- | --- |
| `listen_address` | 面板监听地址，默认 `:8080` |
| `data_dir` | SQLite 数据目录 |
| `poll_interval_seconds` | 完整 RouterOS 数据采集间隔，默认 `10` 秒 |
| `realtime_poll_interval_seconds` | 实时概览采集间隔，默认 `1` 秒 |
| `terminal_poll_interval_seconds` | 终端发现、地址与在线状态采集间隔，默认 `5` 秒；终端页当前速率由独立的 1 秒 conntrack 采集更新 |
| `sample_retention_hours` | 历史采样保留时长 |
| `allowed_cidrs` | 允许访问 `/api/*` 的客户端网段 |
| `devices[].id` | 设备稳定标识；创建后不应修改 |
| `devices[].name` | 面板中显示的设备名称 |
| `devices[].enabled` | 是否在后台持续采集该设备 |
| `devices[].routeros.*` | 每台设备的 REST 地址、账号、密码、采集接口和终端网段 |

设备由面板在连接测试通过后写入配置文件；每台设备至少需要一个采集接口和一个 IPv4/IPv6 本地 CIDR。支持 `ROSBOARD_LISTEN_ADDRESS` 和 `ROSBOARD_DATA_DIR` 环境变量覆盖。自动创建与后续更新的配置文件权限均为 `0600`。

### 采集与页面刷新

| 层级 | 默认周期 | 控制项 | 作用 |
| --- | --- | --- | --- |
| 完整采集 | 10 秒 | `poll_interval_seconds` | 系统、接口、地址、路由、策略、终端完整快照与接口流量 |
| 概览实时采集 | 1 秒 | `realtime_poll_interval_seconds` | CPU、内存、所选 WAN 接口速率与图表采样 |
| 终端发现采集 | 5 秒 | `terminal_poll_interval_seconds` | DHCP、ARP、IPv6 Neighbor、地址/MAC 关联、在线状态、累计流量与历史 |
| 终端当前速率 | 1 秒 | 固定 | 终端页面可见时仅读取 IPv4/IPv6 conntrack，更新当前上下行速率、连接和流量分类 |

终端当前速率采集不受上述三个配置项、也不受页面“自动刷新”设置控制；终端页面离开或隐藏约 30 秒后会自动停止。页面刷新设置只控制浏览器读取后端缓存并更新显示：终端列表、终端详情、实时概览以及负载/流量历史遵从所选周期；选择停止后只停止周期读取，立即刷新仍然有效。

## 开发

先启动 Go 后端，再在另一个终端启动 Vite 开发服务器：

```bash
go run ./cmd/rosboard
```

```bash
cd web
npm ci
npm run dev
```

Vite 会将 `/api` 请求代理到 `http://127.0.0.1:8080`。提交前可运行：

```bash
go test ./...
cd web
npm run lint
npm run build
```

## 构建与运行

生产构建必须先生成前端资源，再编译 Go 二进制：

```bash
cd web
npm ci
npm run build
cd ..
go build -o ./rosboard ./cmd/rosboard
```

本机可使用仓库中的启动脚本：

```bash
./scripts/run-local.sh
```

该脚本使用仓库根目录已忽略的 `config.yaml`；首次保存设备时自动创建该文件，且不会从环境或历史记录中提取凭据。

## 发布版本

根目录的 [`VERSION`](VERSION) 是唯一的发布开关，当前已发布版本为 `0.1.2`。普通代码提交不会创建 Release。准备下一版时，在任务分支上将 `VERSION` 改为新的正式语义版本（例如 `0.2.0`）；经过验证与验收后合并到 `main`，由 GitHub Actions 发布。不要为触发发布直接向 `main` 推送开发代码。

Actions 会运行后端测试、前端测试/lint/build/资源检查，并生成 `linux_amd64`、`linux_amd64-v3`、`linux_arm64`、`linux_armv7` 压缩包及 `sha256sums.txt`。版本号、提交号、构建时间和架构会注入可执行文件；`./rosboard version` 输出这些信息的 JSON。普通 `go build` 显示为开发构建，不能在线更新。

附件全部生成后先上传到 Draft Release，再发布正式版，避免在线更新读到未上传完整的版本。同一版本号已存在时失败，不覆盖既有 Release。`amd64-v3` 仅用于支持 x86-64-v3 的处理器；通用 x86 服务器使用 `amd64`。

## systemd 部署

仓库提供了 [`deploy/rosboard.service`](deploy/rosboard.service)。以下示例在 Linux 上将程序安装到 `/opt/rosboard`：

```bash
sudo useradd --system --home /opt/rosboard --shell /usr/sbin/nologin rosboard
sudo install -d -o rosboard -g rosboard /opt/rosboard
sudo install -o rosboard -g rosboard -m 0755 ./rosboard /opt/rosboard/rosboard
# A separate, stable copy can recover even when the updated executable cannot start.
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

### 版本与在线更新（从 v0.2.0 开始）

在“面板设置 → 维护设置 → 版本与更新”中手动检查 GitHub 正式版本。只有更新的版本、匹配的 Linux 安装包和完整校验文件都存在时，才能确认安装；不提供后台自动检查、自动安装、重新安装或降级。页面只显示最近一次更新结果，详细错误见 `journalctl -u rosboard`。确认“完全重新初始化”时也会清除更新器保留的恢复副本。

在线安装需要上述 systemd 守护部署。守护副本启动面板子进程，下载及 SHA-256 校验期间面板继续运行；随后停止子进程，备份完整数据目录、配置和旧程序，原子替换，再验证新进程并允许其开始工作。验证前不启动采集或策略写入。启动失败时恢复旧程序及配套数据；中断的安装在下一次守护启动时先恢复。更新会短暂中断面板和采集，关闭浏览器不会取消已提交的更新。

从旧部署首次升级到 v0.2.0 时，需要先停止服务，再手动安装两个程序副本及新版 service 文件，执行 `systemctl daemon-reload` 后启动。后续在线更新仅替换 `rosboard`；`rosboard-supervisor` 保持稳定，不随在线更新替换。未来如果守护协议需要改变，会要求单独手动升级守护副本。手动运行的程序仍可检查版本，但安装按钮不可用。

- 默认更新状态位于程序旁的 `.rosboard-update/`，其中 `backup/previous/` 保留最近一次更新的完整恢复点。目录须仅服务账号可访问，备份包含敏感配置和数据库，不应提交或分享。
- 可通过 systemd 环境变量 `ROSBOARD_UPDATE_BACKUP_DIR` 指定专用备份目录；需要空间容纳完整数据和程序。数据、程序和备份路径不得相互覆盖或含符号链接。备份失败会停止安装。
- 设置 `ROSBOARD_UPDATE_DISABLED=1` 可禁用安装，仍允许检查版本。项目维护者的生产实例必须继续遵守 `AGENTS.md` 的 NAS 备份和人工验收流程；在完成符合该流程的集成前禁用面板在线安装，不在生产机创建开发回滚备份。
- 下载由服务器访问官方 GitHub，支持 Go HTTP 客户端的 `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` 环境变量。不使用第三方镜像。检查失败会保留缓存信息并明确显示失败；每次检查有 30 秒冷却。
- SHA-256 校验用于检测文件损坏，信任来源是固定的官方 GitHub 仓库和 HTTPS；第一版没有额外的发布签名系统。

## 项目结构

```text
cmd/rosboard/       程序入口
configs/            配置示例
deploy/             systemd 服务文件
internal/api/       HTTP API 与静态页面服务
internal/config/    配置加载、校验与保存
internal/routeros/  RouterOS REST API 客户端
internal/service/   采集、关联与业务逻辑
internal/store/     SQLite 持久化
internal/ui/        嵌入 Go 二进制的前端构建产物
web/                React + TypeScript 前端源码
```

## 安全说明

- rosboard 使用单管理员账号和 7 天滚动会话；首次初始化页面受 `allowed_cidrs` 限制，仍不应直接暴露到公网。
- `/api/*` 受 `allowed_cidrs` 限制；请按实际管理网段收紧默认配置，并配合主机防火墙或反向代理访问控制。
- RouterOS 凭据保存在原子写入的本地 YAML 中，不会返回浏览器。请保持文件权限为 `0600`，使用专用的最小权限账号，并优先在可信网络中通过 HTTPS 连接 RouterOS。
- `config.yaml`、`configs/config.local.yaml`、`data/`、`web/node_modules/` 和本地 `rosboard` 二进制已加入 `.gitignore`。

忘记管理员密码时，可在服务器终端交互式重置；该操作会撤销全部现有会话：

```bash
rosboard admin reset-password -config /opt/rosboard/config.yaml
```

维护设置中的“完全重新初始化”是独立的不可撤销操作：确认后会删除配置文件、管理员、全部会话、所有 RouterOS 设备及采集历史，并在服务重启后回到首次创建管理员页面。“重置界面偏好”只影响当前浏览器，两者不会互相替代。

## 当前限制

- 以 Linux 和 systemd 部署为主，暂未提供 Docker 镜像
- RouterOS 硬件能力与版本差异可能导致部分健康、IPv6 或策略数据不可用
- 项目尚未提供开源许可证；公开仓库仅用于当前阶段的代码归档与协作

## 双 UI 面板

当前提供 Compact 紧凑版和 Aurora 玻璃版，完整保留各自页面、导航和图表，共用后端、账号与设备数据。原 main 界面已被这两套 UI 替代。

在任一界面的「面板设置 → 界面设置 → UI 风格」选择另一套 UI，点击「保存并切换 UI」。切换会重新加载页面，请先保存其他编辑；浏览器会记住选择，首次访问默认 Aurora。明暗主题、刷新间隔和设备选择兼容共享。也可通过 `/?ui=compact` 或 `/?ui=aurora` 指定入口。

前端开发使用 `npm --prefix web install`，执行 `npm --prefix web test`、`npm --prefix web run lint`、`npm --prefix web run build` 和 `npm --prefix web run check:ui-build`。开发 API 默认代理本地 `127.0.0.1:8090`；可用 `ROSBOARD_DEV_PROXY` 指定隔离后端。
