# 接入 RouterOS

rosboard 提供「快速接入」方式简化 RouterOS 设备添加流程。

## 快速接入流程

1. 用户只需填写**设备名称**和 **RouterOS IP/主机名**。
2. 默认通过 **HTTP/80** 连接（协议和端口位于「高级设置」中，默认收起）。
3. 后端自动生成：
   - 随机 RouterOS 用户名（`rosboard_<16位hex>`）
   - 随机 RouterOS 用户组名（`rosboard_g_<16位hex>`）
   - 32 位随机强密码
   - 一段可复制、可重复执行的 RouterOS 脚本
4. 将脚本粘贴到 RouterOS Terminal 执行，脚本会创建专用账号。
5. 回到 rosboard 点击「我已执行脚本，开始接入」，后端自动完成验证、识别和保存。

## 权限与安全

- 生成的 RouterOS 专用账号权限固定为 `read,write,test,api,rest-api,ssh,ftp`。
- `write` 用于策略路由分流与访问控制：rosboard 会创建/更新/删除 mangle、routing rule、routing table、address-list、DNS static 等对象。**所有写入对象都带 `rbs_` 前缀的归属标记，rosboard 只管理自己创建的对象**，不触碰人工配置。
- `api` 用于兼容部分 RouterOS 版本中 REST 的内部 API 登录通道；账号仍不具备 `policy`、`sensitive` 等高危权限。
- 策略与规则下发前会做 RouterOS 能力探测（如定时规则的 `time` 字段支持情况）；不支持的能力会在页面以问题清单形式暴露，而不是静默失败。
- 账号和密码仅保存在本地 `0600` 权限的 config.yaml 中，设置接口和配置导出不返回密码。
- 脚本不会自动启用 www/www-ssl、SSH 或 FTP 服务，也不会修改防火墙、证书、主机密钥信任或容器写入开关。
- 接入会话 15 分钟后在 rosboard 端失效，需重新生成。已粘贴到 RouterOS 创建的账号不会自动删除。
- 归档由快速接入添加的设备后，rosboard 会显示一段不含密码的 RouterOS 清理脚本；该脚本删除专用用户，并且只在专用组没有其他用户时删除该组。清理前应确认不再恢复该设备。

## 版本要求

- 快速接入要求 **RouterOS 7** 或更高版本。
- HTTP REST 要求 **RouterOS 7.9** 或更高版本。
- 如果 www 未启用，需在 RouterOS 的 IP → Services 中自行启用，或改用 HTTPS。

## HTTP / HTTPS

默认通过 HTTP（明文）连接。HTTPS 可在高级设置中选择，但自签证书必须被 rosboard 主机信任；当前客户端不会擅自忽略 TLS 校验。HTTP Basic Auth 明文传输凭据，只应在可信局域网使用。

## Transfer authority and existing accounts

`ssh` and `ftp` group policies allow SFTP image transfer in the tested RouterOS
7.23.7 configuration. The FTP service can remain disabled. SSH service/port,
source restrictions, reachability and a separately verified pinned host key are
independent prerequisites. The connection account has no `policy` permission
and cannot elevate itself. Software upgrades do not change router-side roles.

Both account settings views provide **镜像传输权限与旧账号升级**. Its check only
reads the selected device. A generated upgrade script requires recorded generated
username/group metadata, fresh matching object IDs, the managed ownership marker
and exactly one intended group member. Prefix-inferred legacy, shared, disabled,
foreign or ambiguous accounts are refused. Run the script as an administrator on
the same router, then use **检查权限 / 执行后重新验证** for fresh role/SFTP reads.

Only the exact old five-right role can become the exact seven-right role.
Already-new roles cause no group/user/password write. Every other role is rejected
unchanged, including extra, missing or partial permissions. Known disabled `!`
flags and ordering are normalized; unknown or contradictory data is rejected.
Scripts check the live role immediately before setting and check identity and
exact role afterward. These are optimistic guards, not atomic RouterOS CAS. Stop
on drift, read-back failure or an unknown result and inspect manually; do not
blindly rerun. The upgrade preserves the username, password and application
configuration. Repeated new-account scripts also preserve existing passwords;
a collision with an old role requires a fresh identity-bound upgrade instead.

Ordinary checks authenticate and list metadata over pinned SFTP only. They never
create a test file or claim verified upload success. Missing SSH does not block
monitoring-only onboarding. There is no optional write-probe endpoint. Actual
image uploads still require private per-device `container_writes: true` and the
shared write gate.

By default the server reuses saved connection credentials and the REST endpoint's
host with optional `container_ssh_port` (default 22). On the first explicit image
create/update/upload, it authenticates REST, observes the SSH key without sending
credentials, authenticates and reads SFTP with that exact key, then persists
`container_ssh_host_key` automatically. No manual fingerprint entry or confirmation
is required. This is trust on first use, not an independent cryptographic binding
between REST and SSH. Subsequent connections require the saved key; a changed key
is rejected rather than silently re-enrolled. Ordinary GET/preview checks do not
save trust. Connection changes during enrollment reject the stale result. An optional
private per-device `container_transfer` mapping accepts `username`, `password`,
`host_key`, and `port` for a dedicated account. These settings can also be saved in the per-device image-transfer panel after
a read-only SSH/SFTP verification. Passwords are never returned to the browser;
the panel shows only whether a password is saved. Saving transfer settings does
not enable the independent private container-write opt-in. An incomplete override fails rather
than borrowing the connection password; its initial SSH key is verified automatically. Removing the override restores the
connection source. Changing the REST endpoint or credentials through the device
editor clears transfer configuration, the pinned key and container-write opt-in.
