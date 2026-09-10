import { useCallback, useEffect, useState } from 'react'
import { deleteRoutingRule, fetchRoutingContext, saveRoutingRule, waitForPolicyJob, type Egress, type PolicyTerminal, type RoutingRule, type TargetList, type TrafficIngressScope } from './canonical'
import { RoutingRuleWizard } from './RoutingRuleWizard'
import { PolicyEmptyState, PolicyErrorDisplay, PolicyModal, PolicyNotice, PolicyPreparing, PolicyStatusBadge, type StatusTone } from '../policy-routing/components'

type RoutingContext = { egresses: Egress[]; rules: RoutingRule[]; targetLists: TargetList[]; terminals: PolicyTerminal[]; trafficIngress: TrafficIngressScope }
type EditorState = { rule: RoutingRule | null; egress: Egress | null }

function jobID(value: { jobId?: string; job?: { id: string } }) { return value.jobId ?? value.job?.id ?? '' }

function subjectLabel(rule: RoutingRule) {
  if (rule.sourceScope?.kind === 'interface') return `接口 ${rule.sourceScope.name || '未命名'}`
  if (rule.sourceScope?.kind === 'interface-list') return `接口列表 ${rule.sourceScope.name || '未命名'}`
  if (rule.sourceScope?.kind === 'all') return '全部来源（安全门延后）'
  if (rule.subject.mode === 'all') return `${[...rule.ingress.interfaceLists, ...rule.ingress.interfaces].join('、') || '入口'} 全部`
  const prefix = `${rule.subject.members.length} 台设备${rule.subject.prefixes.length ? ` + ${rule.subject.prefixes.length} 个地址范围` : ''}`
  return rule.subject.mode === 'excluded' ? `${[...rule.ingress.interfaceLists, ...rule.ingress.interfaces].join('、') || '入口'} 排除 ${prefix}` : prefix
}

function egressStatus(rule: RoutingRule, egress?: Egress): { tone: StatusTone; label: string } {
  if (!egress) return { tone: 'bad', label: '出口缺失' }
  if (!rule.enabled) return { tone: 'neutral', label: '已停用' }
  if (egress.pendingDeletion) return { tone: 'warn', label: '出口待删除' }
  if (!egress.enabled) return { tone: 'warn', label: '出口已停用' }
  if (!egress.applied) return { tone: 'warn', label: '待应用' }
  return { tone: 'good', label: '已启用' }
}

/** 出口摘要：每个启用的协议族一行（IPv4 在前）。
 *  下一跳模式（wanSource=next-hop）没有接口名，显示网关 IP；
 *  接口模式显示「接口·网关」，点对点无 IP 只显示接口名。 */
function egressLines(egress?: Egress): string[] {
  if (!egress) return ['出口缺失']
  const familyOrder = (family: string) => (family === 'ipv4' ? 0 : family === 'ipv6' ? 1 : 2)
  const lines = [...egress.families]
    .filter((family) => family.enabled)
    .sort((left, right) => familyOrder(left.family) - familyOrder(right.family))
    .map((family) => {
      const iface = family.wanInterface.trim()
      const gateway = family.gateway.trim()
      if (!iface || family.wanSource === 'next-hop') return gateway
      return gateway ? `${iface}·${gateway}` : iface
    })
    .filter((line) => line.length > 0)
  return lines.length ? lines : ['未配置出口']
}

export function RoutingRulesPage({ deviceID, refreshNonce }: { deviceID: string; refreshNonce: number }) {
  const [context, setContext] = useState<RoutingContext | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [editing, setEditing] = useState<EditorState | undefined>(undefined)
  const [deleting, setDeleting] = useState<RoutingRule | null>(null)

  const reload = useCallback(async () => {
    try { setContext(await fetchRoutingContext(deviceID)); setError(null) } catch (loadError) { setError(loadError) } finally { setLoading(false) }
  }, [deviceID])
  useEffect(() => { setLoading(true); void reload() }, [reload, refreshNonce])
  useEffect(() => { const timer = window.setInterval(() => void reload(), 5000); return () => window.clearInterval(timer) }, [reload])
  useEffect(() => { if (!notice) return; const timer = window.setTimeout(() => setNotice(null), 5000); return () => window.clearTimeout(timer) }, [notice])

  if (loading && !context) return <PolicyPreparing text="正在读取策略路由…" />
  if (!context) return <>{error ? <PolicyErrorDisplay error={error} /> : <PolicyPreparing text="正在读取策略路由…" />}</>

  const egressByID = new Map(context.egresses.map((egress) => [egress.id, egress]))
  const targetByID = new Map(context.targetLists.map((target) => [target.id, target]))
  const openNewRule = () => setEditing({ rule: null, egress: null })
  return <div className="policy-page">
    {notice ? <PolicyNotice tone="good">{notice}</PolicyNotice> : null}
    {error ? <PolicyErrorDisplay error={error} /> : null}
    <section className="panel policy-panel" aria-label="策略路由">
      <div className="policy-panel-head"><div><h3>策略路由</h3><p className="policy-hint">每条策略一次配置来源、目标和出口；出口配置会在后台自动复用或安全复制。</p></div><button type="button" className="primary-button" onClick={openNewRule}>新增策略</button></div>
      {context.rules.length === 0 ? <PolicyEmptyState title="尚未配置策略" description="将来源、访问目标和出口组合成一条策略，即可生成可审阅的 RouterOS 变更计划。" action={<button type="button" className="primary-button" onClick={openNewRule}>新增策略</button>} /> : <div className="policy-table-scroll"><table className="data-table"><thead><tr><th>名称</th><th>来源</th><th>目标</th><th>出口</th><th>状态</th><th>操作</th></tr></thead><tbody>{context.rules.map((rule) => {
        const egress = egressByID.get(rule.egressId)
        const status = egressStatus(rule, egress)
        const targetNames = rule.targetListIds.map((id) => targetByID.get(id)?.name ?? id).join('、')
        return <RoutingRuleRow key={rule.id} rule={rule} subject={subjectLabel(rule)} targets={targetNames || '—'} egressLines={egressLines(egress)} status={status} onEdit={() => setEditing({ rule, egress: egress ?? null })} onDelete={() => setDeleting(rule)} onToggle={async () => {
          try {
            const result = await saveRoutingRule(deviceID, { ...rule, enabled: !rule.enabled })
            const id = jobID(result)
            if (id) await waitForPolicyJob(deviceID, id)
            setNotice(rule.enabled ? '策略已停用。' : '策略已启用。')
            await reload()
          } catch (toggleError) { setError(toggleError) }
        }} />
      })}</tbody></table></div>}
    </section>
    {editing ? <RoutingRuleWizard deviceID={deviceID} context={context} rule={editing.rule} egress={editing.egress} onClose={() => setEditing(undefined)} onSaved={async () => { setNotice('策略已应用。'); await reload() }} /> : null}
    {deleting ? <PolicyModal title="删除路由策略" onClose={() => setDeleting(null)} footer={<><button type="button" className="toolbar-button" onClick={() => setDeleting(null)}>取消</button><button type="button" className="danger-button" onClick={() => void (async () => { try { const result = await deleteRoutingRule(deviceID, deleting.id, deleting.revision); const id = jobID(result); if (id) await waitForPolicyJob(deviceID, id); setDeleting(null); setNotice('策略已删除。'); await reload() } catch (deleteError) { setError(deleteError) } })()}>删除</button></>}><p>确定删除“{deleting.name}”？删除后会在变更计划中清理对应的 RouterOS 规则。</p></PolicyModal> : null}
  </div>
}

function RoutingRuleRow({ rule, subject, targets, egressLines: egress, status, onEdit, onDelete, onToggle }: { rule: RoutingRule; subject: string; targets: string; egressLines: string[]; status: { tone: StatusTone; label: string }; onEdit: () => void; onDelete: () => void; onToggle: () => Promise<void> }) {
  const [toggling, setToggling] = useState(false)
  return <tr className={rule.enabled ? '' : 'disabled-row'}><td><strong>{rule.name}</strong><span className="policy-sub-cell">Priority {rule.priority}</span></td><td>{subject}</td><td>{targets}</td><td>{egress[0]}{egress.length > 1 ? <span className="policy-sub-cell">{egress.slice(1).join('；')}</span> : null}</td><td><PolicyStatusBadge tone={status.tone}>{status.label}</PolicyStatusBadge></td><td><div className="action-links"><button type="button" className="link-button" disabled={toggling} onClick={() => { setToggling(true); void onToggle().finally(() => setToggling(false)) }}>{toggling ? '处理中…' : rule.enabled ? '停用' : '启用'}</button><button type="button" className="link-button" disabled={toggling} onClick={onEdit}>编辑</button><button type="button" className="link-button link-button--danger" disabled={toggling} onClick={onDelete}>删除</button></div></td></tr>
}
