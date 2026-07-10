import { useEffect, useMemo, useState } from 'react'

type RateSample = {
  timestamp: string
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
}

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
}

type CapabilityNote = {
  area: string
  item: string
  status: string
  details: string
}

type DashboardResponse = {
  overview: Overview
  interfaces: InterfaceStatus[]
  terminals: Terminal[]
  capabilities: CapabilityNote[]
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
}

type ActiveView = 'overview' | 'interfaces' | 'terminals'
type TerminalTab = 'basic' | 'connections' | 'flows' | 'history'
type ConnectionFamily = 'ipv4' | 'ipv6'

function App() {
  const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
  const [activeView, setActiveView] = useState<ActiveView>('overview')
  const [query, setQuery] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [selectedTerminalID, setSelectedTerminalID] = useState<string | null>(null)
  const [terminalDetail, setTerminalDetail] = useState<TerminalDetail | null>(null)
  const [terminalTab, setTerminalTab] = useState<TerminalTab>('basic')
  const [connectionFamily, setConnectionFamily] = useState<ConnectionFamily>('ipv4')
  const [remarkModalOpen, setRemarkModalOpen] = useState(false)
  const [remarkDraft, setRemarkDraft] = useState('')
  const [savingRemark, setSavingRemark] = useState(false)

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
    const timer = window.setInterval(refresh, 5000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

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

    load().catch((detailError) => {
      if (!cancelled) {
        setError(detailError instanceof Error ? detailError.message : '终端详情读取失败')
      }
    })

    return () => {
      cancelled = true
    }
  }, [selectedTerminalID])

  const filteredTerminals = useMemo(() => {
    if (!dashboard) {
      return []
    }
    const keyword = query.trim().toLowerCase()
    if (!keyword) {
      return dashboard.terminals
    }
    return dashboard.terminals.filter((terminal) =>
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
        .includes(keyword),
    )
  }, [dashboard, query])

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
                activeView === 'interfaces' || activeView === 'terminals'
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
                接口状态
              </button>
              <button
                type="button"
                className={activeView === 'terminals' ? 'submenu-item active' : 'submenu-item'}
                onClick={() => setActiveView('terminals')}
              >
                终端监控
              </button>
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
                ? `状态监控 > 终端监控 > ${currentTerminal?.ipv4[0] ? 'IPv4' : 'IPv6'}`
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

        {activeView === 'overview' ? (
          <OverviewPage dashboard={dashboard} />
        ) : null}

        {activeView === 'interfaces' ? (
          <InterfacesPage interfaces={dashboard.interfaces} />
        ) : null}

        {activeView === 'terminals' && !detailMode ? (
          <TerminalsPage
            terminals={filteredTerminals}
            query={query}
            onQueryChange={setQuery}
            onOpenDetail={(terminalID) => {
              setSelectedTerminalID(terminalID)
              setTerminalTab('basic')
              setConnectionFamily('ipv4')
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
            onBack={() => {
              setSelectedTerminalID(null)
              setTerminalDetail(null)
            }}
            onTabChange={setTerminalTab}
            onConnectionFamilyChange={setConnectionFamily}
            onOpenRemark={() => {
              setRemarkDraft(terminalDetail.terminal.remark ?? '')
              setRemarkModalOpen(true)
            }}
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
          <div className="panel-head">
            <h3>能力说明</h3>
            <span>按当前 RouterOS 能力区分</span>
          </div>
          <div className="capability-list">
            {props.dashboard.capabilities.map((note) => (
              <article key={`${note.area}-${note.item}`} className="capability-card">
                <div className="capability-top">
                  <strong>{note.item}</strong>
                  <span className={`badge badge-${note.status}`}>{statusText(note.status)}</span>
                </div>
                <p className="muted-text">{note.details}</p>
              </article>
            ))}
          </div>
        </section>
      </section>
    </div>
  )
}

function InterfacesPage(props: { interfaces: InterfaceStatus[] }) {
  return (
    <section className="panel">
      <div className="panel-head">
        <h3>接口状态</h3>
        <span>当前接口与线路运行情况</span>
      </div>
      <div className="table-scroll">
        <table className="data-table">
          <thead>
            <tr>
              <th>接口</th>
              <th>类型</th>
              <th>状态</th>
              <th>上行速率</th>
              <th>下行速率</th>
              <th>累计上行</th>
              <th>累计下行</th>
              <th>MTU</th>
              <th>掉线次数</th>
            </tr>
          </thead>
          <tbody>
            {props.interfaces.map((item) => (
              <tr key={item.name}>
                <td>{item.name}</td>
                <td>{item.type}</td>
                <td>{item.running && !item.disabled ? '在线' : '离线'}</td>
                <td>{formatBits(item.currentTxBps)}</td>
                <td>{formatBits(item.currentRxBps)}</td>
                <td>{formatBytes(item.txBytes)}</td>
                <td>{formatBytes(item.rxBytes)}</td>
                <td>{item.actualMtu || '-'}</td>
                <td>{item.linkDowns}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function TerminalsPage(props: {
  terminals: Terminal[]
  query: string
  onQueryChange: (value: string) => void
  onOpenDetail: (terminalID: string) => void
  onOpenRemark: (terminal: Terminal) => void
}) {
  return (
    <section className="panel">
      <div className="panel-head terminals-head">
        <div>
          <h3>终端监控</h3>
          <span>按设备聚合显示，连接数和速率已合并 IPv4 / IPv6</span>
        </div>
        <input
          className="search-input"
          value={props.query}
          onChange={(event) => props.onQueryChange(event.target.value)}
          placeholder="备注 / IP / MAC"
        />
      </div>

      <div className="table-scroll">
        <table className="data-table terminal-table">
          <thead>
            <tr>
              <th>设备</th>
              <th>地址</th>
              <th>连接数</th>
              <th>上行速率</th>
              <th>下行速率</th>
              <th>累计上行</th>
              <th>累计下行</th>
              <th>在线时长</th>
              <th>备注</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {props.terminals.map((terminal) => (
              <tr key={terminal.id}>
                <td>
                  <button
                    type="button"
                    className="link-button terminal-link"
                    onClick={() => props.onOpenDetail(terminal.id)}
                  >
                    <strong>{terminal.remark || terminal.displayName || terminal.macAddress || '未知设备'}</strong>
                    <span className="muted-text">{terminal.macAddress || 'MAC 未知'}</span>
                  </button>
                </td>
                <td>
                  <div className="address-stack">
                    {terminal.ipv4.length > 0 ? <span>IPv4: {terminal.ipv4.join(' / ')}</span> : null}
                    {terminal.ipv6.length > 0 ? <span>IPv6: {terminal.ipv6.join(' / ')}</span> : <span className="muted-text">IPv6: 无</span>}
                  </div>
                </td>
                <td>{terminal.connectionCount}</td>
                <td>{formatBits(terminal.currentUploadBps)}</td>
                <td>{formatBits(terminal.currentDownloadBps)}</td>
                <td>{formatBytes(terminal.totalUploadBytes)}</td>
                <td>{formatBytes(terminal.totalDownloadBytes)}</td>
                <td>{formatOnlineDuration(terminal.trackingSince)}</td>
                <td>{terminal.remark || '-'}</td>
                <td>
                  <div className="action-links">
                    <button type="button" className="link-button" onClick={() => props.onOpenDetail(terminal.id)}>
                      详情
                    </button>
                    <button type="button" className="link-button" onClick={() => props.onOpenRemark(terminal)}>
                      修改备注
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function TerminalDetailPage(props: {
  detail: TerminalDetail
  activeTab: TerminalTab
  connectionFamily: ConnectionFamily
  onBack: () => void
  onTabChange: (value: TerminalTab) => void
  onConnectionFamilyChange: (value: ConnectionFamily) => void
  onOpenRemark: () => void
}) {
  const capabilityByTab = Object.fromEntries(
    props.detail.capabilities.map((item) => [item.tab, item]),
  )
  const ipv4Connections = props.detail.connections.filter((item) => item.family === 'ipv4')
  const ipv6Connections = props.detail.connections.filter((item) => item.family === 'ipv6')
  const visibleConnections =
    props.connectionFamily === 'ipv4' ? ipv4Connections : ipv6Connections

  return (
    <section className="detail-page">
      <div className="detail-breadcrumb">
        <span>状态监控</span>
        <span>&gt;</span>
        <span>终端监控</span>
        <span>&gt;</span>
        <span>{props.detail.terminal.ipv4[0] || props.detail.terminal.ipv6[0] || props.detail.terminal.macAddress}</span>
      </div>

      <div className="detail-page-head">
        <div>
          <h3>
            终端详情（IP:
            {props.detail.terminal.ipv4[0] || props.detail.terminal.ipv6[0] || '-'}）
          </h3>
          <p className="muted-text">
            设备主体按 MAC 聚合，概览速率与连接数已合并 IPv4 / IPv6
          </p>
        </div>
        <div className="detail-head-actions">
          <button type="button" className="primary-button" onClick={props.onOpenRemark}>
            修改备注
          </button>
          <button type="button" className="close-button" onClick={props.onBack}>
            返回
          </button>
        </div>
      </div>

      <div className="tab-row detail-tabs">
        <TabButton label="基础信息" active={props.activeTab === 'basic'} onClick={() => props.onTabChange('basic')} />
        <TabButton label="连接详情" active={props.activeTab === 'connections'} onClick={() => props.onTabChange('connections')} />
        <TabButton label="流量分布" active={props.activeTab === 'flows'} onClick={() => props.onTabChange('flows')} />
        <TabButton label="历史记录" active={props.activeTab === 'history'} onClick={() => props.onTabChange('history')} />
      </div>

      <section className="panel detail-panel">
        {props.activeTab === 'basic' ? (
          <div className="detail-grid">
            <DetailItem label="IP 地址" value={props.detail.terminal.ipv4.join(' / ') || '-'} />
            <DetailItem label="IPv6 地址" value={props.detail.terminal.ipv6.join(' / ') || '-'} />
            <DetailItem label="MAC 地址" value={props.detail.terminal.macAddress || '-'} />
            <DetailItem label="接入接口" value={props.detail.terminal.primaryInterface || '-'} />
            <DetailItem label="连接数（IPv4+IPv6）" value={`${props.detail.terminal.connectionCount}`} />
            <DetailItem label="在线时长" value={formatOnlineDuration(props.detail.terminal.trackingSince)} />
            <DetailItem label="当前上行速率" value={formatBits(props.detail.terminal.currentUploadBps)} />
            <DetailItem label="当前下行速率" value={formatBits(props.detail.terminal.currentDownloadBps)} />
            <DetailItem label="累计上行" value={formatBytes(props.detail.terminal.totalUploadBytes)} />
            <DetailItem label="累计下行" value={formatBytes(props.detail.terminal.totalDownloadBytes)} />
            <DetailItem label="备注" value={props.detail.terminal.remark || '-'} />
            <DetailItem label="最后活动时间" value={formatDateTime(props.detail.terminal.lastSeen)} />
          </div>
        ) : null}

        {props.activeTab === 'connections' ? (
          <div>
            <CapabilityHint capability={capabilityByTab['连接详情']} />
            <div className="family-switch">
              <TabButton
                label={`IPv4 连接详情 (${ipv4Connections.length})`}
                active={props.connectionFamily === 'ipv4'}
                onClick={() => props.onConnectionFamilyChange('ipv4')}
              />
              <TabButton
                label={`IPv6 连接详情 (${ipv6Connections.length})`}
                active={props.connectionFamily === 'ipv6'}
                onClick={() => props.onConnectionFamilyChange('ipv6')}
              />
            </div>
            <div className="table-scroll">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>应用</th>
                    <th>协议</th>
                    <th>线路</th>
                    <th>源端口</th>
                    <th>目的地址</th>
                    <th>目的端口</th>
                    <th>累计上行</th>
                    <th>累计下行</th>
                    <th>连接状态</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleConnections.length === 0 ? (
                    <tr>
                      <td colSpan={9} className="empty-row">
                        当前没有 {props.connectionFamily.toUpperCase()} 连接详情
                      </td>
                    </tr>
                  ) : (
                    visibleConnections.map((connection) => (
                      <tr key={connection.key}>
                        <td>{connection.application}</td>
                        <td>{connection.protocol}</td>
                        <td>{connection.line}</td>
                        <td>{connection.sourcePort || '-'}</td>
                        <td>{connection.destinationAddress}</td>
                        <td>{connection.destinationPort || '-'}</td>
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
            <CapabilityHint capability={capabilityByTab['流量分布']} />
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
                  {props.detail.flowCategories.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="empty-row">
                        当前暂无足够连接用于估算流量分布
                      </td>
                    </tr>
                  ) : (
                    props.detail.flowCategories.map((flow) => (
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
            <CapabilityHint capability={capabilityByTab['历史记录']} />
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

function CapabilityHint(props: { capability?: TerminalCapability }) {
  if (!props.capability) {
    return null
  }
  return (
    <div className="capability-hint">
      <span className={`badge badge-${props.capability.status}`}>{statusText(props.capability.status)}</span>
      <p>{props.capability.details}</p>
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
      return '接口状态'
    case 'terminals':
      return '终端监控'
    default:
      return ''
  }
}

function statusText(value: string) {
  switch (value) {
    case 'supported_now':
      return '当前可用'
    case 'supported_with_panel_persistence':
      return '面板本地累计'
    case 'not_natively_feasible':
      return '当前不适合做'
    case 'deferred':
      return '后续扩展'
    case 'limited':
      return '当前数据有限'
    default:
      return value
  }
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
  return new Date(value).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function formatOnlineDuration(trackingSince: string) {
  if (!trackingSince) {
    return '-'
  }
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(trackingSince).getTime()) / 1000))
  return formatSeconds(seconds)
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
