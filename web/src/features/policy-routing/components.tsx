import { useEffect, useRef, useState } from 'react'

// ---- Icon ----

export type PolicyIconName = 'check' | 'alert' | 'info' | 'refresh' | 'eye' | 'eyeOff' | 'shield' | 'copy' | 'more' | 'close' | 'chevronDown'

export function PolicyIcon({ name }: { name: PolicyIconName }) {
  const paths: Record<PolicyIconName, React.ReactNode> = {
    check: <><circle cx="12" cy="12" r="9" /><path d="m8 12 2.5 2.5L16 9" /></>,
    alert: <><path d="M12 3 2.5 20h19L12 3Z" /><path d="M12 9v5m0 3h.01" /></>,
    info: <><circle cx="12" cy="12" r="9" /><path d="M12 11v6m0-10h.01" /></>,
    refresh: <><path d="M20 11a8 8 0 1 0-2.3 5.7" /><path d="M20 4v7h-7" /></>,
    eye: <><path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12Z" /><circle cx="12" cy="12" r="3" /></>,
    eyeOff: <><path d="m3 3 18 18" /><path d="M10.6 6.2A10.8 10.8 0 0 1 12 6c6.5 0 10 6 10 6a18 18 0 0 1-2.1 2.8M6.5 6.5C3.5 8.3 2 12 2 12s3.5 6 10 6c1.8 0 3.3-.5 4.6-1.2" /><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2" /></>,
    shield: <><path d="M12 3 4 7v5c0 5 3.4 8 8 9 4.6-1 8-4 8-9V7l-8-4Z" /><path d="m9 12 2 2 4-4" /></>,
    copy: <><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15V5a2 2 0 0 1 2-2h10" /></>,
    more: <><circle cx="12" cy="5" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="19" r="1.5" /></>,
    close: <path d="M6 6l12 12M6 18 18 6" />,
    chevronDown: <path d="m6 9 6 6 6-6" />,
  }
  return <svg className="policy-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>
}

// ---- Status badge ----

export type StatusTone = 'good' | 'bad' | 'warn' | 'info' | 'neutral'

export function PolicyStatusBadge({ tone, children }: { tone: StatusTone; children: React.ReactNode }) {
  return <span className={`policy-status policy-status-${tone}`}>{children}</span>
}

// ---- Empty state ----

export function PolicyEmptyState({ title, description, action }: { title: string; description?: string; action?: React.ReactNode }) {
  return (
    <div className="policy-empty-state">
      <h4>{title}</h4>
      {description ? <p>{description}</p> : null}
      {action ? <div className="policy-empty-action">{action}</div> : null}
    </div>
  )
}

// ---- Error display ----

export function PolicyErrorDisplay({ error }: { error: unknown }) {
  if (!error) return null
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div className="policy-notice policy-notice-error" role="alert">
      <PolicyIcon name="alert" />
      <div className="policy-notice-body">
        <strong>{message}</strong>
      </div>
    </div>
  )
}

// ---- Notification banner ----

export function PolicyNotice({ tone, title, children }: { tone: StatusTone; title?: string; children?: React.ReactNode }) {
  return (
    <div className={`policy-notice policy-notice-${tone}`}>
      <PolicyIcon name={tone === 'good' ? 'check' : tone === 'info' || tone === 'neutral' ? 'info' : 'alert'} />
      <div className="policy-notice-body">
        {title ? <strong>{title}</strong> : null}
        {children}
      </div>
    </div>
  )
}

// ---- Form field ----

export function PolicyField({ label, htmlFor, error, hint, children }: { label: string; htmlFor?: string; error?: string | null; hint?: string; children: React.ReactNode }) {
  return (
    <div className="policy-field">
      <label className="policy-field-label" htmlFor={htmlFor}>{label}</label>
      {children}
      {error ? <p className="policy-field-error" role="alert">{error}</p> : null}
      {hint && !error ? <p className="policy-hint">{hint}</p> : null}
    </div>
  )
}

// ---- Admin password input ----

export function PolicyPasswordInput({ value, onChange, className, placeholder }: { value: string; onChange: (v: string) => void; className?: string; placeholder?: string }) {
  const [visible, setVisible] = useState(false)
  return (
    <span className="policy-password-input">
      <input
        type={visible ? 'text' : 'password'}
        className={className ?? 'settings-input'}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        autoComplete="current-password"
      />
      <button type="button" className="policy-toggle-button" onClick={() => setVisible((v) => !v)} aria-label={visible ? '隐藏密码' : '显示密码'}>
        <PolicyIcon name={visible ? 'eyeOff' : 'eye'} />
      </button>
    </span>
  )
}

// ---- Modal dialog ----

export function PolicyModal({ title, subtitle, wide, closeDisabled = false, onClose, header, children, footer }: { title: string; subtitle?: React.ReactNode; wide?: boolean; closeDisabled?: boolean; onClose: () => void; header?: React.ReactNode; children: React.ReactNode; footer?: React.ReactNode }) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const previousFocus = useRef<HTMLElement | null>(null)
  const onCloseRef = useRef(onClose)
  const closeDisabledRef = useRef(closeDisabled)
  onCloseRef.current = onClose
  closeDisabledRef.current = closeDisabled

  // 焦点初始化只在挂载时执行一次：父组件轮询重渲染会生成新的 onClose 引用，
  // 若把它放入依赖，effect 重跑会把焦点抢回弹窗内第一个可聚焦元素（关闭按钮），
  // 用户正在输入时输入框会莫名失焦。
  useEffect(() => {
    previousFocus.current = document.activeElement as HTMLElement | null
    const focusable = () => Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('button, input, select, textarea, [href], [tabindex]:not([tabindex="-1"])') ?? []).filter((element) => !element.hasAttribute('disabled'))
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (!closeDisabledRef.current) onCloseRef.current()
        return
      }
      if (e.key !== 'Tab') return
      const elements = focusable()
      if (!elements.length) return
      const first = elements[0]
      const last = elements[elements.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    requestAnimationFrame(() => focusable()[0]?.focus())
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      previousFocus.current?.focus()
    }
  }, [])

  return (
    <div className="dialog-backdrop" role="presentation">
      <div className={`remark-modal policy-modal${wide ? ' policy-modal--wide' : ''}`} ref={dialogRef} role="dialog" aria-modal="true" aria-label={title} tabIndex={-1}>
        <div className="dialog-head">
          <div>
            <h3>{title}</h3>
            {subtitle ? <p className="policy-modal-subtitle">{subtitle}</p> : null}
          </div>
          <button type="button" className="close-button" disabled={closeDisabled} onClick={onClose}>关闭</button>
        </div>
        {header ? <div className="policy-modal-header-addon">{header}</div> : null}
        <div className="remark-modal-body policy-modal-body">{children}</div>
        {footer ? <div className="remark-modal-actions policy-modal-footer">{footer}</div> : null}
      </div>
    </div>
  )
}

// ---- Wizard step indicator ----

export function PolicyWizardSteps({ steps, current, unlockedThrough = steps.length - 1, planStale = false, disabled = false, onJump }: { steps: string[]; current: number; unlockedThrough?: number; planStale?: boolean; disabled?: boolean; onJump?: (index: number) => void }) {
  return (
    <ol className="policy-wizard-steps" aria-label="向导步骤">
      {steps.map((step, i) => {
        const done = i < unlockedThrough
        const active = i === current
        const locked = i > unlockedThrough
        const stale = i === steps.length - 1 && planStale
        const className = [active ? 'active' : '', done ? 'done' : '', locked ? 'locked' : '', stale ? 'stale' : ''].filter(Boolean).join(' ')
        const stateLabel = locked ? '，仅查看，完成前面的步骤后可编辑' : stale ? '，配置已修改，需要更新预览' : ''
        return (
          <li key={step} className={className} aria-current={active ? 'step' : undefined}>
            <button type="button" disabled={disabled} aria-label={`${i + 1}. ${step}${stateLabel}`} onClick={() => onJump?.(i)}>
              <span className="step-number">{i + 1}</span><span>{step}</span>
            </button>
          </li>
        )
      })}
    </ol>
  )
}

// ---- Pagination controls ----

export function PolicyPagination({ pageIndex, pageCount, onPrev, onNext, loading }: { pageIndex: number; pageCount: number; onPrev: () => void; onNext: () => void; loading?: boolean }) {
  return (
    <div className="policy-pagination">
      <button type="button" className="toolbar-button" disabled={pageIndex === 0 || loading} onClick={onPrev}>上一页</button>
      <span>{pageIndex + 1} / {Math.max(1, pageCount)}</span>
      <button type="button" className="toolbar-button" disabled={pageIndex >= pageCount - 1 || loading} onClick={onNext}>下一页</button>
    </div>
  )
}

// ---- Metadata dl ----

export function PolicyMetadata({ entries }: { entries: Array<[string, React.ReactNode]> }) {
  return (
    <dl className="policy-meta">
      {entries.map(([key, value]) => (
        <div key={key}>
          <dt>{key}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

// ---- Copy button ----

export function PolicyCopyButton({ text, label }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      if (window.isSecureContext && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text)
      } else {
        const ta = document.createElement('textarea')
        ta.value = text
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        ta.remove()
      }
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch { /* ignore */ }
  }
  return (
    <button type="button" className="policy-copy toolbar-button" onClick={copy}>
      <PolicyIcon name="copy" />
      {copied ? '已复制' : (label ?? '复制')}
    </button>
  )
}

// ---- More actions dropdown ----

export function PolicyMoreMenu({ label, items }: { label?: string; items: Array<{ label: string; onClick: () => void; danger?: boolean }> }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  return (
    <div className="policy-more-menu" ref={ref}>
      <button type="button" className="pill pill--pad-sm" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {label ?? '更多操作'}
      </button>
      {open ? (
        <div className="policy-more-menu-list" role="menu">
          {items.map((item) => (
            <button key={item.label} type="button" role="menuitem" className="policy-more-menu-item" onClick={() => { setOpen(false); item.onClick() }}>
              {item.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

// ---- Preparing hint ----

export function PolicyPreparing({ text }: { text?: string }) {
  return <span className="policy-hint">{text ?? '准备中…'}</span>
}
