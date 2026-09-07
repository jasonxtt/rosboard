/**
 * Theme control: `data-theme` on <html>, preference persisted under
 * localStorage["rosboard:theme"], default follows prefers-color-scheme
 * (dark-first product default when the preference cannot be read).
 */

export type Theme = 'dark' | 'light'

export const THEME_STORAGE_KEY = 'rosboard:theme'

const listeners = new Set<(theme: Theme) => void>()
let current: Theme = 'dark'

function systemTheme(): Theme {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return 'dark'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function readStoredTheme(): Theme | null {
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
    return stored === 'dark' || stored === 'light' ? stored : null
  } catch {
    return null
  }
}

function applyTheme(theme: Theme): void {
  current = theme
  document.documentElement.dataset.theme = theme
  document.documentElement.style.colorScheme = theme
}

/** Apply the stored or system theme. Call once from main.tsx before render. */
export function initTheme(): Theme {
  applyTheme(readStoredTheme() ?? systemTheme())
  try {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (!readStoredTheme()) setTheme(systemTheme())
    })
  } catch {
    // Older WebKit without addEventListener on MediaQueryList: keep initial theme.
  }
  return current
}

export function getTheme(): Theme {
  return current
}

export function setTheme(theme: Theme): void {
  applyTheme(theme)
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, theme)
  } catch {
    // Storage may be unavailable; the theme still applies for this session.
  }
  for (const listener of listeners) listener(theme)
}

export function toggleTheme(): Theme {
  const next: Theme = current === 'dark' ? 'light' : 'dark'
  setTheme(next)
  return next
}

export function subscribeTheme(listener: (theme: Theme) => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
