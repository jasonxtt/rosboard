import { useEffect, useState } from 'react'
import type uPlot from 'uplot'

/** CSS-token palette read from the live document, so charts follow data-theme. */
export type ChartPalette = {
  ink3: string
  grid: string
  accent: string
  accent2: string
  ok: string
  warn: string
  err: string
  gradFrom: string
  gradTo: string
  series: string[]
}

function cssVar(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

export function readPalette(): ChartPalette {
  return {
    ink3: cssVar('--ink-3', 'rgba(226,232,255,.38)'),
    grid: cssVar('--grid-line', 'rgba(255,255,255,.11)'),
    accent: cssVar('--series-1', '#67e8f9'),
    accent2: cssVar('--series-2', '#c4b5fd'),
    ok: cssVar('--series-3', '#6ee7b7'),
    warn: cssVar('--series-4', '#fcd34d'),
    err: cssVar('--series-5', '#fda4af'),
    gradFrom: cssVar('--grad-line-from', '#818cf8'),
    gradTo: cssVar('--grad-line-to', '#67e8f9'),
    series: [
      cssVar('--series-1', '#67e8f9'),
      cssVar('--series-2', '#c4b5fd'),
      cssVar('--series-3', '#6ee7b7'),
      cssVar('--series-4', '#fcd34d'),
      cssVar('--series-5', '#fda4af'),
    ],
  }
}

/** Subscribe to <html data-theme> flips and re-read the token palette. */
export function usePalette(): ChartPalette {
  const [palette, setPalette] = useState<ChartPalette>(() => readPalette())
  useEffect(() => {
    const observer = new MutationObserver(() => setPalette(readPalette()))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'style'] })
    return () => observer.disconnect()
  }, [])
  return palette
}

export function withAlpha(hex: string, alpha: number): string {
  const match = /^#([0-9a-f]{6})$/i.exec(hex.trim())
  if (!match) return hex
  const value = Number.parseInt(match[1], 16)
  const r = (value >> 16) & 0xff
  const g = (value >> 8) & 0xff
  const b = value & 0xff
  return `rgba(${r},${g},${b},${alpha})`
}

/** Horizontal brand gradient stroke for the primary series. */
export function lineGradient(self: uPlot, palette: ChartPalette): CanvasGradient {
  const { left, width } = self.bbox
  const gradient = self.ctx.createLinearGradient(left, 0, left + width, 0)
  gradient.addColorStop(0, palette.gradFrom)
  gradient.addColorStop(1, palette.gradTo)
  return gradient
}

/** Vertical area fill fading to transparent below the primary series. */
export function areaGradient(self: uPlot, palette: ChartPalette, alpha = 0.3): CanvasGradient {
  const { top, height } = self.bbox
  const gradient = self.ctx.createLinearGradient(0, top, 0, top + height)
  gradient.addColorStop(0, withAlpha(palette.accent, alpha))
  gradient.addColorStop(1, withAlpha(palette.accent, 0))
  return gradient
}

export const chartFont = '11px -apple-system, "SF Pro Rounded", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif'

export function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

export function formatHHMMSS(unixSeconds: number): string {
  const date = new Date(unixSeconds * 1000)
  return `${pad2(date.getHours())}:${pad2(date.getMinutes())}:${pad2(date.getSeconds())}`
}

export function formatHHMM(unixSeconds: number): string {
  const date = new Date(unixSeconds * 1000)
  return `${pad2(date.getHours())}:${pad2(date.getMinutes())}`
}
