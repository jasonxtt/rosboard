import { useEffect, useRef } from 'react'

/**
 * Shared polling model: the callback runs once immediately (initial loads
 * still happen when periodic refresh is stopped), then on `intervalMs` while
 * the document is visible, and once more whenever the page becomes visible
 * again. `intervalMs <= 0` disables the periodic part only.
 */
export function usePolling(callback: () => void, intervalMs: number, deps: readonly unknown[]): void {
  const callbackRef = useRef(callback)
  callbackRef.current = callback

  useEffect(() => {
    let disposed = false
    const run = () => {
      if (!disposed) callbackRef.current()
    }
    const runWhenVisible = () => {
      if (document.visibilityState === 'visible') run()
    }

    run()
    const timer = intervalMs > 0 ? window.setInterval(runWhenVisible, intervalMs) : 0
    document.addEventListener('visibilitychange', runWhenVisible)
    return () => {
      disposed = true
      if (timer) window.clearInterval(timer)
      document.removeEventListener('visibilitychange', runWhenVisible)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- deps are the caller's schedule key
  }, [intervalMs, ...deps])
}
