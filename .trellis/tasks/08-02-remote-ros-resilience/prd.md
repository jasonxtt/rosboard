# 远程 RouterOS 采集容错

## 目标

降低远程 RouterOS（尤其是 RouterOS 7.10）在网络抖动或 REST 并发能力较弱时被误判为不可用的概率，同时保持多设备采集互不阻塞。

## 要求

- [x] `/rest/system/resource` 同时兼容 RouterOS 返回的对象和单元素数组。
- [x] CPU、IRQ、硬件详情属于可选采集，不得阻塞主资源、接口和设备启动；单台设备的详情请求不得并发轰击同一个 RouterOS。
- [x] realtime 采集不得每秒重复请求低频资源详情；主资源失败时沿用现有快照并按现有告警机制重试。
- [x] 设备启动失败重试采用递增间隔，避免多个设备同时持续重试。
- [x] 空或非法更新时间在 UI 中显示为“尚未成功采集”，不得显示异常的大分钟数。
- [x] 不修改 SQLite schema，不改变 RouterOS 配置写入行为。

## 验收标准

- [x] Go 单元测试覆盖资源对象/数组解析、详情串行/可选降级和重试间隔。
- [x] `go test ./...`、`go test -race ./internal/service ./internal/api` 通过。
- [x] `npm --prefix web run lint`、`npm --prefix web run build` 通过，嵌入前端资源可加载。
- [x] 模拟慢速/数组响应的 RouterOS 不会因可选详情失败而阻塞主采集。
- [x] 部署到 `10.0.0.6` 后 systemd、健康接口和受影响 API 验证通过，用户已手动验收。

## Goal

TBD.

## Requirements

- TBD

## Acceptance Criteria

- [ ] TBD

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
