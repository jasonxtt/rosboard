/**
 * Supplementary typed client for the policy feature.
 *
 * canonical.ts is the preserved contract layer and stays untouched; this
 * module adds only the endpoints/fields it does not expose (overview meta,
 * job snapshots, egress state/delete, access-control sync + createdAt,
 * target-list nextRunAt/rules pagination). Components never fetch raw.
 */
import {
  CanonicalPolicyError,
  type AccessOverview,
  type AccessRule,
  type PolicyTerminal,
  type Subject,
  type TargetList,
  type TargetListRule,
} from './canonical.ts'
import { alwaysAccessSchedule, normalizeAccessSchedule, type AccessTimeWindow, type AccessWeekday } from './schedule.ts'
import { apiGet, safeArray, safeObject, scoped } from '../../lib/api.ts'
import { parseInterfaceStatus } from '../../lib/types.ts'

export type PolicyJob = { id: string; state: string; phase: string; progress: number; error?: string }

export type InternetEgressCandidate = { interface: string; type: string; running: boolean; reason?: string }
export type InternetEgressCandidates = Record<string, InternetEgressCandidate[]>

export class PolicyApiError extends CanonicalPolicyError {
  details: unknown
  constructor(message: string, status: number, code?: string, details?: unknown) {
    super(message, status, code)
    this.name = 'PolicyApiError'
    this.details = details
  }
}

function objectValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : {}
}
function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}
function numberValue(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}
function booleanValue(value: unknown): boolean {
  return value === true
}
function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

async function requestPolicy<T>(path: string, init: RequestInit | undefined, parse: (value: unknown) => T): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, { credentials: 'same-origin', ...init })
  } catch {
    throw new PolicyApiError('网络请求失败，请检查面板连接', 0, 'network_error')
  }
  if (response.status === 401) window.dispatchEvent(new Event('rosboard:authentication-required'))
  const text = await response.text()
  let payload: unknown = null
  if (text.trim()) {
    try {
      payload = JSON.parse(text)
    } catch {
      throw new PolicyApiError('服务返回了无法解析的响应', response.status, 'invalid_response')
    }
  }
  if (!response.ok) {
    const failure = objectValue(payload)
    throw new PolicyApiError(stringValue(failure.error) || `HTTP ${response.status}`, response.status, stringValue(failure.code) || undefined, payload)
  }
  return parse(payload)
}

function jsonInit(method: string, body: unknown): RequestInit {
  return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }
}

/** jobId from any mutation envelope ({jobId} and/or {job:{id}}). */
export function jobIdOf(result: { jobId?: string; job?: { id: string } } | null | undefined): string {
  return result?.jobId || result?.job?.id || ''
}

/* ---------- jobs ---------- */

function parsePolicyJob(value: unknown): PolicyJob {
  const object = objectValue(objectValue(value).job ?? value)
  return {
    id: stringValue(object.id),
    state: stringValue(object.state),
    phase: stringValue(object.phase),
    progress: numberValue(object.progress),
    error: stringValue(object.error) || undefined,
  }
}

export function fetchPolicyJob(deviceID: string, jobID: string) {
  return requestPolicy(scoped(`/api/policy-routing/jobs/${encodeURIComponent(jobID)}`, deviceID), { cache: 'no-store' }, parsePolicyJob)
}
export function fetchAccessJob(deviceID: string, jobID: string) {
  return requestPolicy(`/api/access-control/devices/${encodeURIComponent(deviceID)}/jobs/${encodeURIComponent(jobID)}`, { cache: 'no-store' }, parsePolicyJob)
}

/** Terminal states that end job polling (committed is the only success). */
export const JOB_FAILED_STATES: ReadonlySet<string> = new Set(['failed', 'committed_partial', 'needs_decision', 'rolled_back', 'rollback_failed'])
export function jobIsTerminal(state: string): boolean {
  return state === 'committed' || JOB_FAILED_STATES.has(state)
}

/* ---------- policy-routing overview meta ---------- */

export type PolicyOverviewMeta = {
  deviceName: string
  deviceEnabled: boolean
  account: { username: string; permission: string; writeAccess: boolean; group?: string; error?: string }
  setupState: string
  activeJobs: PolicyJob[]
  health: { state: string; driftState: string; mutationPaused: boolean; manualInterventionRequired: boolean; pauseReason: string }
  driftState: string
  applied: boolean
}

export function fetchPolicyOverviewMeta(deviceID: string): Promise<PolicyOverviewMeta> {
  return requestPolicy(scoped('/api/policy-routing/overview', deviceID), { cache: 'no-store' }, (value) => {
    const object = objectValue(value)
    const device = objectValue(object.device)
    const account = objectValue(object.account)
    const setup = objectValue(object.setup)
    const health = objectValue(object.health)
    const drift = objectValue(object.drift)
    return {
      deviceName: stringValue(device.name),
      deviceEnabled: booleanValue(device.enabled),
      account: {
        username: stringValue(account.username),
        permission: stringValue(account.permission),
        writeAccess: booleanValue(account.writeAccess),
        group: stringValue(account.group) || undefined,
        error: stringValue(account.error) || undefined,
      },
      setupState: stringValue(setup.state),
      activeJobs: safeArray<unknown>(object.activeJobs).map(parsePolicyJob).filter((job) => job.id),
      health: {
        state: stringValue(health.state),
        driftState: stringValue(health.driftState),
        mutationPaused: booleanValue(health.mutationPaused),
        manualInterventionRequired: booleanValue(health.manualInterventionRequired),
        pauseReason: stringValue(health.pauseReason),
      },
      driftState: stringValue(drift.state),
      applied: booleanValue(object.applied),
    }
  })
}

/* ---------- egress state / delete (not covered by canonical) ---------- */

export function setEgressEnabled(deviceID: string, id: string, revision: number, enabled: boolean) {
  return requestPolicy(scoped(`/api/policy-routing/egresses/${encodeURIComponent(id)}/state`, deviceID), jsonInit('POST', { enabled, revision }), (value) => {
    const object = objectValue(value)
    return { jobId: stringValue(object.jobId) || undefined, job: object.job ? { id: stringValue(objectValue(object.job).id) } : undefined }
  })
}

export function deleteEgress(deviceID: string, id: string, revision: number) {
  return requestPolicy(scoped(`/api/policy-routing/egresses/${encodeURIComponent(id)}?revision=${revision}`, deviceID), { method: 'DELETE' }, (value) => {
    const object = objectValue(value)
    return {
      deleted: booleanValue(object.deleted),
      pendingDeletion: booleanValue(object.pendingDeletion),
      jobId: stringValue(object.jobId) || undefined,
      job: object.job ? { id: stringValue(objectValue(object.job).id) } : undefined,
    }
  })
}

/* ---------- access control (detail incl. createdAt + sync) ---------- */

export type AccessRuleDetail = AccessRule & { createdAt: string }
export type AccessOverviewDetail = Omit<AccessOverview, 'rules' | 'job' | 'state'> & {
  rules: AccessRuleDetail[]
  job?: PolicyJob
  state: { desiredRevision: number; appliedRevision: number; appliedAt: string }
}

function parseSubject(value: unknown): Subject {
  const object = objectValue(value)
  const rawMode = stringValue(object.mode)
  const mode = rawMode === 'all' || rawMode === 'excluded' ? rawMode : 'selected'
  return {
    mode,
    members: safeArray<unknown>(object.members).map((raw) => {
      const member = objectValue(raw)
      return {
        terminalId: stringValue(member.terminalId),
        binding: stringValue(member.binding) === 'fixed' ? ('fixed' as const) : ('auto' as const),
        pinnedIpv4: stringArray(member.pinnedIpv4),
        pinnedIpv6: stringArray(member.pinnedIpv6),
      }
    }),
    prefixes: stringArray(object.prefixes),
  }
}

function parseAccessSchedule(value: unknown) {
  const object = objectValue(value)
  const windows = safeArray<unknown>(object.windows).map((raw): AccessTimeWindow => {
    const window = objectValue(raw)
    return {
      days: stringArray(window.days).filter((day): day is AccessWeekday => ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].includes(day)),
      start: stringValue(window.start),
      end: stringValue(window.end),
    }
  })
  return normalizeAccessSchedule({ mode: stringValue(object.mode) === 'weekly' ? 'weekly' : 'always', windows })
}

function parseAccessRuleDetail(value: unknown): AccessRuleDetail {
  const object = objectValue(value)
  return {
    id: stringValue(object.id),
    name: stringValue(object.name),
    subject: parseSubject(object.subject),
    targetScope: stringValue(object.targetScope) === 'targets' ? 'targets' : 'internet',
    targetListIds: stringArray(object.targetListIds),
    schedule: object.schedule ? parseAccessSchedule(object.schedule) : alwaysAccessSchedule(),
    enabled: booleanValue(object.enabled),
    revision: numberValue(object.revision),
    createdAt: stringValue(object.createdAt),
    members: safeArray<unknown>(object.members).map((raw) => {
      const member = objectValue(raw)
      return {
        terminalId: stringValue(member.terminalId),
        binding: stringValue(member.binding) === 'fixed' ? ('fixed' as const) : ('auto' as const),
        state: stringValue(member.state),
        ipv4: stringArray(member.ipv4),
        ipv6: stringArray(member.ipv6),
        reason: stringValue(member.reason) || undefined,
      }
    }),
    status: stringValue(object.status),
    issues: stringArray(object.issues),
  }
}

export function fetchAccessOverviewDetail(deviceID: string): Promise<AccessOverviewDetail> {
  return requestPolicy(`/api/access-control/devices/${encodeURIComponent(deviceID)}`, { cache: 'no-store' }, (value) => {
    const object = objectValue(value)
    const state = objectValue(object.state)
    const device = objectValue(object.device)
    return {
      device: { id: stringValue(device.id), name: stringValue(device.name), enabled: booleanValue(device.enabled) },
      rules: safeArray<unknown>(object.rules).map(parseAccessRuleDetail),
      terminals: safeArray<unknown>(object.terminals).map((raw): PolicyTerminal => {
        const terminal = objectValue(raw)
        return {
          id: stringValue(terminal.id),
          displayName: stringValue(terminal.displayName),
          macAddress: stringValue(terminal.macAddress),
          ipv4: stringArray(terminal.ipv4),
          ipv6: stringArray(terminal.ipv6),
          routingIpv4: stringArray(terminal.routingIpv4),
          routingIpv6: stringArray(terminal.routingIpv6),
          autoEligible: booleanValue(terminal.autoEligible),
        }
      }),
      targetLists: safeArray<unknown>(object.targetLists).map((raw): TargetList => {
        const target = objectValue(raw)
        const usage = objectValue(target.usage)
        return {
          id: stringValue(target.id),
          name: stringValue(target.name),
          kind: stringValue(target.kind) === 'ip' ? 'ip' : 'domain',
          sourceType: stringValue(target.sourceType ?? target.type),
          presetId: stringValue(target.presetId) || undefined,
          url: stringValue(target.url) || undefined,
          schedule: stringValue(target.schedule),
          enabled: booleanValue(target.enabled),
          activeVersionId: stringValue(target.activeVersionId),
          pendingVersionId: stringValue(target.pendingVersionId) || undefined,
          revision: numberValue(target.revision),
          pendingDeletion: booleanValue(target.pendingDeletion),
          counts: objectValue(target.counts) as Record<string, number>,
          usage: { routingRuleCount: numberValue(usage.routingRuleCount), accessRuleCount: numberValue(usage.accessRuleCount) },
          versions: safeArray<unknown>(target.versions).map((versionRaw) => {
            const version = objectValue(versionRaw)
            return { id: stringValue(version.id), state: stringValue(version.state), counts: objectValue(version.counts) as Record<string, number>, createdAt: stringValue(version.createdAt) }
          }),
        }
      }),
      state: { desiredRevision: numberValue(state.desiredRevision), appliedRevision: numberValue(state.appliedRevision), appliedAt: stringValue(state.appliedAt) },
      job: object.job ? parsePolicyJob(object.job) : undefined,
      boundary: stringValue(object.boundary),
    }
  })
}

/** POST /devices/{id}/sync — applies desired access state; may 422 with internetEgressCandidates. */
export function syncAccessControl(deviceID: string, internetEgresses?: Record<string, string[]>) {
  return requestPolicy(`/api/access-control/devices/${encodeURIComponent(deviceID)}/sync`, jsonInit('POST', internetEgresses ? { internetEgresses } : {}), (value) => {
    const object = objectValue(value)
    return { jobId: stringValue(object.jobId) || undefined, job: object.job ? { id: stringValue(objectValue(object.job).id) } : undefined }
  })
}

/** Extract internetEgressCandidates from any policy-layer error (canonical or api). */
export function internetEgressCandidatesOf(error: unknown): InternetEgressCandidates | null {
  if (!(error instanceof CanonicalPolicyError)) return null
  const details = objectValue(error.details)
  const raw = objectValue(details.internetEgressCandidates)
  const result: InternetEgressCandidates = {}
  for (const [family, list] of Object.entries(raw)) {
    const candidates = safeArray<unknown>(list)
      .map((item) => {
        const candidate = objectValue(item)
        return {
          interface: stringValue(candidate.interface),
          type: stringValue(candidate.type),
          running: booleanValue(candidate.running),
          reason: stringValue(candidate.reason) || undefined,
        }
      })
      .filter((candidate) => candidate.interface)
    if (candidates.length) result[family] = candidates
  }
  return Object.keys(result).length ? result : null
}

/**
 * Partial-success markers on a failed mutation envelope. The backend commits
 * desired state before applying (routing rules; access deletes), so an apply
 * failure can still mean "the change is saved — only RouterOS sync failed".
 * `deleted` additionally means the object is already gone from desired state:
 * the UI must not offer a second delete, only a sync recovery.
 */
export function mutationPartialStateOf(error: unknown): { desiredSaved: boolean; deleted: boolean } {
  if (!(error instanceof CanonicalPolicyError)) return { desiredSaved: false, deleted: false }
  const details = objectValue(error.details)
  return { desiredSaved: details.desiredSaved === true, deleted: details.deleted === true }
}

/* ---------- target lists (entries incl. nextRunAt + rules pages) ---------- */

export function deleteTargetListEntry(deviceID: string, id: string, revision: number) {
  return requestPolicy(scoped(`/api/target-lists/${encodeURIComponent(id)}?revision=${revision}`, deviceID), { method: 'DELETE' }, (value) => {
    const object = objectValue(value)
    return {
      deleted: booleanValue(object.deleted),
      pendingDeletion: booleanValue(object.pendingDeletion),
      jobId: stringValue(object.jobId) || undefined,
      job: object.job ? { id: stringValue(objectValue(object.job).id) } : undefined,
    }
  })
}

export type TargetListEntry = TargetList & { nextRunAt: string }

export function fetchTargetListEntries(deviceID: string): Promise<TargetListEntry[]> {
  return requestPolicy(scoped('/api/target-lists?includePreset=true', deviceID), { cache: 'no-store' }, (value) =>
    safeArray<unknown>(objectValue(value).targetLists).map((raw): TargetListEntry => {
      const target = objectValue(raw)
      const usage = objectValue(target.usage)
      return {
        id: stringValue(target.id),
        name: stringValue(target.name),
        kind: stringValue(target.kind) === 'ip' ? 'ip' : 'domain',
        sourceType: stringValue(target.sourceType ?? target.type),
        presetId: stringValue(target.presetId) || undefined,
        url: stringValue(target.url) || undefined,
        schedule: stringValue(target.schedule),
        enabled: booleanValue(target.enabled),
        activeVersionId: stringValue(target.activeVersionId),
        pendingVersionId: stringValue(target.pendingVersionId) || undefined,
        revision: numberValue(target.revision),
        pendingDeletion: booleanValue(target.pendingDeletion),
        counts: objectValue(target.counts) as Record<string, number>,
        usage: { routingRuleCount: numberValue(usage.routingRuleCount), accessRuleCount: numberValue(usage.accessRuleCount) },
        versions: safeArray<unknown>(target.versions).map((versionRaw) => {
          const version = objectValue(versionRaw)
          return { id: stringValue(version.id), state: stringValue(version.state), counts: objectValue(version.counts) as Record<string, number>, createdAt: stringValue(version.createdAt) }
        }),
        nextRunAt: stringValue(target.nextRunAt),
      }
    }),
  )
}

export type TargetListRulesPage = { versionId: string; rules: TargetListRule[]; nextCursor: string }

export function fetchTargetListRules(deviceID: string, id: string, options: { limit?: number; cursor?: string; query?: string } = {}): Promise<TargetListRulesPage> {
  const params = new URLSearchParams()
  params.set('limit', String(options.limit ?? 100))
  if (options.cursor) params.set('cursor', options.cursor)
  if (options.query) params.set('query', options.query)
  return requestPolicy(scoped(`/api/target-lists/${encodeURIComponent(id)}/rules?${params.toString()}`, deviceID), { cache: 'no-store' }, (value) => {
    const object = objectValue(value)
    return {
      versionId: stringValue(object.versionId),
      nextCursor: stringValue(object.nextCursor),
      rules: safeArray<unknown>(object.rules).map((raw) => {
        const rule = objectValue(raw)
        return { type: stringValue(rule.type), domain: stringValue(rule.domain) || undefined, address: stringValue(rule.address) || undefined }
      }),
    }
  })
}

/* ---------- interface live rates (for egress cards) ---------- */

export type InterfaceRate = { downloadBps: number; uploadBps: number; running: boolean }

export async function fetchInterfaceRates(deviceID: string): Promise<Map<string, InterfaceRate>> {
  const payload = await apiGet(scoped('/api/interfaces', deviceID))
  const rates = new Map<string, InterfaceRate>()
  for (const raw of safeArray<unknown>(safeObject(payload).interfaces)) {
    const status = parseInterfaceStatus(raw)
    if (status.name) rates.set(status.name, { downloadBps: status.currentRxBps, uploadBps: status.currentTxBps, running: status.running })
  }
  return rates
}
