import { useMemo, useState } from 'react'
import { formatCount, formatRelativeTime, formatUptime, splitBitRate } from '../lib/format'
import { Badge, Button, Card, EmptyState, SearchInput, Skeleton, StatusDot } from '../ui'
import { useShell } from '../shell/useShell'
import { useFleetOverview } from '../features/monitoring/hooks'
import type { FleetDevice } from '../features/monitoring/types'
import './fleet.css'

type StatTone = 'accent' | 'ok' | 'err' | 'warn'

function StatCard({ label, value, sub, tone, icon }: { label: string; value: number | null; sub: string; tone: StatTone; icon: string }) {
  return (
    <div className="glass fleet-stat">
      <span className={`fleet-stat-icon fleet-tone-${tone}`} aria-hidden="true">
        {icon}
      </span>
      <span className="fleet-stat-body">
        <small>{label}</small>
        {value === null ? <Skeleton height={26} width={64} /> : <strong className="num">{formatCount(value)}</strong>}
        <span className="fleet-stat-sub">{sub}</span>
      </span>
    </div>
  )
}

/** Thin usage bar; tone shifts warn >80 / err >95. */
function PercentCell({ label, value }: { label: string; value: number }) {
  const clamped = Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0))
  const tone = clamped > 95 ? 'err' : clamped > 80 ? 'warn' : 'ok'
  return (
    <div className="fleet-cell">
      <small>{label}</small>
      <b className="num">{Math.round(clamped)}%</b>
      <span className={`fleet-bar fleet-bar-${tone}`} role="img" aria-label={`${label} ${Math.round(clamped)}%`}>
        <i style={{ width: `${clamped}%` }} />
      </span>
    </div>
  )
}

function RateCell({ device }: { device: FleetDevice }) {
  const down = splitBitRate(device.downloadBps)
  const up = splitBitRate(device.uploadBps)
  return (
    <div className="fleet-cell">
      <small>流量速率</small>
      <b className="num fleet-rate-down">
        ↓ {down.value} <small>{down.unit}</small>
      </b>
      <span className="num fleet-rate-up">
        ↑ {up.value} {up.unit}
      </span>
    </div>
  )
}

function TerminalCell({ device }: { device: FleetDevice }) {
  const online = Math.max(0, device.terminalOnline)
  const inactive = Math.max(0, device.terminalInactive)
  const offline = Math.max(0, device.terminalOffline)
  const total = Math.max(device.terminalCount, online + inactive + offline)
  return (
    <div className="fleet-cell">
      <small>终端</small>
      <b className="num">
        {formatCount(device.terminalCount)} <small>台</small>
      </b>
      {total > 0 ? (
        <>
          <span
            className="fleet-dist"
            role="img"
            aria-label={`在线 ${online}，空闲 ${inactive}，离线 ${offline}`}
          >
            <i className="fleet-dist-ok" style={{ width: `${(online / total) * 100}%` }} />
            <i className="fleet-dist-warn" style={{ width: `${(inactive / total) * 100}%` }} />
            <i className="fleet-dist-off" style={{ width: `${(offline / total) * 100}%` }} />
          </span>
          <span className="fleet-cell-sub num">
            {online} 在线 · {inactive} 空闲 · {offline} 离线
          </span>
        </>
      ) : (
        <span className="fleet-cell-sub">暂无终端</span>
      )}
    </div>
  )
}

function DeviceRow({ device, onOpen }: { device: FleetDevice; onOpen: (id: string) => void }) {
  const online = device.state === 'online'
  const identity = [device.routerName, device.boardName, device.platform, device.version ? `v${device.version}` : '']
    .filter((part, index, parts) => part && parts.indexOf(part) === index)
    .join(' · ')
  const updatedAt = device.updatedAt ? formatRelativeTime(device.updatedAt) : '尚未成功采集'
  const classes = ['fleet-row', online ? '' : 'fleet-row-offline', device.alerting && online ? 'fleet-row-alert' : '']
    .filter(Boolean)
    .join(' ')

  return (
    <Card className={classes} onClick={() => onOpen(device.id)}>
      <div className="fleet-row-grid">
        <div className="fleet-id">
          <strong>
            <StatusDot tone={online ? 'ok' : 'err'} />
            {device.name}
            {device.alerting && online ? (
              <Badge tone="warn" dot>
                告警
              </Badge>
            ) : null}
            {!online ? (
              <Badge tone="err" dot>
                离线
              </Badge>
            ) : null}
          </strong>
          <small>{identity || 'RouterOS 设备'}</small>
          <small className="faint">{device.address || '未设置地址'}</small>
        </div>
        {online ? (
          <>
            <PercentCell label="CPU" value={device.cpuLoadPercent} />
            <PercentCell label="内存" value={device.memoryUsedPercent} />
            <RateCell device={device} />
            <TerminalCell device={device} />
            <div className="fleet-cell">
              <small>连接</small>
              <b className="num">{formatCount(device.connectionCount)}</b>
              <span className="fleet-cell-sub">活动连接</span>
            </div>
            <div className="fleet-cell">
              <small>运行时间</small>
              <b className="num">{formatUptime(device.uptime)}</b>
              <span className="fleet-cell-sub">更新于 {updatedAt}</span>
            </div>
          </>
        ) : (
          <div className="fleet-offline-note">
            <span className="fleet-offline-error">{device.error || '设备离线，暂时无法采集数据'}</span>
            <span className="fleet-cell-sub num">最后采集：{updatedAt}</span>
          </div>
        )}
        <span className="fleet-chevron" aria-hidden="true">
          ›
        </span>
      </div>
    </Card>
  )
}

function FleetSkeleton() {
  return (
    <>
      <div className="fleet-stats">
        {Array.from({ length: 4 }, (_, index) => (
          <div className="glass fleet-stat" key={index}>
            <Skeleton width={36} height={36} />
            <span className="fleet-stat-body">
              <Skeleton height={12} width="52%" />
              <Skeleton height={26} width="64%" />
              <Skeleton height={11} width="70%" />
            </span>
          </div>
        ))}
      </div>
      <div className="fleet-rows">
        {Array.from({ length: 3 }, (_, index) => (
          <div className="glass fleet-row" key={index}>
            <Skeleton lines={2} height={14} />
          </div>
        ))}
      </div>
    </>
  )
}

export default function FleetPage() {
  const { refreshMs, reloadNonce, selectDevice, navigate } = useShell()
  const { data, loading, error, reload } = useFleetOverview(refreshMs, reloadNonce)
  const [query, setQuery] = useState('')

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    const devices = data?.devices ?? []
    if (!keyword) return devices
    return devices.filter((device) =>
      [device.name, device.routerName, device.boardName, device.version, device.address].join(' ').toLowerCase().includes(keyword),
    )
  }, [data, query])

  const openDevice = (id: string) => {
    selectDevice(id)
    navigate('overview')
  }

  const firstLoad = loading && !data

  return (
    <div className="page">
      <header className="page-head">
        <h1>仪表台</h1>
        <span className="page-sub">全部 RouterOS 设备的健康、流量与告警一览</span>
        <span className="fleet-head-actions">
          <SearchInput value={query} onChange={setQuery} placeholder="搜索设备名称 / 地址" ariaLabel="搜索设备" width={240} />
        </span>
      </header>

      {error && data ? (
        <p className="fleet-error" role="alert">
          <span>刷新失败：{error}（显示的是最近一次成功的数据）</span>
          <Button size="sm" onClick={reload}>
            重试
          </Button>
        </p>
      ) : null}

      {firstLoad ? (
        <FleetSkeleton />
      ) : !data ? (
        <Card>
          <EmptyState
            icon="⚠️"
            title="设备列表读取失败"
            description={error ?? '请检查面板连接后重试'}
            actionLabel="重试"
            onAction={reload}
          />
        </Card>
      ) : (
        <>
          <div className="fleet-stats">
            <StatCard label="全部设备" value={data.totalDevices} sub="已纳管设备" tone="accent" icon="▦" />
            <StatCard label="在线" value={data.onlineDevices} sub="采集正常" tone="ok" icon="✓" />
            <StatCard label="离线" value={data.offlineDevices} sub={data.offlineDevices > 0 ? '需要检查连接' : '无离线设备'} tone="err" icon="⏻" />
            <StatCard label="告警" value={data.alertDevices} sub={data.alertDevices > 0 ? '存在待关注告警' : '一切正常'} tone="warn" icon="⚠" />
          </div>

          {data.devices.length === 0 ? (
            <Card>
              <EmptyState
                icon="🌐"
                title="还没有设备"
                description="添加第一台 RouterOS 设备后，这里会展示它的实时状态。"
                actionLabel="去添加设备"
                onAction={() => navigate('settings')}
              />
            </Card>
          ) : filtered.length === 0 ? (
            <Card>
              <EmptyState icon="⌕" title="没有匹配的设备" description={`没有名称或地址包含「${query.trim()}」的设备。`} />
            </Card>
          ) : (
            <div className="fleet-rows">
              {filtered.map((device) => (
                <DeviceRow key={device.id} device={device} onOpen={openDevice} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
