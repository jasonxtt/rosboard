import { ContainerButton, ContainerFloat } from './ContainerUI'
import { useId, useRef, useState } from 'react'
import { DirectoryPicker } from './DirectoryPicker'
import type { DirectoryMutation } from './types'

/** Path combobox: clicking the input (or ▾) opens a bound directory tree as
 * a floating panel. Clicking a folder selects it live (entering it), so the
 * input always shows the folder you last clicked; typing a path reveals its
 * ancestors and Enter creates-and-selects a missing leaf folder. Manual path
 * input still works and the next open starts there. */
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
  /** retained for the field contract; the tree only ever offers directories */
  allowFiles?: boolean
  error?: string
  hint?: string
  disabled?: boolean
}) {
  void allowFiles
  const inputId = useId(),
    pickerId = useId()
  const anchor = useRef<HTMLDivElement>(null)
  const createRef = useRef<(() => void) | null>(null)
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
              if (e.key === 'Enter' && open) {
                e.preventDefault()
                e.stopPropagation()
                if (createRef.current) createRef.current()
                else close()
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
            value={value}
            createRef={createRef}
            onMutation={onMutation}
            onSelect={onChange}
            onClose={close}
          />
        </ContainerFloat>
      )}
    </>
  )
}
