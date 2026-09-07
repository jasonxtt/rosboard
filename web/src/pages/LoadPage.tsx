import { useEffect, useMemo, useRef, useState } from 'react'
import { LoadLineChart, type LoadMetric } from '../charts/LoadLineChart'
import { formatBitRate } from '../lib/format'
import type { LoadSample } from '../lib/types'
import { useShell } from '../shell/useShell'
import { Card, EmptyState, SegTabs, Skeleton } from '../ui'
import { fetchLoadHistory, type LoadWindow } from '../features/monitor-detail/api'
import { useMonitorResource } from '../features/monitor-detail/hooks'
import './monitor-common.css'
import './load.css'

const WINDOW_OPTIONS: Array<{ value: LoadWindow; label: string }> = [
  { value: '1h', label: '1 小时' },
  { value: '1d', label: '1 天' },
  { value: '1w', label: '1 周' },
  { value: '1m', label: '1 月' },
]

type MetricSpec = {
  key: string
  title: string
  metric: LoadMetric
  tone: 'accent' | 'ok' | 'accent2' | 'warn' | 'err'
  format: (value: number) => string
  /** derive the plotted value from a sample (default: sample[metric]) */
  value?: (sample: LoadSample) => number
}

const percent = (value: number) => `${value.toFixed(1)}%`

const METRICS: MetricSpec[] = [
  { key: 'cpu', title: 'CPU 使用率', metric: 'cpuLoadPercent', tone: 'accent', format: percent },
  { key: 'memory', title: '内存使用率', metric: 'memoryUsedPercent', tone: 'ok', format: percent },
  { key: 'storage', title: '存储使用率', metric: 'storageUsedPercent', tone: 'accent2', format: percent },
  { key: 'terminals', title: '在线终端', metric: 'onlineTerminalCount', tone: 'warn', format: (value) => `${Math.round(value)} 台` },
  {
    key: 'throughput',
    title: '总吞吐',
    metric: 'uploadBps',
    tone: 'err',
    format: formatBitRate,
    value: (sample) => sample.uploadBps + sample.downloadBps,
  },
]

function metricStats(samples: LoadSample[], spec: MetricSpec): { current: number; peak: number; average: number } | null {
  const values = samples.map((sample) => (spec.value ? spec.value(sample) : sample[spec.metric])).filter((value) => Number.isFinite(value) && value >= 0)
  if (!values.length) return null
  const sum = values.reduce((total, value) => total + value, 0)
  return { current: values[values.length - 1], peak: Math.max(...values), average: sum / values.length }
}

export default function LoadPage() {
  const { scopedPath, selectedDeviceId, refreshMs, reloadNonce } = useShell()
  const [range, setRange] = useState<LoadWindow>('1h')
  const cacheRef = useRef(new Map<LoadWindow, LoadSample[]>())
  const { data, loading, error, reload } = useMonitorResource(
    () => fetchLoadHistory(scopedPath, range).then((result) => ({ range, samples: result })),
    refreshMs,
    [selectedDeviceId, range, reloadNonce],
  )

  useEffect(() => {
    if (data) cacheRef.current.set(data.range, data.samples)
  }, [data])

  // 切窗口先显示该窗口的缓存曲线（§8：切换不清空已有曲线），新数据到达后替换。
  const samples = useMemo(() => {
    if (data?.range === range) return data.samples
    return cacheRef.current.get(range) ?? data?.samples ?? []
  }, [data, range])
  const hasAnySamples = samples.length > 0 || cacheRef.current.size > 0

  return (
    <div className="page load-page">
      <header className="page-head">
        <h1>负载历史</h1>
        <span className="page-sub">按分钟聚合，最长保留 35 天</span>
      </header>

      <div className="mon-toolbar load-toolbar">
        <SegTabs options={WINDOW_OPTIONS} value={range} onChange={setRange} ariaLabel="历史范围" />
      </div>

      {error && hasAnySamples ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      {loading && !hasAnySamples ? (
        <div className="load-grid">
          {METRICS.map((spec) => (
            <Card key={spec.key} title={spec.title}>
              <Skeleton height={140} />
            </Card>
          ))}
        </div>
      ) : error && !hasAnySamples ? (
        <Card>
          <EmptyState icon="⚠️" title="负载历史读取失败" description={error} actionLabel="重试" onAction={reload} />
        </Card>
      ) : (
        <div className="load-grid">
          {METRICS.map((spec) => {
            const stats = metricStats(samples, spec)
            const derive = spec.value
            const chartSamples = derive ? samples.map((sample) => ({ ...sample, uploadBps: derive(sample) })) : samples
            return (
              <Card
                key={spec.key}
                title={spec.title}
                className="load-metric-card"
                actions={
                  stats ? (
                    <span className="load-stat-head faint num">
                      当前 {spec.format(stats.current)} · 峰值 {spec.format(stats.peak)} · 平均 {spec.format(stats.average)}
                    </span>
                  ) : null
                }
              >
                <LoadLineChart samples={chartSamples} metric={spec.metric} tone={spec.tone} height={140} formatValue={spec.format} ariaLabel={`${spec.title}历史趋势`} />
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
