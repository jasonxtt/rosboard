import { readRefreshPreference } from '../uiPreference'
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from 'react'
import { apiGet, apiPost, scoped } from '../lib/api'
import { getTheme, subscribeTheme, toggleTheme } from '../lib/theme'
import { parseDashboard, parseDevices, type DeviceStatus } from '../lib/types'
import { loadPanelPreferences } from '../features/settings/prefs'
import { usePolling } from './usePolling'
import { REFRESH_MS_KEY, REFRESH_OPTIONS, SELECTED_DEVICE_KEY, ShellContext, type ShellContextValue } from './shellContext'
import { hashForView, viewFromHash, type View } from './views'

const DEVICE_LIST_POLL_MS = 10_000
const HEARTBEAT_MS = 10_000

function readSelectedDevice(): string {
  try {
    return window.localStorage.getItem(SELECTED_DEVICE_KEY) ?? ''
  } catch {
    return ''
  }
}

/** Initial view: the URL hash wins (deep link / browser refresh); bare `/`
 * falls back to the landing-page preference (默认仪表台). */
function readInitialView(): View {
  try {
    const fromHash = viewFromHash(window.location.hash)
    if (fromHash) return fromHash
  } catch {
    // hash parsing never blocks startup
  }
  return loadPanelPreferences().landingView
}

/**
 * Shell state owner: active view (hash-routed), device list + selection,
 * refresh preference, theme, viewer heartbeat, and the alerts/warnings
 * summary that feeds the top bar. Device-scoped pages reset by remounting on
 * `selectedDeviceId` changes (see ShellApp).
 */
export function ShellProvider({ children }: { children: ReactNode }) {
  const [view, setView] = useState<View>(readInitialView)
  const [devices, setDevices] = useState<DeviceStatus[]>([])
  const [devicesLoading, setDevicesLoading] = useState(true)
  const [selectedDeviceId, setSelectedDeviceId] = useState(readSelectedDevice)
  const [refreshMs, setRefreshMsState] = useState(readRefreshPreference)
  const [reloadNonce, setReloadNonce] = useState(0)
  const [alerts, setAlerts] = useState<ShellContextValue['alerts']>([])
  const [warnings, setWarnings] = useState<string[]>([])
  const theme = useSyncExternalStore(subscribeTheme, getTheme)

  const navigate = useCallback((next: View) => {
    setView(next)
    const target = hashForView(next)
    try {
      if (window.location.hash !== target) window.location.hash = target
    } catch {
      // hash routing is best-effort; in-app navigation still works
    }
  }, [])

  // Browser back/forward and manual hash edits drive the view too.
  useEffect(() => {
    const onHashChange = () => {
      const next = viewFromHash(window.location.hash)
      if (next) setView(next)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  const setRefreshMs = useCallback((ms: number) => {
    const valid = REFRESH_OPTIONS.some((option) => option.value === ms) ? ms : 1000
    setRefreshMsState(valid)
    try {
      window.localStorage.setItem(REFRESH_MS_KEY, String(valid))
    } catch {
      // storage unavailable — session-only preference
    }
  }, [])

  const requestReload = useCallback(() => setReloadNonce((value) => value + 1), [])

  // Device list: keeps the device pill and selection validity current.
  usePolling(
    useCallback(() => {
      void apiGet('/api/devices', parseDevices)
        .then((list) => {
          setDevices(list)
          setDevicesLoading(false)
        })
        .catch(() => setDevicesLoading(false))
    }, []),
    DEVICE_LIST_POLL_MS,
    [reloadNonce],
  )

  // Selection validity: fall back to the first usable device; persist choice.
  const available = useMemo(() => devices.filter((device) => device.enabled && !device.archived), [devices])
  const selectDevice = useCallback((id: string) => {
    setSelectedDeviceId(id)
    try {
      window.localStorage.setItem(SELECTED_DEVICE_KEY, id)
    } catch {
      // storage unavailable — session-only selection
    }
  }, [])
  const effectiveDeviceId = available.some((device) => device.id === selectedDeviceId) ? selectedDeviceId : (available[0]?.id ?? '')

  // Keep the backend's fast polling alive while anyone watches (§12).
  usePolling(
    useCallback(() => {
      void apiPost('/api/viewer-heartbeat').catch(() => undefined)
    }, []),
    HEARTBEAT_MS,
    [],
  )

  // Shell-level dashboard summary: alerts for the bell, warnings for the bar.
  usePolling(
    useCallback(() => {
      if (!effectiveDeviceId) return
      void apiGet(scoped('/api/dashboard', effectiveDeviceId), parseDashboard)
        .then((dashboard) => {
          setAlerts(dashboard.alerts)
          setWarnings(Array.from(new Set(dashboard.warnings.map((warning) => warning.trim()).filter(Boolean))))
        })
        .catch(() => {
          // Keep the last good summary rather than flashing the bar/bell empty.
        })
    }, [effectiveDeviceId]),
    refreshMs,
    [effectiveDeviceId, reloadNonce],
  )

  const value = useMemo<ShellContextValue>(
    () => ({
      view,
      navigate,
      devices,
      devicesLoading,
      selectedDeviceId: effectiveDeviceId,
      selectDevice,
      scopedPath: (path: string) => scoped(path, effectiveDeviceId),
      refreshMs,
      setRefreshMs,
      reloadNonce,
      requestReload,
      alerts,
      warnings,
      theme,
      toggleTheme,
    }),
    [view, navigate, devices, devicesLoading, effectiveDeviceId, selectDevice, refreshMs, setRefreshMs, reloadNonce, requestReload, alerts, warnings, theme],
  )

  return <ShellContext.Provider value={value}>{children}</ShellContext.Provider>
}
