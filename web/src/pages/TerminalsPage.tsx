import { useCallback, useEffect, useMemo, useState } from 'react'
import { formatBitRate, formatBytes, formatDuration } from '../lib/format'
import type { Terminal, TerminalFamily } from '../lib/types'
import { loadPanelPreferences } from '../features/settings/prefs'
import { useShell } from '../shell/useShell'
import { Badge, Card, DataTable, EmptyState, Pagination, SearchInput, SegTabs, Select, Skeleton, StatusDot, type TableColumn } from '../ui'
import { usePolling } from '../shell/usePolling'
import { fetchTerminals, postTerminalViewerHeartbeat } from '../features/monitor-detail/api'
import { useMonitorResource, useSortState } from '../features/monitor-detail/hooks'
import { SortHeader } from '../features/monitor-detail/SortHeader'
import {
  compareTerminal,
  onlineDurationSeconds,
  terminalMetrics,
  terminalPrimaryAddress,
  terminalStateText,
  terminalStateTone,
  type TerminalSortKey,
} from '../features/monitor-detail/utils'
import TerminalDetailPage from './TerminalDetailPage'
import './monitor-common.css'
import './terminals.css'

type VisibilityFilter = 'online' | 'all' | 'offline'

const FAMILY_OPTIONS: Array<{ value: TerminalFamily; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'ipv4', label: 'IPv4' },
  { value: 'ipv6', label: 'IPv6' },
]

const VISIBILITY_OPTIONS: Array<{ value: VisibilityFilter; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'online', label: '在线' },
  { value: 'offline', label: '离线' },
]

const PAGE_SIZE_OPTIONS = [
  { value: '10', label: '10 条/页' },
  { value: '20', label: '20 条/页' },
  { value: '50', label: '50 条/页' },
]

const TERMINAL_HASH_RE = /^#\/terminals\/([^/]+)/

function terminalIdFromHash(): string | null {
  const match = TERMINAL_HASH_RE.exec(window.location.hash)
  if (!match) return null
  try {
    return decodeURIComponent(match[1]) || null
  } catch {
    return null
  }
}

export default function TerminalsPage() {
  const { scopedPath, selectedDeviceId, refreshMs, reloadNonce } = useShell()
  const { data: terminals, loading, error, reload } = useMonitorResource(() => fetchTerminals(scopedPath), refreshMs, [selectedDeviceId, reloadNonce])

  const [family, setFamily] = useState<TerminalFamily>(() => loadPanelPreferences().terminalFamily)
  const [query, setQuery] = useState('')
  const [visibility, setVisibility] = useState<VisibilityFilter>('online')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const sort = useSortState<TerminalSortKey>('address')
  const [selectedId, setSelectedId] = useState<string | null>(() => terminalIdFromHash())

  // Terminal-detail deep link: opening pushes #/terminals/<id>, back pops it.
  useEffect(() => {
    const onPopState = () => setSelectedId(terminalIdFromHash())
    window.addEventListener('popstate', onPopState)
    window.addEventListener('hashchange', onPopState)
    return () => {
      window.removeEventListener('popstate', onPopState)
      window.removeEventListener('hashchange', onPopState)
    }
  }, [])

  const openTerminal = useCallback((id: string) => {
    setSelectedId(id)
    window.history.pushState(null, '', `#/terminals/${encodeURIComponent(id)}`)
  }, [])

  const closeTerminal = useCallback(() => {
    // Direct write: tab changes push history entries, so history.back() could
    // land on another tab of the same terminal instead of the list.
    setSelectedId(null)
    if (TERMINAL_HASH_RE.test(window.location.hash)) {
      window.history.pushState(null, '', '#/terminals')
    }
  }, [])

  // Keep the backend's fast terminal polling alive while the page is visible.
  usePolling(
    useCallback(() => {
      void postTerminalViewerHeartbeat(scopedPath).catch(() => undefined)
    }, [scopedPath]),
    10_000,
    [selectedDeviceId],
  )

  useEffect(() => setPage(1), [query, visibility, family, pageSize])

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    return (terminals ?? []).filter((terminal) => {
      if (visibility === 'online' && terminal.state !== 'online') return false
      if (visibility === 'offline' && terminal.state === 'online') return false
      if (family === 'ipv4' && terminal.ipv4.length === 0) return false
      if (family === 'ipv6' && terminal.ipv6.length === 0) return false
      if (!keyword) return true
      return [terminal.displayName, terminal.customName, terminal.remark, terminal.macAddress, terminal.primaryInterface, ...terminal.ipv4, ...terminal.ipv6]
        .join(' ')
        .toLowerCase()
        .includes(keyword)
    })
  }, [terminals, visibility, family, query])

  const sorted = useMemo(() => {
    const direction = sort.direction === 'asc' ? 1 : -1
    return [...filtered].sort((left, right) => compareTerminal(left, right, sort.key, family) * direction)
  }, [filtered, sort.key, sort.direction, family])

  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const currentPage = Math.min(page, totalPages)
  const rows = sorted.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  const sortHeader = (label: string, key: TerminalSortKey) => (
    <SortHeader label={label} sortKey={key} activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
  )

  const columns: Array<TableColumn<Terminal>> = [
    {
      key: 'device',
      title: sortHeader('名称', 'device'),
      render: (terminal) => (
        <span className="terminal-name-cell">
          <StatusDot tone={terminalStateTone(terminal.state)} />
          <span className="terminal-name-stack">
            <span className="terminal-display-name">
              {terminal.displayName || terminal.id}
              {terminal.customName ? (
                <span className="terminal-custom-flag" title={`自定义名称，自动识别为 ${terminal.autoName || '未知'}`}>
                  ✎
                </span>
              ) : null}
            </span>
            <small className="faint num">{terminal.macAddress || 'MAC 未知'}</small>
          </span>
        </span>
      ),
    },
    {
      key: 'address',
      title: sortHeader('IP 地址', 'address'),
      render: (terminal) => {
        const primary = terminalPrimaryAddress(terminal, family) || '-'
        const addressCount = family === 'ipv4' ? terminal.ipv4.length : family === 'ipv6' ? terminal.ipv6.length : terminal.ipv4.length + terminal.ipv6.length
        return (
          <span className="terminal-address-stack">
            <span className="num">{primary}</span>
            {family === 'all' && terminal.primaryIpv4 && terminal.primaryIpv6 ? <small className="faint num">{terminal.primaryIpv6}</small> : null}
            {addressCount > 1 ? <small className="faint">+{addressCount - 1} 个地址</small> : null}
          </span>
        )
      },
    },
    {
      key: 'connections',
      title: sortHeader('连接数', 'connections'),
      numeric: true,
      render: (terminal) => <span className="num">{terminalMetrics(terminal, family).connectionCount}</span>,
    },
    {
      key: 'rates',
      title: (
        <span className="terminal-rate-head">
          <SortHeader label="实时 ↓" sortKey="download" activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
          <SortHeader label="↑" sortKey="upload" activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
        </span>
      ),
      numeric: true,
      render: (terminal) => {
        const metrics = terminalMetrics(terminal, family)
        return (
          <span className="terminal-rates num">
            <span className="terminal-rate-down">↓ {formatBitRate(metrics.currentDownloadBps)}</span>
            <span className="terminal-rate-up">↑ {formatBitRate(metrics.currentUploadBps)}</span>
          </span>
        )
      },
    },
    {
      key: 'totals',
      title: (
        <span className="terminal-rate-head">
          <SortHeader label="累计 ↓" sortKey="totalDownload" activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
          <SortHeader label="↑" sortKey="totalUpload" activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
        </span>
      ),
      numeric: true,
      render: (terminal) => {
        const metrics = terminalMetrics(terminal, family)
        return (
          <span className="terminal-rates num">
            <span className="terminal-rate-down">↓ {formatBytes(metrics.totalDownloadBytes)}</span>
            <span className="terminal-rate-up">↑ {formatBytes(metrics.totalUploadBytes)}</span>
          </span>
        )
      },
    },
    {
      key: 'state',
      title: '状态',
      render: (terminal) => (
        <Badge tone={terminalStateTone(terminal.state)} dot>
          {terminalStateText(terminal.state)}
        </Badge>
      ),
    },
    {
      key: 'online',
      title: sortHeader('在线时长', 'online'),
      numeric: true,
      render: (terminal) => <span className="num">{terminal.state === 'online' ? formatDuration(onlineDurationSeconds(terminal.onlineSince)) : '-'}</span>,
    },
    {
      key: 'remark',
      title: sortHeader('备注', 'remark'),
      render: (terminal) => (
        <span className="terminal-remark" title={terminal.remark || undefined}>
          {terminal.remark || '-'}
        </span>
      ),
    },
  ]

  if (selectedId) {
    return (
      <TerminalDetailPage
        terminalId={selectedId}
        onBack={closeTerminal}
        onMetadataSaved={() => {
          reload()
        }}
      />
    )
  }

  return (
    <div className="page terminals-page">
      <header className="page-head">
        <h1>终端监控</h1>
        <span className="page-sub">局域网终端的在线状态、连接数与实时速率</span>
      </header>

      <div className="mon-toolbar terminals-toolbar">
        <SegTabs options={FAMILY_OPTIONS} value={family} onChange={setFamily} ariaLabel="终端地址族" />
        <SegTabs options={VISIBILITY_OPTIONS} value={visibility} onChange={setVisibility} ariaLabel="在线状态筛选" />
        <SearchInput value={query} onChange={setQuery} placeholder="搜索名称 / IP / MAC / 备注" ariaLabel="搜索终端" width={260} />
      </div>

      {error && terminals ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      {loading && !terminals ? (
        <Card>
          <Skeleton lines={6} height={16} />
        </Card>
      ) : error && !terminals ? (
        <Card>
          <EmptyState icon="⚠️" title="终端列表读取失败" description={error} actionLabel="重试" onAction={reload} />
        </Card>
      ) : (
        <Card className="terminal-list-card">
          <DataTable
            ariaLabel="终端列表"
            columns={columns}
            rows={rows}
            rowKey={(terminal) => terminal.id}
            onRowClick={(terminal) => openTerminal(terminal.id)}
            emptyTitle={visibility === 'online' ? '当前没有在线终端' : '没有符合条件的终端'}
            emptyDescription={visibility === 'online' ? '切换到「全部」可查看离线与未活跃终端。' : '调整搜索或筛选条件后再试。'}
          />
          <div className="terminal-list-footer">
            <Select value={String(pageSize)} onChange={(value) => setPageSize(Number(value))} options={PAGE_SIZE_OPTIONS} ariaLabel="每页条数" />
            <Pagination page={currentPage} pageSize={pageSize} total={sorted.length} onChange={setPage} />
          </div>
        </Card>
      )}
    </div>
  )
}
