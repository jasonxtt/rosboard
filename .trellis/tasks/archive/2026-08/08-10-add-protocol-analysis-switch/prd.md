# 协议分析总开关

## 背景

面板当前无条件对每条防火墙连接做应用分类：先用端口猜测（`classifyApplication`），
再叠加 MosDNS + 特征库的域名反查（`ApplicationResolver`），聚合成协议统计并**每次轮询
写一次 sqlite**（`SaveProtocolSamples`）。

一部分用户不需要协议维度的数据，这些工作对他们是纯开销。需要一个总开关，关掉后彻底
不做分析。

## 关键约束（实现前必读）

**连接抓取不能砍。** `FirewallConnectionsV4/V6` 拿到的连接列表同时支撑：

- 每终端实时速率 `CurrentUploadBps` / `CurrentDownloadBps`（`monitor.go:564-568` 按连接字节累加）
- `Terminal.ConnectionCount`、`FamilyStats`、`FamilySummaries`
- `Overview.ConnectionCount`、`ConnectionProtocolCounts`
- 连接明细表（远端地址、端口、字节数）

即使关闭协议分析，这些调用和计算**必须原样保留**。开关只作用于分析层。

**保留的字段（不是"分析"，是 RouterOS 原样给的事实）：**

- `TerminalConnection.Protocol`（tcp/udp/icmp 小写透传）— 连接表的协议筛选器依赖它
- `Overview.ConnectionProtocolCounts`（TCP/UDP/其他计数）— 概览"活动连接"卡片的构成条

## 配置

新增 section，与 `mosdns` / `feature_library` 平级：

```yaml
protocol_analysis:
  enabled: false
```

Go 侧：

```go
type ProtocolAnalysisConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
}
```

挂在 `Config` 上：`ProtocolAnalysis ProtocolAnalysisConfig` + `yaml:"protocol_analysis,omitempty"`

### 默认值与迁移（务必按此实现）

要求：**全新安装默认关；已有安装升级后默认开**（不改变老用户既有行为）。

复用现有 `RecognitionDefaultsMigrated` 的模式，新增顶层标记：

```go
ProtocolAnalysisMigrated bool `yaml:"protocol_analysis_migrated,omitempty"`
```

`Load(path)` 中的逻辑：

1. 结构体默认值里 `ProtocolAnalysis.Enabled = false`（= 新装默认关）
2. `Load` 需要知道**配置文件是否已存在并被成功解析**。当前 `Load` 在 114-123 行读文件，
   读到了就 unmarshal。用一个局部 `fileExisted bool` 记录这个事实。
3. 新增 `cfg.migrateProtocolAnalysisDefault(...)`，紧跟在
   `cfg.migrateRecognitionDefaults()`（125 行）之后调用：

```go
func (c *Config) migrateProtocolAnalysisDefault(fileExisted, sectionPresent bool) {
    if c.ProtocolAnalysisMigrated {
        return
    }
    if fileExisted && !sectionPresent {
        // 老配置文件里没有 protocol_analysis 段 → 保持既有行为，开启
        c.ProtocolAnalysis.Enabled = true
        c.MigrationPending = true
    }
    c.ProtocolAnalysisMigrated = true
}
```

`MigrationPending` 会让 `cmd/rosboard/main.go` 把迁移后的配置落盘（该逻辑已存在，无需改）。

`saveSettings`（`internal/api/server.go:1021` 附近）已经设 `next.RecognitionDefaultsMigrated = true`，
同处补 `next.ProtocolAnalysisMigrated = true`。

**必须避免的坑**：用户显式写了 `protocol_analysis: {enabled: false}` 的老配置，
首次加载时 `ProtocolAnalysisMigrated` 仍是 false，若迁移只看 `fileExisted` 就会把它
强行改成 true —— 用户的显式选择被覆盖。所以迁移必须只在**配置里完全没有
`protocol_analysis` 段**时才生效，即上面的 `sectionPresent` 参数。

`sectionPresent` 的取法：**不要**把 `ProtocolAnalysis` 改成指针类型（会把 nil 检查
扩散到所有下游读取点）。改用一个只关心该键的 probe struct 先解析一遍原始 YAML：

```go
var probe struct {
    ProtocolAnalysis *ProtocolAnalysisConfig `yaml:"protocol_analysis"`
}
_ = yaml.Unmarshal(payload, &probe)
sectionPresent := probe.ProtocolAnalysis != nil
```

请在 `design.md` 里确认这个方案后再动手实现。

## 后端改动点

### `internal/service/manager.go`

- `NewMonitorManager`：`cfg.ProtocolAnalysis.Enabled` 为 false 时，
  **不构造** `MosDNSSynchronizer`（101 行）、`FeatureLibrarySynchronizer`（112 行）、
  `ApplicationResolver`（123 行）。三者保持 nil。
- 把开关状态存到 manager 上（如 `protocolAnalysis bool`），供 monitor 与 status 查询。
- `RecognitionStatus()`（195 行）：关闭时返回零值状态（现有 nil 检查应已能覆盖，需确认）。

### `internal/service/monitor.go`

Monitor 需要知道开关状态。加一个 `SetProtocolAnalysisEnabled(bool)`
（与现有 `SetApplicationResolver`(77 行) 同风格，manager.go:138 附近一并调用），
或在构造时传入。

关闭时的行为：

1. **`terminalConnectionRow`（590-612 行）**
   - 跳过 `classifyApplication` 调用
   - `Application` 置为空字符串 `""`，`ApplicationSource` 置为 `""`
   - `Estimated` 相应置 false
   - `Protocol` 字段照常填（见上文"保留的字段"）
   - resolver 为 nil 时已有保护，确认无空指针风险

2. **协议聚合与落库（408-409、477、1153-1154 行）**
   - 跳过 `aggregateProtocols(details)`，`snapshot.Protocols` 置空切片
   - **跳过 `m.store.SaveProtocolSamples(...)`**（这是主要的资源节省点）

3. **流量分类（576、581-584、1903、1957 行）**
   - 跳过 `terminalFlowCategories(...)`
   - `detail.FlowCategories` 与 `detail.FamilyFlows` 置为空
   - 理由：该函数按 `connection.Application` 分组，关闭后全部落进同一个空名字桶，无意义

4. `connectionProtocolCounts`（422、479、1148 行）**保持不动**

### `internal/api/server.go`

- `GET /api/protocols`（321-327 行）：关闭时直接返回
  `{"protocols": [], "history": [], "enabled": false}`，不查 `ProtocolHistory`
  （避免无谓的 DB 查询）。开启时在现有响应里补 `"enabled": true`。
- `GET /api/settings`：响应里加 `protocolAnalysis: { enabled: bool }`
- `POST /api/settings/recognition`：`recognitionSettingsRequest`（589 行）加
  `protocolAnalysis` 字段并持久化。
  **校验**：`protocolAnalysis.enabled == false` 时，强制把 `mosdns.enabled` 与
  `featureLibrary.enabled` 一并写 false（总开关压掉子开关，避免"总开关关了但子开关还
  记着 true"的不一致状态）。
- `GET /api/recognition`（237-251 行）：关闭时返回零值状态。

### 数据保留

**不要**删除或清空 `protocol_samples` 表。只停止写入，老数据按 `sample_retention_hours`
自然过期。重新开启后历史能衔接。

## 前端改动点（`web/src/`）

### `lib/types.ts`

- `SettingsResponse` 加 `protocolAnalysis: { enabled: boolean }`
- `/api/protocols` 响应类型加可选 `enabled?: boolean`

### `App.tsx`

- `RecognitionDraft`（65-68 行）加 `protocolAnalysis: { enabled: boolean }`，
  `recognitionDraftFromSettings`（243 行）一并映射。
- `RecognitionSettingsForm`（2189 行）：在**最上方**加总开关
  「启用协议分析」。关闭时，下方 MosDNS 与特征库两个 fieldset 整体 `disabled`
  并加视觉弱化，让层级关系一眼可见。
- **导航隐藏**（已确认方案：直接隐藏，不置灰）：
  - 侧栏「流量监控」下的「协议统计」按钮（1175 行）在关闭时不渲染；
    「策略统计」仍在，故该父项保留。
  - 视图切换器 options（1056 行）关闭时只留「策略统计」。
  - `landingViews`（206 行）与 `statusActive`（1037 行）中的 `'protocols'`
    需要相应处理：关闭时若 `activeView === 'protocols'`，**重定向到 `'policies'`**，
    避免用户停在已隐藏的空页面（例如从 localStorage 恢复出旧的 activeView）。
  - `ProtocolPage`（1356 行渲染点）关闭时不渲染。
- **终端详情「流量分类」面板**（3238 行 `visibleFlows`）：关闭时整块隐藏。
- 检查 `payload.protocols ??= []`（804 行）与 `detail.flowCategories ??= []`（339 行）
  在空数据下不报错。

## 文档

- `configs/config.example.yaml`：加 `protocol_analysis: {enabled: false}` 段
- `.trellis/spec/backend/runtime-configuration.md`：补新字段说明、默认值与迁移行为、
  以及"连接抓取不受开关影响"这条约束

## 测试要求

Go（表驱动，跟随现有 `config_test.go` / `server_test.go` 风格）：

1. `config`：全新安装（路径不存在）→ `Enabled == false`
2. `config`：老配置文件存在且无 `protocol_analysis` 段 → `Enabled == true` 且
   `MigrationPending == true`
3. `config`：老配置显式写 `protocol_analysis: {enabled: false}` → **保持 false**，
   不被迁移覆盖（这条是上面那个坑的回归测试，必须有）
4. `config`：`protocol_analysis_migrated: true` 已存在 → 迁移不再触发
5. `service`：开关关闭时，`snapshot.Protocols` 为空、`FlowCategories` 为空、
   `TerminalConnection.Application` 为空，但 `ConnectionCount`、
   `CurrentUploadBps`/`CurrentDownloadBps`、`ConnectionProtocolCounts`、
   `TerminalConnection.Protocol` **仍然正确**（这是核心回归，证明只砍了分析层）
6. `service`：开关关闭时 `SaveProtocolSamples` 未被调用（可用 fake store 断言）
7. `api`：`GET /api/protocols` 关闭时返回 `enabled: false` 且空数组
8. `api`：`POST /api/settings/recognition` 传 `protocolAnalysis.enabled=false` +
   `mosdns.enabled=true` → 落盘后 `mosdns.enabled` 被压成 false
9. `api`：`GET /api/settings` 含 `protocolAnalysis` 字段

前端：`npm run build` 通过，`tsc` 无错误。

## 验收标准

- [x] `go build ./...` 与 `go test ./...` 全绿（另含 `go vet ./...` 与 `npm run lint`）
- [x] `cd web && npm run build` 通过，产物已同步到 `internal/ui/dist/`（重建复现出一致哈希）
- [x] 关闭开关后：协议统计入口消失、终端详情无流量分类面板、
      概览"活动连接"卡片与 TCP/UDP 构成条**照常显示**
- [x] 关闭开关后：每终端实时上下行速率**照常显示且数值正确**（核心回归）
- [x] 关闭开关后：sqlite 不再新增 `protocol_samples` 行，老数据仍在
      —— 由 `TestProtocolSamplesAreNotSavedWhenAnalysisDisabled` 以临时 SQLite 断言；
      远端未装 `sqlite3` CLI，未做真机行数比对
- [x] 开启开关后：所有协议功能恢复原状
- [x] 老配置文件升级后协议分析仍为开启状态，行为无变化
      —— 已在 `10.0.0.6` 现场验证：原配置无 `protocol_analysis` 段，
      重启后自动写入 `enabled: true` 与 `protocol_analysis_migrated: true`

## 不在范围内

- 不改连接抓取逻辑与轮询间隔
- 不动 `connectionProtocolCounts` 与连接表的协议筛选
- 不清理已有 `protocol_samples` 数据
- 不改 MosDNS / 特征库各自的独立开关语义（总开关只是叠加在它们之上的门）
