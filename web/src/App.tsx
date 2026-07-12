import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import rosboardMark from './assets/rosboard-mark.svg'
import {
  compareTerminal,
  formatBitRate,
  formatBits,
  formatBytes,
  formatDateTime,
  formatEndpoint,
  formatOnlineDuration,
  formatSeconds,
  formatShortTime,
  terminalMetrics,
  terminalPrimaryAddress,
  terminalStateText,
  viewTitle,
} from './lib/format'
import type {
  ActiveView,
  ConnectionFamily,
  DashboardResponse,
  InterfaceDetail,
  InterfaceStatus,
  LoadSample,
  PolicyStat,
  ProtocolHistorySample,
  ProtocolStat,
  RouteStat,
  Terminal,
  TerminalDetail,
  TerminalFamily,
  TerminalSortKey,
  TerminalTab,
} from './lib/types'

type IconName = 'overview' | 'status' | 'network' | 'terminal' | 'traffic' | 'policy' | 'runtime' | 'route' | 'refresh' | 'cpu' | 'memory' | 'connections' | 'shield' | 'router' | 'storage' | 'alert' | 'info' | 'check'

const RealtimeTrafficChart = lazy(() => import('./components/RealtimeTrafficChart').then((module) => ({ default: module.RealtimeTrafficChart })))

function Icon(props: { name: IconName }) {
  const paths: Record<IconName, React.ReactNode> = {
    overview: <><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></>,
    status: <><circle cx="12" cy="12" r="9"/><path d="m8 12 2.5 2.5L16 9"/></>,
    network: <><circle cx="5" cy="12" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="19" cy="18" r="2"/><path d="m7 11 10-4M7 13l10 4"/></>,
    terminal: <><rect x="3" y="4" width="18" height="15" rx="2"/><path d="M8 22h8M9 9l2 2-2 2m4 0h3"/></>,
    traffic: <><path d="M5 20V10m5 10V4m5 16v-7m5 7V7"/></>,
    policy: <><path d="M12 3 4 7v5c0 5 3.4 8 8 9 4.6-1 8-4 8-9V7l-8-4Z"/><path d="m9 12 2 2 4-4"/></>,
    runtime: <><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m6 14 3-3 3 2 4-5 2 3"/></>,
    route: <><circle cx="6" cy="18" r="2"/><circle cx="18" cy="6" r="2"/><path d="M8 18h3a4 4 0 0 0 4-4v-3a3 3 0 0 1 3-3"/></>,
    refresh: <><path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/></>,
    cpu: <><rect x="7" y="7" width="10" height="10" rx="1"/><path d="M9 1v3m6-3v3M9 20v3m6-3v3M20 9h3m-3 6h3M1 9h3m-3 6h3M10 10h4v4h-4z"/></>,
    memory: <><path d="M4 7h16v10H4zM7 4v3m4-3v3m4-3v3m3-3v3M7 17v3m4-3v3m4-3v3"/></>,
    connections: <><circle cx="6" cy="7" r="3"/><circle cx="18" cy="7" r="3"/><circle cx="12" cy="18" r="3"/><path d="m8.5 9 2 6m5-6-2 6M9 7h6"/></>,
    shield: <><path d="M12 3 4 7v5c0 5 3.4 8 8 9 4.6-1 8-4 8-9V7l-8-4Z"/><path d="m9 12 2 2 4-4"/></>,
    router: <><rect x="3" y="7" width="18" height="11" rx="2"/><path d="M7 12h.01M11 12h.01M15 12h3M8 7V4m8 3V4"/></>,
    storage: <><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v7c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12v7c0 1.7 3.6 3 8 3s8-1.3 8-3v-7"/></>,
    alert: <><path d="M12 3 2.5 20h19L12 3Z"/><path d="M12 9v5m0 3h.01"/></>,
    info: <><circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10h.01"/></>,
    check: <><circle cx="12" cy="12" r="9"/><path d="m8 12 2.5 2.5L16 9"/></>,
  }
  return <svg className="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[props.name]}</svg>
}

function NavLabel(props: { icon: IconName; label: string }) { return <span className="nav-label"><Icon name={props.icon} /><span>{props.label}</span></span> }

function relativeUpdateTime(value: string) {
  const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000))
  if (seconds < 10) return '刚刚'
  if (seconds < 60) return `${seconds} 秒前`
  return `${Math.floor(seconds / 60)} 分钟前`
}

function App() {
  const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
  const [activeView, setActiveView] = useState<ActiveView>('overview')
  const [query, setQuery] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [selectedTerminalID, setSelectedTerminalID] = useState<string | null>(null)
  const [terminalDetail, setTerminalDetail] = useState<TerminalDetail | null>(null)
  const [terminalTab, setTerminalTab] = useState<TerminalTab>('basic')
  const [connectionFamily, setConnectionFamily] = useState<ConnectionFamily>('all')
  const [detailScope, setDetailScope] = useState<TerminalFamily>('all')
  const [editingTerminalID, setEditingTerminalID] = useState<string | null>(null)
  const [customNameDraft, setCustomNameDraft] = useState('')
  const [remarkDraft, setRemarkDraft] = useState('')
  const [savingRemark, setSavingRemark] = useState(false)
  const [terminalFamily, setTerminalFamily] = useState<TerminalFamily>('all')
  const [dashboardRefreshMs, setDashboardRefreshMs] = useState(5000)
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [loadWindow, setLoadWindow] = useState('1h')
  const [loadSamples, setLoadSamples] = useState<LoadSample[]>([])
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [statusExpanded, setStatusExpanded] = useState(true)
  const [expandedMonitorGroup, setExpandedMonitorGroup] = useState<'terminals' | 'traffic' | 'runtime' | null>(null)

  useEffect(() => {
    if (activeView === 'overview') return
    setStatusExpanded(true)
    if (activeView === 'terminals') setExpandedMonitorGroup('terminals')
    if (activeView === 'protocols' || activeView === 'policies') setExpandedMonitorGroup('traffic')
    if (activeView === 'load' || activeView === 'routes') setExpandedMonitorGroup('runtime')
  }, [activeView])

  useEffect(() => {
    let cancelled = false

    const refresh = async () => {
      try {
        const response = await fetch('/api/dashboard')
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`)
        }
        const payload = (await response.json()) as DashboardResponse
        if (!cancelled) {
          setDashboard(payload)
          setError(null)
        }
      } catch (refreshError) {
        if (!cancelled) {
          setError(refreshError instanceof Error ? refreshError.message : '读取失败')
        }
      }
    }

    refresh()
    const timer = dashboardRefreshMs > 0 ? window.setInterval(refresh, dashboardRefreshMs) : 0
    return () => {
      cancelled = true
      if (timer) window.clearInterval(timer)
    }
  }, [dashboardRefreshMs, refreshNonce])

  useEffect(() => {
    if (!selectedTerminalID) {
      setTerminalDetail(null)
      return
    }

    let cancelled = false
    const load = async () => {
      const response = await fetch(`/api/terminals/${encodeURIComponent(selectedTerminalID)}`)
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`)
      }
      const payload = (await response.json()) as TerminalDetail
      if (!cancelled) {
        setTerminalDetail(payload)
      }
    }

    const handleError = (detailError: unknown) => {
      if (!cancelled) {
        setError(detailError instanceof Error ? detailError.message : '终端详情读取失败')
      }
    }
    load().catch(handleError)
    const timer = window.setInterval(() => load().catch(handleError), 3000)

    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [selectedTerminalID])

  useEffect(() => {
    if (activeView !== 'load' && activeView !== 'overview') return
    let cancelled = false
    const load = async () => {
      const response = await fetch(`/api/load?window=${activeView === 'overview' ? '1h' : loadWindow}`)
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as { samples: LoadSample[] }
      if (!cancelled) setLoadSamples(payload.samples ?? [])
    }
    load().catch((loadError) => setError(loadError instanceof Error ? loadError.message : '负载历史读取失败'))
    const timer = window.setInterval(() => load().catch(() => undefined), 30000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [activeView, loadWindow])

  const filteredTerminals = useMemo(() => {
    if (!dashboard) {
      return []
    }
    const keyword = query.trim().toLowerCase()
    return dashboard.terminals.filter((terminal) => {
      if (terminalFamily === 'ipv4' && terminal.ipv4.length === 0) return false
      if (terminalFamily === 'ipv6' && terminal.ipv6.length === 0) return false
      if (!keyword) return true
      return (
      [
        terminal.displayName,
        terminal.remark,
        terminal.macAddress,
        terminal.primaryInterface,
        ...terminal.ipv4,
        ...terminal.ipv6,
      ]
        .join(' ')
        .toLowerCase()
        .includes(keyword)
      )
    })
  }, [dashboard, query, terminalFamily])

  const editingTerminal = useMemo(() => {
    if (!dashboard || !editingTerminalID) {
      return null
    }
    return dashboard.terminals.find((terminal) => terminal.id === editingTerminalID) ?? null
  }, [dashboard, editingTerminalID])

  if (!dashboard) {
    return (
      <main className="shell loading-shell">
        <div className="loading-card">
          <img className="brand-mark" src={rosboardMark} alt="" />
          <h1>Rosboard</h1>
          <p>正在读取 RouterOS 数据。</p>
          {error ? <p className="error-text">{error}</p> : null}
        </div>
      </main>
    )
  }

  const detailMode = activeView === 'terminals' && selectedTerminalID && terminalDetail

  return (
    <main className={sidebarOpen ? 'shell sidebar-open' : 'shell'}>
      <button
        type="button"
        className="sidebar-backdrop"
        aria-label="关闭导航"
        onClick={() => setSidebarOpen(false)}
      />
      <aside className="sidebar">
        <div className="brand">
          <img className="brand-mark" src={rosboardMark} alt="" />
          <div className="brand-copy">
            <h1>Rosboard</h1>
            <p>{dashboard.overview.version}</p>
          </div>
        </div>

        <nav className="menu">
          <button
            type="button"
            className={activeView === 'overview' ? 'menu-item active' : 'menu-item'}
            onClick={() => {
              setActiveView('overview')
              setSelectedTerminalID(null)
              setSidebarOpen(false)
            }}
          >
            <NavLabel icon="overview" label="系统概览" />
          </button>

          <div className="menu-group">
            <button
              type="button"
              className={
                activeView !== 'overview'
                  ? 'menu-item active'
                  : 'menu-item'
              }
              aria-expanded={statusExpanded}
              aria-controls="status-monitor-menu"
              onClick={() => setStatusExpanded((value) => !value)}
            >
              <NavLabel icon="status" label="状态监控" />
            </button>
            {statusExpanded ? <div className="submenu" id="status-monitor-menu">
              <button
                type="button"
                className={activeView === 'interfaces' ? 'submenu-item active' : 'submenu-item'}
                onClick={() => {
                  setActiveView('interfaces')
                  setSelectedTerminalID(null)
                  setSidebarOpen(false)
                }}
              >
                <NavLabel icon="network" label="线路监控" />
              </button>
              <button type="button" className="submenu-section submenu-toggle" aria-expanded={expandedMonitorGroup === 'terminals'} onClick={() => setExpandedMonitorGroup((value) => value === 'terminals' ? null : 'terminals')}><NavLabel icon="terminal" label="终端监控" /></button>
              {expandedMonitorGroup === 'terminals' ? (['all', 'ipv4', 'ipv6'] as TerminalFamily[]).map((family) => (
                <button
                  key={family}
                  type="button"
                  className={activeView === 'terminals' && terminalFamily === family ? 'submenu-item nested active' : 'submenu-item nested'}
                  onClick={() => {
                    setActiveView('terminals')
                    setTerminalFamily(family)
                    setSelectedTerminalID(null)
                    setSidebarOpen(false)
                  }}
                >
                  <NavLabel icon="terminal" label={family === 'all' ? '全部终端' : family.toUpperCase()} />
                </button>
              )) : null}
              <button type="button" className="submenu-section submenu-toggle" aria-expanded={expandedMonitorGroup === 'traffic'} onClick={() => setExpandedMonitorGroup((value) => value === 'traffic' ? null : 'traffic')}><NavLabel icon="traffic" label="流量监控" /></button>
              {expandedMonitorGroup === 'traffic' ? <>
                <button type="button" className={activeView === 'protocols' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('protocols'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="traffic" label="协议统计" /></button>
                <button type="button" className={activeView === 'policies' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('policies'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="policy" label="策略统计" /></button>
              </> : null}
              <button type="button" className="submenu-section submenu-toggle" aria-expanded={expandedMonitorGroup === 'runtime'} onClick={() => setExpandedMonitorGroup((value) => value === 'runtime' ? null : 'runtime')}><NavLabel icon="runtime" label="运行监控" /></button>
              {expandedMonitorGroup === 'runtime' ? <>
                <button type="button" className={activeView === 'load' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('load'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="runtime" label="负载历史" /></button>
                <button type="button" className={activeView === 'routes' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('routes'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="route" label="路由 / 分流" /></button>
              </> : null}
            </div> : null}
          </div>
        </nav>
        <div className="sidebar-device-card">
          <dl>
            <div><dt>设备名称</dt><dd>{dashboard.overview.routerName || '-'}</dd></div>
            <div><dt>RouterOS 版本</dt><dd>{dashboard.overview.version || '-'}</dd></div>
            <div><dt>运行时间</dt><dd>{dashboard.overview.uptime || '-'}</dd></div>
          </dl>
        </div>
      </aside>

      <section className="content">
        <header className={detailMode ? 'topbar detail-topbar' : activeView === 'overview' ? 'topbar overview-topbar' : 'topbar'}>
          <div className="topbar-title">
            <button
              type="button"
              className="mobile-menu-button"
              aria-label="打开导航"
              aria-expanded={sidebarOpen}
              onClick={() => setSidebarOpen(true)}
            >
              <span />
            </button>
            <div>
              <h2>{detailMode ? '终端详情' : viewTitle(activeView)}</h2>
              <p className="topbar-subtitle">
                {detailMode
                  ? `状态监控 > 终端监控 > ${detailScope === 'all' ? '全部终端' : detailScope.toUpperCase()}`
                  : `系统正常 · 更新于 ${formatDateTime(dashboard.overview.updatedAt)}`}
              </p>
            </div>
          </div>
          <div className="topbar-controls">
            <span className={dashboard.alerts?.length ? 'system-ok system-alerting' : 'system-ok'}><i />{dashboard.alerts?.length ? `${dashboard.alerts.length} 项告警` : '系统正常'}</span>
            <span className="last-updated">最后更新 {relativeUpdateTime(dashboard.overview.updatedAt)}</span>
            <button type="button" className="icon-button" aria-label="立即刷新" onClick={() => setRefreshNonce((value) => value + 1)}><Icon name="refresh" /></button>
            <select value={dashboardRefreshMs} onChange={(event) => setDashboardRefreshMs(Number(event.target.value))} aria-label="全局自动刷新">
              <option value={0}>停止刷新</option><option value={3000}>自动刷新（3 秒）</option><option value={5000}>自动刷新（5 秒）</option><option value={10000}>自动刷新（10 秒）</option>
            </select>
          </div>
        </header>

        {error ? <div className="global-error">最近一次刷新失败: {error}</div> : null}
        {dashboard.warnings?.length ? <div className="global-warning">{dashboard.warnings.join(' ')}</div> : null}

        {activeView === 'overview' ? (
          <OverviewPage dashboard={dashboard} loadSamples={loadSamples} />
        ) : null}

        {activeView === 'interfaces' ? (
          <InterfacesPage interfaces={dashboard.interfaces} />
        ) : null}

        {activeView === 'load' ? <LoadPage samples={loadSamples} window={loadWindow} onWindowChange={setLoadWindow} /> : null}
        {activeView === 'protocols' ? <ProtocolPage protocols={dashboard.protocols ?? []} /> : null}
        {activeView === 'policies' ? <PolicyPage policies={dashboard.policies ?? []} /> : null}
        {activeView === 'routes' ? <RoutesPage routes={dashboard.routes ?? []} /> : null}

        {activeView === 'terminals' && !detailMode ? (
          <TerminalsPage
            terminals={filteredTerminals}
            family={terminalFamily}
            query={query}
            onQueryChange={setQuery}
            refreshMs={dashboardRefreshMs}
            onRefreshMsChange={setDashboardRefreshMs}
            onRefresh={() => setRefreshNonce((value) => value + 1)}
            onOpenDetail={(terminalID) => {
              setSelectedTerminalID(terminalID)
              setTerminalTab('basic')
              setDetailScope(terminalFamily)
              setConnectionFamily(terminalFamily)
            }}
            onOpenRemark={(terminal) => {
              setEditingTerminalID(terminal.id)
              setCustomNameDraft(terminal.customName ?? '')
              setRemarkDraft(terminal.remark ?? '')
            }}
          />
        ) : null}

        {detailMode ? (
          <TerminalDetailPage
            detail={terminalDetail}
            activeTab={terminalTab}
            connectionFamily={connectionFamily}
            scope={detailScope}
            onBack={() => {
              setSelectedTerminalID(null)
              setTerminalDetail(null)
            }}
            onTabChange={setTerminalTab}
            onConnectionFamilyChange={setConnectionFamily}
          />
        ) : null}
      </section>

      {editingTerminal ? (
        <TerminalMetadataModal
          terminal={editingTerminal}
          customName={customNameDraft}
          remark={remarkDraft}
          saving={savingRemark}
          onCustomNameChange={setCustomNameDraft}
          onRemarkChange={setRemarkDraft}
          onClose={() => setEditingTerminalID(null)}
          onSave={async () => {
            setSavingRemark(true)
            try {
              const response = await fetch(
                `/api/terminals/${encodeURIComponent(editingTerminal.id)}/metadata`,
                {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({ customName: customNameDraft, remark: remarkDraft }),
                },
              )
              if (!response.ok) {
                const failure = await response.json().catch(() => null) as { error?: string } | null
                throw new Error(failure?.error || `HTTP ${response.status}`)
              }
              const payload = (await response.json()) as TerminalDetail
              setTerminalDetail(payload)
              setDashboard((previous) =>
                previous
                  ? {
                      ...previous,
                      terminals: previous.terminals.map((terminal) =>
                        terminal.id === payload.terminal.id ? payload.terminal : terminal,
                      ),
                    }
                  : previous,
              )
              setEditingTerminalID(null)
              setError(null)
            } catch (saveError) {
              setError(saveError instanceof Error ? saveError.message : '备注保存失败')
            } finally {
              setSavingRemark(false)
            }
          }}
        />
      ) : null}
    </main>
  )
}

function OverviewPage(props: { dashboard: DashboardResponse; loadSamples: LoadSample[] }) {
  const { overview } = props.dashboard
  const terminals = props.dashboard.terminals ?? []
  const interfaces = props.dashboard.interfaces ?? []
  const protocols = props.dashboard.protocols ?? []
  const alerts = props.dashboard.alerts ?? []
  const samples = props.loadSamples ?? []
  const cpuValues = samples.map((item) => item.cpuLoadPercent)
  const memoryValues = samples.map((item) => item.memoryUsedPercent)
  const terminalValues = samples.map((item) => item.onlineTerminalCount)
  const inactive = terminals.filter((item) => item.state === 'inactive').length
  const offline = terminals.filter((item) => item.state === 'offline').length
  const protocolCounts = protocols.reduce<Record<string, number>>((result, item) => {
    const key = item.kind.toUpperCase()
    result[key] = (result[key] ?? 0) + item.connections
    return result
  }, {})
  const interfaceRows = [...interfaces]
    .sort((left, right) => Number(right.running && !right.disabled) - Number(left.running && !left.disabled))
    .slice(0, 7)

  return (
    <div className="overview-dashboard">
      <section className="reference-metric-grid">
        <MetricCard title="CPU 使用率" value={`${overview.cpuLoadPercent}%`} detail="当前负载" icon="cpu" tone="blue" values={cpuValues} footerLeft={`平均 ${average(cpuValues).toFixed(0)}%`} footerRight={`峰值 ${maximum(cpuValues).toFixed(0)}%`} progress={overview.cpuLoadPercent} />
        <MetricCard title="内存使用率" value={`${overview.memoryUsedPercent.toFixed(1)}%`} detail={`${formatBytes(overview.memoryUsedBytes)} / ${formatBytes(overview.memoryTotalBytes)}`} icon="memory" tone="green" values={memoryValues} footerLeft={`已用 ${formatBytes(overview.memoryUsedBytes)}`} footerRight={`可用 ${formatBytes(Math.max(0, overview.memoryTotalBytes - overview.memoryUsedBytes))}`} progress={overview.memoryUsedPercent} />
        <MetricCard title="在线终端" value={`${overview.connectedDeviceCount}`} detail={`/ ${terminals.length}`} icon="terminal" tone="purple" values={terminalValues} footerLeft={`未活跃 ${inactive}`} footerRight={`离线 ${offline}`} />
        <MetricCard title="活动连接" value={overview.connectionCount.toLocaleString()} detail="IPv4 + IPv6 conntrack" icon="connections" tone="orange" footerLeft={`TCP ${protocolCounts.TCP ?? 0}`} footerRight={`UDP ${protocolCounts.UDP ?? 0}`} />
      </section>

      <section className="overview-main-grid">
        <section className="panel reference-panel traffic-panel">
          <div className="panel-head reference-panel-head">
            <div className="traffic-heading-block"><h3>实时流量</h3><div className="traffic-live-values" aria-live="polite"><span className="download-key">下载（{formatBitRate(overview.downloadBps)}）</span><span className="upload-key">上传（{formatBitRate(overview.uploadBps)}）</span></div></div>
            <span className="range-pills"><b>5 分钟</b></span>
          </div>
          {overview.chartSamples.length ? <Suspense fallback={<div className="realtime-traffic-chart chart-loading">正在加载图表...</div>}><RealtimeTrafficChart samples={overview.chartSamples} /></Suspense> : <div className="empty-chart">暂无速率采样</div>}
        </section>

        <section className="panel reference-panel status-panel">
          <div className="panel-head reference-panel-head"><h3>系统状态</h3></div>
          <SystemStatusList dashboard={props.dashboard} />
        </section>
      </section>

      <section className="overview-bottom-grid">
        <section className="panel reference-panel interface-summary-panel">
          <div className="panel-head reference-panel-head"><h3>接口状态</h3><span>{interfaces.length} 个接口</span></div>
          <div className="table-scroll">
            <table className="overview-interface-table">
              <thead><tr><th>接口名称</th><th>类型</th><th>状态</th><th>链路</th><th>接收速率</th><th>发送速率</th><th>接收流量</th><th>发送流量</th></tr></thead>
              <tbody>{interfaceRows.map((item) => <tr key={item.name}><td><strong>{item.name}</strong></td><td>{item.type || '-'}</td><td><StatusText ok={item.running && !item.disabled} trueText="已连接" falseText={item.disabled ? '已禁用' : '未连接'} /></td><td>{item.linkRate || (item.running ? '运行中' : '-')}</td><td>{formatBits(item.currentRxBps)}</td><td>{formatBits(item.currentTxBps)}</td><td>{formatBytes(item.rxBytes)}</td><td>{formatBytes(item.txBytes)}</td></tr>)}</tbody>
            </table>
          </div>
        </section>

        <section className="panel reference-panel events-panel">
          <div className="panel-head reference-panel-head"><h3>当前告警</h3><span>{alerts.length ? `${alerts.length} 项需要关注` : '全部正常'}</span></div>
          <div className="event-list">{alerts.length ? alerts.slice(0, 5).map((item) => <div className={`event-row event-${item.level === 'error' ? 'danger' : 'warning'}`} key={item.id}><span className="event-icon"><Icon name={item.level === 'error' ? 'alert' : 'info'} /></span><span><strong>{item.source}</strong> · {item.message}</span><small>{formatShortTime(item.timestamp)}</small></div>) : <div className="event-empty"><Icon name="check" /><span>当前没有采集告警</span></div>}</div>
          <div className="event-summary"><span className="danger-dot">严重 {alerts.filter((item) => item.level === 'error').length}</span><span className="warning-dot">警告 {alerts.filter((item) => item.level === 'warning').length}</span></div>
        </section>
      </section>
    </div>
  )
}

function MetricCard(props: { title: string; value: string; detail: string; icon: IconName; tone: string; values?: number[]; footerLeft: string; footerRight: string; progress?: number }) {
  return <article className={`metric-card metric-${props.tone}`}><p>{props.title}</p><div className="metric-card-main"><span className="metric-icon"><Icon name={props.icon} /></span><div className="metric-value"><strong>{props.value}</strong><small>{props.detail}</small></div>{props.values?.length ? <MiniSparkline values={props.values} /> : <div className="protocol-bars"><i /><i /><i /></div>}</div>{typeof props.progress === 'number' ? <div className="metric-progress"><i style={{ width: `${Math.min(100, Math.max(0, props.progress))}%` }} /></div> : null}<footer><span>{props.footerLeft}</span><span>{props.footerRight}</span></footer></article>
}

function MiniSparkline(props: { values: number[] }) {
  const width = 116; const height = 34; const max = Math.max(1, ...props.values); const min = Math.min(...props.values); const range = Math.max(1, max - min)
  const points = props.values.map((value, index) => `${index * width / Math.max(1, props.values.length - 1)},${height - 3 - (value - min) / range * (height - 6)}`).join(' ')
  return <svg className="mini-sparkline" viewBox={`0 0 ${width} ${height}`} aria-hidden="true"><polyline points={points} /></svg>
}

function SystemStatusList(props: { dashboard: DashboardResponse }) {
  const { overview } = props.dashboard
  const interfaces = props.dashboard.interfaces ?? []
  const activeInterfaces = interfaces.filter((item) => item.running && !item.disabled).length
  const updatedAt = new Date(overview.updatedAt)
  const freshnessSeconds = Math.max(0, (Date.now() - updatedAt.getTime()) / 1000)
  const fresh = Number.isFinite(freshnessSeconds) && freshnessSeconds <= 30
  const rows = [
    { icon: 'runtime' as IconName, label: '运行时间', value: overview.uptime || '-', ok: Boolean(overview.uptime) },
    { icon: 'router' as IconName, label: 'RouterOS 版本', value: overview.version || '-', ok: Boolean(overview.version) },
    { icon: 'refresh' as IconName, label: '最后成功采集', value: Number.isNaN(updatedAt.getTime()) ? '-' : formatDateTime(overview.updatedAt), ok: fresh },
    { icon: 'network' as IconName, label: '活动接口', value: `${activeInterfaces} / ${interfaces.length}`, ok: activeInterfaces > 0 },
    { icon: 'storage' as IconName, label: '存储使用率', value: overview.storageTotalBytes ? `${overview.storageUsedPercent.toFixed(1)}%` : '-', ok: !overview.storageTotalBytes || overview.storageUsedPercent < 85 },
    { icon: 'shield' as IconName, label: '数据新鲜度', value: Number.isFinite(freshnessSeconds) ? relativeUpdateTime(overview.updatedAt) : '-', ok: fresh },
  ]
  return <div className="system-status-list">{rows.map((row) => <div className="system-status-row" key={row.label}><span className="status-row-icon"><Icon name={row.icon} /></span><span>{row.label}</span><strong>{row.value}</strong><StatusText ok={row.ok} trueText="正常" falseText="注意" /></div>)}</div>
}

function StatusText(props: { ok: boolean; trueText: string; falseText: string }) { return <span className={props.ok ? 'status-text status-good' : 'status-text status-bad'}><i />{props.ok ? props.trueText : props.falseText}</span> }

function average(values: number[]) { return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : 0 }
function maximum(values: number[]) { return values.length ? Math.max(...values) : 0 }

function InterfacesPage(props: { interfaces: InterfaceStatus[] }) {
  const [selected, setSelected] = useState<string | null>(null)
  const [detail, setDetail] = useState<InterfaceDetail | null>(null)
  useEffect(() => {
    if (!selected) { setDetail(null); return }
    let cancelled = false
    const load = async () => {
      const response = await fetch(`/api/interfaces/${encodeURIComponent(selected)}`)
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as InterfaceDetail
      if (!cancelled) setDetail(payload)
    }
    load().catch(() => undefined)
    const timer = window.setInterval(() => load().catch(() => undefined), 5000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [selected])
  return (
    <div className="page-grid">
    {detail ? <section className="panel interface-detail"><div className="panel-head"><h3>{detail.interface.name} 接口详情</h3><button type="button" className="close-button" onClick={() => setSelected(null)}>关闭</button></div><div className="detail-summary"><DetailSummary label="状态" value={detail.interface.running && !detail.interface.disabled ? '在线' : '离线'} /><DetailSummary label="地址" value={detail.interface.addresses?.join(' / ') || '-'} /><DetailSummary label="MAC" value={detail.interface.macAddress || '-'} /><DetailSummary label="协商速率" value={detail.interface.linkRate ? `${detail.interface.linkRate}${detail.interface.fullDuplex ? ' / 全双工' : ''}` : '-'} /><DetailSummary label="当前上行" value={formatBits(detail.interface.currentTxBps)} /><DetailSummary label="当前下行" value={formatBits(detail.interface.currentRxBps)} /><DetailSummary label="收 / 发包" value={`${detail.interface.rxPackets} / ${detail.interface.txPackets}`} /><DetailSummary label="错误 / 丢包" value={`${detail.interface.rxErrors + detail.interface.txErrors} / ${detail.interface.rxDrops + detail.interface.txDrops}`} /></div>{detail.samples.length ? <Suspense fallback={<div className="realtime-traffic-chart chart-loading">正在加载图表...</div>}><RealtimeTrafficChart samples={detail.samples} ariaLabel={`${detail.interface.name} 接口上传和下载速率趋势`} /></Suspense> : <div className="empty-chart">暂无速率采样</div>}</section> : null}
    <section className="panel compact-panel">
      <div className="data-toolbar"><strong>接口运行状态</strong><span className="result-count">物理与逻辑接口只读监控</span><span className="toolbar-spacer" /><span>共 {props.interfaces.length} 个接口</span></div>
      <div className="table-scroll">
        <table className="data-table interface-table">
          <thead>
            <tr>
              <th>接口</th>
              <th>类型</th>
              <th>地址 / MAC</th>
              <th>状态</th>
              <th>上行速率</th>
              <th>下行速率</th>
              <th>累计上行</th>
              <th>累计下行</th>
              <th>MTU</th>
              <th>掉线次数</th>
              <th>错误 / 丢包</th>
            </tr>
          </thead>
          <tbody>
            {props.interfaces.map((item) => (
              <tr key={item.name}>
                <td><button type="button" className="link-button" onClick={() => setSelected(item.name)}>{item.name}</button></td>
                <td>{item.type}</td>
                <td><div className="address-stack"><span>{item.addresses?.join(' / ') || '-'}</span><span className="muted-text">{item.macAddress || '-'}</span></div></td>
                <td>{item.running && !item.disabled ? '在线' : '离线'}</td>
                <td>{formatBits(item.currentTxBps)}</td>
                <td>{formatBits(item.currentRxBps)}</td>
                <td>{formatBytes(item.txBytes)}</td>
                <td>{formatBytes(item.rxBytes)}</td>
                <td>{item.actualMtu || '-'}</td>
                <td>{item.linkDowns}</td>
                <td>{item.rxErrors + item.txErrors} / {item.rxDrops + item.txDrops}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
    </div>
  )
}

function LoadPage(props: { samples: LoadSample[]; window: string; onWindowChange: (value: string) => void }) {
  const latest = props.samples[props.samples.length - 1]
  return (
    <div className="page-grid">
      <div className="data-toolbar panel load-toolbar">
        <strong>历史范围</strong>
        {['1h', '1d', '1w', '1m'].map((value) => <button key={value} type="button" className={props.window === value ? 'toolbar-button active' : 'toolbar-button'} onClick={() => props.onWindowChange(value)}>{value === '1h' ? '1 小时' : value === '1d' ? '1 天' : value === '1w' ? '1 周' : '1 月'}</button>)}
        <span className="toolbar-spacer" /><span className="result-count">每分钟聚合，保留 35 天</span>
      </div>
      <section className="load-grid">
        <MetricHistory title="CPU 使用率" samples={props.samples} value={(sample) => sample.cpuLoadPercent} format={(value) => `${value.toFixed(1)}%`} />
        <MetricHistory title="内存使用率" samples={props.samples} value={(sample) => sample.memoryUsedPercent} format={(value) => `${value.toFixed(1)}%`} />
        <MetricHistory title="存储使用率" samples={props.samples} value={(sample) => sample.storageUsedPercent} format={(value) => `${value.toFixed(1)}%`} />
        <MetricHistory title="在线终端" samples={props.samples} value={(sample) => sample.onlineTerminalCount} format={(value) => `${Math.round(value)} 台`} />
        <MetricHistory title="总吞吐" samples={props.samples} value={(sample) => sample.uploadBps + sample.downloadBps} format={formatBits} />
      </section>
      {latest ? <div className="load-current">最新采样：CPU {latest.cpuLoadPercent.toFixed(1)}% · 内存 {latest.memoryUsedPercent.toFixed(1)}% · 存储 {latest.storageUsedPercent.toFixed(1)}% · 在线 {latest.onlineTerminalCount} 台 · 上行 {formatBits(latest.uploadBps)} · 下行 {formatBits(latest.downloadBps)}</div> : null}
    </div>
  )
}

function MetricHistory(props: { title: string; samples: LoadSample[]; value: (sample: LoadSample) => number; format: (value: number) => string }) {
  const values = props.samples.map(props.value)
  const maximum = Math.max(1, ...values)
  const width = 520
  const height = 170
  const points = values.map((value, index) => `${values.length <= 1 ? width / 2 : 18 + index * (width - 36) / (values.length - 1)},${height - 24 - value / maximum * (height - 46)}`).join(' ')
  return <section className="panel metric-history"><div className="panel-head"><h3>{props.title}</h3><span>当前 {values.length ? props.format(values[values.length - 1]) : '-'} · 最大 {props.format(Math.max(0, ...values))}</span></div>{values.length ? <><svg viewBox={`0 0 ${width} ${height}`}><line x1="18" x2={width - 18} y1={height - 24} y2={height - 24} className="grid-line" /><polyline points={points} className="metric-line" /></svg><div className="chart-time"><span>{formatShortTime(props.samples[0].timestamp)}</span><span>{formatShortTime(props.samples[props.samples.length - 1].timestamp)}</span></div></> : <div className="empty-chart">等待历史采样</div>}</section>
}

function ProtocolPage(props: { protocols: ProtocolStat[] }) {
  const [history, setHistory] = useState<ProtocolHistorySample[]>([])
  useEffect(() => {
    let cancelled = false
    const load = async () => {
      const response = await fetch('/api/protocols?window=30m')
      if (!response.ok) return
      const payload = (await response.json()) as { history: ProtocolHistorySample[] }
      if (!cancelled) setHistory(payload.history)
    }
    load().catch(() => undefined)
    const timer = window.setInterval(() => load().catch(() => undefined), 30000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [])
  const totalBytes = props.protocols.reduce((sum, item) => sum + item.uploadBytes + item.downloadBytes, 0)
  const historyBytes = new Map<string, number>()
  history.forEach((sample) => historyBytes.set(sample.name, (historyBytes.get(sample.name) ?? 0) + (sample.uploadBps + sample.downloadBps) * 60 / 8))
  const historyTotal = Array.from(historyBytes.values()).reduce((sum, value) => sum + value, 0)
  return <section className="panel compact-panel"><div className="data-toolbar"><strong>当前连接协议分布</strong><span className="result-count">基于 RouterOS 连接跟踪与端口估算，不是 DPI</span><span className="toolbar-spacer" /><span>近 30 分钟 {history.length} 条采样</span></div><div className="table-scroll"><table className="data-table"><thead><tr><th>应用分类</th><th>传输协议</th><th>连接数</th><th>当前上行</th><th>当前下行</th><th>活动连接累计</th><th>当前占比</th><th>近30分钟占比</th><th>识别方式</th></tr></thead><tbody>{props.protocols.length ? props.protocols.map((item) => { const bytes = item.uploadBytes + item.downloadBytes; const recent = historyBytes.get(item.name) ?? 0; return <tr key={`${item.name}-${item.kind}`}><td>{item.name}</td><td>{item.kind}</td><td>{item.connections}</td><td>{formatBits(item.uploadBps)}</td><td>{formatBits(item.downloadBps)}</td><td>{formatBytes(bytes)}</td><td>{totalBytes ? `${(bytes / totalBytes * 100).toFixed(1)}%` : '-'}</td><td>{historyTotal ? `${(recent / historyTotal * 100).toFixed(1)}%` : '-'}</td><td>{item.estimated ? '端口估算' : 'RouterOS 原生'}</td></tr> }) : <tr><td colSpan={9} className="empty-row">当前没有可统计的活动连接</td></tr>}</tbody></table></div></section>
}

function PolicyPage(props: { policies: PolicyStat[] }) {
  return <section className="panel compact-panel"><div className="data-toolbar"><strong>现有 RouterOS 策略计数器</strong><span className="result-count">只读展示，不创建或修改规则</span><span className="toolbar-spacer" /><span>共 {props.policies.length} 条</span></div><div className="table-scroll"><table className="data-table"><thead><tr><th>来源</th><th>名称</th><th>目标 / 动作</th><th>标记</th><th>当前速率</th><th>累计流量</th><th>包数</th><th>状态</th></tr></thead><tbody>{props.policies.length ? props.policies.map((item, index) => <tr key={`${item.kind}-${item.name}-${index}`}><td>{item.kind}</td><td>{item.name}</td><td>{item.target || '-'}</td><td>{item.mark || '-'}</td><td>{item.rate || '-'}</td><td>{formatBytes(item.bytes)}</td><td>{item.packets}</td><td>{item.disabled ? '已禁用' : '生效中'}</td></tr>) : <tr><td colSpan={8} className="empty-row">当前 RouterOS 没有可展示的队列、队列树或带计数/标记的 mangle 策略</td></tr>}</tbody></table></div></section>
}

function RoutesPage(props: { routes: RouteStat[] }) {
  return <section className="panel compact-panel"><div className="data-toolbar"><strong>现有路由与分流状态</strong><span className="result-count">来自当前路由表和 routing rule</span><span className="toolbar-spacer" /><span>共 {props.routes.length} 条</span></div><div className="table-scroll"><table className="data-table"><thead><tr><th>类型</th><th>源地址 / 接口</th><th>目标网段</th><th>网关</th><th>路由表</th><th>动作</th><th>距离</th><th>状态</th></tr></thead><tbody>{props.routes.length ? props.routes.map((item, index) => <tr key={`${item.kind}-${item.destination}-${item.table}-${index}`}><td>{item.kind}</td><td>{item.source || '-'}</td><td>{item.destination || '-'}</td><td>{item.gateway || '-'}</td><td>{item.table || 'main'}</td><td>{item.action || '-'}</td><td>{item.distance || '-'}</td><td>{item.disabled ? '已禁用' : item.kind === 'route' ? (item.active ? '活动' : '非活动') : '生效中'}</td></tr>) : <tr><td colSpan={8} className="empty-row">当前没有可读取的路由或分流状态</td></tr>}</tbody></table></div></section>
}

function TerminalsPage(props: {
  terminals: Terminal[]
  family: TerminalFamily
  query: string
  onQueryChange: (value: string) => void
  refreshMs: number
  onRefreshMsChange: (value: number) => void
  onRefresh: () => void
  onOpenDetail: (terminalID: string) => void
  onOpenRemark: (terminal: Terminal) => void
}) {
  const [sortKey, setSortKey] = useState<TerminalSortKey>('device')
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('asc')
  const [stateFilter, setStateFilter] = useState('online')
  const [interfaceFilter, setInterfaceFilter] = useState('all')
  const [pageSize, setPageSize] = useState(20)
  const [page, setPage] = useState(1)
  const interfaces = useMemo(() => Array.from(new Set(props.terminals.map((item) => item.primaryInterface).filter(Boolean))).sort(), [props.terminals])
  const sorted = useMemo(() => {
    const visible = props.terminals.filter((terminal) =>
      (stateFilter === 'all' || terminal.state === stateFilter) &&
      (interfaceFilter === 'all' || terminal.primaryInterface === interfaceFilter),
    )
    return [...visible].sort((left, right) => {
      const comparison = compareTerminal(left, right, sortKey, props.family)
      return sortDirection === 'asc' ? comparison : -comparison
    })
  }, [props.terminals, props.family, stateFilter, interfaceFilter, sortKey, sortDirection])
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const currentPage = Math.min(page, totalPages)
  const rows = sorted.slice((currentPage - 1) * pageSize, currentPage * pageSize)
  const showingInactive = stateFilter !== 'online'

  useEffect(() => setPage(1), [props.query, stateFilter, interfaceFilter, pageSize])

  const changeSort = (key: TerminalSortKey) => {
    if (sortKey === key) setSortDirection((value) => value === 'asc' ? 'desc' : 'asc')
    else {
      setSortKey(key)
      setSortDirection('asc')
    }
  }

  return (
    <section className="panel compact-panel">
      <div className="data-toolbar">
        <input className="search-input" value={props.query} onChange={(event) => props.onQueryChange(event.target.value)} placeholder="备注 / 名称 / IP / MAC" />
        <select value={stateFilter} onChange={(event) => setStateFilter(event.target.value)} aria-label="终端状态">
          <option value="all">全部状态</option><option value="online">在线</option><option value="inactive">近期未活跃</option><option value="offline">离线</option>
        </select>
        <select value={interfaceFilter} onChange={(event) => setInterfaceFilter(event.target.value)} aria-label="接入接口">
          <option value="all">全部接口</option>{interfaces.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
        <button
          type="button"
          className={showingInactive ? 'toolbar-button active' : 'toolbar-button'}
          aria-pressed={showingInactive}
          onClick={() => setStateFilter(showingInactive ? 'online' : 'all')}
        >
          {showingInactive ? '隐藏非在线设备' : '显示非在线设备'}
        </button>
        <span className="toolbar-spacer" />
        <span className="result-count">共 {sorted.length} 台</span>
        <select value={props.refreshMs} onChange={(event) => props.onRefreshMsChange(Number(event.target.value))} aria-label="自动刷新">
          <option value={0}>停止刷新</option><option value={3000}>3 秒刷新</option><option value={5000}>5 秒刷新</option><option value={10000}>10 秒刷新</option>
        </select>
        <button type="button" className="toolbar-button" onClick={props.onRefresh}>刷新</button>
      </div>

      <div className="table-scroll">
        <table className="data-table terminal-table">
          <thead><tr>
            <SortHeader label="设备名称" sortKey="device" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="IP / MAC" sortKey="address" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="连接数" sortKey="connections" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="上行速率" sortKey="upload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="下行速率" sortKey="download" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label={props.family === 'all' ? '累计上行' : '活动累计上行'} sortKey="totalUpload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label={props.family === 'all' ? '累计下行' : '活动累计下行'} sortKey="totalDownload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="在线时长" sortKey="online" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="备注" sortKey="remark" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <th>操作</th>
          </tr></thead>
          <tbody>
            {rows.map((terminal) => {
              const mainAddress = terminalPrimaryAddress(terminal, props.family)
              const metrics = terminalMetrics(terminal, props.family)
              const addressCount = props.family === 'ipv4' ? terminal.ipv4.length : props.family === 'ipv6' ? terminal.ipv6.length : terminal.ipv4.length + terminal.ipv6.length
              return <tr key={terminal.id}>
                <td><button type="button" className="link-button terminal-name-link" onClick={() => props.onOpenDetail(terminal.id)}><strong>{terminal.displayName}</strong></button></td>
                <td><button type="button" className="link-button terminal-link" onClick={() => props.onOpenDetail(terminal.id)}>
                  <strong>{mainAddress || '-'}</strong>
                  <span className="muted-text">{terminal.macAddress || 'MAC 未知'}{addressCount > 1 ? `  +${addressCount - 1}` : ''}</span>
                </button></td>
                <td>{metrics.connectionCount}</td><td>{formatBits(metrics.currentUploadBps)}</td><td>{formatBits(metrics.currentDownloadBps)}</td>
                <td>{formatBytes(metrics.totalUploadBytes)}</td><td>{formatBytes(metrics.totalDownloadBytes)}</td>
                <td><span className={`state-dot state-${terminal.state}`} />{terminal.state === 'online' ? formatOnlineDuration(terminal.onlineSince) : terminalStateText(terminal.state)}</td>
                <td>{terminal.remark || '-'}</td>
                <td><div className="action-links"><button type="button" className="link-button" onClick={() => props.onOpenDetail(terminal.id)}>详情</button><button type="button" className="link-button" onClick={() => props.onOpenRemark(terminal)}>编辑终端</button></div></td>
              </tr>
            })}
          </tbody>
        </table>
      </div>
      <div className="pagination">
        <span>每页</span><select value={pageSize} onChange={(event) => setPageSize(Number(event.target.value))}><option value={10}>10</option><option value={20}>20</option><option value={50}>50</option></select>
        <button type="button" disabled={currentPage <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}>上一页</button>
        <span>{currentPage} / {totalPages}</span>
        <button type="button" disabled={currentPage >= totalPages} onClick={() => setPage((value) => Math.min(totalPages, value + 1))}>下一页</button>
      </div>
    </section>
  )
}

function SortHeader(props: { label: string; sortKey: TerminalSortKey; activeKey: TerminalSortKey; direction: 'asc' | 'desc'; onSort: (key: TerminalSortKey) => void }) {
  return <th><button type="button" className="sort-button" onClick={() => props.onSort(props.sortKey)}>{props.label}<span>{props.activeKey === props.sortKey ? (props.direction === 'asc' ? '↑' : '↓') : '↕'}</span></button></th>
}

function TerminalDetailPage(props: {
  detail: TerminalDetail
  activeTab: TerminalTab
  connectionFamily: ConnectionFamily
  scope: TerminalFamily
  onBack: () => void
  onTabChange: (value: TerminalTab) => void
  onConnectionFamilyChange: (value: ConnectionFamily) => void
}) {
  const ipv4Connections = props.detail.connections.filter((item) => item.family === 'ipv4')
  const ipv6Connections = props.detail.connections.filter((item) => item.family === 'ipv6')
  const isRouterConntrack = props.detail.terminal.id === 'routeros:self'
  const scopedConnections = props.scope === 'all' ? props.detail.connections : props.detail.connections.filter((item) => item.family === props.scope)
  const repliedConnections = scopedConnections.filter((item) => item.seenReply).length
  const unrepliedConnections = scopedConnections.length - repliedConnections
  const summary = props.scope === 'all' ? props.detail.terminal : (props.detail.familySummaries?.[props.scope] ?? props.detail.terminal)
  const visibleFlows = props.scope === 'all' ? props.detail.flowCategories : (props.detail.familyFlows?.[props.scope] ?? [])
  const [connectionQuery, setConnectionQuery] = useState('')
  const [protocolFilter, setProtocolFilter] = useState('all')
  const familyConnections = props.connectionFamily === 'all' ? props.detail.connections : props.connectionFamily === 'ipv4' ? ipv4Connections : ipv6Connections
  const protocols = Array.from(new Set(familyConnections.map((item) => item.protocol))).sort()
  const visibleConnections = familyConnections.filter((connection) =>
    (protocolFilter === 'all' || connection.protocol === protocolFilter) &&
    [connection.application, connection.sourceAddress, connection.destinationAddress, connection.publicAddress, connection.connectionMark]
      .join(' ').toLowerCase().includes(connectionQuery.trim().toLowerCase()),
  )

  return (
    <section className="detail-page">
      <div className="detail-page-head">
        <div className="detail-identity">
          <h3>{summary.displayName}</h3>
          <div className="detail-identity-meta">
            <span>IP {terminalPrimaryAddress(summary, props.scope) || '-'}</span>
            <span>MAC {summary.macAddress || '-'}</span>
            <span className={`identity-state ${summary.state}`}>{terminalStateText(summary.state)}</span>
            <span>{isRouterConntrack ? '跟踪条目' : '连接'} {summary.connectionCount}</span>
            {isRouterConntrack ? <span>已回包 {repliedConnections} / 未回包 {unrepliedConnections}</span> : null}
            <span>↑ {formatBits(summary.currentUploadBps)}</span>
            <span>↓ {formatBits(summary.currentDownloadBps)}</span>
          </div>
        </div>
        <div className="detail-head-actions">
          <button type="button" className="close-button" onClick={props.onBack}>
            返回
          </button>
        </div>
      </div>

      <div className="tab-row detail-tabs">
        <TabButton label="基础信息" active={props.activeTab === 'basic'} onClick={() => props.onTabChange('basic')} />
        <TabButton label={isRouterConntrack ? '跟踪详情' : '连接详情'} active={props.activeTab === 'connections'} onClick={() => props.onTabChange('connections')} />
        <TabButton label="流量分布" active={props.activeTab === 'flows'} onClick={() => props.onTabChange('flows')} />
        {props.scope === 'all' ? <TabButton label="历史记录" active={props.activeTab === 'history'} onClick={() => props.onTabChange('history')} /> : null}
      </div>

      <section className="panel detail-panel">
        {props.activeTab === 'basic' ? (
          <div className="detail-grid">
            <DetailItem label="设备名称" value={summary.displayName} />
            <DetailItem label="自动识别名称" value={summary.autoName || '暂未识别'} />
            {props.scope !== 'ipv6' ? <DetailItem label="IPv4 地址" value={summary.ipv4.join(' / ') || '-'} /> : null}
            {props.scope !== 'ipv4' ? <DetailItem label="IPv6 地址" value={summary.ipv6.join(' / ') || '-'} /> : null}
            <DetailItem label="MAC 地址" value={summary.macAddress || '-'} />
            <DetailItem label="接入接口" value={summary.primaryInterface || '-'} />
            <DetailItem label={isRouterConntrack ? (props.scope === 'all' ? 'conntrack 条目（IPv4+IPv6）' : `${props.scope.toUpperCase()} conntrack 条目`) : (props.scope === 'all' ? '连接数（IPv4+IPv6）' : `${props.scope.toUpperCase()} 连接数`)} value={`${summary.connectionCount}`} />
            {isRouterConntrack ? <DetailItem label="已见回包（S）" value={`${repliedConnections}`} /> : null}
            {isRouterConntrack ? <DetailItem label="未见回包" value={`${unrepliedConnections}`} /> : null}
            <DetailItem label="本次在线时长" value={summary.state === 'online' ? formatOnlineDuration(summary.onlineSince) : '-'} />
            <DetailItem label="当前上行速率" value={formatBits(summary.currentUploadBps)} />
            <DetailItem label="当前下行速率" value={formatBits(summary.currentDownloadBps)} />
            <DetailItem label={props.scope === 'all' ? '累计上行' : '活动连接累计上行'} value={formatBytes(summary.totalUploadBytes)} />
            <DetailItem label={props.scope === 'all' ? '累计下行' : '活动连接累计下行'} value={formatBytes(summary.totalDownloadBytes)} />
            <DetailItem label="备注" value={summary.remark || '-'} />
            <DetailItem label="面板开始统计" value={formatDateTime(summary.trackingSince)} />
            <DetailItem label="最后活动时间" value={formatDateTime(summary.lastSeen)} />
          </div>
        ) : null}

        {props.activeTab === 'connections' ? (
          <div>
            <div className="connection-toolbar">
              <div className="family-switch">
              {props.scope === 'all' ? <TabButton
                label={`${isRouterConntrack ? '全部跟踪条目' : '全部连接'} (${props.detail.connections.length})`}
                active={props.connectionFamily === 'all'}
                onClick={() => props.onConnectionFamilyChange('all')}
              /> : null}
              {props.scope !== 'ipv6' ? (
              <TabButton
                label={`IPv4 ${isRouterConntrack ? '跟踪条目' : '连接详情'} (${ipv4Connections.length})`}
                active={props.connectionFamily === 'ipv4'}
                onClick={() => props.onConnectionFamilyChange('ipv4')}
              />
              ) : null}
              {props.scope !== 'ipv4' ? (
              <TabButton
                label={`IPv6 ${isRouterConntrack ? '跟踪条目' : '连接详情'} (${ipv6Connections.length})`}
                active={props.connectionFamily === 'ipv6'}
                onClick={() => props.onConnectionFamilyChange('ipv6')}
              />
              ) : null}
              </div>
              <select value={protocolFilter} onChange={(event) => setProtocolFilter(event.target.value)} aria-label="连接协议">
                <option value="all">全部协议</option>{protocols.map((protocol) => <option key={protocol} value={protocol}>{protocol}</option>)}
              </select>
              <input value={connectionQuery} onChange={(event) => setConnectionQuery(event.target.value)} placeholder="目标地址 / 应用 / 标记" />
            </div>
            <div className="table-scroll">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>应用</th>
                    <th>协议</th>
                    <th>出口线路</th>
                    <th>本地地址 / 端口</th>
                    <th>目的地址</th>
                    <th>目的端口</th>
                    <th>外网地址</th>
                    <th>当前上行</th>
                    <th>当前下行</th>
                    <th>累计上行</th>
                    <th>累计下行</th>
                    <th>标志</th>
                    <th>连接状态</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleConnections.length === 0 ? (
                    <tr>
                      <td colSpan={14} className="empty-row">
                        当前没有 {props.connectionFamily === 'all' ? '活动' : props.connectionFamily.toUpperCase()} {isRouterConntrack ? '跟踪条目' : '连接详情'}
                      </td>
                    </tr>
                  ) : (
                    visibleConnections.map((connection) => (
                      <tr key={connection.key}>
                        <td>{connection.application}</td>
                        <td>{connection.protocol}</td>
                        <td>{connection.line}</td>
                        <td>{formatEndpoint(connection.sourceAddress, connection.sourcePort)}</td>
                        <td>{connection.destinationAddress}</td>
                        <td>{connection.destinationPort || '-'}</td>
                        <td>{connection.publicAddress || '-'}</td>
                        <td>{formatBits(connection.uploadBps)}</td>
                        <td>{formatBits(connection.downloadBps)}</td>
                        <td>{formatBytes(connection.uploadBytes)}</td>
                        <td>{formatBytes(connection.downloadBytes)}</td>
                        <td><span className="connection-flags" title="S=已见回包，A=Assured">{connection.seenReply ? 'S' : '-'} {connection.assured ? 'A' : '-'}</span></td>
                        <td>{connection.status}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}

        {props.activeTab === 'flows' ? (
          <div>
            <p className="table-note">按当前活动连接的协议和端口估算，不等同于 DPI 应用识别。</p>
            <div className="table-scroll">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>应用</th>
                    <th>上行速率</th>
                    <th>累计上行及占比</th>
                    <th>下行速率</th>
                    <th>累计下行及占比</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleFlows.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="empty-row">
                        当前暂无足够连接用于估算流量分布
                      </td>
                    </tr>
                  ) : (
                    visibleFlows.map((flow) => (
                      <tr key={flow.name}>
                        <td>{flow.name}</td>
                        <td>{formatBits(flow.currentUploadBps)}</td>
                        <td>{formatBytes(flow.totalUploadBytes)} / {flow.uploadPercent.toFixed(2)}%</td>
                        <td>{formatBits(flow.currentDownloadBps)}</td>
                        <td>{formatBytes(flow.totalDownloadBytes)} / {flow.downloadPercent.toFixed(2)}%</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}

        {props.activeTab === 'history' ? (
          <div>
            <p className="table-note">每分钟保存一条面板本地累计快照。</p>
            <div className="table-scroll">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>日期/时间</th>
                    <th>在线时长</th>
                    <th>累计上行</th>
                    <th>累计下行</th>
                  </tr>
                </thead>
                <tbody>
                  {props.detail.history.length === 0 ? (
                    <tr>
                      <td colSpan={4} className="empty-row">
                        暂无历史记录
                      </td>
                    </tr>
                  ) : (
                    props.detail.history.map((entry) => (
                      <tr key={entry.timestamp}>
                        <td>{formatDateTime(entry.timestamp)}</td>
                        <td>{formatSeconds(entry.onlineSeconds)}</td>
                        <td>{formatBytes(entry.totalUploadBytes)}</td>
                        <td>{formatBytes(entry.totalDownloadBytes)}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}
      </section>
    </section>
  )
}

function TerminalMetadataModal(props: {
  terminal: Terminal
  customName: string
  remark: string
  saving: boolean
  onCustomNameChange: (value: string) => void
  onRemarkChange: (value: string) => void
  onClose: () => void
  onSave: () => void
}) {
  return (
    <div className="dialog-backdrop" role="dialog" aria-modal="true">
      <div className="remark-modal">
        <div className="dialog-head">
          <div>
            <h3>编辑终端</h3>
            <p className="muted-text">设备名称和备注只保存到面板本地，不写回 RouterOS。</p>
          </div>
          <button type="button" className="close-button" onClick={props.onClose}>
            关闭
          </button>
        </div>
        <div className="remark-modal-body">
          <label className="metadata-field">
            <span>设备名称</span>
            <input value={props.customName} onChange={(event) => props.onCustomNameChange(event.target.value)} maxLength={100} placeholder={props.terminal.displayName} />
            <small>自动识别：{props.terminal.autoName || '暂未识别'}；清空后恢复自动名称。</small>
          </label>
          <label className="metadata-field">
            <span>备注</span>
          <textarea
            value={props.remark}
            onChange={(event) => props.onRemarkChange(event.target.value)}
            rows={5}
            maxLength={500}
            className="remark-textarea"
          />
          </label>
          <div className="remark-modal-actions">
            <button type="button" className="close-button" onClick={props.onClose}>
              取消
            </button>
            <button type="button" className="primary-button" onClick={props.onSave} disabled={props.saving}>
              {props.saving ? '保存中...' : '保存'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function TabButton(props: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      className={props.active ? 'tab-button active' : 'tab-button'}
      onClick={props.onClick}
    >
      {props.label}
    </button>
  )
}

function DetailItem(props: { label: string; value: string }) {
  return (
    <div className="detail-item">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </div>
  )
}

function DetailSummary(props: { label: string; value: string }) {
  return (
    <div className="detail-summary-item">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </div>
  )
}

export default App
