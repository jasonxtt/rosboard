import { useContext } from 'react'
import { ShellContext, type ShellContextValue } from './shellContext'

export function useShell(): ShellContextValue {
  const context = useContext(ShellContext)
  if (!context) throw new Error('useShell must be used inside <ShellProvider>')
  return context
}

/** Selected device id ('' while the device list is still loading). */
export function useDeviceId(): string {
  return useShell().selectedDeviceId
}
