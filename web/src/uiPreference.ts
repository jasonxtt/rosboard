/** Shared, non-sensitive preferences. No UI, CSS or server dependencies. */
export type UiVariant = 'compact' | 'aurora'
export const UI_VARIANT_KEY = 'rosboard:ui-variant'
export const PANEL_PREFERENCES_KEY = 'rosboard:panel-preferences'
export const THEME_KEY = 'rosboard:theme'
export const REFRESH_KEY = 'rosboard:refresh-ms'
export const UI_OPTIONS: ReadonlyArray<{ value: UiVariant; label: string }> = [
  { value: 'compact', label: 'Compact 紧凑版' },
  { value: 'aurora', label: 'Aurora 玻璃版' },
]
export function readLocal(key: string): string | null {
  try { return window.localStorage.getItem(key) } catch { return null }
}
export function writeLocal(key: string, value: string): void {
  try { window.localStorage.setItem(key, value) } catch { /* URL keeps switching usable without storage. */ }
}
export function removeLocal(key: string): void {
  try { window.localStorage.removeItem(key) } catch { /* session-only */ }
}
export function readSession(key: string): string | null {
  try { return window.sessionStorage.getItem(key) } catch { return null }
}
export function writeSession(key: string, value: string): void {
  try { window.sessionStorage.setItem(key, value) } catch { /* session storage may be denied */ }
}
export function removeSession(key: string): void {
  try { window.sessionStorage.removeItem(key) } catch { /* session-only */ }
}
export function isUiVariant(value: unknown): value is UiVariant {
  return value === 'compact' || value === 'aurora'
}
export function resolveUiVariant(search: string, stored: unknown): UiVariant {
  const explicit = new URLSearchParams(search).get('ui')
  return isUiVariant(explicit) ? explicit : isUiVariant(stored) ? stored : 'aurora'
}
export function readUiVariant(): UiVariant {
  return resolveUiVariant(window.location.search, readLocal(UI_VARIANT_KEY))
}
export function uiSwitchURL(href: string, variant: UiVariant): string {
  const url = new URL(href)
  url.searchParams.set('ui', variant)
  url.hash = '/settings/ui'
  return url.href
}
/** Full navigation discards the outgoing stylesheet/runtime graph. */
export function switchUi(variant: UiVariant): void {
  writeLocal(UI_VARIANT_KEY, variant)
  window.location.assign(uiSwitchURL(window.location.href, variant))
}
export function readPanelRecord(): Record<string, unknown> {
  try {
    const value: unknown = JSON.parse(readLocal(PANEL_PREFERENCES_KEY) ?? '{}')
    return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
  } catch { return {} }
}
/** Merge fields so either UI saves without erasing the other's preferences. */
export function savePanelRecord(values: Record<string, unknown>): void {
  writeLocal(PANEL_PREFERENCES_KEY, JSON.stringify({ ...readPanelRecord(), ...values }))
}
export function readRefreshPreference(): number {
  const stored = readLocal(REFRESH_KEY)
  const value = stored === null ? readPanelRecord().refreshMs : stored
  const ms = typeof value === 'number' || (typeof value === 'string' && value.trim() !== '') ? Number(value) : NaN
  return [0, 1000, 3000, 5000, 10000].includes(ms) ? ms : 1000
}
export function readThemePreference(): 'light' | 'dark' | null {
  const value = readLocal(THEME_KEY) ?? readPanelRecord().theme
  return value === 'light' || value === 'dark' ? value : null
}
export function resetSharedPanelPreferences(): void {
  removeLocal(PANEL_PREFERENCES_KEY)
  removeLocal(REFRESH_KEY)
  removeLocal(THEME_KEY)
  // Resetting display options must not switch applications.
}
