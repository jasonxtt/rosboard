# 策略路由 IP 列表 PRD

## 1. 背景与目标

现有 rosboard 策略路由 V2 已支持“域名列表 → RouterOS DNS Static FWD → firewall address-list → mangle → routing table → WAN”的域名分流。

本任务增加与“域名列表”平级的“IP 列表”，用于直接访问目标 IP/CIDR 时走指定策略路由。IP 列表不需要 DNS 解析，应直接物化为 RouterOS IPv4/IPv6 firewall address-list 条目，并复用现有 mangle、routing table、route 和 WAN 出口逻辑。

核心目标：在不引入第二套策略路由框架的前提下，用最小修改补齐 IP/CIDR 目标分流。

## 2. 已确认需求

1. 新增独立“IP 列表”业务类型，与现有“域名列表”分开管理。
2. IP 列表支持：
   - 手动输入；
   - 本地上传；
   - 远程 HTTPS URL；
   - Clash YAML 中的 `IP-CIDR` 与 `IP-CIDR6`。
3. 支持单 IP 和 CIDR：
   - IPv4，例如 `8.8.8.8`、`1.1.1.0/24`；
   - IPv6，例如 `2001:4860:4860::8888`、`2606:4700::/32`。
4. 同一个来源不得混合 domain 与 IP 语义；每个 source 明确是 `domain` 或 `ip`。
5. 一个策略可：
   - 只绑定域名列表；
   - 只绑定 IP 列表；
   - 同时绑定域名列表和 IP 列表。
6. shared 模式：同一出口下的域名来源和 IP 来源共用现有出口 address-list 与业务 mangle。
7. dedicated 模式：每个域名来源、每个 IP 来源分别拥有稳定的独立 address-list，但仍共用该出口的 routing table/route 体系。
8. 只有 IP 来源的策略不得创建 DNS forwarder、DNS static、Fake DNS transport、DNS output mangle/DNAT 等 DNS 对象。
9. 域名来源与 IP 来源同时存在时，DNS 相关对象只为域名来源服务；IP 条目直接写入对应 address-list。
10. 同一 IP 列表可同时包含 IPv4/IPv6；应用时按策略实际启用的地址族筛选并分别写入：
    - IPv4 → `/ip/firewall/address-list`；
    - IPv6 → `/ipv6/firewall/address-list`。
    若策略只启用其中一个地址族，列表中另一地址族的条目直接忽略，不作为 blocker。
11. 不使用 `/routing/rule dst-address=...` 建立另一套 IP 分流路径；继续复用现有 mangle `dst-address-list` + routing mark 机制。

## 3. 简化与实现约束

以下是本任务的硬约束：

1. 优先复用当前 V2 的 `Source`、`SourceVersion`、source refresh、pending/active promotion、plan/reconcile、apply job 和前端请求链路。
2. 不新建一套 IP source/version/repository/reconciler 框架。
3. 不为 IP 列表新增 routing table、routing mark 或 route 模型；复用现有出口 family 配置。
4. 不为每条 IP 创建独立 mangle；mangle 继续按 address-list 工作。
5. 不做无关 schema 重命名或大迁移；既有域名数据和 API 行为必须保持兼容。
6. 不进行与本需求无关的重构、命名清理、组件重写或抽象化。
7. 每一处生产代码改动都必须能直接追溯到本任务需求。

## 4. 用户工作流

### 4.1 IP 列表管理

“IP 列表”作为与现有“域名列表”同层级的独立页面，沿用相同的设计语言和交互习惯。用户可在该页面新建、编辑、启停、删除 IP 来源，并选择：

- 手动输入；
- 文件上传；
- HTTPS URL。

预览时展示有效条目、忽略统计和错误样例；保存后沿用现有 pending version 与自动同步策略。前端实现应尽量复用现有 source CRUD/modal/API 逻辑，但 UI 层级上保持“域名列表”和“IP 列表”为两个平级页面。

### 4.2 策略绑定

策略向导中的列表选择同时支持域名列表和 IP 列表，但 UI 保持两类列表语义清晰，不将一个 source 混成两类。

### 4.3 RouterOS 结果

- IP-only shared：静态 IP/CIDR address-list + 现有业务 mangle/route；无 DNS 对象。
- domain + IP shared：DNS 动态结果与静态 IP/CIDR 汇入同一 list，复用同一业务 mangle。
- dedicated：每个 source 使用自己的 list；每个 list 仍走同一出口的 route table。

## 5. 输入规范

### 5.1 手动输入

每行一条，同一个 IP 列表允许混合 IPv4 和 IPv6，并同时支持裸 IP/CIDR 与 Clash 单行格式。至少支持：

```text
IP-CIDR,91.108.0.0/16
IP-CIDR6,2001:67c:4e8::/48
91.108.0.0/16
1.1.1.1
```

同时兼容用户从 Clash YAML payload 中直接复制出来、带可选前导 `-` 的单行规则，以及 Clash 规则尾部的策略名/`no-resolve` 等附加字段；这些附加字段不参与 rosboard 路由判断。解析后由 rosboard 按实际地址族自动拆分 IPv4/IPv6，不要求用户分别维护两份列表。

### 5.2 Clash YAML

读取顶层 `payload`，接受：

```yaml
payload:
  - IP-CIDR,1.1.1.0/24,no-resolve
  - IP-CIDR6,2606:4700::/32,no-resolve
```

后续策略字段如 `DIRECT`、`PROXY`、`no-resolve` 不参与 rosboard 路由决策，仅提取地址/CIDR。

域名 source 仍只接受其现有 DOMAIN / DOMAIN-SUFFIX 语义；IP source 只提取 IP-CIDR / IP-CIDR6。

## 6. 规范化要求

1. 使用标准 IP/prefix 解析，不自行实现地址语法。
2. 单 IP 规范化为地址字符串；CIDR 规范化为 masked prefix 字符串。
3. `IP-CIDR` 必须是 IPv4；`IP-CIDR6` 必须是 IPv6；地址族不一致作为无效规则处理。
4. 去重基于规范化后的地址/CIDR文本。
5. 沿用现有单 source 大小和有效规则数量限制，除非实测发现 RouterOS address-list 需要更严格限制。

## 7. 兼容性

1. 现有数据库中的所有 source 默认视为 `domain`。
2. 现有域名 API、前端页面和已保存数据不需要用户迁移操作。
3. 现有 address-list 名称、routing table、mangle identity 与 ownership 边界保持不变。
4. 升级后首次启动只做必要的兼容 schema 增量，不重建 policy 表。

## 8. 非目标

- 不支持 GeoIP、ASN、国家/地区数据库。
- 不支持 IP range 自定义语法（如 `1.1.1.1-1.1.1.9`）；如需要应先转换为 CIDR。
- 不引入 `/routing/rule` 作为 IP 分流执行路径。
- 不自动把域名解析结果写成持久 IP source。
- 不把 domain 和 IP 混在同一 source。
- 不改变现有 WAN、NAT、gateway discovery 或 traffic ingress 的职责。

## 9. 验收标准

- [ ] 现有域名 source 升级后行为完全不变，旧数据自动按 `domain` 读取。
- [ ] 可创建手动 IPv4/IPv6 单 IP/CIDR 列表并保存、预览、分页查看。
- [ ] 可从上传和 HTTPS Clash YAML 提取 `IP-CIDR` / `IP-CIDR6`。
- [ ] 无效 IP、错误地址族、重复规则被正确归类，不污染有效规则。
- [ ] IP-only shared 策略只生成 address-list + 现有路由链路，不生成任何 DNS 对象。
- [ ] domain + IP shared 使用同一策略 address-list 和同一业务 mangle。
- [ ] dedicated 模式下 domain/IP source 各有独立稳定 address-list，仍复用同一出口 routing table。
- [ ] 同一 IP 列表可混合 IPv4/IPv6，并可混合裸 IP/CIDR 与 Clash 单行 IP-CIDR/IP-CIDR6 格式；解析后自动按地址族拆分。
- [ ] IPv4 条目只进入 `/ip/firewall/address-list`，IPv6 条目只进入 `/ipv6/firewall/address-list`；策略未启用的地址族条目被忽略且不阻断应用。
- [ ] 启停、删除、URL refresh、pending→active promotion、失败重试沿用现有 source 生命周期并正确工作。
- [ ] 外部 RouterOS 对象不被修改；现有精确 ownership/reconcile 边界保持不变。
- [ ] targeted Go tests、`go test ./...`、`go vet ./...`、前端 typecheck/build/lint（项目已有命令）和 `git diff --check` 通过。
- [ ] 按项目 deployment acceptance gate 完成本地验证、NAS 备份、部署验证和用户人工验收后，才允许提交程序改动。
