# 来源级列表与永久阻断技术设计

## 1. Scope

本子任务实现父设计的第一阶段：来源级 RouterOS 地址列表迁移、设备独立的永久访问阻断，以及完成该工作所需的 API 和前端。时间窗口与每日活跃配额不进入本次数据模型或 RouterOS 规则。

## 2. Existing Ownership

- `internal/policy` 继续拥有 domain/IP 规则解析。
- `internal/policyv2` 继续拥有来源版本、策略路由 desired state、plan/apply 和 pending promotion。
- `internal/routeros` 继续拥有 RouterOS REST read/mutation allow-list。
- 新增 `internal/accesscontrol`，只拥有访问策略、终端地址投影、filter desired state、同步和应用状态。
- device SQLite 保存访问策略、终端固定地址、同步状态和审计；不复制来源规则。
- policy-routing apply 与 access-control sync 共用同一个 per-device write gate。

## 3. Source Lists

唯一命名函数 `policyv2.SourceListName(deviceIdentity, sourceID, sourceName)` 生成 `rb_src_<sanitized-name>_<stable-suffix>`。stable suffix 只依赖设备身份和 source ID；来源改名只更新可读标签，不改变逻辑身份。

domain 来源的 DNS Static `address-list` 和 IP 来源的静态 CIDR 都写入该来源级列表。来源只要被 enabled policy egress 或 enabled access policy 引用就应物化；两者都不引用时应清理其受管对象。

shared 出口改为：每个来源各有一条 `mark-connection dst-address-list=<source-list>`，但同一出口/地址族仍共用原 connection mark；每个出口/地址族仍只有一条 mark-routing。dedicated 旧数据保留现有 mark schema，本阶段不做无关迁移。

## 4. Shared Migration

迁移状态按设备持久化为 `none|dual|cleanup_ready|complete`：

1. 创建来源级静态成员与新 mark-connection，保留旧 shared 兼容规则。
2. 把 DNS Static patch 到来源级列表并 flush RouterOS DNS cache。
3. 旧列表中的动态成员在 TTL 到期前继续由兼容规则路由。
4. 旧静态成员确认复制后可删除；旧动态成员清空后只进入 `cleanup_ready`。
5. 只有远端人工验收后的后续 cleanup apply 才删除兼容规则并标记 `complete`。

每次 apply 都重新扫描实际状态；RouterOS 或 SQLite 写入结果不确定时停止，不跨过迁移阶段。

## 5. Logical Access Rule And Permanent Deny

第一阶段把用户主实体改为逻辑 `AccessRule`，而不是 `(terminal_id, source_id)` pair：

```text
AccessRule
  id / name / target_scope / enabled / revision / timestamps
AccessRuleClient
  rule_id / member_id / terminal_id / binding(auto|fixed) / anchor_mac / fixed addresses
AccessRuleSource
  rule_id / source_id               # 仅 target_scope=sources
```

`target_scope=internet` 时没有 source rows；`target_scope=sources` 至少有一个来源。第一阶段 action 固定为 deny，不预先实现窗口/配额行为，但后续配额必须以 `rule_id` 聚合，因此现在就不能把用户规则继续建模为单 terminal/source pair。

Auto-follow 是 UI 默认：成员从当前 Monitor snapshot 投影地址，MAC 只是底层稳定 anchor；fixed IP 仅作为高级方式。成员短暂不可解析采用 resolved / temporarily_unresolved / conflicted 三态：暂时未知保持最后已成功应用且没有相反事实的投影并将该成员标记 degraded，不阻断其他规则；确认旧地址已属于其他身份时删除错误投影。新建 auto 成员仍要求初次可靠解析。

### Sources scope

每个逻辑规则物化一个"规则成员地址列表"（全部成员当前地址的并集）；对每个来源生成出站/回程双向 jump（成员列表 ↔ 来源列表），子链按规则共享。对象规模是 成员地址数 + 2×来源数，而不是成员×来源的 jump 矩阵。用户仍只看到一条 rule。

### Internet scope

不再用 `TerminalScope.Prefixes` 物化 local-prefix 列表，也不通过 jump/sub-chain 判断“互联网”。每个地址族读取 RouterOS `/ip/route`、`/ipv6/route` 的全部默认路由（统一 `/routing/route` 作为兼容回退），从 `immediate-interface`、`immediate-gw` 或 `gateway` 解析实际出口接口；所有 routing table 的非禁用主备出口都纳入，去重后每个接口生成直接 `forward` 规则：出站用 `src-address-list + out-interface`，回程用 `dst-address-list + in-interface`，TCP reset、UDP/其他协议 drop。TerminalScope 和 `LAN` interface list 只作为排除本地接口的证据；没有可证明出口时阻断计划，绝不生成无接口的全局 drop。

终端地址列表继续按成员当前投影的地址并集物化；同一终端观察到多个 IPv6 地址时全部纳入，但排除 interface-scoped 的 link-local 地址。

所有 managed access filter 均置于 filter 顶部受管块；不生成 `accept`。RouterOS read-back 必须确认 ownership、顺序、matcher 和 action。

## 6. API And UI

第一阶段 API 提供设备级 overview、logical rule CRUD 和 job read。保存 desired state 后自动启动同设备串行 apply；响应区分 desired、applied、degraded 和 blocked。保留内部 reconcile endpoint 仅作为故障恢复能力，不把“同步 RouterOS”作为常驻产品主操作。

前端第一阶段仍只实现 deny action，但创建/编辑流程直接采用最终信息架构：规则名称、多选设备、`整个互联网 | 指定网站/IP`、多选来源、启用状态。默认不展示 MAC/IP selector；fixed IP 放高级设置。主表改为“规则 / 设备 / 访问范围 / 生效条件 / 状态”，身份和地址只在详情显示。

保存按钮只写“保存”；保存成功后自动 apply。常驻“同步 RouterOS”按钮移除，只有 failed/degraded/drift 时出现“重新应用”。界面继续说明普通 DNS 之外的解析、代理或 VPN 可能绕过指定来源规则。

## 7. Failure And Rollback

- 来源没有可用版本、domain 来源无法得到可证明的 DNS upstream、规则无法置顶、foreign `ra_` 冲突或 RouterOS write outcome unknown 时 block。
- 新建 auto-follow 成员无任何可靠身份/地址时拒绝保存；已存在成员暂时不可解析时只将该成员/规则标记 degraded，不再全设备 fail-closed。确认地址身份冲突时移除错误成员投影。
- `internet` 规则若无法从 RouterOS 默认路由得到可验证的独立出口接口时 block，绝不把 LAN 或无接口的所有 forward 流量一起阻断。
- rosboard 离线时已应用的 RouterOS block 继续生效；desired 变更等待恢复后 reconcile。
- 回滚必须同时恢复 NAS 备份中的旧 binary、配置、SQLite 和 service unit；旧 shared 兼容规则在人工验收前保留，降低回滚期间的路由风险。

## 8. Official RouterOS Basis

- DNS Static address-list: RouterOS DNS documentation.
- IPv4/IPv6 filter jump, reject and counters: RouterOS firewall filter documentation.
- mangle connection/routing marks: RouterOS policy routing documentation.
- 实机 RouterOS 7.21.5 `/console/inspect` 已只读确认 IPv4/IPv6 `reject-with=tcp-reset`。
