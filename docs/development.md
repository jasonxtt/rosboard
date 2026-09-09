# 开发指南

## 技术架构

| 层级 | 技术 |
| --- | --- |
| 后端 | Go、`net/http`、RouterOS REST API |
| 数据 | SQLite（`modernc.org/sqlite`，无需 CGO） |
| 前端 | React、TypeScript、Vite、ECharts |
| 交付 | 前端静态资源嵌入 Go 二进制，单进程运行 |

## 开发环境

先启动 Go 后端，再在另一个终端启动 Vite 开发服务器：

```bash
go run ./cmd/rosboard
```

```bash
cd web
npm ci
npm run dev
```

前端开发使用 `npm --prefix web install`，执行 `npm --prefix web test`、`npm --prefix web run lint`、`npm --prefix web run build` 和 `npm --prefix web run check:ui-build`。开发 API 默认代理本地 `127.0.0.1:8090`；可用 `ROSBOARD_DEV_PROXY` 指定隔离后端。

提交前可运行：

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

## 项目结构

```text
cmd/rosboard/            程序入口（serve / supervise / admin reset-password / version）
configs/                 配置示例
deploy/                  systemd 服务文件
docs/                    使用文档与界面截图
internal/api/            HTTP API 与静态页面服务
internal/config/         配置加载、校验与保存
internal/routeros/       RouterOS REST API 客户端
internal/service/        采集、关联与业务逻辑
internal/policy/         分流目标源解析（Clash YAML / 域名 / IP / 远程订阅）
internal/policyv2/       策略路由规范模型与调和引擎
internal/accesscontrol/  访问控制规则模型与定时阻断
internal/ownership/      写入对象的 rbs_ 归属命名空间
internal/subject/        规则主体抽象（被分流与访问控制共用）
internal/applicationpreset/ 内置应用预设目录
internal/mosdns/         MosDNS 审计日志客户端
internal/update/         在线更新与 supervisor 守护协议
internal/store/          SQLite 持久化
internal/ui/             嵌入 Go 二进制的前端构建产物
web/                     React + TypeScript 前端源码（Aurora / Compact 双 UI）
```

## 发布

发布流程见[部署与更新](deployment.md#发布版本)。
