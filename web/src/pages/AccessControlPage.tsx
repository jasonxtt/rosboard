import { useCallback, useMemo, useState } from 'react'
import { errorMessage } from '../lib/api'
import { formatRelativeTime } from '../lib/format'
import { usePolling } from '../shell/usePolling'
import { useShell } from '../shell/useShell'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { EmptyState } from '../ui/EmptyState'
import { Modal } from '../ui/Modal'
import { Skeleton } from '../ui/Skeleton'
import { toast } from '../ui/toastStore'
import { deleteAccessRule, saveAccessRule, type AccessRuleDraft, type PolicyTerminal, type TargetList } from '../features/policy/canonical'
import {
  fetchAccessOverviewDetail,
  internetEgressCandidatesOf,
  jobIdOf,
  jobIsTerminal,
  syncAccessControl,
  type AccessOverviewDetail,
  type AccessRuleDetail,
  type InternetEgressCandidates,
} from '../features/policy/api'
import { AccessRuleModal } from '../features/policy/ui/AccessRuleModal'
import { FlowCard } from '../features/policy/ui/FlowCard'
import type { FlowCardStatus } from '../features/policy/ui/FlowCard'
import { InternetEgressModal } from '../features/policy/ui/InternetEgressModal'
import { JobProgress, JobProgressLine } from '../features/policy/ui/JobProgress'
import { Notice } from '../features/policy/ui/Notice'
import { targetCountCap } from '../features/policy/ui/labels'
import type { FlowNode } from '../ui/FlowNodes'
import '../features/policy/policy.css'
import './AccessControlPage.css'

type TrackedJob = { id: string; label: string; successMessage: string }

const statusPresentation: Record<string, FlowCardStatus> = {
  applied: { tone: 'ok', label: '已应用' },
  applying: { tone: 'accent', label: '应用中' },
  pending: { tone: 'warn', label: '待应用' },
  failed: { tone: 'err', label: '应用失败' },
  degraded: { tone: 'warn', label: '部分降级' },
  disabled: { tone: 'neutral', label: '已停用' },
}

function ruleStatus(rule: AccessRuleDetail): FlowCardStatus {
  const presentation = statusPresentation[rule.status] ?? { tone: 'neutral' as const, label: rule.status || '未知' }
  return rule.issues.length ? { ...presentation, tip: rule.issues.join('；') } : presentation
}

function ruleSourceNodes(rule: AccessRuleDetail, terminalByID: Map<string, PolicyTerminal>): FlowNode[] {
  const { subject } = rule
  if (subject.mode === 'all') return [{ label: '全部终端' }]
  const nodes: FlowNode[] = subject.members.slice(0, 4).map((member) => {
    const unresolved = rule.members.some((entry) => entry.terminalId === member.terminalId && entry.state !== 'resolved')
    return {
      label: (
        <>
          {terminalByID.get(member.terminalId)?.displayName || member.terminalId}
          <span className={`flow-node-tag${unresolved ? ' flow-node-tag-warn' : ''}`}>{unresolved ? '未解析' : member.binding === 'fixed' ? '固定' : '自动'}</span>
        </>
      ),
    }
  })
  if (subject.members.length > 4) nodes.push({ label: `+${subject.members.length - 4} 台` })
  if (subject.prefixes.length) nodes.push({ label: `${subject.prefixes.length} 个网段` })
  if (!nodes.length) nodes.push({ label: '未选择来源' })
  return nodes
}

function ruleTargetNodes(rule: AccessRuleDetail, targetByID: Map<string, TargetList>): FlowNode[] {
  if (rule.targetScope === 'internet') return [{ label: '整个互联网' }]
  const nodes = rule.targetListIds.map((id) => {
    const target = targetByID.get(id)
    return target ? { label: target.name, cap: targetCountCap(target) } : { label: '未知目标库' }
  })
  return nodes.length ? nodes : [{ label: '未选择目标' }]
}

export default function AccessControlPage() {
  const { selectedDeviceId, reloadNonce } = useShell()
  const [overview, setOverview] = useState<AccessOverviewDetail | null>(null)
  const [initialLoading, setInitialLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [trackedJob, setTrackedJob] = useState<TrackedJob | null>(null)
  const [jobError, setJobError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState('')
  const [editing, setEditing] = useState<AccessRuleDetail | null | undefined>(undefined)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<AccessRuleDetail | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [syncing, setSyncing] = useState(false)
  const [egressPick, setEgressPick] = useState<InternetEgressCandidates | null>(null)
  const [egressPickBusy, setEgressPickBusy] = useState(false)

  const load = useCallback(
    async (silent: boolean) => {
      if (!selectedDeviceId) return
      try {
        setOverview(await fetchAccessOverviewDetail(selectedDeviceId))
        setLoadError(null)
      } catch (error) {
        if (!silent) setLoadError(errorMessage(error, '访问控制读取失败'))
      } finally {
        setInitialLoading(false)
      }
    },
    [selectedDeviceId],
  )

  usePolling(() => void load(true), 5000, [load, reloadNonce])

  const terminalByID = useMemo(() => new Map((overview?.terminals ?? []).map((terminal) => [terminal.id, terminal])), [overview])
  const targetByID = useMemo(() => new Map((overview?.targetLists ?? []).map((target) => [target.id, target])), [overview])

  const trackJob = (result: { jobId?: string; job?: { id: string } }, label: string, successMessage: string): boolean => {
    const id = jobIdOf(result)
    if (!id) return false
    setJobError(null)
    setTrackedJob({ id, label, successMessage })
    return true
  }

  const sync = async (internetEgresses?: Record<string, string[]>) => {
    if (syncing) return
    setSyncing(true)
    try {
      const result = await syncAccessControl(selectedDeviceId, internetEgresses)
      if (!trackJob(result, '正在同步访问控制到 RouterOS', '访问控制已同步')) {
        toast('访问控制已同步')
        await load(true)
      }
    } catch (error) {
      const candidates = internetEgressCandidatesOf(error)
      if (candidates) setEgressPick(candidates)
      else toast(errorMessage(error, '访问控制同步失败'), { tone: 'err' })
    } finally {
      setSyncing(false)
    }
  }

  const saveRule = async (draft: AccessRuleDraft) => {
    if (saving) return
    setSaving(true)
    setSaveError(null)
    try {
      const result = await saveAccessRule(selectedDeviceId, draft)
      const creating = !draft.id
      setEditing(undefined)
      if (!trackJob(result, '正在应用访问规则', creating ? '访问规则已创建并应用' : '访问规则已更新并应用')) {
        toast(creating ? '访问规则已创建并应用' : '访问规则已更新并应用')
        await load(true)
      }
    } catch (error) {
      const candidates = internetEgressCandidatesOf(error)
      if (candidates) {
        // Desired state was saved; only the apply step needs an explicit internet egress.
        setEditing(undefined)
        toast('规则已保存，需要确认互联网出口后完成同步', { tone: 'err' })
        setEgressPick(candidates)
      } else {
        setSaveError(errorMessage(error, '访问规则保存失败'))
      }
    } finally {
      setSaving(false)
    }
  }

  const toggleRule = async (rule: AccessRuleDetail) => {
    if (busyId) return
    setBusyId(rule.id)
    try {
      const result = await saveAccessRule(selectedDeviceId, {
        id: rule.id,
        name: rule.name,
        subject: rule.subject,
        targetScope: rule.targetScope,
        targetListIds: rule.targetListIds,
        enabled: !rule.enabled,
        revision: rule.revision,
      })
      if (!trackJob(result, rule.enabled ? `正在停用「${rule.name}」` : `正在启用「${rule.name}」`, rule.enabled ? '规则已停用' : '规则已启用')) {
        toast(rule.enabled ? '规则已停用' : '规则已启用')
        await load(true)
      }
    } catch (error) {
      const candidates = internetEgressCandidatesOf(error)
      if (candidates) setEgressPick(candidates)
      else toast(errorMessage(error, '规则状态更新失败'), { tone: 'err' })
    } finally {
      setBusyId('')
    }
  }

  const confirmDelete = async () => {
    const rule = deleting
    if (!rule || busyId) return
    setBusyId(rule.id)
    setDeleteError(null)
    try {
      const result = await deleteAccessRule(selectedDeviceId, rule.id, rule.revision)
      if (!trackJob(result, `正在删除「${rule.name}」`, '访问规则已删除')) {
        toast('访问规则已删除')
        await load(true)
      }
      setDeleting(null)
    } catch (error) {
      setDeleteError(errorMessage(error, '访问规则删除失败'))
    } finally {
      setBusyId('')
    }
  }

  if (initialLoading && !overview) {
    return (
      <div className="page pol-page">
        <header className="page-head">
          <h1>访问控制</h1>
          <span className="page-sub">断网与屏蔽类规则：谁 → 什么目标 → 阻断</span>
        </header>
        <Card>
          <Skeleton lines={5} height={13} />
        </Card>
      </div>
    )
  }

  if (!overview) {
    return (
      <div className="page pol-page">
        <header className="page-head">
          <h1>访问控制</h1>
          <span className="page-sub">断网与屏蔽类规则：谁 → 什么目标 → 阻断</span>
        </header>
        <Card>
          <Notice tone="err" title="访问控制读取失败">
            {loadError ?? '无法读取访问控制配置。'}
          </Notice>
          <Button variant="primary" onClick={() => void load(false)}>
            重试
          </Button>
        </Card>
      </div>
    )
  }

  const inSync = overview.state.desiredRevision === overview.state.appliedRevision
  const activeJob = overview.job && overview.job.id && !jobIsTerminal(overview.job.state) ? overview.job : undefined

  return (
    <div className="page pol-page">
      <header className="page-head">
        <h1>访问控制</h1>
        <span className="page-sub">断网与屏蔽类规则：谁 → 什么目标 → 阻断</span>
        <span className="pol-page-actions">
          <Button variant="primary" disabled={!overview.device.enabled} onClick={() => { setSaveError(null); setEditing(null) }}>
            ＋ 新建规则
          </Button>
        </span>
      </header>

      <div className="pol-fold-row">
        <details className="glass card pol-boundary">
          <summary>
            访问控制能力边界说明
            <span className="faint">（展开查看）</span>
          </summary>
          <p className="pol-boundary-text">{overview.boundary}</p>
        </details>
        <details className="glass card pol-boundary">
          <summary>
            规则同步状态
            <Badge tone={inSync ? 'ok' : 'warn'}>{inSync ? '已同步' : '待同步'}</Badge>
            <span className="faint">（展开查看）</span>
          </summary>
          <div className="pol-kv-list">
            <div className="pol-kv">
              <span>期望修订</span>
              <b>{overview.state.desiredRevision}</b>
            </div>
            <div className="pol-kv">
              <span>已应用修订</span>
              <b>{overview.state.appliedRevision}</b>
            </div>
            <div className="pol-kv">
              <span>最近应用</span>
              <b>{overview.state.appliedAt ? formatRelativeTime(overview.state.appliedAt) : '—'}</b>
            </div>
          </div>
          <div className="pol-fold-actions">
            <Button size="sm" onClick={() => void sync()} loading={syncing} disabled={!overview.device.enabled}>
              同步到 RouterOS
            </Button>
          </div>
        </details>
      </div>

      {trackedJob ? (
        <Card>
          <JobProgress
            deviceID={selectedDeviceId}
            domain="access"
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
      ) : activeJob ? (
        <Card>
          <JobProgressLine job={activeJob} label="正在应用访问控制变更" />
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

      <div className="section-head">
        <h2>访问规则</h2>
        <span className="section-sub">{overview.rules.length ? `${overview.rules.length} 条规则` : '阻断类规则按来源与目标匹配'}</span>
      </div>
      {overview.rules.length ? (
        <div className="pol-rule-list">
          {overview.rules.map((rule) => (
            <FlowCard
              key={rule.id}
              title={rule.name}
              meta={`创建于 ${rule.createdAt ? formatRelativeTime(rule.createdAt) : '未知时间'} · 修订 ${rule.revision}`}
              status={ruleStatus(rule)}
              enabled={rule.enabled}
              busy={busyId === rule.id}
              onToggle={() => void toggleRule(rule)}
              onEdit={() => { setSaveError(null); setEditing(rule) }}
              onDelete={() => { setDeleteError(null); setDeleting(rule) }}
              source={ruleSourceNodes(rule, terminalByID)}
              target={ruleTargetNodes(rule, targetByID)}
              exit={{ label: rule.targetScope === 'internet' ? '禁止互联网' : '阻断访问' }}
              exitTone="err"
              footer={
                rule.issues.length ? (
                  <>
                    {rule.issues.map((issue, index) => (
                      <div className="pol-member-row" key={`issue-${index}`}>
                        <Badge tone="warn">问题</Badge>
                        <span>{issue}</span>
                      </div>
                    ))}
                  </>
                ) : undefined
              }
            />
          ))}
        </div>
      ) : (
        <Card>
          <EmptyState
            icon="🚫"
            title="还没有访问规则"
            description="选择终端与阻断目标（整个互联网或目标库），保存后立即同步到 RouterOS。"
            actionLabel={overview.device.enabled ? '＋ 新建规则' : undefined}
            onAction={overview.device.enabled ? () => { setSaveError(null); setEditing(null) } : undefined}
          />
        </Card>
      )}

      {editing !== undefined ? (
        <AccessRuleModal
          deviceID={selectedDeviceId}
          rule={editing}
          terminals={overview.terminals}
          targetLists={overview.targetLists}
          saving={saving}
          error={saveError}
          onClose={() => setEditing(undefined)}
          onSave={saveRule}
        />
      ) : null}

      {deleting ? (
        <Modal
          open
          onClose={() => setDeleting(null)}
          title="删除访问规则"
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
          <p className="muted">确定删除「{deleting.name}」？删除后会同步清理对应的 RouterOS 规则。</p>
        </Modal>
      ) : null}

      {egressPick ? (
        <InternetEgressModal
          candidates={egressPick}
          busy={egressPickBusy}
          onClose={() => setEgressPick(null)}
          onSubmit={(selection) => {
            setEgressPickBusy(true)
            void (async () => {
              try {
                const result = await syncAccessControl(selectedDeviceId, selection)
                setEgressPick(null)
                if (!trackJob(result, '正在按指定出口同步访问控制', '访问控制已同步')) {
                  toast('访问控制已同步')
                  await load(true)
                }
              } catch (error) {
                toast(errorMessage(error, '访问控制同步失败'), { tone: 'err' })
              } finally {
                setEgressPickBusy(false)
              }
            })()
          }}
        />
      ) : null}
    </div>
  )
}
