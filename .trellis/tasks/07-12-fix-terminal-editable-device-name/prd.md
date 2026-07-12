# 修复终端编辑并改进设备命名

## Goal

修复终端备注编辑被实时刷新覆盖、保存后返回 HTTP 500 的问题，并把终端列表改成以“可识别、可人工修正的设备名称”为第一信息层级。

## Confirmed Evidence

- `web/src/App.tsx:135-146` 在选中终端后每 3 秒加载详情，并无条件执行 `setRemarkDraft(payload.terminal.remark)`；备注弹窗复用了 `selectedTerminalID`，因此用户输入会被下一次轮询覆盖。用户在约第 5 个字符遇到清空，实际取决于输入时点与 3 秒轮询重合。
- 备注 POST 已用数据库原值 `network-vm` 无损复现：数据库 UPDATE 成功，但接口返回 `500 {"error":"failed to update remark"}`。
- `internal/service/monitor.go:113-118` 保存本地备注后同步调用完整 `m.refresh(request.Context())`；任何 RouterOS/SQLite 全量刷新错误都会让已经成功的本地保存被报告为失败，且弹窗不会关闭。
- `internal/api/server.go:152-154` 将上述所有失败统一隐藏为 “failed to update remark”，当前没有向日志记录真实原因。
- 当前 `displayName` 来源是 DHCP lease 的 comment/hostname，缺失时回退到 MAC 或 IP（`internal/service/monitor.go:455,498`）；它不是用户可编辑字段，并会在后续采集时继续由自动值更新。
- 当前终端表第一列是 IP/MAC，后面另有“接口、设备、备注”三列；“设备”只是自动 `displayName`，所以常出现 `-`、MAC 或不易理解的 DHCP hostname。

## Requirements

### R1. 编辑过程稳定

- 打开编辑框后，后台终端详情轮询不得覆盖用户正在编辑的草稿。
- 保存成功后立即关闭弹窗并更新列表/详情；保存失败时保留草稿和弹窗，并显示可定位的错误。
- 本地元数据保存不得依赖一次完整 RouterOS 刷新成功。

### R2. 设备名称语义

- 自动名称继续优先使用 RouterOS DHCP comment/hostname；无法识别时使用明确的地址/MAC 回退显示。
- 用户可以设置持久化的自定义设备名称；自定义名称优先于自动识别结果，后续采集不得覆盖。
- 清空自定义名称后恢复自动识别名称。
- 自定义设备名称与自由文本备注是不同语义，不能因改名丢失备注。

### R3. 列表信息层级

- 删除“接口”列。
- 第一列为“设备名称”，只显示有效设备名。
- 第二列为“IP / MAC”，主行显示主要 IP，次行显示 MAC 和其他地址数量。
- 删除当前重复的“设备”列；保留备注列和编辑入口。
- 搜索、排序、详情页同步支持有效设备名和备注。

## Acceptance Criteria

- [x] 连续输入超过 3 秒或多个轮询周期，编辑内容不被清空或回退。
- [x] 保存设备名/备注返回 200，弹窗关闭，刷新页面后仍保持；不触发全量 RouterOS 刷新导致的假 500。
- [x] 自动识别名称、自定义名称优先、清空后恢复自动名称均有后端测试。
- [x] 终端表第一列为设备名称、第二列为 IP/MAC，且不再展示接口列和重复设备列。
- [x] 前端 lint/build、Go 测试、真实 API、桌面/移动端浏览器验证通过。

## Out of Scope

- 不引入外部 MAC 厂商云服务或联网设备指纹库。
- 不把用户自定义名称写回 RouterOS DHCP comment。
- 不根据流量内容做 DPI 设备类型识别。
