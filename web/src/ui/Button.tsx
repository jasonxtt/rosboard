import type { ReactNode } from 'react'

export type ButtonVariant = 'primary' | 'ghost' | 'danger'

type ButtonProps = {
  variant?: ButtonVariant
  size?: 'md' | 'sm'
  type?: 'button' | 'submit'
  disabled?: boolean
  loading?: boolean
  onClick?: () => void
  children: ReactNode
  className?: string
  title?: string
}

/** §7 buttons: primary = brand gradient, ghost = glass + stroke, danger = soft red. */
export function Button({
  variant = 'ghost',
  size = 'md',
  type = 'button',
  disabled = false,
  loading = false,
  onClick,
  children,
  className = '',
  title,
}: ButtonProps) {
  const classes = ['btn', `btn-${variant}`, size === 'sm' ? 'btn-sm' : '', className].filter(Boolean).join(' ')
  return (
    <button type={type} className={classes} disabled={disabled || loading} onClick={onClick} title={title}>
      {loading ? '处理中…' : children}
    </button>
  )
}
