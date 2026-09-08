import { useMemo, useState } from 'react'
import { TrafficChart } from '../charts/TrafficChart'
import { formatBytes, formatCount, formatUptime, splitBitRate } from '../lib/format'
import type { ChartWindow, InterfaceStatus, Overview, Terminal, TerminalState } from '../lib/types'
import { useShell } from '../shell/useShell'
import { Badge, Button, Card, DataTable, EmptyState, GaugeRing, Glass, SegTabs, Skeleton, StatusDot, type TableColumn } from '../ui'
import {
  useInterfaceList,
  useRealtimeOverview,
  useTerminalList,
  useTrafficHistory,
} from '../features/monitoring/hooks'
import './overview.css'

const WINDOW_KEY = 'rosboard:ov-window'
const WINDOW_OPTIONS: Array<{ value: ChartWindow; label: string }> = [
  { value: '5m', label: '5m' },
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
]

function readWindow(): ChartWindow {
  try {
    const stored = window.sessionStorage.getItem(WINDOW_KEY)
    return stored === '5m' || stored === '1h' || stored === '6h' || stored === '24h' ? stored : '5m'
  } catch {
    return '5m'
  }
}

function persistWindow(value: ChartWindow) {
  try {
    window.sessionStorage.setItem(WINDOW_KEY, value)
  } catch {
    // storage unavailable — session-only preference
  }
}

/** Gauge gradient stops shift with load (§9.1: warn >80 / err >95). */
function gaugeTone(percent: number): { from?: string; to?: string } {
  if (percent > 95) return { from: 'var(--err)', to: 'var(--err)' }
  if (percent > 80) return { from: 'var(--warn)', to: 'var(--warn)' }
  return {}
}

/** Name heuristics → terminal icon (§9.1 active-terminal grid). */
function terminalIcon(name: string): string {
  const text = name.toLowerCase()
  if (/iphone|ipad|android|手机|平板/.test(text)) return '📱'
  if (/macbook|laptop|thinkpad|notebook|笔记本/.test(text)) return '💻'
  if (/tv|电视|盒子|box/.test(text)) return '📺'
  if (/nas|synology|群晖|server|服务器|存储/.test(text)) return '🗄️'
  if (/switch|ps5|ps4|xbox|game|游戏/.test(text)) return '🎮'
  if (/bot|扫地|机器人|vacuum|homepod|音箱|摄像|camera/.test(text)) return '🤖'
  if (/mac|pc|desktop|台式|电脑|imac|windows/.test(text)) return '🖥️'
  return '🖥️'
}

const TERMINAL_STATE_TONE: Record<TerminalState, 'ok' | 'warn' | 'neutral'> = {
  online: 'ok',
  inactive: 'warn',
  offline: 'neutral',
}

function HeroSkeleton() {
  return (
    <div className="ov-hero-grid">
      <div className="glass ov-hero">
        <Skeleton height={14} width="46%" />
        <Skeleton height={44} width="58%" className="ov-skel-big" />
        <Skeleton height={19} width="72%" />
        <Skeleton height={150} className="ov-skel-chart" />
      </div>
      <div className="glass ov-side">
        <div className="ov-gauges">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} width={84} height={84} className="ov-skel-ring" />
          ))}
        </div>
        <Skeleton lines={4} height={14} />
      </div>
    </div>
  )
}

function Hero({
  overview,
  issueCount,
  deviceName,
  trafficWindow,
  onWindowChange,
}: {
  overview: Overview
  issueCount: number
  /** User-configured device name (面板设置 → 设备管理), preferred over RouterOS identity. */
  deviceName: string
  trafficWindow: ChartWindow
  onWindowChange: (next: ChartWindow) => void
}) {
  const down = splitBitRate(overview.downloadBps)
  const up = splitBitRate(overview.uploadBps)
  const states = overview.terminalStateCounts
  const terminalTotal = Math.max(0, states.online + states.inactive + states.offline)
  const protocols = overview.connectionProtocolCounts
  // The hero shows the name the user gave the device; RouterOS identity is
  // only a fallback. CSS caps it at half the hero width for the window picker.
  const identity = useMemo(() => {
    if (deviceName.trim()) return deviceName.trim()
    const parts = [overview.routerName.trim(), overview.boardName.trim()].filter(Boolean)
    const unique = parts.filter((part, index) => index === 0 || part !== parts[index - 1])
    return unique.join(' · ') || 'RouterOS 设备'
  }, [deviceName, overview.routerName, overview.boardName])
  return (
    <>
      <div className="ov-hero-status">
        <StatusDot tone={issueCount > 0 ? 'warn' : 'ok'} pulse={issueCount === 0} />
        <span className="ov-hero-device" title={identity}>
          {identity}
        </span>
        <span className="faint ov-hero-uptime">已运行 {formatUptime(overview.uptime)}</span>
        <SegTabs className="ov-hero-window" options={WINDOW_OPTIONS} value={trafficWindow} onChange={onWindowChange} ariaLabel="流量时间窗口" />
      </div>
      <div className="ov-big num">
        {down.value} <small>{down.unit} 下载</small>
      </div>
      <div className="ov-metrics">
        <span>
          上传
          <b className="num">
            {up.value} {up.unit}
          </b>
        </span>
        <span>
          在线终端
          <b className="num">
            {formatCount(states.online)} / {formatCount(terminalTotal)}
          </b>
        </span>
        <span>
          活动连接
          <b className="num">{formatCount(overview.connectionCount)}</b>
        </span>
        <span>
          TCP · UDP
          <b className="num">
            {formatCount(protocols.tcp)} · {formatCount(protocols.udp)}
          </b>
        </span>
      </div>
    </>
  )
}

function formatFrequency(mhz: string): string {
  const value = Number(mhz)
  if (!Number.isFinite(value) || value <= 0) return mhz.trim()
  return value >= 1000 ? `${(value / 1000).toFixed(1)} GHz` : `${Math.round(value)} MHz`
}

function cpuLine(overview: Overview): string {
  const model = overview.system.cpu.trim()
  const spec = [overview.system.cpuCount.trim() ? `${overview.system.cpuCount.trim()}核` : '', overview.system.cpuFrequency.trim() ? `@ ${formatFrequency(overview.system.cpuFrequency)}` : '']
    .filter(Boolean)
    .join(' ')
  // Spec first, model last: if the value ever truncates, the tail (model)
  // is the expendable part and remains available via the title tooltip.
  return [spec, model].filter(Boolean).join(' · ') || '-'
}

function capacityLine(usedBytes: number, totalBytes: number): string {
  if (totalBytes <= 0) return '-'
  return `${formatBytes(usedBytes)} / ${formatBytes(totalBytes)}`
}

function SidePanel({ overview }: { overview: Overview }) {
  const platformArch = [overview.platform.trim(), overview.system.architectureName.trim()].filter(Boolean).join(' · ')
  return (
    <>
      <div className="ov-gauges">
        <GaugeRing percent={overview.cpuLoadPercent} label="CPU" {...gaugeTone(overview.cpuLoadPercent)} />
        <GaugeRing percent={overview.memoryUsedPercent} label="内存" {...gaugeTone(overview.memoryUsedPercent)} />
        <GaugeRing percent={overview.storageUsedPercent} label="存储" {...gaugeTone(overview.storageUsedPercent)} />
      </div>
      <div className="ov-meta">
        <div className="ov-fact">
          <span>ROS 版本</span>
          <b>{overview.version ? `v${overview.version}` : '-'}</b>
        </div>
        <div className="ov-fact">
          <span>平台架构</span>
          <b title={platformArch}>{platformArch || '-'}</b>
        </div>
        <div className="ov-fact">
          <span>CPU</span>
          <b title={cpuLine(overview)}>{cpuLine(overview)}</b>
        </div>
        <div className="ov-fact">
          <span>内存</span>
          <b>{capacityLine(overview.memoryUsedBytes, overview.memoryTotalBytes)}</b>
        </div>
        <div className="ov-fact">
          <span>存储</span>
          <b>{capacityLine(overview.storageUsedBytes, overview.storageTotalBytes)}</b>
        </div>
      </div>
    </>
  )
}

function TerminalCard({ terminal, onOpen }: { terminal: Terminal; onOpen: () => void }) {
  const name = terminal.displayName || terminal.autoName || terminal.macAddress || '未知终端'
  const address = terminal.primaryIpv4 || terminal.primaryIpv6 || terminal.macAddress || '-'
  const down = splitBitRate(terminal.currentDownloadBps)
  const up = splitBitRate(terminal.currentUploadBps)
  const busy = terminal.currentDownloadBps > 0 || terminal.currentUploadBps > 0
  return (
    <Card className="ov-term" onClick={onOpen}>
      <span className="ov-term-av" aria-hidden="true">
        {terminalIcon(name)}
      </span>
      <span className="ov-term-id">
        <b>
          <StatusDot tone={TERMINAL_STATE_TONE[terminal.state] ?? 'neutral'} />
          {name}
        </b>
        <small className="num">
          {address} · {formatCount(terminal.connectionCount)} 连接
        </small>
      </span>
      {busy ? (
        <span className="ov-term-rate num">
          <b>
            ↓ {down.value} <small>{down.unit}</small>
          </b>
          <small>
            ↑ {up.value} {up.unit}
          </small>
        </span>
      ) : (
        <span className="ov-term-rate ov-term-idle">空闲</span>
      )}
    </Card>
  )
}

const INTERFACE_STATE: Array<{ match: (row: InterfaceStatus) => boolean; tone: 'ok' | 'warn' | 'neutral'; label: string }> = [
  { match: (row) => row.running && !row.disabled, tone: 'ok', label: '在线' },
  { match: (row) => row.disabled, tone: 'neutral', label: '已禁用' },
  { match: () => true, tone: 'warn', label: 'Down' },
]

function interfaceBadge(row: InterfaceStatus) {
  const state = INTERFACE_STATE.find((candidate) => candidate.match(row)) ?? INTERFACE_STATE[INTERFACE_STATE.length - 1]
  return (
    <Badge tone={state.tone} dot>
      {state.label}
    </Badge>
  )
}

const interfaceColumns: Array<TableColumn<InterfaceStatus>> = [
  {
    key: 'name',
    title: '接口',
    width: '18%',
    render: (row) => <strong>{row.name}</strong>,
  },
  {
    key: 'type',
    title: '类型',
    width: '12%',
    render: (row) => <span className="faint">{row.type || '—'}</span>,
  },
  {
    key: 'address',
    title: '地址',
    width: '22%',
    render: (row) => <span className="num faint">{row.addresses[0] ?? '—'}</span>,
  },
  {
    key: 'down',
    title: '↓ 下载',
    numeric: true,
    width: '16%',
    render: (row) => {
      const down = splitBitRate(row.currentRxBps)
      return (
        <span className="num ov-rate-down">
          {down.value} <small>{down.unit}</small>
        </span>
      )
    },
  },
  {
    key: 'up',
    title: '↑ 上传',
    numeric: true,
    width: '16%',
    render: (row) => {
      const up = splitBitRate(row.currentTxBps)
      return (
        <span className="num faint">
          {up.value} <small>{up.unit}</small>
        </span>
      )
    },
  },
  {
    key: 'state',
    title: '状态',
    width: '12%',
    render: (row) => interfaceBadge(row),
  },
]

export default function OverviewPage() {
  const { selectedDeviceId, refreshMs, reloadNonce, alerts, warnings, devices, navigate } = useShell()
  const deviceId = selectedDeviceId
  const realtime = useRealtimeOverview(deviceId, refreshMs, reloadNonce)
  const terminals = useTerminalList(deviceId, refreshMs, reloadNonce)
  const interfaces = useInterfaceList(deviceId, refreshMs, reloadNonce)
  const [trafficWindow, setTrafficWindow] = useState<ChartWindow>(readWindow)
  const traffic = useTrafficHistory(deviceId, trafficWindow, refreshMs, reloadNonce)

  const issueCount = alerts.length + warnings.length
  const deviceName = devices.find((device) => device.id === deviceId)?.name ?? ''
  const overview = realtime.data
  const firstLoad = realtime.loading && !overview
  const pageError = realtime.error ?? terminals.error ?? interfaces.error ?? traffic.error

  const topTerminals = useMemo(
    () =>
      [...(terminals.data ?? [])]
        .sort((left, right) => right.currentDownloadBps + right.currentUploadBps - (left.currentDownloadBps + left.currentUploadBps))
        .slice(0, 8),
    [terminals.data],
  )
  const terminalTotal = terminals.data?.length ?? 0

  const topInterfaces = useMemo(
    () =>
      [...(interfaces.data ?? [])]
        .sort((left, right) => right.currentRxBps + right.currentTxBps - (left.currentRxBps + left.currentTxBps))
        .slice(0, 6),
    [interfaces.data],
  )

  const reloadAll = () => {
    realtime.reload()
    terminals.reload()
    interfaces.reload()
    traffic.reload()
  }

  const changeWindow = (next: ChartWindow) => {
    setTrafficWindow(next)
    persistWindow(next)
  }

  if (!deviceId) {
    return (
      <div className="page">
        <header className="page-head">
          <h1>系统概览</h1>
        </header>
        <Card>
          <EmptyState
            icon="🌐"
            title="还没有可用设备"
            description="添加一台 RouterOS 设备后，这里会展示它的实时概览。"
            actionLabel="去添加设备"
            onAction={() => navigate('settings')}
          />
        </Card>
      </div>
    )
  }

  return (
    <div className="page">
      <header className="page-head">
        <h1>系统概览</h1>
        <span className="page-sub">{overview ? `${overview.routerName || '当前设备'} 的实时运行状态` : '当前设备的实时运行状态'}</span>
      </header>

      {pageError && overview ? (
        <p className="ov-error" role="alert">
          <span>刷新失败：{pageError}（显示的是最近一次成功的数据）</span>
          <Button size="sm" onClick={reloadAll}>
            重试
          </Button>
        </p>
      ) : null}

      {firstLoad ? (
        <HeroSkeleton />
      ) : !overview ? (
        <div className="ov-hero-grid">
          <div className="glass ov-hero">
            <EmptyState
              icon="⚠️"
              title="暂时无法读取设备状态"
              description={realtime.error ?? '设备可能离线，请稍后重试。'}
              actionLabel="重试"
              onAction={reloadAll}
            />
          </div>
          <div className="glass ov-side">
            <Skeleton lines={4} height={14} />
          </div>
        </div>
      ) : (
        <div className="ov-hero-grid">
          <Glass className="ov-hero">
            <Hero overview={overview} issueCount={issueCount} deviceName={deviceName} trafficWindow={trafficWindow} onWindowChange={changeWindow} />
            <div className="ov-chart">
              <TrafficChart
                samples={traffic.data?.samples ?? []}
                window={trafficWindow}
                height={180}
                className="ov-chart-bleed"
                ariaLabel="实时流量曲线"
              />
            </div>
          </Glass>
          <Glass className="ov-side">
            <SidePanel overview={overview} />
          </Glass>
        </div>
      )}

      <div className="section-head">
        <h2>活跃终端</h2>
        <span className="section-sub">按实时速率排序</span>
        {terminalTotal > 0 ? (
          <button type="button" className="section-link" onClick={() => navigate('terminals')}>
            查看全部 {formatCount(terminalTotal)} 台 →
          </button>
        ) : null}
      </div>
      {terminals.loading && !terminals.data ? (
        <div className="ov-terms">
          {Array.from({ length: 4 }, (_, index) => (
            <div className="glass ov-term" key={index}>
              <Skeleton width={40} height={40} />
              <Skeleton lines={2} height={12} />
            </div>
          ))}
        </div>
      ) : topTerminals.length === 0 ? (
        <Card>
          <EmptyState icon="📡" title="暂无终端" description="还没有识别到任何终端，等待采集器完成一次扫描。" />
        </Card>
      ) : (
        <div className="ov-terms">
          {topTerminals.map((terminal) => (
            <TerminalCard key={terminal.id} terminal={terminal} onOpen={() => navigate('terminals')} />
          ))}
        </div>
      )}

      <Card
        title="接口状态"
        sub={interfaces.data ? `${interfaces.data.length} 个接口 · 按速率排序` : '按速率排序'}
        actions={
          <button type="button" className="link-button" onClick={() => navigate('interfaces')}>
            查看全部 →
          </button>
        }
      >
        <DataTable
          columns={interfaceColumns}
          rows={topInterfaces}
          rowKey={(row) => row.name}
          loading={interfaces.loading && !interfaces.data}
          emptyTitle="暂无接口数据"
          emptyDescription="等待首次采集完成后展示接口状态。"
          onRowClick={() => navigate('interfaces')}
          ariaLabel="接口状态表"
        />
      </Card>
    </div>
  )
}
