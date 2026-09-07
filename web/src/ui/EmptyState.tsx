import type { ReactNode } from 'react'
import { Button } from './Button'

type EmptyStateProps = {
  icon?: ReactNode
  title: string
  description?: string
  actionLabel?: string
  onAction?: () => void
}

/** Empty state: icon + one-line explanation + one primary action (§7). */
export function EmptyState({ icon = '◌', title, description, actionLabel, onAction }: EmptyStateProps) {
  return (
    <div className="empty-state">
      <span className="empty-icon" aria-hidden="true">
        {icon}
      </span>
      <strong>{title}</strong>
      {description ? <p>{description}</p> : null}
      {actionLabel && onAction ? (
        <Button variant="primary" onClick={onAction}>
          {actionLabel}
        </Button>
      ) : null}
    </div>
  )
}
