import type { ReactNode } from 'react'

type GlassProps = {
  children: ReactNode
  className?: string
  /** hover lift + brighten for clickable cards (§3) */
  interactive?: boolean
  /** solid near-glass without backdrop-filter for dense list surfaces */
  solid?: boolean
}

export function Glass({ children, className = '', interactive = false, solid = false }: GlassProps) {
  const classes = [solid ? 'glass-solid' : 'glass', interactive ? 'glass-interactive' : '', className]
    .filter(Boolean)
    .join(' ')
  return <div className={classes}>{children}</div>
}
