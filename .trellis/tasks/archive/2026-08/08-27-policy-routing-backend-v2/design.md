# 策略路由后端 V2 技术设计

## 1. 边界

后端只保留一条主数据流：

```text
前端草稿 → SQLite V2 配置 → Desired Builder
                              ↓
RouterOS Scanner → 简单 Diff → 计划预览 → 幂等 Apply → 结果状态
```

API DTO 与内部模型分离。API 保持当前前端字段形状，内部不沿用旧 Planner/Executor 的类型体系。

## 2. 组件

### 2.1 Repository

使用六类设备级数据：egress、egress family、source、source version、source rule、device state。device state 保存 desired/applied revision、最近 apply ID/state/error/timestamps，不建设任务历史系统。

若现有数据库包含旧策略数据，只迁移用户配置、来源内容和当前 active/pending 版本；不迁移旧 plan、job、journal、backup、runtime mapping。旧表在人工验收前保留但不再读取，保证二进制回退不会被破坏。

删除规则：

- 从未应用的记录直接物理删除。
- 已应用的出口或来源先设 `pending_deletion`，成功应用清理 RouterOS 后再物理删除。
- 保存来源内容只创建 pending version；成功应用后才提升为 active/last-good。

### 2.2 Desired Builder

输入是单设备 V2 配置和待应用规则；输出是扁平 `DesiredObject[]`：

```go
type DesiredObject struct {
    LogicalID string
    Menu      string
    Fields    map[string]string
    Order     int
    Phase     string
}
```

`LogicalID` 必须稳定，受管 comment 包含安装身份、设备身份和 LogicalID。共享/专用 address-list、Fake DNS、IPv4/IPv6 和固定写入顺序都在一个 builder 内以普通函数实现，不引入图框架。

出口状态分为三种普通期望状态：启用时规则为 `disabled=no`；停用时仍输出该出口的稳定对象，但把支持停用的 DNS Static、route、routing rule、mangle 和 NAT 设为 `disabled=yes`；待删除时不再输出该出口对象，由 diff 精确清理。routing table、DNS forwarder 和设备级策略流量入口作为无害基础设施在停用时保留。共享关系由所有出口期望对象的并集自然决定，不增加引用计数数据库。

设备级策略流量入口只在至少存在一条非待删除策略时进入 Desired；最后一条策略删除后，diff 自然删除聚合 interface list 及成员，但 SQLite 中保存的用户入口选择不清空。

受管 comment 由“稳定身份段 + 可读说明段”组成。Scanner 只用稳定身份段匹配所有权和 LogicalID，同时把完整 comment 纳入字段差异，因此 comment 说明变化不会丢失所有权或把对象误判为 foreign。可读说明包含策略名、域名列表名和用途。专用 address-list 名称使用经过 RouterOS 安全归一化的域名列表名称关键字及短 ID 哈希，兼顾可读性和碰撞隔离；域名列表改名会作为显式结构变更更新其专用列表引用。

策略流量入口使用设备级 `{interfaceLists: string[], interfaces: string[]}` 契约。discovery 只返回 RouterOS 实际存在且适合作为三层入口的候选，不返回 `all / none / dynamic / static`，也不允许前端手工输入。存在 `LAN` interface list 且尚未保存入口配置时由前端默认选择它。

Desired Builder 创建一个精确所有权的聚合 interface list：所选用户列表写入 `include`，所选固定接口写入受管 list member，全部 LAN/WireGuard/VPN 业务 mangle 只引用该聚合列表。动态 L2TP/SSTP/OpenVPN 不绑定短生命周期接口名，只允许通过现有用户列表（例如由 VPN 配置维护的 `VPN-LAN`）加入；VPN 配置不在 rosboard 范围内。

### 2.3 Scanner 与 Diff

Scanner 做两件事：

- discovery 扫描前端选择 WAN/策略流量入口所需的只读拓扑，并标明接口已被哪些候选列表覆盖；
- apply 扫描相关菜单和本安装、本设备的受管对象。

Diff 按 `(menu, logicalID)` 比较期望与实际归一化字段，输出 `create / patch / move / delete`。foreign/manual 对象可以成为 blocker，但绝不生成写操作。计划指纹是归一化草稿和实际受管对象的普通 SHA-256，用于拒绝过期预览，不承担所有权或恢复语义。

### 2.4 Plan Cache

`POST /plans` 扫描并计算计划，将 `{planID, deviceID, desiredRevision, actualFingerprint, operations, expiresAt}` 放入进程内短期 cache。重启后计划自然失效。apply 必须重新读取 revision 和扫描 fingerprint；不一致返回 stale plan，用户重新生成即可。

API 仍返回前端需要的 `planID/planHash/summary/blockers/warnings/acknowledgements/operations`，不要求内部存在旧 ChangePlan 聚合体。

### 2.5 Applier

每设备一个非阻塞互斥锁。HTTP handler 依赖现有面板登录会话，随后返回 job ID，并由一个短生命周期 worker 执行：

1. 重新验证 plan、revision 和 fingerprint。
2. 创建或更新 foundation、routing、DNS 和列表对象。
3. patch 或启用 mangle/NAT 等激活对象；同一 LogicalID 尽量原地修改。
4. flush DNS cache。
5. 删除已失效且身份精确匹配的受管对象。
6. 只读重扫关键字段；成功提交 applied revision、来源版本和 tombstone。

状态复用前端现有枚举的最小子集：`queued → staging → verifying → committed|failed`。进程启动时发现非终态任务，直接标记 failed/interrupted；不自动续跑。任何写入失败立即停止，不做全局回滚；下一次 apply 以 RouterOS 实际状态重新 diff，因此必须可重入且不得依赖内存步骤历史。

## 3. API 范围

必须保留：

- `GET /overview`, `GET /discovery`
- 复用设备接入的 provisioning session complete；不再提供策略专用 `/access` 或策略账号 cleanup API
- `PUT /lan-scope`
- egress/source 的 GET、POST、PUT、DELETE
- URL/upload preview、source rules 分页
- `POST /plans`, `POST /plans/{id}/apply`
- `GET /jobs/{id}` 和 overview 中的 `activeJobs`

暂不实现或不从 overview 暴露入口：drift recovery、adoption/takeover、job cancel/resume/rollback、audit、backup download。前端已有但当前不可达的辅助函数不构成 V2 首版要求。

## 4. 来源刷新

复用经过验证的 Clash parser 和安全 URL fetcher。一个简单 ticker 查询到期 URL 来源并生成 pending version，更新 `nextRunAt`；不生成计划、不自动 apply。单个来源刷新失败只记录来源错误和下一次时间，不影响其他来源。

## 5. 错误与并发

- 继续使用现有 policy error JSON 形状和设备校验 helper。
- revision 冲突返回 409；无效输入返回 400/422；设备忙返回 409；RouterOS 写入失败返回任务 failed 状态。
- Repository 更新使用短 SQLite transaction；网络请求和 RouterOS 调用不得持有数据库事务。
- apply worker 只串行化同一设备，不建立全局 manager 状态机；apply API 以已认证登录会话授权，不再传递或校验管理员密码。

## 7. 设备账号与权限

- 删除 `DeviceConfig.PolicyAccess` 及策略账号专用 API、前端类型和 provisioning 代码；策略 runtime 的 reader/mutation client 都使用 `DeviceConfig.RouterOS` 凭据。
- 快速接入脚本把受管组权限从 `read,test,api,rest-api` 调整为 `read,write,test,api,rest-api`，不授予 `policy`，因此账号能改普通配置但不能管理 RouterOS 用户。
- 设备账号信息通过 RouterOS `/user` 与 `/user/group` 读取当前用户名、组和 policy，向前端投影为可写、只读或未知。策略页在确认无 `write` 时显示设备管理入口并阻止写操作。
- 设备管理连接行只承载设备名称、协议、主机和端口；账号模块单独展示用户名和权限。编辑账号复用现有设备验证/保存契约，一键更换复用现有设备 onboarding session，不建设第二套账号 API。

## 6. 切换与回退

新 V2 代码先与旧包并存，通过 runtime 装配开关切换。切换前完成前端契约 fixture 和 fake RouterOS 集成测试；切换后确认旧路径无调用再删除旧 policy 实现。数据库旧表直到 `10.0.0.6` 人工验收通过后仍保留。程序部署严格执行 AGENTS.md 的 NAS 备份和人工验收门禁，验收前不提交程序变更。
