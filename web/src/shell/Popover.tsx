import { useEffect, useRef, useState, type ReactNode } from 'react'

type PopoverProps = {
  /** render prop: receives open state and the toggle callback */
  trigger: (open: boolean, toggle: () => void) => ReactNode
  children: ReactNode
  align?: 'left' | 'right'
  width?: number
  ariaLabel?: string
}

/** Glass dropdown anchored to its trigger; outside click and Esc close it. */
export function Popover({ trigger, children, align = 'right', width, ariaLabel }: PopoverProps) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false)
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

  return (
    <span className="nav-pop" ref={rootRef}>
      {trigger(open, () => setOpen((value) => !value))}
      {open ? (
        <span
          className={`popover glass popover-${align}`}
          role="menu"
          aria-label={ariaLabel}
          style={width ? { width } : undefined}
          onClick={() => setOpen(false)}
        >
          {children}
        </span>
      ) : null}
    </span>
  )
}
