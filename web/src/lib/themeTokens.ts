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
  '--mint',
  '--mint-deep',
  '--on-code',
  '--status-ok',
  '--status-warn',
  '--status-error',
  '--status-info',
  '--status-idle',
  '--chart-upload',
  '--chart-download',
  '--chart-purple',
  '--chart-orange',
] as const

export type ThemeTokenName = (typeof tokenNames)[number]
export type ThemeTokens = Record<ThemeTokenName, string>

const fallback: ThemeTokens = {
  '--font-sans': 'Inter, system-ui, sans-serif',
  '--font-mono': "'Geist Mono', ui-monospace, monospace",
  '--canvas': '#F5F7FA',
  '--surface': '#ffffff',
  '--surface-soft': '#f7f7f7',
  '--surface-code': '#1c1c1e',
  '--hairline': '#EAEEF2',
  '--hairline-soft': '#F0F0F0',
  '--ink': '#1a1a1a',
  '--slate': '#333333',
  '--steel': '#666666',
  '--stone': '#999999',
  '--muted': '#999999',
  '--mint': '#4794EB',
  '--mint-deep': '#3A83D4',
  '--on-code': '#f5f5f7',
  '--status-ok': '#22C55E',
  '--status-warn': '#F5A623',
  '--status-error': '#d45656',
  '--status-info': '#4794EB',
  '--status-idle': '#888888',
  '--chart-upload': '#4794EB',
  '--chart-download': '#7FD38D',
  '--chart-purple': '#A5A0F8',
  '--chart-orange': '#F5A623',
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
      return tokens['--chart-upload']
    case 'udp':
      return tokens['--chart-purple']
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
