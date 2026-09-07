import { useEffect, useMemo, useRef } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import type { LoadSample } from '../lib/types'
import { chartFont, formatHHMM, usePalette, type ChartPalette } from './chartRuntime'

export type LoadMetric =
  | 'cpuLoadPercent'
  | 'memoryUsedPercent'
  | 'storageUsedPercent'
  | 'onlineTerminalCount'
  | 'connectionCount'
  | 'uploadBps'
  | 'downloadBps'

type LoadLineChartProps = {
  samples: LoadSample[]
  metric: LoadMetric
  /** series color key — line colors cycle accent / ok / accent-2 / warn / err (§8) */
  tone?: 'accent' | 'ok' | 'accent2' | 'warn' | 'err'
  height?: number
  formatValue?: (value: number) => string
  ariaLabel?: string
}

function toneColor(palette: ChartPalette, tone: NonNullable<LoadLineChartProps['tone']>): string {
  switch (tone) {
    case 'accent':
      return palette.accent
    case 'ok':
      return palette.ok
    case 'accent2':
      return palette.accent2
    case 'warn':
      return palette.warn
    case 'err':
      return palette.err
  }
}

function buildOptions(palette: ChartPalette, height: number, tone: NonNullable<LoadLineChartProps['tone']>, formatValue: (value: number) => string): uPlot.Options {
  const color = toneColor(palette, tone)
  return {
    width: 320,
    height,
    padding: [6, 4, 0, 0],
    legend: { show: false },
    cursor: { show: false, drag: { x: false, y: false } },
    scales: {
      x: { time: true },
      y: { range: (_self, min, max) => [Math.min(0, min ?? 0), (max ?? 1) * 1.1] },
    },
    axes: [
      {
        stroke: palette.ink3,
        font: chartFont,
        grid: { show: false },
        ticks: { show: false },
        values: (_self, splits) => splits.map((ts) => formatHHMM(ts)),
      },
      {
        stroke: palette.ink3,
        font: chartFont,
        size: 48,
        grid: { show: true, stroke: palette.grid, width: 1, dash: [4, 4] },
        ticks: { show: false },
        values: (_self, splits) => splits.map((value) => formatValue(value)),
      },
    ],
    series: [
      {},
      {
        stroke: color,
        width: 1.8,
        points: { show: false },
      },
    ],
  }
}

/** Single-series small line chart for the load-history small multiples (§8). */
export function LoadLineChart({ samples, metric, tone = 'accent', height = 120, formatValue = (value) => String(Math.round(value)), ariaLabel }: LoadLineChartProps) {
  const palette = usePalette()
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)

  const data = useMemo(() => {
    const sorted = samples
      .map((sample) => ({ ts: Date.parse(sample.timestamp) / 1000, value: sample[metric] }))
      .filter((sample) => Number.isFinite(sample.ts) && Number.isFinite(sample.value) && sample.value >= 0)
      .sort((a, b) => a.ts - b.ts)
    return [sorted.map((s) => s.ts), sorted.map((s) => s.value)] as [number[], number[]]
  }, [samples, metric])
  const dataRef = useRef(data)
  dataRef.current = data
  const formatRef = useRef(formatValue)
  formatRef.current = formatValue

  useEffect(() => {
    const container = containerRef.current
    if (!container) return
    const chart = new uPlot(
      buildOptions(palette, height, tone, (value) => formatRef.current(value)),
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
  }, [palette, height, tone])

  useEffect(() => {
    chartRef.current?.setData(data as uPlot.AlignedData)
  }, [data])

  return (
    <div className="load-line-chart" role={ariaLabel ? 'img' : undefined} aria-label={ariaLabel} aria-hidden={ariaLabel ? undefined : true}>
      <div className="chart-frame" style={{ height }}>
        <div ref={containerRef} style={{ height }} />
        {data[0].length === 0 ? <div className="chart-empty faint">暂无历史数据</div> : null}
      </div>
    </div>
  )
}
