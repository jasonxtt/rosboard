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

## 快速开始

1. 安装并构建前端：

   ```bash
   cd web
   npm ci
   npm run build
   cd ..
   ```

2. 创建本地配置：

   ```bash
   cp configs/config.example.yaml configs/config.local.yaml
   chmod 600 configs/config.local.yaml
   ```

3. 按管理网段调整 `allowed_cidrs`，然后启动后端：

   ```bash
   go run ./cmd/rosboard -config ./configs/config.local.yaml
   ```

4. 打开 `http://127.0.0.1:8080`，先创建管理员账号，再按引导测试并添加 RouterOS。RouterOS 步骤可以跳过，稍后从设备管理添加。

## 配置说明

完整示例见 [`configs/config.example.yaml`](configs/config.example.yaml)。

| 字段 | 说明 |
| --- | --- |
| `listen_address` | 面板监听地址，默认 `:8080` |
| `data_dir` | SQLite 数据目录 |
| `poll_interval_seconds` | 常规 RouterOS 数据采集间隔 |
| `realtime_poll_interval_seconds` | 实时概览采集间隔 |
| `terminal_poll_interval_seconds` | 终端与连接数据采集间隔 |
| `sample_retention_hours` | 历史采样保留时长 |
| `allowed_cidrs` | 允许访问 `/api/*` 的客户端网段 |
| `devices[].id` | 设备稳定标识；创建后不应修改 |
| `devices[].name` | 面板中显示的设备名称 |
| `devices[].enabled` | 是否在后台持续采集该设备 |
| `devices[].routeros.*` | 每台设备的 REST 地址、账号、密码、采集接口和终端网段 |

设备由面板在连接测试通过后写入配置文件；每台设备至少需要一个采集接口和一个 IPv4/IPv6 本地 CIDR。支持 `ROSBOARD_LISTEN_ADDRESS` 和 `ROSBOARD_DATA_DIR` 环境变量覆盖。长期部署建议使用权限为 `0600` 的配置文件。

## 开发

先启动 Go 后端，再在另一个终端启动 Vite 开发服务器：

```bash
go run ./cmd/rosboard -config ./configs/config.local.yaml
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

该脚本读取已忽略的 `configs/config.local.yaml`，不会从环境或历史记录中提取凭据。

## systemd 部署

仓库提供了 [`deploy/rosboard.service`](deploy/rosboard.service)。以下示例在 Linux 上将程序安装到 `/opt/rosboard`：

```bash
sudo useradd --system --home /opt/rosboard --shell /usr/sbin/nologin rosboard
sudo install -d -o rosboard -g rosboard /opt/rosboard
sudo install -o rosboard -g rosboard -m 0755 ./rosboard /opt/rosboard/rosboard
sudo install -o rosboard -g rosboard -m 0600 configs/config.example.yaml /opt/rosboard/config.yaml
sudo install -m 0644 deploy/rosboard.service /etc/systemd/system/rosboard.service
sudoedit /opt/rosboard/config.yaml
sudo systemctl daemon-reload
sudo systemctl enable --now rosboard
```

查看运行状态与日志：

```bash
systemctl status rosboard
journalctl -u rosboard -f
```

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
- `configs/config.local.yaml`、`data/`、`web/node_modules/` 和本地 `rosboard` 二进制已加入 `.gitignore`。

忘记管理员密码时，可在服务器终端交互式重置；该操作会撤销全部现有会话：

```bash
rosboard admin reset-password -config /opt/rosboard/config.yaml
```

维护设置中的“完全重新初始化”是独立的不可撤销操作：确认后会删除配置文件、管理员、全部会话、所有 RouterOS 设备及采集历史，并在服务重启后回到首次创建管理员页面。“重置界面偏好”只影响当前浏览器，两者不会互相替代。

## 当前限制

- 以 Linux 和 systemd 部署为主，暂未提供 Docker 镜像
- RouterOS 硬件能力与版本差异可能导致部分健康、IPv6 或策略数据不可用
- 项目尚未提供开源许可证；公开仓库仅用于当前阶段的代码归档与协作
