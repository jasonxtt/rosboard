import { ContainerButton, ContainerFloat } from './ContainerUI'
import { useId, useRef, useState } from 'react'
import { DirectoryPicker } from './DirectoryPicker'
import type { DirectoryMutation } from './types'

/** Path combobox: clicking the input (or ▾) opens a bound Files browser as a
 * floating panel. Clicking a folder enters it and selects it live, so the
 * input always shows the folder you last clicked; typing a path manually
 * still works and the next open starts there. */
export function DirectoryField({
  deviceId,
  writable,
  purpose,
  label,
  value,
  onChange,
  onMutation,
  allowFiles = false,
  error,
  hint,
  disabled = false,
}: {
  deviceId: string
  writable: boolean
  purpose: string
  label: string
  value: string
  onChange: (value: string) => void
  onMutation?: (result: DirectoryMutation) => void
  allowFiles?: boolean
  error?: string
  hint?: string
  disabled?: boolean
}) {
  const inputId = useId(),
    pickerId = useId()
  const anchor = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false),
    [session, setSession] = useState(0)
  const openPicker = () => {
    if (open) return
    setSession((s) => s + 1)
    setOpen(true)
  }
  const close = () => {
    if (!open) return
    setOpen(false)
    const input = anchor.current?.querySelector('input')
    if (input instanceof window.HTMLInputElement)
      input.focus({ preventScroll: true })
  }
  return (
    <>
      <div className="ct-field">
        <label htmlFor={inputId}>{label}</label>
        <div className="ct-directory-input" ref={anchor}>
          <input
            id={inputId}
            value={value}
            disabled={disabled}
            aria-invalid={!!error}
            aria-expanded={open}
            aria-controls={open ? pickerId : undefined}
            onChange={(e) => onChange(e.target.value)}
            onClick={() => {
              if (!disabled) openPicker()
            }}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                e.preventDefault()
                openPicker()
              }
              if (e.key === 'Escape' && open) {
                e.preventDefault()
                e.stopPropagation()
                close()
              }
            }}
          />
          <ContainerButton
            type="button"
            aria-label={`浏览${purpose}`}
            aria-expanded={open}
            aria-controls={open ? pickerId : undefined}
            disabled={disabled}
            onClick={open ? close : openPicker}
          >
            ▾
          </ContainerButton>
        </div>
        {hint && <small>{hint}</small>}
        {error && <strong role="alert">{error}</strong>}
      </div>
      {open && (
        <ContainerFloat anchor={anchor.current} onClose={close}>
          <DirectoryPicker
            key={session}
            id={pickerId}
            deviceId={deviceId}
            writable={writable}
            purpose={purpose}
            initialPath={value}
            allowFiles={allowFiles}
            onMutation={onMutation}
            onSelect={onChange}
            onClose={close}
          />
        </ContainerFloat>
      )}
    </>
  )
}
