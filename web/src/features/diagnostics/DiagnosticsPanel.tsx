import { useMemo } from 'react'
import { useDiagnostics } from './hooks'
import type { DeepDiagnosticReport, DiagnosticFinding, DiagnosticStatus } from './types'

const STATUS_LABELS: Record<DiagnosticStatus, string> = {
  ok: '正常',
  warning: '注意',
  error: '错误',
  disabled: '已禁用',
  skipped: '已跳过',
}

const OVERALL_LABELS = { healthy: '系统正常', warning: '系统有注意项', error: '系统有错误' } as const

const GROUP_ORDER = ['system', 'routeros', 'topology', 'monitor', 'policy', 'access', 'recognition', 'update']
const GROUP_LABELS: Record<string, string> = {
  system: '系统核心',
  routeros: 'RouterOS 连接',
  topology: '网络拓扑',
  monitor: '采集健康',
  policy: '策略路由',
  access: '访问控制',
  recognition: '识别同步',
  update: '更新恢复',
}

function formatEvidence(value: unknown): string {
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (value === null || value === undefined) return '—'
  try {
    return JSON.stringify(value)
  } catch {
    return '—'
  }
}

function formatTime(value: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function groupFindings(findings: DiagnosticFinding[]): Array<[string, DiagnosticFinding[]]> {
  const groups = new Map<string, DiagnosticFinding[]>()
  for (const finding of findings) groups.set(finding.group, [...(groups.get(finding.group) ?? []), finding])
  const ordered = GROUP_ORDER.filter((group) => groups.has(group))
  const remaining = [...groups.keys()].filter((group) => !GROUP_ORDER.includes(group))
  return [...ordered, ...remaining].map((group) => [group, groups.get(group) ?? []])
}

function FindingCard({ finding }: { finding: DiagnosticFinding }) {
  const evidence = Object.entries(finding.evidence)
  return (
    <article className={`diagnostics-finding diagnostics-finding-${finding.status}`}>
      <div className="diagnostics-finding-head">
        <div className="diagnostics-finding-title">
          <span className="diagnostics-status-dot" aria-hidden="true" />
          <strong>{finding.title}</strong>
        </div>
        <div className="diagnostics-finding-meta">
          <span className="diagnostics-status-label">{STATUS_LABELS[finding.status]}</span>
          <span className="diagnostics-scope-label">{finding.affectsOverall ? '影响总体' : '模块状态'}</span>
        </div>
      </div>
      <p className="diagnostics-finding-summary">{finding.summary}</p>
      {finding.recommendation ? <p className="diagnostics-finding-recommendation">建议：{finding.recommendation}</p> : null}
      {evidence.length ? (
        <details className="diagnostics-evidence">
          <summary>查看证据</summary>
          <dl>
            {evidence.map(([key, value]) => (
              <div key={key}>
                <dt>{key}</dt>
                <dd>{formatEvidence(value)}</dd>
              </div>
            ))}
          </dl>
        </details>
      ) : null}
    </article>
  )
}

function DeepReportSection({ report }: { report: DeepDiagnosticReport }) {
  const topologyFinding = report.findings.find((finding) => finding.id === 'topology.deep')
  const failedEndpoints = report.snapshot.endpoints.filter((endpoint) => endpoint.error)
  return (
    <section className="diagnostics-deep" aria-labelledby="diagnostics-deep-title">
      <div className="diagnostics-deep-header">
        <div>
          <p className="diagnostics-eyebrow">DEEP SNAPSHOT</p>
          <h3 id="diagnostics-deep-title">全面体检</h3>
          <p>基于同一次只读 RouterOS 快照展示网络拓扑和策略入口推荐分析。</p>
        </div>
        <span className={`diagnostics-deep-state diagnostics-deep-state-${report.overall}`}>{OVERALL_LABELS[report.overall]}</span>
      </div>
      {topologyFinding ? <FindingCard finding={topologyFinding} /> : null}
      <section className="diagnostics-deep-section" aria-labelledby="diagnostics-snapshot-title">
          <div className="diagnostics-deep-section-head">
            <h4 id="diagnostics-snapshot-title">RouterOS 快照</h4>
            <span>{report.snapshot.endpoints.length} 个 endpoint{failedEndpoints.length ? ` · ${failedEndpoints.length} 个失败` : ''}</span>
          </div>
          <p className="diagnostics-deep-meta">指纹：{report.snapshot.fingerprint || '—'} · 读取于 {formatTime(report.snapshot.capturedAt)}</p>
          <ul className="diagnostics-endpoint-list">
            {report.snapshot.endpoints.map((endpoint) => (
              <li key={endpoint.endpoint} className={endpoint.error ? 'diagnostics-endpoint-error' : undefined}>
                <div>
                  <strong>{endpoint.endpoint}</strong>
                  <span>{endpoint.error ? `失败：${endpoint.error}` : `已读取 · ${endpoint.objectCount} 条对象`}</span>
                </div>
                <small>{endpoint.required ? '必需' : '可选'} · read {endpoint.readCount}</small>
              </li>
            ))}
          </ul>
      </section>
    </section>
  )
}

export function DiagnosticsPanel({ deviceId, deviceName }: { deviceId: string; deviceName?: string }) {
  const { report, loading, error, reload, deepReport, deepLoading, deepError, runDeep, exportLoading, exportError, runExport } = useDiagnostics(deviceId)
  const counts = useMemo(() => {
    const values: Record<DiagnosticStatus, number> = { ok: 0, warning: 0, error: 0, disabled: 0, skipped: 0 }
    for (const finding of report?.findings ?? []) values[finding.status] += 1
    return values
  }, [report])

  if (!deviceId) {
    return (
      <section className="diagnostics-panel diagnostics-empty" aria-labelledby="diagnostics-title">
        <div>
          <h2 id="diagnostics-title">系统诊断</h2>
          <p>选择一台启用的 RouterOS 设备后才能运行快速健康检查。</p>
        </div>
      </section>
    )
  }

  const groups = report ? groupFindings(report.findings) : []
  return (
    <section className="diagnostics-panel" aria-labelledby="diagnostics-title">
      <div className="diagnostics-header">
        <div>
          <p className="diagnostics-eyebrow">QUICK HEALTH</p>
          <h2 id="diagnostics-title">系统诊断</h2>
          <p>{deviceName || deviceId} · 快速检查读取现有面板状态；全面体检和导出会执行只读 RouterOS 诊断。</p>
        </div>
        <div className="diagnostics-actions">
          <button type="button" className="diagnostics-refresh" onClick={() => void reload()} disabled={loading || deepLoading || exportLoading}>
            {loading ? '检查中…' : '重新检查'}
          </button>
          <button type="button" className="diagnostics-refresh diagnostics-deep-button" onClick={() => void runDeep()} disabled={loading || deepLoading || exportLoading}>
            {deepLoading ? '体检中…' : '全面体检'}
          </button>
          <button type="button" className="diagnostics-refresh diagnostics-export-button" onClick={() => void runExport()} disabled={loading || deepLoading || exportLoading}>
            {exportLoading ? '导出中…' : '导出诊断包'}
          </button>
        </div>
      </div>

      {error ? <p className="diagnostics-error" role="alert">{error}</p> : null}
      {deepError ? <p className="diagnostics-error" role="alert">{deepError}</p> : null}
      {exportError ? <p className="diagnostics-error" role="alert">{exportError}</p> : null}
      <p className="diagnostics-export-notice">诊断包会自动脱敏密码、令牌、私钥、公网 IP 和设备标识；仍会包含接口名称、内网地址、路由关系和设备运行状态，请仅发送给可信技术支持人员。</p>
      {loading && !report ? <p className="diagnostics-loading">正在读取诊断报告…</p> : null}

      {report ? (
        <>
          <div className={`diagnostics-overall diagnostics-overall-${report.overall}`}>
            <div>
              <span className="diagnostics-overall-label">{OVERALL_LABELS[report.overall]}</span>
              <span className="diagnostics-generated">生成于 {formatTime(report.generatedAt)}</span>
            </div>
            <div className="diagnostics-counts" aria-label="诊断结果统计">
              <span className="diagnostics-count diagnostics-count-ok">正常 {counts.ok}</span>
              <span className="diagnostics-count diagnostics-count-warning">注意 {counts.warning}</span>
              <span className="diagnostics-count diagnostics-count-error">错误 {counts.error}</span>
              <span className="diagnostics-count diagnostics-count-muted">禁用/跳过 {counts.disabled + counts.skipped}</span>
            </div>
          </div>
          <div className="diagnostics-groups">
            {groups.map(([group, findings]) => (
              <section key={group} className="diagnostics-group" aria-labelledby={`diagnostics-group-${group}`}>
                <h3 id={`diagnostics-group-${group}`}>{GROUP_LABELS[group] ?? group}</h3>
                <div className="diagnostics-finding-list">
                  {findings.map((finding) => <FindingCard key={finding.id} finding={finding} />)}
                </div>
              </section>
            ))}
          </div>
          {deepReport ? <DeepReportSection report={deepReport} /> : null}
        </>
      ) : null}
      {!report && deepReport ? <DeepReportSection report={deepReport} /> : null}
    </section>
  )
}
