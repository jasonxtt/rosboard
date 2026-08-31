import type { AccessApplyResult, AccessEgressCandidate, AccessEgressCandidates, AccessJob, AccessOverview, AccessRuleInput } from './types'

export class AccessApiError extends Error {
  status: number
  code?: string
  deleted: boolean
  desiredSaved: boolean
  internetEgressCandidates: AccessEgressCandidates

  constructor(message: string, status: number, code?: string, payload?: unknown) {
    super(message)
    this.name = 'AccessApiError'
    this.status = status
    this.code = code
    const object = objectValue(payload)
    this.deleted = booleanValue(object.deleted)
    this.desiredSaved = booleanValue(object.desiredSaved)
    this.internetEgressCandidates = egressCandidatesValue(object.internetEgressCandidates)
  }
}

function objectValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function numberValue(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return 0
}

function booleanValue(value: unknown): boolean {
  return value === true
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

function egressCandidatesValue(value: unknown): AccessEgressCandidates {
  const object = objectValue(value)
  const result: AccessEgressCandidates = {}
  for (const [family, rawCandidates] of Object.entries(object)) {
    if (!Array.isArray(rawCandidates)) continue
    const candidates: AccessEgressCandidate[] = []
    for (const rawCandidate of rawCandidates) {
      const candidate = objectValue(rawCandidate)
      const interfaceName = stringValue(candidate.interface)
      if (!interfaceName) continue
      candidates.push({
        interface: interfaceName,
        type: stringValue(candidate.type),
        running: booleanValue(candidate.running),
        reason: stringValue(candidate.reason) || undefined,
      })
    }
    if (candidates.length) result[family] = candidates
  }
  return result
}

function parseJob(value: unknown): AccessJob {
  const object = objectValue(value)
  return {
    id: stringValue(object.id), state: stringValue(object.state), phase: stringValue(object.phase),
    progress: numberValue(object.progress), error: stringValue(object.error) || undefined,
  }
}

function parseRuleMember(value: unknown) {
  const object = objectValue(value)
  const binding = stringValue(object.binding)
  return {
    terminalId: stringValue(object.terminalId), binding: binding === 'fixed' ? 'fixed' as const : 'auto' as const,
    state: stringValue(object.state) as 'resolved' | 'temporarily_unresolved' | 'conflicted',
    ipv4: stringArray(object.ipv4), ipv6: stringArray(object.ipv6), reason: stringValue(object.reason) || undefined,
  }
}

function parseRule(value: unknown) {
  const object = objectValue(value)
  const targetScope = stringValue(object.targetScope)
  return {
    id: stringValue(object.id), name: stringValue(object.name),
    targetScope: targetScope === 'sources' ? 'sources' as const : 'internet' as const,
    sourceIds: stringArray(object.sourceIds), enabled: booleanValue(object.enabled), revision: numberValue(object.revision),
    createdAt: stringValue(object.createdAt), updatedAt: stringValue(object.updatedAt),
    members: Array.isArray(object.members) ? object.members.map(parseRuleMember) : [],
    status: stringValue(object.status) as 'applied' | 'applying' | 'pending' | 'failed' | 'degraded' | 'disabled',
    issues: stringArray(object.issues),
  }
}

function parseTerminal(value: unknown) {
  const object = objectValue(value)
  return {
    id: stringValue(object.id), displayName: stringValue(object.displayName),
    ipv4: stringArray(object.ipv4), ipv6: stringArray(object.ipv6), autoEligible: booleanValue(object.autoEligible),
  }
}

function parseSourceVersion(value: unknown) {
  const object = objectValue(value)
  return { id: stringValue(object.id), state: stringValue(object.state) }
}

function parseSource(value: unknown) {
  const object = objectValue(value)
  return {
    id: stringValue(object.id), name: stringValue(object.name), kind: stringValue(object.kind) === 'ip' ? 'ip' as const : 'domain' as const,
    enabled: booleanValue(object.enabled), pendingDeletion: booleanValue(object.pendingDeletion),
    activeVersionId: stringValue(object.activeVersionId), versions: Array.isArray(object.versions) ? object.versions.map(parseSourceVersion) : [],
  }
}

function parseOverview(value: unknown): AccessOverview {
  const object = objectValue(value)
  const device = objectValue(object.device)
  return {
    device: { id: stringValue(device.id), name: stringValue(device.name), enabled: booleanValue(device.enabled) },
    rules: Array.isArray(object.rules) ? object.rules.map(parseRule) : [],
    sources: Array.isArray(object.sources) ? object.sources.map(parseSource) : [],
    terminals: Array.isArray(object.terminals) ? object.terminals.map(parseTerminal) : [],
    state: (() => {
      const state = objectValue(object.state)
      return { deviceId: stringValue(state.deviceId), desiredRevision: numberValue(state.desiredRevision), appliedRevision: numberValue(state.appliedRevision), appliedAt: stringValue(state.appliedAt) || undefined }
    })(),
    job: parseJob(object.job), boundary: stringValue(object.boundary),
  }
}

function parseApplyResult(value: unknown): AccessApplyResult {
  const object = objectValue(value)
  const job = object.job ? parseJob(object.job) : undefined
  return { rule: object.rule ? parseRule(object.rule) : undefined, deleted: booleanValue(object.deleted), desiredSaved: booleanValue(object.desiredSaved), job, jobId: stringValue(object.jobId ?? job?.id) || undefined }
}

function parseJobResult(value: unknown): { job: AccessJob } {
  const object = objectValue(value)
  return { job: parseJob(object.job) }
}

async function accessRequest<T>(path: string, init: RequestInit | undefined, parse: (value: unknown) => T): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, init)
  } catch {
    throw new AccessApiError('网络请求失败，请检查面板连接', 0, 'network_error')
  }
  if (response.status === 401) window.dispatchEvent(new Event('rosboard:authentication-required'))
  const responseText = await response.text()
  let payload: unknown = null
  if (responseText.trim()) {
    try {
      payload = JSON.parse(responseText)
    } catch {
      throw new AccessApiError('服务返回了无法解析的响应', response.status, 'invalid_response')
    }
  }
  if (!response.ok) {
    const failure = objectValue(payload)
    throw new AccessApiError(stringValue(failure.error) || `HTTP ${response.status}`, response.status, stringValue(failure.code) || undefined, payload)
  }
  try {
    return parse(payload)
  } catch {
    throw new AccessApiError('服务返回了无效的访问控制响应', response.status, 'invalid_response')
  }
}

function devicePath(deviceID: string, suffix = '') {
  return `/api/access-control/devices/${encodeURIComponent(deviceID)}${suffix}`
}

export function loadAccessOverview(deviceID: string) {
  return accessRequest(devicePath(deviceID), { cache: 'no-store' }, parseOverview)
}

export function createAccessRule(deviceID: string, rule: AccessRuleInput) {
  return accessRequest(devicePath(deviceID, '/rules'), {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(rule),
  }, parseApplyResult)
}

export function updateAccessRule(deviceID: string, rule: AccessRuleInput) {
  return accessRequest(devicePath(deviceID, `/rules/${encodeURIComponent(rule.id)}`), {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(rule),
  }, parseApplyResult)
}

export function deleteAccessRule(deviceID: string, ruleID: string, revision: number) {
  return accessRequest(devicePath(deviceID, `/rules/${encodeURIComponent(ruleID)}?revision=${revision}`), { method: 'DELETE' }, parseApplyResult)
}

// /sync 保留为恢复/运维入口：主界面不常驻，只在应用异常时作为“重新应用”使用。
export function reapplyAccessControl(deviceID: string, internetEgresses?: Record<string, string[]>) {
  const init: RequestInit = { method: 'POST' }
  if (internetEgresses) {
    init.headers = { 'Content-Type': 'application/json' }
    init.body = JSON.stringify({ internetEgresses })
  }
  return accessRequest(devicePath(deviceID, '/sync'), init, parseApplyResult)
}

export function getAccessJob(deviceID: string, jobID: string) {
  return accessRequest(devicePath(deviceID, `/jobs/${encodeURIComponent(jobID)}`), { cache: 'no-store' }, parseJobResult)
}

export async function waitForAccessJob(deviceID: string, jobID: string): Promise<AccessJob> {
  const deadline = Date.now() + 120_000
  while (Date.now() < deadline) {
    const { job } = await getAccessJob(deviceID, jobID)
    if (job.state === 'committed') return job
    if (job.state === 'failed') throw new AccessApiError(job.error || 'RouterOS 应用失败', 409, 'job_failed')
    await new Promise((resolve) => window.setTimeout(resolve, 700))
  }
  throw new AccessApiError('等待 RouterOS 应用超时', 408, 'job_timeout')
}
