import { useCallback, useEffect, useMemo, useState } from 'react'
import { deleteTargetList, fetchTargetLists, previewTargetList, refreshTargetList, saveTargetList, waitForPolicyJob, type TargetList, type TargetListPreview } from './canonical'
import { PolicyEmptyState, PolicyErrorDisplay, PolicyField, PolicyModal, PolicyNotice, PolicyPreparing, PolicyStatusBadge, type StatusTone } from '../policy-routing/components'

type KindFilter = 'all' | 'domain' | 'ip'
type SourceType = 'manual' | 'url' | 'upload'

function jobID(value: { jobId?: string; job?: { id: string } }) { return value.jobId ?? value.job?.id ?? '' }

export function TargetLibraryPage({ deviceID, refreshNonce }: { deviceID: string; refreshNonce: number }) {
  const [targets, setTargets] = useState<TargetList[]>([])
  const [filter, setFilter] = useState<KindFilter>('all')
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [editing, setEditing] = useState<TargetList | null | undefined>(undefined)

  const reload = useCallback(async () => {
    try {
      setTargets((await fetchTargetLists(deviceID)).filter((target) => target.sourceType !== 'preset'))
      setError(null)
    } catch (loadError) {
      setError(loadError)
    } finally {
      setLoading(false)
    }
  }, [deviceID])

  useEffect(() => { setLoading(true); void reload() }, [reload, refreshNonce])

  const visible = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    return targets.filter((target) => (filter === 'all' || target.kind === filter) && (!keyword || `${target.name} ${target.id} ${target.url ?? ''}`.toLowerCase().includes(keyword)))
  }, [filter, query, targets])

  const performRefresh = async (target: TargetList) => {
    try {
      const result = await refreshTargetList(deviceID, target.id)
      const id = jobID(result)
      if (id) await waitForPolicyJob(deviceID, id)
      setNotice(`${target.name} 已刷新。`)
      await reload()
    } catch (refreshError) {
      setError(refreshError)
    }
  }

  const remove = async (target: TargetList) => {
    if (!window.confirm(`删除目标列表“${target.name}”？`)) return
    try {
      await deleteTargetList(deviceID, target.id, target.revision)
      setNotice('目标列表已删除。')
      await reload()
    } catch (deleteError) {
      setError(deleteError)
    }
  }

  if (loading && !targets.length) return <PolicyPreparing text="正在读取目标库…" />
  return <div className="policy-page target-library-page">
    {notice ? <PolicyNotice tone="good">{notice}</PolicyNotice> : null}
    {error ? <PolicyErrorDisplay error={error} /> : null}
    <section className="panel policy-panel">
      <div className="policy-panel-head">
        <div>
          <h3>Target Library</h3>
          <p className="policy-hint">这里仅管理你创建的手动、URL、上传目标列表；未被策略路由或访问规则引用时保持待命，不单独写入设备。</p>
        </div>
        <button type="button" className="primary-button" onClick={() => setEditing(null)}>新增目标列表</button>
      </div>
      <div className="policy-toolbar">
        <div className="segmented-control">
          <button type="button" className={filter === 'all' ? 'active' : ''} onClick={() => setFilter('all')}>全部</button>
          <button type="button" className={filter === 'domain' ? 'active' : ''} onClick={() => setFilter('domain')}>域名</button>
          <button type="button" className={filter === 'ip' ? 'active' : ''} onClick={() => setFilter('ip')}>IP</button>
        </div>
        <input className="settings-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索目标列表" />
      </div>
      {visible.length === 0 ? <PolicyEmptyState title="暂无目标列表" description="从手动内容、URL 或上传文件创建可复用目标。" action={<button type="button" className="primary-button" onClick={() => setEditing(null)}>新增目标列表</button>} /> : (
        <div className="policy-table-scroll">
          <table className="data-table">
			<thead><tr><th>目标列表</th><th>类型</th><th>来源</th><th>内容</th><th>使用中</th><th>状态</th><th>操作</th></tr></thead>
			<tbody>{visible.map((target) => {
				const hasConsumers = target.usage.routingRuleCount + target.usage.accessRuleCount > 0
				const hasContent = Boolean(target.activeVersionId || target.pendingVersionId)
				const status: { tone: StatusTone; label: string } = target.pendingDeletion ? { tone: 'warn', label: '清理中' } : !hasConsumers && hasContent ? { tone: 'neutral', label: '待命' } : target.activeVersionId ? { tone: 'good', label: '已应用' } : { tone: 'warn', label: '待应用' }
				return <tr key={target.id}>
                <td><strong>{target.name}</strong><span className="policy-sub-cell">{target.id}</span></td>
                <td>{target.kind === 'ip' ? 'IP' : '域名'}</td>
                <td>{target.sourceType === 'url' ? 'URL' : target.sourceType === 'upload' ? '上传' : '手动'}</td>
                <td>{target.counts.valid ?? 0} 条<span className="policy-sub-cell">版本 {target.versions?.length ?? 0}</span></td>
                <td>{target.usage.routingRuleCount + target.usage.accessRuleCount} 条规则<span className="policy-sub-cell">路由 {target.usage.routingRuleCount} · 访问 {target.usage.accessRuleCount}</span></td>
                <td><PolicyStatusBadge tone={status.tone}>{status.label}</PolicyStatusBadge></td>
                <td><div className="action-links">
                  <button type="button" className="link-button" onClick={() => setEditing(target)}>编辑</button>
                  {target.sourceType === 'url' ? <button type="button" className="link-button" onClick={() => void performRefresh(target)}>刷新</button> : null}
                  <button type="button" className="link-button link-button--danger" disabled={target.usage.routingRuleCount + target.usage.accessRuleCount > 0} onClick={() => void remove(target)}>删除</button>
                </div></td>
              </tr>
            })}</tbody>
          </table>
        </div>
      )}
    </section>
	{editing !== undefined ? <TargetListModal deviceID={deviceID} target={editing} onClose={() => setEditing(undefined)} onSaved={async () => { setEditing(undefined); setNotice(editing ? '目标列表已更新。' : '目标列表已创建。'); await reload() }} /> : null}
  </div>
}

export function TargetListModal({ deviceID, target, initialKind = 'domain', onClose, onSaved }: { deviceID: string; target: TargetList | null; initialKind?: 'domain' | 'ip'; onClose: () => void; onSaved: (target: TargetList) => Promise<void> }) {
  const [name, setName] = useState(target?.name ?? '')
  const [kind, setKind] = useState<'domain' | 'ip'>(target?.kind ?? initialKind)
  const [sourceType, setSourceType] = useState<SourceType>(target?.sourceType === 'url' || target?.sourceType === 'upload' ? target.sourceType : 'manual')
  const [url, setURL] = useState(target?.url ?? '')
  const [text, setText] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [schedule, setSchedule] = useState(target?.schedule && target.schedule !== 'manual' ? target.schedule : '7d')
  const [preview, setPreview] = useState<TargetListPreview | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const canPreview = sourceType === 'url' ? Boolean(url.trim()) : sourceType === 'manual' ? Boolean(text.trim()) : Boolean(file)

  const doPreview = async () => {
    if (!canPreview) return
    setPreviewing(true)
    setError(null)
    try {
      const input = sourceType === 'upload' ? (() => { const form = new FormData(); form.append('file', file as File); return form })() : sourceType === 'url' ? url.trim() : text
      setPreview(await previewTargetList(deviceID, kind, sourceType, input))
    } catch (previewError) {
      setError(previewError)
    } finally {
      setPreviewing(false)
    }
  }

  const submit = async () => {
    if (!name.trim() || (!target && !preview)) return
    setSaving(true)
    setError(null)
    try {
      const result = await saveTargetList(deviceID, { name: name.trim(), kind, sourceType, url: sourceType === 'url' ? url.trim() : undefined, schedule: sourceType === 'url' ? schedule : 'manual', enabled: true, revision: target?.revision ?? 0 }, target?.id, preview?.previewId)
      const id = jobID(result)
      if (id) await waitForPolicyJob(deviceID, id)
      if (!result.targetList) throw new Error('目标列表保存响应无效')
      await onSaved(result.targetList)
    } catch (saveError) {
      setError(saveError)
    } finally {
      setSaving(false)
    }
  }

  return <PolicyModal title={target ? '编辑目标列表' : '新增目标列表'} wide onClose={onClose} footer={<>
    <button type="button" className="toolbar-button" disabled={saving} onClick={onClose}>取消</button>
    <button type="button" className="primary-button" disabled={saving || !name.trim() || (!target && !preview)} onClick={() => void submit()}>{saving ? '正在保存…' : '保存'}</button>
  </>}>
    <div className="policy-form">
      {error ? <PolicyErrorDisplay error={error} /> : null}
      <PolicyField label="名称" htmlFor="target-list-name"><input id="target-list-name" className="settings-input" value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：视频站点" /></PolicyField>
      <PolicyField label="类型"><select className="select-control" value={kind} disabled={Boolean(target)} onChange={(event) => { setKind(event.target.value as 'domain' | 'ip'); setPreview(null) }}><option value="domain">域名列表</option><option value="ip">IP 列表</option></select></PolicyField>
      <PolicyField label="来源"><select className="select-control" value={sourceType} onChange={(event) => { setSourceType(event.target.value as SourceType); setPreview(null) }}><option value="manual">手动粘贴</option><option value="url">URL</option><option value="upload">上传文件</option></select></PolicyField>
      {sourceType === 'url' ? <PolicyField label="URL"><input className="settings-input" type="url" value={url} onChange={(event) => { setURL(event.target.value); setPreview(null) }} placeholder="https://example.com/list.txt" /></PolicyField> : sourceType === 'upload' ? <PolicyField label="文件"><input className="settings-input" type="file" accept=".txt,.list,.yaml,.yml,.csv" onChange={(event) => { setFile(event.target.files?.[0] ?? null); setPreview(null) }} /></PolicyField> : <PolicyField label="内容" hint={kind === 'ip' ? '每行一个 IP 或 CIDR。' : '每行一个域名；支持 exact / suffix 解析。'}><textarea className="settings-input policy-textarea" rows={8} value={text} onChange={(event) => { setText(event.target.value); setPreview(null) }} placeholder={kind === 'ip' ? '203.0.113.0/24' : 'example.com'} /></PolicyField>}
      {sourceType === 'url' ? <PolicyField label="刷新"><select className="select-control" value={schedule} onChange={(event) => setSchedule(event.target.value)}><option value="1h">每 1 小时</option><option value="6h">每 6 小时</option><option value="12h">每 12 小时</option><option value="24h">每天</option><option value="7d">每 7 天</option><option value="30d">每 30 天</option></select></PolicyField> : null}
      <button type="button" className="toolbar-button" disabled={!canPreview || previewing} onClick={() => void doPreview()}>{previewing ? '正在预览…' : '预览并校验'}</button>
      {preview ? <PolicyNotice tone={preview.errorSamples.length ? 'warn' : 'good'} title="预览结果">有效规则 {preview.validRules} 条{preview.errorSamples.length ? `，${preview.errorSamples.length} 条错误样例` : ''}。</PolicyNotice> : null}
    </div>
  </PolicyModal>
}
