import { useMemo, useState, type ChangeEvent } from 'react'
import { formatBitRate, formatBytes } from '../../lib/format'
import { Badge, Tooltip } from '../../ui'
import { useSortState } from './hooks'
import { SortHeader } from './SortHeader'
import type { TerminalConnection } from './api'

type ConnectionSortKey = 'protocol' | 'application' | 'source' | 'destination' | 'upload' | 'download' | 'uploadBytes' | 'downloadBytes' | 'status' | 'route'

type ConnectionFilters = {
  protocol: string
  application: string
  source: string
  destination: string
  status: string
  route: string
}

const EMPTY_FILTERS: ConnectionFilters = { protocol: '', application: '', source: '', destination: '', status: '', route: '' }

function routeText(connection: TerminalConnection): string {
  return [connection.routeTable, connection.matchedRule, ...connection.egressInterfaces, ...connection.routeGateways].filter(Boolean).join(' ')
}

function stateLabel(connection: TerminalConnection): string {
  if (connection.assured) return '已确认'
  if (connection.seenReply) return '已回包'
  return '未回包'
}

function matches(connection: TerminalConnection, filters: ConnectionFilters): boolean {
  const includes = (haystack: string, needle: string) => haystack.toLowerCase().includes(needle.trim().toLowerCase())
  if (filters.protocol && !includes(connection.protocol, filters.protocol)) return false
  if (filters.application && !includes([connection.application, connection.service ?? '', connection.matchedDomain ?? ''].join(' '), filters.application)) return false
  if (filters.source && !includes(`${connection.sourceAddress} ${connection.sourcePort}`, filters.source)) return false
  if (filters.destination && !includes(`${connection.destinationAddress} ${connection.destinationPort}`, filters.destination)) return false
  if (filters.status && !includes(`${stateLabel(connection)} ${connection.status}`, filters.status)) return false
  if (filters.route && !includes(routeText(connection), filters.route)) return false
  return true
}

function compareConnection(left: TerminalConnection, right: TerminalConnection, key: ConnectionSortKey): number {
  const text = (a: string, b: string) => a.localeCompare(b, 'zh-CN', { numeric: true, sensitivity: 'base' })
  switch (key) {
    case 'protocol':
      return text(left.protocol, right.protocol)
    case 'application':
      return text(left.application || left.service || '', right.application || right.service || '')
    case 'source':
      return text(`${left.sourceAddress}:${left.sourcePort}`, `${right.sourceAddress}:${right.sourcePort}`)
    case 'destination':
      return text(`${left.destinationAddress}:${left.destinationPort}`, `${right.destinationAddress}:${right.destinationPort}`)
    case 'upload':
      return left.uploadBps - right.uploadBps
    case 'download':
      return left.downloadBps - right.downloadBps
    case 'uploadBytes':
      return left.uploadBytes - right.uploadBytes
    case 'downloadBytes':
      return left.downloadBytes - right.downloadBytes
    case 'status':
      return text(stateLabel(left), stateLabel(right))
    case 'route':
      return text(routeText(left), routeText(right))
  }
}

function endpoint(address: string, port: string): string {
  if (!address) return '-'
  return port ? `${address}:${port}` : address
}

type ConnectionTableProps = {
  connections: TerminalConnection[]
  emptyLabel?: string
}

/**
 * Terminal connection table: sortable headers with a per-column text filter
 * row (component-guidelines: filters stay column-local, no global toolbar).
 */
export function ConnectionTable({ connections, emptyLabel = '当前没有符合筛选条件的连接' }: ConnectionTableProps) {
  const sort = useSortState<ConnectionSortKey>('downloadBytes', 'desc')
  const [filters, setFilters] = useState<ConnectionFilters>(EMPTY_FILTERS)

  const rows = useMemo(() => {
    const filtered = filters === EMPTY_FILTERS ? connections : connections.filter((connection) => matches(connection, filters))
    const direction = sort.direction === 'asc' ? 1 : -1
    return [...filtered].sort((left, right) => compareConnection(left, right, sort.key) * direction)
  }, [connections, filters, sort.key, sort.direction])

  const setFilter = (key: keyof ConnectionFilters) => (event: ChangeEvent<HTMLInputElement>) =>
    setFilters((current) => ({ ...current, [key]: event.target.value }))

  const filterInput = (key: keyof ConnectionFilters, placeholder: string) => (
    <input
      className="connection-filter-input"
      value={filters[key]}
      placeholder={placeholder}
      aria-label={`筛选${placeholder}`}
      onChange={setFilter(key)}
    />
  )

  const header = (label: string, key: ConnectionSortKey) => (
    <th key={key} className={key === 'upload' || key === 'download' || key === 'uploadBytes' || key === 'downloadBytes' ? 'num' : undefined}>
      <SortHeader label={label} sortKey={key} activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
    </th>
  )

  return (
    <div className="table-scroll connection-table-scroll">
      <table className="table connection-table" aria-label="终端连接明细">
        <thead>
          <tr>
            {header('协议', 'protocol')}
            {header('应用', 'application')}
            {header('源 → 目的', 'destination')}
            {header('↑ 速率', 'upload')}
            {header('↓ 速率', 'download')}
            {header('↑ 累计', 'uploadBytes')}
            {header('↓ 累计', 'downloadBytes')}
            {header('状态', 'status')}
            {header('路由', 'route')}
          </tr>
          <tr className="connection-filter-row">
            <th>{filterInput('protocol', '协议')}</th>
            <th>{filterInput('application', '应用 / 域名')}</th>
            <th>
              <span className="connection-filter-pair">
                {filterInput('source', '来源地址/端口')}
                {filterInput('destination', '目的地址/端口')}
              </span>
            </th>
            <th colSpan={4} />
            <th>{filterInput('status', '状态')}</th>
            <th>{filterInput('route', '路由 / 规则 / 出口')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={9} className="connection-empty">
                {emptyLabel}
              </td>
            </tr>
          ) : (
            rows.map((connection, index) => (
              <tr key={connection.key || `${connection.sourceAddress}-${connection.destinationAddress}-${index}`}>
                <td>
                  <Badge tone={connection.protocol === 'tcp' ? 'accent' : 'neutral'}>{connection.protocol ? connection.protocol.toUpperCase() : '-'}</Badge>
                </td>
                <td>
                  <span className="connection-app">
                    <span>
                      {connection.application || connection.service || '-'}
                      {connection.estimated ? (
                        <Badge tone="warn" className="connection-estimated">
                          估算
                        </Badge>
                      ) : null}
                    </span>
                    {connection.matchedDomain ? <small className="faint">{connection.matchedDomain}</small> : null}
                  </span>
                </td>
                <td className="connection-endpoint">
                  <span className="num">{endpoint(connection.sourceAddress, connection.sourcePort)}</span>
                  <span className="connection-arrow" aria-hidden="true">
                    →
                  </span>
                  <span className="num">{endpoint(connection.destinationAddress, connection.destinationPort)}</span>
                </td>
                <td className="num">{formatBitRate(connection.uploadBps)}</td>
                <td className="num">{formatBitRate(connection.downloadBps)}</td>
                <td className="num">{formatBytes(connection.uploadBytes)}</td>
                <td className="num">{formatBytes(connection.downloadBytes)}</td>
                <td>
                  <Badge tone={connection.assured ? 'ok' : connection.seenReply ? 'accent' : 'neutral'} dot>
                    {stateLabel(connection)}
                  </Badge>
                </td>
                <td>
                  <Tooltip
                    tip={`规则 ${connection.matchedRule || '-'} · 网关 ${connection.routeGateways.join(' / ') || '-'} · 出口 ${connection.egressInterfaces.join(' / ') || '-'}`}
                  >
                    <span className="connection-route">
                      {connection.routeTable || '无法判断'}
                      {connection.matchedRule ? <small className="faint">{connection.matchedRule}</small> : null}
                    </span>
                  </Tooltip>
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  )
}
