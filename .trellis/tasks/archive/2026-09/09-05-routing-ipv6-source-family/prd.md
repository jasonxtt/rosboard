# IPv6 来源族解析与排除语义

## Goal

修复策略启用 IPv6 时，来源设备地址族发现不完整导致的错误/不安全行为：IPv6 出口基础设施应按显式配置生成，但策略规则的 IPv6 来源匹配必须按来源模式安全地决定。

## Requirements

- 保留 IPv6 出口的路由表、默认路由等基础对象；来源设备没有可用 IPv6 时，不得因为缺少来源匹配地址而生成无来源限制的 IPv6 规则。
- `selected` 来源：
  - 至少一个来源成员或手动 IPv6 前缀有可用地址时，只为已知 IPv6 地址生成匹配；未知 IPv6 的成员不得扩大匹配范围，并产生明确的部分解析警告。
  - 没有任何可用 IPv6 地址时，不生成该规则的 IPv6 激活匹配，并产生明确警告。
- `excluded` 来源：来源成员的 IPv6 地址只要有一个无法解析，就必须对该地址族 fail closed：不生成 IPv6 `!subject-list` 激活规则，并指出未解析的排除成员。不能因另一个成员有 IPv6 就消除警告。
- `all` 来源不依赖终端地址解析；在入口、目标和 IPv6 出口条件有效时，应正常生成 IPv6 激活。
- 手动 IPv6 前缀必须参与对应地址族的来源解析；但排除模式中，手动前缀不能掩盖未知排除成员。
- 只在目标列表实际支持 IPv6 时生成 IPv6 规则；不能因 IPv6 出口存在而对 IPv4-only 目标扩大匹配。
- 使用现有可路由地址语义：忽略带 zone 的地址和 IPv6 link-local 地址；保留 global/ULA IPv6。
- 不修改已通过审核的 proposal/apply 流程、canonical subject/TargetList、入口候选 discovery 或无关前端逻辑。

## Acceptance Criteria

- [x] IPv4-only selected 来源 + IPv6 出口：IPv6 出口基础对象存在，IPv6 来源激活不存在，且有明确的无可用来源 IPv6 警告。
- [x] selected 中一个 IPv4-only 设备和一个双栈设备：IPv6 激活只匹配双栈设备地址，并明确警告未覆盖的设备。
- [x] excluded 中存在 IPv4-only 设备：无论是否还有双栈设备，都不生成 IPv6 排除激活，并明确警告；所有排除成员有 IPv6 时才生成 `!subject-list`。
- [x] all 来源、手动前缀、IPv4-only 目标、link-local/ULA/global 地址的行为符合上述语义。
- [x] 完成后端回归测试和项目规定的 Go/Web/Trellis 检查；同一 `feat/policy-access-rebuild` 分支和 Draft PR #4 提交并推送。
- [x] GitHub 根审核明确返回 `APPROVED` 后，才部署到 `10.0.0.60`；不触碰生产机 `10.0.0.6`。
