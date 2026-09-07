import { useEffect, useMemo, useRef, useState } from 'react'
import { Badge } from '../../../ui/Badge'
import { Button } from '../../../ui/Button'
import { Field, Input } from '../../../ui/inputs'
import { Modal } from '../../../ui/Modal'
import { Toggle } from '../../../ui/Toggle'
import {
  fetchPolicyDiscovery,
  generatePolicyPlan,
  type ApplicationPresetSelection,
  type Egress,
  type PlanEnvelope,
  type PolicyDiscovery,
  type PolicyPlanProposal,
  type PolicyTerminal,
  type RoutingRule,
  type Subject,
  type TargetList,
  type TrafficIngressScope,
} from '../canonical'
import { hasTrafficIngress, requiresTrafficIngress, sourceIsValid } from '../source'
import { EgressFields } from './EgressModal'
import { defaultEgressDraft, egressDraftErrors, egressDraftFrom } from './egressDraft'
import { errorMessage } from '../../../lib/api'
import { Notice } from './Notice'
import { PlanReviewBody } from './PlanReview'
import type { PlanSummaryEntry } from './PlanReview'
import { SubjectSelector } from './SubjectSelector'
import { TargetListModal } from './TargetListModal'
import { TargetSelector } from './TargetSelector'
import type { PresetPresentation } from './TargetSelector'
import { WizardSteps } from './WizardSteps'
import { failureModeLabel } from './labels'

export type WizardContext = {
  egresses: Egress[]
  targetLists: TargetList[]
  terminals: PolicyTerminal[]
  trafficIngress: TrafficIngressScope
}

type EgressChoice = 'existing' | 'new'

const STEPS = ['策略与来源', '访问目标', '出口', '审查并应用']

function targetKindLabel(kind: 'domain' | 'ip') {
  return kind === 'ip' ? 'IP' : '域名'
}

function targetNamesForReview(targetListIDs: string[], targetLists: TargetList[], presetPresentations: PresetPresentation[]): string {
  const names: string[] = []
  const unresolvedPresets = new Set<string>()
  for (const id of targetListIDs) {
    const match = /^preset:([^:]+):(domain|ip)$/.exec(id)
    if (match) {
      unresolvedPresets.add(match[1])
      continue
    }
    names.push(targetLists.find((target) => target.id === id)?.name ?? '目标库')
  }
  for (const presentation of presetPresentations) {
    const kinds = presentation.requestedKinds.filter((kind) => targetListIDs.includes(`preset:${presentation.presetId}:${kind}`))
    if (!kinds.length) continue
    names.push(`${presentation.name} · ${kinds.length === 2 ? '域名/IP' : targetKindLabel(kinds[0])}`)
    unresolvedPresets.delete(presentation.presetId)
  }
  for (const presetID of unresolvedPresets) {
    names.push(targetLists.find((target) => target.id.startsWith(`preset:${presetID}:`) && targetListIDs.includes(target.id))?.name ?? '应用预设')
  }
  return names.join('、')
}

function egressFamilySummaryLine(egress: Egress): string {
  return (
    egress.families
      .filter((family) => family.enabled)
      .map((family) => {
        const endpoint = family.wanSource === 'next-hop' || !family.wanInterface ? `下一跳 ${family.gateway || '未设置'}` : `${family.wanInterface}${family.gateway ? ` · ${family.gateway}` : ''}`
        return `${family.family.toUpperCase()} ${endpoint}`
      })
      .join('；') || '未启用协议族'
  )
}

type RoutingRuleWizardProps = {
  deviceID: string
  context: WizardContext
  /** null = 新建规则 */
  rule: RoutingRule | null
  onClose: () => void
  onSaved: () => void | Promise<void>
}

/** 4-step 分流规则 wizard: 策略与来源 → 访问目标 → 出口 → 审查并应用. */
export function RoutingRuleWizard({ deviceID, context, rule, onClose, onSaved }: RoutingRuleWizardProps) {
  const initialEgress = rule ? (context.egresses.find((candidate) => candidate.id === rule.egressId) ?? null) : null
  const [activeStep, setActiveStep] = useState(0)
  const [maxUnlockedStep, setMaxUnlockedStep] = useState(rule ? 2 : 0)
  const [ruleName, setRuleName] = useState(rule?.name ?? '')
  const [rulePriority, setRulePriority] = useState(String(rule?.priority ?? 100))
  const [enabled, setEnabled] = useState(rule?.enabled ?? true)
  const [subject, setSubject] = useState<Subject>(() => rule?.subject ?? { mode: 'all', members: [], prefixes: [] })
  const [trafficIngress, setTrafficIngress] = useState<TrafficIngressScope>(() => ({
    interfaceLists: [...(rule?.ingress?.interfaceLists ?? context.trafficIngress.interfaceLists)],
    interfaces: [...(rule?.ingress?.interfaces ?? context.trafficIngress.interfaces)],
  }))
  const [egressChoice, setEgressChoice] = useState<EgressChoice>(initialEgress ? 'existing' : 'new')
  const [selectedEgressId, setSelectedEgressId] = useState(initialEgress?.id ?? '')
  const [draft, setDraft] = useState<Egress>(() => (initialEgress ? egressDraftFrom(initialEgress) : defaultEgressDraft()))
  const [discovery, setDiscovery] = useState<PolicyDiscovery | null>(null)
  const [discoveryError, setDiscoveryError] = useState<string | null>(null)
  const [targetListIDs, setTargetListIDs] = useState<string[]>(() => [...(rule?.targetListIds ?? [])])
  const [targetLists, setTargetLists] = useState<TargetList[]>(() => [...context.targetLists])
  const [creatingTargetKind, setCreatingTargetKind] = useState<'domain' | 'ip' | null>(null)
  const [presetPresentations, setPresetPresentations] = useState<PresetPresentation[]>([])
  const [generating, setGenerating] = useState(false)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [plan, setPlan] = useState<PlanEnvelope | null>(null)
  const [planSummary, setPlanSummary] = useState<PlanSummaryEntry[] | null>(null)
  const [draftRevision, setDraftRevision] = useState(0)
  const [planDraftRevision, setPlanDraftRevision] = useState<number | null>(null)
  const draftRevisionRef = useRef(0)
  const generationInFlightRef = useRef(false)
  const [ingressDefaulted, setIngressDefaulted] = useState(false)

  useEffect(() => {
    let active = true
    fetchPolicyDiscovery(deviceID)
      .then((value) => {
        if (active) setDiscovery(value)
      })
      .catch((loadError) => {
        if (active) setDiscoveryError(errorMessage(loadError, '设备发现读取失败'))
      })
    return () => {
      active = false
    }
  }, [deviceID])

  // Default the ingress to the discovery-provided default interface-list once,
  // mirroring the old wizard (only for all/excluded source modes).
  useEffect(() => {
    if (ingressDefaulted || !discovery?.available || subject.mode === 'selected') return
    setIngressDefaulted(true)
    if (hasTrafficIngress(trafficIngress)) return
    const defaultList = discovery.trafficIngress.find((candidate) => candidate.kind === 'interface-list' && candidate.default)
    if (defaultList) setTrafficIngress({ interfaceLists: [defaultList.name], interfaces: [] })
  }, [discovery, ingressDefaulted, subject.mode, trafficIngress])

  const markDraftChanged = () => {
    draftRevisionRef.current += 1
    setDraftRevision(draftRevisionRef.current)
    setError(null)
  }

  const strategyErrors = useMemo(() => {
    const errors: string[] = []
    if (!ruleName.trim()) errors.push('规则名称不能为空')
    if (!sourceIsValid(subject, trafficIngress)) errors.push('来源范围配置不完整，请选择有效入口或来源终端/地址')
    return errors
  }, [ruleName, subject, trafficIngress])

  const invalidTargetIDs = useMemo(() => {
    const targetByID = new Map(targetLists.map((target) => [target.id, target]))
    return targetListIDs.filter((id) => !id.startsWith('preset:') && (!targetByID.get(id) || targetByID.get(id)?.pendingDeletion))
  }, [targetListIDs, targetLists])
  const targetErrors = useMemo(() => {
    if (!targetListIDs.length) return ['至少选择一个访问目标']
    return invalidTargetIDs.length ? [`以下访问目标已不可用，请重新选择：${invalidTargetIDs.join('、')}`] : []
  }, [invalidTargetIDs, targetListIDs.length])
  const egressErrors = useMemo(() => egressDraftErrors(draft, discovery, false), [draft, discovery])

  const busy = generating || applying
  const planFresh = Boolean(plan && planDraftRevision === draftRevision)

  const updateDraft = (next: Egress) => {
    markDraftChanged()
    setDraft(next)
  }
  const toggleIngress = (candidate: PolicyDiscovery['trafficIngress'][number]) => {
    markDraftChanged()
    if (candidate.kind === 'interface-list') {
      const selected = trafficIngress.interfaceLists.includes(candidate.name)
      setTrafficIngress((current) => ({
        interfaceLists: selected ? current.interfaceLists.filter((name) => name !== candidate.name) : [...current.interfaceLists, candidate.name],
        interfaces: selected ? current.interfaces : current.interfaces.filter((name) => !candidate.include.includes(name) && !candidate.staticMembers.includes(name)),
      }))
      return
    }
    setTrafficIngress((current) => ({
      ...current,
      interfaces: current.interfaces.includes(candidate.name) ? current.interfaces.filter((name) => name !== candidate.name) : [...current.interfaces, candidate.name],
    }))
  }

  const firstInvalidStep = () => {
    if (strategyErrors.length) return 0
    if (targetErrors.length) return 1
    if (egressErrors.length) return 2
    return null
  }

  const generateCurrentPlan = async () => {
    if (generationInFlightRef.current) return
    const invalidStep = firstInvalidStep()
    if (invalidStep !== null) {
      setError(null)
      setActiveStep(invalidStep)
      return
    }
    const requestRevision = draftRevisionRef.current
    const selections: ApplicationPresetSelection[] = presetPresentations.filter((selection) => selection.previewId)
    const proposal: PolicyPlanProposal = {
      egress: draft,
      routingRule: {
        id: rule?.id ?? '',
        name: ruleName.trim(),
        subject,
        ingress: trafficIngress,
        targetListIds: targetListIDs,
        egressId: draft.id,
        priority: Number(rulePriority) || 0,
        enabled,
        revision: rule?.revision ?? 0,
      },
      ...(selections.length ? { presetSelections: selections } : {}),
    }
    generationInFlightRef.current = true
    setGenerating(true)
    setError(null)
    setActiveStep(3)
    try {
      const envelope = await generatePolicyPlan(deviceID, context.egresses.length ? 'structural' : 'initial', proposal)
      if (requestRevision !== draftRevisionRef.current) {
        setError('配置在生成期间发生变化，请重新生成预览。')
        return
      }
      setPlanSummary([
        ['规则', `${ruleName.trim()} · ${enabled ? '启用' : '停用'} · 优先级 ${Number(rulePriority) || 0}`],
        ['来源', subject.mode === 'all' ? '全部终端' : subject.mode === 'excluded' ? `排除 ${subject.members.length} 台终端 / ${subject.prefixes.length} 个网段` : `${subject.members.length} 台终端 / ${subject.prefixes.length} 个网段`],
        ['策略入口', [...trafficIngress.interfaceLists, ...trafficIngress.interfaces].join('、') || '仅来源匹配（无入口）'],
        ['访问目标', targetNamesForReview(targetListIDs, targetLists, presetPresentations) || '—'],
        ['出口', `${draft.name || '自动命名'} · ${egressFamilySummaryLine(draft)}`],
        ['故障策略', failureModeLabel(draft.failureMode)],
      ])
      setPlan(envelope)
      setPlanDraftRevision(requestRevision)
      setMaxUnlockedStep(3)
    } catch (generateError) {
      setError(errorMessage(generateError, '变更计划生成失败'))
    } finally {
      generationInFlightRef.current = false
      setGenerating(false)
    }
  }

  const advance = () => {
    if (activeStep === 0) {
      if (strategyErrors.length) return
      setMaxUnlockedStep((current) => Math.max(current, 1))
      setActiveStep(1)
      return
    }
    if (activeStep === 1) {
      if (targetErrors.length) return
      setMaxUnlockedStep((current) => Math.max(current, 2))
      setActiveStep(2)
      return
    }
    if (activeStep === 2) {
      if (planFresh) {
        setActiveStep(3)
        return
      }
      void generateCurrentPlan()
    }
  }

  const handleStepJump = (index: number) => {
    if (busy) return
    setError(null)
    if (index === 3) {
      if (planFresh) setActiveStep(3)
      else void generateCurrentPlan()
      return
    }
    setActiveStep(index)
  }

  const selectExistingEgress = (egress: Egress) => {
    markDraftChanged()
    setSelectedEgressId(egress.id)
    setDraft(egressDraftFrom(egress))
  }
  const switchChoice = (choice: EgressChoice) => {
    markDraftChanged()
    setEgressChoice(choice)
    if (choice === 'new') {
      setSelectedEgressId('')
      setDraft(defaultEgressDraft())
    } else {
      const fallback = context.egresses.find((egress) => egress.id === selectedEgressId) ?? context.egresses[0]
      if (fallback) {
        setSelectedEgressId(fallback.id)
        setDraft(egressDraftFrom(fallback))
      }
    }
  }

  const title = rule ? `编辑分流规则：${rule.name}` : '新建分流规则'
  const primaryDisabled = busy || (activeStep === 0 && strategyErrors.length > 0) || (activeStep === 1 && targetErrors.length > 0) || (activeStep === 2 && egressErrors.length > 0)
  const footer =
    activeStep < 3 ? (
      <>
        <Button disabled={busy} onClick={activeStep === 0 ? onClose : () => setActiveStep((current) => Math.max(0, current - 1))}>
          {activeStep === 0 ? '取消' : '上一步'}
        </Button>
        <Button variant="primary" loading={generating} disabled={primaryDisabled} onClick={advance}>
          {activeStep < 2 ? '下一步' : '生成变更计划'}
        </Button>
      </>
    ) : undefined

  const kindLabelForIngress: Record<string, string> = { 'interface-list': '接口列表', bridge: 'Bridge', vlan: 'VLAN', wireguard: 'WireGuard', vpn: 'VPN', tunnel: '隧道', physical: '物理接口' }
  const selectedLists = new Set(trafficIngress.interfaceLists)
  const hasIngress = hasTrafficIngress(trafficIngress)

  return (
    <>
      <Modal
        open
        onClose={() => {
          if (!busy) onClose()
        }}
        title={title}
        maxWidth={760}
        footer={footer}
      >
        <WizardSteps steps={STEPS} current={activeStep} maxUnlocked={planFresh ? 3 : maxUnlockedStep} disabled={busy} onJump={handleStepJump} />
        {error ? <Notice tone="err">{error}</Notice> : null}
        {discoveryError ? (
          <Notice tone="warn" title="设备发现不可用">
            {discoveryError}；接口与网关需要手动填写。
          </Notice>
        ) : null}
        {discovery && !discovery.available ? (
          <Notice tone="warn" title="设备发现不可用">
            {discovery.reason || '无法读取 RouterOS WAN 与入口候选。'}
          </Notice>
        ) : null}
        {discovery?.warnings.length ? (
          <Notice tone="warn" title="设备发现警告">
            {discovery.warnings.join('；')}
          </Notice>
        ) : null}

        {activeStep === 0 ? (
          <div className="pol-wizard-stage">
            <section className="pol-section">
              <h4 className="pol-section-title">策略基础</h4>
              <div className="pol-form-grid">
                <Field label="规则名称">
                  <Input value={ruleName} onChange={(value) => { markDraftChanged(); setRuleName(value) }} placeholder="例如：工作设备走主线路" disabled={busy} autoFocus />
                </Field>
                <Field label="优先级" hint="数字越小越先评估">
                  <Input type="number" min={0} value={rulePriority} onChange={(value) => { markDraftChanged(); setRulePriority(value) }} disabled={busy} />
                </Field>
                <Field label="启用状态">
                  <div className="pol-toggle-row">
                    <Toggle checked={enabled} disabled={busy} onChange={(value) => { markDraftChanged(); setEnabled(value) }} label="启用此规则" />
                    <span className="muted">{enabled ? '创建后立即启用' : '创建后保持停用'}</span>
                  </div>
                </Field>
              </div>
            </section>
            <section className="pol-section">
              <h4 className="pol-section-title">策略入口</h4>
              <p className="pol-hint">入口决定哪些接口进入的流量参与策略匹配；接口列表优先，被列表覆盖的成员不重复添加。仅指定终端/地址时可留空。</p>
              <div className="pol-choice-list">
                {(discovery?.trafficIngress ?? []).map((candidate) => {
                  const selected = candidate.kind === 'interface-list' ? trafficIngress.interfaceLists.includes(candidate.name) : trafficIngress.interfaces.includes(candidate.name)
                  const covered = candidate.kind !== 'interface-list' && candidate.coveredBy.some((name) => selectedLists.has(name))
                  return (
                    <label key={`${candidate.kind}:${candidate.name}`} className={`pol-choice${selected || covered ? ' pol-choice-active' : ''}${covered ? ' pol-choice-disabled' : ''}`}>
                      <input type="checkbox" checked={selected || covered} disabled={covered || busy} onChange={() => toggleIngress(candidate)} />
                      <span>
                        <strong>{candidate.name}</strong>
                        <small>
                          {kindLabelForIngress[candidate.kind] ?? candidate.kind}
                          {candidate.addresses.length ? ` · ${candidate.addresses.join(', ')}` : ''}
                          {candidate.reason ? ` · ${candidate.reason}` : ''}
                        </small>
                      </span>
                    </label>
                  )
                })}
                {discovery && !discovery.trafficIngress.length ? <p className="pol-hint">没有发现可用的入口接口。</p> : null}
              </div>
              {!hasIngress && requiresTrafficIngress(subject) ? (
                <Notice tone="warn">尚未选择入口。「全部终端」与「排除」模式必须先选择入口。</Notice>
              ) : null}
            </section>
            <section className="pol-section">
              <h4 className="pol-section-title">来源范围</h4>
              <SubjectSelector
                terminals={context.terminals}
                value={subject}
                allowExcluded
                excludedDisabled={!hasIngress}
                requireObservedAddress
                onChange={(next) => {
                  markDraftChanged()
                  setSubject(next)
                }}
              />
            </section>
            {strategyErrors.length ? (
              <Notice tone="warn" title="完成后才能继续">
                {strategyErrors.join('；')}
              </Notice>
            ) : null}
          </div>
        ) : null}

        {activeStep === 1 ? (
          <div className="pol-wizard-stage">
            <section className="pol-section">
              <h4 className="pol-section-title">访问目标</h4>
              <p className="pol-hint">复用现有目标库或应用预设；预设按域名 / IP 独立准备。</p>
              <TargetSelector
                deviceID={deviceID}
                targetLists={targetLists}
                selectedIDs={targetListIDs}
                onChange={(ids) => {
                  markDraftChanged()
                  setTargetListIDs(ids)
                }}
                onPresetPresentationChange={(value) => {
                  markDraftChanged()
                  setPresetPresentations(value)
                }}
                onCreateTargetList={setCreatingTargetKind}
              />
            </section>
            {targetErrors.length ? (
              <Notice tone="warn" title="访问目标校验未通过">
                <span>{targetErrors.join('；')}</span>
                {invalidTargetIDs.length ? (
                  <span className="pol-invalid-targets">
                    {invalidTargetIDs.map((id) => (
                      <button key={id} type="button" className="link-button" onClick={() => { markDraftChanged(); setTargetListIDs(targetListIDs.filter((targetID) => targetID !== id)) }}>
                        移除失效目标：{id}
                      </button>
                    ))}
                  </span>
                ) : null}
              </Notice>
            ) : null}
            {targetListIDs.length ? (
              <div className="pol-selected-summary">
                已选 {targetListIDs.length} 个目标：
                {targetNamesForReview(targetListIDs, targetLists, presetPresentations)}
              </div>
            ) : null}
          </div>
        ) : null}

        {activeStep === 2 ? (
          <div className="pol-wizard-stage">
            <section className="pol-section">
              <h4 className="pol-section-title">出口</h4>
              <div className="pol-choice-list" role="radiogroup" aria-label="出口方式">
                <label className={`pol-choice${egressChoice === 'existing' ? ' pol-choice-active' : ''}${context.egresses.length ? '' : ' pol-choice-disabled'}`}>
                  <input type="radio" checked={egressChoice === 'existing'} disabled={!context.egresses.length || busy} onChange={() => switchChoice('existing')} />
                  <span>
                    <strong>使用现有出口</strong>
                    <small>{context.egresses.length ? '等价出口会被自动复用；编辑共享出口时后台安全复制' : '还没有可用出口，请新建'}</small>
                  </span>
                </label>
                <label className={`pol-choice${egressChoice === 'new' ? ' pol-choice-active' : ''}`}>
                  <input type="radio" checked={egressChoice === 'new'} disabled={busy} onChange={() => switchChoice('new')} />
                  <span>
                    <strong>新建出口</strong>
                    <small>按协议族配置 WAN / 下一跳、路由与 NAT</small>
                  </span>
                </label>
              </div>
              {egressChoice === 'existing' && context.egresses.length ? (
                <div className="pol-egr-pick-list">
                  {context.egresses.map((egress) => (
                    <button
                      key={egress.id}
                      type="button"
                      className={`pol-egr-pick${draft.id === egress.id ? ' pol-egr-pick-active' : ''}`}
                      disabled={busy || egress.pendingDeletion}
                      onClick={() => selectExistingEgress(egress)}
                    >
                      <strong>{egress.name || '未命名出口'}</strong>
                      <small>{egressFamilySummaryLine(egress)}</small>
                      <span className="pol-egr-pick-badges">
                        {!egress.enabled ? <Badge tone="neutral">已停用</Badge> : egress.applied ? <Badge tone="ok">已应用</Badge> : <Badge tone="warn">待应用</Badge>}
                        {egress.pendingDeletion ? <Badge tone="warn">待删除</Badge> : null}
                      </span>
                    </button>
                  ))}
                </div>
              ) : null}
            </section>
            <section className="pol-section">
              <h4 className="pol-section-title">{egressChoice === 'existing' ? '出口配置（可调整，保存时自动复用或复制）' : '新出口配置'}</h4>
              <EgressFields draft={draft} discovery={discovery} onChange={updateDraft} readOnly={busy} />
            </section>
            {egressErrors.length ? (
              <Notice tone="warn" title="完成后才能继续">
                {egressErrors.join('；')}
              </Notice>
            ) : null}
          </div>
        ) : null}

        {activeStep === 3 ? (
          <div className="pol-wizard-stage">
            {generating ? <Notice tone="info">正在根据当前配置生成变更计划…</Notice> : null}
            {!generating && plan && planFresh ? (
              <PlanReviewBody
                deviceID={deviceID}
                envelope={plan}
                summary={planSummary ?? undefined}
                onBack={() => setActiveStep(2)}
                onRepreview={() => void generateCurrentPlan()}
                onBusyChange={setApplying}
                onApplied={async () => {
                  await onSaved()
                }}
              />
            ) : null}
            {!generating && (!plan || !planFresh) && !error ? (
              <Notice tone="warn" title="预览需要更新">
                配置已修改，点击「审查并应用」步骤重新生成预览。
              </Notice>
            ) : null}
          </div>
        ) : null}
      </Modal>
      {creatingTargetKind ? (
        <TargetListModal
          deviceID={deviceID}
          target={null}
          initialKind={creatingTargetKind}
          onClose={() => setCreatingTargetKind(null)}
          onSaved={({ targetList }) => {
            markDraftChanged()
            setTargetLists((current) => (current.some((item) => item.id === targetList.id) ? current.map((item) => (item.id === targetList.id ? targetList : item)) : [...current, targetList]))
            setTargetListIDs((current) => (current.includes(targetList.id) ? current : [...current, targetList.id]))
            setCreatingTargetKind(null)
          }}
        />
      ) : null}
    </>
  )
}
