/**
 * Feature-local hooks for the settings/recognition pages
 * (.trellis/spec/frontend/hook-guidelines.md): explicit loading/error state,
 * cancellation on unmount, and the shared "save → panel restart → reload"
 * flow every restarting mutation must follow (UX standard §9.4).
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { fetchSettings, waitForPanelRestart, type MutationResult, type SettingsResponse } from './api'

export type SettingsState = {
  settings: SettingsResponse | null
  loading: boolean
  error: string | null
  reload: () => Promise<void>
}

/** GET /api/settings with explicit loading/error and a reload callback. */
export function useSettings(): SettingsState {
  const [settings, setSettings] = useState<SettingsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(async () => {
    try {
      const result = await fetchSettings()
      setSettings(result)
      setError(null)
    } catch (loadError) {
      setError(errorMessage(loadError, '设置读取失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fetchSettings()
      .then((result) => {
        if (cancelled) return
        setSettings(result)
        setError(null)
      })
      .catch((loadError) => {
        if (!cancelled) setError(errorMessage(loadError, '设置读取失败'))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  return { settings, loading, error, reload }
}

export type RestartingActionState = {
  /** A restarting mutation is in flight or the panel is being awaited. */
  waiting: boolean
  /** The panel has been observed offline (restart actually began). */
  offline: boolean
  /**
   * Run a mutation. When its result carries `restarting: true`, poll
   * /api/health until the panel returns and reload the page; otherwise call
   * `onSettled` so the caller can refresh its data. Re-throws action errors
   * after resetting the waiting state.
   */
  run: (action: () => Promise<MutationResult | void>, onSettled?: () => void) => Promise<void>
}

/**
 * Shared restart gate for device/collection/recognition mutations (§9.4:
 * 按钮变为「已保存，等待面板重启…」并自动轮询恢复).
 */
export function useRestartingAction(): RestartingActionState {
  const [waiting, setWaiting] = useState(false)
  const [offline, setOffline] = useState(false)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const run = useCallback(async (action: () => Promise<MutationResult | void>, onSettled?: () => void) => {
    setWaiting(true)
    setOffline(false)
    try {
      const result = await action()
      if (result?.restarting) {
        // Reloads the page on success; throws on timeout.
        await waitForPanelRestart(() => {
          if (mounted.current) setOffline(true)
        })
        return
      }
      if (mounted.current) setWaiting(false)
      onSettled?.()
    } catch (error) {
      if (mounted.current) setWaiting(false)
      throw error
    }
  }, [])

  return { waiting, offline, run }
}
