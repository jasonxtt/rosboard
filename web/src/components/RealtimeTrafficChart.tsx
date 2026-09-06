import { useEffect, useRef } from 'react'
import * as echarts from 'echarts/core'
import type { ECharts, EChartsCoreOption } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { formatBitRate } from '../lib/format'
import { useThemeTokens, withAlpha, type ThemeTokens } from '../lib/themeTokens'
import type { RateSample } from '../lib/types'

echarts.use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

const DOWNLOAD_SERIES = '下载'
const UPLOAD_SERIES = '上传'

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? ''
    : date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function tooltipHTML(labelColor: string) {
  return (params: unknown) => {
    const items = (Array.isArray(params) ? params : [params]) as Array<{
      axisValueLabel?: string
      axisValue?: string
      marker?: string
      seriesName?: string
      value?: number
    }>
    if (!items.length) return ''
    const lines = [`<div style="margin-bottom:4px;color:${labelColor}">时间：${items[0]?.axisValueLabel || items[0]?.axisValue || '-'}</div>`]
    for (const item of items) {
      lines.push(`<div>${item.marker || ''}${item.seriesName || ''}：${formatBitRate(Number(item.value) || 0)}</div>`)
    }
    return lines.join('')
  }
}

function seriesOption(id: string, name: string, color: string, areaAlpha: number) {
  return {
    id,
    name,
    type: 'line' as const,
    smooth: 0.26,
    showSymbol: false,
    symbol: 'circle',
    symbolSize: 5,
    lineStyle: { width: 2, color },
    itemStyle: { color },
    areaStyle: {
      color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
        { offset: 0, color: withAlpha(color, areaAlpha) },
        { offset: 1, color: withAlpha(color, 0.02) },
      ]),
    },
    emphasis: { focus: 'series' as const, scale: false },
    data: [] as number[],
  }
}

function baseOption(tokens: ThemeTokens, reducedMotion: boolean): EChartsCoreOption {
  const label = tokens['--stone']
  const axis = tokens['--hairline']
  const grid = withAlpha(tokens['--stone'], 0.2)
  return {
    animation: !reducedMotion,
    animationDuration: 520,
    animationEasing: 'cubicOut',
    animationDurationUpdate: 700,
    animationEasingUpdate: 'cubicOut',
    textStyle: { color: label, fontFamily: tokens['--font-sans'] },
    tooltip: {
      trigger: 'axis',
      borderWidth: 1,
      borderColor: withAlpha(tokens['--on-code'], 0.14),
      backgroundColor: tokens['--surface-code'],
      textStyle: { color: tokens['--on-code'], fontSize: 13, fontFamily: tokens['--font-sans'] },
      formatter: tooltipHTML(tokens['--muted']),
      axisPointer: {
        type: 'line',
        lineStyle: { type: 'dashed', color: withAlpha(tokens['--stone'], 0.62), width: 1 },
      },
    },
    grid: { top: 18, left: 78, right: 18, bottom: 38 },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: [],
      axisTick: { show: false },
      axisLine: { lineStyle: { color: axis } },
      axisLabel: { color: label, fontSize: 12, fontFamily: tokens['--font-mono'] },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      min: 0,
      axisTick: { show: false },
      axisLine: { show: false },
      axisLabel: { color: label, fontSize: 12, fontFamily: tokens['--font-mono'], formatter: (value: number) => formatBitRate(value) },
      splitLine: { lineStyle: { color: grid, type: 'dashed' } },
    },
    series: [
      seriesOption('upload-series', UPLOAD_SERIES, tokens['--chart-upload'], 0.15),
      seriesOption('download-series', DOWNLOAD_SERIES, tokens['--chart-download'], 0.15),
    ],
  }
}

export function RealtimeTrafficChart(props: { samples: RateSample[]; ariaLabel?: string }) {
  const chartElement = useRef<HTMLDivElement | null>(null)
  const chart = useRef<ECharts | null>(null)
  const samples = useRef(props.samples)
  const tokens = useThemeTokens()
  samples.current = props.samples

  useEffect(() => {
    if (!chartElement.current) return
    const instance = echarts.init(chartElement.current, undefined, { renderer: 'canvas' })
    chart.current = instance
    const resizeObserver = new ResizeObserver(() => instance.resize())
    resizeObserver.observe(chartElement.current)
    const resize = () => instance.resize()
    window.addEventListener('resize', resize)
    return () => {
      window.removeEventListener('resize', resize)
      resizeObserver.disconnect()
      instance.dispose()
      chart.current = null
    }
  }, [])

  // Re-applies the whole palette whenever the theme tokens change.
  useEffect(() => {
    const instance = chart.current
    if (!instance) return
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    instance.setOption(baseOption(tokens, reducedMotion))
    instance.setOption({
      xAxis: { data: samples.current.map((sample) => formatTime(sample.timestamp)) },
      series: [
        { id: 'upload-series', name: UPLOAD_SERIES, data: samples.current.map((sample) => sample.uploadBps) },
        { id: 'download-series', name: DOWNLOAD_SERIES, data: samples.current.map((sample) => sample.downloadBps) },
      ],
    }, { notMerge: false, lazyUpdate: true, silent: true })
  }, [tokens])

  useEffect(() => {
    chart.current?.setOption({
      xAxis: { data: props.samples.map((sample) => formatTime(sample.timestamp)) },
      series: [
        { id: 'upload-series', name: UPLOAD_SERIES, data: props.samples.map((sample) => sample.uploadBps) },
        { id: 'download-series', name: DOWNLOAD_SERIES, data: props.samples.map((sample) => sample.downloadBps) },
      ],
    }, { notMerge: false, lazyUpdate: true, silent: true })
  }, [props.samples])

  return <div ref={chartElement} className="realtime-traffic-chart" role="img" aria-label={props.ariaLabel || '实时上传和下载速率趋势'} />
}
