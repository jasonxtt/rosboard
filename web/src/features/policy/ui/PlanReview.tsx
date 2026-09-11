import { useMemo, useState } from 'react'
import { Badge } from '../../../ui/Badge'
import type { BadgeTone } from '../../../ui/Badge'
import { Button } from '../../../ui/Button'
import { Modal } from '../../../ui/Modal'
import { CanonicalPolicyError, applyPolicyPlan, type PlanEnvelope, type PlanIssue, type PlanOperation, type PolicyPlan } from '../canonical'
import { formatDateTime } from '../../../lib/format'
import { JobProgress } from './JobProgress'
import { errorMessage } from '../../../lib/api'
import { Notice } from './Notice'
import { PLAN_ACTION_LABELS, planDomainLabel, planKindLabel, planMenuLabel } from './labels'

export type PlanSummaryEntry = [string, string]

type PlanReviewBodyProps = {
  deviceID: string
  envelope: PlanEnvelope
  /** optional 「本次配置」 summary rows (wizard) */
  summary?: PlanSummaryEntry[]
  onApplied: () => void | Promise<void>
  onBack?: () => void
  /** regenerate the plan (plan_expired / stale_plan recovery) */
  onRepreview?: () => void | Promise<void>
  /** wizard uses this to block close/step jumps while an apply is running */
  onBusyChange?: (busy: boolean) => void
}

const ACTION_TONES: Record<string, { tone: BadgeTone; symbol: string }> = {
  create: { tone: 'ok', symbol: '+' },
  enable: { tone: 'ok', symbol: '+' },
  reference_add: { tone: 'ok', symbol: '+' },
  patch: { tone: 'warn', symbol: '~' },
  move: { tone: 'warn', symbol: '~' },
  delete: { tone: 'err', symbol: '−' },
  disable: { tone: 'err', symbol: '−' },
  reference_remove: { tone: 'err', symbol: '−' },
  reuse: { tone: 'accent', symbol: '↻' },
  adopt: { tone: 'accent', symbol: '↻' },
}

function formatFieldValue(value: unknown): string {
  if (value === undefined || value === null) return '—'
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function operationLabel(operation: PlanOperation): string {
  for (const fields of [operation.after, operation.before]) {
    const comment = fields?.comment
    if (typeof comment !== 'string' || !comment.trim()) continue
    const separator = comment.indexOf(' | ')
    return separator >= 0 ? comment.slice(separator + 3) : comment
  }
  if ((operation.logicalID ?? '').includes('preset:')) return '应用预设'
  return planMenuLabel(operation.menu)
}

function operationDiff(operation: PlanOperation): Array<[string, string, string]> {
  const before = operation.before ?? {}
  const after = operation.after ?? {}
  const rows: Array<[string, string, string]> = []
  for (const key of Array.from(new Set([...Object.keys(before), ...Object.keys(after)]))) {
    if (key === 'comment') continue
    const beforeText = formatFieldValue(before[key])
    const afterText = formatFieldValue(after[key])
    if (beforeText !== afterText) rows.push([key, beforeText, afterText])
  }
  return rows
}

function IssueRows({ title, issues, tone }: { title: string; issues: PlanIssue[]; tone: 'err' | 'warn' }) {
  if (!issues.length) return null
  return (
    <div className={`pol-issue-block pol-issue-${tone}`}>
      <h4>
        {title}（{issues.length}）
      </h4>
      <ul>
        {issues.map((issue, index) => (
          <li key={`${issue.code}:${index}`}>
            <span>{issue.reason || '（无详细说明）'}</span>
            {issue.family ? <small> · {issue.family}</small> : null}
          </li>
        ))}
      </ul>
    </div>
  )
}

function SummaryChips({ plan }: { plan: PolicyPlan }) {
  const order: Array<[string, BadgeTone, string]> = [
    ['create', 'ok', '+'],
    ['patch', 'warn', '~'],
    ['delete', 'err', '−'],
    ['enable', 'ok', '启用'],
    ['disable', 'err', '停用'],
    ['move', 'warn', '移动'],
    ['reuse', 'accent', '复用'],
    ['adopt', 'accent', '接管'],
    ['referenceAdd', 'ok', '引用'],
    ['referenceRemove', 'err', '解除引用'],
  ]
  const chips = order.filter(([key]) => (plan.summary[key] ?? 0) > 0)
  if (!chips.length) return null
  return (
    <div className="pol-plan-chips">
      {chips.map(([key, tone, label]) => (
        <span key={key} className={`pol-plan-chip pol-plan-chip-${tone}`}>
          <b>{label}</b> {plan.summary[key]}
        </span>
      ))}
    </div>
  )
}

function OperationRow({ operation }: { operation: PlanOperation }) {
  const [expanded, setExpanded] = useState(false)
  const action = ACTION_TONES[operation.action] ?? { tone: 'neutral' as BadgeTone, symbol: '•' }
  const diff = useMemo(() => operationDiff(operation), [operation])
  return (
    <div className="pol-op">
      <span className={`pol-op-symbol pol-op-symbol-${action.tone}`} aria-hidden="true">
        {action.symbol}
      </span>
      <span className="pol-op-label">{operationLabel(operation)}</span>
      {operation.family ? <Badge tone="neutral">{operation.family}</Badge> : null}
      <Badge tone={action.tone}>{PLAN_ACTION_LABELS[operation.action] ?? operation.action}</Badge>
      {diff.length ? (
        <button type="button" className="link-button pol-op-diff-toggle" aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>
          {expanded ? '收起差异' : `${diff.length} 项差异`}
        </button>
      ) : null}
      {expanded && diff.length ? (
        <div className="pol-op-diff">
          {diff.slice(0, 10).map(([key, beforeText, afterText]) => (
            <div className="pol-op-diff-row" key={key}>
              <span className="pol-op-diff-key">{key}</span>
              <span className="pol-op-diff-before">{beforeText}</span>
              <span className="pol-op-diff-arrow" aria-hidden="true">
                →
              </span>
              <span className="pol-op-diff-after">{afterText}</span>
            </div>
          ))}
          {diff.length > 10 ? <small className="faint">…还有 {diff.length - 10} 项差异</small> : null}
        </div>
      ) : null}
    </div>
  )
}

/** Plan review body (§9.2): metadata, blockers, warnings, acks, grouped operations, apply → job. */
export function PlanReviewBody({ deviceID, envelope, summary, onApplied, onBack, onRepreview, onBusyChange }: PlanReviewBodyProps) {
  const plan = envelope.plan
  const [acks, setAcks] = useState<Set<string>>(() => new Set(plan.acknowledgements.filter((ack) => ack.accepted).map((ack) => ack.code)))
  const [applying, setApplying] = useState(false)
  const [applyJobId, setApplyJobId] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [errorCode, setErrorCode] = useState<string | null>(null)
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

  const toggleAck = (code: string) =>
    setAcks((current) => {
      const next = new Set(current)
      if (next.has(code)) next.delete(code)
      else next.add(code)
      return next
    })

  const toggleGroup = (label: string) =>
    setExpandedGroups((current) => {
      const next = new Set(current)
      if (next.has(label)) next.delete(label)
      else next.add(label)
      return next
    })

  const apply = async () => {
    if (applying || applyJobId) return
    setApplying(true)
    onBusyChange?.(true)
    setError(null)
    setErrorCode(null)
    try {
      const result = await applyPolicyPlan(deviceID, envelope.planId || plan.planID, Array.from(acks), envelope.planHash || plan.planHash)
      const jobId = result.jobId ?? result.job?.id ?? ''
      if (jobId) setApplyJobId(jobId)
      else {
        onBusyChange?.(false)
        await onApplied()
      }
    } catch (applyError) {
      setError(errorMessage(applyError, '计划应用失败'))
      setErrorCode(applyError instanceof CanonicalPolicyError ? (applyError.code ?? null) : null)
      onBusyChange?.(false)
    } finally {
      setApplying(false)
    }
  }

  if (applyJobId) {
    return (
      <div className="pol-plan">
        <JobProgress
          deviceID={deviceID}
          domain="policy"
          jobId={applyJobId}
          label="正在应用变更计划"
          onCommitted={() => void onApplied()}
          onFailed={(job) => {
            setApplyJobId('')
            setError(job.error || 'RouterOS 应用失败')
            setErrorCode('job_failed')
            onBusyChange?.(false)
          }}
        />
      </div>
    )
  }

  const planGone = errorCode === 'plan_expired' || errorCode === 'stale_plan' || errorCode === 'plan_not_found'

  return (
    <div className="pol-plan">
      {summary?.length ? (
        <div className="pol-meta-grid">
          {summary.map(([label, value]) => (
            <div className="pol-kv" key={label}>
              <span>{label}</span>
              <b>{value}</b>
            </div>
          ))}
        </div>
      ) : null}
      <div className="pol-meta-grid">
        <div className="pol-kv">
          <span>计划类型</span>
          <b>{planKindLabel(plan.kind)}</b>
        </div>
        <div className="pol-kv">
          <span>作用域</span>
          <b>{planDomainLabel(plan.domain)}</b>
        </div>
        <div className="pol-kv">
          <span>目标修订</span>
          <b>{plan.desiredRevision}</b>
        </div>
        <div className="pol-kv">
          <span>有效期至</span>
          <b>{plan.expiresAt ? formatDateTime(plan.expiresAt) : '—'}</b>
        </div>
        <div className="pol-kv">
          <span>计划哈希</span>
          <b>{plan.planHash ? `${plan.planHash.slice(0, 16)}…` : '—'}</b>
        </div>
      </div>
      <SummaryChips plan={plan} />
      <IssueRows title="阻断项" issues={plan.blockers} tone="err" />
      <IssueRows title="协议族阻断" issues={plan.familyBlockers} tone="err" />
      <IssueRows title="警告" issues={plan.warnings} tone="warn" />
      {plan.blockers.some((issue) => issue.code === 'access_internet_egress_unavailable') ? (
        <Notice tone="warn" title="需要指定互联网出口">
          访问控制的「整个互联网」规则无法自动确认互联网出口，请到「访问控制」页点击「同步到 RouterOS」并选择出口接口。
        </Notice>
      ) : null}
      {plan.pendingReview ? (
        <Notice tone="warn" title="计划需要重新审阅">
          结构性变化或来源缩减尚未获得应用确认。
        </Notice>
      ) : null}
      {operationGroups.length ? (
        <div className="pol-plan-ops">
          <h4 className="pol-plan-ops-title">变更清单（按 RouterOS 菜单位置分组）</h4>
          {operationGroups.map((group) => {
            const expanded = expandedGroups.has(group.label)
            const visible = expanded ? group.operations : group.operations.slice(0, 3)
            return (
              <div key={group.label} className="pol-op-group">
                <div className="pol-op-group-head">
                  <strong>{planMenuLabel(group.label)}</strong>
                  <Badge tone="neutral">{group.operations.length} 项</Badge>
                </div>
                {visible.map((operation) => (
                  <OperationRow key={`${group.label}:${operation.seq}`} operation={operation} />
                ))}
                {group.operations.length > 3 ? (
                  <button type="button" className="link-button" aria-expanded={expanded} onClick={() => toggleGroup(group.label)}>
                    {expanded ? '收起' : `查看更多（剩余 ${group.operations.length - 3} 条）`}
                  </button>
                ) : null}
              </div>
            )
          })}
        </div>
      ) : (
        <Notice tone="info">没有需要写入 RouterOS 的变更。</Notice>
      )}
      {required.length ? (
        <div className="pol-acks">
          <h4 className="pol-plan-ops-title">应用前确认</h4>
          {required.map((ack) => (
            <label key={ack.code} className="pol-check pol-ack">
              <input type="checkbox" checked={acks.has(ack.code)} disabled={applying} onChange={() => toggleAck(ack.code)} />
              <span>{ack.code}</span>
              <Badge tone="warn">必选</Badge>
            </label>
          ))}
        </div>
      ) : null}
      {error ? (
        <Notice
          tone={planGone ? 'warn' : 'err'}
          action={
            planGone && onRepreview ? (
              <button type="button" className="link-button" onClick={() => void onRepreview()}>
                重新生成预览
              </button>
            ) : undefined
          }
        >
          {errorCode === 'job_conflict' ? '有正在执行的 RouterOS 任务，请等待完成后再应用。' : error}
          {planGone ? '（计划已过期或状态已变化，请重新生成预览）' : ''}
        </Notice>
      ) : null}
      <div className="pol-plan-actions">
        {onBack ? (
          <Button disabled={applying} onClick={onBack}>
            返回修改
          </Button>
        ) : null}
        <Button variant="primary" loading={applying} disabled={!ready} onClick={() => void apply()}>
          确认并应用
        </Button>
      </div>
    </div>
  )
}

type PlanReviewModalProps = {
  open: boolean
  deviceID: string
  envelope: PlanEnvelope | null
  onClose: () => void
  onApplied: () => void | Promise<void>
  onRepreview?: () => void | Promise<void>
}

/** Standalone 计划审查 modal for pending desired-state changes (§9.2). */
export function PlanReviewModal({ open, deviceID, envelope, onClose, onApplied, onRepreview }: PlanReviewModalProps) {
  if (!envelope) return null
  return (
    <Modal open={open} onClose={onClose} title="审查变更计划" maxWidth={720} persistent>
      <PlanReviewBody deviceID={deviceID} envelope={envelope} onApplied={onApplied} onBack={onClose} onRepreview={onRepreview} />
    </Modal>
  )
}
