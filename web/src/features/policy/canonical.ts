import { alwaysAccessSchedule, normalizeAccessSchedule } from './schedule'

export type SubjectBinding = 'auto' | 'fixed'
export type SubjectMember = { terminalId: string; binding: SubjectBinding; pinnedIpv4: string[]; pinnedIpv6: string[] }
export type Subject = { mode: 'all' | 'selected' | 'excluded'; members: SubjectMember[]; prefixes: string[] }
export type { AccessSchedule, AccessScheduleMode, AccessTimeWindow, AccessWeekday } from './schedule'

export type TrafficIngressScope = { interfaceLists: string[]; interfaces: string[] }

export type RoutingSourceKind = 'device' | 'ip' | 'interface' | 'interface-list' | 'all'
export type RoutingSourceScope = {
  kind: RoutingSourceKind
  /** 旧版单选字段；读取时由后端折叠进 interfaces，前端只在兼容旧数据时回退使用 */
  name?: string
  /** kind === 'interface'：命中的 RouterOS 接口（可多选） */
  interfaces?: string[]
  /** kind === 'interface'：命中的 RouterOS 接口列表（可多选） */
  interfaceLists?: string[]
  /** 仅 kind === 'interface'：在接口边界内排除的来源 IP/CIDR */
  excludePrefixes?: string[]
}

export type SourceSelectorInterface = {
  name: string
  type: string
  kind: string
  running: boolean
  disabled: boolean
  dynamic: boolean
  recommended: boolean
  roleHints: string[]
  warnings: string[]
  reason: string
  coveredBy: string[]
}

export type SourceSelectorInterfaceList = {
  name: string
  include: string[]
  exclude: string[]
  staticMembers: string[]
  recommended: boolean
  roleHints: string[]
  warnings: string[]
  reason: string
  safetyCode?: string
}

export type PolicySourceSelectors = {
  available: boolean
  factStatus: 'available' | 'partial' | 'unavailable' | string
  recommendationStatus: 'available' | 'partial' | 'unavailable' | string
  reason?: string
  warnings: string[]
  snapshot: { fingerprint: string }
  interfaces: SourceSelectorInterface[]
  interfaceLists: SourceSelectorInterfaceList[]
}

export type PolicyTerminal = {
  id: string
  displayName: string
  macAddress: string
  ipv4: string[]
  ipv6: string[]
  routingIpv4: string[]
  routingIpv6: string[]
  autoEligible: boolean
}

export type TargetList = {
  id: string
  name: string
  kind: 'domain' | 'ip'
  sourceType: 'manual' | 'url' | 'upload' | 'preset' | string
  presetId?: string
  url?: string
  schedule: string
  enabled: boolean
  activeVersionId: string
  pendingVersionId?: string
  revision: number
  pendingDeletion: boolean
  counts: Record<string, number>
  usage: { routingRuleCount: number; accessRuleCount: number }
  versions: Array<{ id: string; state: string; counts: Record<string, number>; createdAt: string }>
}

export type TargetListDetail = TargetList & { editableContent?: string }

export type TargetListRule = { type: string; domain?: string; address?: string }
export type TargetListPreview = {
  previewId?: string
  url?: string
  filename?: string
  notModified?: boolean
  validRules: number
  counts: Record<string, number>
  ignored: Record<string, number>
  errorSamples: string[]
  rules: TargetListRule[]
  sha256?: string
  size?: number
}

export type ApplicationPreset = { id: string; name: string; category?: string; aliases: string[]; rulePath?: string; ruleURL: string }
export type ApplicationPresetSelection = { presetId: string; previewId: string; requestedKinds: Array<'domain' | 'ip'> }
export type PresetPreview = {
  previewId: string
  id: string
  name: string
  category?: string
  aliases: string[]
  existingTargetListIds: string[]
  domain: TargetListPreview
  ip: TargetListPreview
}

export type EgressFamily = {
  family: 'ipv4' | 'ipv6' | string
  enabled: boolean
  wanInterface: string
  gateway: string
  routeTable: string
  routeMode: string
  natMode: string
  wanSource: string
}

export type Egress = {
  id: string
  origin?: 'legacy' | 'policy' | string
  name: string
  priority: number
  listMode: string
  listName: string
  dnsUpstream: string
  fakeAlias: string
  failureMode: string
  routerOutput: boolean
  enabled: boolean
  pendingDeletion: boolean
  revision: number
  applied: boolean
  families: EgressFamily[]
}

export type RoutingRule = {
  id: string
  name: string
  subject: Subject
  ingress: TrafficIngressScope
  sourceScope?: RoutingSourceScope
  targetListIds: string[]
  egressId: string
  priority: number
  enabled: boolean
  includeKeywordDomains: boolean
  revision: number
}

export type AccessRule = {
  id: string
  name: string
  subject: Subject
  targetScope: 'internet' | 'targets'
  targetListIds: string[]
  schedule: import('./schedule').AccessSchedule
  enabled: boolean
  revision: number
  members: Array<{ terminalId: string; binding: SubjectBinding; state: string; ipv4: string[]; ipv6: string[]; reason?: string }>
  status: string
  issues: string[]
}

export type AccessOverview = {
  device: { id: string; name: string; enabled: boolean }
  rules: AccessRule[]
  terminals: PolicyTerminal[]
  targetLists: TargetList[]
  state: { desiredRevision: number; appliedRevision: number }
  job?: { id: string; state: string; phase: string; error?: string }
  boundary: string
}

export type DiscoveryRoute = { family: string; destination: string; gateway: string; immediateGateway: string; table: string; active: boolean; proven: boolean }
export type DiscoveryWAN = { interface: string; type: string; running: boolean; pointToPoint: boolean; proven: boolean; routes: DiscoveryRoute[] }
export type DiscoveryCandidate = { name: string; kind: string; include: string[]; exclude: string[]; staticMembers: string[]; dynamicMembers: boolean; frozen: boolean; addresses: string[]; reason: string; coveredBy: string[]; default: boolean; dynamic: boolean; running: boolean }
export type PolicyDiscovery = { available: boolean; reason?: string; warnings: string[]; snapshot: { fingerprint: string }; wans: DiscoveryWAN[]; trafficIngress: DiscoveryCandidate[] }
export type PolicyDiscoverySnapshot = { discovery: PolicyDiscovery; sourceSelectors: PolicySourceSelectors }

export type PlanIssue = { code: string; status: string; family?: string; egressID?: string; logicalID?: string; reason: string; requiresAcknowledgement?: boolean }
export type TargetVersionPromotion = { targetListId: string; versionId: string }
export type PlanAcknowledgement = { code: string; required: boolean; accepted: boolean }
export type KeywordImpact = { enabled: boolean; availableCount: number; projectedCount: number; keywords: string[]; introducedKeywords: string[]; requiresConfirmation: boolean; precedenceMode: string }
export type PlanOperation = { seq: number; groupID?: string; egressID?: string; family?: string; phase: string; action: string; menu?: string; logicalID?: string; routerID?: string; ownership?: string; before?: Record<string, unknown>; after?: Record<string, unknown> }
export type ExecutionGroup = { id: string; role: string; egressID?: string; family?: string; operationSeqs: number[] }
export type FastTrackReport = { retainOnly?: boolean; consumers: number; rules: Array<{ id: string; menu: string; status: string; reason: string }> }
export function planAcknowledgementLabel(code: string) {
 if (code === 'routing_keyword_regexp_precedence') return '我已理解关键字域名 regexp 会优先于普通域名匹配，仍要继续。'
 return code.startsWith('fasttrack_') ? '我已了解 FastTrack 可能导致策略路由不稳定的风险，仍要继续。' : code
}
export function fastTrackSummary(report: FastTrackReport) {
 if (report.retainOnly) return '其他已保存策略仍在使用 FastTrack 兼容配置，本次删除不修改或恢复 FastTrack。'
 if (!report.consumers) return '最后一条策略已删除；确认策略路由清理完成后，尝试恢复未被外部修改的 FastTrack。'
 if (!report.rules.length) return '未检测到启用的 FastTrack，无需修改。'
 if (report.rules.every((rule) => rule.status === 'compatible')) return '当前 FastTrack 已排除策略连接，无需修改。'
 return '以下调整在策略路由启用前执行，仅影响新连接；现有连接不会被强制中断。'
}
/** 路由器上没有启用的 FastTrack 且没有待恢复项时，提示块不展示。 */
export function fastTrackNoticeVisible(report: FastTrackReport) {
 if (report.retainOnly) return true
 if (!report.consumers) return true
 return report.rules.length > 0
}
export type PolicyPlan = {
  fastTrack?: FastTrackReport
  planID: string
  kind: string
  lifecycle: string
  createdAt: string
  expiresAt?: string
  desiredRevision: number
  domain?: 'routing' | 'access' | 'combined' | string
  accessRevision?: number
  actualFingerprint: string
  blockers: PlanIssue[]
  familyBlockers: PlanIssue[]
  warnings: PlanIssue[]
  acknowledgements: PlanAcknowledgement[]
  keywordImpact?: KeywordImpact
  requiresAcknowledgement?: boolean
  summary: Record<string, number>
  pendingReview: boolean
  operations: PlanOperation[]
  targetPromotions?: TargetVersionPromotion[]
  executionGroups: ExecutionGroup[]
  planHash: string
  state: string
}
export type PlanEnvelope = { plan: PolicyPlan; planId: string; planHash: string; readOnly: boolean }
export type PolicyPlanProposal = { egress?: Egress; trafficIngress?: TrafficIngressScope; routingRule?: RoutingRule; presetSelections?: ApplicationPresetSelection[] }
export type AccessRuleDraft = Omit<AccessRule, 'members' | 'status' | 'issues'> & {
  presetSelections?: ApplicationPresetSelection[]
  /** Manual internet-egress pick when retrying a save blocked by access_internet_egress_unavailable. */
  internetEgresses?: Record<string, string[]>
}

export type ApplyResult = { jobId?: string; job?: { id: string } }
export type TargetListMutation = { targetList?: TargetList; jobId?: string; job?: { id: string } }

export function applicationPresetTargetListPlan(preview: PresetPreview, requestedKinds: Array<'domain' | 'ip'> = []): { requiredIDs: string[]; reuseExisting: boolean } {
  const requiredIDs: string[] = []
  const kinds = requestedKinds.length ? requestedKinds : preview.domain.validRules > 0 ? ['domain'] : preview.ip.validRules > 0 ? ['ip'] : []
  for (const kind of kinds) {
    if (kind === 'domain' && preview.domain.validRules > 0) requiredIDs.push(`preset:${preview.id}:domain`)
    if (kind === 'ip' && preview.ip.validRules > 0) requiredIDs.push(`preset:${preview.id}:ip`)
  }
  const existingIDs = new Set(preview.existingTargetListIds)
  return { requiredIDs, reuseExisting: requiredIDs.length > 0 && requiredIDs.every((id) => existingIDs.has(id)) }
}

export class CanonicalPolicyError extends Error {
  status: number
  code?: string
  /** Raw backend error envelope (code/error/internetEgressCandidates/desiredSaved/deleted…), when available. */
  details?: unknown
  constructor(message: string, status: number, code?: string, details?: unknown) {
    super(message)
    this.name = 'CanonicalPolicyError'
    this.status = status
    this.code = code
    this.details = details
  }
}

function objectValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function stringValue(value: unknown): string { return typeof value === 'string' ? value : '' }
function numberValue(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}
function booleanValue(value: unknown): boolean { return value === true }
function stringArray(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [] }

function parseSubject(value: unknown): Subject {
  const object = objectValue(value)
  const rawMode = stringValue(object.mode)
  const mode = rawMode === 'all' || rawMode === 'excluded' ? rawMode : 'selected'
  const members = Array.isArray(object.members) ? object.members.map((raw) => {
    const member = objectValue(raw)
    return {
      terminalId: stringValue(member.terminalId),
      binding: stringValue(member.binding) === 'fixed' ? 'fixed' as const : 'auto' as const,
      pinnedIpv4: stringArray(member.pinnedIpv4), pinnedIpv6: stringArray(member.pinnedIpv6),
    }
  }) : []
  return { mode, members, prefixes: stringArray(object.prefixes) }
}

function parseTargetList(value: unknown): TargetList {
  const object = objectValue(value)
  const kind = stringValue(object.kind) === 'ip' ? 'ip' : 'domain'
  const usage = objectValue(object.usage)
  return {
    id: stringValue(object.id), name: stringValue(object.name), kind,
    sourceType: stringValue(object.sourceType ?? object.type), presetId: stringValue(object.presetId) || undefined,
    url: stringValue(object.url) || undefined, schedule: stringValue(object.schedule), enabled: booleanValue(object.enabled),
    activeVersionId: stringValue(object.activeVersionId), pendingVersionId: stringValue(object.pendingVersionId) || undefined,
    revision: numberValue(object.revision), pendingDeletion: booleanValue(object.pendingDeletion),
    counts: objectValue(object.counts) as Record<string, number>,
    usage: { routingRuleCount: numberValue(usage.routingRuleCount), accessRuleCount: numberValue(usage.accessRuleCount) },
    versions: Array.isArray(object.versions) ? object.versions.map((raw) => { const version = objectValue(raw); return { id: stringValue(version.id), state: stringValue(version.state), counts: objectValue(version.counts) as Record<string, number>, createdAt: stringValue(version.createdAt) } }) : [],
  }
}

function parseTargetListDetail(value: unknown): TargetListDetail {
  const object = objectValue(value)
  const target = parseTargetList(value)
  const editableContent = stringValue(object.editableContent)
  return editableContent ? { ...target, editableContent } : target
}

function parsePreview(value: unknown): TargetListPreview {
  const object = objectValue(value)
  const parseRules = (raw: unknown): TargetListRule[] => Array.isArray(raw) ? raw.map((item) => {
    const rule = objectValue(item)
    return { type: stringValue(rule.type), domain: stringValue(rule.domain) || undefined, address: stringValue(rule.address) || undefined }
  }) : []
  return {
    previewId: stringValue(object.previewId) || undefined, url: stringValue(object.url) || undefined,
    filename: stringValue(object.filename) || undefined, notModified: booleanValue(object.notModified),
    validRules: numberValue(object.validRules), counts: objectValue(object.counts) as Record<string, number>, ignored: objectValue(object.ignored) as Record<string, number>,
    errorSamples: stringArray(object.errorSamples), rules: parseRules(object.rules),
    sha256: stringValue(object.sha256) || undefined, size: numberValue(object.size) || undefined,
  }
}

function parseTerminal(value: unknown): PolicyTerminal {
  const object = objectValue(value)
  return { id: stringValue(object.id), displayName: stringValue(object.displayName), macAddress: stringValue(object.macAddress), ipv4: stringArray(object.ipv4), ipv6: stringArray(object.ipv6), routingIpv4: stringArray(object.routingIpv4), routingIpv6: stringArray(object.routingIpv6), autoEligible: booleanValue(object.autoEligible) }
}

function parseRule(value: unknown): AccessRule {
  const object = objectValue(value)
  const scheduleObject = objectValue(object.schedule)
  const scheduleWindows = Array.isArray(scheduleObject.windows) ? scheduleObject.windows.map((raw) => {
    const window = objectValue(raw)
    return {
      days: stringArray(window.days).filter((day): day is import('./schedule').AccessWeekday => ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].includes(day)),
      start: stringValue(window.start),
      end: stringValue(window.end),
    }
  }) : []
  const schedule = normalizeAccessSchedule({ mode: stringValue(scheduleObject.mode) === 'weekly' ? 'weekly' : 'always', windows: scheduleWindows })
  return {
    id: stringValue(object.id), name: stringValue(object.name), subject: parseSubject(object.subject),
    targetScope: stringValue(object.targetScope) === 'targets' ? 'targets' : 'internet', targetListIds: stringArray(object.targetListIds),
    schedule: object.schedule ? schedule : alwaysAccessSchedule(),
    enabled: booleanValue(object.enabled), revision: numberValue(object.revision),
    members: Array.isArray(object.members) ? object.members.map((raw) => {
      const member = objectValue(raw)
      return { terminalId: stringValue(member.terminalId), binding: stringValue(member.binding) === 'fixed' ? 'fixed' as const : 'auto' as const, state: stringValue(member.state), ipv4: stringArray(member.ipv4), ipv6: stringArray(member.ipv6), reason: stringValue(member.reason) || undefined }
    }) : [],
    status: stringValue(object.status), issues: stringArray(object.issues),
  }
}

function parseEgress(value: unknown): Egress {
  const object = objectValue(value)
  return {
    id: stringValue(object.id), origin: stringValue(object.origin) || undefined, name: stringValue(object.name), priority: numberValue(object.priority),
    listMode: stringValue(object.listMode), listName: stringValue(object.listName), dnsUpstream: stringValue(object.dnsUpstream),
    fakeAlias: stringValue(object.fakeAlias), failureMode: stringValue(object.failureMode), routerOutput: booleanValue(object.routerOutput),
    enabled: booleanValue(object.enabled), pendingDeletion: booleanValue(object.pendingDeletion), revision: numberValue(object.revision), applied: booleanValue(object.applied),
    families: Array.isArray(object.families) ? object.families.map((raw) => {
      const family = objectValue(raw)
      return { family: stringValue(family.family), enabled: booleanValue(family.enabled), wanInterface: stringValue(family.wanInterface), gateway: stringValue(family.gateway), routeTable: stringValue(family.routeTable), routeMode: stringValue(family.routeMode), natMode: stringValue(family.natMode), wanSource: stringValue(family.wanSource) }
    }) : [],
  }
}

function parseRoutingRule(value: unknown): RoutingRule {
  const object = objectValue(value)
  const ingress = objectValue(object.ingress)
  const sourceScope = parseRoutingSourceScope(object.sourceScope)
  return { id: stringValue(object.id), name: stringValue(object.name), subject: parseSubject(object.subject), ingress: { interfaceLists: stringArray(ingress.interfaceLists), interfaces: stringArray(ingress.interfaces) }, ...(sourceScope ? { sourceScope } : {}), targetListIds: stringArray(object.targetListIds), egressId: stringValue(object.egressId), priority: numberValue(object.priority), enabled: booleanValue(object.enabled), includeKeywordDomains: booleanValue(object.includeKeywordDomains), revision: numberValue(object.revision) }
}

function parseRoutingSourceScope(value: unknown): RoutingSourceScope | undefined {
  const object = objectValue(value)
  const kind = stringValue(object.kind)
  if (!['device', 'ip', 'interface', 'interface-list', 'all'].includes(kind)) return undefined
  const name = stringValue(object.name).trim()
  const interfaces = stringArray(object.interfaces)
  const interfaceLists = stringArray(object.interfaceLists)
  const excludePrefixes = stringArray(object.excludePrefixes)
  const scope: RoutingSourceScope = { kind: kind as RoutingSourceKind }
  if (name) scope.name = name
  if (interfaces.length) scope.interfaces = interfaces
  if (interfaceLists.length) scope.interfaceLists = interfaceLists
  if (excludePrefixes.length) scope.excludePrefixes = excludePrefixes
  return scope
}

function parseDiscovery(value: unknown): PolicyDiscovery {
  const object = objectValue(value)
  const snapshot = objectValue(object.snapshot)
  return {
    available: booleanValue(object.available), reason: stringValue(object.reason) || undefined,
    snapshot: { fingerprint: stringValue(snapshot.fingerprint) },
    warnings: stringArray(object.warnings),
    wans: Array.isArray(object.wans) ? object.wans.map((raw) => {
      const wan = objectValue(raw)
      return {
        interface: stringValue(wan.interface), type: stringValue(wan.type), running: booleanValue(wan.running), pointToPoint: booleanValue(wan.pointToPoint), proven: booleanValue(wan.proven),
        routes: Array.isArray(wan.routes) ? wan.routes.map((routeRaw) => { const route = objectValue(routeRaw); return { family: stringValue(route.family), destination: stringValue(route.destination), gateway: stringValue(route.gateway), immediateGateway: stringValue(route.immediateGateway), table: stringValue(route.table), active: booleanValue(route.active), proven: booleanValue(route.proven) } }) : [],
      }
    }) : [],
    trafficIngress: Array.isArray(object.trafficIngress) ? object.trafficIngress.map((raw) => {
      const candidate = objectValue(raw)
      return {
        name: stringValue(candidate.name), kind: stringValue(candidate.kind), include: stringArray(candidate.include), exclude: stringArray(candidate.exclude), staticMembers: stringArray(candidate.staticMembers), dynamicMembers: booleanValue(candidate.dynamicMembers), frozen: booleanValue(candidate.frozen), addresses: stringArray(candidate.addresses), reason: stringValue(candidate.reason), coveredBy: stringArray(candidate.coveredBy), default: booleanValue(candidate.default), dynamic: booleanValue(candidate.dynamic), running: booleanValue(candidate.running),
      }
    }) : [],
  }
}

function parseSourceSelectors(value: unknown): PolicySourceSelectors {
  const object = objectValue(value)
  const snapshot = objectValue(object.snapshot)
  return {
    available: booleanValue(object.available),
    factStatus: stringValue(object.factStatus) || 'unavailable',
    recommendationStatus: stringValue(object.recommendationStatus) || 'unavailable',
    reason: stringValue(object.reason) || undefined,
    warnings: stringArray(object.warnings),
    snapshot: { fingerprint: stringValue(snapshot.fingerprint) },
    interfaces: Array.isArray(object.interfaces) ? object.interfaces.map((raw) => {
      const item = objectValue(raw)
      return {
        name: stringValue(item.name), type: stringValue(item.type), kind: stringValue(item.kind),
        running: booleanValue(item.running), disabled: booleanValue(item.disabled), dynamic: booleanValue(item.dynamic),
        recommended: booleanValue(item.recommended), roleHints: stringArray(item.roleHints), warnings: stringArray(item.warnings),
        reason: stringValue(item.reason), coveredBy: stringArray(item.coveredBy),
      }
    }) : [],
    interfaceLists: Array.isArray(object.interfaceLists) ? object.interfaceLists.map((raw) => {
      const item = objectValue(raw)
      return {
        name: stringValue(item.name), include: stringArray(item.include), exclude: stringArray(item.exclude), staticMembers: stringArray(item.staticMembers),
        recommended: booleanValue(item.recommended), roleHints: stringArray(item.roleHints), warnings: stringArray(item.warnings),
        reason: stringValue(item.reason), safetyCode: stringValue(item.safetyCode) || undefined,
      }
    }) : [],
  }
}

function parsePolicyDiscoverySnapshot(value: unknown): PolicyDiscoverySnapshot {
  const object = objectValue(value)
  return {
    discovery: parseDiscovery(object.discovery),
    sourceSelectors: parseSourceSelectors(object.sourceSelectors),
  }
}

function parsePlanIssue(value: unknown): PlanIssue {
  const object = objectValue(value)
  return { code: stringValue(object.code), status: stringValue(object.status), family: stringValue(object.family) || undefined, egressID: stringValue(object.egressID ?? object.egressId) || undefined, logicalID: stringValue(object.logicalID ?? object.logicalId) || undefined, reason: stringValue(object.reason), requiresAcknowledgement: booleanValue(object.requiresAcknowledgement) }
}

function parsePlanOperation(value: unknown): PlanOperation {
  const object = objectValue(value)
  return { seq: numberValue(object.seq ?? object.sequence), groupID: stringValue(object.groupID ?? object.groupId) || undefined, egressID: stringValue(object.egressID ?? object.egressId) || undefined, family: stringValue(object.family) || undefined, phase: stringValue(object.phase), action: stringValue(object.action), menu: stringValue(object.menu) || undefined, logicalID: stringValue(object.logicalID ?? object.logicalId) || undefined, routerID: stringValue(object.routerID ?? object.routerId) || undefined, ownership: stringValue(object.ownership) || undefined, before: objectValue(object.before), after: objectValue(object.after) }
}

function parseFastTrack(value: unknown): FastTrackReport | undefined {
 if (!value || typeof value !== 'object') return undefined
 const object = objectValue(value)
 return { retainOnly: booleanValue(object.retainOnly), consumers: numberValue(object.consumers), rules: Array.isArray(object.rules) ? object.rules.map((value) => { const rule = objectValue(value); return { id: stringValue(rule.id), menu: stringValue(rule.menu), status: stringValue(rule.status), reason: stringValue(rule.reason) } }) : [] }
}
function parsePlan(value: unknown): PolicyPlan {
  const object = objectValue(value)
  const acknowledgements = Array.isArray(object.acknowledgements) ? object.acknowledgements.map((raw) => { const ack = objectValue(raw); return { code: stringValue(ack.code), required: booleanValue(ack.required), accepted: booleanValue(ack.accepted) } }) : []
  const keywordObject = objectValue(object.keywordImpact)
  const keywordImpact = object.keywordImpact && typeof object.keywordImpact === 'object' ? {
    enabled: booleanValue(keywordObject.enabled), availableCount: numberValue(keywordObject.availableCount), projectedCount: numberValue(keywordObject.projectedCount),
    keywords: stringArray(keywordObject.keywords), introducedKeywords: stringArray(keywordObject.introducedKeywords),
    requiresConfirmation: booleanValue(keywordObject.requiresConfirmation), precedenceMode: stringValue(keywordObject.precedenceMode),
  } : undefined
  const groups = Array.isArray(object.executionGroups) ? object.executionGroups.map((raw) => { const group = objectValue(raw); return { id: stringValue(group.id), role: stringValue(group.role), egressID: stringValue(group.egressID ?? group.egressId) || undefined, family: stringValue(group.family) || undefined, operationSeqs: Array.isArray(group.operationSeqs) ? group.operationSeqs.map(numberValue) : [] } }) : []
  const summary: Record<string, number> = {}
  for (const [key, raw] of Object.entries(objectValue(object.summary))) summary[key] = numberValue(raw)
  return {
    fastTrack: parseFastTrack(object.fastTrack), planID: stringValue(object.planID ?? object.planId), kind: stringValue(object.kind), lifecycle: stringValue(object.lifecycle), createdAt: stringValue(object.createdAt), expiresAt: stringValue(object.expiresAt) || undefined,
    desiredRevision: numberValue(object.desiredRevision), domain: stringValue(object.domain) || undefined, accessRevision: numberValue(object.accessRevision) || undefined, actualFingerprint: stringValue(object.actualFingerprint), blockers: Array.isArray(object.blockers) ? object.blockers.map(parsePlanIssue) : [], familyBlockers: Array.isArray(object.familyBlockers) ? object.familyBlockers.map(parsePlanIssue) : [], warnings: Array.isArray(object.warnings) ? object.warnings.map(parsePlanIssue) : [], acknowledgements, keywordImpact, requiresAcknowledgement: booleanValue(object.requiresAcknowledgement), summary, pendingReview: booleanValue(object.pendingReview), operations: Array.isArray(object.operations) ? object.operations.map(parsePlanOperation) : [], targetPromotions: Array.isArray(object.targetPromotions) ? object.targetPromotions.map((raw) => { const promotion = objectValue(raw); return { targetListId: stringValue(promotion.targetListId ?? promotion.targetID), versionId: stringValue(promotion.versionId) } }) : [], executionGroups: groups, planHash: stringValue(object.planHash), state: stringValue(object.state),
  }
}

function parsePlanEnvelope(value: unknown): PlanEnvelope {
  const object = objectValue(value)
  return { plan: parsePlan(object.plan ?? value), planId: stringValue(object.planId ?? object.planID ?? objectValue(object.plan).planID), planHash: stringValue(object.planHash ?? objectValue(object.plan).planHash), readOnly: booleanValue(object.readOnly) }
}

async function requestJSON<T>(path: string, deviceID: string, init: RequestInit | undefined, parse: (value: unknown) => T): Promise<T> {
  const query = deviceID ? `${path.includes('?') ? '&' : '?'}device=${encodeURIComponent(deviceID)}` : ''
  let response: Response
  try {
    response = await fetch(`${path}${query}`, init)
  } catch {
    throw new CanonicalPolicyError('网络请求失败，请检查面板连接', 0, 'network_error')
  }
  if (response.status === 401) window.dispatchEvent(new Event('rosboard:authentication-required'))
  const text = await response.text()
  let payload: unknown = null
  if (text.trim()) {
    try { payload = JSON.parse(text) } catch { throw new CanonicalPolicyError('服务返回了无法解析的响应', response.status, 'invalid_response') }
  }
  if (!response.ok) {
    const failure = objectValue(payload)
    throw new CanonicalPolicyError(stringValue(failure.error) || `HTTP ${response.status}`, response.status, stringValue(failure.code) || undefined, payload)
  }
  return parse(payload)
}

function jsonInit(method: string, body: unknown): RequestInit { return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } }

export function fetchTargetLists(deviceID: string, includePreset = false) { return requestJSON(`/api/target-lists${includePreset ? '?includePreset=true' : ''}`, deviceID, { cache: 'no-store' }, (value) => objectValue(value).targetLists instanceof Array ? (objectValue(value).targetLists as unknown[]).map(parseTargetList) : []) }
export function fetchTargetList(deviceID: string, id: string) { return requestJSON(`/api/target-lists/${encodeURIComponent(id)}`, deviceID, { cache: 'no-store' }, parseTargetListDetail) }
export function saveTargetList(deviceID: string, target: Record<string, unknown>, id?: string, previewId?: string) {
  return requestJSON(`/api/target-lists${id ? `/${encodeURIComponent(id)}` : ''}`, deviceID, jsonInit(id ? 'PUT' : 'POST', { ...target, ...(previewId ? { previewId } : {}) }), (value) => {
    const object = objectValue(value)
    return { targetList: object.targetList ? parseTargetList(object.targetList) : parseTargetList(value), jobId: stringValue(object.jobId) || undefined, job: object.job ? { id: stringValue(objectValue(object.job).id) } : undefined }
  })
}
export function deleteTargetList(deviceID: string, id: string, revision: number) { return requestJSON(`/api/target-lists/${encodeURIComponent(id)}?revision=${revision}`, deviceID, { method: 'DELETE' }, () => undefined) }
export function refreshTargetList(deviceID: string, id: string) { return requestJSON(`/api/target-lists/${encodeURIComponent(id)}/refresh`, deviceID, { method: 'POST' }, (value) => { const object = objectValue(value); return { targetList: object.targetList ? parseTargetList(object.targetList) : undefined, jobId: stringValue(object.jobId) || undefined, job: object.job ? { id: stringValue(objectValue(object.job).id) } : undefined } }) }

export function previewTargetList(deviceID: string, kind: 'domain' | 'ip', sourceType: 'url' | 'manual' | 'upload', input: string | FormData) {
  const path = `/api/target-lists/${sourceType}/preview${sourceType === 'upload' ? `?kind=${encodeURIComponent(kind)}` : ''}`
  if (sourceType === 'upload') return requestJSON(path, deviceID, { method: 'POST', body: input instanceof FormData ? input : undefined }, parsePreview)
  return requestJSON(path, deviceID, jsonInit('POST', sourceType === 'url' ? { url: input, kind } : { text: input, kind }), parsePreview)
}

export function fetchApplicationPresets() { return requestJSON('/api/application-presets', '', { cache: 'no-store' }, (value) => (objectValue(value).presets as unknown[] ?? []).map((item) => { const object = objectValue(item); return { id: stringValue(object.id), name: stringValue(object.name), category: stringValue(object.category) || undefined, aliases: stringArray(object.aliases), rulePath: stringValue(object.rulePath) || undefined, ruleURL: stringValue(object.ruleURL) } })) }
export function previewApplicationPreset(deviceID: string, id: string) { return requestJSON(`/api/application-presets/${encodeURIComponent(id)}/preview`, deviceID, { method: 'POST' }, (value) => { const object = objectValue(value); return { previewId: stringValue(object.previewId), id: stringValue(object.id), name: stringValue(object.name), category: stringValue(object.category) || undefined, aliases: stringArray(object.aliases), existingTargetListIds: stringArray(object.existingTargetListIds), domain: parsePreview(object.domain), ip: parsePreview(object.ip) } }) }
export function materializeApplicationPreset(deviceID: string, id: string, previewId: string, requestedKinds: Array<'domain' | 'ip'> = []) { return requestJSON(`/api/application-presets/${encodeURIComponent(id)}/target-lists?previewId=${encodeURIComponent(previewId)}`, deviceID, jsonInit('POST', { requestedKinds }), (value) => (objectValue(value).targetLists as unknown[] ?? []).map(parseTargetList)) }

export function fetchPolicyDiscovery(deviceID: string) { return requestJSON('/api/policy-routing/discovery', deviceID, { cache: 'no-store' }, parseDiscovery) }
export function fetchPolicySourceSelectors(deviceID: string) { return requestJSON('/api/policy-routing/source-selectors', deviceID, { cache: 'no-store' }, parseSourceSelectors) }
export function fetchPolicyDiscoverySnapshot(deviceID: string) { return requestJSON('/api/policy-routing/discovery-snapshot', deviceID, { cache: 'no-store' }, parsePolicyDiscoverySnapshot) }
export function saveTrafficIngress(deviceID: string, trafficIngress: TrafficIngressScope) { return requestJSON('/api/policy-routing/traffic-ingress', deviceID, jsonInit('PUT', { trafficIngress }), (value) => { const object = objectValue(value); const scope = objectValue(object.trafficIngress ?? value); return { interfaceLists: stringArray(scope.interfaceLists), interfaces: stringArray(scope.interfaces) } }) }
export function saveEgress(deviceID: string, egress: Egress) { return requestJSON(`/api/policy-routing/egresses${egress.id ? `/${encodeURIComponent(egress.id)}` : ''}`, deviceID, jsonInit(egress.id ? 'PUT' : 'POST', egress), parseEgress) }
export function generatePolicyPlan(deviceID: string, kind: string, proposal?: PolicyPlanProposal) { return requestJSON('/api/policy-routing/plans', deviceID, jsonInit('POST', { kind, ...(proposal ? { proposal } : {}) }), parsePlanEnvelope) }
export function applyPolicyPlan(deviceID: string, planId: string, acknowledgements: string[], planHash?: string) { return requestJSON(`/api/policy-routing/plans/${encodeURIComponent(planId)}/apply`, deviceID, jsonInit('POST', { acknowledgements, ...(planHash ? { planHash } : {}) }), (value) => { const object = objectValue(value); return { jobId: stringValue(object.jobId) || undefined, job: object.job ? { id: stringValue(objectValue(object.job).id) } : undefined } }) }

export function fetchRoutingContext(deviceID: string) {
  return Promise.all([
    requestJSON('/api/policy-routing/overview', deviceID, { cache: 'no-store' }, (value) => { const object = objectValue(value); const scope = objectValue(object.trafficIngress); return { egresses: (object.egresses as unknown[] ?? []).map(parseEgress), trafficIngress: { interfaceLists: stringArray(scope.interfaceLists), interfaces: stringArray(scope.interfaces) } } }),
    requestJSON('/api/policy-routing/rules', deviceID, { cache: 'no-store' }, (value) => (objectValue(value).rules as unknown[] ?? []).map(parseRoutingRule)),
    fetchTargetLists(deviceID, true),
    loadAccessOverview(deviceID),
  ]).then(([overview, rules, targetLists, access]) => ({ ...overview, rules, targetLists, terminals: access.terminals }))
}

export function saveRoutingRule(deviceID: string, rule: RoutingRule, deferApply = false) { return requestJSON(`/api/policy-routing/rules${rule.id ? `/${encodeURIComponent(rule.id)}` : ''}`, deviceID, jsonInit(rule.id ? 'PUT' : 'POST', { ...rule, ...(deferApply ? { deferApply: true } : {}) }), (value) => { const object = objectValue(value); return { rule: object.rule ? parseRoutingRule(object.rule) : parseRoutingRule(value), jobId: stringValue(object.jobId) || undefined, job: object.job ? { id: stringValue(objectValue(object.job).id) } : undefined } }) }
export function deleteRoutingRule(deviceID: string, id: string, revision: number) { return requestJSON(`/api/policy-routing/rules/${encodeURIComponent(id)}?revision=${revision}`, deviceID, { method: 'DELETE' }, (value) => objectValue(value) as ApplyResult) }

export function loadAccessOverview(deviceID: string) { return requestJSON(`/api/access-control/devices/${encodeURIComponent(deviceID)}`, '', { cache: 'no-store' }, (value) => { const object = objectValue(value); const state = objectValue(object.state); return { device: objectValue(object.device) as AccessOverview['device'], rules: (object.rules as unknown[] ?? []).map(parseRule), terminals: (object.terminals as unknown[] ?? []).map(parseTerminal), targetLists: (object.targetLists as unknown[] ?? []).map(parseTargetList), state: { desiredRevision: numberValue(state.desiredRevision), appliedRevision: numberValue(state.appliedRevision) }, job: object.job ? objectValue(object.job) as AccessOverview['job'] : undefined, boundary: stringValue(object.boundary) } }) }
export function saveAccessRule(deviceID: string, rule: AccessRuleDraft) { return requestJSON(`/api/access-control/devices/${encodeURIComponent(deviceID)}/rules${rule.id ? `/${encodeURIComponent(rule.id)}` : ''}`, '', jsonInit(rule.id ? 'PUT' : 'POST', rule), (value) => objectValue(value) as ApplyResult) }
export function deleteAccessRule(deviceID: string, id: string, revision: number) { return requestJSON(`/api/access-control/devices/${encodeURIComponent(deviceID)}/rules/${encodeURIComponent(id)}?revision=${revision}`, '', { method: 'DELETE' }, (value) => objectValue(value) as ApplyResult) }

export async function waitForPolicyJob(deviceID: string, jobID: string) {
  const deadline = Date.now() + 120_000
  while (Date.now() < deadline) {
    const job = await requestJSON(`/api/policy-routing/jobs/${encodeURIComponent(jobID)}`, deviceID, { cache: 'no-store' }, (value) => objectValue(objectValue(value).job ?? value))
    const state = stringValue(objectValue(job).state)
    if (state === 'committed') return Array.isArray(job.warnings) ? job.warnings.map(parsePlanIssue) : []
    if (['failed', 'committed_partial', 'needs_decision', 'rolled_back', 'rollback_failed'].includes(state)) throw new CanonicalPolicyError(stringValue(objectValue(job).error) || 'RouterOS 同步失败', 409, 'job_failed')
    await new Promise((resolve) => window.setTimeout(resolve, 500))
  }
  throw new CanonicalPolicyError('等待 RouterOS 应用超时', 408, 'job_timeout')
}

export async function waitForAccessJob(deviceID: string, jobID: string) {
  const deadline = Date.now() + 120_000
  while (Date.now() < deadline) {
    const job = await requestJSON(`/api/access-control/devices/${encodeURIComponent(deviceID)}/jobs/${encodeURIComponent(jobID)}`, '', { cache: 'no-store' }, (value) => objectValue(objectValue(value).job ?? value))
    const state = stringValue(objectValue(job).state)
    if (state === 'committed') return
    if (['failed', 'committed_partial', 'needs_decision', 'rolled_back', 'rollback_failed'].includes(state)) throw new CanonicalPolicyError(stringValue(objectValue(job).error) || 'RouterOS 同步失败', 409, 'job_failed')
    await new Promise((resolve) => window.setTimeout(resolve, 500))
  }
  throw new CanonicalPolicyError('等待 RouterOS 应用超时', 408, 'job_timeout')
}
