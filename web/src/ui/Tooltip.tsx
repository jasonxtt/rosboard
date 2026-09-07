import type { ReactNode } from 'react'

type TooltipProps = {
  tip: string
  children: ReactNode
  className?: string
}

/** Lightweight CSS tooltip; keep for short hints, not interactive content. */
export function Tooltip({ tip, children, className = '' }: TooltipProps) {
  return (
    <span className={`tooltip ${className}`.trim()} data-tip={tip}>
      {children}
    </span>
  )
}
