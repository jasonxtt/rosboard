import { createContext } from 'react'
import type { Theme } from '../lib/theme'
import type { AlertEvent, DeviceStatus } from '../lib/types'
import type { View } from './views'

export const SELECTED_DEVICE_KEY = 'rosboard:selected-device'
export const REFRESH_MS_KEY = 'rosboard:refresh-ms'
export const REFRESH_OPTIONS: ReadonlyArray<{ value: number; label: string }> = [
  { value: 0, label: '停止刷新' },
  { value: 1000, label: '1 秒刷新' },
  { value: 3000, label: '3 秒刷新' },
  { value: 5000, label: '5 秒刷新' },
  { value: 10000, label: '10 秒刷新' },
]

export type ShellContextValue = {
  view: View
  navigate: (view: View) => void
  devices: DeviceStatus[]
  devicesLoading: boolean
  /** '' until the device list resolves */
  selectedDeviceId: string
  selectDevice: (id: string) => void
  /** append ?device= to a device-scoped API path */
  scopedPath: (path: string) => string
  refreshMs: number
  setRefreshMs: (ms: number) => void
  /** bump to force an immediate refresh of shell-level data */
  reloadNonce: number
  requestReload: () => void
  alerts: AlertEvent[]
  warnings: string[]
  theme: Theme
  toggleTheme: () => void
}

export const ShellContext = createContext<ShellContextValue | null>(null)
