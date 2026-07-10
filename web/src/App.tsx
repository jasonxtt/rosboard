import { useEffect, useMemo, useState } from 'react'

type RateSample = {
  timestamp: string
  uploadBps: number
  downloadBps: number
}

type LoadSample = {
  timestamp: string
  cpuLoadPercent: number
  memoryUsedPercent: number
  storageUsedPercent: number
  onlineTerminalCount: number
  uploadBps: number
  downloadBps: number
}

type Overview = {
  routerName: string
  platform: string
  version: string
  boardName: string
  uptime: string
  cpuLoadPercent: number
  memoryUsedPercent: number
  memoryUsedBytes: number
  memoryTotalBytes: number
  storageUsedPercent: number
  storageUsedBytes: number
  storageTotalBytes: number
  uploadBps: number
  downloadBps: number
  trafficInterfaces: string[]
  healthEnabled: boolean
  updatedAt: string
  chartSamples: RateSample[]
}

type InterfaceStatus = {
  name: string
  type: string
  running: boolean
  disabled: boolean
  macAddress: string
  status: string
  lastLinkUpTime: string
  linkDowns: number
  actualMtu: number
  rxBytes: number
  txBytes: number
  currentRxBps: number
  currentTxBps: number
  addresses: string[]
  rxPackets: number
  txPackets: number
  rxDrops: number
  txDrops: number
  rxErrors: number
  txErrors: number
  linkRate: string
  fullDuplex: boolean
}
type InterfaceDetail = { interface: InterfaceStatus; samples: RateSample[] }

type Terminal = {
  id: string
  displayName: string
  remark: string
  macAddress: string
  primaryInterface: string
  ipv4: string[]
  ipv6: string[]
  connectionCount: number
  currentUploadBps: number
  currentDownloadBps: number
  totalUploadBytes: number
  totalDownloadBytes: number
  trackingSince: string
  lastSeen: string
  primaryIpv4: string
  primaryIpv6: string
  state: 'online' | 'idle' | 'offline'
  onlineSince: string
}

type CapabilityNote = {
  area: string
  item: string
  status: string
  details: string
}

type ProtocolStat = { name: string; kind: string; connections: number; uploadBps: number; downloadBps: number; uploadBytes: number; downloadBytes: number; estimated: boolean }
type ProtocolHistorySample = { timestamp: string; name: string; kind: string; connections: number; uploadBps: number; downloadBps: number }
type PolicyStat = { kind: string; name: string; target: string; mark: string; rate: string; bytes: number; packets: number; disabled: boolean }
type RouteStat = { kind: string; destination: string; gateway: string; table: string; action: string; source: string; distance: number; active: boolean; disabled: boolean }

type DashboardResponse = {
  overview: Overview
  interfaces: InterfaceStatus[]
  terminals: Terminal[]
  capabilities: CapabilityNote[]
  protocols: ProtocolStat[]
  policies: PolicyStat[]
  routes: RouteStat[]
  warnings: string[]
}

type TerminalConnection = {
  key: string
  family: string
  application: string
  protocol: string
  line: string
  sourceAddress: string
  sourcePort: string
  destinationAddress: string
  destinationPort: string
  uploadBytes: number
  downloadBytes: number
  uploadBps: number
  downloadBps: number
  status: string
  publicAddress: string
  connectionMark: string
  estimated: boolean
}

type TerminalFlowCategory = {
  name: string
  currentUploadBps: number
  currentDownloadBps: number
  totalUploadBytes: number
  totalDownloadBytes: number
  uploadPercent: number
  downloadPercent: number
  estimated: boolean
}

type TerminalHistoryEntry = {
  timestamp: string
  onlineSeconds: number
  totalUploadBytes: number
  totalDownloadBytes: number
}

type TerminalCapability = {
  tab: string
  status: string
  details: string
}

type TerminalDetail = {
  terminal: Terminal
  connections: TerminalConnection[]
  flowCategories: TerminalFlowCategory[]
  history: TerminalHistoryEntry[]
  capabilities: TerminalCapability[]
  familySummaries: Record<'ipv4' | 'ipv6', Terminal>
  familyFlows: Record<'ipv4' | 'ipv6', TerminalFlowCategory[]>
}

type ActiveView = 'overview' | 'interfaces' | 'terminals' | 'load' | 'protocols' | 'policies' | 'routes'
type TerminalTab = 'basic' | 'connections' | 'flows' | 'history'
type ConnectionFamily = 'all' | 'ipv4' | 'ipv6'
type TerminalFamily = 'all' | 'ipv4' | 'ipv6'

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
  const [remarkModalOpen, setRemarkModalOpen] = useState(false)
  const [remarkDraft, setRemarkDraft] = useState('')
  const [savingRemark, setSavingRemark] = useState(false)
  const [terminalFamily, setTerminalFamily] = useState<TerminalFamily>('all')
  const [dashboardRefreshMs, setDashboardRefreshMs] = useState(5000)
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [loadWindow, setLoadWindow] = useState('1h')
  const [loadSamples, setLoadSamples] = useState<LoadSample[]>([])

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
        setRemarkDraft(payload.terminal.remark ?? '')
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
    if (activeView !== 'load') return
    let cancelled = false
    const load = async () => {
      const response = await fetch(`/api/load?window=${loadWindow}`)
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as { samples: LoadSample[] }
      if (!cancelled) setLoadSamples(payload.samples)
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

  const currentTerminal = useMemo(() => {
    if (!dashboard || !selectedTerminalID) {
      return null
    }
    return dashboard.terminals.find((terminal) => terminal.id === selectedTerminalID) ?? null
  }, [dashboard, selectedTerminalID])

  if (!dashboard) {
    return (
      <main className="shell loading-shell">
        <div className="loading-card">
          <h1>rosboard</h1>
          <p>正在读取 RouterOS 数据。</p>
          {error ? <p className="error-text">{error}</p> : null}
        </div>
      </main>
    )
  }

  const detailMode = activeView === 'terminals' && selectedTerminalID && terminalDetail

  return (
    <main className="shell">
      <aside className="sidebar">
        <div className="brand">
          <h1>rosboard</h1>
          <p>{dashboard.overview.version}</p>
        </div>

        <nav className="menu">
          <button
            type="button"
            className={activeView === 'overview' ? 'menu-item active' : 'menu-item'}
            onClick={() => {
              setActiveView('overview')
              setSelectedTerminalID(null)
            }}
          >
            系统概况
          </button>

          <div className="menu-group">
            <button
              type="button"
              className={
                activeView !== 'overview'
                  ? 'menu-item active'
                  : 'menu-item'
              }
              onClick={() => {
                setActiveView('interfaces')
                setSelectedTerminalID(null)
              }}
            >
              状态监控
            </button>
            <div className="submenu">
              <button
                type="button"
                className={activeView === 'interfaces' ? 'submenu-item active' : 'submenu-item'}
                onClick={() => {
                  setActiveView('interfaces')
                  setSelectedTerminalID(null)
                }}
              >
                线路监控
              </button>
              <div className="submenu-section">终端监控</div>
              {(['all', 'ipv4', 'ipv6'] as TerminalFamily[]).map((family) => (
                <button
                  key={family}
                  type="button"
                  className={activeView === 'terminals' && terminalFamily === family ? 'submenu-item nested active' : 'submenu-item nested'}
                  onClick={() => {
                    setActiveView('terminals')
                    setTerminalFamily(family)
                    setSelectedTerminalID(null)
                  }}
                >
                  {family === 'all' ? '全部终端' : family.toUpperCase()}
                </button>
              ))}
              <div className="submenu-section">流量监控</div>
              <button type="button" className={activeView === 'protocols' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('protocols'); setSelectedTerminalID(null) }}>协议统计</button>
              <button type="button" className={activeView === 'policies' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('policies'); setSelectedTerminalID(null) }}>策略统计</button>
              <div className="submenu-section">运行监控</div>
              <button type="button" className={activeView === 'load' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('load'); setSelectedTerminalID(null) }}>负载历史</button>
              <button type="button" className={activeView === 'routes' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('routes'); setSelectedTerminalID(null) }}>路由 / 分流</button>
            </div>
          </div>
        </nav>
      </aside>

      <section className="content">
        <header className="topbar">
          <div>
            <h2>{detailMode ? '终端详情' : viewTitle(activeView)}</h2>
            <p className="topbar-subtitle">
              {detailMode
                ? `状态监控 > 终端监控 > ${detailScope === 'all' ? '全部终端' : detailScope.toUpperCase()}`
                : `更新时间 ${formatDateTime(dashboard.overview.updatedAt)}`}
            </p>
          </div>
          <div className="topbar-metrics">
            <span>CPU: {dashboard.overview.cpuLoadPercent}%</span>
            <span>内存: {dashboard.overview.memoryUsedPercent.toFixed(1)}%</span>
            <span>上行速率: {formatBits(dashboard.overview.uploadBps)}</span>
            <span>下行速率: {formatBits(dashboard.overview.downloadBps)}</span>
          </div>
        </header>

        {error ? <div className="global-error">最近一次刷新失败: {error}</div> : null}
        {dashboard.warnings?.length ? <div className="global-warning">{dashboard.warnings.join(' ')}</div> : null}

        {activeView === 'overview' ? (
          <OverviewPage dashboard={dashboard} />
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
              setSelectedTerminalID(terminal.id)
              setRemarkDraft(terminal.remark ?? '')
              setRemarkModalOpen(true)
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

      {remarkModalOpen && currentTerminal ? (
        <RemarkModal
          value={remarkDraft}
          saving={savingRemark}
          onChange={setRemarkDraft}
          onClose={() => setRemarkModalOpen(false)}
          onSave={async () => {
            setSavingRemark(true)
            try {
              const response = await fetch(
                `/api/terminals/${encodeURIComponent(currentTerminal.id)}/remark`,
                {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({ remark: remarkDraft }),
                },
              )
              if (!response.ok) {
                throw new Error(`HTTP ${response.status}`)
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
              setRemarkModalOpen(false)
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

function OverviewPage(props: { dashboard: DashboardResponse }) {
  const totalConnected = props.dashboard.interfaces.filter(
    (item) => item.running && !item.disabled,
  ).length

  return (
    <div className="page-grid">
      <section className="panel summary-grid">
        <StatusTile label="运行时间" value={props.dashboard.overview.uptime} />
        <StatusTile label="CPU 占用" value={`${props.dashboard.overview.cpuLoadPercent}%`} />
        <StatusTile
          label="内存占用"
          value={`${props.dashboard.overview.memoryUsedPercent.toFixed(1)}%`}
          detail={`${formatBytes(props.dashboard.overview.memoryUsedBytes)} / ${formatBytes(props.dashboard.overview.memoryTotalBytes)}`}
        />
        <StatusTile label="上行总速率" value={formatBits(props.dashboard.overview.uploadBps)} />
        <StatusTile label="下行总速率" value={formatBits(props.dashboard.overview.downloadBps)} />
        <StatusTile label="在线接口" value={`${totalConnected}`} />
      </section>

      <section className="two-column">
        <section className="panel">
          <div className="panel-head">
            <h3>近 5 分钟上下行速率</h3>
            <span>采样接口: {props.dashboard.overview.trafficInterfaces.join(', ')}</span>
          </div>
          <TrafficChart samples={props.dashboard.overview.chartSamples} />
        </section>

        <section className="panel">
          <div className="panel-head"><h3>设备信息</h3><span>RouterOS 只读连接</span></div>
          <div className="detail-grid overview-info">
            <DetailItem label="设备名称" value={props.dashboard.overview.routerName || '-'} />
            <DetailItem label="平台" value={props.dashboard.overview.platform || '-'} />
            <DetailItem label="硬件" value={props.dashboard.overview.boardName || '-'} />
            <DetailItem label="版本" value={props.dashboard.overview.version || '-'} />
            <DetailItem label="流量接口" value={props.dashboard.overview.trafficInterfaces.join(', ') || '-'} />
            <DetailItem label="存储使用" value={props.dashboard.overview.storageTotalBytes ? `${props.dashboard.overview.storageUsedPercent.toFixed(1)}%（${formatBytes(props.dashboard.overview.storageUsedBytes)} / ${formatBytes(props.dashboard.overview.storageTotalBytes)}）` : '-'} />
            <DetailItem label="硬件健康数据" value={props.dashboard.overview.healthEnabled ? '可用' : '当前 CHR 不提供'} />
          </div>
        </section>
      </section>
    </div>
  )
}

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
    {detail ? <section className="panel interface-detail"><div className="panel-head"><h3>{detail.interface.name} 接口详情</h3><button type="button" className="close-button" onClick={() => setSelected(null)}>关闭</button></div><div className="detail-summary"><DetailSummary label="状态" value={detail.interface.running && !detail.interface.disabled ? '在线' : '离线'} /><DetailSummary label="地址" value={detail.interface.addresses?.join(' / ') || '-'} /><DetailSummary label="MAC" value={detail.interface.macAddress || '-'} /><DetailSummary label="协商速率" value={detail.interface.linkRate ? `${detail.interface.linkRate}${detail.interface.fullDuplex ? ' / 全双工' : ''}` : '-'} /><DetailSummary label="当前上行" value={formatBits(detail.interface.currentTxBps)} /><DetailSummary label="当前下行" value={formatBits(detail.interface.currentRxBps)} /><DetailSummary label="收 / 发包" value={`${detail.interface.rxPackets} / ${detail.interface.txPackets}`} /><DetailSummary label="错误 / 丢包" value={`${detail.interface.rxErrors + detail.interface.txErrors} / ${detail.interface.rxDrops + detail.interface.txDrops}`} /></div><TrafficChart samples={detail.samples} /></section> : null}
    <section className="panel compact-panel">
      <div className="data-toolbar"><strong>接口运行状态</strong><span className="result-count">物理与逻辑接口只读监控</span><span className="toolbar-spacer" /><span>共 {props.interfaces.length} 个接口</span></div>
      <div className="table-scroll">
        <table className="data-table">
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

type TerminalSortKey = 'address' | 'connections' | 'upload' | 'download' | 'totalUpload' | 'totalDownload' | 'online' | 'interface' | 'device' | 'remark'

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
  const [sortKey, setSortKey] = useState<TerminalSortKey>('address')
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('asc')
  const [stateFilter, setStateFilter] = useState('all')
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
          <option value="all">全部状态</option><option value="online">在线</option><option value="idle">空闲</option><option value="offline">离线</option>
        </select>
        <select value={interfaceFilter} onChange={(event) => setInterfaceFilter(event.target.value)} aria-label="接入接口">
          <option value="all">全部接口</option>{interfaces.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
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
            <SortHeader label="IP / MAC" sortKey="address" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="连接数" sortKey="connections" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="上行速率" sortKey="upload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="下行速率" sortKey="download" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="累计上行" sortKey="totalUpload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="累计下行" sortKey="totalDownload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="在线时长" sortKey="online" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="接口" sortKey="interface" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="设备" sortKey="device" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="备注" sortKey="remark" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <th>操作</th>
          </tr></thead>
          <tbody>
            {rows.map((terminal) => {
              const mainAddress = terminalPrimaryAddress(terminal, props.family)
              const addressCount = props.family === 'ipv4' ? terminal.ipv4.length : props.family === 'ipv6' ? terminal.ipv6.length : terminal.ipv4.length + terminal.ipv6.length
              return <tr key={terminal.id}>
                <td><button type="button" className="link-button terminal-link" onClick={() => props.onOpenDetail(terminal.id)}>
                  <strong>{mainAddress || '-'}</strong>
                  <span className="muted-text">{terminal.macAddress || 'MAC 未知'}{addressCount > 1 ? `  +${addressCount - 1}` : ''}</span>
                </button></td>
                <td>{terminal.connectionCount}</td><td>{formatBits(terminal.currentUploadBps)}</td><td>{formatBits(terminal.currentDownloadBps)}</td>
                <td>{formatBytes(terminal.totalUploadBytes)}</td><td>{formatBytes(terminal.totalDownloadBytes)}</td>
                <td><span className={`state-dot state-${terminal.state}`} />{terminal.state === 'offline' ? '-' : formatOnlineDuration(terminal.onlineSince)}</td>
                <td>{terminal.primaryInterface || '-'}</td>
                <td>{terminal.displayName && terminal.displayName !== terminal.macAddress && terminal.displayName !== mainAddress ? terminal.displayName : '-'}</td>
                <td>{terminal.remark || '-'}</td>
                <td><div className="action-links"><button type="button" className="link-button" onClick={() => props.onOpenDetail(terminal.id)}>详情</button><button type="button" className="link-button" onClick={() => props.onOpenRemark(terminal)}>修改备注</button></div></td>
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
        <div className="detail-summary">
          <DetailSummary label="主地址" value={terminalPrimaryAddress(summary, props.scope)} />
          <DetailSummary label="MAC" value={summary.macAddress || '-'} />
          <DetailSummary label="状态" value={terminalStateText(summary.state)} />
          <DetailSummary label="连接" value={`${summary.connectionCount}`} />
          <DetailSummary label="当前上行" value={formatBits(summary.currentUploadBps)} />
          <DetailSummary label="当前下行" value={formatBits(summary.currentDownloadBps)} />
        </div>
        <div className="detail-head-actions">
          <button type="button" className="close-button" onClick={props.onBack}>
            返回
          </button>
        </div>
      </div>

      <div className="tab-row detail-tabs">
        <TabButton label="基础信息" active={props.activeTab === 'basic'} onClick={() => props.onTabChange('basic')} />
        <TabButton label="连接详情" active={props.activeTab === 'connections'} onClick={() => props.onTabChange('connections')} />
        <TabButton label="流量分布" active={props.activeTab === 'flows'} onClick={() => props.onTabChange('flows')} />
        {props.scope === 'all' ? <TabButton label="历史记录" active={props.activeTab === 'history'} onClick={() => props.onTabChange('history')} /> : null}
      </div>

      <section className="panel detail-panel">
        {props.activeTab === 'basic' ? (
          <div className="detail-grid">
            {props.scope !== 'ipv6' ? <DetailItem label="IPv4 地址" value={summary.ipv4.join(' / ') || '-'} /> : null}
            {props.scope !== 'ipv4' ? <DetailItem label="IPv6 地址" value={summary.ipv6.join(' / ') || '-'} /> : null}
            <DetailItem label="MAC 地址" value={summary.macAddress || '-'} />
            <DetailItem label="接入接口" value={summary.primaryInterface || '-'} />
            <DetailItem label={props.scope === 'all' ? '连接数（IPv4+IPv6）' : `${props.scope.toUpperCase()} 连接数`} value={`${summary.connectionCount}`} />
            <DetailItem label="本次在线时长" value={summary.state === 'offline' ? '-' : formatOnlineDuration(summary.onlineSince)} />
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
                label={`全部连接 (${props.detail.connections.length})`}
                active={props.connectionFamily === 'all'}
                onClick={() => props.onConnectionFamilyChange('all')}
              /> : null}
              {props.scope !== 'ipv6' ? (
              <TabButton
                label={`IPv4 连接详情 (${ipv4Connections.length})`}
                active={props.connectionFamily === 'ipv4'}
                onClick={() => props.onConnectionFamilyChange('ipv4')}
              />
              ) : null}
              {props.scope !== 'ipv4' ? (
              <TabButton
                label={`IPv6 连接详情 (${ipv6Connections.length})`}
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
                    <th>连接状态</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleConnections.length === 0 ? (
                    <tr>
                      <td colSpan={13} className="empty-row">
                        当前没有 {props.connectionFamily === 'all' ? '活动' : props.connectionFamily.toUpperCase()} 连接详情
                      </td>
                    </tr>
                  ) : (
                    visibleConnections.map((connection) => (
                      <tr key={connection.key}>
                        <td>{connection.application}</td>
                        <td>{connection.protocol}</td>
                        <td>{connection.line}</td>
                        <td>{connection.sourceAddress}{connection.sourcePort ? `:${connection.sourcePort}` : ''}</td>
                        <td>{connection.destinationAddress}</td>
                        <td>{connection.destinationPort || '-'}</td>
                        <td>{connection.publicAddress || '-'}</td>
                        <td>{formatBits(connection.uploadBps)}</td>
                        <td>{formatBits(connection.downloadBps)}</td>
                        <td>{formatBytes(connection.uploadBytes)}</td>
                        <td>{formatBytes(connection.downloadBytes)}</td>
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

function RemarkModal(props: {
  value: string
  saving: boolean
  onChange: (value: string) => void
  onClose: () => void
  onSave: () => void
}) {
  return (
    <div className="dialog-backdrop" role="dialog" aria-modal="true">
      <div className="remark-modal">
        <div className="dialog-head">
          <div>
            <h3>修改备注</h3>
            <p className="muted-text">备注只保存到面板本地，不写回 RouterOS。</p>
          </div>
          <button type="button" className="close-button" onClick={props.onClose}>
            关闭
          </button>
        </div>
        <div className="remark-modal-body">
          <textarea
            value={props.value}
            onChange={(event) => props.onChange(event.target.value)}
            rows={5}
            className="remark-textarea"
          />
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

function StatusTile(props: { label: string; value: string; detail?: string }) {
  return (
    <article className="status-tile">
      <p>{props.label}</p>
      <h3>{props.value}</h3>
      {props.detail ? <span>{props.detail}</span> : null}
    </article>
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

function TrafficChart(props: { samples: RateSample[] }) {
  const width = 820
  const height = 280
  const padding = 28

  if (props.samples.length === 0) {
    return <div className="empty-chart">暂无速率采样</div>
  }

  const maxValue = Math.max(
    1,
    ...props.samples.flatMap((sample) => [sample.uploadBps, sample.downloadBps]),
  )

  const toPoint = (value: number, index: number) => {
    const x =
      props.samples.length === 1
        ? width / 2
        : padding + (index * (width - padding * 2)) / (props.samples.length - 1)
    const y = height - padding - (value / maxValue) * (height - padding * 2)
    return `${x},${y}`
  }

  return (
    <div className="chart-box">
      <svg viewBox={`0 0 ${width} ${height}`} className="chart-svg">
        {[0, 0.25, 0.5, 0.75, 1].map((step) => {
          const y = height - padding - step * (height - padding * 2)
          return <line key={step} x1={padding} x2={width - padding} y1={y} y2={y} className="grid-line" />
        })}
        <polyline
          className="polyline upload"
          points={props.samples.map((sample, index) => toPoint(sample.uploadBps, index)).join(' ')}
        />
        <polyline
          className="polyline download"
          points={props.samples.map((sample, index) => toPoint(sample.downloadBps, index)).join(' ')}
        />
      </svg>
      <div className="legend">
        <span><i className="legend-dot upload"></i>上行</span>
        <span><i className="legend-dot download"></i>下行</span>
        <span>峰值 {formatBits(maxValue)}</span>
      </div>
    </div>
  )
}

function viewTitle(view: ActiveView) {
  switch (view) {
    case 'overview':
      return '系统概况'
    case 'interfaces':
      return '线路监控'
    case 'terminals':
      return '终端监控'
    case 'load':
      return '负载历史'
    case 'protocols':
      return '协议统计'
    case 'policies':
      return '策略统计'
    case 'routes':
      return '路由 / 分流'
    default:
      return ''
  }
}

function terminalStateText(value: Terminal['state']) {
  if (value === 'online') return '在线'
  if (value === 'idle') return '空闲'
  return '离线'
}

function formatBits(value: number) {
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let output = value / 8
  let index = 0
  while (output >= 1024 && index < units.length - 1) {
    output /= 1024
    index += 1
  }
  return `${output.toFixed(output >= 100 ? 0 : output >= 10 ? 1 : 2)} ${units[index]}`
}

function formatBytes(value: number) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let output = value
  let index = 0
  while (output >= 1024 && index < units.length - 1) {
    output /= 1024
    index += 1
  }
  return `${output.toFixed(output >= 100 ? 0 : output >= 10 ? 1 : 2)} ${units[index]}`
}

function formatDateTime(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function formatShortTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function formatOnlineDuration(trackingSince: string) {
  if (!trackingSince) {
    return '-'
  }
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(trackingSince).getTime()) / 1000))
  return formatSeconds(seconds)
}

function terminalPrimaryAddress(terminal: Terminal, family: TerminalFamily) {
  if (family === 'ipv4') return terminal.primaryIpv4 || '-'
  if (family === 'ipv6') return terminal.primaryIpv6 || '-'
  return terminal.primaryIpv4 || terminal.primaryIpv6 || '-'
}

function compareTerminal(left: Terminal, right: Terminal, key: TerminalSortKey, family: TerminalFamily) {
  const text = (a: string, b: string) => a.localeCompare(b, 'zh-CN', { numeric: true, sensitivity: 'base' })
  switch (key) {
    case 'address':
      if (family === 'ipv4') return compareIp(left.primaryIpv4, right.primaryIpv4) || text(left.macAddress, right.macAddress)
      if (family === 'ipv6') return compareIp(left.primaryIpv6, right.primaryIpv6) || text(left.macAddress, right.macAddress)
      return compareIp(left.primaryIpv4, right.primaryIpv4) || compareIp(left.primaryIpv6, right.primaryIpv6) || text(left.macAddress, right.macAddress)
    case 'connections': return left.connectionCount - right.connectionCount
    case 'upload': return left.currentUploadBps - right.currentUploadBps
    case 'download': return left.currentDownloadBps - right.currentDownloadBps
    case 'totalUpload': return left.totalUploadBytes - right.totalUploadBytes
    case 'totalDownload': return left.totalDownloadBytes - right.totalDownloadBytes
    case 'online': return new Date(left.onlineSince || 0).getTime() - new Date(right.onlineSince || 0).getTime()
    case 'interface': return text(left.primaryInterface, right.primaryInterface)
    case 'device': return text(left.displayName, right.displayName)
    case 'remark': return text(left.remark, right.remark)
  }
}

function compareIp(left: string, right: string) {
  if (left && !right) return -1
  if (!left && right) return 1
  if (!left && !right) return 0
  const leftV4 = left.split('.').map(Number)
  const rightV4 = right.split('.').map(Number)
  if (leftV4.length === 4 && rightV4.length === 4) {
    for (let index = 0; index < 4; index += 1) {
      if (leftV4[index] !== rightV4[index]) return leftV4[index] - rightV4[index]
    }
    return 0
  }
  return left.localeCompare(right, 'en', { numeric: true })
}

function formatSeconds(seconds: number) {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remainSeconds = seconds % 60
  if (days > 0) {
    return `${days}天${hours}小时${minutes}分`
  }
  if (hours > 0) {
    return `${hours}小时${minutes}分${remainSeconds}秒`
  }
  if (minutes > 0) {
    return `${minutes}分${remainSeconds}秒`
  }
  return `${remainSeconds}秒`
}

export default App
