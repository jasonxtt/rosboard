# 实施记录：协议分析总开关

## 分工

PRD 与 review 由 Claude 负责，实现由 Herdr 管理的 codex agent 执行，Claude 逐条核对
后放行。design.md 第 8 节列出的 6 条冲突经逐一对照源码后裁定：#1 #2 #4 #5 #6 成立
（其中 #1 #2 #4 #6 是 PRD 自身的错误），#3 驳回——`ApplicationResolver.Resolve`
在 `internal/service/application_resolver.go:38-41` 已有 nil 接收者保护，
`terminalConnectionRow` 不需要额外的 nil 判断。

## 实施步骤

1. 配置层：新增 `ProtocolAnalysisConfig` 值类型、`ProtocolAnalysisMigrated` 顶层标记，
   默认 `Enabled: false`。`Load` 内用 `map[string]yaml.Node` 做**键存在性** probe
   （而非指针 probe），使 `protocol_analysis: null` 也被判定为"段已存在"，避免用户显式
   配置被迁移覆盖。`migrateProtocolAnalysisDefault(fileExisted, sectionPresent)` 紧随
   `migrateRecognitionDefaults()` 调用。
2. 服务层：`MonitorManager` 持有 `protocolAnalysis`；关闭时不构造 MosDNS 同步器、
   特征库同步器与 `ApplicationResolver`，`MosDNSStatus`/`RecognitionStatus` 直接返回零值
   （原实现会在对象为 nil 时从子配置拼出"已启用"，必须显式早返回）。
3. Monitor：`NewMonitor` 从 config 初始化开关，构造后不再写入，无需加锁。关闭时跳过
   `classifyApplication`、resolver 查询、`aggregateProtocols`、`terminalFlowCategories`
   与 `SaveProtocolSamples`；`Application`/`ApplicationSource` 为空、`Estimated` 为 false。
   连接抓取、`connectionProtocolCounts`、终端速率、`FamilyStats`/`FamilySummaries`
   与连接明细路径原样保留。
4. API：`/api/protocols` 的关闭分支前移到 `monitorFor` 之前，返回空数组与
   `enabled: false`，不触碰 DB；`/api/settings` 增加 `protocolAnalysis`；
   `/api/settings/recognition` 把 `protocolAnalysis` 作为可选输入，为 false 时在校验前
   将两个子开关强制置 false；`saveSettings` 同步写入 `ProtocolAnalysisMigrated`。
   设置接口报告**存储值**、状态接口报告**生效值**，该分歧是刻意的。
5. 前端：`RecognitionDraft` 增加总开关并置于表单最上方，关闭时两个子 fieldset 原生
   `disabled` 加视觉弱化；顶部视图切换器去掉"协议统计"，侧栏"流量监控"在关闭时落到
   策略统计；`activeView === 'protocols'` 与终端详情 `flows` tab 均有回退，消化
   localStorage 中的旧值；移动端 tab 网格按 tab 数调整列数。所有开关读取均用双层可选链
   `settings?.protocolAnalysis?.enabled !== false`，类型层声明为可选以强制该写法。
6. 文档：`configs/config.example.yaml` 与
   `.trellis/spec/backend/runtime-configuration.md` 补字段、默认值、迁移行为与
   "连接抓取不受开关影响"约束。

## 验证

- `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全绿
- 新增测试：config 迁移 5 个子用例（缺文件 / 旧文件无段 / 显式 false / null 段 /
  已有迁移标记）、manager 识别对象不构造与状态归零、monitor 核心回归、
  `protocol_samples` 不新增、API 短路与子开关压制
- 核心回归直接断言 `CurrentUploadBps == 800`、`CurrentDownloadBps == 7200`、
  `Protocol == "tcp"`、`ConnectionProtocolCounts{TCP: 1}`，同时断言应用字段与
  流量分类为空，证明只砍了分析层
- `npm run lint`（oxlint）无输出、`npm run build` 通过，重建复现出与提交一致的资源哈希

## 部署与验收

备份目录 `/opt/rosboard/backups/rosboard-protocol-switch-20260810-235125/`，含二进制、
`config.yaml`、systemd unit，以及服务停止后（wal 已 checkpoint）复制的 `rosboard.db`。

远端验证：systemd active、health 200、资源哈希与字节数与本地构建一致、
`Cache-Control: no-cache` 在位。**迁移现场生效**——原配置无 `protocol_analysis` 段，
重启后自动写入 `enabled: true` 与 `protocol_analysis_migrated: true`，老用户行为不变。
API 契约因面板要求认证未在远端直接验证，由 `server_test.go` 覆盖。

用户于 2026-08-10 在 `http://10.0.0.6:8080` 手动验收后回复"通过"。

## 遗留

- `web/src/index.css` 中总开关 label 带有 `protocol-analysis-toggle` 类但无对应样式规则。
  若后续希望总开关在视觉上与两个子 fieldset 形成更明确的层级，这里是落点。
- 远端未装 `sqlite3` CLI，"关闭后 protocol_samples 不再增长"只有单元测试证据，
  没有真机行数比对。
