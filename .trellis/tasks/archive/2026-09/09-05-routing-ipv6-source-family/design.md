# 技术设计

## 根因

`buildRoutingMangleFamily` 目前对 `selected` 和 `excluded` 都调用同一个 `buildRoutingSubjectList`。该函数只把所有成员和前缀合并成一个地址集合：集合非空就允许整条地址族继续生成规则。因此：

- 单个 IPv4-only 来源没有 IPv6 地址时，IPv6 出口基础对象虽已先生成，但规则激活被跳过，提示使用了过于笼统的“来源没有地址”语义。
- IPv4-only 成员与双栈成员混选时，只要集合中有一个 IPv6 地址，警告消失，无法说明前者没有被 IPv6 匹配覆盖。
- `excluded` 模式下同样的聚合会把部分已知集合当成完整排除集合，生成可能扩大流量范围的 `!subject-list`。

## 设计边界

先解析一个地址族的来源，并保留成员级别的已解析/未解析结果；再由来源模式决定是否可以物化 RouterOS 地址列表和激活规则。

```text
显式 egress family
  ├─ buildRoutingFamilyFoundation  -> IPv6 table/default route 等基础设施
  └─ target + ingress + subject family resolution
       ├─ selected: known addresses 可激活；unresolved 只造成 under-match 警告
       ├─ excluded: 任一 unresolved 则 fail closed，不激活
       └─ all: 不读取终端地址
```

解析器继续使用 `accesscontrol.EvaluateMembers` 的现有固定地址、auto-follow、last trusted 和冲突处理语义。终端显示名由当前 terminal snapshot 按 ID 映射；找不到显示名时使用终端 ID，避免只给用户 UUID 且不改变底层身份逻辑。

手动前缀始终加入该地址族的地址集合。`selected` 的部分解析仍可使用这些已知地址；`excluded` 仍要求所有成员族解析完成，因为前缀不能证明未知成员不在被排除集合中。

## 警告与兼容性

- 无地址的 selected 使用来源族未解析警告。
- 有地址但部分 selected 成员未解析使用部分解析警告，并列出成员。
- excluded 任一成员未解析使用排除族未解析警告，并列出成员。

已有的成员总体状态警告保留用于身份/在线/冲突等信息；新增的族级警告不改变 DesiredResult 的 blocker 语义，也不修改 canonical 数据。

## 测试策略

在现有 `internal/policyv2/desired_test.go` 中通过 `buildRoutingMangleFamily` 检查地址列表和 mangle 激活，通过 `buildRoutingFamilyFoundation` 检查 IPv6 出口基础对象。覆盖 selected none/partial, excluded none/partial/complete, all, prefixes, IPv4-only target 和 link-local/ULA/global 地址。

本轮不修改 RouterOS capability 探测；若现有能力检查已 fail closed，则保留其行为并用现有测试验证，不把独立的 RouterOS 能力问题混入来源族解析修复。
