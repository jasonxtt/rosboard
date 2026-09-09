# 安全说明

- rosboard 使用单管理员账号和 7 天滚动会话；首次初始化页面受 `allowed_cidrs` 限制，仍不应直接暴露到公网。
- `/api/*` 受 `allowed_cidrs` 限制；请按实际管理网段收紧默认配置，并配合主机防火墙或反向代理访问控制。
- rosboard 会向 RouterOS 写入策略路由与访问控制配置，但**只管理自己创建的对象**：所有写入对象带 `rbs_` 前缀的归属标记，人工配置和其他工具创建的对象不受影响。快速接入账号权限为 `read,write,test,api,rest-api`，无 `policy`、`sensitive` 权限。
- RouterOS 凭据保存在原子写入的本地 YAML 中，不会返回浏览器。请保持文件权限为 `0600`，使用专用的最小权限账号，并优先在可信网络中通过 HTTPS 连接 RouterOS。
- `config.yaml`、`configs/config.local.yaml`、`data/`、`web/node_modules/` 和本地 `rosboard` 二进制已加入 `.gitignore`。

## 忘记管理员密码

可在服务器终端交互式重置；该操作会撤销全部现有会话：

```bash
rosboard admin reset-password -config /opt/rosboard/config.yaml
```

## 完全重新初始化

维护设置中的「完全重新初始化」是独立的不可撤销操作：确认后会删除配置文件、管理员、全部会话、所有 RouterOS 设备及采集历史，并在服务重启后回到首次创建管理员页面。「重置界面偏好」只影响当前浏览器，两者不会互相替代。
