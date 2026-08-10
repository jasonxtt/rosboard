# 协议分析总开关：设计

## 1. 设计边界与成功标准

本设计只覆盖 PRD 中的协议分析总开关，不改变连接抓取、采集周期、`connectionProtocolCounts`、连接表协议字段、已有 `protocol_samples` 数据或 MosDNS/特征库各自的独立配置语义。

实现完成后应满足：

- 新安装或缺少配置文件时默认关闭；已有配置文件没有 `protocol_analysis` 段时迁移为开启。
- 关闭时不构造识别同步器、特征库同步器或应用解析器，不执行应用分类、协议聚合和协议样本写入。
- 关闭时连接列表、连接字节/速率、终端连接数、家庭统计、原始协议字段和概览 TCP/UDP/其他计数仍正确。
- API、设置页、导航和终端详情页都以同一个总开关为准。

## 2. 已读取的规范

按 `AGENTS.md` 的要求，已完整读取并采用以下相关规范：

- `.trellis/spec/backend/runtime-configuration.md`
- `.trellis/spec/backend/database-guidelines.md`
- `.trellis/spec/backend/monitoring-contracts.md`
- `.trellis/spec/frontend/component-guidelines.md`
- `.trellis/spec/frontend/quality-guidelines.md`

相关约束的落点是：配置写入继续通过现有的串行 `saveSettings`；SQLite 只停止新增协议样本、不删除历史；RouterOS 原始连接数据继续用于终端速率和连接统计；前端继续在响应边界把可选集合规范化为空数组；移动端控件保持现有 44px 触控尺寸。前端实现后执行确定性的 TypeScript/Vite 构建和 oxlint，浏览器人工验收留到实现完成后进行。

## 3. 默认值、迁移与 probe 方案

### 结论：需调整 probe 的作用域，并改用顶层键存在性判断

PRD 给出的“二次解析原始 YAML”方向可行，不需要把 `Config.ProtocolAnalysis` 改成指针。问题在于 `config.Load` 当前的 `payload` 是在 `if path != ""` 块内由 114 行声明的，作用域只覆盖同一块内的 115-123 行；如果把第二次解析放到 125 行迁移调用附近，无法编译。

实现时采用以下最小调整：

1. 在读文件分支外声明 `fileExisted` 与 `sectionPresent`。
2. 文件读取成功且第一次 `yaml.Unmarshal(payload, &cfg)` 成功后，仍在同一个 `else` 分支内解析顶层 YAML 节点，并判断 `protocol_analysis` 键是否存在。
3. 只有成功解析的文件才将 `fileExisted` 设为 `true`；文件不存在保持 `false`。
4. 将 probe 得到的 `sectionPresent` 带到文件读取块之外，并在 `migrateRecognitionDefaults()` 紧后调用 `migrateProtocolAnalysisDefault(fileExisted, sectionPresent)`。

这样可以区分：缺少文件（新安装，默认关闭）、已有文件但没有该段（迁移开启）、已有文件显式写 `{enabled: false}`（保持关闭）以及已经写入迁移标记（不再迁移）。顶层 `yaml.Node`/等价 map 的键判断也会把 `protocol_analysis: null` 视为“段存在”，不会误迁移。现有 `cmd/rosboard/main.go` 对 `MigrationPending` 的落盘逻辑无需修改。

## 4. 后端设计

### 配置

在 `Config` 中加入值类型 `ProtocolAnalysisConfig` 与 `ProtocolAnalysisMigrated`。默认配置中的 `Enabled` 为 `false`。迁移方法只在“文件成功解析、段不存在、迁移标记为 false”时把开关置为 `true` 并设置 `MigrationPending`；显式段和已经迁移的配置均保持原值。

### Manager 与 Monitor 生命周期

`MonitorManager` 保存 `protocolAnalysis`。当它为 `false` 时，`NewMonitorManager` 跳过三个构造点：`MosDNSSynchronizer`、`FeatureLibrarySynchronizer` 和 `ApplicationResolver`；因此 `Start` 也不会启动识别相关 goroutine。`RecognitionStatus` 和 `MosDNSStatus` 在总开关关闭时直接返回零值，避免当前代码在对象为 nil 时又从子配置拼出“已启用”状态。`ApplicationResolver.Resolve` 已有 nil receiver guard；不增加额外的 resolver nil 判断或调用结构改动。

`NewMonitor` 从传入的 `config.Config.ProtocolAnalysis.Enabled` 初始化 monitor 的显式开关状态。manager 为每个设备复制全局配置后再构造 monitor，因此不需要把开关重复放到设备 YAML 中。该字段在构造函数返回前就已确定，manager 只有在 monitor 完整初始化后才启动 refresh goroutine，不存在带着错误开关值跑过一轮的窗口，也不需要 `SetProtocolAnalysisEnabled`。

关闭时：

- `terminalConnectionRow` 仍填充 RouterOS 原始协议、地址、端口、字节、速率、连接状态和路由归属；总开关关闭时跳过 `classifyApplication` 与 resolver 查询，`Application`、`ApplicationSource` 为空，`Estimated` 为 false。现有 `ApplicationResolver.Resolve` 的 nil receiver guard 保持不变。
- `buildTerminals` 仍构建连接 map、终端速率和家庭统计，但不更新 `flowMap`，并给 `FlowCategories`/`FamilyFlows` 写入空切片，避免空应用名被聚成一个无意义的桶。
- `refreshTerminals`、`refreshTerminalRates` 和完整 `refresh` 只在开关开启时调用 `aggregateProtocols` 与 `SaveProtocolSamples`；关闭时 snapshot 的 `Protocols` 是空切片。
- `FirewallConnectionsV4/V6`、`connectionProtocolCounts`、连接数量、终端速率、`FamilyStats`、`FamilySummaries` 和连接明细路径保持不变。
- `ProtocolHistory` 在关闭时返回空切片；API 会在进入 monitor 查找之前短路，因此不会查询 SQLite。不会清空或删除 `protocol_samples`。

### API

增加 settings response 的 `protocolAnalysis` 字段。`GET /api/protocols` 的关闭分支必须放在当前 `monitorFor` 调用之前，直接返回非 nil 的空 `protocols`、空 `history` 和 `enabled: false`；开启分支保留历史查询并增加 `enabled: true`。这样即使没有可用 RouterOS monitor，也不会因协议功能关闭而触发无意义的 monitor/DB 路径。

`GET /api/recognition` 的 manager nil 分支也要先检查总开关；否则当前代码会直接根据 MosDNS/特征库配置返回非零状态。settings projection 始终原样返回配置文件中存储的 MosDNS/特征库子开关和可编辑字段；总开关关闭时只由前端 fieldset 的 `disabled` 状态表达“当前不生效”，不把存储值投影成 false。`/api/recognition` 仍按 PRD 在总开关关闭时返回零值。

`recognitionSettingsRequest` 增加 `protocolAnalysis`。为兼容旧版前端/调用方，可将该请求字段按“可选输入”处理：字段存在时使用其值，缺失时沿用当前配置；最终为 false 时在校验前把两个子开关强制为 false，再按现有逻辑保存。这样关闭总开关不会因无关的子配置地址缺失而被拒绝，且保存后不会留下子开关为 true 的不一致状态。`saveSettings` 同步设置 `ProtocolAnalysisMigrated = true`。

## 5. 前端设计

`SettingsResponse` 增加可选的 `protocolAnalysis`，协议接口响应类型增加可选 `enabled`。`RecognitionDraft` 与 `recognitionDraftFromSettings` 同步增加总开关。

页面层使用 `settings?.protocolAnalysis?.enabled !== false` 作为加载期间的兼容默认值；设置加载完成且发现关闭时，若当前 view 是 `protocols`，立即重定向到 `policies`。静态 `landingViews` 仍需接受旧 localStorage 中的 `protocols` 值，不能在模块初始化时按尚未加载的后端设置删掉它；通过设置加载后的重定向消化旧值。

识别设置页顶部增加“启用协议分析”复选框；关闭时两个子 fieldset 使用原生 `disabled` 并通过 `web/src/index.css` 做视觉弱化，仍保留字段值。视图切换器只在开启时显示“协议统计”，关闭时保留“策略统计”。侧栏保留“流量监控”父项；其点击目标在开启时为协议统计，关闭时直接落到策略统计。协议页面渲染点增加开关保护。

终端详情页接收总开关状态：关闭时不渲染“流量分布” tab 和面板；若开关在详情页打开期间被关闭，active tab 回退到基础信息。连接表继续显示原始 `Protocol` 和速率。现有 `payload.protocols ??= []`、`detail.flowCategories ??= []`、`familyFlows` 规范化继续保留。

## 6. 测试设计

- `config_test.go`：表驱动覆盖缺少文件默认 false、旧文件无段迁移 true + pending、显式 false 不被覆盖、迁移标记 true 不再触发；同时保留现有配置保存/重载覆盖。
- `manager_test.go`：总开关关闭时三个识别对象均不构造，两个 status 方法返回零值。
- `monitor_test.go`：使用现有临时 SQLite 与终端构造路径增加开关 true/false 对照，验证关闭时应用字段/流量分类/协议聚合为空，而连接数、上下行速率、原始协议和 TCP/UDP/其他计数不变。保存样本测试使用临时 SQLite 查询前后样本数量；当前 `Monitor.store` 是具体的 `*store.Store`，不能在不引入大范围存储接口重构的情况下注入 fake store。
- `server_test.go`：覆盖关闭时 `/api/protocols` 的空数组和 `enabled:false`、settings 中的 `protocolAnalysis`、关闭总开关时保存请求会把 MosDNS 子开关压为 false；开启分支保留既有 recognition 测试。
- `web/package.json` 已确认存在 `npm run lint`（脚本为 `oxlint`）；前端实现后运行 `cd web && npm run build`（包含 `tsc -b`）和 `npm run lint`。

## 7. 计划修改的文件

| 文件 | 计划改动 |
| --- | --- |
| `internal/config/config.go` | 新配置类型、字段、默认值、作用域调整后的 probe 和迁移方法。 |
| `internal/config/config_test.go` | 四类默认值/迁移回归测试。 |
| `internal/service/manager.go` | 总开关状态、识别对象构造门控、零值状态门控。 |
| `internal/service/manager_test.go` | 验证关闭时识别对象不构造、状态归零。 |
| `internal/service/monitor.go` | 分析开关传递、分类/解析/聚合/样本写入门控；保留原始连接统计。 |
| `internal/service/monitor_test.go` | 关闭路径核心数据保留和样本不新增测试。 |
| `internal/api/server.go` | 协议 API 短路与 enabled 字段、settings projection、recognition 请求和迁移标记。 |
| `internal/api/server_test.go` | 新增协议 API、settings 和子开关压制测试，并调整既有请求 fixture。 |
| `web/src/lib/types.ts` | settings 与协议响应类型；`protocolAnalysis` 声明为可选，迫使调用方使用双层可选链。 |
| `web/src/App.tsx` | 总开关表单、动态导航/重定向、协议页和终端流量分类门控。 |
| `web/src/index.css` | 关闭子 fieldset 的视觉弱化；保持既有响应式与触控尺寸。 |
| `configs/config.example.yaml` | 增加 `protocol_analysis.enabled: false` 示例。 |
| `.trellis/spec/backend/runtime-configuration.md` | 增加字段、迁移、settings 响应和“连接抓取不受开关影响”的英文运行时契约。 |

前端构建成功后还会更新 `internal/ui/dist/index.html` 及 hash 命名的资源文件；这些是构建产物，不手工编辑，具体资源文件名和数量以构建结果为准。

## 8. 与 PRD/实际代码的冲突与调整

1. **`payload` 作用域不足。** PRD 的 probe 代码若按 125 行之后放置无法访问 `payload`；已在本设计的第 3 节明确调整为在成功 unmarshal 的同一分支内完成顶层键存在性判断。
2. **manager 的 nil 状态判断并不能覆盖当前行为。** PRD 认为已有 nil 检查应足够，但实际 `MosDNSStatus` 会在 synchronizer 为 nil 时从 `mosdnsConfig.Configured()` 生成非零状态，`RecognitionStatus` 也会从 feature config 生成状态。因此需要显式的总开关早返回；`/api/recognition` 的 manager nil 分支也必须补同样的判断。
3. **resolver nil 保护的判断已更正。** 实际 `ApplicationResolver.Resolve` 在 nil receiver 上会直接返回空结果；不新增 `resolver != nil` 判断，只由总开关跳过整个分类/解析段。
4. **侧栏行号对应的 UI 结构不同。** PRD 所说的“协议统计按钮”在当前 1175 行实际是唯一的“流量监控”父项，协议/策略两个按钮只存在于顶部 `monitorTabs`（1052-1057）。不会凭空增加另一层导航；保留父项，关闭时令其进入策略统计，并隐藏顶部的协议 tab。
5. **关闭 API 的短路位置。** 当前 `/api/protocols` case 位于统一 `monitorFor` 之后；若只修改 case，仍可能先访问不可用 monitor。设计要求把关闭判断前移，才能满足 PRD 的“不查历史 DB/直接返回”。
6. **fake store 测试不可直接套用。** 当前 monitor 依赖具体 `*store.Store`，不是可替换接口。为避免超出任务范围的存储层重构，测试用临时 SQLite 验证 `protocol_samples` 行数不增加；行为断言不变。

除上述语义/结构差异外，PRD 提到的 config 114-123、迁移调用 125、manager 101/112/123/195、monitor 408/409/477/576/581-584/590-612/1153-1154、API 321-327/589/1021、前端 206/339/804/1056/1356/2189/3238 等关键行号在当前工作树基本对应，没有发现需要静默改写依赖关系的行号漂移。
