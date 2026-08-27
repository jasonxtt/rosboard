import { useState } from 'react'
import { usePolicyAudit } from './hooks'
import { PolicyErrorDisplay, PolicyModal, PolicyPagination } from './components'
import type { PolicyAuditEntry } from './types'

export function PolicyAuditDialog({ deviceID, onClose }: { deviceID: string; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const { items, pageIndex, pageCount, loading, error, nextPage, prevPage } = usePolicyAudit(deviceID, true)
  const entries = items as PolicyAuditEntry[]

  const filtered = query.trim()
    ? entries.filter((e) => [e.actor, e.action, e.objectId, e.planId, e.summary, e.result].join(' ').toLowerCase().includes(query.toLowerCase()))
    : entries

  return (
    <PolicyModal title="操作审计" wide onClose={onClose}>
      <div className="policy-audit-toolbar">
        <input
          className="settings-input policy-audit-search"
          placeholder="搜索审计记录…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      {error ? <PolicyErrorDisplay error={error} /> : null}
      <div className="policy-audit-scroll">
        <table className="data-table policy-audit-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>操作者</th>
              <th>动作</th>
              <th>对象</th>
              <th>结果</th>
              <th>摘要</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length ? (
              filtered.map((entry) => (
                <tr key={entry.id}>
                  <td>{formatTimestamp(entry.createdAt)}</td>
                  <td>{entry.actor}{entry.remoteIp ? <small> ({entry.remoteIp})</small> : null}</td>
                  <td>{entry.action}</td>
                  <td>{objectLabel(entry)}</td>
                  <td>{entry.result}</td>
                  <td>{entry.summary}</td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={6} className="empty-row">{loading ? '加载中…' : '暂无审计记录'}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <div className="policy-audit-footer">
        <PolicyPagination pageIndex={pageIndex} pageCount={pageCount} onPrev={prevPage} onNext={nextPage} loading={loading} />
      </div>
    </PolicyModal>
  )
}

function objectLabel(entry: PolicyAuditEntry): string {
  const parts: string[] = []
  if (entry.objectId) parts.push(entry.objectId)
  if (entry.planId) parts.push(`计划:${entry.planId.slice(0, 8)}`)
  if (entry.jobId) parts.push(`作业:${entry.jobId.slice(0, 8)}`)
  if (entry.versionId) parts.push(`版本:${entry.versionId.slice(0, 8)}`)
  return parts.join(' · ') || '-'
}

function formatTimestamp(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}
