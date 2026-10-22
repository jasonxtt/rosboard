import type { HTMLAttributes, ReactNode } from 'react'

type CardProps = Omit<HTMLAttributes<HTMLDivElement>, 'title' | 'onClick'> & {
  as?: 'div' | 'section' | 'footer'
  title?: ReactNode
  sub?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
  /** hover lift + brighten for clickable cards (§3) */
  interactive?: boolean
  onClick?: () => void
}

/** Signature glass card with an optional header row (title + actions). */
export function Card({ as: Element = 'div', title, sub, actions, children, className = '', interactive = false, onClick, ...props }: CardProps) {
  const classes = [
    'glass',
    'card',
    interactive || onClick ? 'glass-interactive' : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')
  const body = (
    <>
      {title || actions ? (
        <header className="card-header">
          {title ? <h3>{title}</h3> : null}
          {sub ? <span className="card-sub">{sub}</span> : null}
          {actions ? <div className="card-actions">{actions}</div> : null}
        </header>
      ) : null}
      {children}
    </>
  )
  if (onClick) {
    return (
      <Element {...props} className={classes} role="button" tabIndex={0} onClick={onClick} onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          onClick()
        }
      }}>
        {body}
      </Element>
    )
  }
  return <Element {...props} className={classes}>{body}</Element>
}
