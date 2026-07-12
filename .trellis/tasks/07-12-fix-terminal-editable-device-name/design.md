# Technical Design

## Data Model

现有 `terminals.display_name` 继续保存 RouterOS 自动识别名称，新增 `custom_name TEXT NOT NULL DEFAULT ''` 保存用户覆盖值。两者保持独立：

- `autoName`：DHCP comment/hostname；无可靠自动名称时为空。
- `customName`：用户输入，SQLite 持久化。
- `displayName`：API 提供的有效名称，计算顺序为 `customName > autoName > primary address > MAC > 未命名设备`。

`Terminal` JSON 新增 `autoName`、`customName`，保留 `displayName` 作为所有展示和搜索的统一有效名称，避免每个前端组件自行实现优先级。

SQLite 采用兼容迁移 `ALTER TABLE terminals ADD COLUMN custom_name ...`。现有 `display_name` 数据无需迁移；MAC/IP 形式的旧回退值在投影时不当作可靠 autoName 展示提示，但仍可安全兼容。

## Metadata Save Flow

新增单一元数据更新契约：

```text
POST /api/terminals/{id}/metadata
{ "customName": string, "remark": string }
-> TerminalDetail
```

服务流程：

1. API 解码、trim 并限制合理长度。
2. Store 在一个 UPDATE 中保存 `custom_name` 和 `remark`，检查 affected rows；未知终端返回 not found。
3. Monitor 在锁内更新当前 snapshot、terminal detail 及 family summaries 的名称/备注投影。
4. 立即返回更新后的 `TerminalDetail`，不调用 RouterOS 全量 `refresh()`。

旧 `/remark` 端点暂时保留兼容，但内部走同一局部更新机制，避免已有调用方继续触发假 500。

保存失败时 API 返回可区分的 400/404/500；服务端记录底层错误，前端读取响应 JSON 的错误文本。保存成功后前端更新 dashboard/detail 并关闭弹窗。

## Draft Isolation

编辑弹窗使用独立 `editingTerminalID`，不再借用 `selectedTerminalID`。打开弹窗时只初始化一次 `customNameDraft` 和 `remarkDraft`；dashboard 和 detail 的后台轮询只更新服务端数据，不写入正在编辑的草稿。

关闭或保存成功时清理 editing state。保存中禁用提交，防止重复请求。

## Automatic Naming

自动识别继续使用已有 RouterOS DHCP lease：优先 comment，其次 hostname。MAC/IP 只作为显示回退，不伪装成“识别出的设备型号”。编辑框展示“自动识别：xxx”提示；没有可靠名称时显示“暂未识别”。清空自定义名称即恢复自动/回退名称。

本次不新增外部厂商库：MAC OUI 最多能识别厂商，无法可靠得出 `iPhone 13 Pro Max` 这一级型号，避免制造看似准确的错误名称。

## Table Layout

终端表列顺序：

1. 设备名称（有效 `displayName`）
2. IP / MAC
3. 连接数
4. 上行速率
5. 下行速率
6. 累计上行
7. 累计下行
8. 在线状态/时长
9. 备注
10. 操作

删除接口列和旧设备列。第一、二列均可进入详情；移动端优先保留设备名称、IP/MAC、状态和操作，其余列在表内按现有响应式规则隐藏。

## Compatibility and Rollback

- 数据库只新增非空默认列，无破坏性迁移。
- `displayName` JSON 字段保留，现有消费者继续工作。
- 旧 remark API 保留，回滚前端不会失去备注编辑能力。
- 不写回 RouterOS，所有人工名称和备注仍仅保存在面板本地。
