import { useCallback, useEffect, useState } from 'react'
import {
  deleteAccessRule,
  loadAccessOverview,
  saveAccessRule,
  waitForAccessJob,
  type AccessOverview,
  type AccessRule,
  type AccessRuleDraft,
  type ApplicationPresetSelection,
  type PolicyTerminal,
  type Subject,
  type TargetList,
} from '../policy/canonical'
import { SubjectSelector, TargetSelector } from '../policy/Selectors'
import { TargetListModal } from '../policy/TargetLibraryPage'
import { PolicyEmptyState, PolicyErrorDisplay, PolicyField, PolicyModal, PolicyNotice, PolicyPreparing, PolicyStatusBadge, type StatusTone } from '../policy-routing/components'

const statusPresentation: Record<string, { tone: StatusTone; label: string }> = {
  applied: { tone: 'good', label: '已应用' }, applying: { tone: 'info', label: '正在应用' }, pending: { tone: 'warn', label: '待应用' },
  failed: { tone: 'bad', label: '应用失败' }, degraded: { tone: 'warn', label: '部分降级' }, disabled: { tone: 'neutral', label: '已停用' },
}

function jobID(result: { jobId?: string; job?: { id: string } }) { return result.jobId ?? result.job?.id ?? '' }
function rulePayload(rule: AccessRule) { return { id: rule.id, name: rule.name, subject: rule.subject, targetScope: rule.targetScope, targetListIds: rule.targetListIds, enabled: rule.enabled, revision: rule.revision } }

export function AccessControlPage({ deviceID, refreshNonce }: { deviceID: string; refreshNonce: number }) {
  const [overview, setOverview] = useState<AccessOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [editing, setEditing] = useState<AccessRule | null | undefined>(undefined)
  const [deleting, setDeleting] = useState<AccessRule | null>(null)

  const reload = useCallback(async () => {
    if (!deviceID) return
    try { setOverview(await loadAccessOverview(deviceID)); setError(null) } catch (loadError) { setError(loadError) } finally { setLoading(false) }
  }, [deviceID])
  useEffect(() => { setLoading(true); void reload() }, [reload, refreshNonce])
  useEffect(() => { const timer = window.setInterval(() => void reload(), 5000); return () => window.clearInterval(timer) }, [reload])
  useEffect(() => { if (!notice) return; const timer = window.setTimeout(() => setNotice(null), 5000); return () => window.clearTimeout(timer) }, [notice])

  const apply = async (result: { jobId?: string; job?: { id: string } }, message: string) => {
    const id = jobID(result)
    if (id) await waitForAccessJob(deviceID, id)
    setNotice(message)
    await reload()
  }

  if (loading && !overview) return <PolicyPreparing text="正在读取访问规则…" />
  if (!overview) return <>{error ? <PolicyErrorDisplay error={error} /> : <PolicyPreparing text="正在读取访问规则…" />}</>

  const targetByID = new Map(overview.targetLists.map((target) => [target.id, target]))
  const terminalByID = new Map(overview.terminals.map((terminal) => [terminal.id, terminal]))
  const activeJob = overview.job && !['committed', 'failed'].includes(overview.job.state) ? overview.job : undefined
  return <div className="policy-page access-page">
    {notice ? <PolicyNotice tone="good">{notice}</PolicyNotice> : null}
    {error ? <PolicyErrorDisplay error={error} /> : null}
    <details className="policy-advanced"><summary>访问控制说明</summary><div className="policy-advanced-body"><PolicyNotice tone="warn">{overview.boundary}</PolicyNotice></div></details>
    {activeJob ? <PolicyNotice tone="info">正在应用到 RouterOS，阶段 {activeJob.phase || activeJob.state}。</PolicyNotice> : null}
    <section className="panel policy-panel" aria-label="访问规则">
      <div className="policy-panel-head"><div><h3>访问规则</h3><p className="policy-hint">设备：{overview.device.name}</p></div><button type="button" className="primary-button" disabled={!overview.device.enabled} onClick={() => setEditing(null)}>新增规则</button></div>
      {overview.rules.length === 0 ? <PolicyEmptyState title="尚未配置访问规则" description="规则可以选择设备、互联网或可复用的目标列表。" action={<button type="button" className="primary-button" disabled={!overview.device.enabled} onClick={() => setEditing(null)}>新增规则</button>} /> : <div className="policy-table-scroll"><table className="data-table access-policy-table"><thead><tr><th>规则</th><th>Who</th><th>What</th><th>时间</th><th>状态</th><th>操作</th></tr></thead><tbody>{overview.rules.map((rule) => <AccessRuleRow key={rule.id} rule={rule} terminals={rule.subject.members.map((member) => terminalByID.get(member.terminalId)).filter((terminal): terminal is PolicyTerminal => Boolean(terminal))} targets={rule.targetListIds.map((id) => targetByID.get(id)).filter((target): target is TargetList => Boolean(target))} busy={Boolean(activeJob)} onEdit={() => setEditing(rule)} onDelete={() => setDeleting(rule)} onToggle={async () => { try { await apply(await saveAccessRule(deviceID, { ...rulePayload(rule), enabled: !rule.enabled }), rule.enabled ? '规则已停用。' : '规则已启用。') } catch (toggleError) { setError(toggleError) } }} />)}</tbody></table></div>}
    </section>
    {editing !== undefined ? <AccessRuleModal deviceID={deviceID} rule={editing} terminals={overview.terminals} targetLists={overview.targetLists} onClose={() => setEditing(undefined)} onSave={async (rule) => { await apply(await saveAccessRule(deviceID, rule), '访问规则已保存并应用。'); setEditing(undefined) }} /> : null}
    {deleting ? <PolicyModal title="删除访问规则" onClose={() => setDeleting(null)} footer={<><button type="button" className="toolbar-button" onClick={() => setDeleting(null)}>取消</button><button type="button" className="danger-button" onClick={() => void (async () => { try { await apply(await deleteAccessRule(deviceID, deleting.id, deleting.revision), '访问规则已删除。'); setDeleting(null) } catch (deleteError) { setError(deleteError) } })()}>删除</button></>}><p>确定删除“{deleting.name}”？删除后会同步清理对应的 RouterOS 规则。</p></PolicyModal> : null}
  </div>
}

function AccessRuleRow({ rule, terminals, targets, busy, onEdit, onDelete, onToggle }: { rule: AccessRule; terminals: PolicyTerminal[]; targets: TargetList[]; busy: boolean; onEdit: () => void; onDelete: () => void; onToggle: () => Promise<void> }) {
  const [toggling, setToggling] = useState(false)
  const status = statusPresentation[rule.status] ?? { tone: 'neutral' as const, label: rule.status || '未知' }
  const who = rule.subject.mode === 'all' ? '全部设备' : `${rule.subject.members.length} 台设备${rule.subject.prefixes.length ? ` + ${rule.subject.prefixes.length} 个地址范围` : ''}`
  const what = rule.targetScope === 'internet' ? '整个互联网' : targets.map((target) => target.name).join('、') || `${rule.targetListIds.length} 个目标列表`
  return <tr className={rule.enabled ? '' : 'disabled-row'}><td><strong>{rule.name}</strong><span className="policy-sub-cell">{rule.enabled ? '启用' : '停用'}</span></td><td>{who}<span className="policy-sub-cell">{terminals.map((terminal) => terminal.displayName || terminal.id).join('、') || '按手动地址'}</span></td><td>{what}<span className="policy-sub-cell">{rule.targetScope === 'internet' ? '局域网访问不受影响' : '共享目标列表（含应用规则）'}</span></td><td>始终<span className="policy-sub-cell">指定时间：即将支持</span></td><td><PolicyStatusBadge tone={status.tone}>{status.label}</PolicyStatusBadge>{rule.issues[0] ? <span className="policy-sub-cell">{rule.issues[0]}</span> : null}</td><td><div className="action-links"><button type="button" className="link-button" disabled={busy || toggling} onClick={() => { setToggling(true); void onToggle().finally(() => setToggling(false)) }}>{toggling ? '处理中…' : rule.enabled ? '停用' : '启用'}</button><button type="button" className="link-button" disabled={busy} onClick={onEdit}>编辑</button><button type="button" className="link-button link-button--danger" disabled={busy} onClick={onDelete}>删除</button></div></td></tr>
}

function AccessRuleModal({ deviceID, rule, terminals, targetLists, onClose, onSave }: { deviceID: string; rule: AccessRule | null; terminals: PolicyTerminal[]; targetLists: TargetList[]; onClose: () => void; onSave: (rule: AccessRuleDraft) => Promise<void> }) {
  const [availableTargetLists, setAvailableTargetLists] = useState<TargetList[]>(() => [...targetLists])
  const [name, setName] = useState(rule?.name ?? '')
  const [subject, setSubject] = useState<Subject>(rule?.subject ?? { mode: 'selected', members: [], prefixes: [] })
  const [targetScope, setTargetScope] = useState<'internet' | 'targets'>(rule?.targetScope ?? 'internet')
  const [targetListIds, setTargetListIds] = useState<string[]>(rule?.targetListIds ?? [])
  const enabled = rule?.enabled ?? true
  const [creatingTargetKind, setCreatingTargetKind] = useState<'domain' | 'ip' | null>(null)
  const [presetPresentations, setPresetPresentations] = useState<Array<ApplicationPresetSelection & { name: string }>>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const addTargetList = (target: TargetList) => {
    setAvailableTargetLists((current) => current.some((item) => item.id === target.id) ? current.map((item) => item.id === target.id ? target : item) : [...current, target])
    setTargetListIds((current) => current.includes(target.id) ? current : [...current, target.id])
    setCreatingTargetKind(null)
  }
  const submit = async () => {
    if (!name.trim() || (subject.mode === 'selected' && subject.members.length === 0 && subject.prefixes.length === 0) || (targetScope === 'targets' && targetListIds.length === 0)) return
    setSaving(true); setError(null)
    try {
      await onSave({ id: rule?.id ?? '', name: name.trim(), subject, targetScope, targetListIds: targetScope === 'targets' ? targetListIds : [], enabled, revision: rule?.revision ?? 0, ...(targetScope === 'targets' && presetPresentations.length ? { presetSelections: presetPresentations.filter((selection) => selection.previewId && selection.requestedKinds.length).map(({ name: _name, ...selection }) => selection) } : {}) })
    } catch (saveError) { setError(saveError) } finally { setSaving(false) }
  }
  return <>
    <PolicyModal title={rule ? '编辑访问规则' : '新增访问规则'} wide onClose={onClose} footer={<><button type="button" className="toolbar-button" disabled={saving} onClick={onClose}>取消</button><button type="button" className="primary-button" disabled={saving || !name.trim() || (subject.mode === 'selected' && !subject.members.length && !subject.prefixes.length) || (targetScope === 'targets' && !targetListIds.length)} onClick={() => void submit()}>{saving ? '正在保存…' : '保存'}</button></>}>
      <div className="policy-form">{error ? <PolicyErrorDisplay error={error} /> : null}<PolicyField label="规则名称" htmlFor="access-rule-name"><input id="access-rule-name" className="settings-input" value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：儿童娱乐限制" /></PolicyField><PolicyField label="Who · 受控对象"><SubjectSelector terminals={terminals} value={subject} onChange={setSubject} /></PolicyField><PolicyField label="What · 访问范围"><div className="policy-choice-list"><label className={`policy-choice${targetScope === 'internet' ? ' active' : ''}`}><input type="radio" checked={targetScope === 'internet'} onChange={() => setTargetScope('internet')} /><span><strong>整个互联网</strong><small>阻断互联网，局域网访问不受影响</small></span></label><label className={`policy-choice${targetScope === 'targets' ? ' active' : ''}`}><input type="radio" checked={targetScope === 'targets'} onChange={() => setTargetScope('targets')} /><span><strong>目标列表</strong><small>阻断目标库中的域名或 IP 列表</small></span></label></div>{targetScope === 'targets' ? <TargetSelector deviceID={deviceID} targetLists={availableTargetLists} selectedIDs={targetListIds} onChange={setTargetListIds} onPresetPresentationChange={setPresetPresentations} onCreateTargetList={setCreatingTargetKind} /> : null}</PolicyField><PolicyField label="时间"><label className="policy-choice active"><input type="radio" checked readOnly /><span><strong>始终生效</strong><small>指定时间控制将在后续版本支持</small></span></label><label className="policy-choice disabled"><input type="radio" disabled /><span><strong>指定时间（即将支持）</strong></span></label></PolicyField></div>
    </PolicyModal>
    {creatingTargetKind ? <TargetListModal deviceID={deviceID} target={null} initialKind={creatingTargetKind} onClose={() => setCreatingTargetKind(null)} onSaved={async (target) => { addTargetList(target) }} /> : null}
  </>
}
