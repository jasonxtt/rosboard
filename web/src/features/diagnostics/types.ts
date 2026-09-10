export type DiagnosticStatus = 'ok' | 'warning' | 'error' | 'disabled' | 'skipped'
export type DiagnosticOverall = 'healthy' | 'warning' | 'error'

export type DiagnosticEvidence = Record<string, unknown>

export type DiagnosticFinding = {
  id: string
  group: string
  status: DiagnosticStatus
  title: string
  summary: string
  recommendation: string
  affectsOverall: boolean
  evidence: DiagnosticEvidence
}

export type DiagnosticReport = {
  generatedAt: string
  mode: string
  deviceId: string
  overall: DiagnosticOverall
  findings: DiagnosticFinding[]
}
