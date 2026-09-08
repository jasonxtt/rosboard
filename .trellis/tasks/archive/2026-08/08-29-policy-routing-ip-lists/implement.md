# 策略路由 IP 列表实施计划

> 当前状态：planning。未经用户 review 通过，不执行 `task.py start`，不修改生产代码。

## 0. 实施纪律

- [ ] 每一步都优先复用现有 V2 路径；不新建平行框架。
- [ ] 不清理、不覆盖当前 main worktree 中与本任务无关的既有 dirty 修改。
- [ ] 不顺手重构 domain source、policy reconciler 或前端页面。
- [ ] 任何新增抽象都必须有至少两个当前真实调用点，否则优先不用。
- [ ] 每个阶段完成后先看 diff；如果实现明显比需求复杂，先简化再继续。

## 1. Schema + model 最小扩展

目标：让现有 Source 能区分 `domain` / `ip`。

- [ ] `policy_v2_sources` CREATE TABLE 增加 `kind TEXT NOT NULL DEFAULT 'domain'`。
- [ ] 对旧库做幂等 `ALTER TABLE ... ADD COLUMN kind ... DEFAULT 'domain'`。
- [ ] `policyv2.Source` 增加 Kind；repository 的 SELECT/INSERT/UPDATE/scan 透传。
- [ ] SaveSource 校验 kind 仅允许 domain/ip；空值按 domain 兼容。
- [ ] 不修改 `policy_v2_source_rules` 表结构。

验证：

```bash
go test ./internal/store ./internal/policyv2
```

重点回归：旧 schema 升级后所有旧 source 都是 domain；原域名 CRUD 行为不变。

## 2. IP parser / source content

目标：复用现有 source 安全边界，增加最小 IP 解析能力。

- [ ] 使用 `net/netip` 实现 IP/CIDR normalize。
- [ ] 新增 IP 手动行解析：同一输入中可混合裸 IPv4/IPv6 地址、CIDR、Clash 单行 IP-CIDR/IP-CIDR6，并兼容可选前导 `-`；忽略规则尾部策略名/`no-resolve` 等附加字段。
- [ ] Clash YAML IP preview 支持顶层 payload 的 IP-CIDR/IP-CIDR6。
- [ ] 复用现有 byte/UTF-8/YAML node/scalar 限制；只抽取确实共享的 payload 读取代码。
- [ ] domain parser 的结果和 ignored 统计保持兼容。
- [ ] IP pending version 继续写现有 SourceVersion / SourceRule；rule type 使用 IP-CIDR/IP-CIDR6，rule value 保存规范化地址/CIDR。

验证：

```bash
go test ./internal/policy -run 'IP|Clash|Source|DomainLines|Upload'
```

必须覆盖：同一手动列表混合 IPv4/IPv6、裸 IP/CIDR 与 Clash 单行混输、可选前导 `-`、CIDR canonicalization、Clash rule family mismatch、duplicate、无效输入、mixed Clash payload 中只提取目标 kind。

## 3. API source kind 复用

目标：不新增平行 API 资源树。

- [ ] Source 请求/响应增加 kind，缺失按 domain。
- [ ] URL/upload/manual preview/save/refresh 根据 kind 调用对应 parser。
- [ ] IP rules API 返回 address；domain rules contract 保持 domain。
- [ ] 定时 URL refresh 根据保存的 source kind 选择 parser。
- [ ] 现有 pending/active promotion、ETag/Last-Modified、自动同步路径不分叉复制。

验证：

```bash
go test ./internal/api -run 'PolicyV2.*Source|Policy.*Source'
```

新增 API contract tests：domain 老请求不带 kind 仍成功；IP manual/upload/url preview/save/rules/refresh 正确。

## 4. Desired builder / RouterOS address-list

目标：IP 只增加 address-list 物化，路由链路全部复用。

- [ ] `enabledSourcesByEgress` 等现有 source 聚合保持通用，只在需要 DNS 时筛 domain。
- [ ] 只有 egress 存在可应用 domain source 时才生成 forwarder / DNS static / DNS transport。
- [ ] IP-only egress 不校验/阻断 DNS alias/upstream。
- [ ] IP source 规则按 family 生成受管 `/ip firewall address-list` 或 `/ipv6 firewall address-list` 条目。
- [ ] shared：domain/IP source 共用同一 `listBySource` 值，业务 mangle 保持一组。
- [ ] dedicated：复用现有 dedicatedListName(source)，每 source 一个 list；不新增 IP 专用命名函数。
- [ ] Desired 按 egress 已启用地址族过滤 IP 规则；未启用地址族的条目直接忽略，不生成对象、不产生 blocker。
- [ ] logical ID / comment 保持精确 ownership；source rename 只影响 readable label/必要的 dedicated list replacement，不影响其他 source。
- [ ] 不创建 `/routing rule`。

验证：

```bash
go test ./internal/policyv2 -run 'Desired|IP|Source|Shared|Dedicated|DNS'
go test ./internal/routeros
```

必须覆盖四个核心 desired 场景：

1. domain-only：输出与改动前一致；
2. ip-only shared：无 DNS 对象，有 address-list + 现有 mangle/route；混合 IPv4/IPv6 列表只物化 egress 已启用的 family；
3. domain+ip shared：同 list、同 mangle；
4. domain+ip dedicated：各 source 独立 list、同出口 route table。

## 5. Frontend 最小扩展

目标：UI 上新增与“域名列表”平级的“IP 列表”页面，但代码层复用现有 source CRUD，不复制业务框架。

- [ ] types 增加 `PolicySourceKind = domain | ip` 及 IP rule shape。
- [ ] api parser/serializer 透传 source kind。
- [ ] 在策略路由页面层级新增独立“IP 列表”入口/页面，与“域名列表”平级，并采用相同或接近的设计语言。
- [ ] 最大程度复用现有 `PolicySourcesPage` 的表格、modal、URL/upload/manual preview、CRUD/API 逻辑；允许抽取很小的共享组件或通过 kind 参数复用，但不复制整套 source 业务代码。
- [ ] IP manual 文案与 preview 展示 address/CIDR，并明确同一列表可混合 IPv4/IPv6 与裸格式/Clash 单行格式。
- [ ] Wizard 分组显示 domain/IP source；校验改为至少选择任一列表。
- [ ] Egress 列表/详情文案从只写“域名列表”调整为能表达两类来源，不进行无关视觉重构。

验证：

```bash
cd web
npm run typecheck
npm run build
```

若项目存在 lint/test 脚本，按 package.json 当前定义运行，不新造脚本。

## 6. Lifecycle / reconcile 回归

- [ ] IP source enable/disable 只影响自身受管 address-list 条目和对应现有策略激活结果。
- [ ] shared 删除一个 IP source 不删除其他 domain/IP source 的条目或业务 mangle。
- [ ] dedicated 删除 source 清理自己的 list entries/mangle，不动同出口其他 source。
- [ ] URL scheduled refresh 的 IP pending version 成功 apply 后正确 promote；失败保留 pending。
- [ ] egress 删除/停用继续遵守现有精确 ownership。
- [ ] IP-only egress 从无 domain → 新增 domain 后，下一次 reconcile 能补建 DNS objects；反向删除最后 domain 后能清理 DNS objects，但保留 IP 路由链路。

## 7. 全量质量门禁

```bash
gofmt -w <仅本任务修改的 Go 文件>
go test ./internal/policy ./internal/policyv2 ./internal/store ./internal/api ./internal/routeros
go test -race ./internal/policyv2 ./internal/store ./internal/api
go test ./...
go vet ./...
git diff --check
```

前端按当前 package scripts 运行 typecheck/build/lint/test。

检查：

- [ ] `git diff --stat` 与 `git diff` 无无关重构。
- [ ] 搜索确认没有新增 `/routing/rule` IP 路径。
- [ ] 搜索确认没有 IP-only DNS 对象。
- [ ] Trellis validate PASS。

## 8. 部署 acceptance gate

程序改动在 commit 前必须遵守 `rosboard/AGENTS.md`：

1. 本地自动化验证通过。
2. 确认 NAS `/Users/tom/nas/wyp/github/rosboard/backups/` 可写。
3. 在 NAS 创建时间戳备份，保存当前远端 binary、config、SQLite、systemd unit，并遵守最多 10 份保留规则。
4. 部署到 `10.0.0.6` 前仅使用项目允许的 SSH 别名/流程。
5. 验证 systemd、health、策略 API、前端 assets。
6. 用户人工检查并明确批准。
7. 只有人工批准后才允许 commit / Trellis finish/archive。

## 9. 实施停止条件

出现以下任一情况时，停止扩大实现并回到设计：

- 为 IP source 需要复制整套 repository/API/reconciler；
- 为支持两种 source 引入 registry/plugin/DSL；
- 需要改变现有 routing table / routing mark 体系；
- 需要大范围 rename `domain` 字段才能继续；
- 本任务 diff 混入当前 worktree 的其他功能。

这些都意味着方案偏离“最小增量”，应先重新简化。
