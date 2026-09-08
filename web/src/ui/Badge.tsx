import type { ReactNode } from 'react'

export type BadgeTone = 'ok' | 'warn' | 'err' | 'accent' | 'neutral'

type BadgeProps = {
  tone?: BadgeTone
  /** prepend a color dot (status is never color-only — keep the label) */
  dot?: boolean
  children: ReactNode
  className?: string
}

/** Pill badge: soft background + solid text (§7 badge). */
export function Badge({ tone = 'neutral', dot = false, children, className = '' }: BadgeProps) {
  return (
    <span className={`badge badge-${tone} ${className}`.trim()}>
      {dot ? <span className="badge-dot" aria-hidden="true" /> : null}
      {children}
    </span>
  )
}
