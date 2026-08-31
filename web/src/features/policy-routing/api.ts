import type {
  PolicyApplyResult,
  PolicyAuditPage,
  PolicyDiscovery,
  PolicyEgress,
  PolicyEgressDraft,
  PolicyJob,
  PolicyOverview,
  PolicyPlan,
  PolicyPlanEnvelope,
  PolicyPreview,
  PolicyRulesPage,
  PolicySource,
  PolicySourceDraft,
  PolicySourceSaveResult,
} from './types'
import { PolicyApiError } from './types'

function isAbortError(e: unknown): boolean {
  return e instanceof DOMException && e.name === 'AbortError'
}

function safeObject(value: unknown): Record<string, unknown> {
  if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

function safeArray<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value as T[]
  return []
}

function safeString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function safeNumber(value: unknown): number {
  if (typeof value === 'number') return Number.isFinite(value) ? value : 0
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : 0
  }
  return 0
}

function safeBoolean(value: unknown): boolean {
  return value === true
}

function stringRecord(value: unknown): Record<string, string> {
  const result: Record<string, string> = {}
  const obj = safeObject(value)
  for (const [key, val] of Object.entries(obj)) {
    if (typeof val === 'string') result[key] = val
  }
  return result
}

function numberRecord(value: unknown): Record<string, number> {
  const result: Record<string, number> = {}
  const obj = safeObject(value)
  for (const [key, val] of Object.entries(obj)) {
    if (typeof val === 'number' && Number.isFinite(val)) result[key] = val
  }
  return result
}

function valueRecord(value: unknown): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  for (const [key, val] of Object.entries(safeObject(value))) {
    if (val === null || typeof val === 'string' || typeof val === 'number' || typeof val === 'boolean') result[key] = val
    else if (Array.isArray(val)) result[key] = val
    else if (typeof val === 'object') result[key] = val
  }
  return result
}

function policyURL(deviceID: string, path: string): string {
  const separator = path.includes('?') ? '&' : '?'
  return `/api/policy-routing${path}${separator}device=${encodeURIComponent(deviceID)}`
}

interface RequestOptions {
  method?: string
  signal?: AbortSignal | null
  body?: unknown
  formData?: FormData
}

async function policyFetch<T>(
  deviceID: string,
  path: string,
  options: RequestOptions,
  parse: (data: unknown) => T,
): Promise<T> {
  let response: Response
  try {
    response = await fetch(policyURL(deviceID, path), {
      method: options.method ?? 'GET',
      signal: options.signal ?? null,
      headers: options.formData === undefined && options.body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: options.formData ?? (options.body !== undefined ? JSON.stringify(options.body) : undefined),
    })
  } catch (fetchError) {
    if (isAbortError(fetchError)) throw fetchError
    throw new PolicyApiError('网络请求失败，请检查面板连接', 0, 'network_error')
  }
  if (response.status === 401) {
    window.dispatchEvent(new Event('rosboard:authentication-required'))
  }
  if (!response.ok) {
    const failure = (await response.json().catch(() => null)) as { error?: string; code?: string; details?: Record<string, unknown> } | null
    throw new PolicyApiError(failure?.error ?? `HTTP ${response.status}`, response.status, failure?.code, failure?.details)
  }
  const responseText = await response.text()
  if (!responseText.trim()) return parse(null)
  try {
    return parse(JSON.parse(responseText))
  } catch {
    throw new PolicyApiError('服务返回了无法解析的响应', response.status, 'invalid_response')
  }
}

// ---- Parsers ----

function parseAccount(value: unknown): import('./types').PolicyAccount {
  const o = safeObject(value)
  return {
    username: safeString(o.username ?? o.Username),
    group: safeString(o.group),
    policies: safeArray<unknown>(o.policies).map(safeString).filter(Boolean),
    permission: safeString(o.permission) || 'unknown',
    writeAccess: safeBoolean(o.writeAccess),
    error: safeString(o.error) || undefined,
  }
}

function parseEgressFamily(value: unknown): import('./types').PolicyEgressFamily {
  const o = safeObject(value)
  return {
    family: safeString(o.family) as import('./types').PolicyAddressFamily,
    enabled: safeBoolean(o.enabled),
    wanInterface: safeString(o.wanInterface ?? o.WANInterface),
    gateway: safeString(o.gateway ?? o.Gateway),
    routeTable: safeString(o.routeTable ?? o.RouteTable),
    routeMode: safeString(o.routeMode ?? o.RouteMode),
    natMode: safeString(o.natMode ?? o.NATMode),
    wanSource: safeString(o.wanSource ?? o.WANSource),
  }
}

function parseSourceSummary(value: unknown): import('./types').PolicyEgressSourceSummary {
  const o = safeObject(value)
  return {
    id: safeString(o.id),
    name: safeString(o.name),
    type: safeString(o.type),
    enabled: safeBoolean(o.enabled),
    ruleCount: safeNumber(o.ruleCount),
  }
}

function parseEgress(value: unknown): PolicyEgress {
  const o = safeObject(value)
  return {
    id: safeString(o.id ?? o.ID),
    name: safeString(o.name ?? o.Name),
    priority: safeNumber(o.priority ?? o.Priority),
    listMode: safeString(o.listMode ?? o.ListMode),
    listName: safeString(o.listName ?? o.ListName),
    dnsUpstream: safeString(o.dnsUpstream ?? o.DNSUpstream),
    fakeAlias: safeString(o.fakeAlias ?? o.FakeAlias),
    failureMode: safeString(o.failureMode ?? o.FailureMode),
    routerOutput: safeBoolean(o.routerOutput ?? o.RouterOutput),
    enabled: safeBoolean(o.enabled ?? o.Enabled),
    revision: safeNumber(o.revision ?? o.Revision),
    pendingDeletion: safeBoolean(o.pendingDeletion ?? o.PendingDelete),
    applied: safeBoolean(o.applied),
    families: safeArray<unknown>(o.families ?? o.Families).map(parseEgressFamily),
    sources: safeArray<unknown>(o.sources).map((source) => {
      const sourceObject = safeObject(source)
      // Old overview responses embed complete sources.  A summary response
      // is still accepted so this frontend remains compatible with the
      // transitional backend.
      return sourceObject.versions !== undefined || sourceObject.egressId !== undefined || sourceObject.egressID !== undefined
        ? parseSource(source)
        : parseSourceSummary(source)
    }),
    objects: safeArray<unknown>(o.objects ?? o.Objects).map(parseObjectReference),
  }
}

function parseSourceVersion(value: unknown): import('./types').PolicySourceVersion {
  const o = safeObject(value)
  return {
    id: safeString(o.id ?? o.ID),
    sourceId: safeString(o.sourceId ?? o.SourceID),
    sha256: safeString(o.sha256 ?? o.SHA256),
    state: safeString(o.state ?? o.State),
    error: safeString(o.error ?? o.Error),
    httpStatus: safeNumber(o.httpStatus ?? o.HTTPStatus),
    createdAt: safeString(o.createdAt ?? o.CreatedAt),
    counts: numberRecord(o.counts ?? o.Counts),
    diff: safeObject(o.diff ?? o.Diff),
  }
}

function parseSource(value: unknown): PolicySource {
  const o = safeObject(value)
  return {
    id: safeString(o.id ?? o.ID),
    egressId: safeString(o.egressId ?? o.EgressID),
    type: safeString(o.type ?? o.Type),
    kind: safeString(o.kind ?? o.Kind) || 'domain',
    name: safeString(o.name ?? o.Name),
    url: safeString(o.url ?? o.URL),
    schedule: safeString(o.schedule ?? o.Schedule),
    enabled: safeBoolean(o.enabled ?? o.Enabled),
    activeVersionId: safeString(o.activeVersionId ?? o.ActiveVersionID),
    lastGoodVersionId: safeString(o.lastGoodVersionId ?? o.LastGoodVersionID),
    etag: safeString(o.etag ?? o.ETag),
    lastModified: safeString(o.lastModified ?? o.LastModified),
    nextRunAt: safeString(o.nextRunAt ?? o.NextRunAt),
    revision: safeNumber(o.revision ?? o.Revision),
    pendingDeletion: safeBoolean(o.pendingDeletion ?? o.PendingDelete),
    versions: safeArray<unknown>(o.versions ?? o.Versions).map(parseSourceVersion),
    counts: numberRecord(o.counts ?? o.Counts),
  }
}

function parseSourceSaveResult(value: unknown): PolicySourceSaveResult {
  const o = safeObject(value)
  const job = o.job ? parseJob(o.job) : undefined
  return {
    source: parseSource(o.source ?? value),
    job,
    jobId: safeString(o.jobId ?? job?.id),
  }
}

function parsePreviewRule(value: unknown): import('./types').PolicyPreviewRule {
  const o = safeObject(value)
  return { type: safeString(o.type), domain: safeString(o.domain) || undefined, address: safeString(o.address) || undefined }
}

function parsePreview(value: unknown): PolicyPreview {
  const o = safeObject(value)
  return {
    previewId: safeString(o.previewId),
    url: safeString(o.url),
    filename: safeString(o.filename),
    statusCode: safeNumber(o.statusCode),
    contentType: safeString(o.contentType),
    etag: safeString(o.etag),
    lastModified: safeString(o.lastModified),
    notModified: safeBoolean(o.notModified),
    size: safeNumber(o.size),
    sha256: safeString(o.sha256),
    validRules: safeNumber(o.validRules),
    ignored: numberRecord(o.ignored),
    errorSamples: safeArray<unknown>(o.errorSamples).filter((v): v is string => typeof v === 'string'),
    rules: safeArray<unknown>(o.rules).map(parsePreviewRule),
  }
}

function parseWANRoute(value: unknown): import('./types').PolicyWANRoute {
  const o = safeObject(value)
  return {
    id: safeString(o.id),
    family: safeString(o.family),
    destination: safeString(o.destination),
    gateway: safeString(o.gateway),
    immediateGateway: safeString(o.immediateGateway),
    table: safeString(o.table),
    source: safeString(o.source),
    distance: safeNumber(o.distance),
    active: safeBoolean(o.active),
    proven: safeBoolean(o.proven),
  }
}

function parseTrafficIngressCandidate(value: unknown): import('./types').PolicyTrafficIngressCandidate {
  const o = safeObject(value)
  return {
    name: safeString(o.name),
    kind: safeString(o.kind),
    include: safeArray<unknown>(o.include).filter((v): v is string => typeof v === 'string'),
    exclude: safeArray<unknown>(o.exclude).filter((v): v is string => typeof v === 'string'),
    staticMembers: safeArray<unknown>(o.staticMembers).filter((v): v is string => typeof v === 'string'),
    dynamicMembers: safeBoolean(o.dynamicMembers),
    frozen: safeBoolean(o.frozen),
    addresses: safeArray<unknown>(o.addresses).filter((v): v is string => typeof v === 'string'),
    reason: safeString(o.reason),
    coveredBy: safeArray<unknown>(o.coveredBy).filter((v): v is string => typeof v === 'string'),
    default: safeBoolean(o.default),
    dynamic: safeBoolean(o.dynamic),
    running: safeBoolean(o.running),
  }
}

function parseObjectReference(value: unknown): import('./types').PolicyObjectReference {
  const o = safeObject(value)
  return {
    logicalId: safeString(o.logicalId ?? o.logicalID),
    menu: safeString(o.menu),
    routerId: safeString(o.routerId ?? o.routerID),
    ownership: safeString(o.ownership),
  }
}

function parseExistingPolicy(value: unknown): import('./types').PolicyExistingPolicy {
  const o = safeObject(value)
  return {
    ...parseObjectReference(value),
    reason: safeString(o.reason),
    foreignManager: safeBoolean(o.foreignManager),
  }
}

function parseDiscovery(value: unknown): PolicyDiscovery {
  const o = safeObject(value)
  if (!safeBoolean(o.available)) {
    return {
      available: false,
      reason: safeString(o.reason),
      warnings: safeArray<unknown>(o.warnings).filter((item): item is string => typeof item === 'string'),
      wans: [],
      trafficIngress: [],
    }
  }
  const snapshot = safeObject(o.snapshot)
  const device = safeObject(o.device)
  return {
    device: safeString(device.id ?? device.deviceId ?? o.deviceId)
      ? { id: safeString(device.id ?? device.deviceId ?? o.deviceId), name: safeString(device.name ?? o.deviceName) }
      : undefined,
    available: true,
    reason: safeString(o.reason),
    warnings: safeArray<unknown>(o.warnings).filter((item): item is string => typeof item === 'string'),
    snapshot: {
      fingerprint: safeString(snapshot.fingerprint),
      deviceIdentity: stringRecord(snapshot.deviceIdentity),
      capabilities: Object.fromEntries(
        Object.entries(safeObject(snapshot.capabilities)).map(([key, evidence]) => {
          const parsed = safeObject(evidence)
          return [key, {
            capability: safeString(parsed.capability ?? key),
            status: safeString(parsed.status ?? parsed.state),
            reason: safeString(parsed.reason),
            evidence: safeArray<unknown>(parsed.evidence).filter((item): item is string => typeof item === 'string'),
          }]
        }),
      ),
    },
    wans: safeArray<unknown>(o.wans).map((w) => {
      const o = safeObject(w)
      return {
        interface: safeString(o.interface),
        type: safeString(o.type),
        running: safeBoolean(o.running),
        pointToPoint: safeBoolean(o.pointToPoint),
        proven: safeBoolean(o.proven),
        routes: safeArray<unknown>(o.routes).map(parseWANRoute),
      }
    }),
    trafficIngress: safeArray<unknown>(o.trafficIngress ?? o.lan).map(parseTrafficIngressCandidate),
    builtins: safeArray<unknown>(o.builtins).map(parseObjectReference),
    existingPolicy: safeArray<unknown>(o.existingPolicy).map(parseExistingPolicy),
    adoptionCandidates: safeArray<unknown>(o.adoptionCandidates).map(parseExistingPolicy),
  }
}

function parsePlanIssue(value: unknown): import('./types').PolicyPlanIssue {
  const o = safeObject(value)
  return {
    code: safeString(o.code),
    status: safeString(o.status),
    family: safeString(o.family),
    egressID: safeString(o.egressID ?? o.egressId),
    logicalID: safeString(o.logicalID ?? o.logicalId),
    reason: safeString(o.reason),
  }
}

function parsePlanAck(value: unknown): import('./types').PolicyPlanAcknowledgement {
  const o = safeObject(value)
  return {
    code: safeString(o.code),
    required: safeBoolean(o.required),
    accepted: safeBoolean(o.accepted),
  }
}

function parsePlanOperation(value: unknown): import('./types').PolicyPlanOperation {
  const o = safeObject(value)
  const anchor = safeObject(o.anchor)
  return {
    seq: safeNumber(o.seq ?? o.sequence),
    operationID: safeString(o.operationID),
    groupID: safeString(o.groupID),
    egressID: safeString(o.egressID),
    family: safeString(o.family),
    phase: safeString(o.phase),
    action: safeString(o.action),
    menu: safeString(o.menu),
    logicalID: safeString(o.logicalID),
    routerID: safeString(o.routerID),
    ownership: safeString(o.ownership),
    before: valueRecord(o.before),
    after: valueRecord(o.after),
    anchor: Object.keys(anchor).length ? anchor as import('./types').PolicyAnchorDescriptor : undefined,
    verification: safeObject(o.verification) as import('./types').PolicyVerificationDescriptor,
    compensation: safeObject(o.compensation) as import('./types').PolicyCompensationDescriptor,
  }
}

function parsePlan(value: unknown): PolicyPlan {
  const o = safeObject(value)
  const summary = safeObject(o.summary)
  return {
    planID: safeString(o.planID ?? o.planId),
    deviceID: safeString(o.deviceID ?? o.deviceId),
    kind: safeString(o.kind),
    lifecycle: safeString(o.lifecycle),
    createdAt: safeString(o.createdAt),
    expiresAt: safeString(o.expiresAt),
    desiredRevision: safeNumber(o.desiredRevision),
    internetEgressCandidates: (() => {
      const result: Record<string, Array<{ interface: string; type: string; running: boolean; reason?: string }>> = {}
      for (const [family, values] of Object.entries(safeObject(o.internetEgressCandidates))) {
        if (!Array.isArray(values)) continue
        const candidates = values.map((value) => {
          const candidate = safeObject(value)
          return {
            interface: safeString(candidate.interface),
            type: safeString(candidate.type),
            running: safeBoolean(candidate.running),
            reason: safeString(candidate.reason) || undefined,
          }
        }).filter((candidate) => candidate.interface)
        if (candidates.length) result[family] = candidates
      }
      return result
    })(),
    desiredHash: safeString(o.desiredHash),
    actualFingerprint: safeString(o.actualFingerprint),
    capabilities: stringRecord(o.capabilities),
    blockers: safeArray<unknown>(o.blockers).map(parsePlanIssue),
    familyBlockers: safeArray<unknown>(o.familyBlockers).map(parsePlanIssue),
    warnings: safeArray<unknown>(o.warnings).map(parsePlanIssue),
    acknowledgements: safeArray<unknown>(o.acknowledgements).map(parsePlanAck),
    ownershipStrict: safeBoolean(o.ownershipStrict),
    summary: {
      create: safeNumber(summary.create),
      patch: safeNumber(summary.patch),
      delete: safeNumber(summary.delete),
      move: safeNumber(summary.move),
      disable: safeNumber(summary.disable),
      enable: safeNumber(summary.enable),
      referenceAdd: safeNumber(summary.referenceAdd ?? summary.reference_add),
      referenceRemove: safeNumber(summary.referenceRemove ?? summary.reference_remove),
      reuse: safeNumber(summary.reuse),
      adopt: safeNumber(summary.adopt),
      blockers: safeNumber(summary.blockers),
      warnings: safeNumber(summary.warnings),
      familyBlockers: safeNumber(summary.familyBlockers ?? summary.family_blockers),
    },
    resourceEstimate: safeObject(o.resourceEstimate) as import('./types').PolicyResourceEstimate,
    pendingReview: safeBoolean(o.pendingReview),
    operations: safeArray<unknown>(o.operations).map(parsePlanOperation),
    executionGroups: safeArray<unknown>(o.executionGroups).map((g) => {
      const o = safeObject(g)
      return {
        id: safeString(o.id),
        role: safeString(o.role),
        egressID: safeString(o.egressID),
        family: safeString(o.family),
        operationSeqs: safeArray<unknown>(o.operationSeqs).map((v) => safeNumber(v)),
      }
    }),
    planHash: safeString(o.planHash),
    state: safeString(o.state),
  }
}

function parsePlanEnvelope(value: unknown): PolicyPlanEnvelope {
  const o = safeObject(value)
  const plan = parsePlan(o.plan ?? o)
  return {
    plan,
    planId: safeString(o.planId ?? plan.planID),
    planHash: safeString(o.planHash ?? plan.planHash),
    readOnly: safeBoolean(o.readOnly),
  }
}

function parseJobStep(value: unknown): import('./types').PolicyJobStep {
  const o = safeObject(value)
  return {
    sequence: safeNumber(o.sequence ?? o.seq),
    action: safeString(o.action),
    target: safeString(o.target),
    routerId: safeString(o.routerId),
    status: safeString(o.status),
    attempt: safeNumber(o.attempt),
    error: safeString(o.error),
    updatedAt: safeString(o.updatedAt),
  }
}

function parseJob(value: unknown): import('./types').PolicyJob {
  const o = safeObject(value)
  return {
    id: safeString(o.id),
    planId: safeString(o.planId),
    acknowledgements: safeArray<unknown>(o.acknowledgements).filter((v): v is string => typeof v === 'string'),
    stepUpApproved: safeBoolean(o.stepUpApproved),
    state: safeString(o.state),
    phase: safeString(o.phase),
    progress: safeNumber(o.progress),
    cancelRequested: safeBoolean(o.cancelRequested),
    error: safeString(o.error),
    primaryError: safeString(o.primaryError),
    rollbackError: safeString(o.rollbackError),
    failedOperation: safeString(o.failedOperation),
    createdAt: safeString(o.createdAt),
    startedAt: safeString(o.startedAt),
    finishedAt: safeString(o.finishedAt),
    steps: safeArray<unknown>(o.steps).map(parseJobStep),
    recoveryChoices: safeArray<unknown>(o.recoveryChoices).filter((v): v is string => typeof v === 'string'),
    partialResult: o.partialResult ? safeObject(o.partialResult) as import('./types').PolicyPartialResult : undefined,
  }
}

function parseTrafficIngress(value: unknown): import('./types').PolicyTrafficIngressScope {
  const o = safeObject(value)
  const values = (key: string) => safeArray<unknown>(o[key]).filter((item): item is string => typeof item === 'string' && Boolean(item.trim()))
  if (Object.prototype.hasOwnProperty.call(o, 'interfaceLists')) {
    return { interfaceLists: values('interfaceLists'), interfaces: values('interfaces') }
  }
  const legacyInterfaces = values('interfaces')
  const legacyList = safeString(o.interfaceList ?? o.listName ?? o.lanScope)
  return { interfaceLists: legacyInterfaces.length ? legacyInterfaces : legacyList ? [legacyList] : [], interfaces: [] }
}

function parseOverview(value: unknown): PolicyOverview {
  const o = safeObject(value)
  const device = safeObject(o.device)
  const account = parseAccount(o.account)
  const setup = safeObject(o.setup)
  const health = safeObject(o.health)
  const drift = safeObject(o.drift)
  return {
    device: {
      id: safeString(device.id),
      name: safeString(device.name),
      enabled: safeBoolean(device.enabled),
      archived: safeBoolean(device.archived),
    },
    account,
    setup: {
      state: (safeString(setup.state) || 'runtime_unavailable') as import('./types').PolicySetupState,
      managerAvailable: safeBoolean(setup.managerAvailable),
    },
    capability: o.capability ? parseRuntimeCapability(o.capability) : undefined,
    trafficIngress: parseTrafficIngress(o.trafficIngress ?? o.lanScope),
    health: {
      state: safeString(health.state),
      driftState: safeString(health.driftState),
      mutationPaused: safeBoolean(health.mutationPaused),
      manualInterventionRequired: safeBoolean(health.manualInterventionRequired),
      pauseReason: safeString(health.pauseReason),
      pauseJobId: safeString(health.pauseJobId),
    },
    drift: {
      state: safeString(drift.state),
      items: safeArray<unknown>(drift.items).map((item) => {
        const o = safeObject(item)
        return {
          code: safeString(o.code),
          status: safeString(o.status),
          family: safeString(o.family),
          egressID: safeString(o.egressID),
          logicalID: safeString(o.logicalID),
          reason: safeString(o.reason),
        }
      }),
    },
    egresses: safeArray<unknown>(o.egresses).map(parseEgress),
    sources: safeArray<unknown>(o.sources).map(parseSource),
    activeJobs: safeArray<unknown>(o.activeJobs).map(parseJob),
    pendingJobs: safeArray<unknown>(o.pendingJobs).map(parseJob),
    applied: safeBoolean(o.applied),
  }
}

function parseRulesPage(value: unknown): PolicyRulesPage {
  const o = safeObject(value)
  return {
    sourceId: safeString(o.sourceId),
    versionId: safeString(o.versionId),
    rules: safeArray<unknown>(o.rules).map(parsePreviewRule),
    nextCursor: safeString(o.nextCursor),
  }
}

function parseAuditPage(value: unknown): PolicyAuditPage {
  const o = safeObject(value)
  return {
    entries: safeArray<unknown>(o.entries).map((entry) => {
      const o = safeObject(entry)
      return {
        id: safeString(o.id),
        actor: safeString(o.actor),
        remoteIp: safeString(o.remoteIp),
        action: safeString(o.action),
        objectId: safeString(o.objectId),
        planId: safeString(o.planId),
        planHash: safeString(o.planHash),
        jobId: safeString(o.jobId),
        versionId: safeString(o.versionId),
        result: safeString(o.result),
        summary: safeString(o.summary),
        createdAt: safeString(o.createdAt),
      }
    }),
    nextCursor: safeString(o.nextCursor),
  }
}

function parseRuntimeCapability(value: unknown): import('./types').PolicyRuntimeCapability {
  const o = safeObject(value)
  const rawEntries = safeObject(o.entries ?? o.capabilities)
  return {
    state: safeString(o.state),
    reason: safeString(o.reason),
    version: safeString(o.version),
    entries: Object.fromEntries(
      Object.entries(rawEntries).map(([key, evidence]) => {
        const parsed = safeObject(evidence)
        return [key, {
          capability: safeString(parsed.capability ?? key),
          status: safeString(parsed.status ?? parsed.state),
          reason: safeString(parsed.reason),
          evidence: safeArray<unknown>(parsed.evidence).filter((item): item is string => typeof item === 'string'),
        }]
      }),
    ),
  }
}

// ---- Public API functions ----

export function fetchOverview(deviceID: string, signal?: AbortSignal | null): Promise<PolicyOverview> {
  return policyFetch(deviceID, '/overview', { signal }, parseOverview)
}

export function fetchDiscovery(deviceID: string, signal?: AbortSignal | null): Promise<PolicyDiscovery> {
  return policyFetch(deviceID, '/discovery', { signal }, parseDiscovery)
}

export function fetchEgress(deviceID: string, id: string, signal?: AbortSignal | null): Promise<PolicyEgress> {
  return policyFetch(deviceID, `/egresses/${encodeURIComponent(id)}`, { signal }, parseEgress)
}

export function fetchSource(deviceID: string, id: string, signal?: AbortSignal | null): Promise<PolicySource> {
  return policyFetch(deviceID, `/sources/${encodeURIComponent(id)}`, { signal }, parseSource)
}

export function saveTrafficIngress(
  deviceID: string,
  trafficIngress: import('./types').PolicyTrafficIngressScope,
  signal?: AbortSignal | null,
): Promise<import('./types').PolicyTrafficIngressScope> {
  return policyFetch(deviceID, '/traffic-ingress', { method: 'PUT', body: { trafficIngress }, signal }, (value) => {
    const o = safeObject(value)
    return parseTrafficIngress(o.trafficIngress ?? value)
  })
}

export function saveEgress(deviceID: string, draft: PolicyEgressDraft, signal?: AbortSignal | null): Promise<PolicyEgress> {
  const method = draft.id ? 'PUT' : 'POST'
  const path = draft.id ? `/egresses/${encodeURIComponent(draft.id)}` : '/egresses'
  return policyFetch(deviceID, path, { method, body: draft, signal }, parseEgress)
}

function parseApplyResult(value: unknown): PolicyApplyResult {
  const o = safeObject(value)
  return {
    ...o,
    job: o.job ? parseJob(o.job) : undefined,
    jobId: safeString(o.jobId ?? safeObject(o.job).id),
  }
}

export function setEgressEnabled(deviceID: string, id: string, enabled: boolean, revision: number, signal?: AbortSignal | null): Promise<PolicyApplyResult> {
  return policyFetch(deviceID, `/egresses/${encodeURIComponent(id)}/state`, { method: 'POST', body: { enabled, revision }, signal }, parseApplyResult)
}

export function deleteEgress(deviceID: string, id: string, revision: number, signal?: AbortSignal | null): Promise<PolicyApplyResult> {
  return policyFetch(deviceID, `/egresses/${encodeURIComponent(id)}?revision=${revision}`, { method: 'DELETE', signal }, parseApplyResult)
}

export function saveSource(
  deviceID: string,
  draft: PolicySourceDraft,
  extra?: { previewId?: string; deferApply?: boolean },
  signal?: AbortSignal | null,
): Promise<PolicySourceSaveResult> {
  const method = draft.id ? 'PUT' : 'POST'
  const path = draft.id ? `/sources/${encodeURIComponent(draft.id)}` : '/sources'
  const body = {
    ...draft,
    ...(extra?.previewId ? { previewId: extra.previewId } : {}),
    ...(extra?.deferApply ? { deferApply: true } : {}),
  }
  return policyFetch(deviceID, path, { method, body, signal }, parseSourceSaveResult)
}

export function deleteSource(deviceID: string, id: string, revision: number, signal?: AbortSignal | null): Promise<PolicyApplyResult> {
  return policyFetch(deviceID, `/sources/${encodeURIComponent(id)}?revision=${revision}`, { method: 'DELETE', signal }, parseApplyResult)
}

export function fetchSourceRules(
  deviceID: string,
  id: string,
  options: { cursor?: string; limit?: number; query?: string; type?: string; version?: 'active' | 'pending' },
  signal?: AbortSignal | null,
): Promise<PolicyRulesPage> {
  const params = new URLSearchParams()
  if (options.limit) params.set('limit', String(options.limit))
  if (options.cursor) params.set('cursor', options.cursor)
  if (options.query) params.set('query', options.query)
  if (options.type) params.set('type', options.type)
  if (options.version) params.set('version', options.version)
  const qs = params.toString()
  return policyFetch(deviceID, `/sources/${encodeURIComponent(id)}/rules${qs ? `?${qs}` : ''}`, { signal }, parseRulesPage)
}

export function fetchJob(deviceID: string, id: string, signal?: AbortSignal | null): Promise<PolicyJob> {
  return policyFetch(deviceID, `/jobs/${encodeURIComponent(id)}`, { signal }, (value) => parseJob(safeObject(value).job ?? value))
}

export async function waitForPolicyJob(deviceID: string, id: string): Promise<PolicyJob> {
  for (;;) {
    const job = await fetchJob(deviceID, id)
    if (job.state === 'committed') return job
    if (['cancelled_before_write', 'committed_partial', 'failed', 'needs_decision', 'rolled_back', 'rollback_failed'].includes(job.state)) {
      throw new Error(job.primaryError || job.error || 'RouterOS 同步失败')
    }
    await new Promise((resolve) => window.setTimeout(resolve, 500))
  }
}

export function cancelJob(deviceID: string, id: string, signal?: AbortSignal | null): Promise<PolicyJob> {
  return policyFetch(deviceID, `/jobs/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: {}, signal }, (value) => parseJob(safeObject(value).job ?? value))
}

export function resumeJob(deviceID: string, id: string, signal?: AbortSignal | null): Promise<PolicyJob> {
  return policyFetch(deviceID, `/jobs/${encodeURIComponent(id)}/resume`, { method: 'POST', body: {}, signal }, (value) => parseJob(safeObject(value).job ?? value))
}

export function rollbackJob(deviceID: string, id: string, signal?: AbortSignal | null): Promise<PolicyJob> {
  return policyFetch(deviceID, `/jobs/${encodeURIComponent(id)}/rollback`, { method: 'POST', body: {}, signal }, (value) => parseJob(safeObject(value).job ?? value))
}

export function previewURL(deviceID: string, body: { url: string; etag?: string; lastModified?: string; kind?: string }, signal?: AbortSignal | null): Promise<PolicyPreview> {
  return policyFetch(deviceID, '/sources/url/preview', { method: 'POST', body, signal }, parsePreview)
}

export function previewUpload(deviceID: string, fileOrFormData: File | FormData, kind?: string, signal?: AbortSignal | null): Promise<PolicyPreview> {
  const formData = fileOrFormData instanceof FormData ? fileOrFormData : (() => {
    const value = new FormData()
    value.append('file', fileOrFormData, fileOrFormData.name)
    return value
  })()
  const qs = kind ? `?kind=${encodeURIComponent(kind)}` : ''
  return policyFetch(deviceID, `/sources/upload/preview${qs}`, { method: 'POST', formData, signal }, parsePreview)
}

export function previewManual(deviceID: string, body: { text: string; kind?: string }, signal?: AbortSignal | null): Promise<PolicyPreview> {
  return policyFetch(deviceID, '/sources/manual/preview', { method: 'POST', body, signal }, parsePreview)
}

export function generatePlan(deviceID: string, kind: string, signal?: AbortSignal | null): Promise<PolicyPlanEnvelope> {
  return policyFetch(deviceID, '/plans', { method: 'POST', body: { kind }, signal }, parsePlanEnvelope)
}

export function generateDriftPlan(deviceID: string, signal?: AbortSignal | null): Promise<PolicyPlanEnvelope> {
  return policyFetch(deviceID, '/drift/plan', { method: 'POST', body: {}, signal }, parsePlanEnvelope)
}

export function generateAdoptionPlan(deviceID: string, items: unknown, signal?: AbortSignal | null): Promise<PolicyPlanEnvelope> {
  return policyFetch(deviceID, '/adoption/preview', { method: 'POST', body: { objects: items }, signal }, parsePlanEnvelope)
}

export function generateTakeoverPlan(deviceID: string, items: unknown, signal?: AbortSignal | null): Promise<PolicyPlanEnvelope> {
  return policyFetch(deviceID, '/takeover/preview', { method: 'POST', body: { objects: items }, signal }, parsePlanEnvelope)
}

export function applyPlan(
  deviceID: string,
  planId: string,
  body: { acknowledgements: string[] },
  signal?: AbortSignal | null,
): Promise<PolicyApplyResult> {
  return policyFetch(deviceID, `/plans/${encodeURIComponent(planId)}/apply`, { method: 'POST', body, signal }, parseApplyResult)
}

export function fetchAudit(deviceID: string, options: { cursor?: string; limit?: number }, signal?: AbortSignal | null): Promise<PolicyAuditPage> {
  const params = new URLSearchParams()
  if (options.limit) params.set('limit', String(options.limit))
  if (options.cursor) params.set('cursor', options.cursor)
  const qs = params.toString()
  return policyFetch(deviceID, `/audit${qs ? `?${qs}` : ''}`, { signal }, parseAuditPage)
}
