import { useMemo, useState } from 'react'
import { formatDuration } from '../lib/format'
import type { DHCPLeaseStat, DHCPPoolStat, DHCPServerStat } from '../lib/types'
import { useShell } from '../shell/useShell'
import { Badge, Card, DataTable, EmptyState, SearchInput, Skeleton, type TableColumn } from '../ui'
import { fetchDhcp } from '../features/monitor-detail/api'
import { useMonitorResource } from '../features/monitor-detail/hooks'
import './monitor-common.css'
import './dhcp.css'

function poolForServer(server: DHCPServerStat, pools: DHCPPoolStat[]): DHCPPoolStat | undefined {
  return pools.find((pool) => pool.name === server.addressPool || pool.servers.includes(server.name))
}

function leaseStatus(lease: DHCPLeaseStat): { tone: 'ok' | 'warn' | 'err' | 'neutral'; label: string } {
  if (lease.blocked) return { tone: 'err', label: '已阻止' }
  if (lease.disabled) return { tone: 'neutral', label: '已禁用' }
  if (lease.status === 'bound') return { tone: 'ok', label: '已绑定' }
  if (lease.status === 'waiting') return { tone: 'warn', label: '等待中' }
  return { tone: 'neutral', label: lease.status || '-' }
}

export default function DhcpPage() {
  const { scopedPath, selectedDeviceId, refreshMs, reloadNonce } = useShell()
  const { data: dhcp, loading, error, reload } = useMonitorResource(() => fetchDhcp(scopedPath), refreshMs, [selectedDeviceId, reloadNonce])
  const [query, setQuery] = useState('')

  const leases = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    if (!keyword) return dhcp?.leases ?? []
    return (dhcp?.leases ?? []).filter((lease) => [lease.address, lease.macAddress, lease.hostName, lease.comment].join(' ').toLowerCase().includes(keyword))
  }, [dhcp, query])

  const columns: Array<TableColumn<DHCPLeaseStat>> = [
    { key: 'address', title: '地址', render: (lease) => <span className="num">{lease.address || '-'}</span> },
    { key: 'mac', title: 'MAC', render: (lease) => <span className="num">{lease.macAddress || '-'}</span> },
    { key: 'hostName', title: '主机名', render: (lease) => lease.hostName || '-' },
    {
      key: 'comment',
      title: '备注',
      render: (lease) => (
        <span className="dhcp-comment" title={lease.comment || undefined}>
          {lease.comment || '-'}
        </span>
      ),
    },
    { key: 'server', title: 'Server', render: (lease) => lease.server || '-' },
    {
      key: 'status',
      title: '状态',
      render: (lease) => {
        const status = leaseStatus(lease)
        return <Badge tone={status.tone} dot={status.tone === 'ok' || status.tone === 'err'}>{status.label}</Badge>
      },
    },
    {
      key: 'expires',
      title: '剩余到期',
      numeric: true,
      render: (lease) => <span className="num">{lease.expiresAfter > 0 ? formatDuration(lease.expiresAfter) : '-'}</span>,
    },
    {
      key: 'lastSeen',
      title: '最后活跃',
      numeric: true,
      render: (lease) => <span className="num">{lease.lastSeen > 0 ? `${formatDuration(lease.lastSeen)}前` : '刚刚'}</span>,
    },
    {
      key: 'kind',
      title: '类型',
      render: (lease) => (
        <span className="dhcp-kind-cell">
          <Badge tone={lease.dynamic ? 'neutral' : 'accent'}>{lease.dynamic ? '动态' : '静态'}</Badge>
          {lease.blocked ? <Badge tone="err">已阻止</Badge> : null}
        </span>
      ),
    },
  ]

  const hasAny = (dhcp?.servers.length ?? 0) > 0 || (dhcp?.leases.length ?? 0) > 0

  return (
    <div className="page dhcp-page">
      <header className="page-head">
        <h1>DHCP</h1>
        <span className="page-sub">DHCP 服务、地址池使用率与租约明细</span>
      </header>

      {error && dhcp ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      {loading && !dhcp ? (
        <Card>
          <Skeleton lines={6} height={16} />
        </Card>
      ) : error && !dhcp ? (
        <Card>
          <EmptyState icon="⚠️" title="DHCP 数据读取失败" description={error} actionLabel="重试" onAction={reload} />
        </Card>
      ) : !hasAny ? (
        <Card>
          <EmptyState icon="🧾" title="未启用 DHCP Server 或接口无权限" description="当 RouterOS 上配置了 DHCP Server 并产生租约后，这里会展示地址池使用率与租约明细。" />
        </Card>
      ) : dhcp ? (
        <>
          <section>
            <div className="section-head">
              <h2>DHCP Server</h2>
              <span className="section-sub">{dhcp.servers.length} 个</span>
            </div>
            <div className="dhcp-server-grid" role="list" aria-label="DHCP Server 列表">
              {dhcp.servers.length === 0 ? (
                <Card className="dhcp-server-empty">
                  <span className="faint">没有 DHCP Server 配置</span>
                </Card>
              ) : (
                dhcp.servers.map((server) => {
                  const pool = poolForServer(server, dhcp.pools)
                  const usage = pool ? Math.min(100, Math.max(0, pool.usedPercent)) : 0
                  return (
                    <Card key={server.name} className="dhcp-server-card">
                      <div className="dhcp-server-head">
                        <strong>{server.name}</strong>
                        {server.disabled ? <Badge tone="neutral">已禁用</Badge> : server.invalid ? <Badge tone="err">配置无效</Badge> : <Badge tone="ok" dot>运行中</Badge>}
                      </div>
                      <div className="mon-kv-stack">
                        <div className="kv">
                          <span>接口</span>
                          <b>{server.interface || '-'}</b>
                        </div>
                        <div className="kv">
                          <span>地址池</span>
                          <b className="num">{server.addressPool || '-'}</b>
                        </div>
                        <div className="kv">
                          <span>IP 范围</span>
                          <b className="num">{pool?.ranges || '-'}</b>
                        </div>
                        <div className="kv">
                          <span>Lease 时长</span>
                          <b>{server.leaseTime || '-'}</b>
                        </div>
                      </div>
                      <div className="dhcp-usage">
                        <div className="dhcp-usage-label">
                          <span className="faint">地址使用</span>
                          <span className="num">{pool ? `${pool.used} / ${pool.total || '-'} · ${usage.toFixed(1)}%` : '-'}</span>
                        </div>
                        <span className="mon-meter" role="img" aria-label={`地址池已使用 ${usage.toFixed(1)}%`}>
                          <span className={`mon-meter-fill${usage >= 95 ? ' mon-meter-fill-err' : usage >= 85 ? ' mon-meter-fill-warn' : ''}`} style={{ width: `${usage}%` }} />
                        </span>
                      </div>
                    </Card>
                  )
                })
              )}
            </div>
          </section>

          <section>
            <div className="section-head">
              <h2>DHCP 租约</h2>
              <span className="section-sub num">
                显示 {leases.length} / {dhcp.leases.length} 条
              </span>
            </div>
            <div className="mon-toolbar dhcp-lease-toolbar">
              <SearchInput value={query} onChange={setQuery} placeholder="地址 / MAC / 主机名 / 备注" ariaLabel="搜索租约" width={280} />
            </div>
            <Card className="dhcp-lease-card">
              <DataTable
                ariaLabel="DHCP 租约"
                columns={columns}
                rows={leases}
                rowKey={(lease) => lease.id || `${lease.address}-${lease.macAddress}`}
                emptyTitle={dhcp.leases.length ? '没有匹配搜索条件的租约' : '当前没有租约'}
                emptyDescription={dhcp.leases.length ? '调整搜索关键词后再试。' : '等设备获取 DHCP 租约后会出现在这里。'}
              />
            </Card>
          </section>
        </>
      ) : null}
    </div>
  )
}
