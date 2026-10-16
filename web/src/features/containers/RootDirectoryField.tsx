import { useId } from 'react'

export function RootDirectoryField({
  value,
  error,
  hint,
  pickerID,
  open,
  onChange,
  onOpen,
  onClose,
}: {
  value: string
  error?: string
  hint: string
  pickerID: string
  open: boolean
  onChange: (value: string) => void
  onOpen: () => void
  onClose: () => void
}) {
  const id = useId()
  return (
    <div className="ct-field">
      <label htmlFor={id}>容器运行目录（root-dir）</label>
      <div className="ct-root-directory-input">
        <input
          id={id}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={!!error}
          aria-expanded={open}
          aria-controls={open ? pickerID : undefined}
          onClick={onOpen}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              onOpen()
            }
            if (e.key === 'Escape' && open) {
              e.preventDefault()
              onClose()
            }
          }}
        />
        <button
          type="button"
          aria-label="浏览容器运行目录"
          aria-expanded={open}
          aria-controls={open ? pickerID : undefined}
          onClick={open ? onClose : onOpen}
        >
          ▾
        </button>
      </div>
      <small>{hint}。点击输入框或右侧箭头选择，也可直接输入。</small>
      {error && <strong role="alert">{error}</strong>}
    </div>
  )
}
