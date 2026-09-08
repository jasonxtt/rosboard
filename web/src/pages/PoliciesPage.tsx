import { useMemo } from 'react'
import { formatBytes, formatCount } from '../lib/format'
import type { PolicyStat } from '../lib/types'
import { useShell } from '../shell/useShell'
import { Badge, Card, DataTable, EmptyState, Skeleton, type TableColumn } from '../ui'
import { fetchPolicies } from '../features/monitor-detail/api'
import { useMonitorResource } from '../features/monitor-detail/hooks'
import './monitor-common.css'

const KIND_ORDER = ['simple queue', 'queue tree', 'mangle'] as const

const KIND_LABELS: Record<string, { title: string; sub: string }> = {
  'simple queue': { title: 'Simple Queue', sub: '简单队列的实时速率与累计计数' },
  'queue tree': { title: 'Queue Tree', sub: '队列树节点的速率与累计计数' },
  mangle: { title: 'Mangle 标记', sub: '带计数或路由/连接标记的 mangle 规则' },
}

const COLUMNS: Array<TableColumn<PolicyStat>> = [
  { key: 'name', title: '名称', render: (item) => <strong>{item.name || '-'}</strong> },
  { key: 'target', title: '目标 / 动作', render: (item) => <span className="num">{item.target || '-'}</span> },
  { key: 'mark', title: '标记', render: (item) => (item.mark ? <Badge tone="accent">{item.mark}</Badge> : '-') },
  { key: 'rate', title: '当前速率', numeric: true, render: (item) => <span className="num">{item.rate || '-'}</span> },
  { key: 'bytes', title: '累计流量', numeric: true, render: (item) => <span className="num">{formatBytes(item.bytes)}</span> },
  { key: 'packets', title: '包数', numeric: true, render: (item) => <span className="num">{formatCount(item.packets)}</span> },
  {
    key: 'state',
    title: '状态',
    render: (item) => (item.disabled ? <Badge tone="neutral">已禁用</Badge> : <Badge tone="ok" dot>生效中</Badge>),
  },
]

export default function PoliciesPage() {
  const { scopedPath, selectedDeviceId, refreshMs, reloadNonce } = useShell()
  const { data: policies, loading, error, reload } = useMonitorResource(() => fetchPolicies(scopedPath), refreshMs, [selectedDeviceId, reloadNonce])

  const groups = useMemo(() => {
    const byKind = new Map<string, PolicyStat[]>()
    for (const item of policies ?? []) {
      const list = byKind.get(item.kind) ?? []
      list.push(item)
      byKind.set(item.kind, list)
    }
    const ordered = [...byKind.entries()].sort((a, b) => {
      const rank = (kind: string) => {
        const index = KIND_ORDER.indexOf(kind as (typeof KIND_ORDER)[number])
        return index === -1 ? KIND_ORDER.length : index
      }
      return rank(a[0]) - rank(b[0]) || a[0].localeCompare(b[0])
    })
    return ordered
  }, [policies])

  return (
    <div className="page policies-page">
      <header className="page-head">
        <h1>策略统计</h1>
        <span className="page-sub">RouterOS 现有队列与标记规则的实时计数，只读展示，不创建或修改规则</span>
      </header>

      {error && policies ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      {loading && !policies ? (
        <Card>
          <Skeleton lines={6} height={16} />
        </Card>
      ) : error && !policies ? (
        <Card>
          <EmptyState icon="⚠️" title="策略统计读取失败" description={error} actionLabel="重试" onAction={reload} />
        </Card>
      ) : groups.length === 0 ? (
        <Card>
          <EmptyState
            icon="🧷"
            title="没有可展示的 RouterOS 策略计数器"
            description="当 RouterOS 上存在 simple queue、queue tree，或带计数 / 路由标记的 mangle 规则时，这里会展示它们的实时速率、累计流量与包数。本页只读，策略配置请前往「策略路由」。"
          />
        </Card>
      ) : (
        groups.map(([kind, items]) => {
          const meta = KIND_LABELS[kind] ?? { title: kind, sub: '' }
          return (
            <section key={kind} className="policy-group">
              <div className="section-head">
                <h2>{meta.title}</h2>
                <span className="section-sub">
                  {meta.sub}
                  {meta.sub ? ' · ' : ''}共 {items.length} 条
                </span>
              </div>
              <Card className="policy-group-card">
                <DataTable ariaLabel={`${meta.title} 策略计数`} columns={COLUMNS} rows={items} rowKey={(item) => `${item.kind}-${item.name}-${item.target}`} />
              </Card>
            </section>
          )
        })
      )}
    </div>
  )
}
