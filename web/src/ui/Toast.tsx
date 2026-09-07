import { useSyncExternalStore } from 'react'
import { createPortal } from 'react-dom'
import { StatusDot } from './StatusDot'
import { snapshotToasts, subscribeToasts } from './toastStore'

/** Toast stack mounted once at the shell root; fire via `toast()` from ui/toastStore. */
export function ToastHost() {
  const toasts = useSyncExternalStore(subscribeToasts, snapshotToasts)
  return createPortal(
    <div className="toast-host" role="status" aria-live="polite">
      {toasts.map((item) => (
        <div key={item.id} className={`glass toast${item.leaving ? ' toast-leaving' : ''}`}>
          <StatusDot tone={item.tone === 'accent' ? 'neutral' : item.tone} />
          <span>{item.message}</span>
        </div>
      ))}
    </div>,
    document.body,
  )
}
