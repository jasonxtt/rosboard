import { VIEW_TITLES, type View } from '../../shell/views'
import { readPanelRecord, savePanelRecord, resetSharedPanelPreferences } from '../../uiPreference'
/**
 * Browser-local, non-sensitive UI preferences and transient cleanup state.
 * Passwords / RouterOS credentials must never touch these helpers
 * (.trellis/spec/frontend/state-management.md).
 *
 * Ownership split after the shell rebuild:
 * - theme            → lib/theme.ts (`rosboard:theme`), exposed via useShell
 * - refresh interval → shell (`rosboard:refresh-ms`), exposed via useShell
 * - landing view / default terminal family stay under
 *   `rosboard:panel-preferences` (this module).
 */

import { setTheme, THEME_STORAGE_KEY, type Theme } from '../../lib/theme'
import type { RouterOSCleanup } from './api'

export const PANEL_PREFERENCES_KEY = 'rosboard:panel-preferences'
const PENDING_CLEANUP_KEY = 'rosboard:pending-routeros-cleanup'

export type LandingView = View
export type DefaultTerminalFamily = 'all' | 'ipv4' | 'ipv6'

export type PanelPreferences = {
  landingView: LandingView
  terminalFamily: DefaultTerminalFamily
}

export const defaultPanelPreferences: PanelPreferences = {
  landingView: 'fleet',
  terminalFamily: 'all',
}

export function loadPanelPreferences(): PanelPreferences {
  try {
    const parsed = readPanelRecord()
    return {
      landingView: typeof parsed.landingView === 'string' && Object.hasOwn(VIEW_TITLES, parsed.landingView) ? parsed.landingView as LandingView : defaultPanelPreferences.landingView,
      terminalFamily:
        parsed.terminalFamily === 'ipv4' || parsed.terminalFamily === 'ipv6' || parsed.terminalFamily === 'all'
          ? parsed.terminalFamily
          : defaultPanelPreferences.terminalFamily,
    }
  } catch {
    return defaultPanelPreferences
  }
}

export function savePanelPreferences(preferences: PanelPreferences): void {
  try {
    savePanelRecord(preferences)
  } catch {
    // Storage unavailable — preferences stay session-only.
  }
}

export function resetPanelPreferences(): void {
  try {
    resetSharedPanelPreferences()
  } catch {
    // ignore
  }
}

/* ---------- theme (shell-owned, surfaced here for the 界面设置 form) ---------- */

export type ThemeChoice = 'system' | Theme

/** Current effective choice: explicit stored theme, else 跟随系统. */
export function readThemeChoice(): ThemeChoice {
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
    if (stored === 'dark' || stored === 'light') return stored
  } catch {
    // fall through to system
  }
  return 'system'
}

export function applyThemeChoice(choice: ThemeChoice): void {
  if (choice !== 'system') {
    setTheme(choice)
    return
  }
  // Follow system: apply the live system theme through the shared setter so
  // subscribers (shell) update, then drop the persisted override so future
  // system changes are followed again (lib/theme re-reads the key on change).
  const system: Theme =
    typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  setTheme(system)
  try {
    window.localStorage.removeItem(THEME_STORAGE_KEY)
    savePanelRecord({ theme: null })
  } catch {
    // ignore
  }
}

/* ---------- pending RouterOS cleanup (survives the post-archive reload) ---------- */

export function storePendingCleanup(cleanup: RouterOSCleanup): void {
  try {
    window.sessionStorage.setItem(PENDING_CLEANUP_KEY, JSON.stringify(cleanup))
  } catch {
    // Session storage full/unavailable — the card just won't reappear.
  }
}

export function consumePendingCleanup(): RouterOSCleanup | null {
  try {
    const raw = window.sessionStorage.getItem(PENDING_CLEANUP_KEY)
    window.sessionStorage.removeItem(PENDING_CLEANUP_KEY)
    if (!raw) return null
    const value = JSON.parse(raw) as Partial<RouterOSCleanup>
    if (!value.deviceId || !value.name || !value.username || !value.groupName || !value.script) return null
    return {
      deviceId: value.deviceId,
      name: value.name,
      username: value.username,
      groupName: value.groupName,
      script: value.script,
    }
  } catch {
    return null
  }
}

/** Clear every browser-local panel key after a full reset. */
export function clearLocalPanelState(): void {
  try {
    resetSharedPanelPreferences()
    window.localStorage.removeItem('rosboard:selected-device')
    window.localStorage.removeItem('rosboard:refresh-ms')
    window.sessionStorage.removeItem(PENDING_CLEANUP_KEY)
    window.sessionStorage.removeItem('rosboard:traffic-window')
  } catch {
    // ignore
  }
}
