import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { ContainerFloatProps } from '../features/containers/ContainerUI'

/** Aurora floating layer for the container Files browser: a fixed-position
 * opaque glass panel anchored below its field, portal-rendered to body so
 * overflow-clipping containers (modals, scroll areas) cannot cut it off.
 * Flips above the field when there is no room below. */
export function AuroraContainerFloat({ anchor, onClose, children }: ContainerFloatProps) {
  const panel = useRef<HTMLDivElement>(null)
  const [coords, setCoords] = useState<{ top: number; left: number; width: number } | null>(null)
  useLayoutEffect(() => {
    if (!anchor) return
    const update = () => {
      const rect = anchor.getBoundingClientRect()
      const width = Math.max(rect.width, 380)
      const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))
      const height = Math.min(440, window.innerHeight - 32)
      const below = rect.bottom + 8
      const top =
        below + height > window.innerHeight - 8 && rect.top > height + 8
          ? Math.max(8, rect.top - height - 8)
          : Math.min(below, Math.max(8, window.innerHeight - height - 8))
      setCoords({ top, left, width })
    }
    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [anchor])
  useEffect(() => {
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (anchor?.contains(target) || panel.current?.contains(target)) return
      onClose()
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [anchor, onClose])
  if (!anchor || !coords) return null
  return createPortal(
    <div ref={panel} className="ct ct-float" style={coords}>
      {children}
    </div>,
    document.body,
  )
}
