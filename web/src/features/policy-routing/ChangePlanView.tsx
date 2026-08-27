import { useMemo, useState } from 'react'
import type { PolicyApplyResult, PolicyPlan, PolicyPlanIssue, PolicyPlanOperation } from './types'
import { applyPlan } from './api'
import {
  PolicyErrorDisplay,
  PolicyMetadata,
  PolicyNotice,
  PolicyStatusBadge,
  type StatusTone,
} from './components'
import {
  policyAckCodeLabel,
  policyOperationActionLabel,
  policyOwnershipLabel,
  policyPlanKindLabel,
  policyProofStatusLabel,
} from './format'
import { policyErrorDescription } from './planIssues'
import { PolicyApiError } from './types'

export function ChangePlanView({
  deviceID,
  plan,
  onApplied,
  onCancel,
  onRegenerate,
  compact = false,
}: {
  deviceID: string
  plan: PolicyPlan
  onApplied: (result?: PolicyApplyResult) => void
  onCancel: () => void
  onRegenerate?: () => void
  compact?: boolean
}) {
  const [acks, setAcks] = useState<Set<string>>(() => new Set(plan.acknowledgements.filter((ack) => ack.accepted).map((ack) => ack.code)))
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const requiredAcks = plan.acknowledgements.filter((a) => a.required)
  const allAcksAccepted = requiredAcks.every((a) => acks.has(a.code))
  const planBlocked = plan.pendingReview || plan.state === 'blocked' || plan.state === 'pending_review'
  const canApply = allAcksAccepted && !plan.blockers.length && !planBlocked

  const toggleAck = (code: string) => {
    setAcks((prev) => {
      const next = new Set(prev)
      if (next.has(code)) next.delete(code)
      else next.add(code)
      return next
    })
  }

  const operationsByAction = useMemo(() => {
    const groups: Array<{ key: string; label: string; operations: PolicyPlanOperation[] }> = [
      { key: 'reuse', label: '复用 / 接管（不修改用户对象）', operations: [] },
      { key: 'create', label: '创建', operations: [] },
      { key: 'modify', label: '修改 / 启停 / 引用', operations: [] },
      { key: 'move', label: '移动（顺序调整）', operations: [] },
      { key: 'delete', label: '删除', operations: [] },
    ]
    for (const op of plan.operations) {
      const key = op.action === 'reuse' || op.action === 'adopt'
        ? 'reuse'
        : op.action === 'create'
          ? 'create'
          : op.action === 'move'
            ? 'move'
            : op.action === 'delete'
              ? 'delete'
              : 'modify'
      groups.find((group) => group.key === key)?.operations.push(op)
    }
    return groups.filter((group) => group.operations.length)
  }, [plan.operations])

  const handleApply = async () => {
    setError(null)
    setApplying(true)
    try {
      const result = await applyPlan(deviceID, plan.planID, {
        acknowledgements: Array.from(acks),
      })
      onApplied(result)
    } catch (e) {
      if (e instanceof PolicyApiError) setError(policyErrorDescription(e))
      else setError(e instanceof Error ? e.message : '应用失败')
    } finally {
      setApplying(false)
    }
  }

  const displayedOperationsByAction = compact
    ? operationsByAction.filter((group) => ['create', 'modify', 'move'].includes(group.key))
    : operationsByAction

  const applyButton = (
    <button type="button" className="primary-button policy-apply-button" disabled={!canApply || applying} onClick={handleApply}>
      {applying ? '应用中…' : '应用计划'}
    </button>
  )
  const cancelButton = (
    <button type="button" className="primary-button policy-cancel-button" disabled={applying} onClick={onCancel}>取消</button>
  )

  return (
    <div className={`policy-plan${compact ? ' policy-plan--compact' : ''}`}>
      {!compact ? (
        <div className="policy-plan-summary">
          <PolicyMetadata entries={[
            ['计划类型', policyPlanKindLabel[plan.kind] ?? plan.kind],
            ['计划状态', plan.state],
            ['创建时间', formatTime(plan.createdAt)],
            ['过期时间', plan.expiresAt ? formatTime(plan.expiresAt) : '—'],
            ['期望版本', String(plan.desiredRevision)],
            ['实际指纹', plan.actualFingerprint || '—'],
            ['计划哈希', plan.planHash ? `${plan.planHash.slice(0, 16)}…` : '—'],
          ]} />
          <div className="policy-plan-counts">
            {Object.entries(plan.summary).map(([key, count]) => {
              if (!count) return null
              const label = policyOperationActionLabel[key] ?? key
              return <span key={key} className="policy-status policy-status-neutral">{label}: {count}</span>
            })}
          </div>
          {plan.resourceEstimate?.resourceWarning ? (
            <PolicyNotice tone="warn" title="资源警告">
              有效域名 {plan.resourceEstimate.validDomains} 条 · 最大列表 {plan.resourceEstimate.largestSource} 条
              {plan.resourceEstimate.scheduledShrink ? ' · 包含计划性缩减' : ''}
            </PolicyNotice>
          ) : null}
        </div>
      ) : null}

      {plan.blockers.length ? (
        <IssueBlock title="阻断项" issues={plan.blockers} tone="bad" />
      ) : null}
      {plan.familyBlockers?.length ? (
        <IssueBlock title="地址族阻断" issues={plan.familyBlockers} tone="bad" />
      ) : null}
      {plan.warnings.length ? (
        <IssueBlock title="警告" issues={plan.warnings} tone="warn" />
      ) : null}

      {plan.pendingReview ? (
        <PolicyNotice tone="warn" title="计划处于待审阅状态">
          结构性变化或来源缩减需要重新生成可交互预览后才能应用。
        </PolicyNotice>
      ) : null}

      {!compact && plan.capabilities && Object.keys(plan.capabilities).length ? (
        <div className="policy-plan-capabilities">
          <h4>能力矩阵</h4>
          <div className="policy-capability-list">
            {Object.entries(plan.capabilities).map(([key, status]) => (
              <span key={key} className="policy-status policy-status-neutral">
                {key}: {policyProofStatusLabel[status] ?? status}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {displayedOperationsByAction.length ? (
        <div className="policy-operations">
          <h4>操作列表</h4>
          <div className="policy-operation-list">
            {displayedOperationsByAction.map((group) => (
              <div key={group.key} className="policy-operation-group">
                <div className="policy-operation-head">
                  <strong>{group.label}</strong>
                  <span className="policy-operation-count">{group.operations.length}</span>
                </div>
                <div className="policy-operation-list">
                  {groupOperationsByMenu(group.operations).map((typeGroup) => (
                    <OperationTypeGroup
                      key={`${group.key}:${typeGroup.menu}`}
                      menu={typeGroup.menu}
                      operations={typeGroup.operations}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {requiredAcks.length ? (
        <div className="policy-acknowledge-block">
          <h4>需要确认</h4>
          {requiredAcks.map((ack) => (
            <label key={ack.code} className="policy-ack-item">
              <input type="checkbox" checked={acks.has(ack.code)} onChange={() => toggleAck(ack.code)} />
              <span>{policyAckCodeLabel[ack.code] ?? ack.code}</span>
              {ack.required ? <span className="policy-ack-required-badge">必选</span> : null}
            </label>
          ))}
        </div>
      ) : null}

      {error ? <PolicyErrorDisplay error={error} /> : null}

      <div className="policy-form-actions">
        {compact ? cancelButton : applyButton}
        {onRegenerate ? (
          <button type="button" className="toolbar-button" disabled={applying} onClick={onRegenerate}>重新生成</button>
        ) : null}
        {compact ? applyButton : cancelButton}
      </div>
    </div>
  )
}

type OperationTypeGroupData = {
  menu: string
  operations: PolicyPlanOperation[]
}

function groupOperationsByMenu(operations: PolicyPlanOperation[]): OperationTypeGroupData[] {
  const groups = new Map<string, PolicyPlanOperation[]>()
  for (const operation of operations) {
    const menu = operation.menu || 'RouterOS 对象'
    const groupedOperations = groups.get(menu)
    if (groupedOperations) groupedOperations.push(operation)
    else groups.set(menu, [operation])
  }
  return Array.from(groups, ([menu, groupedOperations]) => ({ menu, operations: groupedOperations }))
}

function OperationTypeGroup({ menu, operations }: OperationTypeGroupData) {
  const [showAll, setShowAll] = useState(false)
  const [expandedOperations, setExpandedOperations] = useState<Set<number>>(new Set())
  const visibleOperations = showAll ? operations : operations.slice(0, 1)

  return (
    <div className="policy-operation-type-group">
      <div className="policy-operation-head">
        <span className="policy-operation-menu">{menu}</span>
        <span className="policy-operation-count">{operations.length}</span>
      </div>
      <div className="policy-operation-list">
        {visibleOperations.map((op) => (
          <div key={op.seq} className="policy-operation">
            <span className="policy-operation-seq">{op.seq}</span>
            {op.logicalID ? <span className="policy-operation-target">{op.logicalID}</span> : null}
            {op.family ? <span className="policy-status policy-status-neutral">{op.family}</span> : null}
            {op.ownership ? <span className="policy-status policy-status-neutral">{policyOwnershipLabel[op.ownership] ?? op.ownership}</span> : null}
            <button
              type="button"
              className="link-button"
              onClick={() => setExpandedOperations((current) => {
                const next = new Set(current)
                if (next.has(op.seq)) next.delete(op.seq)
                else next.add(op.seq)
                return next
              })}
            >
              {expandedOperations.has(op.seq) ? '收起字段差异' : '查看字段差异'}
            </button>
            {expandedOperations.has(op.seq) ? <OperationDetails operation={op} /> : null}
          </div>
        ))}
      </div>
      {operations.length > 1 ? (
        <button
          type="button"
          className="link-button policy-operation-more"
          onClick={() => setShowAll((expanded) => !expanded)}
          aria-expanded={showAll}
        >
          {showAll ? '收起' : `显示更多（还有 ${operations.length - 1} 条）`}
        </button>
      ) : null}
    </div>
  )
}

function IssueBlock({ title, issues, tone }: { title: string; issues: PolicyPlanIssue[]; tone: StatusTone }) {
  return (
    <div className={`policy-issue-block policy-issue-${tone}`}>
      <h4>{title} ({issues.length})</h4>
      <ul>
        {issues.map((issue, i) => (
          <li key={i}>
            <span className="policy-issue-scope">
              {issue.status ? <PolicyStatusBadge tone={tone}>{policyProofStatusLabel[issue.status] ?? issue.status}</PolicyStatusBadge> : null}
              {issue.family ? <span>{issue.family}</span> : null}
              {issue.egressID ? <span>出口:{issue.egressID.slice(0, 8)}</span> : null}
              {issue.logicalID ? <span>{issue.logicalID}</span> : null}
            </span>
            <span>{issue.reason}</span>
            {issue.code ? <code className="policy-issue-code">{issue.code}</code> : null}
          </li>
        ))}
      </ul>
    </div>
  )
}

function OperationDetails({ operation }: { operation: PolicyPlanOperation }) {
  const keys = Array.from(new Set([
    ...Object.keys(operation.before ?? {}),
    ...Object.keys(operation.after ?? {}),
  ]))
  return (
    <div className="policy-operation-details">
      {operation.anchor ? (
        <div className="policy-operation-meta">
          位置：{operation.anchor.relation === 'after' ? '插入到锚点之后' : '插入到锚点之前'}
          {operation.anchor.neighborID ? ` · ${operation.anchor.neighborID}` : ''}
        </div>
      ) : null}
      {operation.compensation ? (
        <div className="policy-operation-meta">回滚：{operation.compensation.reason || operation.compensation.action || '可回滚'}</div>
      ) : null}
      {keys.length ? (
        <div className="policy-table-scroll">
          <table className="data-table policy-diff-table">
            <thead><tr><th>字段</th><th>变更前</th><th>变更后</th></tr></thead>
            <tbody>
              {keys.map((key) => {
                const before = operation.before?.[key] ?? '—'
                const after = operation.after?.[key] ?? '—'
                return <tr key={key} className={before !== after ? 'policy-diff-changed' : ''}><td>{key}</td><td>{formatFieldValue(before)}</td><td>{formatFieldValue(after)}</td></tr>
              })}
            </tbody>
          </table>
        </div>
      ) : <span className="policy-hint">该操作没有字段差异。</span>}
    </div>
  )
}

function formatFieldValue(value: unknown): string {
  if (typeof value === 'string') return value
  if (value === undefined || value === null) return '—'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function formatTime(value: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}
