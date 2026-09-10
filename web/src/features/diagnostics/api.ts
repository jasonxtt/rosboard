import { apiGet, safeArray, safeBoolean, safeObject, safeString, scoped } from '../../lib/api'
import type { DiagnosticFinding, DiagnosticOverall, DiagnosticReport, DiagnosticStatus } from './types'

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

export type { DiagnosticFinding, DiagnosticOverall, DiagnosticReport, DiagnosticStatus } from './types'
