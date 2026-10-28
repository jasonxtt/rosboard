import { createContext, useContext, useEffect, useId, useRef, type ButtonHTMLAttributes, type ComponentType, type HTMLAttributes, type ReactNode } from 'react'

export type ContainerButtonProps = ButtonHTMLAttributes<HTMLButtonElement>
export type ContainerSurfaceProps = Pick<HTMLAttributes<HTMLDivElement>, 'className' | 'children' | 'role' | 'aria-label' | 'id'> & { as?: 'div' | 'section' | 'footer' }
export type ContainerStatusProps = { status: string; children: ReactNode }
export type ContainerFloatProps = {
  /** element to anchor the floating layer to; null while measuring */
  anchor: HTMLElement | null
  onClose: () => void
  children: ReactNode
}
export type ContainerDialogProps = {
  title: string
  className?: string
  onClose: () => void
  /** block dismissal while an operation is in flight */
  busy?: boolean
  /** wide dialogs host the long create/edit form */
  wide?: boolean
  children: ReactNode
}
export type ContainerPresentation = {
  Button: ComponentType<ContainerButtonProps>
  Surface: ComponentType<ContainerSurfaceProps>
  Status: ComponentType<ContainerStatusProps>
  tableClassName: string
  /** Optional floating layer for popover panels (directory browser).
   * Shells without one render the panel inline in the document flow. */
  Float?: ComponentType<ContainerFloatProps>
  /** Optional modal dialog layer. Shells without one fall back to the
   * native <dialog> implementation below. */
  Dialog?: ComponentType<ContainerDialogProps>
}
const Presentation = createContext<ContainerPresentation>({
  tableClassName: 'data-table',
  Button: ({ type = 'button', ...props }) => <button type={type} {...props} />,
  Surface: ({ as: Element = 'div', className = '', ...props }) => <Element className={`ct-card ${className}`} {...props} />,
  Status: ({ status, children }) => <span className={`ct-state ct-state-${status}`}>{children}</span>,
})
export const ContainerPresentationProvider = Presentation.Provider
export function ContainerButton(props: ContainerButtonProps) {
  const { Button } = useContext(Presentation)
  return <Button {...props} />
}
export function ContainerSurface(props: ContainerSurfaceProps) {
  const { Surface } = useContext(Presentation)
  return <Surface {...props} />
}
export function ContainerStatus(props: ContainerStatusProps) {
  const { Status } = useContext(Presentation)
  return <Status {...props} />
}

export function ContainerTable({ children }: { children: ReactNode }) {
  const { tableClassName } = useContext(Presentation)
  return <table className={tableClassName}>{children}</table>
}

/** Floating panel host: shells provide their popover layer; the fallback
 * keeps the panel inline (Compact document flow). */
export function ContainerFloat({ anchor, onClose, children }: ContainerFloatProps) {
  const { Float } = useContext(Presentation)
  if (Float) return <Float anchor={anchor} onClose={onClose}>{children}</Float>
  return <>{children}</>
}

/** Modal dialog host: shells provide their dialog layer; the fallback uses
 * the native top-layer <dialog> (Compact). */
export function ContainerDialog(props: ContainerDialogProps) {
  const { Dialog } = useContext(Presentation)
  if (Dialog) return <Dialog {...props} />
  return <NativeContainerDialog {...props} />
}

function NativeContainerDialog({ title, className = '', onClose, busy = false, wide = false, children }: ContainerDialogProps) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  useEffect(() => {
    const previous = document.activeElement
    const element = ref.current
    element?.showModal()
    return () => {
      element?.close()
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus({ preventScroll: true })
    }
  }, [])
  return <dialog ref={ref} className={`ct-dialog${wide ? ' ct-dialog-wide' : ''} ${className}`} aria-labelledby={titleId}
    onCancel={(event) => { event.preventDefault(); if (!busy) onClose() }}>
    <header className="ct-dialog-heading"><h2 id={titleId}>{title}</h2>
      <ContainerButton aria-label="关闭弹窗" disabled={busy} onClick={onClose}>×</ContainerButton>
    </header>
    {children}
  </dialog>
}

export function ContainerMoreActions({ name, children }: { name: string; children: ReactNode }) {
  const ref = useRef<HTMLDetailsElement>(null)
  useEffect(() => {
    const dismiss = (event: PointerEvent) => {
      if (event.target instanceof window.Node && !ref.current?.contains(event.target)) ref.current?.removeAttribute('open')
    }
    document.addEventListener('pointerdown', dismiss)
    return () => document.removeEventListener('pointerdown', dismiss)
  }, [])
  return <details ref={ref} className="ct-more"
    onKeyDown={(event) => {
      if (event.key === 'Escape') { ref.current?.removeAttribute('open'); ref.current?.querySelector('summary')?.focus(); event.stopPropagation() }
    }}>
    <summary aria-label={`更多操作 ${name}`}>更多</summary>
    <div className="ct-more-options" onClick={(event) => {
      if (event.target instanceof window.Element && event.target.closest('button:not(:disabled)')) {
        ref.current?.removeAttribute('open')
        ref.current?.querySelector('summary')?.focus()
      }
    }}>{children}</div>
  </details>
}
