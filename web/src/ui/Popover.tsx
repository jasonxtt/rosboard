import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

type PopoverProps = {
  /** render prop: receives open state and the toggle callback */
  trigger: (open: boolean, toggle: () => void) => ReactNode
  children: ReactNode | ((close: () => void) => ReactNode)
  align?: 'left' | 'right'
  width?: number
  ariaLabel?: string
  /**
   * Render the panel in a portal at fixed coordinates next to the trigger —
   * required inside overflow-clipping containers (e.g. horizontally
   * scrollable tables). Default false (absolute inside the anchor span).
   */
  portal?: boolean
  /** default true: any click inside the panel closes it (menu semantics).
   * Form popovers pass false and close explicitly. */
  closeOnContentClick?: boolean
}

/** Glass dropdown anchored to its trigger; outside click and Esc close it. */
export function Popover({ trigger, children, align = 'right', width, ariaLabel, portal = false, closeOnContentClick = true }: PopoverProps) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLSpanElement>(null)
  const panelRef = useRef<HTMLSpanElement>(null)
  const [coords, setCoords] = useState<{ top: number; left: number } | null>(null)

  useLayoutEffect(() => {
    if (!open || !portal) return
    const update = () => {
      const rect = rootRef.current?.getBoundingClientRect()
      if (!rect) return
      const panelWidth = width ?? 260
      const left = align === 'right' ? Math.max(8, Math.min(rect.right - panelWidth, window.innerWidth - panelWidth - 8)) : Math.max(8, Math.min(rect.left, window.innerWidth - panelWidth - 8))
      setCoords({ top: rect.bottom + 8, left })
    }
    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [open, portal, align, width])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (rootRef.current?.contains(target)) return
      if (panelRef.current?.contains(target)) return
      setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  const panel = open ? (
    <span
      ref={panelRef}
      className={`popover glass ${portal ? 'popover-portal' : `popover-${align}`}`}
      role="menu"
      aria-label={ariaLabel}
      style={{ ...(width ? { width } : undefined), ...(portal && coords ? { top: coords.top, left: coords.left } : undefined) }}
      onClick={closeOnContentClick ? () => setOpen(false) : undefined}
    >
      {typeof children === 'function' ? children(() => setOpen(false)) : children}
    </span>
  ) : null

  return (
    <span className="nav-pop" ref={rootRef}>
      {trigger(open, () => setOpen((value) => !value))}
      {portal && open ? createPortal(panel, document.body) : panel}
    </span>
  )
}
