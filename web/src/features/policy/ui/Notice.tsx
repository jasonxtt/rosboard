import type { ReactNode } from 'react'

type NoticeProps = {
  tone?: 'err' | 'warn' | 'info' | 'ok'
  title?: ReactNode
  children: ReactNode
  /** right-aligned action (link-style button content) */
  action?: ReactNode
}

/** Inline status strip for banners, form errors and validation hints. */
export function Notice({ tone = 'info', title, children, action }: NoticeProps) {
  return (
    <div className={`pol-notice pol-notice-${tone}`} role={tone === 'err' ? 'alert' : undefined}>
      <span className="pol-notice-dot" aria-hidden="true" />
      <div className="pol-notice-body">
        {title ? <strong>{title}</strong> : null}
        <div className="pol-notice-text">{children}</div>
      </div>
      {action ? <div className="pol-notice-action">{action}</div> : null}
    </div>
  )
}
