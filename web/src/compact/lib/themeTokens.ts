import { useSyncExternalStore } from 'react'

/**
 * Single source of truth for chart colours: read them back out of the CSS
 * custom properties in index.css instead of duplicating hex values in TSX.
 * Snapshots are memoised per `data-theme` value and invalidated by a shared
 * MutationObserver, so light/dark switches repaint the charts.
 */

const tokenNames = [
  '--font-sans',
  '--font-mono',
  '--canvas',
  '--surface',
  '--surface-soft',
  '--surface-code',
  '--hairline',
  '--hairline-soft',
  '--ink',
  '--slate',
  '--steel',
  '--stone',
  '--muted',
  '--chart-upload',
  '--chart-download',
  '--mint',
  '--mint-deep',
  '--on-code',
  '--status-ok',
  '--status-warn',
  '--status-error',
  '--status-info',
  '--status-idle',
] as const

export type ThemeTokenName = (typeof tokenNames)[number]
export type ThemeTokens = Record<ThemeTokenName, string>

const fallback: ThemeTokens = {
  '--font-sans': '-apple-system, BlinkMacSystemFont, sans-serif',
  '--font-mono': "'Geist Mono', ui-monospace, monospace",
  '--canvas': '#f7f9fc',
  '--surface': '#ffffff',
  '--surface-soft': '#f8fafc',
  '--surface-code': '#1c1c1e',
  '--hairline': '#e9edf2',
  '--hairline-soft': '#e9edf2',
  '--ink': '#343b46',
  '--slate': '#343b46',
  '--steel': '#737e8f',
  '--stone': '#737e8f',
  '--muted': '#8893a3',
  '--chart-upload': '#9181cf',
  '--chart-download': '#449979',
  '--mint': '#5b8fdb',
  '--mint-deep': '#467cc9',
  '--on-code': '#f5f5f7',
  '--status-ok': '#449979',
  '--status-warn': '#c79a4d',
  '--status-error': '#cc6366',
  '--status-info': '#5b8fdb',
  '--status-idle': '#8893a3',
}

const listeners = new Set<() => void>()
let observer: MutationObserver | null = null
let cachedTheme: string | null = null
let cachedTokens: ThemeTokens = fallback

function currentTheme() {
  return document.documentElement.dataset.theme || 'light'
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  if (!observer) {
    observer = new MutationObserver(() => {
      cachedTheme = null
      for (const notify of listeners) notify()
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
  }
  return () => {
    listeners.delete(listener)
    if (!listeners.size) {
      observer?.disconnect()
      observer = null
    }
  }
}

/** Reads the active theme's tokens. The returned object is stable per theme. */
export function readThemeTokens(): ThemeTokens {
  const theme = currentTheme()
  if (cachedTheme === theme) return cachedTokens
  const styles = window.getComputedStyle(document.documentElement)
  const tokens = { ...fallback }
  for (const name of tokenNames) {
    const value = styles.getPropertyValue(name).trim()
    if (value) tokens[name] = value
  }
  cachedTheme = theme
  cachedTokens = tokens
  return cachedTokens
}

/** Subscribes a component to the active theme's tokens. */
export function useThemeTokens(): ThemeTokens {
  return useSyncExternalStore(subscribe, readThemeTokens, () => fallback)
}

/** Colour for a fleet distribution / status slice key. */
export function statusColor(tokens: ThemeTokens, key: string): string {
  switch (key) {
    case 'online':
      return tokens['--status-ok']
    case 'inactive':
    case 'other':
      return tokens['--status-warn']
    case 'offline':
      return tokens['--status-idle']
    case 'tcp':
      return tokens['--status-info']
    case 'udp':
      return tokens['--mint-deep']
    default:
      return tokens['--muted']
  }
}

/**
 * Applies an alpha channel to a token value. Canvas (ECharts) parses colours
 * with its own parser, so tokens are expanded to `rgba()` here rather than
 * handed over as `color-mix()`.
 */
export function withAlpha(color: string, alpha: number) {
  const hex = color.trim()
  const short = /^#([\da-f])([\da-f])([\da-f])$/i.exec(hex)
  const long = /^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i.exec(hex)
  let channels: number[] | null = null
  if (short) channels = [short[1], short[2], short[3]].map((part) => Number.parseInt(part + part, 16))
  else if (long) channels = [long[1], long[2], long[3]].map((part) => Number.parseInt(part, 16))
  if (!channels) return hex
  return `rgba(${channels[0]}, ${channels[1]}, ${channels[2]}, ${alpha})`
}
