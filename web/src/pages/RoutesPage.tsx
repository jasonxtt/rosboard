import { useEffect, useMemo, useState } from 'react'
import { formatCount } from '../lib/format'
import type { RouteStat } from '../lib/types'
import { useShell } from '../shell/useShell'
import { Badge, Card, DataTable, EmptyState, SegTabs, Skeleton, StatusDot, Toggle, type TableColumn } from '../ui'
import { fetchRoutes } from '../features/monitor-detail/api'
import { useMonitorResource } from '../features/monitor-detail/hooks'
import './monitor-common.css'
import './routes.css'

type RouteTab = 'rules' | 'routes'

const ROUTES_HASH_RE = /^#\/routes(?:\/([a-z-]+))?$/

/** Hash-routed tab: `#/routes` = 路由规则（默认）, `#/routes/tables` = 路由表. */
function routesTabFromHash(): RouteTab {
  return ROUTES_HASH_RE.exec(window.location.hash)?.[1] === 'tables' ? 'routes' : 'rules'
}

const TAB_OPTIONS: Array<{ value: RouteTab; label: string }> = [
  { value: 'rules', label: '路由规则' },
  { value: 'routes', label: '路由表' },
]

function protocolBadge(protocol: string) {
  if (protocol === 'static') return <Badge tone="accent">静态</Badge>
  if (protocol === 'connected') return <Badge tone="ok">直连</Badge>
  if (protocol === 'dynamic') return <Badge tone="neutral">动态</Badge>
  return <Badge tone="neutral">{protocol || '-'}</Badge>
}

function familyBadge(family: string) {
  return <Badge tone={family === 'ipv6' ? 'accent' : 'neutral'}>{family === 'ipv6' ? 'IPv6' : 'IPv4'}</Badge>
}

function matchBadge(matches: number) {
  return matches > 0 ? <Badge tone="accent">{formatCount(matches)} 命中</Badge> : <span className="faint num">0</span>
}

function routeState(item: RouteStat) {
  if (item.disabled) return <Badge tone="neutral">已禁用</Badge>
  return (
    <span className="route-active-cell">
      <StatusDot tone={item.active ? 'ok' : 'neutral'} />
      {item.active ? '活动' : '非活动'}
    </span>
  )
}

const RULE_COLUMNS: Array<TableColumn<RouteStat>> = [
  { key: 'family', title: '协议族', render: (item) => familyBadge(item.family) },
  { key: 'source', title: '源地址 / 接口', render: (item) => <span className="num">{item.source || '-'}</span> },
  { key: 'destination', title: '目标网段', render: (item) => <span className="num">{item.destination || '-'}</span> },
  { key: 'table', title: '路由表', render: (item) => <Badge tone="accent">{item.table || 'main'}</Badge> },
  { key: 'action', title: '动作', render: (item) => item.action || '-' },
  { key: 'matches', title: '命中连接', numeric: true, render: (item) => matchBadge(item.currentMatches) },
  {
    key: 'comment',
    title: '备注',
    render: (item) => (
      <span className="route-comment" title={item.comment || undefined}>
        {item.comment || '-'}
      </span>
    ),
  },
  { key: 'state', title: '状态', render: (item) => (item.disabled ? <Badge tone="neutral">已禁用</Badge> : <Badge tone="ok" dot>生效中</Badge>) },
]

const ROUTE_COLUMNS: Array<TableColumn<RouteStat>> = [
  { key: 'family', title: '协议族', render: (item) => familyBadge(item.family) },
  { key: 'destination', title: '目标网段', render: (item) => <span className="num">{item.destination || '-'}</span> },
  {
    key: 'gateway',
    title: '网关',
    render: (item) => (
      <span className="route-gateway-cell">
        <span className="num">{item.gateway || '-'}</span>
        {item.immediateGateway && item.immediateGateway !== item.gateway ? <small className="faint num">下一跳 {item.immediateGateway}</small> : null}
      </span>
    ),
  },
  { key: 'prefSrc', title: 'pref-src', render: (item) => <span className="num">{item.prefSrc || '-'}</span> },
  { key: 'protocol', title: '来源', render: (item) => protocolBadge(item.protocol) },
  { key: 'distance', title: '距离', numeric: true, render: (item) => <span className="num">{item.distance}</span> },
  { key: 'matches', title: '命中连接', numeric: true, render: (item) => matchBadge(item.currentMatches) },
  {
    key: 'comment',
    title: '备注',
    render: (item) => (
      <span className="route-comment" title={item.comment || undefined}>
        {item.comment || '-'}
      </span>
    ),
  },
  { key: 'state', title: '状态', render: (item) => routeState(item) },
]

export default function RoutesPage() {
  const { scopedPath, selectedDeviceId, refreshMs, reloadNonce } = useShell()
  const { data: routes, loading, error, reload } = useMonitorResource(() => fetchRoutes(scopedPath), refreshMs, [selectedDeviceId, reloadNonce])
  const [tab, setTab] = useState<RouteTab>(() => routesTabFromHash())
  const [hideDisabled, setHideDisabled] = useState(true)
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())

  // Browser back/forward and direct hash edits drive the tab too.
  useEffect(() => {
    const sync = () => setTab(routesTabFromHash())
    window.addEventListener('popstate', sync)
    window.addEventListener('hashchange', sync)
    return () => {
      window.removeEventListener('popstate', sync)
      window.removeEventListener('hashchange', sync)
    }
  }, [])

  const selectTab = (next: RouteTab) => {
    setTab(next)
    window.history.pushState(null, '', next === 'routes' ? '#/routes/tables' : '#/routes')
  }

  const all = routes ?? []
  const disabledCount = all.filter((item) => item.disabled).length
  const visible = hideDisabled ? all.filter((item) => !item.disabled) : all
  const rules = useMemo(() => visible.filter((item) => item.kind === 'rule'), [visible])
  const routeItems = useMemo(() => visible.filter((item) => item.kind !== 'rule'), [visible])

  const groups = useMemo(() => {
    const byTable = new Map<string, RouteStat[]>()
    for (const item of routeItems) {
      const table = item.table || 'main'
      const list = byTable.get(table) ?? []
      list.push(item)
      byTable.set(table, list)
    }
    return [...byTable.entries()].sort((a, b) => (a[0] === 'main' ? -1 : b[0] === 'main' ? 1 : a[0].localeCompare(b[0])))
  }, [routeItems])

  const toggleGroup = (table: string) => {
    setCollapsed((current) => {
      const next = new Set(current)
      if (next.has(table)) next.delete(table)
      else next.add(table)
      return next
    })
  }

  const isDefaultRoute = (item: RouteStat) => item.destination === '0.0.0.0/0' || item.destination === '::/0'
  const hasAny = all.length > 0
  const currentRowsEmpty = tab === 'rules' ? rules.length === 0 : routeItems.length === 0

  return (
    <div className="page routes-page">
      <header className="page-head">
        <h1>路由 / 分流</h1>
        <span className="page-sub">路由规则与路由表条目，命中数为当前 conntrack 快照推算</span>
      </header>

      <div className="mon-toolbar routes-toolbar">
        <SegTabs options={TAB_OPTIONS} value={tab} onChange={selectTab} ariaLabel="路由视图" />
        <label className="routes-hide-disabled">
          <Toggle checked={hideDisabled} onChange={setHideDisabled} label="隐藏已禁用" />
          <span>隐藏已禁用</span>
        </label>
        <span className="faint num routes-count">
          显示 {visible.length} / {all.length} 条{disabledCount ? `，已禁用 ${disabledCount}` : ''}
        </span>
      </div>

      {error && routes ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      {loading && !routes ? (
        <Card>
          <Skeleton lines={6} height={16} />
        </Card>
      ) : error && !routes ? (
        <Card>
          <EmptyState icon="⚠️" title="路由数据读取失败" description={error} actionLabel="重试" onAction={reload} />
        </Card>
      ) : !hasAny ? (
        <Card>
          <EmptyState icon="🛣️" title="当前没有可读取的路由或分流状态" description="面板尚未从 RouterOS 采集到路由规则或路由表数据。" />
        </Card>
      ) : currentRowsEmpty ? (
        <Card>
          <EmptyState
            icon="🛣️"
            title={hideDisabled ? '已隐藏全部禁用条目' : tab === 'rules' ? '当前没有路由规则' : '当前没有路由表条目'}
            description={hideDisabled ? '关闭「隐藏已禁用」可查看全部条目。' : 'RouterOS 未报告该类型的条目。'}
          />
        </Card>
      ) : tab === 'rules' ? (
        <Card className="routes-table-card">
          <DataTable ariaLabel="路由规则" columns={RULE_COLUMNS} rows={rules} rowKey={(item) => item.id || `${item.table}-${item.source}-${item.destination}`} />
        </Card>
      ) : (
        groups.map(([table, items]) => {
          const defaultRoute = items.find((item) => isDefaultRoute(item) && !item.disabled)
          const matchTotal = items.reduce((sum, item) => sum + item.currentMatches, 0)
          const isCollapsed = collapsed.has(table)
          return (
            <Card key={table} className="routes-group-card">
              <button type="button" className="routes-group-head" aria-expanded={!isCollapsed} onClick={() => toggleGroup(table)}>
                <span className={`routes-group-chevron${isCollapsed ? ' routes-group-chevron-collapsed' : ''}`} aria-hidden="true">
                  ▾
                </span>
                <strong>路由表 {table}</strong>
                <span className="faint num">
                  {items.length} 条 · 命中连接 {formatCount(matchTotal)} ·{' '}
                  {defaultRoute ? `默认路由${defaultRoute.active ? '活动' : '非活动'}（${defaultRoute.gateway || '-'}）` : '无默认路由'}
                </span>
              </button>
              {isCollapsed ? null : (
                <DataTable ariaLabel={`路由表 ${table}`} columns={ROUTE_COLUMNS} rows={items} rowKey={(item) => item.id || `${item.destination}-${item.gateway}-${item.table}`} />
              )}
            </Card>
          )
        })
      )}
    </div>
  )
}
