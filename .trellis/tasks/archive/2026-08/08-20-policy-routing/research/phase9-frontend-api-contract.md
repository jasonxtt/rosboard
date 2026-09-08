# Phase 9 前端消费的 API 契约（实测提取）

来源：`internal/api/policy_routing.go`、`policy_review.go`、`policy_materialization.go`、`provisioning.go`、`internal/policy/planner.go`、`capabilities.go`、`desired_loader.go`。提取日期：2026-08-23。供 Phase 9/10 前端与 Lead review 使用；如后端变动以代码为准。

## 通用约定

- 所有端点位于 `/api/policy-routing/**`，必须带 `?device=<id>`。
- 设备门禁：缺 device → `400 device_required`；未知 → `404 device_not_found`；archived/disabled → `409 device_archived|device_disabled`。
- 错误体（`writePolicyError`）：`{code, error, details}`，`details` 恒为对象；但共享 helper 抛出的 `invalid_json`/`authentication_required`/`onboarding_required` **无 details 字段**。
- Step-up：access/lan-scope/egress/source 保存、plan apply（requiresStepUp 时）、job resume/rollback、session complete 都需要请求体 `adminPassword`。错误：`403 step_up_required`、`401 step_up_failed`、`429 step_up_rate_limited`（带 `Retry-After`）。
- JSON 请求体上限 64 KiB。

## 端点摘要

| 方法/路径 | 说明 |
|---|---|
| GET `/overview` | 页面主 payload（见下） |
| GET `/discovery` | 只读 WAN/LAN/existingPolicy/adoptionCandidates；失败用 `available:false`+`reason` 表达，恒 200 |
| PUT `/access` | `{enabled, username?, password?, adminPassword}` → `{access, restarting}` |
| POST `/access/sessions` | → 201 `{sessionId, deviceId, username, script, expiresAt}`（15 分钟有效，密码只在 script 内） |
| POST `/access/sessions/{id}/complete` | `{adminPassword}` → `{access, restarting}`；`410 policy_provisioning_expired`、`502 routeros_policy_write_capability_unverified`（只读账号）等 |
| PUT `/lan-scope` | `{scope: {...}, adminPassword}` → `{lanScope}`；scope 是 planner 消费的期望状态 JSON（见下） |
| POST/PUT `/egresses[/{id}]`、GET `/egresses/{id}` | egress 草稿保存；`409 stale_egress`（revision）；**无 DELETE、无列表端点** |
| POST/PUT `/sources[/{id}]`、GET `/sources/{id}` | 来源保存；内容变化必须带 `previewId`；`410 preview_expired`、`409 preview_mismatch`；无 DELETE |
| GET `/sources/{id}/rules?cursor=&limit=&query=&type=` | active 版本规则分页；limit 1–1000 默认 100；`nextCursor` 空串结束；无 active 版本返回空页 |
| POST `/sources/url/preview` | `{url, etag?, lastModified?}` → `PolicySourcePreview`；`502 source_upstream_failed` |
| POST `/sources/upload/preview` | multipart，单文件 ≤5 MiB，`.yaml/.yml`；`413 upload_too_large`、`400 invalid_upload` |
| POST `/plans` | `{kind, lifecycle?, fallback?, mainTableReuse?, reuseUserList?, highRiskFirewall?}` → 201 `{plan, planId, planHash}`；`409 stale_desired_state/plan_conflict`、`422 plan_generation_failed` |
| POST `/plans/{id}/apply` | `{acknowledgements: string[], adminPassword}` → 202 `{job, jobId}`；`409 stale_plan/plan_expired/plan_replayed/job_conflict`、`403 acknowledgements_required details.codes`、`422 plan_blocked details.blockers` |
| GET `/jobs/{id}` | job + steps + recoveryChoices + partialResult |
| POST `/jobs/{id}/cancel` | 无 body → 202 |
| POST `/jobs/{id}/resume` `/rollback` | `{adminPassword}` → 202；`409 job_recovery_failed/job_rollback_failed` |
| GET `/audit?cursor=&limit=` | `{entries[], nextCursor}` |
| GET `/backups/{id}/download` | 二进制 zip；**无 backup 列表端点**（Phase 10 再补 UI） |
| POST `/drift/plan` | 同 review request → `{plan, planId, planHash, readOnly:true}` |
| POST `/adoption/preview` `/takeover/preview` | `{objects/selected/candidates: [{logicalId, routerId, selected...}], force?}` → 同上；takeover 隐含 force |

## 关键 payload 形状

### overview

```ts
{
  device: {id, name, enabled, archived},
  access: {enabled, username, passwordSet, managed, cleanupAvailable},
  setup: {state: 'access_required'|'manager_unavailable'|'runtime_unavailable'|'ready', managerAvailable: boolean},
  capability: {state: 'unavailable'|'ready', reason: string, version?: string,
               entries: Record<string, CapabilityEvidence>},  // PascalCase 字段！
  lanScope: Record<string, unknown>,   // {} 未配置；内容为 PUT /lan-scope 的 scope
  health: {state, driftState, mutationPaused, manualInterventionRequired, pauseReason?, pauseJobId?},
  drift: {state, items: []},           // items 当前恒为 []
  egresses: Egress[],                  // sources/objects 为空壳；详情用 GET /egresses/{id}
  sources: Source[],                   // 扁平列表；versions=[] counts={}
  activeJobs: Job[],                   // 非 needs_decision 的可恢复 job；steps=[]
  pendingJobs: Job[],                  // state=='needs_decision'，recoveryChoices=['resume','rollback']
}
```

### lan-scope scope JSON（planner 实际消费的键，desired_loader.go）

`interfaceList`（主 LAN 范围名）→ 回退 `lanScope`；`listName`/`lanListName`；`interfaces`/`lanInterfaces`（确认的 LAN 接口）；`trustedLANScope`；`allowRemoteRequests`、`needForwarders`、`needOutputPlumbing`；`disableEgresses`、`deleteEgresses`（disable_delete 计划的目标选择）；另有 anchors/NAT/firewall 等高级键（Phase 10）。前端写入最小形状：`{interfaceList, listName, interfaces}`，合并更新时保留未知键。

### egress

```ts
{id, name, priority, listMode: 'shared'|'dedicated', listName, dnsUpstream, fakeAlias,
 failureMode: 'strict'|'fallback'|'existing', routerOutput, enabled, revision,
 families: [{family: 'ipv4'|'ipv6', enabled, wanInterface, gateway, routeTable, routeMode, natMode, wanSource}],
 sources: [], objects: []}  // 后两者仅 GET /egresses/{id} 填充
```

### source / preview

```ts
Source {id, egressId, type: 'url'|'upload', name, url?, schedule, enabled,
        activeVersionId, lastGoodVersionId, etag?, lastModified?, nextRunAt?, revision,
        versions: [{id, sha256, state: 'pending'|'success'|'failed', error?, httpStatus?, createdAt, counts, diff}], counts}
Preview {previewId, url, filename, statusCode, contentType, etag, lastModified, notModified,
         size, sha256, validRules, ignored: Record<string,number>, errorSamples: [],
         rules: [{type: 'DOMAIN'|'DOMAIN-SUFFIX', domain}]}  // rules 仅前 100 条；previewId 15 分钟有效
```

### ChangePlan（plans 与 review 端点返回）

- 顶层：`planID, deviceID, kind, lifecycle, createdAt, expiresAt`（零值序列化为 `0001-01-01T00:00:00Z`）`, desiredRevision, desiredHash, actualFingerprint, capabilities: Record<string,string>, blockers: [], familyBlockers?: [], warnings: [], acknowledgements: [{code, required, accepted}], requiresStepUp, ownershipStrict, summary{create,patch,delete,move,disable,enable,referenceAdd,referenceRemove,reuse,adopt,blockers,warnings,familyBlockers}, resourceEstimate{validDomains,activeDNSStatic,resourceWarning,sourceLimit,deviceLimit,largestSource,scheduledShrink,removedDomains}, pendingReview, operations: [], executionGroups: [], planHash, state`。
- `familyBlockers`/`priorityOrder`/`migration`/`referenceDeltas`/`sourceActivations` 为 omitempty —— 空时**字段缺失**，前端必须归一化为 []。
- operation：`{seq, operationID?, groupID?, egressID?, family?, phase, action, menu?, logicalID?, routerID?, ownership?, before?, after?, managedDelta?, managedBefore?, anchor?, verification{action,...}, compensation{action,menu?,reason?,...}}`。
- action 枚举：`create patch delete move disable enable reference_add reference_remove reuse adopt dns_cache_flush`；phase：`foundation routing plumbing dns_static references cleanup activation`；ownership：`owned reused foreign manual_candidate`。
- acknowledgement codes：`fallback_main_table main_table_reuse firewall_high_risk_exception reuse_user_list adoption force_adoption managed_field_delta large_change source_shrink_review`。
- **大小写陷阱**：plan JSON 用 `planID/egressID/logicalID`（大写 ID），API DTO 用 `planId/jobId/egressId`，`plan_blocked` 的 details 用 `logicalId/egressId`；`CapabilityEvidence` 与 `OwnershipComment` 无 json tag → PascalCase 键（`Capability/Status/Reason/Evidence`）。

### job

```ts
{id, planId, acknowledgements: [], stepUpApproved, state, phase, progress: number,
 cancelRequested, error?, primaryError?, rollbackError?, failedOperation?,
 createdAt, startedAt?, finishedAt?, steps: [{sequence, action, target, routerId?, status, attempt, error?, updatedAt}],
 recoveryChoices: [],   // needs_decision/rollback_failed 时 ['resume','rollback']
 partialResult: {planId?, planHash?, configuredFamilies?, blockedFamilies?, entries?, createdAt?}}  // {} 表示无
```

state 枚举：`queued reconciling backing_up staging ordering activating flushing_cache verifying committed committed_partial rolled_back rollback_failed needs_decision cancelled_before_write failed`；终态：`committed committed_partial rolled_back rollback_failed cancelled_before_write failed`。step status：`not_applied prepared applied failed`。

**progress 语义（Lead review 两轮确认）**：`Job.Progress` 是 int 类型的**已完成步骤数**（executor 用 `index`/`len(steps)` 赋值），不是 0..1 比例；`job.steps` 只是当前已写入 journal 的 prepared/applied/failed 行，**不是 immutable plan 的总操作数**（多操作计划第一步 progress=1 时 steps.length=1），任何端点都没有可信总数。因此 UI 只显示「已完成 N 步」文本，绝不显示百分比。若未来需要百分比，需后端在 job 响应中补 `totalSteps`（属后端改动，本阶段未动）。

**adoption 契约（Lead review 确认）**：`reviewAdoptionEvidence` 要求 `selected && userSelected && compatibilityComplete` 三者同时具备，planner 才不产生 manual_candidate blocker；同一 logicalID 在快照中出现多次时服务端强制 `compatibilityComplete=false`（fail closed）。foreign 对象即使 `force=true` 也会产生 blocker —— force 只是 acknowledgement，Phase 9 前端不提供 takeover UI。

### discovery

```ts
{device, available, reason,
 snapshot: {fingerprint, deviceIdentity: {}, capabilities: Record<string, CapabilityEvidence>},
 wans: [{interface, type, running, pointToPoint, proven,
         routes: [{id, family, destination, gateway, immediateGateway, table, source, distance, active, proven}]}],
 lan: [{name, include?, exclude?, staticMembers: [], dynamicMembers?, frozen}],
 existingPolicy: [{logicalId, routerId, menu, ownership, reason, foreignManager}],
 adoptionCandidates: [同上子集]}  // routerId != '' 且 ownership != 'owned'
```

## 标记列表语义（Lead review 第八轮确认）

- `shared`：一个出口一个 address-list，多域名列表共享；出口级 `listName` 可编辑（默认 `manual_proxy_lab`）。
- `dedicated`：一个域名来源（PolicySource）对应一个独立 address-list，名称由 source 稳定命名规则自动生成；出口级 `listName` **不是**每来源名称，仅作后端保存契约的兼容值（`saveEgress` 仍要求非空）。
- 前端展示：dedicated 不提供出口级名称编辑；出口表按域名列表逐行显示「域名列表名 → 专用标记列表（自动生成）」；来源表按归属出口模式显示共享名或「专用（随本域名列表自动生成）」；向导确认草稿显示逐列表映射预览（明确为预计生成语义）。
- 边界：真实后端尚无逐来源自定义名称的展示/查询字段；UI 不伪造实际 RouterOS 名称。

## Phase 9 边界说明

- 无 egress/source DELETE 端点；出口硬删除经 lan-scope 的 `deleteEgresses` + `kind=disable_delete` 计划驱动，implement.md 把“删除/停用/迁移、最后来源、出口删除、account cleanup 全生命周期”划给 Phase 10。Phase 9 前端提供 create/read/update + enabled 停用。
- backup 无列表端点，download 需要已知 id；Phase 9 不提供备份下载 UI。
- 健康/漂移状态当前后端只写 `unknown`；UI 按字符串透传并对 `unknown` 显示中性态。
