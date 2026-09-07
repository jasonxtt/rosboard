import { useCallback, useMemo, useState } from 'react'
import { errorMessage } from '../lib/api'
import { usePolling } from '../shell/usePolling'
import { useShell } from '../shell/useShell'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { EmptyState } from '../ui/EmptyState'
import { Modal } from '../ui/Modal'
import { Skeleton } from '../ui/Skeleton'
import { toast } from '../ui/toastStore'
import {
  deleteRoutingRule,
  fetchRoutingContext,
  generatePolicyPlan,
  saveRoutingRule,
  type Egress,
  type PlanEnvelope,
  type PolicyTerminal,
  type RoutingRule,
  type TargetList,
  type TrafficIngressScope,
} from '../features/policy/canonical'
import {
  deleteEgress,
  fetchInterfaceRates,
  fetchPolicyOverviewMeta,
  jobIdOf,
  setEgressEnabled,
  type InterfaceRate,
  type PolicyOverviewMeta,
} from '../features/policy/api'
import { EgressCard } from '../features/policy/ui/EgressCard'
import { EgressModal } from '../features/policy/ui/EgressModal'
import { FlowCard } from '../features/policy/ui/FlowCard'
import type { FlowCardStatus } from '../features/policy/ui/FlowCard'
import { JobProgress, JobProgressLine } from '../features/policy/ui/JobProgress'
import { Notice } from '../features/policy/ui/Notice'
import { PlanReviewModal } from '../features/policy/ui/PlanReview'
import { RoutingRuleWizard } from '../features/policy/ui/RoutingRuleWizard'
import { targetCountCap } from '../features/policy/ui/labels'
import type { FlowNode } from '../ui/FlowNodes'
import '../features/policy/policy.css'
import './PolicyRoutingPage.css'

type RoutingContext = {
  egresses: Egress[]
  rules: RoutingRule[]
  targetLists: TargetList[]
  terminals: PolicyTerminal[]
  trafficIngress: TrafficIngressScope
}

type TrackedJob = { id: string; label: string; successMessage: string }

function ruleStatus(rule: RoutingRule, egress: Egress | undefined): FlowCardStatus {
  if (!rule.enabled) return { tone: 'neutral', label: '已停用' }
  if (!egress) return { tone: 'err', label: '出口缺失' }
  if (egress.pendingDeletion) return { tone: 'warn', label: '出口待删除' }
  if (!egress.enabled) return { tone: 'warn', label: '出口已停用' }
  if (!egress.applied) return { tone: 'warn', label: '待应用' }
  return { tone: 'ok', label: '已应用' }
}

function ruleSourceNodes(rule: RoutingRule, terminalByID: Map<string, PolicyTerminal>): FlowNode[] {
  const { subject } = rule
  if (subject.mode === 'all') return [{ label: '全部终端' }]
  const names = subject.members.map((member) => terminalByID.get(member.terminalId)?.displayName || member.terminalId)
  const nodes: FlowNode[] = []
  if (subject.mode === 'excluded') {
    nodes.push({ label: '全部终端' })
    if (names.length) nodes.push({ label: `排除 ${names.length} 台` })
  } else {
    for (const name of names.slice(0, 3)) nodes.push({ label: name })
    if (names.length > 3) nodes.push({ label: `+${names.length - 3} 台` })
  }
  if (subject.prefixes.length) nodes.push({ label: `${subject.prefixes.length} 个网段` })
  if (!nodes.length) nodes.push({ label: '未选择来源' })
  return nodes
}

function ruleTargetNodes(rule: RoutingRule, targetByID: Map<string, TargetList>): FlowNode[] {
  if (!rule.targetListIds.length) return [{ label: '整个互联网' }]
  return rule.targetListIds.map((id) => {
    const target = targetByID.get(id)
    if (target) return { label: target.name, cap: targetCountCap(target) }
    const preset = /^preset:([^:]+):(domain|ip)$/.exec(id)
    if (preset) return { label: `应用预设 · ${preset[1]}` }
    return { label: '未知目标库' }
  })
}

export default function PolicyRoutingPage() {
  const { selectedDeviceId, reloadNonce } = useShell()
  const [context, setContext] = useState<RoutingContext | null>(null)
  const [meta, setMeta] = useState<PolicyOverviewMeta | null>(null)
  const [rates, setRates] = useState<Map<string, InterfaceRate> | null>(null)
  const [initialLoading, setInitialLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [trackedJob, setTrackedJob] = useState<TrackedJob | null>(null)
  const [jobError, setJobError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState('')
  const [egressEditor, setEgressEditor] = useState<Egress | null | undefined>(undefined)
  const [egressDeleting, setEgressDeleting] = useState<Egress | null>(null)
  const [deleteEgressError, setDeleteEgressError] = useState<string | null>(null)
  const [wizard, setWizard] = useState<RoutingRule | null | undefined>(undefined)
  const [ruleDeleting, setRuleDeleting] = useState<RoutingRule | null>(null)
  const [deleteRuleError, setDeleteRuleError] = useState<string | null>(null)
  const [reviewPlan, setReviewPlan] = useState<PlanEnvelope | null>(null)
  const [reviewGenerating, setReviewGenerating] = useState(false)

  const load = useCallback(
    async (silent: boolean) => {
      if (!selectedDeviceId) return
      try {
        const [nextContext, nextMeta] = await Promise.all([fetchRoutingContext(selectedDeviceId), fetchPolicyOverviewMeta(selectedDeviceId)])
        setContext(nextContext)
        setMeta(nextMeta)
        setLoadError(null)
      } catch (error) {
        if (!silent) setLoadError(errorMessage(error, '策略路由读取失败'))
      } finally {
        setInitialLoading(false)
      }
      // Live rates are best-effort; never let them break the page.
      try {
        setRates(await fetchInterfaceRates(selectedDeviceId))
      } catch {
        /* keep previous rates */
      }
    },
    [selectedDeviceId],
  )

  usePolling(() => void load(true), 5000, [load, reloadNonce])

  const egressByID = useMemo(() => new Map((context?.egresses ?? []).map((egress) => [egress.id, egress])), [context])
  const targetByID = useMemo(() => new Map((context?.targetLists ?? []).map((target) => [target.id, target])), [context])
  const terminalByID = useMemo(() => new Map((context?.terminals ?? []).map((terminal) => [terminal.id, terminal])), [context])

  const writeBlocked = Boolean(meta && !meta.account.writeAccess)
  const dirtyEgresses = useMemo(() => (context?.egresses ?? []).filter((egress) => egress.pendingDeletion || !egress.applied), [context])

  const openReview = useCallback(async () => {
    if (!selectedDeviceId || !context || reviewGenerating) return
    setReviewGenerating(true)
    try {
      const envelope = await generatePolicyPlan(selectedDeviceId, context.egresses.length ? 'structural' : 'initial')
      if (!envelope.plan.operations.length && !envelope.plan.blockers.length) {
        toast('没有需要应用的变更', { tone: 'ok' })
        return
      }
      setReviewPlan(envelope)
    } catch (error) {
      toast(errorMessage(error, '变更计划生成失败'), { tone: 'err' })
    } finally {
      setReviewGenerating(false)
    }
  }, [selectedDeviceId, context, reviewGenerating])

  const trackJob = (result: { jobId?: string; job?: { id: string } }, label: string, successMessage: string): boolean => {
    const id = jobIdOf(result)
    if (!id) return false
    setJobError(null)
    setTrackedJob({ id, label, successMessage })
    return true
  }

  const toggleRule = async (rule: RoutingRule) => {
    if (busyId) return
    setBusyId(rule.id)
    try {
      const result = await saveRoutingRule(selectedDeviceId, { ...rule, enabled: !rule.enabled })
      if (!trackJob(result, rule.enabled ? `正在停用「${rule.name}」` : `正在启用「${rule.name}」`, rule.enabled ? '规则已停用' : '规则已启用')) {
        toast(rule.enabled ? '规则已停用' : '规则已启用')
        await load(true)
      }
    } catch (error) {
      toast(errorMessage(error, '规则状态更新失败'), { tone: 'err' })
    } finally {
      setBusyId('')
    }
  }

  const confirmDeleteRule = async () => {
    const rule = ruleDeleting
    if (!rule || busyId) return
    setBusyId(rule.id)
    setDeleteRuleError(null)
    try {
      const result = await deleteRoutingRule(selectedDeviceId, rule.id, rule.revision)
      if (!trackJob(result, `正在删除「${rule.name}」`, '规则已删除')) {
        toast('规则已删除')
        await load(true)
      }
      setRuleDeleting(null)
    } catch (error) {
      setDeleteRuleError(errorMessage(error, '规则删除失败'))
    } finally {
      setBusyId('')
    }
  }

  const toggleEgress = async (egress: Egress) => {
    if (busyId) return
    setBusyId(egress.id)
    try {
      const result = await setEgressEnabled(selectedDeviceId, egress.id, egress.revision, !egress.enabled)
      if (!trackJob(result, egress.enabled ? `正在停用出口「${egress.name}」` : `正在启用出口「${egress.name}」`, egress.enabled ? '出口已停用' : '出口已启用')) {
        toast(egress.enabled ? '出口已停用' : '出口已启用')
        await load(true)
      }
    } catch (error) {
      toast(errorMessage(error, '出口状态更新失败'), { tone: 'err' })
    } finally {
      setBusyId('')
    }
  }

  const confirmDeleteEgress = async () => {
    const egress = egressDeleting
    if (!egress || busyId) return
    setBusyId(egress.id)
    setDeleteEgressError(null)
    try {
      const result = await deleteEgress(selectedDeviceId, egress.id, egress.revision)
      if (result.pendingDeletion) {
        if (!trackJob(result, `正在清理出口「${egress.name}」`, '出口已删除')) {
          toast('出口已标记删除，待应用后清理')
          await load(true)
        }
      } else {
        toast('出口已删除')
        await load(true)
      }
      setEgressDeleting(null)
    } catch (error) {
      setDeleteEgressError(errorMessage(error, '出口删除失败'))
    } finally {
      setBusyId('')
    }
  }

  if (initialLoading && !context) {
    return (
      <div className="page pol-page">
        <header className="page-head">
          <h1>策略路由</h1>
          <span className="page-sub">谁 → 访问什么 → 走哪条线路</span>
        </header>
        <div className="pol-egr-grid">
          {[0, 1, 2].map((index) => (
            <Card key={index}>
              <Skeleton lines={5} height={13} />
            </Card>
          ))}
        </div>
      </div>
    )
  }

  if (!context) {
    return (
      <div className="page pol-page">
        <header className="page-head">
          <h1>策略路由</h1>
          <span className="page-sub">谁 → 访问什么 → 走哪条线路</span>
        </header>
        <Card>
          <Notice tone="err" title="策略路由读取失败">
            {loadError ?? '无法读取策略路由配置。'}
          </Notice>
          <Button variant="primary" onClick={() => void load(false)}>
            重试
          </Button>
        </Card>
      </div>
    )
  }

  const overviewJobs = meta?.activeJobs ?? []

  return (
    <div className="page pol-page">
      <header className="page-head">
        <h1>策略路由</h1>
        <span className="page-sub">谁 → 访问什么 → 走哪条线路</span>
      </header>

      {meta?.setupState === 'write_access_required' ? (
        <Notice tone="err" title="RouterOS 账号只有只读权限">
          账号「{meta.account.username || '未知'}」当前为只读{meta.account.group ? `（用户组 ${meta.account.group}）` : ''}
          。请在「面板设置 → 设备管理」中把 rosboard 使用的 RouterOS 账号换成具备写入权限的账号（write 或 full 用户组），否则出口与规则无法保存。
        </Notice>
      ) : null}
      {meta && (meta.setupState === 'manager_unavailable' || meta.setupState === 'runtime_unavailable') ? (
        <Notice tone="warn" title="策略运行时不可用">
          {meta.account.error || '策略管理器或设备运行时未就绪，配置变更暂时无法应用到 RouterOS。'}
        </Notice>
      ) : null}
      {meta?.health.mutationPaused ? (
        <Notice tone="warn" title="变更执行已暂停">
          {meta.health.pauseReason || '存在需要人工处理的任务，新的变更不会写入 RouterOS。'}
        </Notice>
      ) : null}
      {meta?.health.state === 'degraded' && !meta.health.mutationPaused ? (
        <Notice tone="warn" title="上一次应用存在问题">
          {meta.health.pauseReason || '最近一次 RouterOS 应用任务失败了，新的应用会重试。'}
        </Notice>
      ) : null}

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
      ) : (
        overviewJobs.map((job) => (
          <Card key={job.id}>
            <JobProgressLine job={job} />
          </Card>
        ))
      )}
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
        <h2>出口线路</h2>
        <span className="section-sub">
          {context.egresses.length ? `${context.egresses.length} 条出口${dirtyEgresses.length ? ` · ${dirtyEgresses.length} 条待应用` : ''}` : '还没有出口'}
        </span>
        {dirtyEgresses.length ? (
          <button type="button" className="section-link" disabled={reviewGenerating || writeBlocked} onClick={() => void openReview()}>
            {reviewGenerating ? '正在生成计划…' : '审查并应用 →'}
          </button>
        ) : null}
        <button type="button" className="section-link" disabled={writeBlocked} onClick={() => setEgressEditor(null)}>
          ＋ 添加出口
        </button>
      </div>
      {context.egresses.length ? (
        <div className="pol-egr-grid">
          {context.egresses.map((egress) => (
            <EgressCard
              key={egress.id}
              egress={egress}
              rates={rates ?? undefined}
              busy={busyId === egress.id}
              onEdit={() => setEgressEditor(egress)}
              onDelete={() => {
                setDeleteEgressError(null)
                setEgressDeleting(egress)
              }}
              onToggle={() => void toggleEgress(egress)}
            />
          ))}
        </div>
      ) : (
        <Card>
          <EmptyState
            icon="🌐"
            title="还没有出口"
            description="出口是一条 WAN 线路加它的路由 / NAT / 故障策略，分流规则会把流量指向出口。"
            actionLabel={writeBlocked ? undefined : '＋ 添加出口'}
            onAction={writeBlocked ? undefined : () => setEgressEditor(null)}
          />
        </Card>
      )}

      <div className="section-head">
        <h2>分流规则</h2>
        <span className="section-sub">{context.rules.length ? `${context.rules.length} 条规则` : '流量从「谁」经过「什么目标」流向「哪条线路」'}</span>
        <button type="button" className="section-link" disabled={writeBlocked} onClick={() => setWizard(null)}>
          ＋ 新建规则
        </button>
      </div>
      {context.rules.length ? (
        <div className="pol-rule-list">
          {context.rules.map((rule) => {
            const egress = egressByID.get(rule.egressId)
            const status = ruleStatus(rule, egress)
            const pending = status.label === '待应用'
            return (
              <FlowCard
                key={rule.id}
                title={rule.name}
                meta={`优先级 ${rule.priority} · 修订 ${rule.revision}`}
                status={status}
                enabled={rule.enabled}
                busy={busyId === rule.id}
                onToggle={() => void toggleRule(rule)}
                onEdit={() => setWizard(rule)}
                onDelete={() => {
                  setDeleteRuleError(null)
                  setRuleDeleting(rule)
                }}
                source={ruleSourceNodes(rule, terminalByID)}
                target={ruleTargetNodes(rule, targetByID)}
                exit={{ label: egress?.name || '出口缺失' }}
                pending={pending}
                onReview={pending ? () => void openReview() : undefined}
              />
            )
          })}
        </div>
      ) : (
        <Card>
          <EmptyState
            icon="🔀"
            title="还没有分流规则"
            description="把来源、访问目标和出口组合成一条规则，生成可审阅的 RouterOS 变更计划后再应用。"
            actionLabel={writeBlocked ? undefined : '＋ 新建规则'}
            onAction={writeBlocked ? undefined : () => setWizard(null)}
          />
        </Card>
      )}

      {egressEditor !== undefined ? (
        <EgressModal
          deviceID={selectedDeviceId}
          egress={egressEditor}
          onClose={() => setEgressEditor(undefined)}
          onSaved={async () => {
            setEgressEditor(undefined)
            toast('出口已保存，变更待应用', { tone: 'ok' })
            await load(true)
          }}
        />
      ) : null}

      {wizard !== undefined ? (
        <RoutingRuleWizard
          deviceID={selectedDeviceId}
          context={{ egresses: context.egresses, targetLists: context.targetLists, terminals: context.terminals, trafficIngress: context.trafficIngress }}
          rule={wizard}
          onClose={() => setWizard(undefined)}
          onSaved={async () => {
            setWizard(undefined)
            toast(wizard ? '规则已更新并应用' : '规则已创建并应用')
            await load(true)
          }}
        />
      ) : null}

      {ruleDeleting ? (
        <Modal
          open
          onClose={() => setRuleDeleting(null)}
          title="删除分流规则"
          footer={
            <>
              <Button onClick={() => setRuleDeleting(null)}>取消</Button>
              <Button variant="danger" loading={busyId === ruleDeleting.id} onClick={() => void confirmDeleteRule()}>
                删除
              </Button>
            </>
          }
        >
          {deleteRuleError ? <Notice tone="err">{deleteRuleError}</Notice> : null}
          <p className="muted">
            确定删除「{ruleDeleting.name}」？删除后会生成变更计划并清理对应的 RouterOS 规则。
          </p>
        </Modal>
      ) : null}

      {egressDeleting ? (
        <Modal
          open
          onClose={() => setEgressDeleting(null)}
          title="删除出口"
          footer={
            <>
              <Button onClick={() => setEgressDeleting(null)}>取消</Button>
              <Button variant="danger" loading={busyId === egressDeleting.id} onClick={() => void confirmDeleteEgress()}>
                删除
              </Button>
            </>
          }
        >
          {deleteEgressError ? <Notice tone="err">{deleteEgressError}</Notice> : null}
          <p className="muted">
            确定删除出口「{egressDeleting.name}」？
            {egressDeleting.applied ? '该出口已应用到 RouterOS，删除会先标记为待删除，并在应用后清理路由、地址列表与 NAT。' : '该出口尚未应用，删除立即生效。'}
          </p>
          {context.rules.some((rule) => rule.egressId === egressDeleting.id) ? (
            <Notice tone="warn" title="出口仍被引用">
              有分流规则正在使用此出口，删除后这些规则会显示「出口缺失」。
            </Notice>
          ) : null}
        </Modal>
      ) : null}

      <PlanReviewModal
        open={Boolean(reviewPlan)}
        deviceID={selectedDeviceId}
        envelope={reviewPlan}
        onClose={() => setReviewPlan(null)}
        onApplied={async () => {
          setReviewPlan(null)
          toast('变更已应用到 RouterOS')
          await load(true)
        }}
        onRepreview={async () => {
          setReviewPlan(null)
          await openReview()
        }}
      />
    </div>
  )
}
