import { useState } from 'react'
import type { PolicyEgress, PolicyOverview, PolicySource } from './types'
import {
  deleteSource,
  fetchSource,
  previewUpload,
  previewURL,
  saveSource,
  waitForPolicyJob,
} from './api'
import {
  PolicyErrorDisplay,
  PolicyEmptyState,
  PolicyField,
  PolicyModal,
  PolicyPagination,
  PolicyNotice,
  PolicyStatusBadge,
  type StatusTone,
} from './components'
import { useSourceRules } from './hooks'
import {
  policyScheduleLabel,
  policySourceTypeLabel,
  scheduleOptions,
} from './format'
import { defaultSourceDraft, sourceDraftFromSource } from './drafts'
import type { PolicySourceDraft } from './types'

export function PolicySourcesPage({
  deviceID,
  overview,
  sources,
  egresses,
  readOnly,
  onChanged,
}: {
  deviceID: string
  overview: PolicyOverview | null
  sources: PolicySource[]
  egresses: PolicyEgress[]
  readOnly: boolean
  onChanged: () => void
}) {
  const [editing, setEditing] = useState<PolicySourceDraft | null>(null)
  const [viewingRules, setViewingRules] = useState<PolicySource | null>(null)
  const [deleting, setDeleting] = useState<PolicySource | null>(null)

  return (
    <section className="panel policy-panel" aria-label="域名列表">
      <div className="policy-panel-head">
        <h3>域名列表</h3>
      </div>
      {sources.length === 0 ? (
        <PolicyEmptyState
          title="尚未配置域名列表"
          description={readOnly ? '当前为只读状态。' : '添加远程 URL 或本地上传的 Clash YAML；内容变化先预览，确认保存后会自动同步到所选策略路由。'}
          action={readOnly ? undefined : <button type="button" className="primary-button" onClick={() => setEditing(defaultSourceDraft(egresses[0]?.id ?? ''))}>新建域名列表</button>}
        />
      ) : (
        <>
          <div className="table-scroll policy-table-scroll">
            <table className="data-table policy-source-table">
              <thead>
                <tr>
                  <th>名称</th>
                  <th>类型</th>
                  <th>归属策略路由</th>
                  <th>标记列表</th>
                  <th>更新计划</th>
                  <th>活动版本</th>
                  <th>下次更新</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {sources.map((src) => (
                  <SourceRow
                    key={src.id}
                    source={src}
                    egresses={egresses}
                    overview={overview}
                    readOnly={readOnly}
                    onEdit={() => setEditing(sourceDraftFromSource(src))}
                    onViewRules={() => setViewingRules(src)}
                    onDelete={() => setDeleting(src)}
                  />
                ))}
              </tbody>
            </table>
          </div>
          {!readOnly ? (
            <div className="policy-panel-add-action">
              <button type="button" className="primary-button" onClick={() => setEditing(defaultSourceDraft(egresses[0]?.id ?? ''))}>增加域名列表</button>
            </div>
          ) : null}
        </>
      )}

      {editing ? (
        <SourceEditorModal
          deviceID={deviceID}
          draft={editing}
          egresses={egresses}
          onClose={() => setEditing(null)}
          onSuccess={() => { setEditing(null); onChanged() }}
        />
      ) : null}

      {viewingRules ? (
        <SourceRulesModal deviceID={deviceID} source={viewingRules} onClose={() => setViewingRules(null)} />
      ) : null}

      {deleting ? (
        <SourceDeleteModal
          deviceID={deviceID}
          source={deleting}
          onClose={() => setDeleting(null)}
          onDone={() => { setDeleting(null); onChanged() }}
        />
      ) : null}
    </section>
  )
}

function sourceStatus(src: PolicySource, overview: PolicyOverview | null): { tone: StatusTone; label: string } {
  if (src.pendingDeletion) return { tone: 'warn', label: '清理未完成，可重试' }
  if (!src.egressId) return { tone: 'neutral', label: '未分配（不参与同步）' }
  const egress = overview?.egresses.find((e) => e.id === src.egressId)
  if (egress && (egress.pendingDeletion || !egress.enabled)) return { tone: 'warn', label: '策略已停用（暂不参与同步）' }
  const pendingVersion = src.versions.some((version) => version.state === 'pending')
  if (pendingVersion) {
    const failed = overview?.health?.state === 'degraded' && overview.activeJobs.length === 0
    return failed
      ? { tone: 'bad', label: '已引用（同步失败，可重新保存）' }
      : { tone: 'info', label: '已引用（同步中）' }
  }
  if (src.activeVersionId) return { tone: 'good', label: '已引用' }
  return { tone: 'info', label: '已引用（草稿）' }
}

function SourceRow({
  source,
  egresses,
  overview,
  readOnly,
  onEdit,
  onViewRules,
  onDelete,
}: {
  source: PolicySource
  egresses: PolicyEgress[]
  overview: PolicyOverview | null
  readOnly: boolean
  onEdit: () => void
  onViewRules: () => void
  onDelete: () => void
}) {
  const status = sourceStatus(source, overview)
  const egress = egresses.find((e) => e.id === source.egressId)
  const activeVersion = source.versions.find((v) => v.id === source.activeVersionId)
  const nextRun = source.nextRunAt ? new Date(source.nextRunAt) : null

  return (
    <tr>
      <td>
        <span className="policy-cell-stack">
          <strong>{source.name}</strong>
          {source.url ? <span className="policy-subcell mono">{source.url}</span> : null}
        </span>
      </td>
      <td>{policySourceTypeLabel[source.type] ?? source.type}</td>
      <td>{egress ? <span>{egress.name}</span> : <PolicyStatusBadge tone="neutral">未分配</PolicyStatusBadge>}</td>
      <td>{egress ? egress.listMode === 'dedicated' ? '专用（随本域名列表自动生成）' : <span className="mono">{egress.listName || '—'}</span> : '—'}</td>
      <td>{source.type === 'url' ? policyScheduleLabel[source.schedule] ?? source.schedule : '手动更新'}</td>
      <td className="mono">{activeVersion ? activeVersion.id.slice(0, 12) : '—'}</td>
      <td className="policy-date-cell mono">{nextRun && Number.isFinite(nextRun.getTime()) ? formatPolicyDateTime(nextRun) : '—'}</td>
      <td><PolicyStatusBadge tone={status.tone}>{status.label}</PolicyStatusBadge></td>
      <td>
        <div className="action-links">
          <button type="button" className="link-button" onClick={onViewRules}>规则</button>
          {readOnly ? null : (
            <>
              <button
                type="button"
                className="link-button"
                disabled={source.pendingDeletion}
                title={source.pendingDeletion ? '该记录已标记待清理，无法编辑；请先完成清理' : undefined}
                onClick={onEdit}
              >
                编辑
              </button>
              <button type="button" className="link-button link-button--danger" onClick={onDelete}>删除</button>
            </>
          )}
        </div>
      </td>
    </tr>
  )
}

function formatPolicyDateTime(value: Date): string {
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${value.getFullYear()}-${pad(value.getMonth() + 1)}-${pad(value.getDate())} ${pad(value.getHours())}:${pad(value.getMinutes())}:${pad(value.getSeconds())}`
}

export function SourceEditorModal({
  deviceID,
  draft,
  egresses,
  onClose,
  onSuccess,
  deferApply = false,
}: {
  deviceID: string
  draft: PolicySourceDraft
  egresses: PolicyEgress[]
  onClose: () => void
  onSuccess: (source: PolicySource) => void
  deferApply?: boolean
}) {
  const [name, setName] = useState(draft.name)
  const [type, setType] = useState(draft.type)
  const [url, setUrl] = useState(draft.url)
  const [schedule, setSchedule] = useState(draft.schedule)
  const [egressId, setEgressId] = useState(draft.egressId)
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState<import('./types').PolicyPreview | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const contentChanged = !draft.id
    || type !== draft.type
    || (type === 'url' && url.trim() !== draft.url)
    || (type === 'upload' && file !== null)
  const validationError = name.trim()
    ? type === 'url' && !url.trim() ? '请填写 Clash YAML 的 HTTPS 地址'
      : type === 'upload' && !draft.id && !file ? '请选择本地 YAML 文件'
        : null
    : '请填写列表名称'

  const handlePreview = async () => {
    setError(null)
    setPreviewing(true)
    try {
      if (type === 'url') {
        if (!url.trim().startsWith('https://')) throw new Error('请填写 Clash YAML 的 HTTPS 地址')
        const p = await previewURL(deviceID, { url })
        setPreview(p)
      } else if (file) {
        const fd = new FormData()
        fd.append('file', file)
        const p = await previewUpload(deviceID, fd)
        setPreview(p)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : '预览失败')
    } finally {
      setPreviewing(false)
    }
  }

  const handleSave = async () => {
    setError(null)
    if (validationError) return
    if (contentChanged && !preview?.previewId && !preview?.notModified) {
      await handlePreview()
      return
    }
    setSaving(true)
    try {
      if (preview && preview.validRules <= 0) {
        throw new Error('解析结果没有有效规则，无法继续保存该列表')
      }
      const saveResult = await saveSource(deviceID, { ...draft, name, type, url, schedule, egressId }, {
        previewId: preview?.previewId,
        deferApply,
      })
      let savedSource = saveResult.source
      if (saveResult.jobId) {
        await waitForPolicyJob(deviceID, saveResult.jobId)
        savedSource = await fetchSource(deviceID, saveResult.source.id)
      }
      onSuccess(savedSource)
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <PolicyModal
      title={draft.id ? `编辑列表：${draft.name}` : '新建域名列表'}
      subtitle="内容变化会先解析预览；确认保存后，已归属启用策略的列表会自动同步到 RouterOS。"
      wide
      onClose={onClose}
    >
      {error ? <PolicyErrorDisplay error={error} /> : null}
      <div className="policy-form">
        <PolicyField label="列表名称" htmlFor="policy-source-name">
          <input id="policy-source-name" className="settings-input" value={name} onChange={(e) => setName(e.target.value)} maxLength={128} placeholder="例如 OpenAI 域名" />
        </PolicyField>
        <PolicyField label="来源类型" htmlFor="policy-source-type">
          <div className="policy-choice-list" role="radiogroup" aria-label="来源类型">
            <label className={`policy-choice${type === 'url' ? ' active' : ''}`}>
              <input type="radio" name="policy-source-type" checked={type === 'url'} onChange={() => { setType('url'); setPreview(null); setFile(null) }} />
              <span>
                <strong>远程 URL</strong>
                <small>由 rosboard 主机下载公共 HTTPS Clash YAML；RouterOS 不保存 URL。</small>
              </span>
            </label>
            <label className={`policy-choice${type === 'upload' ? ' active' : ''}`}>
              <input type="radio" name="policy-source-type" checked={type === 'upload'} onChange={() => { setType('upload'); setPreview(null) }} />
              <span>
                <strong>本地上传</strong>
                <small>上传浏览器本地 .yaml/.yml（不超过 5 MiB），仅重新上传时更新。</small>
              </span>
            </label>
          </div>
        </PolicyField>
        {type === 'url' ? (
          <div className="policy-form-grid">
            <PolicyField label="来源 URL" htmlFor="policy-source-url" hint="仅接受公共 HTTPS；GitHub blob 地址会自动转换为 raw。">
              <input id="policy-source-url" className="settings-input" type="url" value={url} onChange={(e) => { setUrl(e.target.value); setPreview(null) }} placeholder="https://example.com/rules.yaml" />
            </PolicyField>
            <PolicyField label="更新计划" htmlFor="policy-source-schedule">
              <select id="policy-source-schedule" className="settings-select" value={schedule} onChange={(e) => setSchedule(e.target.value)}>
                {scheduleOptions.map((opt) => <option key={opt.value} value={opt.value}>{opt.value === '24h' ? '每 24 小时（默认）' : opt.label}</option>)}
              </select>
            </PolicyField>
          </div>
        ) : (
          <PolicyField label="YAML 文件" htmlFor="policy-source-file" hint="解析顶层 payload 中的 DOMAIN 与 DOMAIN-SUFFIX 规则。">
            <input id="policy-source-file" type="file" className="settings-input" accept=".yaml,.yml" onChange={(e) => { setFile(e.target.files?.[0] ?? null); setPreview(null) }} />
          </PolicyField>
        )}
        <PolicyField label="归属策略路由" htmlFor="policy-source-egress" hint="一个域名列表最多归属一个策略路由；选择启用策略并保存后会自动同步，可先保存为未分配列表。">
          <select id="policy-source-egress" className="settings-select" value={egressId} onChange={(e) => setEgressId(e.target.value)}>
            <option value="">未分配（不参与同步）</option>
            {egresses.map((eg) => <option key={eg.id} value={eg.id}>{eg.name}{eg.enabled && !eg.pendingDeletion ? '' : '（已停用/待删除）'}</option>)}
          </select>
        </PolicyField>
        {contentChanged ? (
          <div className="policy-form-section">
            {preview ? <SourcePreview preview={preview} /> : <p className="policy-hint">点击“保存并预览”查看解析结果，确认后再次点击“保存”提交新版本。</p>}
          </div>
        ) : <p className="policy-hint">内容未变化，可直接保存名称 / 更新计划 / 归属等元数据修改。</p>}
        {validationError ? <p className="policy-field-error">{validationError}</p> : null}
        <div className="policy-form-actions">
          <button type="button" className="close-button" onClick={onClose} disabled={saving}>取消</button>
          <button type="button" className="primary-button" disabled={saving || previewing || Boolean(validationError)} onClick={() => { void handleSave() }}>
            {saving ? '正在保存…' : previewing ? '正在解析…' : contentChanged ? preview?.previewId || preview?.notModified ? '保存' : '保存并预览' : '保存列表'}
          </button>
        </div>
      </div>
    </PolicyModal>
  )
}

function SourcePreview({ preview }: { preview: import('./types').PolicyPreview }) {
  const ignored = Object.entries(preview.ignored)
  return (
    <div className="policy-preview" aria-live="polite">
      {preview.notModified ? <PolicyNotice tone="info" title="远端内容未变化">当前 ETag / Last-Modified 未变化，保留现有活动版本。</PolicyNotice> : null}
      <dl className="policy-preview-stats">
        <div><dt>有效规则</dt><dd className="mono">{preview.validRules}</dd></div>
        <div><dt>大小</dt><dd className="mono">{formatBytes(preview.size)}</dd></div>
        <div><dt>SHA-256</dt><dd className="mono">{preview.sha256 ? preview.sha256.slice(0, 12) : '-'}</dd></div>
        <div><dt>来源</dt><dd className="mono">{preview.filename || preview.url || '-'}</dd></div>
      </dl>
      {ignored.length ? <p className="policy-hint">忽略：{ignored.map(([key, value]) => `${key} ${value} 条`).join('，')}</p> : null}
      {preview.errorSamples.length ? <ul className="policy-preview-errors">{preview.errorSamples.map((item, index) => <li key={`${item}-${index}`}>{item}</li>)}</ul> : null}
      {preview.rules.length ? (
        <div className="policy-preview-rules" aria-label="规则样本（前 100 条）">
          {preview.rules.map((rule) => <code key={`${rule.type}:${rule.domain}`}>{rule.domain}</code>)}
          {preview.validRules > preview.rules.length ? <span>… 共 {preview.validRules} 条，仅显示前 {preview.rules.length} 条</span> : null}
        </div>
      ) : null}
      {!preview.previewId && !preview.notModified ? <p className="policy-field-error">预览未被保存（预览存储不可用），暂时无法保存该来源。</p> : null}
    </div>
  )
}

function formatBytes(value: number | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return '-'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / (1024 * 1024)).toFixed(2)} MiB`
}

function SourceRulesModal({ deviceID, source, onClose }: { deviceID: string; source: PolicySource; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const { items, pageIndex, pageCount, loading, error, nextPage, prevPage } = useSourceRules(deviceID, source.id, true)

  const rules = items as Array<{ type: string; domain: string }>
  const filtered = query.trim()
    ? rules.filter((r) => r.domain.toLowerCase().includes(query.toLowerCase()))
    : typeFilter
      ? rules.filter((r) => r.type === typeFilter)
      : rules

  return (
    <PolicyModal title={`规则列表 — ${source.name}`} wide onClose={onClose}>
      <div className="policy-rules-toolbar">
        <input className="settings-input" placeholder="搜索域名…" value={query} onChange={(e) => setQuery(e.target.value)} />
        <select className="select-control" value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)}>
          <option value="">全部类型</option>
          <option value="DOMAIN">精确匹配</option>
          <option value="DOMAIN-SUFFIX">后缀匹配</option>
        </select>
      </div>
      {error ? <PolicyErrorDisplay error={error} /> : null}
      <div className="policy-rules-scroll">
        <table className="data-table policy-rules-table">
          <thead>
            <tr>
              <th>类型</th>
              <th>域名</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length ? (
              filtered.map((r, i) => (
                <tr key={`${r.domain}-${i}`}>
                  <td><span className={`policy-rule-type ${r.type === 'DOMAIN' ? 'policy-rule-type-exact' : 'policy-rule-type-suffix'}`}>{r.type === 'DOMAIN' ? '精确' : '后缀'}</span></td>
                  <td><code>{r.domain}</code></td>
                </tr>
              ))
            ) : (
              <tr><td colSpan={2} className="empty-row">{loading ? '加载中…' : '暂无规则'}</td></tr>
            )}
          </tbody>
        </table>
      </div>
      <div className="policy-pagination">
        <PolicyPagination pageIndex={pageIndex} pageCount={pageCount} onPrev={prevPage} onNext={nextPage} loading={loading} />
      </div>
    </PolicyModal>
  )
}

function SourceDeleteModal({ deviceID, source, onClose, onDone }: { deviceID: string; source: PolicySource; onClose: () => void; onDone: () => void }) {
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)

  const handleDelete = async () => {
    setError(null)
    setDeleting(true)
    try {
      const result = await deleteSource(deviceID, source.id, source.revision)
      if (result.jobId) await waitForPolicyJob(deviceID, result.jobId)
      onDone()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除失败')
    } finally {
      setDeleting(false)
    }
  }

  const message = source.pendingDeletion
    ? '该列表上次清理未完成；确认后将重新清理它在 RouterOS 中生成的规则。'
    : source.egressId
      ? '确认后将立即解除策略绑定并清理该列表在 RouterOS 中生成的规则；其他列表和策略不受影响。'
      : '该列表未分配给策略；确认后将立即删除 rosboard 中的定义与历史版本。此操作不可撤销。'

  return (
    <PolicyModal title="删除域名列表" onClose={onClose}>
      {error ? <PolicyErrorDisplay error={error} /> : null}
      <p className="policy-hint">{message}</p>
      {source.revision ? <p className="policy-hint">修订版本：{source.revision}</p> : null}
      <div className="policy-form-actions">
        <button type="button" className="primary-button" disabled={deleting} onClick={handleDelete}>
          {deleting ? '删除中…' : '确认删除'}
        </button>
        <button type="button" className="toolbar-button" onClick={onClose}>取消</button>
      </div>
    </PolicyModal>
  )
}
