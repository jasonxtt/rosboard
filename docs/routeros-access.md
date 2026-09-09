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

- 生成的 RouterOS 专用账号权限固定为 `read,write,test,api,rest-api`。
- `write` 用于策略路由分流与访问控制：rosboard 会创建/更新/删除 mangle、routing rule、routing table、address-list、DNS static 等对象。**所有写入对象都带 `rbs_` 前缀的归属标记，rosboard 只管理自己创建的对象**，不触碰人工配置。
- `api` 用于兼容部分 RouterOS 版本中 REST 的内部 API 登录通道；账号仍不具备 `policy`、`sensitive` 等高危权限。
- 策略与规则下发前会做 RouterOS 能力探测（如定时规则的 `time` 字段支持情况）；不支持的能力会在页面以问题清单形式暴露，而不是静默失败。
- 账号和密码仅保存在本地 `0600` 权限的 config.yaml 中，设置接口和配置导出不返回密码。
- 脚本不会自动启用 www/www-ssl，也不会修改防火墙、证书或其他 RouterOS 设置。
- 接入会话 15 分钟后在 rosboard 端失效，需重新生成。已粘贴到 RouterOS 创建的账号不会自动删除。
- 归档由快速接入添加的设备后，rosboard 会显示一段不含密码的 RouterOS 清理脚本；该脚本删除专用用户，并且只在专用组没有其他用户时删除该组。清理前应确认不再恢复该设备。

## 版本要求

- 快速接入要求 **RouterOS 7** 或更高版本。
- HTTP REST 要求 **RouterOS 7.9** 或更高版本。
- 如果 www 未启用，需在 RouterOS 的 IP → Services 中自行启用，或改用 HTTPS。

## HTTP / HTTPS

默认通过 HTTP（明文）连接。HTTPS 可在高级设置中选择，但自签证书必须被 rosboard 主机信任；当前客户端不会擅自忽略 TLS 校验。HTTP Basic Auth 明文传输凭据，只应在可信局域网使用。
