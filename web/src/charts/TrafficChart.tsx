import { useEffect, useMemo, useRef, useState } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import type { ChartWindow, RateSample } from '../lib/types'
import { formatBitRate } from '../lib/format'
import { areaGradient, chartFont, formatHHMM, formatHHMMSS, lineGradient, usePalette, withAlpha, type ChartPalette } from './chartRuntime'

type TrafficChartProps = {
  samples: RateSample[]
  window: ChartWindow
  height?: number
  className?: string
  ariaLabel?: string
}

type AlignedTriple = [number[], number[], number[]]

type Readout = { download: number; upload: number }

/** Shared 2d context for measuring axis label widths. */
let measureCtx: CanvasRenderingContext2D | null = null

/** Adaptive y-axis width: never clips labels like "100.0 Mbps" (uPlot calls
   this on each redraw with the formatted tick labels). */
function measureAxisSize(_self: uPlot, values: string[] | null): number {
  if (!values || values.length === 0) return 48
  if (!measureCtx) measureCtx = document.createElement('canvas').getContext('2d')
  if (!measureCtx) return 72
  measureCtx.font = chartFont
  const widest = Math.max(...values.map((label) => measureCtx!.measureText(label).width))
  return Math.ceil(widest) + 18
}

function alignSamples(samples: RateSample[]): AlignedTriple {
  const sorted = samples
    .map((sample) => ({ ts: Date.parse(sample.timestamp) / 1000, down: sample.downloadBps, up: sample.uploadBps }))
    .filter((sample) => Number.isFinite(sample.ts))
    .sort((a, b) => a.ts - b.ts)
  return [sorted.map((s) => s.ts), sorted.map((s) => s.down), sorted.map((s) => s.up)]
}

function buildOptions(
  palette: ChartPalette,
  height: number,
  windowRef: { current: ChartWindow },
  onCursor: (index: number | null) => void,
): uPlot.Options {
  return {
    width: 400,
    height,
    padding: [8, 8, 0, 0],
    legend: { show: false },
    cursor: { show: true, y: false, drag: { x: false, y: false } },
    scales: {
      x: { time: true },
      y: { range: (_self, _min, max) => [0, (max ?? 1) * 1.15] },
    },
    axes: [
      {
        stroke: palette.ink3,
        font: chartFont,
        grid: { show: false },
        ticks: { stroke: palette.grid, width: 1 },
        values: (_self, splits) => splits.map((ts) => (windowRef.current === '5m' ? formatHHMMSS(ts) : formatHHMM(ts))),
      },
      {
        stroke: palette.ink3,
        font: chartFont,
        size: measureAxisSize,
        grid: { show: true, stroke: palette.grid, width: 1, dash: [4, 4] },
        ticks: { stroke: palette.grid, width: 1 },
        values: (_self, splits) => splits.map((value) => formatBitRate(value)),
      },
    ],
    series: [
      {},
      {
        label: '下载',
        stroke: (self) => lineGradient(self, palette),
        width: 2.4,
        fill: (self) => areaGradient(self, palette, 0.3),
        fillTo: 0,
        points: { show: false },
      },
      {
        label: '上传',
        stroke: withAlpha(palette.accent2, 0.85),
        width: 1.4,
        dash: [2, 4],
        points: { show: false },
      },
    ],
    hooks: {
      setCursor: [
        (self) => {
          onCursor(self.cursor.idx ?? null)
        },
      ],
    },
  }
}

/**
 * Realtime traffic chart (§8): brand-gradient download line with fading area,
 * dashed accent-2 upload line, dashed horizontal grid, crosshair + header
 * readout instead of a floating tooltip. Switching the window keeps the old
 * curve on screen until fresh samples arrive (parents retain old samples).
 */
export function TrafficChart({ samples, window, height = 280, className = '', ariaLabel = '实时流量' }: TrafficChartProps) {
  const palette = usePalette()
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const windowRef = useRef(window)
  windowRef.current = window
  const data = useMemo(() => alignSamples(samples), [samples])
  const dataRef = useRef(data)
  dataRef.current = data
  const [readout, setReadout] = useState<Readout | null>(null)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return
    const chart = new uPlot(
      buildOptions(palette, height, windowRef, (index) => {
        const current = dataRef.current
        const at = index ?? (current[0].length > 0 ? current[0].length - 1 : null)
        setReadout(at === null ? null : { download: current[1][at] ?? 0, upload: current[2][at] ?? 0 })
      }),
      dataRef.current as uPlot.AlignedData,
      container,
    )
    chartRef.current = chart
    const observer = new ResizeObserver(() => {
      chart.setSize({ width: container.clientWidth, height })
    })
    observer.observe(container)
    return () => {
      observer.disconnect()
      chart.destroy()
      chartRef.current = null
    }
  }, [palette, height])

  useEffect(() => {
    chartRef.current?.setData(data as uPlot.AlignedData)
    const last = data[0].length > 0 ? data[0].length - 1 : null
    setReadout(last === null ? null : { download: data[1][last] ?? 0, upload: data[2][last] ?? 0 })
  }, [data])

  return (
    <div className={`traffic-chart ${className}`.trim()} role="img" aria-label={ariaLabel}>
      <div className="chart-legend">
        <span className="chart-legend-item">
          <i className="chart-dot chart-dot-down" aria-hidden="true" />
          下载 <b className="num">{readout ? formatBitRate(readout.download) : '—'}</b>
        </span>
        <span className="chart-legend-item">
          <i className="chart-dot chart-dot-up" aria-hidden="true" />
          上传 <b className="num">{readout ? formatBitRate(readout.upload) : '—'}</b>
        </span>
      </div>
      <div className="chart-frame" style={{ height }}>
        <div ref={containerRef} style={{ height }} />
        {samples.length === 0 ? <div className="chart-empty faint">暂无流量数据，等待首次采集…</div> : null}
      </div>
    </div>
  )
}
