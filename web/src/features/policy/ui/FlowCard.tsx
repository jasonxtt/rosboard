import type { ReactNode } from 'react'
import { Badge } from '../../../ui/Badge'
import type { BadgeTone } from '../../../ui/Badge'
import { FlowNodes } from '../../../ui/FlowNodes'
import type { FlowNode } from '../../../ui/FlowNodes'
import { Toggle } from '../../../ui/Toggle'
import { Tooltip } from '../../../ui/Tooltip'

export type FlowCardStatus = { tone: BadgeTone; label: string; tip?: string }

type FlowCardProps = {
  title: ReactNode
  /** muted secondary text next to the title, e.g. 「优先级 100 · 修订 3」 */
  meta?: ReactNode
  status: FlowCardStatus
  enabled: boolean
  onToggle?: (enabled: boolean) => void
  onEdit?: () => void
  onDelete?: () => void
  busy?: boolean
  source: FlowNode[]
  target: FlowNode[]
  exit: FlowNode
  exitTone?: 'accent' | 'err'
  /** 待应用 warn border + 「审查并应用 →」 (§9.2) */
  pending?: boolean
  onReview?: () => void
  /** extra rows under the flow line (issues, member states, …) */
  footer?: ReactNode
}

/** 流向卡 (§9.2): header (名称 + 状态徽章 + toggle + 操作) + 来源 → 目标 → 出口 flow line. */
export function FlowCard({
  title,
  meta,
  status,
  enabled,
  onToggle,
  onEdit,
  onDelete,
  busy = false,
  source,
  target,
  exit,
  exitTone = 'accent',
  pending = false,
  onReview,
  footer,
}: FlowCardProps) {
  const badge = <Badge tone={status.tone}>{status.label}</Badge>
  return (
    <div className={`glass pol-flow-card${pending ? ' pol-flow-pending' : ''}${enabled ? '' : ' pol-flow-disabled'}`}>
      <div className="pol-flow-head">
        <strong className="pol-flow-title">{title}</strong>
        {meta ? <span className="pol-flow-meta">{meta}</span> : null}
        <span className="pol-flow-head-right">
          {status.tip ? <Tooltip tip={status.tip}>{badge}</Tooltip> : badge}
          {onToggle ? <Toggle checked={enabled} disabled={busy} onChange={onToggle} label={enabled ? '停用规则' : '启用规则'} /> : null}
          {onEdit ? (
            <button type="button" className="icon-btn" aria-label="编辑" title="编辑" disabled={busy} onClick={onEdit}>
              ✎
            </button>
          ) : null}
          {onDelete ? (
            <button type="button" className="icon-btn" aria-label="删除" title="删除" disabled={busy} onClick={onDelete}>
              🗑
            </button>
          ) : null}
        </span>
      </div>
      <div className="pol-flow-line">
        <FlowNodes source={source} target={target} exit={exit} exitTone={exitTone} />
        {pending && onReview ? (
          <button type="button" className="pol-review-link" onClick={onReview}>
            审查并应用 →
          </button>
        ) : null}
      </div>
      {footer ? <div className="pol-flow-footer">{footer}</div> : null}
    </div>
  )
}
