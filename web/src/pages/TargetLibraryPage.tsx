import { useCallback, useMemo, useState } from 'react'
import { errorMessage } from '../lib/api'
import { formatCount, formatDateTime } from '../lib/format'
import { usePolling } from '../shell/usePolling'
import { useShell } from '../shell/useShell'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { DataTable } from '../ui/DataTable'
import type { TableColumn } from '../ui/DataTable'
import { Modal } from '../ui/Modal'
import { SearchInput } from '../ui/SearchInput'
import { SegTabs } from '../ui/SegTabs'
import { Skeleton } from '../ui/Skeleton'
import { toast } from '../ui/toastStore'
import { Tooltip } from '../ui/Tooltip'
import { refreshTargetList } from '../features/policy/canonical'
import { deleteTargetListEntry, fetchTargetListEntries, jobIdOf, type TargetListEntry } from '../features/policy/api'
import { JobProgress } from '../features/policy/ui/JobProgress'
import { Notice } from '../features/policy/ui/Notice'
import { RulesViewerModal } from '../features/policy/ui/RulesViewerModal'
import { TargetListModal } from '../features/policy/ui/TargetListModal'
import { kindLabel, scheduleLabel, sourceTypeLabel, targetState } from '../features/policy/ui/labels'
import '../features/policy/policy.css'
import './TargetLibraryPage.css'

type KindFilter = 'all' | 'domain' | 'ip'
type TrackedJob = { id: string; label: string; successMessage: string }

/** future timestamp → 「3 天后」; past/invalid → 「—」 */
function formatNextRun(iso: string): string {
  if (!iso) return '—'
  const timestamp = new Date(iso).getTime()
  if (!Number.isFinite(timestamp)) return '—'
  const seconds = Math.round((timestamp - Date.now()) / 1000)
  if (seconds <= 0) return '即将运行'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${Math.max(1, minutes)} 分钟后`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours} 小时后`
  return `${Math.floor(hours / 24)} 天后`
}

export default function TargetLibraryPage() {
  const { selectedDeviceId, reloadNonce } = useShell()
  const [entries, setEntries] = useState<TargetListEntry[] | null>(null)
  const [initialLoading, setInitialLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [filter, setFilter] = useState<KindFilter>('all')
  const [query, setQuery] = useState('')
  const [trackedJob, setTrackedJob] = useState<TrackedJob | null>(null)
  const [jobError, setJobError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState('')
  const [editing, setEditing] = useState<TargetListEntry | null | undefined>(undefined)
  const [viewing, setViewing] = useState<TargetListEntry | null>(null)
  const [deleting, setDeleting] = useState<TargetListEntry | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const load = useCallback(
    async (silent: boolean) => {
      if (!selectedDeviceId) return
      try {
        setEntries(await fetchTargetListEntries(selectedDeviceId))
        setLoadError(null)
      } catch (error) {
        if (!silent) setLoadError(errorMessage(error, '目标库读取失败'))
      } finally {
        setInitialLoading(false)
      }
    },
    [selectedDeviceId],
  )

  usePolling(() => void load(true), 5000, [load, reloadNonce])

  const partition = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    const filtered = (entries ?? []).filter(
      (entry) =>
        (filter === 'all' || entry.kind === filter) &&
        (!keyword || `${entry.name} ${entry.id} ${entry.url ?? ''} ${entry.presetId ?? ''}`.toLowerCase().includes(keyword)),
    )
    // Unreferenced preset lists are disposable cache — hidden by default
    // behind a toggle; referenced presets and user lists stay visible.
    const unusedPreset = (entry: TargetListEntry) => entry.sourceType === 'preset' && entry.usage.routingRuleCount + entry.usage.accessRuleCount === 0
    return {
      rows: filtered.filter((entry) => !unusedPreset(entry)),
      hiddenPresets: filtered.filter(unusedPreset),
    }
  }, [entries, filter, query])
  const [showUnusedPresets, setShowUnusedPresets] = useState(false)
  const tableRows = showUnusedPresets ? [...partition.rows, ...partition.hiddenPresets] : partition.rows

  const trackJob = (result: { jobId?: string; job?: { id: string } }, label: string, successMessage: string): boolean => {
    const id = jobIdOf(result)
    if (!id) return false
    setJobError(null)
    setTrackedJob({ id, label, successMessage })
    return true
  }

  const refresh = async (entry: TargetListEntry) => {
    if (busyId) return
    setBusyId(entry.id)
    try {
      const result = await refreshTargetList(selectedDeviceId, entry.id)
      const jobId = jobIdOf(result)
      if (jobId) {
        trackJob(result, `正在刷新「${entry.name}」`, '目标库已刷新并应用')
      } else if (result.targetList) {
        toast(`「${entry.name}」已刷新，有效规则 ${formatCount(result.targetList.counts.valid ?? 0)} 条`)
        await load(true)
      } else {
        toast(`「${entry.name}」已是最新，无需更新`)
        await load(true)
      }
    } catch (error) {
      toast(errorMessage(error, '目标库刷新失败'), { tone: 'err' })
    } finally {
      setBusyId('')
    }
  }

  const confirmDelete = async () => {
    const entry = deleting
    if (!entry || busyId) return
    setBusyId(entry.id)
    setDeleteError(null)
    try {
      const result = await deleteTargetListEntry(selectedDeviceId, entry.id, entry.revision)
      if (result.pendingDeletion) {
        if (!trackJob(result, `正在清理「${entry.name}」`, '目标库已删除')) {
          toast('目标库已标记删除，待应用后清理')
          await load(true)
        }
      } else {
        toast('目标库已删除')
        await load(true)
      }
      setDeleting(null)
    } catch (error) {
      setDeleteError(errorMessage(error, '目标库删除失败'))
    } finally {
      setBusyId('')
    }
  }

  const columns: Array<TableColumn<TargetListEntry>> = [
    {
      key: 'name',
      title: '名称',
      render: (entry) => (
        <div className="pol-tl-name">
          <strong>{entry.name}</strong>
          <span className="pol-tl-badges">
            <Badge tone="neutral">{sourceTypeLabel(entry.sourceType)}</Badge>
            {entry.sourceType === 'preset' ? <Badge tone="accent">预设</Badge> : null}
            {!entry.enabled ? <Badge tone="neutral">已停用</Badge> : null}
          </span>
        </div>
      ),
    },
    {
      key: 'kind',
      title: '类型',
      width: '64px',
      render: (entry) => kindLabel(entry.kind),
    },
    {
      key: 'counts',
      title: '规则数',
      numeric: true,
      render: (entry) => {
        const state = targetState(entry)
        return (
          <div className="pol-tl-counts">
            <span>{formatCount(entry.counts.valid ?? 0)} 条</span>
            {entry.kind !== 'ip' && (entry.counts['DOMAIN-KEYWORD'] ?? 0) > 0 ? <small>关键字 {formatCount(entry.counts['DOMAIN-KEYWORD'])} 条</small> : null}
            <Badge tone={state.tone}>{state.label}</Badge>
          </div>
        )
      },
    },
    {
      key: 'schedule',
      title: '更新计划',
      render: (entry) =>
        entry.sourceType === 'url' || entry.sourceType === 'preset' ? (
          <div className="pol-tl-schedule">
            <span>{scheduleLabel(entry.schedule)}</span>
            <small className="faint" title={entry.nextRunAt ? formatDateTime(entry.nextRunAt) : undefined}>
              {entry.nextRunAt ? `下次 ${formatNextRun(entry.nextRunAt)}` : '暂无计划'}
            </small>
          </div>
        ) : (
          <span className="faint">手动刷新</span>
        ),
    },
    {
      key: 'usage',
      title: '被引用',
      render: (entry) => {
        const routing = entry.usage.routingRuleCount
        const access = entry.usage.accessRuleCount
        if (!routing && !access) return <Badge tone="warn">未被使用</Badge>
        return (
          <span className="pol-tl-usage">
            {routing ? <Badge tone="accent">分流 {routing}</Badge> : null}
            {access ? <Badge tone="accent">访问控制 {access}</Badge> : null}
          </span>
        )
      },
    },
    {
      key: 'actions',
      title: '操作',
      render: (entry) => {
        const inUse = entry.usage.routingRuleCount + entry.usage.accessRuleCount > 0
        const preset = entry.sourceType === 'preset'
        const deleteButton = (
          <button
            type="button"
            className="link-button link-danger"
            disabled={busyId === entry.id || inUse || entry.pendingDeletion}
            onClick={() => {
              setDeleteError(null)
              setDeleting(entry)
            }}
          >
            删除
          </button>
        )
        return (
          <div className="pol-tl-actions">
            {preset ? null : (
              <button type="button" className="link-button" disabled={busyId === entry.id} onClick={() => setEditing(entry)}>
                编辑
              </button>
            )}
            <button type="button" className="link-button" disabled={busyId === entry.id} onClick={() => setViewing(entry)}>
              规则
            </button>
            {(entry.sourceType === 'url' || preset) && entry.url ? (
              <button type="button" className="link-button" disabled={busyId === entry.id} onClick={() => void refresh(entry)}>
                {busyId === entry.id ? '刷新中…' : '刷新'}
              </button>
            ) : null}
            {inUse ? (
              <Tooltip tip={`仍被 ${entry.usage.routingRuleCount + entry.usage.accessRuleCount} 条规则引用，先解除引用再删除`}>{deleteButton}</Tooltip>
            ) : preset ? (
              <Tooltip tip="未引用的预设缓存，删除后可在应用预设中随时重新生成">{deleteButton}</Tooltip>
            ) : (
              deleteButton
            )}
          </div>
        )
      },
    },
  ]

  if (initialLoading && !entries) {
    return (
      <div className="page pol-page">
        <header className="page-head">
          <h1>目标库</h1>
          <span className="page-sub">域名 / IP 目标列表，供分流规则与访问控制复用</span>
        </header>
        <Card>
          <Skeleton lines={6} height={13} />
        </Card>
      </div>
    )
  }

  if (!entries) {
    return (
      <div className="page pol-page">
        <header className="page-head">
          <h1>目标库</h1>
          <span className="page-sub">域名 / IP 目标列表，供分流规则与访问控制复用</span>
        </header>
        <Card>
          <Notice tone="err" title="目标库读取失败">
            {loadError ?? '无法读取目标库。'}
          </Notice>
          <Button variant="primary" onClick={() => void load(false)}>
            重试
          </Button>
        </Card>
      </div>
    )
  }

  return (
    <div className="page pol-page">
      <header className="page-head">
        <h1>目标库</h1>
        <span className="page-sub">域名 / IP 目标列表，供分流规则与访问控制复用</span>
        <span className="pol-page-actions">
          <Button variant="primary" onClick={() => setEditing(null)}>
            ＋ 新建目标库
          </Button>
        </span>
      </header>

      {trackedJob ? (
        <Card>
          <JobProgress
            deviceID={selectedDeviceId}
            domain="policy"
            jobId={trackedJob.id}
            label={trackedJob.label}
            onCommitted={() => {
              toast(trackedJob.successMessage)
              setTrackedJob(null)
              void load(true)
            }}
            onFailed={(job) => {
              setTrackedJob(null)
              setJobError(job.error || 'RouterOS 应用失败')
            }}
          />
        </Card>
      ) : null}
      {jobError ? (
        <Notice tone="err" title="应用失败" action={
          <button type="button" className="link-button" onClick={() => setJobError(null)}>
            知道了
          </button>
        }>
          {jobError}
        </Notice>
      ) : null}

      <Card>
        <div className="pol-tl-toolbar">
          <SegTabs
            options={[
              { value: 'all', label: '全部' },
              { value: 'domain', label: '域名' },
              { value: 'ip', label: 'IP' },
            ]}
            value={filter}
            onChange={setFilter}
            ariaLabel="目标库类型筛选"
          />
          <SearchInput value={query} onChange={setQuery} placeholder="搜索名称、ID 或 URL" ariaLabel="搜索目标库" width={240} />
          {partition.hiddenPresets.length > 0 ? (
            <button type="button" className="link-button pol-tl-presets-toggle" onClick={() => setShowUnusedPresets((value) => !value)}>
              {showUnusedPresets ? `收起未引用的预设（${partition.hiddenPresets.length}）` : `显示未引用的预设（${partition.hiddenPresets.length}）`}
            </button>
          ) : null}
        </div>
        <DataTable
          columns={columns}
          rows={tableRows}
          rowKey={(entry) => entry.id}
          emptyTitle={query || filter !== 'all' ? '没有匹配的目标库' : '还没有目标库'}
          emptyDescription={query || filter !== 'all' ? '换个关键词或筛选条件试试。' : '从手动内容、URL 订阅或上传文件创建可复用的目标库。'}
          ariaLabel="目标库列表"
        />
      </Card>

      {editing !== undefined ? (
        <TargetListModal
          deviceID={selectedDeviceId}
          target={editing}
          onClose={() => setEditing(undefined)}
          onSaved={async ({ jobId }) => {
            const creating = !editing
            setEditing(undefined)
            if (jobId) {
              trackJob({ jobId }, creating ? '正在应用新目标库' : '正在应用目标库变更', creating ? '目标库已创建并应用' : '目标库已更新并应用')
            } else {
              toast(creating ? '目标库已创建' : '目标库已更新')
              await load(true)
            }
          }}
        />
      ) : null}

      {viewing ? <RulesViewerModal deviceID={selectedDeviceId} target={viewing} onClose={() => setViewing(null)} /> : null}

      {deleting ? (
        <Modal
          open
          onClose={() => setDeleting(null)}
          title="删除目标库"
          footer={
            <>
              <Button onClick={() => setDeleting(null)}>取消</Button>
              <Button variant="danger" loading={busyId === deleting.id} onClick={() => void confirmDelete()}>
                删除
              </Button>
            </>
          }
        >
          {deleteError ? <Notice tone="err">{deleteError}</Notice> : null}
          <p className="muted">
            确定删除「{deleting.name}」？
            {deleting.usage.routingRuleCount + deleting.usage.accessRuleCount > 0
              ? `它仍被 ${deleting.usage.routingRuleCount + deleting.usage.accessRuleCount} 条规则引用，删除会被拒绝。`
              : deleting.activeVersionId
                ? '内容已从 RouterOS 应用的版本会先标记为待删除，应用后清理。'
                : '内容尚未应用，删除立即生效。'}
          </p>
        </Modal>
      ) : null}
    </div>
  )
}
