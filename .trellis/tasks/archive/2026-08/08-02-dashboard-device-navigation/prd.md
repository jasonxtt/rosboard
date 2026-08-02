# 仪表台设备区域跳转

## Goal

让仪表台设备运行列表的不同信息区域进入对应的监控页面，减少用户从设备概览继续查找目标监控页的操作。

## Requirements

- 点击设备名称区域进入该设备的系统概览。
- 点击 CPU 或内存区域进入该设备的资源监控。
- 点击终端数量或连接数量区域进入该设备的全部终端列表。
- 进入目标页面时保留被点击设备作为当前设备；进入终端列表时使用“全部终端”范围。
- 保留现有仪表台视觉样式与其他设备列表行为，并为分区点击提供键盘可操作性和可辨识的无障碍名称。

## Acceptance Criteria

- [x] 设备名称区域打开对应设备的系统概览。
- [x] CPU 和内存区域打开对应设备的资源监控。
- [x] 终端数量和连接数量区域打开对应设备的全部终端。
- [x] 前端 lint/build 通过，且未引入嵌套交互控件或页面横向溢出。
- [x] 部署实例可正常加载并通过用户手动验收。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
