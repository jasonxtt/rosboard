# 资源监控技术设计

## 1. 边界与职责

- RouterOS client 继续负责调用 `/rest/system/resource`，并新增只读的 `/rest/system/resource/cpu`、`/rest/system/resource/irq` 和 `/rest/system/resource/hardware` 采集。
- Monitor 的 realtime/full 刷新继续负责采集、缓存和发布资源数据。
- 现有 dashboard/realtime API 继续作为前端数据边界，不新增 `/api/resource`。
- 前端新增资源监控视图，只读取 API 返回的快照并负责展示和安全格式化。
- SQLite 不保存硬件身份字段；历史 CPU、内存、存储采样继续使用现有 `load_samples`。

## 2. 数据契约

在现有 `Overview` 中扩展向后兼容的 `systemResource` 对象，映射 `/system/resource` 体系的只读字段：

```text
systemResource: {
  architectureName,
  boardName,
  cpu,
  cpuCount,
  cpuFrequency,
  cpuLoad,
  buildTime,
  factorySoftware,
  badBlocks,
  writeSectSinceReboot,
  writeSectTotal,
  freeMemory,
  freeHddSpace,
  platform,
  totalMemory,
  totalHddSpace,
  uptime,
  version,
  cpuCores: [{ cpu, load, irq, disk }],
  irqs: [{ cpu, activeCpu, count, irq, users }],
  hardware: [{ location, parent, type, vendor, name, serialNumber, vendorId, deviceId, speed, ports, usbVersion, owner, devicePath, category, irq }]
}
```

- 字段名称遵循现有 Go/TypeScript JSON 命名风格；不删除或重命名现有 `Overview` 字段。
- realtime 刷新获得的总资源和逐核 CPU 对象与当前 CPU/内存快照一起合并，避免资源页面等待慢速 full refresh。
- IRQ、硬件和构建/写入统计等低频字段在 full refresh 中更新；realtime 合并时保留这些字段，避免静态详情因快照合并丢失。
- `/system/resource/cpu`、`irq` 或 `hardware` 不可用时不阻塞主资源采集；页面显示对应模块不可用或暂无数据，并保留最近一次有效详情。
- full refresh 提交时保留更新更近的 realtime 资源对象，遵循现有 tiered polling 的新数据优先约定。
- `cpuLoad`、内存和硬盘原始字段保留 RouterOS 的含义；百分比、已用量仅由展示层或现有派生逻辑计算。
- API/前端对 `systemResource` 缺失时使用不可用状态，兼容滚动发布期间的旧后端。

## 3. 页面结构

资源监控页面保持现有监控壳层和设备上下文，主体使用四列窄卡片网格，主体分为：

1. CPU 资源卡：当前总 CPU 负载、CPU 型号、核心数、频率和逐核 CPU/IRQ/Disk 列表；卡片允许跨两列并自然增高。
2. 系统信息卡：平台、架构、主板、RouterOS 版本、构建时间、出厂软件版本、运行时间；与 CPU 卡同一行。
3. 内存资源卡：总内存、已用内存、空闲内存、使用率。
4. 存储资源卡：总硬盘空间、已用空间、空闲空间、使用率、坏块比例和写入扇区统计。
5. IRQ 详情卡：IRQ、活跃 CPU、计数和使用者。
6. 硬件设备卡：设备位置、总线类型、厂商、名称、序列号、速度、所有者、设备路径及 IRQ 等只读字段。

使用现有格式化工具和卡片样式，避免复制一套资源计算/单位逻辑。缺失字段使用 `-`；只有总量和空闲量均为有效正数时才计算使用率。

“负载历史”继续作为独立页面提供趋势图，资源监控不再复制完整历史图表。

## 4. 刷新与状态

- 将资源视图纳入现有 overview/realtime 前端刷新条件。
- CPU 逐核数据随 realtime 刷新；IRQ、硬件和低频系统字段随 full refresh 更新，不为资源页新增独立计时器。
- 资源视图首次加载使用当前 dashboard 快照；后续使用现有 realtime 缓存响应。
- 保持现有设备 ID 作用域和 `updatedAt` 保护，旧响应不能覆盖较新的资源数据。
- Monitor 采集失败时沿用现有错误处理，保留最后一次有效快照，不在页面自行重试 RouterOS。

## 5. 兼容、风险与回滚

- 新增嵌套字段只扩展 JSON，不破坏已有字段；旧消费者可以忽略该字段。
- 资源对象在 realtime 与 full refresh 间合并不当，可能出现硬件字段回退；需要复用现有快照合并规则并增加回归测试。
- RouterOS 不同设备的字段可能为空或单位格式不同；页面展示原始字段，并对数值派生做严格的空值/零值保护。
- `/system/resource/cpu`、`irq`、`hardware` 并非所有 RouterOS 设备/版本都保证可用；各子模块必须独立降级，不能因为一个详情接口失败而让整台设备资源监控失败。
- 硬件详情可能包含序列号等设备识别信息；只在已认证的面板内展示，仍保持只读，不写入或修改设备。
- 新导航项可能影响 landing view 类型、侧边栏展开状态和移动端布局；需要逐一更新 ActiveView 的穷举分支。
- 回滚时删除新视图及 `systemResource` 扩展即可，不涉及数据库迁移；远端按项目要求保留发布前二进制、配置和 SQLite 备份。
