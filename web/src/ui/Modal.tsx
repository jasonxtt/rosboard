import { useEffect, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

type ModalProps = {
  open: boolean
  onClose: () => void
  title?: ReactNode
  children: ReactNode
  footer?: ReactNode
  /** defaults to the 640px §7 dialog width */
  maxWidth?: number
}

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

/** Centered glass dialog; becomes a bottom sheet below 768px. Esc closes, focus is trapped. */
export function Modal({ open, onClose, title, children, footer, maxWidth = 640 }: ModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const restoreFocusRef = useRef<HTMLElement | null>(null)
  // onClose identity changes on every parent render (polling pages); keep it in
  // a ref so the open effect below runs once per open transition, not per render.
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    if (!open) return
    restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const dialog = dialogRef.current
    // Autofocus only when focus is not already inside the dialog (e.g. first
    // open); never yank it out of an input the user is typing in.
    if (dialog && !dialog.contains(document.activeElement)) {
      const focusables = dialog.querySelectorAll<HTMLElement>(FOCUSABLE)
      ;(focusables.length > 0 ? focusables[0] : dialog).focus({ preventScroll: true })
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        onCloseRef.current()
        return
      }
      if (event.key !== 'Tab' || !dialog) return
      const items = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE))
      if (items.length === 0) {
        event.preventDefault()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement
      if (event.shiftKey && (active === first || !dialog.contains(active))) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && active === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown, true)
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKeyDown, true)
      document.body.style.overflow = previousOverflow
      // preventScroll: restoring focus must not scroll the page behind the
      // dialog (the trigger often sits at the top of a long page).
      restoreFocusRef.current?.focus({ preventScroll: true })
    }
  }, [open])

  if (!open) return null
  return createPortal(
    <div
      className="modal-overlay"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div
        className="glass modal"
        style={{ maxWidth }}
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === 'string' ? title : undefined}
        ref={dialogRef}
        tabIndex={-1}
      >
        {title ? (
          <header className="modal-header">
            <h2>{title}</h2>
            <button type="button" className="icon-btn modal-close" aria-label="关闭" onClick={onClose}>
              ✕
            </button>
          </header>
        ) : null}
        <div className="modal-body">{children}</div>
        {footer ? <footer className="modal-footer">{footer}</footer> : null}
      </div>
    </div>,
    document.body,
  )
}
