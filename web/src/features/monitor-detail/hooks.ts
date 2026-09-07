import { useCallback, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { usePolling } from '../../shell/usePolling'

export type MonitorResource<T> = {
  data: T | null
  /** true only until the first load settles (skeleton state) */
  loading: boolean
  error: string | null
  reload: () => void
}

/**
 * Shared monitor-page data hook: initial load + polling at `intervalMs`
 * (shell refreshMs; 0 stops the periodic part), visibility-aware via
 * usePolling. A failed refresh keeps the last good data; a generation
 * counter discards out-of-order responses after deps change or unmount.
 */
export function useMonitorResource<T>(load: () => Promise<T>, intervalMs: number, deps: readonly unknown[]): MonitorResource<T> {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)
  const loadRef = useRef(load)
  loadRef.current = load
  const generationRef = useRef(0)

  const reload = useCallback(() => setNonce((value) => value + 1), [])

  usePolling(
    useCallback(() => {
      const generation = ++generationRef.current
      loadRef.current()
        .then((result) => {
          if (generationRef.current !== generation) return
          setData(result)
          setLoading(false)
          setError(null)
        })
        .catch((loadError: unknown) => {
          if (generationRef.current !== generation) return
          setLoading(false)
          setError(errorMessage(loadError, '数据读取失败，请稍后重试'))
        })
    }, []),
    intervalMs,
    [...deps, nonce],
  )

  return { data, loading, error, reload }
}

export type SortDirection = 'asc' | 'desc'

export type SortState<K extends string> = {
  key: K
  direction: SortDirection
  toggle: (key: K) => void
  reset: (key: K, direction?: SortDirection) => void
}

/** Column sort state: first click selects ascending, next click flips. */
export function useSortState<K extends string>(initialKey: K, initialDirection: SortDirection = 'asc'): SortState<K> {
  const [key, setKey] = useState<K>(initialKey)
  const [direction, setDirection] = useState<SortDirection>(initialDirection)
  const toggle = useCallback(
    (next: K) => {
      if (next === key) {
        setDirection((value) => (value === 'asc' ? 'desc' : 'asc'))
      } else {
        setKey(next)
        setDirection('asc')
      }
    },
    [key],
  )
  const reset = useCallback((next: K, nextDirection: SortDirection = 'asc') => {
    setKey(next)
    setDirection(nextDirection)
  }, [])
  return { key, direction, toggle, reset }
}
