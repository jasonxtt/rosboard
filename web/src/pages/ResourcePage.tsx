import { formatBytes, formatPercent, formatUptime } from '../lib/format'
import { useShell } from '../shell/useShell'
import { Badge, Card, DataTable, EmptyState, GaugeRing, Skeleton, type TableColumn } from '../ui'
import { fetchResourceOverview, type SystemResourceCPU, type SystemResourceHardware, type SystemResourceIRQ } from '../features/monitor-detail/api'
import { useMonitorResource } from '../features/monitor-detail/hooks'
import './monitor-common.css'
import './resource.css'

function parsePercentString(value: string): number | null {
  const parsed = Number.parseFloat(value)
  return value.trim() && Number.isFinite(parsed) ? parsed : null
}

type Health = { tone: 'ok' | 'warn' | 'err'; label: string }

function usageHealth(percent: number | null): Health {
  if (percent === null) return { tone: 'ok', label: '不可用' }
  if (percent >= 95) return { tone: 'err', label: '严重' }
  if (percent >= 85) return { tone: 'warn', label: '注意' }
  return { tone: 'ok', label: '正常' }
}

function HealthBadge({ percent }: { percent: number | null }) {
  const health = usageHealth(percent)
  return (
    <Badge tone={health.tone} dot>
      {health.label}
    </Badge>
  )
}

const CORE_COLUMNS: Array<TableColumn<SystemResourceCPU>> = [
  { key: 'cpu', title: '核心', render: (core, index) => `CPU ${core.cpu || index}` },
  {
    key: 'load',
    title: '总占用',
    width: '46%',
    render: (core) => {
      const percent = parsePercentString(core.load)
      return (
        <span className="resource-meter-cell">
          <span className="mon-meter resource-meter-track">
            <span
              className={`mon-meter-fill${percent !== null && percent >= 95 ? ' mon-meter-fill-err' : percent !== null && percent >= 85 ? ' mon-meter-fill-warn' : ''}`}
              style={{ width: `${Math.min(100, Math.max(0, percent ?? 0))}%` }}
            />
          </span>
          <span className="num resource-meter-value">{percent === null ? '-' : `${percent.toFixed(0)}%`}</span>
        </span>
      )
    },
  },
  {
    key: 'irq',
    title: 'IRQ',
    numeric: true,
    render: (core) => {
      const percent = parsePercentString(core.irq)
      return <span className="num">{percent === null ? '-' : `${percent.toFixed(0)}%`}</span>
    },
  },
  {
    key: 'disk',
    title: '磁盘',
    numeric: true,
    render: (core) => {
      const percent = parsePercentString(core.disk)
      return <span className="num">{percent === null ? '-' : `${percent.toFixed(0)}%`}</span>
    },
  },
]

const HARDWARE_COLUMNS: Array<TableColumn<SystemResourceHardware>> = [
  { key: 'name', title: '名称', render: (item) => item.name || '-' },
  { key: 'type', title: '类型', render: (item) => item.type || '-' },
  { key: 'vendor', title: '厂商', render: (item) => item.vendor || '-' },
  { key: 'location', title: '位置', render: (item) => item.location || '-' },
  { key: 'speed', title: '速度', render: (item) => <span className="num">{item.speed || '-'}</span> },
  { key: 'ports', title: '端口', render: (item) => item.ports || '-' },
  { key: 'irq', title: 'IRQ', numeric: true, render: (item) => <span className="num">{item.irq || '-'}</span> },
]

const IRQ_COLUMNS: Array<TableColumn<SystemResourceIRQ>> = [
  { key: 'irq', title: 'IRQ', render: (item) => <span className="num">{item.irq || '-'}</span> },
  { key: 'cpu', title: 'CPU', render: (item) => <span className="num">{item.cpu || '-'}</span> },
  { key: 'activeCpu', title: '活动 CPU', render: (item) => <span className="num">{item.activeCpu || '-'}</span> },
  {
    key: 'count',
    title: '次数',
    numeric: true,
    render: (item) => <span className="num">{Number.isFinite(Number(item.count)) && item.count.trim() ? Number(item.count).toLocaleString('zh-CN') : '-'}</span>,
  },
  {
    key: 'users',
    title: '用户',
    render: (item) => (
      <span className="resource-irq-users" title={item.users || undefined}>
        {item.users || '-'}
      </span>
    ),
  },
]

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="kv">
      <span>{label}</span>
      <b className="num">{value || '-'}</b>
    </div>
  )
}

export default function ResourcePage() {
  const { scopedPath, selectedDeviceId, refreshMs, reloadNonce } = useShell()
  const { data, loading, error, reload } = useMonitorResource(() => fetchResourceOverview(scopedPath), refreshMs, [selectedDeviceId, reloadNonce])

  const overview = data?.overview ?? null
  const resource = data?.resource ?? null

  return (
    <div className="page resource-page">
      <header className="page-head">
        <h1>资源监控</h1>
        <span className="page-sub">CPU、内存、存储与硬件资源明细</span>
        {overview ? (
          <Badge tone={overview.healthEnabled ? 'ok' : 'neutral'} dot={overview.healthEnabled}>
            {overview.healthEnabled ? '健康监控已开启' : '健康监控未开启'}
          </Badge>
        ) : null}
      </header>

      {error && data ? <p className="mon-error-note">{error}（展示的是最近一次成功数据）</p> : null}

      {loading && !data ? (
        <div className="resource-grid">
          <Card>
            <Skeleton lines={6} height={16} />
          </Card>
          <Card>
            <Skeleton lines={4} height={16} />
          </Card>
        </div>
      ) : error && !data ? (
        <Card>
          <EmptyState icon="⚠️" title="资源数据读取失败" description={error} actionLabel="重试" onAction={reload} />
        </Card>
      ) : overview && resource ? (
        <>
          <div className="resource-grid">
            <Card
              title="CPU"
              sub="总负载与逐核占用"
              className="resource-card-cpu"
              actions={<HealthBadge percent={overview.cpuLoadPercent} />}
            >
              <div className="resource-cpu-summary">
                <strong className="resource-primary num">{formatPercent(overview.cpuLoadPercent)}</strong>
                <div className="mon-kv-stack resource-cpu-kv">
                  <InfoRow label="CPU 型号" value={resource.cpu} />
                  <InfoRow label="核心数" value={resource.cpuCount} />
                  <InfoRow label="频率" value={resource.cpuFrequency} />
                </div>
              </div>
              <DataTable
                ariaLabel="逐核 CPU 占用"
                columns={CORE_COLUMNS}
                rows={resource.cpuCores}
                rowKey={(core) => `${core.cpu}-${core.irq}`}
                emptyTitle="暂无逐核数据"
              />
            </Card>

            <Card title="内存" sub="RouterOS system resource" actions={<HealthBadge percent={overview.memoryUsedPercent} />}>
              <div className="resource-gauge-row">
                <GaugeRing percent={overview.memoryUsedPercent} label="内存使用率" size={96} />
                <div className="mon-kv-stack resource-gauge-kv">
                  <InfoRow label="总内存" value={formatBytes(overview.memoryTotalBytes)} />
                  <InfoRow label="已用" value={formatBytes(overview.memoryUsedBytes)} />
                  <InfoRow label="空闲" value={formatBytes(Math.max(0, overview.memoryTotalBytes - overview.memoryUsedBytes))} />
                </div>
              </div>
            </Card>

            <Card title="存储" sub="RouterOS system resource" actions={<HealthBadge percent={overview.storageUsedPercent} />}>
              <div className="resource-gauge-row">
                <GaugeRing percent={overview.storageUsedPercent} label="存储使用率" size={96} from="var(--series-2)" to="var(--grad-line-from)" />
                <div className="mon-kv-stack resource-gauge-kv">
                  <InfoRow label="总空间" value={formatBytes(overview.storageTotalBytes)} />
                  <InfoRow label="已用" value={formatBytes(overview.storageUsedBytes)} />
                  <InfoRow label="空闲" value={formatBytes(Math.max(0, overview.storageTotalBytes - overview.storageUsedBytes))} />
                  <InfoRow label="坏块" value={resource.badBlocks || '-'} />
                </div>
              </div>
            </Card>

            <Card title="系统信息" sub="RouterOS /system/resource" className="resource-card-system">
              <div className="mon-kv-stack">
                <InfoRow label="平台" value={resource.platform} />
                <InfoRow label="架构" value={resource.architectureName} />
                <InfoRow label="主板" value={resource.boardName} />
                <InfoRow label="RouterOS 版本" value={resource.version} />
                <InfoRow label="编译时间" value={resource.buildTime} />
                <InfoRow label="出厂软件" value={resource.factorySoftware} />
                <InfoRow label="运行时间" value={formatUptime(resource.uptime || overview.uptime)} />
              </div>
            </Card>
          </div>

          <Card title="硬件" sub="RouterOS hardware 只读信息" actions={<Badge tone={resource.hardware.length ? 'ok' : 'neutral'}>{resource.hardware.length ? `${resource.hardware.length} 项` : '无数据'}</Badge>}>
            <DataTable ariaLabel="硬件列表" columns={HARDWARE_COLUMNS} rows={resource.hardware} rowKey={(item) => `${item.name}-${item.location}-${item.type}`} emptyTitle="暂无硬件数据" />
          </Card>

          <Card title="IRQ" sub="系统中断分布" actions={<Badge tone={resource.irqs.length ? 'ok' : 'neutral'}>{resource.irqs.length ? `${resource.irqs.length} 项` : '无数据'}</Badge>}>
            <DataTable ariaLabel="IRQ 列表" columns={IRQ_COLUMNS} rows={resource.irqs} rowKey={(item) => `${item.irq}-${item.cpu}`} emptyTitle="暂无 IRQ 数据" />
          </Card>
        </>
      ) : null}
    </div>
  )
}
