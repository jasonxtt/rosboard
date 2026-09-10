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

export type DiagnosticEndpoint = {
  endpoint: string
  purpose: string
  sharedBy: string[]
  fields: string[]
  required: boolean
  readCount: number
  cacheHits: number
  objectCount: number
  objects: Array<Record<string, string>>
  truncated: boolean
  error: string
}

export type DiagnosticSnapshot = {
  capturedAt: string
  fingerprint: string
  endpoints: DiagnosticEndpoint[]
}

export type DeepDiagnosticReport = DiagnosticReport & {
  snapshot: DiagnosticSnapshot
}
