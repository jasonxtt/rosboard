# 策略路由 IP 列表技术设计

## 1. 设计原则

本设计只给现有 policy-routing V2 增加一种 source 内容类型，不建立新的策略路由子系统。

核心复用链路：

```text
Source / SourceVersion
        ↓
BuildDesired
        ↓
RouterOS address-list
        ↓
现有 dst-address-list mangle
        ↓
现有 routing mark / routing table / route
        ↓
WAN
```

域名 source 比 IP source 多一段 DNS materialization；IP source 直接进入 address-list。

## 2. 数据模型：最小增量

### 2.1 Source 增加 Kind

在 `policy_v2_sources` 增加：

```text
kind TEXT NOT NULL DEFAULT 'domain'
```

Go `policyv2.Source` 增加：

```go
Kind string // "domain" | "ip"
```

旧库通过一次 `ALTER TABLE ... ADD COLUMN kind ... DEFAULT 'domain'` 兼容；新建库直接在 CREATE TABLE 中包含该列。

不新建 `policy_v2_ip_sources`、`policy_v2_ip_versions` 或 `policy_v2_ip_rules`。

### 2.2 SourceRule 保持现有存储表

继续使用：

```text
policy_v2_source_rules(version_id, rule_type, domain)
```

为避免无收益的大 schema rename，数据库列 `domain` 本任务不改名：

- domain source：保存规范化 hostname；
- ip source：保存规范化 IP/CIDR 字符串。

`rule_type`：

- domain：`DOMAIN`、`DOMAIN-SUFFIX`；
- ip：`IP-CIDR`、`IP-CIDR6`。

代码中不要把这个历史列名扩散成新的错误抽象；必要时使用极小 helper 读取规则值即可，不做整个 repository 的通用 rule engine 重构。

## 3. 解析

### 3.1 IP 规范化

使用 Go 标准库 `net/netip`：

- 无 `/`：`netip.ParseAddr`；
- 有 `/`：`netip.ParsePrefix(...).Masked()`。

规则：

- `IP-CIDR` 只接受 IPv4；
- `IP-CIDR6` 只接受 IPv6；
- 手动裸地址由实际 family 决定规则类型；
- 规范化文本用于去重和持久化。

### 3.2 Clash YAML

现有域名 parser 的输入安全边界继续复用：source byte limit、UTF-8、单 YAML document、node/scalar limit、顶层 `payload`。

实现上只抽取“共享 YAML payload 安全读取”这种真正重复的部分；不要为了 domain/IP 两种类型建立插件式 parser 框架。

IP source 从 payload 提取：

```text
IP-CIDR,<ipv4-or-prefix>,...
IP-CIDR6,<ipv6-or-prefix>,...
```

其余 Clash 字段忽略。

域名 source 保持当前 DOMAIN / DOMAIN-SUFFIX 解析结果不变。

### 3.3 手动输入

新增小型 `ParseIPLines` / `PrepareIPLines`（具体命名按现有代码风格）。同一个文本框允许混合 IPv4/IPv6，并同时接受：

- 裸 IP；
- 裸 CIDR；
- `IP-CIDR,...`；
- `IP-CIDR6,...`；
- 从 Clash payload 直接复制出来、带可选前导 `-` 的上述单行格式。

规则尾部的策略名、`no-resolve` 等附加字段忽略。解析完成后根据实际地址族自动归类，不要求用户分别创建 IPv4/IPv6 列表，也不扩大为完整 Clash parser。

## 4. API 与 Source 生命周期

继续复用现有 `/api/policy-routing/.../sources` 生命周期和 URL/upload/manual 入口，不新增一套 `/ip-sources` 后端资源树。

Source 请求/响应增加 `kind`：

```json
{"kind":"domain"}
{"kind":"ip"}
```

默认缺失 `kind` 按 `domain` 处理，保证老前端/旧数据兼容。

preview/save/refresh 根据 source kind 选择 domain 或 IP parser；其余 pending version、ETag、Last-Modified、schedule、自动同步、失败重试全部复用。

规则列表接口对 domain 保持现有 `{type, domain}`；IP source 返回 `{type, address}`。若前端 API parser 为了最小修改更适合统一内部 `value`，只能在新代码边界内转换，不能破坏现有 domain JSON contract。

## 5. Desired Builder

### 5.1 list 映射

`listBySource` 继续是唯一 source→address-list 映射：

- shared：domain 和 IP source 都映射到出口 `listName`；
- dedicated：每个 source 使用现有稳定 dedicated list 命名规则；IP source 不需要另一套命名算法。

### 5.2 DNS materialization 条件化

现有 `BuildDesired` 当前会为每个存在的 egress无条件创建 forwarder/DNS transport。该行为需做最小条件调整：

- 该 egress 有至少一个启用、非删除的 **domain source 且存在可应用版本** → 生成现有 DNS forwarder、DNS static、DNS transport；
- 只有 IP source → 不生成任何 DNS 对象，也不要求 DNS upstream/fake alias 成为 blocker；
- domain + IP → DNS 对象保持现状，IP 不参与 DNS。

这一步是本任务最重要的行为修正；不能让 IP-only 策略因为 DNS transport 配置而被错误阻断。

### 5.3 IP address-list materialization

对每条 IP source rule：

IPv4：

```text
menu: ip/firewall/address-list
fields:
  list=<listBySource[source.ID]>
  address=<canonical IPv4/IP prefix>
  disabled=<egress disabled state>
  comment=<managed identity | readable label>
```

IPv6：

```text
menu: ipv6/firewall/address-list
fields:
  list=<listBySource[source.ID]>
  address=<canonical IPv6/IP prefix>
  disabled=<egress disabled state>
  comment=<managed identity | readable label>
```

logical ID 必须包含 egress/source/rule identity，保证：

- rename 只改 readable label；
- 删除 source 只删除自己的静态条目；
- shared list 中其他 source 和 DNS 动态条目不受影响；
- reconcile 仍可精确识别 ownership。

不创建任何“address-list 容器”对象，因为 RouterOS address-list 本身就是条目集合。

### 5.4 业务 mangle 复用

`buildFamily` 当前根据 `uniqueSortedValues(listBySource)` 为每个 list 生成一组业务 mangle。该机制直接复用：

- shared 下 domain/IP 汇入同一个 list → 仍只有一组 mangle；
- dedicated 下每个 source 一个 list → 各一组 mangle；
- routing table、route、connection mark、routing mark 不新增 IP 专用版本。

## 6. IPv4 / IPv6 family 处理

IP source 可以同时包含 IPv4 和 IPv6，且这是正常使用方式。

Desired materialization 按 egress 实际启用的地址族筛选规则：

- egress 启用 IPv4 → 只把 IPv4 条目写入 `/ip/firewall/address-list`；
- egress 启用 IPv6 → 只把 IPv6 条目写入 `/ipv6/firewall/address-list`；
- source 中属于未启用地址族的条目直接忽略，不创建无消费者对象，也不作为 blocker。

因此同一份混合 IP 列表可以被 IPv4-only、IPv6-only 或双栈策略复用，而不要求用户拆成两份。实现只做简单 family filter，不新增 capability 模型。

## 7. Frontend

### 7.1 页面

“IP 列表”在策略路由导航/页面层级上与“域名列表”平级，使用相同或接近的设计语言、表格结构和编辑体验。

实现层仍最大程度复用 `PolicySourcesPage` 已有的列表、modal、URL/upload/manual preview、删除和 API 逻辑；可以通过共享组件/参数化复用，避免复制整套业务代码。UI 上是两个独立平级页面，代码层不因此建立两套 source 框架。

### 7.2 Wizard

域名列表与 IP 列表都可绑定到同一 egress：

- UI 分组显示，避免把两类概念混在一起；
- 底层仍更新 source.egressId，不增加新的 egress association 表；
- 至少选一个 domain 或 IP source 即可继续，不再强制“至少一个域名列表”。

### 7.3 类型

最小新增：

```ts
type PolicySourceKind = 'domain' | 'ip'
```

IP rule 需要地址字段；现有 domain 类型和 contract 不做无关重命名。

## 8. RouterOS 官方依据

- RouterOS Policy Routing：routing table 可通过 firewall mangle 的 routing mark 选择；官方不建议无必要同时混用 mangle 和 routing rules。
- RouterOS Mangle：`mark-routing` 用于 policy routing，RouterOS v7 中 routing mark 对应 routing table。
- RouterOS Advanced Firewall 示例：IPv4 `/ip firewall address-list` 和 IPv6 `/ipv6 firewall address-list` 均原生接受 CIDR。

因此本功能使用 address-list + 现有 mangle 是与当前 rosboard 架构一致、且比新增 `/routing/rule dst-address` 更简单的实现。

## 9. 失败、启停和删除

全部沿用现有 reconcile 语义：

- 保存 pending source version；
- apply 成功后 promote；
- mutation 失败停止并保留 pending；
- 下一次扫描实际 RouterOS 状态后重试；
- source disable/delete 使其静态 address-list entries 从 desired state 消失或 disabled（遵循当前 lifecycle 约定）；
- shared list 里的其他 source 规则不受影响。

不新增 rollback、journal、resume 或独立 IP job 状态机。

## 10. 兼容与迁移

唯一必要 schema 增量：`policy_v2_sources.kind`。

现有 source 自动为 `domain`。不修改既有 source/version/rule 主键，不重写旧数据，不改变已部署 managed logical identity。

## 11. 明确禁止的过度设计

本任务不得因为“以后可能支持更多规则”而引入：

- RuleProvider / RulePlugin / Materializer registry；
- 新的 IP repository 接口层；
- 新的 IP plan/reconcile engine；
- 新的 routing-rule 执行路径；
- 通用 DSL；
- 对现有 domain 字段的大范围 rename；
- 与本需求无关的 frontend 重构。

如果实现明显超过“source kind + parser + desired address-list + 小范围 UI”这一边界，应先停下来重新简化。
