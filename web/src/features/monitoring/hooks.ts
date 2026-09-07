/**
 * Monitoring data hooks. Each hook owns loading/error/cancellation and polls
 * through the shared usePolling model (immediate run + visible-only interval).
 * A failed reload keeps the last good data and only surfaces the error.
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import type { ChartWindow, InterfaceStatus, Overview, Terminal } from '../../lib/types'
import { usePolling } from '../../shell/usePolling'
import {
  fetchFleetOverview,
  fetchInterfaces,
  fetchRealtime,
  fetchSettingsSummary,
  fetchTerminals,
  fetchTrafficHistory,
} from './api'
import type { FleetOverview, SettingsSummary, TrafficHistory } from './types'

type PolledData<T> = {
  data: T | null
  /** true only until the first load settles — reloads keep the old view */
  loading: boolean
  error: string | null
  reload: () => void
}

/**
 * Shared polled-fetch behavior: one in-flight request at a time (a change of
 * deps while busy queues exactly one follow-up load), state updates guarded
 * against unmount, errors never clear the last good data.
 */
function usePolledData<T>(load: () => Promise<T>, refreshMs: number, deps: readonly unknown[]): PolledData<T> {
  const [state, setState] = useState<{ data: T | null; loading: boolean; error: string | null }>({
    data: null,
    loading: true,
    error: null,
  })
  const mountedRef = useRef(true)
  const inFlightRef = useRef(false)
  const queuedRef = useRef(false)
  const loadRef = useRef(load)
  loadRef.current = load

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const reload = useCallback(async () => {
    if (inFlightRef.current) {
      queuedRef.current = true
      return
    }
    inFlightRef.current = true
    try {
      do {
        queuedRef.current = false
        try {
          const result = await loadRef.current()
          if (mountedRef.current) setState({ data: result, loading: false, error: null })
        } catch (cause) {
          if (mountedRef.current) {
            setState((current) => ({ data: current.data, loading: false, error: errorMessage(cause) }))
          }
        }
      } while (queuedRef.current)
    } finally {
      inFlightRef.current = false
    }
  }, [])

  const fire = useCallback(() => void reload(), [reload])

  usePolling(fire, refreshMs, deps)

  return { data: state.data, loading: state.loading, error: state.error, reload: fire }
}

/** GET /api/fleet-overview — all devices, not device-scoped. */
export function useFleetOverview(refreshMs: number, reloadNonce: number): PolledData<FleetOverview> {
  return usePolledData(fetchFleetOverview, refreshMs, [reloadNonce])
}

/** GET /api/realtime — Overview shape for the selected device. */
export function useRealtimeOverview(deviceId: string, refreshMs: number, reloadNonce: number): PolledData<Overview> {
  return usePolledData(useCallback(() => fetchRealtime(deviceId), [deviceId]), refreshMs, [deviceId, reloadNonce])
}

/** GET /api/traffic-history?window= — switching window keeps the old samples until fresh ones land. */
export function useTrafficHistory(deviceId: string, window: ChartWindow, refreshMs: number, reloadNonce: number): PolledData<TrafficHistory> {
  return usePolledData(
    useCallback(() => fetchTrafficHistory(deviceId, window), [deviceId, window]),
    refreshMs,
    [deviceId, window, reloadNonce],
  )
}

/** GET /api/terminals — live terminal list for the overview grid. */
export function useTerminalList(deviceId: string, refreshMs: number, reloadNonce: number): PolledData<Terminal[]> {
  return usePolledData(useCallback(() => fetchTerminals(deviceId), [deviceId]), refreshMs, [deviceId, reloadNonce])
}

/** GET /api/interfaces — live interface list for the overview table. */
export function useInterfaceList(deviceId: string, refreshMs: number, reloadNonce: number): PolledData<InterfaceStatus[]> {
  return usePolledData(useCallback(() => fetchInterfaces(deviceId), [deviceId]), refreshMs, [deviceId, reloadNonce])
}

/** One-shot GET /api/settings subset (setup choices, collection interval display). */
export function useSettingsSummary(): { summary: SettingsSummary | null; loading: boolean; reload: () => void } {
  const [summary, setSummary] = useState<SettingsSummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [nonce, setNonce] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fetchSettingsSummary()
      .then((result) => {
        if (!cancelled) setSummary(result)
      })
      .catch(() => {
        if (!cancelled) setSummary(null)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [nonce])

  return { summary, loading, reload: useCallback(() => setNonce((value) => value + 1), []) }
}
