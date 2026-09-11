# 警告提示去重并统一中文

## Goal

策略路由/访问控制的「审查并应用」等弹窗中，同一问题会以英文一条、中文一条重复出现，且部分中文文案夹杂英文术语（如 "Priority"）。去重并统一为中文。

## Requirements

1. 去重：
   - ~~FastTrack 每条规则的分析结果同时出现在 plan.FastTrack 专区与 plan.Warnings~~（该代码路径只存在于 codex/fasttrack-compatibility 分支，main 基线上不存在，本分支不涉及；已在该工作流另行记录）；
   - 策略路由保存/应用被阻断时的 toast 是英文 "policy plan is blocked"，真实中文原因只在弹窗里——toast 直接携带首个中文阻断原因（对齐访问控制的 accessPlanBlockedError 行为；新增 policyv2.PlanBlockedError，保留 errors.Is(ErrPlanBlocked) 语义）。
2. 全中文：policyv2 / accesscontrol 产生的所有 PlanIssue Reason、blocker/warning 文本、随 blocker 冒出的校验错误，一律改为中文；中文文案中的英文术语翻译成中文（Priority → 优先级、always → 全天 等）。RouterOS 功能专名（FastTrack、DNS、WireGuard、MosDNS）作为专名保留。
3. 前端不再把机器码（snake_case code）当作文案展示（reason 为空时的兜底改为中文占位）。
4. 机器可读的 code 字段、HTTP 错误 code 保持英文不变（前端按 code 分支）。

## Acceptance Criteria

- [ ] 同一问题在同一弹窗内只出现一次
- [ ] 弹窗/toast 中不再有英文句子或 "Priority" 等英文术语
- [ ] `go test ./...` 全绿（更新了断言英文串的测试）
- [ ] 前端 lint/build 通过

## Out of Scope

- 告警文案的样式/排版调整。
- RouterOS 对象 ID（如 `*12`）的展示方式。

## Notes

- 完整告警链路排查结论（生产者 → API → UI、三类重复机制、待翻译清单）见本会话探索记录；关键项已列入 implement.md。
