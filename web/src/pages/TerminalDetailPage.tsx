import { useEffect, useMemo, useState } from 'react'
import { formatBitRate, formatBytes, formatDateTime, formatDuration, formatRelativeTime } from '../lib/format'
import { useShell } from '../shell/useShell'
import { Badge, Button, Card, DataTable, EmptyState, SegTabs, Skeleton, Tooltip } from '../ui'
import { ConnectionTable } from '../features/monitor-detail/ConnectionTable'
import { fetchProtocols, fetchTerminalDetail, type TerminalDetail, type TerminalFlowCategory } from '../features/monitor-detail/api'
import { useMonitorResource } from '../features/monitor-detail/hooks'
import { terminalStateText, terminalStateTone } from '../features/monitor-detail/utils'

type DetailTab = 'connections' | 'flows' | 'history'

const DETAIL_TABS: Array<{ value: DetailTab; label: string }> = [
  { value: 'connections', label: '连接详情' },
  { value: 'flows', label: '流量分布' },
  { value: 'history', label: '历史记录' },
]

const DETAIL_TAB_RE = /^#\/terminals\/[^/]+\/([a-z-]+)$/

/** Hash-routed tab (`#/terminals/<id>/<tab>`); missing/unknown → connections. */
function detailTabFromHash(): DetailTab {
  const seg = DETAIL_TAB_RE.exec(window.location.hash)?.[1]
  return seg === 'flows' || seg === 'history' ? seg : 'connections'
}

function FlowBars({ flows }: { flows: TerminalFlowCategory[] }) {
  const maxPercent = Math.max(1, ...flows.map((flow) => Math.max(flow.uploadPercent, flow.downloadPercent)))
  return (
    <div className="flow-bars">
      <p className="faint flow-note">按当前活动连接的协议与端口估算，不等同于 DPI 应用识别。</p>
      {flows.map((flow) => (
        <div key={flow.name} className="flow-bar-row">
          <div className="flow-bar-head">
            <span className="flow-bar-name">
              {flow.name}
              {flow.estimated ? <Badge tone="warn">估算</Badge> : null}
            </span>
            <span className="flow-bar-rates num">
              ↓ {formatBitRate(flow.currentDownloadBps)} · ↑ {formatBitRate(flow.currentUploadBps)}
            </span>
          </div>
          <div className="flow-bar-track" role="img" aria-label={`${flow.name} 下载占比 ${flow.downloadPercent.toFixed(1)}%，上传占比 ${flow.uploadPercent.toFixed(1)}%`}>
            <span className="flow-bar-fill flow-bar-down" style={{ width: `${Math.min(100, (flow.downloadPercent / maxPercent) * 100)}%` }} />
          </div>
          <div className="flow-bar-track">
            <span className="flow-bar-fill flow-bar-up" style={{ width: `${Math.min(100, (flow.uploadPercent / maxPercent) * 100)}%` }} />
          </div>
          <div className="flow-bar-legend faint num">
            ↓ {formatBytes(flow.totalDownloadBytes)} · {flow.downloadPercent.toFixed(1)}%　↑ {formatBytes(flow.totalUploadBytes)} · {flow.uploadPercent.toFixed(1)}%
          </div>
        </div>
      ))}
    </div>
  )
}

function HistoryView({ detail }: { detail: TerminalDetail }) {
  const history = detail.history
  const recent = history.slice(-60)
  const latest = history[history.length - 1]
  const maxSeconds = Math.max(1, ...recent.map((entry) => entry.onlineSeconds))
  const familyCards: Array<{ label: string; summary: TerminalDetail['familySummaries']['ipv4'] }> = [
    { label: 'IPv4', summary: detail.familySummaries.ipv4 },
    { label: 'IPv6', summary: detail.familySummaries.ipv6 },
  ]

  if (history.length === 0) {
    return <EmptyState icon="🕘" title="暂无历史记录" description="历史记录来自面板本地累计快照，从面板开始运行后持续记录，稍后再来查看。" />
  }

  return (
    <div className="terminal-history">
      <div className="history-stat-grid">
        <Card className="history-stat">
          <span className="faint">最新累计下行</span>
          <strong className="num">{formatBytes(latest.totalDownloadBytes)}</strong>
        </Card>
        <Card className="history-stat">
          <span className="faint">最新累计上行</span>
          <strong className="num">{formatBytes(latest.totalUploadBytes)}</strong>
        </Card>
        <Card className="history-stat">
          <span className="faint">最近在线时长</span>
          <strong className="num">{formatDuration(latest.onlineSeconds)}</strong>
        </Card>
        <Card className="history-stat">
          <span className="faint">快照条数</span>
          <strong className="num">{history.length}</strong>
        </Card>
      </div>

      <Card title="在线时长分布" sub={`最近 ${recent.length} 条快照，每条代表一次面板采样`}>
        <div className="history-bars" role="img" aria-label="最近快照的在线时长柱状图">
          {recent.map((entry) => (
            <span
              key={entry.timestamp}
              className="history-bar"
              style={{ height: `${Math.max(3, (entry.onlineSeconds / maxSeconds) * 100)}%` }}
              title={`${formatDateTime(entry.timestamp)} · 在线 ${formatDuration(entry.onlineSeconds)}`}
            />
          ))}
        </div>
      </Card>

      <div className="history-family-grid">
        {familyCards.map(({ label, summary }) => (
          <Card key={label} title={`${label} 汇总`} sub="当前活动连接">
            {summary ? (
              <div className="mon-kv-stack">
                <div className="kv">
                  <span>连接数</span>
                  <b>{summary.connectionCount}</b>
                </div>
                <div className="kv">
                  <span>实时 ↓ / ↑</span>
                  <b>
                    {formatBitRate(summary.currentDownloadBps)} / {formatBitRate(summary.currentUploadBps)}
                  </b>
                </div>
                <div className="kv">
                  <span>活动累计 ↓ / ↑</span>
                  <b>
                    {formatBytes(summary.totalDownloadBytes)} / {formatBytes(summary.totalUploadBytes)}
                  </b>
                </div>
              </div>
            ) : (
              <p className="faint">当前没有 {label} 活动连接。</p>
            )}
          </Card>
        ))}
      </div>

      <Card title="历史明细" sub={`共 ${history.length} 条，显示最近 20 条`}>
        <DataTable
          ariaLabel="终端历史明细"
          columns={[
            { key: 'timestamp', title: '时间', render: (row) => formatDateTime(row.timestamp) },
            { key: 'online', title: '在线时长', numeric: true, render: (row) => <span className="num">{formatDuration(row.onlineSeconds)}</span> },
            { key: 'down', title: '累计下行', numeric: true, render: (row) => <span className="num">{formatBytes(row.totalDownloadBytes)}</span> },
            { key: 'up', title: '累计上行', numeric: true, render: (row) => <span className="num">{formatBytes(row.totalUploadBytes)}</span> },
          ]}
          rows={[...history].reverse().slice(0, 20)}
          rowKey={(row) => row.timestamp}
        />
      </Card>
    </div>
  )
}

type TerminalDetailPageProps = {
  terminalId: string
  onBack: () => void
}

export default function TerminalDetailPage({ terminalId, onBack }: TerminalDetailPageProps) {
  const { scopedPath, selectedDeviceId, refreshMs, reloadNonce } = useShell()
  const { data: detail, loading, error, reload } = useMonitorResource(() => fetchTerminalDetail(scopedPath, terminalId), refreshMs, [
    selectedDeviceId,
    terminalId,
    reloadNonce,
  ])
  const [tab, setTab] = useState<DetailTab>(() => detailTabFromHash())
  const [protocolsEnabled, setProtocolsEnabled] = useState<boolean | null>(null)
  const isRouterSelf = terminalId === 'routeros:self'

  // Browser back/forward and direct hash edits drive the detail tab too.
  useEffect(() => {
    const sync = () => setTab(detailTabFromHash())
    window.addEventListener('popstate', sync)
    window.addEventListener('hashchange', sync)
    return () => {
      window.removeEventListener('popstate', sync)
      window.removeEventListener('hashchange', sync)
    }
  }, [])

  const selectTab = (next: DetailTab) => {
    setTab(next)
    window.history.pushState(null, '', next === 'connections' ? `#/terminals/${encodeURIComponent(terminalId)}` : `#/terminals/${encodeURIComponent(terminalId)}/${next}`)
  }

  useEffect(() => {
    let cancelled = false
    fetchProtocols(scopedPath)
      .then((response) => {
        if (!cancelled) setProtocolsEnabled(response.enabled)
      })
      .catch(() => {
        if (!cancelled) setProtocolsEnabled(null)
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- scopedPath 随 shell 每次轮询重建，设备切换才需要重新探测
  }, [selectedDeviceId])

  const terminal = detail?.terminal ?? null
  const chips = useMemo(() => {
    if (!terminal) return []
    const list: Array<{ key: string; label: string; value: string }> = []
    if (terminal.primaryIpv4) list.push({ key: 'ipv4', label: 'IPv4', value: terminal.primaryIpv4 })
    if (terminal.primaryIpv6) list.push({ key: 'ipv6', label: 'IPv6', value: terminal.primaryIpv6 })
    if (terminal.macAddress) list.push({ key: 'mac', label: 'MAC', value: terminal.macAddress })
    if (terminal.primaryInterface) list.push({ key: 'iface', label: '接口', value: terminal.primaryInterface })
    return list
  }, [terminal])

  if (loading && !detail) {
    return (
      <div className="page terminal-detail-page">
        <Skeleton height={22} width="32%" />
        <Card>
          <Skeleton lines={3} height={14} />
        </Card>
        <Card>
          <Skeleton lines={6} height={14} />
        </Card>
      </div>
    )
  }

  if (error && !detail) {
    return (
      <div className="page terminal-detail-page">
        <EmptyState icon="⚠️" title="终端详情读取失败" description={error} actionLabel="重试" onAction={reload} />
        <Button variant="ghost" onClick={onBack} className="detail-back-fallback">
          返回终端列表
        </Button>
      </div>
    )
  }

  if (!detail || !terminal) return null

  return (
    <div className="page terminal-detail-page">
      <header className="detail-header">
        <Button variant="ghost" size="sm" onClick={onBack} className="detail-back">
          ← 返回
        </Button>
        <span className="detail-icon" aria-hidden="true">
          {isRouterSelf ? '🔀' : '💻'}
        </span>
        <h1 className="detail-name">{terminal.displayName || terminal.id}</h1>
        <Badge tone={terminalStateTone(terminal.state)} dot>
          {terminalStateText(terminal.state)}
        </Badge>
        <span className="detail-inline-meta num">
          {chips.map((chip) => (
            <span key={chip.key} title={`${chip.label} ${chip.value}`}>
              <em className="detail-chip-label">{chip.label}</em> {chip.value}
            </span>
          ))}
          <span>↓ {formatBitRate(terminal.currentDownloadBps)}</span>
          <span>↑ {formatBitRate(terminal.currentUploadBps)}</span>
          <span>{isRouterSelf ? `跟踪条目 ${terminal.connectionCount}` : `连接 ${terminal.connectionCount}`}</span>
          <Tooltip tip={formatDateTime(terminal.lastSeen)}>
            <span>最后活动 {formatRelativeTime(terminal.lastSeen)}</span>
          </Tooltip>
          <span className="faint">统计始于 {formatDateTime(terminal.trackingSince)}</span>
        </span>
        {terminal.remark ? (
          <span className="detail-remark faint" title={terminal.remark}>
            {terminal.remark}
          </span>
        ) : null}
      </header>

      {isRouterSelf ? (
        <p className="conntrack-note">
          routeros:self 是 RouterOS 自身的连接跟踪（conntrack）视图：方向以路由器自身为参照，状态列反映回包确认（assured / seenReply）。
        </p>
      ) : null}

      {error ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      <SegTabs options={DETAIL_TABS} value={tab} onChange={selectTab} ariaLabel="终端详情页签" />

      {tab === 'connections' ? (
        <Card className="detail-connections-card">
          <ConnectionTable connections={detail.connections} emptyLabel={detail.connections.length === 0 ? '当前没有活动连接' : '没有符合筛选条件的连接'} />
        </Card>
      ) : null}

      {tab === 'flows' ? (
        <Card title="流量分布" sub="按应用分类的实时速率与累计占比">
          {protocolsEnabled === false ? (
            <EmptyState icon="🧭" title="未开启协议分析" description="在「识别设置」中为该设备开启协议分析后，这里会展示按应用分类的流量分布。" />
          ) : protocolsEnabled === null ? (
            <Skeleton lines={4} height={14} />
          ) : detail.flowCategories.length === 0 ? (
            <EmptyState icon="📊" title="当前暂无足够连接用于估算流量分布" description="等有活动连接后，这里会显示各应用分类的占比。" />
          ) : (
            <FlowBars flows={detail.flowCategories} />
          )}
        </Card>
      ) : null}

      {tab === 'history' ? <HistoryView detail={detail} /> : null}
    </div>
  )
}
