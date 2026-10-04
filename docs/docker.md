# Docker 部署

镜像 `jasonxtt/rosboard` 随版本发布，支持 `linux/amd64` 与 `linux/arm64`。镜像内已内嵌前端，无需任何外部资源文件。

## 镜像与标签

- `jasonxtt/rosboard:v0.2.5`（与 GitHub Release / 二进制包版本一致）
- `jasonxtt/rosboard:0.2.5`
- `jasonxtt/rosboard:latest`

## 快速开始（推荐：宿主网络）

RouterOS 的地址在初始化引导里由你填写（表单预填 `10.0.0.1`，可改），通常是局域网地址。**桥接网络下容器不一定能直达这个地址**（RouterOS 看到的来源地址也会变成 Docker 网桥），宿主网络下语义与裸机部署完全一致，且面板监听 `:8080` 无需端口映射：

```bash
mkdir -p /opt/rosboard-docker && chown 1000:1000 /opt/rosboard-docker
docker run -d --name rosboard \
  --network host \
  -v /opt/rosboard-docker:/var/lib/rosboard \
  --restart unless-stopped \
  jasonxtt/rosboard:latest
```

然后打开 `http://<宿主机>:8080`：先创建管理员账号，再按「快速接入」引导接入 RouterOS。仓库根目录提供 `docker-compose.host.yml.example`（推荐）与 `docker-compose.bridge.yml.example`（端口映射 + 需显式配置 RouterOS 地址）两个模板。

## 数据与配置

镜像只声明一个卷 `/var/lib/rosboard`，即工作目录：

```
/var/lib/rosboard
├── config.yaml        # 首次保存设备时自动生成（权限 0600）
└── data/              # SQLite 数据库、每设备数据、采样样本
```

- 容器以非 root 用户 `rosboard`（UID/GID 1000）运行。bind mount 的宿主目录需要 `chown 1000:1000`，否则容器内无法写入；使用命名卷（`-v rosboard-data:/var/lib/rosboard`）则无需处理属主。
- 空卷首次启动即进入初始化引导，无需预先准备配置文件。

## 环境变量

容器内可直接用环境变量覆盖配置（完整列表见[配置参考](configuration.md)）：

| 变量 | 作用 |
| --- | --- |
| `ROSBOARD_PORT` | Web UI 端口（默认 `8080`）。宿主网络模式下改面板端口只需设这一个变量；桥接模式下改外部端口用 `ports` 映射即可，无需设置 |
| `ROSBOARD_LISTEN_ADDRESS` | 完整监听地址（如 `127.0.0.1:8081`），优先级高于 `ROSBOARD_PORT` |
| `ROSBOARD_DATA_DIR` | 数据目录（默认 `./data`，落在卷内；也可在 config.yaml 设置 `data_dir`） |
| `ROSBOARD_ROUTEROS_BASE_URL` | RouterOS REST API 地址（桥接网络下必设） |
| `ROSBOARD_ROUTEROS_USERNAME` / `ROSBOARD_ROUTEROS_PASSWORD` | RouterOS 凭据（也可以走面板内的快速接入流程） |

## 网络模式

| 模式 | 说明 |
| --- | --- |
| `network_mode: host`（推荐） | RouterOS 地址、`trusted_proxy_cidrs` 等配置与裸机部署完全同语义 |
| 桥接 + 端口映射 | 必须设置 `ROSBOARD_ROUTEROS_BASE_URL` 为容器可达地址；RouterOS 看到的来源地址会变成 Docker 网桥地址 |

反向代理场景（Caddy/nginx 前置 HTTPS）的注意事项与裸机一致：在 config.yaml 设置 `trusted_proxy_cidrs: true`，详见[部署与更新的反向代理一节](deployment.md#https-reverse-proxy)。

## 升级与更新

镜像内已禁用在线自更新（`ROSBOARD_UPDATE_DISABLED=1`）——容器里原地替换二进制既不持久也无意义。升级方式：

```bash
docker compose pull && docker compose up -d
```

配置和数据都在卷里，容器重建后直接恢复。

## 进程重启语义

保存设备、修改关键配置后，程序会**主动优雅退出再启动**（systemd 部署下由 systemd 拉起）。镜像内的 entrypoint 会在容器里自动把进程拉起来，即使 `docker run` 时忘了 `--restart` 也能自愈；进程异常崩溃（非 0 退出）时 entrypoint 不做循环，交给 Docker 的重启策略处理。

因此**仍建议**始终带上 `--restart unless-stopped`（compose 示例已带）：它额外覆盖崩溃自愈、Docker 守护进程重启后自动拉起这两种 entrypoint 兜不住的情况。

## 运维

```bash
# 查看构建信息（版本、提交、架构）
docker exec rosboard rosboard version

# 重置管理员密码
docker exec -it rosboard rosboard admin reset-password -config /var/lib/rosboard/config.yaml

# 查看日志
docker logs -f rosboard
```

镜像自带健康检查（`wget http://127.0.0.1:8080/`），`docker ps` 可见健康状态。`docker stop` 会触发 SIGTERM 优雅关停（最长 10 秒）。

## 从源码构建镜像

```bash
docker buildx build --platform linux/amd64 \
  --build-arg VERSION=0.2.5-local --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILT_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t rosboard:local --load .
```

构建过程与发布流水线一致：Node 阶段构建前端 → Go 阶段交叉编译静态二进制 → Alpine 运行时镜像。CI（`.github/workflows/docker.yml`）在 `VERSION` 变更时自动构建并推送多架构镜像。
