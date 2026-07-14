import { useEffect, useRef } from 'react'
import * as echarts from 'echarts/core'
import type { ECharts, EChartsCoreOption } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { formatBitRate } from '../lib/format'
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

function tooltipHTML(params: unknown) {
  const items = (Array.isArray(params) ? params : [params]) as Array<{
    axisValueLabel?: string
    axisValue?: string
    marker?: string
    seriesName?: string
    value?: number
  }>
  if (!items.length) return ''
  const lines = [`<div style="margin-bottom:4px;color:#b9cfee">时间：${items[0]?.axisValueLabel || items[0]?.axisValue || '-'}</div>`]
  for (const item of items) {
    lines.push(`<div>${item.marker || ''}${item.seriesName || ''}：${formatBitRate(Number(item.value) || 0)}</div>`)
  }
  return lines.join('')
}

function baseOption(reducedMotion: boolean): EChartsCoreOption {
  return {
    animation: !reducedMotion,
    animationDuration: 520,
    animationEasing: 'cubicOut',
    animationDurationUpdate: 700,
    animationEasingUpdate: 'cubicOut',
    textStyle: {
      color: '#64748b',
      fontFamily: 'PingFang SC, Microsoft YaHei, Inter, system-ui, sans-serif',
    },
    tooltip: {
      trigger: 'axis',
      borderWidth: 1,
      borderColor: 'rgba(148, 163, 184, .32)',
      backgroundColor: 'rgba(15, 23, 42, .94)',
      textStyle: { color: '#f8fafc', fontSize: 13 },
      formatter: tooltipHTML,
      axisPointer: {
        type: 'line',
        lineStyle: { type: 'dashed', color: 'rgba(100, 116, 139, .62)', width: 1 },
      },
    },
    grid: { top: 18, left: 78, right: 18, bottom: 38 },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: [],
      axisTick: { show: false },
      axisLine: { lineStyle: { color: 'rgba(148, 163, 184, .32)' } },
      axisLabel: { color: '#7b8798', fontSize: 12 },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      min: 0,
      axisTick: { show: false },
      axisLine: { show: false },
      axisLabel: { color: '#7b8798', fontSize: 12, formatter: (value: number) => formatBitRate(value) },
      splitLine: { lineStyle: { color: 'rgba(148, 163, 184, .18)', type: 'dashed' } },
    },
    series: [
      {
        id: 'download-series',
        name: DOWNLOAD_SERIES,
        type: 'line',
        smooth: 0.26,
        showSymbol: false,
        symbol: 'circle',
        symbolSize: 5,
        lineStyle: { width: 2, color: '#2563eb' },
        itemStyle: { color: '#2563eb' },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: 'rgba(37, 99, 235, .24)' },
            { offset: 1, color: 'rgba(37, 99, 235, .02)' },
          ]),
        },
        emphasis: { focus: 'series', scale: false },
        data: [],
      },
      {
        id: 'upload-series',
        name: UPLOAD_SERIES,
        type: 'line',
        smooth: 0.26,
        showSymbol: false,
        symbol: 'circle',
        symbolSize: 5,
        lineStyle: { width: 2, color: '#16a34a' },
        itemStyle: { color: '#16a34a' },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: 'rgba(22, 163, 74, .20)' },
            { offset: 1, color: 'rgba(22, 163, 74, .02)' },
          ]),
        },
        emphasis: { focus: 'series', scale: false },
        data: [],
      },
    ],
  }
}

export function RealtimeTrafficChart(props: { samples: RateSample[]; ariaLabel?: string }) {
  const chartElement = useRef<HTMLDivElement | null>(null)
  const chart = useRef<ECharts | null>(null)

  useEffect(() => {
    if (!chartElement.current) return
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const instance = echarts.init(chartElement.current, undefined, { renderer: 'canvas' })
    chart.current = instance
    instance.setOption(baseOption(reducedMotion))

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

  useEffect(() => {
    chart.current?.setOption({
      xAxis: { data: props.samples.map((sample) => formatTime(sample.timestamp)) },
      series: [
        { id: 'download-series', name: DOWNLOAD_SERIES, data: props.samples.map((sample) => sample.downloadBps) },
        { id: 'upload-series', name: UPLOAD_SERIES, data: props.samples.map((sample) => sample.uploadBps) },
      ],
    }, { notMerge: false, lazyUpdate: true, silent: true })
  }, [props.samples])

  return <div ref={chartElement} className="realtime-traffic-chart" role="img" aria-label={props.ariaLabel || '实时上传和下载速率趋势'} />
}
