import { useMemo, useState } from 'react'
import { applyPolicyPlan, waitForPolicyJob, type PlanEnvelope, type PlanIssue, type PlanOperation } from './canonical'
import { PolicyErrorDisplay, PolicyMetadata, PolicyNotice, PolicyStatusBadge, type StatusTone } from '../policy-routing/components'

const actionLabel: Record<string, string> = { create: '创建', patch: '修改', delete: '删除', move: '移动', disable: '停用', enable: '启用', reuse: '复用', adopt: '接管', reference_add: '建立引用', reference_remove: '解除引用' }

export type PolicyPlanSummary = { entries: Array<[string, string]> }

export function PolicyPlanPreview({ deviceID, envelope, summary, onApplied, onBack }: { deviceID: string; envelope: PlanEnvelope; summary?: PolicyPlanSummary; onApplied: () => void; onBack: () => void }) {
  const plan = envelope.plan
  const [acks, setAcks] = useState<Set<string>>(() => new Set(plan.acknowledgements.filter((ack) => ack.accepted).map((ack) => ack.code)))
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(() => new Set())
  const required = plan.acknowledgements.filter((ack) => ack.required)
  const ready = !plan.blockers.length && !plan.familyBlockers.length && !plan.pendingReview && required.every((ack) => acks.has(ack.code))
  const operationGroups = useMemo(() => {
    const groups = new Map<string, PlanOperation[]>()
    for (const operation of plan.operations) {
      const key = operation.menu || operation.phase || 'RouterOS 对象'
      const current = groups.get(key)
      if (current) current.push(operation)
      else groups.set(key, [operation])
    }
    return Array.from(groups, ([label, operations]) => ({ label, operations }))
  }, [plan.operations])

  const toggleAck = (code: string) => setAcks((current) => {
    const next = new Set(current)
    if (next.has(code)) next.delete(code)
    else next.add(code)
    return next
  })

  const toggleOperationGroup = (label: string) => setExpandedGroups((current) => {
    const next = new Set(current)
    if (next.has(label)) next.delete(label)
    else next.add(label)
    return next
  })

  const apply = async () => {
    setApplying(true)
    setError(null)
    try {
		const result = await applyPolicyPlan(deviceID, envelope.planId || plan.planID, Array.from(acks), envelope.planHash || plan.planHash)
      if (result.jobId) await waitForPolicyJob(deviceID, result.jobId)
      onApplied()
    } catch (applyError) {
      setError(applyError)
    } finally {
      setApplying(false)
    }
  }

  return <div className="policy-plan policy-plan--compact">
    {summary ? <><h4>本次配置</h4><PolicyMetadata entries={summary.entries} /></> : null}
    <PolicyMetadata entries={[
      ['计划类型', plan.kind || '—'],
      ['计划状态', plan.state || '—'],
      ['目标修订', String(plan.desiredRevision)],
      ['操作数量', String(plan.operations.length)],
      ['执行组', String(plan.executionGroups.length)],
      ['计划哈希', plan.planHash ? `${plan.planHash.slice(0, 16)}…` : '—'],
    ]} />
    {plan.blockers.length ? <IssueBlock title="阻断项" issues={plan.blockers} tone="bad" /> : null}
    {plan.familyBlockers.length ? <IssueBlock title="地址族阻断" issues={plan.familyBlockers} tone="bad" /> : null}
    {plan.warnings.length ? <IssueBlock title="警告" issues={plan.warnings} tone="warn" /> : null}
    {plan.pendingReview ? <PolicyNotice tone="warn" title="计划需要重新审阅">结构性变化或来源缩减尚未获得应用确认。</PolicyNotice> : null}
    {operationGroups.length ? <div className="policy-operations"><h4>变更清单</h4>{operationGroups.map((group) => { const expanded = expandedGroups.has(group.label); const visibleOperations = expanded ? group.operations : group.operations.slice(0, 1); return <div key={group.label} className="policy-operation-type-group"><div className="policy-operation-head"><strong>{group.label}</strong><span className="policy-operation-count">{group.operations.length}</span></div><div className="policy-operation-list">{visibleOperations.map((operation) => <OperationRow key={`${group.label}:${operation.seq}`} operation={operation} />)}{group.operations.length > 1 ? <button type="button" className="link-button policy-operation-more" aria-expanded={expanded} onClick={() => toggleOperationGroup(group.label)}>{expanded ? '收起' : `查看更多（剩余 ${group.operations.length - 1} 条）`}</button> : null}</div></div> })}</div> : <PolicyNotice tone="info">没有需要写入 RouterOS 的变更。</PolicyNotice>}
    {required.length ? <div className="policy-acknowledge-block"><h4>应用前确认</h4>{required.map((ack) => <label key={ack.code} className="policy-ack-item"><input type="checkbox" checked={acks.has(ack.code)} onChange={() => toggleAck(ack.code)} /><span>{ack.code}</span><span className="policy-ack-required-badge">必选</span></label>)}</div> : null}
    {error ? <PolicyErrorDisplay error={error} /> : null}
    <div className="policy-form-actions"><button type="button" className="toolbar-button" disabled={applying} onClick={onBack}>返回修改</button><button type="button" className="primary-button" disabled={!ready || applying} onClick={() => void apply()}>{applying ? '正在应用…' : '确认并应用'}</button></div>
  </div>
}

function OperationRow({ operation }: { operation: PlanOperation }) {
  return <div className="policy-operation"><span className="policy-operation-seq">{operation.seq}</span><span className="policy-operation-target">{operationLabel(operation)}</span>{operation.family ? <PolicyStatusBadge tone="neutral">{operation.family}</PolicyStatusBadge> : null}<span className="policy-status policy-status-info">{actionLabel[operation.action] ?? operation.action}</span></div>
}

function operationLabel(operation: PlanOperation) {
  for (const fields of [operation.after, operation.before]) {
    const comment = fields?.comment
    if (typeof comment !== 'string' || !comment.trim()) continue
    const separator = comment.indexOf(' | ')
    return separator >= 0 ? comment.slice(separator + 3) : comment
  }
  if ((operation.logicalID || '').includes('preset:')) return '应用规则'
  const menuLabels: Record<string, string> = {
    'routing/table': '策略专用路由表',
    'ip/route': 'IPv4 路由',
    'ipv6/route': 'IPv6 路由',
    'routing/rule': '策略路由规则',
    'ip/firewall/mangle': 'IPv4 流量标记',
    'ipv6/firewall/mangle': 'IPv6 流量标记',
  }
  return menuLabels[operation.menu || ''] || 'RouterOS 对象'
}

function IssueBlock({ title, issues, tone }: { title: string; issues: PlanIssue[]; tone: StatusTone }) {
  return <div className={`policy-issue-block policy-issue-${tone}`}><h4>{title} ({issues.length})</h4><ul>{issues.map((issue, index) => <li key={`${issue.code}:${index}`}><span>{issue.reason || issue.code}</span>{issue.family ? <small> · {issue.family}</small> : null}</li>)}</ul></div>
}
