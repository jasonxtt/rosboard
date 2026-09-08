import { useEffect, useMemo, useState } from 'react'
import { TrafficChart } from '../charts/TrafficChart'
import { formatBitRate, formatBytes } from '../lib/format'
import type { InterfaceCategory, InterfaceStatus } from '../lib/types'
import { useShell } from '../shell/useShell'
import { Badge, Button, Card, DataTable, EmptyState, SegTabs, Skeleton, type TableColumn } from '../ui'
import { fetchInterfaceDetail, fetchInterfaces } from '../features/monitor-detail/api'
import { useMonitorResource, useSortState } from '../features/monitor-detail/hooks'
import { SortHeader } from '../features/monitor-detail/SortHeader'
import './monitor-common.css'
import './interfaces.css'

type InterfaceSortKey = 'name' | 'type' | 'state' | 'rx' | 'tx' | 'rxBytes' | 'txBytes' | 'mtu' | 'linkDowns' | 'errors'

type CategoryFilter = 'all' | InterfaceCategory

const CATEGORY_OPTIONS: Array<{ value: CategoryFilter; label: string }> = [
  { value: 'all', label: '全部接口' },
  { value: 'physical', label: '物理接口' },
  { value: 'logical', label: '逻辑接口' },
  { value: 'system', label: '系统接口' },
]

/** 全部 tab 的分组顺序：物理 → 逻辑 → 系统。 */
const CATEGORY_RANK: Record<InterfaceCategory, number> = { physical: 0, logical: 1, system: 2 }

const RELATION_LABELS: Record<string, string> = {
  carrier: '承载',
  parent: '父接口',
  bridge: 'Bridge',
  member: '成员',
}

function stateRank(item: InterfaceStatus): number {
  return item.disabled ? 2 : item.running ? 1 : 0
}

function stateBadge(item: InterfaceStatus) {
  if (item.disabled) return <Badge tone="neutral">已禁用</Badge>
  if (item.running) {
    return (
      <Badge tone="ok" dot>
        {item.category === 'physical' ? '在线' : '运行中'}
      </Badge>
    )
  }
  return (
    <Badge tone="err" dot>
      Down
    </Badge>
  )
}

function compareInterface(left: InterfaceStatus, right: InterfaceStatus, key: InterfaceSortKey): number {
  const text = (a: string, b: string) => a.localeCompare(b, 'zh-CN', { numeric: true, sensitivity: 'base' })
  switch (key) {
    case 'name':
      return text(left.name, right.name)
    case 'type':
      return text(left.type, right.type)
    case 'state':
      return stateRank(left) - stateRank(right) || text(left.name, right.name)
    case 'rx':
      return left.currentRxBps - right.currentRxBps
    case 'tx':
      return left.currentTxBps - right.currentTxBps
    case 'rxBytes':
      return left.rxBytes - right.rxBytes
    case 'txBytes':
      return left.txBytes - right.txBytes
    case 'mtu':
      return left.actualMtu - right.actualMtu
    case 'linkDowns':
      return left.linkDowns - right.linkDowns
    case 'errors':
      return left.rxErrors + left.txErrors + left.rxDrops + left.txDrops - (right.rxErrors + right.txErrors + right.rxDrops + right.txDrops)
  }
}

function DetailSummaryItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="detail-summary-item">
      <span>{label}</span>
      <strong className="num">{value}</strong>
    </div>
  )
}

function InterfaceDetailPanel({ name, onClose }: { name: string; onClose: () => void }) {
  const { scopedPath, selectedDeviceId } = useShell()
  // 5s fixed poll while the panel is open (interface detail cadence).
  const { data: detail, loading, error } = useMonitorResource(() => fetchInterfaceDetail(scopedPath, name), 5000, [selectedDeviceId, name])

  return (
    <Card
      className="interface-detail-card"
      title={`${name} · 接口详情`}
      sub="近 1 小时速率趋势，每 5 秒刷新"
      actions={
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="关闭接口详情">
          关闭 ✕
        </Button>
      }
    >
      {loading && !detail ? (
        <Skeleton lines={4} height={14} />
      ) : error && !detail ? (
        <p className="mon-error-note">{error}</p>
      ) : detail ? (
        <>
          <div className="detail-summary-grid">
            <DetailSummaryItem label="状态" value={detail.interface.disabled ? '已禁用' : detail.interface.running ? '运行中' : 'Down'} />
            <DetailSummaryItem label="协商速率" value={detail.interface.linkRate ? `${detail.interface.linkRate}${detail.interface.fullDuplex ? ' / 全双工' : ''}` : '-'} />
            <DetailSummaryItem label="最后连接时间" value={detail.interface.lastLinkUpTime || '-'} />
            <DetailSummaryItem label="MAC" value={detail.interface.macAddress || '-'} />
            <DetailSummaryItem label="地址" value={detail.interface.addresses.join(' / ') || '-'} />
            <DetailSummaryItem label="当前 ↓ / ↑" value={`${formatBitRate(detail.interface.currentRxBps)} / ${formatBitRate(detail.interface.currentTxBps)}`} />
            <DetailSummaryItem label="收 / 发包" value={`${detail.interface.rxPackets.toLocaleString('zh-CN')} / ${detail.interface.txPackets.toLocaleString('zh-CN')}`} />
            <DetailSummaryItem
              label="错误 / 丢包"
              value={`${detail.interface.rxErrors + detail.interface.txErrors} / ${detail.interface.rxDrops + detail.interface.txDrops}`}
            />
          </div>
          <TrafficChart samples={detail.samples} window="1h" height={220} ariaLabel={`${name} 接口上传和下载速率趋势`} />
        </>
      ) : null}
    </Card>
  )
}

export default function InterfacesPage() {
  const { scopedPath, selectedDeviceId, refreshMs, reloadNonce } = useShell()
  const { data: interfaces, loading, error, reload } = useMonitorResource(() => fetchInterfaces(scopedPath), refreshMs, [selectedDeviceId, reloadNonce])
  const [category, setCategory] = useState<CategoryFilter>('all')
  // physical 默认 Down 优先；逻辑/系统按名称。
  const sort = useSortState<InterfaceSortKey>('state')
  const [expanded, setExpanded] = useState<string | null>(null)

  useEffect(() => {
    setExpanded(null)
    // 物理接口默认 Down 优先，其余按名称（component-guidelines）。
    sort.reset(category === 'physical' || category === 'all' ? 'state' : 'name')
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 切换分类时重置排序与展开态
  }, [category])

  const items = useMemo(() => {
    const scoped = (interfaces ?? []).filter((item) => category === 'all' || item.category === category)
    const direction = sort.direction === 'asc' ? 1 : -1
    return [...scoped].sort((left, right) => {
      if (category === 'all') {
        const rankDelta = (CATEGORY_RANK[left.category] ?? 9) - (CATEGORY_RANK[right.category] ?? 9)
        if (rankDelta !== 0) return rankDelta
      }
      return compareInterface(left, right, sort.key) * direction
    })
  }, [interfaces, category, sort.key, sort.direction])

  const sortHeader = (label: string, key: InterfaceSortKey) => (
    <SortHeader label={label} sortKey={key} activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
  )

  const columns: Array<TableColumn<InterfaceStatus>> = [
    {
      key: 'name',
      title: sortHeader('名称', 'name'),
      render: (item) => (
        <span className="interface-name-cell">
          <strong>{item.name}</strong>
          {item.relations.length ? (
            <span className="relation-chips">
              {item.relations.map((relation) => (
                <span key={`${relation.kind}-${relation.interface}`} className="relation-chip" title={`${RELATION_LABELS[relation.kind] ?? relation.kind}：${relation.interface}`}>
                  {RELATION_LABELS[relation.kind] ?? relation.kind} · {relation.interface}
                </span>
              ))}
            </span>
          ) : null}
        </span>
      ),
    },
    { key: 'type', title: sortHeader('类型', 'type'), render: (item) => item.type || '-' },
    {
      key: 'address',
      title: '地址 / MAC',
      render: (item) => (
        <span className="interface-address-cell">
          <span className="num">{item.addresses.join(' / ') || '-'}</span>
          <small className="faint num">{item.macAddress || '-'}</small>
        </span>
      ),
    },
    { key: 'state', title: sortHeader('状态', 'state'), render: (item) => stateBadge(item) },
    {
      key: 'rates',
      title: (
        <span className="interface-rate-head">
          <SortHeader label="实时 ↓" sortKey="rx" activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
          <SortHeader label="↑" sortKey="tx" activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
        </span>
      ),
      numeric: true,
      render: (item) => (
        <span className="interface-rates num">
          <span className="interface-rate-down">↓ {formatBitRate(item.currentRxBps)}</span>
          <span className="interface-rate-up">↑ {formatBitRate(item.currentTxBps)}</span>
        </span>
      ),
    },
    {
      key: 'totals',
      title: (
        <span className="interface-rate-head">
          <SortHeader label="累计 ↓" sortKey="rxBytes" activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
          <SortHeader label="↑" sortKey="txBytes" activeKey={sort.key} direction={sort.direction} onSort={sort.toggle} />
        </span>
      ),
      numeric: true,
      render: (item) => (
        <span className="interface-rates num">
          <span>↓ {formatBytes(item.rxBytes)}</span>
          <span>↑ {formatBytes(item.txBytes)}</span>
        </span>
      ),
    },
    { key: 'mtu', title: sortHeader('MTU', 'mtu'), numeric: true, render: (item) => <span className="num">{item.actualMtu || '-'}</span> },
    { key: 'linkDowns', title: sortHeader('断链次数', 'linkDowns'), numeric: true, render: (item) => <span className="num">{item.linkDowns}</span> },
    {
      key: 'errors',
      title: sortHeader('错误 / 丢包', 'errors'),
      numeric: true,
      render: (item) => {
        const errors = item.rxErrors + item.txErrors
        const drops = item.rxDrops + item.txDrops
        return (
          <span className={`num ${errors + drops > 0 ? 'interface-errors-hot' : ''}`} title={`错误 ${errors} · 丢包 ${drops}`}>
            {errors} / {drops}
          </span>
        )
      },
    },
  ]

  return (
    <div className="page interfaces-page">
      <header className="page-head">
        <h1>接口监控</h1>
        <span className="page-sub">链路状态、实时速率与错误丢包统计</span>
      </header>

      <div className="mon-toolbar interfaces-toolbar">
        <SegTabs options={CATEGORY_OPTIONS} value={category} onChange={setCategory} ariaLabel="接口类型" />
        <span className="faint toolbar-count num">共 {items.length} 个接口</span>
      </div>

      {error && interfaces ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      {expanded ? <InterfaceDetailPanel name={expanded} onClose={() => setExpanded(null)} /> : null}

      {loading && !interfaces ? (
        <Card>
          <Skeleton lines={6} height={16} />
        </Card>
      ) : error && !interfaces ? (
        <Card>
          <EmptyState icon="⚠️" title="接口列表读取失败" description={error} actionLabel="重试" onAction={reload} />
        </Card>
      ) : (
        <Card className="interface-table-card">
          <DataTable
            ariaLabel="接口列表"
            columns={columns}
            rows={items}
            rowKey={(item) => item.name}
            onRowClick={(item) => setExpanded((current) => (current === item.name ? null : item.name))}
            rowClassName={(item) =>
              item.disabled ? 'interface-row-disabled' : !item.running && item.category === 'physical' ? 'interface-row-down' : undefined
            }
            emptyTitle={category === 'all' ? '设备暂无接口数据' : `当前分类下没有${CATEGORY_OPTIONS.find((option) => option.value === category)?.label ?? '接口'}`}
            emptyDescription={category === 'all' ? '等待首次采集完成后展示接口状态。' : 'RouterOS 未报告该分类的接口，切换分类查看其他接口。'}
          />
        </Card>
      )}
    </div>
  )
}
