import type { ButtonHTMLAttributes } from 'react'

export type ButtonVariant = 'primary' | 'ghost' | 'danger'

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: 'md' | 'sm'
  disabled?: boolean
  loading?: boolean
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
  ...props
}: ButtonProps) {
  const classes = ['btn', `btn-${variant}`, size === 'sm' ? 'btn-sm' : '', className].filter(Boolean).join(' ')
  return (
    <button {...props} type={type} className={classes} disabled={disabled || loading} onClick={onClick} title={title}>
      {loading ? '处理中…' : children}
    </button>
  )
}
