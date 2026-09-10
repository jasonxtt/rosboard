import { apiGet, apiPost, apiPostBlob, safeArray, safeBoolean, safeNumber, safeObject, safeString, safeStringArray, scoped } from '../../lib/api'
import type { DeepDiagnosticReport, DiagnosticEndpoint, DiagnosticFinding, DiagnosticOverall, DiagnosticReport, DiagnosticSnapshot, DiagnosticStatus, IngressDecision } from './types'

function parseStatus(value: unknown): DiagnosticStatus {
  switch (value) {
    case 'ok':
    case 'warning':
    case 'error':
    case 'disabled':
    case 'skipped':
      return value
    default:
      return 'error'
  }
}

function parseOverall(value: unknown): DiagnosticOverall {
  switch (value) {
    case 'healthy':
    case 'warning':
    case 'error':
      return value
    default:
      return 'error'
  }
}

function parseFinding(value: unknown): DiagnosticFinding | null {
  const item = safeObject(value)
  const id = safeString(item.id)
  if (!id) return null
  return {
    id,
    group: safeString(item.group) || 'system',
    status: parseStatus(item.status),
    title: safeString(item.title) || id,
    summary: safeString(item.summary),
    recommendation: safeString(item.recommendation),
    affectsOverall: safeBoolean(item.affectsOverall),
    evidence: safeObject(item.evidence),
  }
}

function parseStringMap(value: unknown): Record<string, string> {
  return Object.fromEntries(Object.entries(safeObject(value)).map(([key, item]) => [key, safeString(item)]))
}

function parseEndpoint(value: unknown): DiagnosticEndpoint | null {
  const item = safeObject(value)
  const endpoint = safeString(item.endpoint)
  if (!endpoint) return null
  return {
    endpoint,
    purpose: safeString(item.purpose),
    sharedBy: safeStringArray(item.sharedBy),
    fields: safeStringArray(item.fields),
    required: safeBoolean(item.required),
    readCount: safeNumber(item.readCount),
    cacheHits: safeNumber(item.cacheHits),
    objectCount: safeNumber(item.objectCount),
    objects: safeArray(item.objects).map(parseStringMap),
    truncated: safeBoolean(item.truncated),
    error: safeString(item.error),
  }
}

function parseSnapshot(value: unknown): DiagnosticSnapshot {
  const item = safeObject(value)
  return {
    capturedAt: safeString(item.capturedAt),
    fingerprint: safeString(item.fingerprint),
    endpoints: safeArray(item.endpoints).map(parseEndpoint).filter((endpoint): endpoint is DiagnosticEndpoint => endpoint !== null),
  }
}

function parseIngressDecision(value: unknown): IngressDecision | null {
  const item = safeObject(value)
  const reasonCode = safeString(item.reasonCode)
  if (!reasonCode) return null
  return {
    interface: safeString(item.interface),
    result: safeString(item.result),
    reasonCode,
    reason: safeString(item.reason),
    evidence: safeObject(item.evidence),
  }
}

export function parseDiagnostics(value: unknown): DiagnosticReport {
  const item = safeObject(value)
  const findings = safeArray(item.findings).map(parseFinding).filter((finding): finding is DiagnosticFinding => finding !== null)
  return {
    generatedAt: safeString(item.generatedAt),
    mode: safeString(item.mode) || 'quick',
    deviceId: safeString(item.deviceId),
    overall: parseOverall(item.overall),
    findings,
  }
}

export function fetchDiagnostics(deviceId: string, signal?: AbortSignal): Promise<DiagnosticReport> {
  return apiGet(scoped('/api/diagnostics', deviceId), parseDiagnostics, signal)
}

export function parseDeepDiagnostics(value: unknown): DeepDiagnosticReport {
  const item = safeObject(value)
  const report = parseDiagnostics(value)
  return {
    ...report,
    mode: 'deep',
    snapshot: parseSnapshot(item.snapshot),
    ingressTrace: safeArray(item.ingressTrace).map(parseIngressDecision).filter((decision): decision is IngressDecision => decision !== null),
  }
}

export function fetchDeepDiagnostics(deviceId: string): Promise<DeepDiagnosticReport> {
  return apiPost(scoped('/api/diagnostics/deep', deviceId), undefined, parseDeepDiagnostics)
}

export function downloadDiagnostics(deviceId: string): Promise<{ blob: Blob; filename: string }> {
  return apiPostBlob(scoped('/api/diagnostics/export', deviceId)).then((result) => ({
    blob: result.blob,
    filename: result.filename || `rosboard-diagnostics-${new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, '').replace('T', '-')}.zip`,
  }))
}

export type { DeepDiagnosticReport, DiagnosticEndpoint, DiagnosticFinding, DiagnosticOverall, DiagnosticReport, DiagnosticSnapshot, DiagnosticStatus, IngressDecision } from './types'
