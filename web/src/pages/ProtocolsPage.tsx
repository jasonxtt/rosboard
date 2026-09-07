import { useMemo } from 'react'
import { formatBitRate, formatBytes, formatCount, formatPercent } from '../lib/format'
import type { ProtocolStat } from '../lib/types'
import { useShell } from '../shell/useShell'
import { Badge, Card, DataTable, EmptyState, Skeleton, type TableColumn } from '../ui'
import { fetchProtocols } from '../features/monitor-detail/api'
import { useMonitorResource } from '../features/monitor-detail/hooks'
import './monitor-common.css'
import './protocols.css'

const PROTOCOLS_POLL_MS = 30_000

function sourceBadge(item: ProtocolStat) {
  if (item.source === 'mosdns') return <Badge tone="accent">MosDNS 匹配</Badge>
  if (item.source === 'mixed') return <Badge tone="accent">DNS + 端口混合</Badge>
  if (item.estimated) return <Badge tone="warn">端口估算</Badge>
  return <Badge tone="neutral">RouterOS 原生</Badge>
}

function kindBadge(kind: string) {
  const normalized = kind.toLowerCase()
  if (!normalized) return <Badge tone="neutral">-</Badge>
  return <Badge tone={normalized === 'tcp' ? 'accent' : 'neutral'}>{kind.toUpperCase()}</Badge>
}

export default function ProtocolsPage() {
  const { scopedPath, selectedDeviceId, reloadNonce, navigate } = useShell()
  const { data, loading, error, reload } = useMonitorResource(() => fetchProtocols(scopedPath), PROTOCOLS_POLL_MS, [selectedDeviceId, reloadNonce])

  const protocols = useMemo(() => [...(data?.protocols ?? [])].sort((a, b) => b.uploadBytes + b.downloadBytes - (a.uploadBytes + a.downloadBytes)), [data])
  const totalBytes = protocols.reduce((sum, item) => sum + item.uploadBytes + item.downloadBytes, 0)
  const totalConnections = protocols.reduce((sum, item) => sum + item.connections, 0)
  const identifiedConnections = protocols.reduce((sum, item) => sum + (item.estimated ? 0 : item.connections), 0)
  const top = protocols[0]

  const columns: Array<TableColumn<ProtocolStat>> = [
    {
      key: 'name',
      title: '应用 / 协议',
      render: (item) => (
        <span className="protocol-name-cell">
          <strong>{item.name}</strong>
          {item.service && item.service !== item.name ? <small className="faint">{item.service}</small> : null}
        </span>
      ),
    },
    { key: 'kind', title: '传输协议', render: (item) => kindBadge(item.kind) },
    { key: 'connections', title: '连接数', numeric: true, render: (item) => <span className="num">{formatCount(item.connections)}</span> },
    {
      key: 'rates',
      title: '实时 ↓ / ↑',
      numeric: true,
      render: (item) => (
        <span className="protocol-rates num">
          <span className="protocol-rate-down">↓ {formatBitRate(item.downloadBps)}</span>
          <span className="protocol-rate-up">↑ {formatBitRate(item.uploadBps)}</span>
        </span>
      ),
    },
    {
      key: 'bytes',
      title: '累计 ↓ / ↑',
      numeric: true,
      render: (item) => (
        <span className="protocol-rates num">
          <span>↓ {formatBytes(item.downloadBytes)}</span>
          <span>↑ {formatBytes(item.uploadBytes)}</span>
        </span>
      ),
    },
    {
      key: 'share',
      title: '占比',
      width: '180px',
      render: (item) => {
        const share = totalBytes > 0 ? ((item.uploadBytes + item.downloadBytes) / totalBytes) * 100 : 0
        return (
          <span className="protocol-share" title={`占当前活动流量 ${share.toFixed(1)}%`}>
            <span className="mon-meter protocol-share-track">
              <span className="mon-meter-fill" style={{ width: `${Math.min(100, share)}%` }} />
            </span>
            <span className="num protocol-share-value">{share.toFixed(1)}%</span>
          </span>
        )
      },
    },
    { key: 'source', title: '识别方式', render: (item) => sourceBadge(item) },
  ]

  return (
    <div className="page protocols-page">
      <header className="page-head">
        <h1>协议统计</h1>
        <span className="page-sub">按应用与协议分类的连接数、速率与占比（30 秒刷新）</span>
      </header>

      {error && data ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      {loading && !data ? (
        <Card>
          <Skeleton lines={6} height={16} />
        </Card>
      ) : error && !data ? (
        <Card>
          <EmptyState icon="⚠️" title="协议统计读取失败" description={error} actionLabel="重试" onAction={reload} />
        </Card>
      ) : data && !data.enabled ? (
        <Card>
          <EmptyState
            icon="🧭"
            title="未开启协议分析"
            description="协议统计依赖协议分析能力：在「识别设置」中为该设备开启协议分析后，这里会按应用展示连接、速率与占比。"
            actionLabel="前往识别设置"
            onAction={() => navigate('recognition')}
          />
        </Card>
      ) : (
        <>
          <div className="mon-summary-grid">
            <Card className="mon-summary-card">
              <span className="mon-summary-label">统计中的协议 / 应用</span>
              <strong className="mon-summary-value num">{formatCount(protocols.length)}</strong>
              <span className="mon-summary-sub faint">活动连接 {formatCount(totalConnections)}</span>
            </Card>
            <Card className="mon-summary-card">
              <span className="mon-summary-label">已识别连接占比</span>
              <strong className="mon-summary-value num">{totalConnections ? formatPercent((identifiedConnections / totalConnections) * 100) : '-'}</strong>
              <span className="mon-summary-sub faint">{`${formatCount(identifiedConnections)} / ${formatCount(totalConnections)} 条连接非端口估算`}</span>
            </Card>
            <Card className="mon-summary-card">
              <span className="mon-summary-label">流量最高应用</span>
              <strong className="mon-summary-value mon-summary-value-text">{top ? top.name : '-'}</strong>
              <span className="mon-summary-sub faint num">{top ? `${formatBytes(top.uploadBytes + top.downloadBytes)} · ${formatCount(top.connections)} 连接` : '暂无数据'}</span>
            </Card>
          </div>
          <Card className="protocol-table-card">
            <DataTable
              ariaLabel="协议统计"
              columns={columns}
              rows={protocols}
              rowKey={(item) => `${item.applicationId ?? item.service ?? item.name}-${item.kind}`}
              emptyTitle="当前没有可统计的活动连接"
              emptyDescription="等设备产生活动连接后，这里会按应用分类展示统计。"
            />
          </Card>
        </>
      )}
    </div>
  )
}
