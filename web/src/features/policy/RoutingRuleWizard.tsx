import { useEffect, useMemo, useRef, useState } from 'react'
import { fetchPolicyDiscovery, generatePolicyPlan, type ApplicationPresetSelection, type Egress, type EgressFamily, type PlanEnvelope, type PolicyDiscovery, type PolicyPlanProposal, type RoutingRule, type Subject, type PolicyTerminal, type TargetList, type TrafficIngressScope } from './canonical'
import { SubjectSelector, TargetSelector } from './Selectors'
import { TargetListModal } from './TargetLibraryPage'
import { gatewayCandidatesForWAN, suggestedGatewayForWAN } from './gateway'
import { PolicyPlanPreview, type PolicyPlanSummary } from './PolicyPlanPreview'
import { hasTrafficIngress, requiresTrafficIngress, sourceIsValid } from './source'
import { PolicyErrorDisplay, PolicyField, PolicyModal, PolicyNotice, PolicyWizardSteps } from '../policy-routing/components'

const nextHopValue = '__rosboard_next_hop__'

type WizardContext = { egresses: Egress[]; rules: RoutingRule[]; targetLists: TargetList[]; terminals: PolicyTerminal[]; trafficIngress: TrafficIngressScope }

function defaultFamily(family: 'ipv4' | 'ipv6'): EgressFamily {
  return { family, enabled: family === 'ipv4', wanInterface: '', gateway: '', routeTable: '', routeMode: '', natMode: '', wanSource: '' }
}

function defaultEgress(): Egress {
  return { id: '', name: '', priority: 100, listMode: 'shared', listName: '', dnsUpstream: '1.1.1.1', fakeAlias: '', failureMode: 'strict', routerOutput: false, enabled: true, pendingDeletion: false, revision: 0, applied: false, families: [defaultFamily('ipv4')] }
}

function draftFromEgress(egress: Egress): Egress {
  return { ...egress, families: egress.families.length ? egress.families.map((family) => ({ ...family })) : [defaultFamily('ipv4')] }
}

function subjectForRule(rule: RoutingRule | null): Subject {
  return rule?.subject ?? { mode: 'all', members: [], prefixes: [] }
}

function egressErrors(draft: Egress, discovery: PolicyDiscovery | null): string[] {
  const errors: string[] = []
  const enabledFamilies = draft.families.filter((family) => family.enabled)
  if (!enabledFamilies.length) errors.push('至少启用一个地址族')
  if (!discovery?.available) errors.push(discovery?.reason || '设备发现不可用，暂不能配置出口')
  for (const family of enabledFamilies) {
    if (family.wanSource === 'next-hop') {
      if (!family.gateway.trim()) errors.push(`${family.family.toUpperCase()} 下一跳模式必须填写网关 IP`)
      continue
    }
    if (!family.wanInterface.trim()) {
      errors.push(`${family.family.toUpperCase()} 必须选择已发现的 WAN 接口`)
      continue
    }
    const wan = discovery?.wans.find((candidate) => candidate.interface === family.wanInterface)
    if (!wan) {
      errors.push(`${family.family.toUpperCase()} 的 WAN 接口未被设备发现`)
    } else if (!wan.pointToPoint && !family.gateway.trim()) {
      errors.push(`${family.family.toUpperCase()} 未发现唯一下一跳，请填写网关 IP`)
    }
  }
  return errors
}

type PresetPresentation = ApplicationPresetSelection & { name: string }

function targetKindLabel(kind: 'domain' | 'ip') { return kind === 'ip' ? 'IP' : '域名' }

function routeModeLabel(family: EgressFamily, egress: Egress) {
  const mode = family.routeMode || egress.failureMode
  if (mode === 'strict') return '严格断线阻断'
  if (mode === 'fallback') return '允许回落 main'
  if (mode === 'existing') return '沿用现有规则'
  return '跟随全局设置'
}

function egressFamilySummary(family: EgressFamily, egress: Egress, discovery: PolicyDiscovery | null) {
  const wan = discovery?.wans.find((candidate) => candidate.interface === family.wanInterface)
  const endpoint = family.wanSource === 'next-hop' || !family.wanInterface
    ? `下一跳：${family.gateway || '未设置'}`
    : `WAN：${family.wanInterface}${wan?.pointToPoint ? ' · 点对点（无需网关）' : ` · 网关：${family.gateway || '未设置'}`}`
  return `${family.family.toUpperCase()} · ${endpoint} · 路由表：${family.routeTable || '自动专用表'} · ${routeModeLabel(family, egress)}`
}

export function RoutingRuleWizard({ deviceID, context, rule, egress, onClose, onSaved }: { deviceID: string; context: WizardContext; rule: RoutingRule | null; egress?: Egress | null; onClose: () => void; onSaved: () => Promise<void> }) {
  const initialEgress = egress ?? (rule ? context.egresses.find((candidate) => candidate.id === rule.egressId) : undefined)
  const wizardSteps = ['策略与来源', '访问目标', '出口', '预览并应用']
  const [activeStep, setActiveStep] = useState(0)
  const [maxUnlockedStep, setMaxUnlockedStep] = useState(rule ? 2 : 0)
  const [draft, setDraft] = useState<Egress>(() => initialEgress ? draftFromEgress(initialEgress) : defaultEgress())
  const [ruleName, setRuleName] = useState(rule?.name ?? '')
  const [subject, setSubject] = useState<Subject>(() => subjectForRule(rule))
  const [trafficIngress, setTrafficIngress] = useState<TrafficIngressScope>(() => ({ interfaceLists: [...(rule?.ingress?.interfaceLists ?? context.trafficIngress.interfaceLists)], interfaces: [...(rule?.ingress?.interfaces ?? context.trafficIngress.interfaces)] }))
  const [discovery, setDiscovery] = useState<PolicyDiscovery | null>(null)
  const [discoveryError, setDiscoveryError] = useState<unknown>(null)
  const [targetListIDs, setTargetListIDs] = useState<string[]>(() => [...(rule?.targetListIds ?? [])])
  const [targetLists, setTargetLists] = useState<TargetList[]>(() => [...context.targetLists])
  const [creatingTargetKind, setCreatingTargetKind] = useState<'domain' | 'ip' | null>(null)
  const [presetPresentations, setPresetPresentations] = useState<PresetPresentation[]>([])
  const [rulePriority, setRulePriority] = useState(String(rule?.priority ?? 100))
  const [enabled, setEnabled] = useState(rule?.enabled ?? true)
  const [generating, setGenerating] = useState(false)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [plan, setPlan] = useState<PlanEnvelope | null>(null)
  const [planSummary, setPlanSummary] = useState<PolicyPlanSummary | null>(null)
  const [draftRevision, setDraftRevision] = useState(0)
  const [planDraftRevision, setPlanDraftRevision] = useState<number | null>(null)
  const draftRevisionRef = useRef(0)
  const generationInFlightRef = useRef(false)
  const [ingressDefaulted, setIngressDefaulted] = useState(false)

  useEffect(() => {
    let active = true
    void fetchPolicyDiscovery(deviceID).then((value) => { if (active) { setDiscovery(value); setDiscoveryError(null) } }).catch((loadError) => { if (active) setDiscoveryError(loadError) })
    return () => { active = false }
  }, [deviceID])

  useEffect(() => {
    if (ingressDefaulted || !discovery?.available || subject.mode === 'selected') return
    setIngressDefaulted(true)
    if (hasTrafficIngress(trafficIngress)) return
    const defaultList = discovery.trafficIngress.find((candidate) => candidate.kind === 'interface-list' && candidate.default)
    if (defaultList) {
      const next = { interfaceLists: [defaultList.name], interfaces: [] }
      setTrafficIngress(next)
    }
  }, [discovery, ingressDefaulted, subject.mode, trafficIngress])

  const strategyErrors = useMemo(() => {
    const next: string[] = []
    if (!ruleName.trim()) next.push('规则名称不能为空')
    if (!sourceIsValid(subject, trafficIngress)) next.push('来源范围配置不完整，请选择有效入口或来源设备/地址')
    return next
  }, [ruleName, subject, trafficIngress])
  const invalidTargetIDs = useMemo(() => {
    const targetByID = new Map(targetLists.map((target) => [target.id, target]))
    return targetListIDs.filter((id) => !id.startsWith('preset:') && (!targetByID.get(id) || targetByID.get(id)?.pendingDeletion))
  }, [targetListIDs, targetLists])
  const targetErrors = useMemo(() => {
    if (!targetListIDs.length) return ['至少选择一个访问目标']
    return invalidTargetIDs.length ? [`以下访问目标已不可用，请重新选择：${invalidTargetIDs.join('、')}`] : []
  }, [invalidTargetIDs, targetListIDs.length])
  const errors = useMemo(() => egressErrors(draft, discovery), [discovery, draft])
  const hasIngress = hasTrafficIngress(trafficIngress)
  const busy = generating || applying
  const planFresh = Boolean(plan && planDraftRevision === draftRevision)
  const stepLocked = activeStep < 3 && activeStep > maxUnlockedStep

  const markDraftChanged = () => {
    draftRevisionRef.current += 1
    setDraftRevision(draftRevisionRef.current)
    setError(null)
  }

  const updateDraft = (patch: Partial<Egress>) => {
    markDraftChanged()
    setDraft((current) => ({ ...current, ...patch }))
  }
  const updateFamily = (family: EgressFamily) => {
    markDraftChanged()
    setDraft((current) => ({ ...current, families: current.families.some((candidate) => candidate.family === family.family) ? current.families.map((candidate) => candidate.family === family.family ? family : candidate) : [...current.families, family] }))
  }

  const updateSubject = (next: Subject) => {
    markDraftChanged()
    setSubject(next)
  }

  const updateRuleName = (next: string) => {
    markDraftChanged()
    setRuleName(next)
  }

  const updateRulePriority = (next: string) => {
    markDraftChanged()
    setRulePriority(next)
  }

  const updateEnabled = (next: boolean) => {
    markDraftChanged()
    setEnabled(next)
  }

  const addTargetList = (target: TargetList) => {
    markDraftChanged()
    setTargetLists((current) => current.some((item) => item.id === target.id) ? current.map((item) => item.id === target.id ? target : item) : [...current, target])
    setTargetListIDs((current) => current.includes(target.id) ? current : [...current, target.id])
    setCreatingTargetKind(null)
  }

  const toggleIngress = (candidate: PolicyDiscovery['trafficIngress'][number]) => {
    markDraftChanged()
    if (candidate.kind === 'interface-list') {
      const selected = trafficIngress.interfaceLists.includes(candidate.name)
      setTrafficIngress((current) => ({ interfaceLists: selected ? current.interfaceLists.filter((name) => name !== candidate.name) : [...current.interfaceLists, candidate.name], interfaces: selected ? current.interfaces : current.interfaces.filter((name) => !candidate.include.includes(name) && !candidate.staticMembers.includes(name)) }))
      return
    }
    setTrafficIngress((current) => ({ ...current, interfaces: current.interfaces.includes(candidate.name) ? current.interfaces.filter((name) => name !== candidate.name) : [...current.interfaces, candidate.name] }))
  }

  const updateTargetLists = (next: string[]) => {
    markDraftChanged()
    setTargetListIDs(next)
  }

  const updatePresetPresentations = (next: PresetPresentation[]) => {
    markDraftChanged()
    setPresetPresentations(next)
  }

  const firstInvalidStep = () => {
    if (strategyErrors.length) return 0
    if (targetErrors.length) return 1
    if (errors.length) return 2
    return null
  }

  const generateCurrentPlan = async () => {
    if (generationInFlightRef.current || applying) return
    const invalidStep = firstInvalidStep()
    if (invalidStep !== null) {
      setError(null)
      setActiveStep(invalidStep)
      return
    }

    const requestRevision = draftRevisionRef.current
    const proposal: PolicyPlanProposal = {
      egress: draft,
      routingRule: { id: rule?.id ?? '', name: ruleName.trim(), subject, ingress: trafficIngress, targetListIds: targetListIDs, egressId: draft.id, priority: Number(rulePriority) || 0, enabled, revision: rule?.revision ?? 0 },
      ...(presetPresentations.some((selection) => selection.previewId) ? { presetSelections: presetPresentations.filter((selection) => selection.previewId) } : {}),
    }

    generationInFlightRef.current = true
    setGenerating(true)
    setError(null)
    setActiveStep(3)
    try {
      const envelope = await generatePolicyPlan(deviceID, context.egresses.length ? 'structural' : 'initial', proposal)
      if (requestRevision !== draftRevisionRef.current) {
        setError(new Error('配置在生成期间发生变化，请重新生成预览。'))
        return
      }
      setPlanSummary({ entries: planSummaryEntries(draft, discovery, trafficIngress, subject, targetListIDs, targetLists, presetPresentations, rulePriority, enabled) })
      setPlan(envelope)
      setPlanDraftRevision(requestRevision)
      setMaxUnlockedStep(3)
    } catch (saveError) {
      setError(saveError)
    } finally {
      generationInFlightRef.current = false
      setGenerating(false)
    }
  }

  const previewReachable = Boolean(rule) || maxUnlockedStep >= 3
  const handleStepJump = (index: number) => {
    if (busy) return
    setError(null)
    if (index === 3) {
      if (!previewReachable) {
        setActiveStep(3)
        return
      }
      if (planFresh) {
        setActiveStep(3)
        return
      }
      void generateCurrentPlan()
      return
    }
    setActiveStep(index)
  }

  const advanceFromStep = () => {
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

  const title = rule ? `编辑策略：${rule.name}` : '新增策略'
  const isPreview = activeStep === 3 && planFresh && !generating
  const primaryDisabled = busy || stepLocked || (activeStep === 0 && strategyErrors.length > 0) || (activeStep === 1 && targetErrors.length > 0)
  return <>
    <PolicyModal title={title} subtitle="完成配置后直接生成可审阅的 RouterOS 变更计划，确认后才会应用。" wide closeDisabled={busy} onClose={() => { if (!busy) onClose() }} header={<PolicyWizardSteps steps={wizardSteps} current={activeStep} unlockedThrough={previewReachable ? 3 : maxUnlockedStep} planStale={Boolean(plan && (!planFresh || generating))} disabled={busy} onJump={handleStepJump} />} footer={isPreview ? undefined : <div className="policy-form-actions policy-wizard-nav policy-wizard-footer"><button type="button" className="close-button" disabled={busy} onClick={activeStep === 0 ? onClose : () => setActiveStep((current) => Math.max(0, current - 1))}>{activeStep === 0 ? '取消' : '上一步'}</button>{activeStep < 2 ? <button type="button" className="primary-button" disabled={primaryDisabled} onClick={advanceFromStep}>下一步</button> : activeStep === 2 ? <button type="button" className="primary-button" disabled={primaryDisabled} onClick={advanceFromStep}>{generating ? '生成计划中…' : '生成变更计划'}</button> : null}</div>}>
    {error ? <PolicyErrorDisplay error={error} /> : null}
    {discoveryError ? <PolicyErrorDisplay error={discoveryError} /> : null}
    {activeStep < 3 && discovery && !discovery.available ? <PolicyNotice tone="warn" title="设备发现不可用">{discovery.reason || '无法读取 RouterOS WAN 与入口候选。'}</PolicyNotice> : null}
    {activeStep < 3 && discovery?.warnings.length ? <PolicyNotice tone="warn" title="设备发现警告">{discovery.warnings.join('；')}</PolicyNotice> : null}
    {activeStep === 3 && (!planFresh || generating) ? <PolicyNotice tone={previewReachable ? 'warn' : 'info'} title={generating ? '正在生成预览' : previewReachable ? '预览需要更新' : '完成前面的步骤后才能生成预览'}>{previewReachable ? (generating ? '正在根据当前草稿生成预览…' : '草稿已修改，点击“预览并应用”标题会重新校验并刷新预览。') : '请先完成当前步骤并点击“生成变更计划”。'}</PolicyNotice> : null}
    {isPreview ? <PolicyPlanPreview deviceID={deviceID} envelope={plan!} summary={planSummary ?? undefined} onBusyChange={setApplying} onBack={() => setActiveStep(2)} onApplied={async () => { await onSaved(); onClose() }} /> : null}
    {activeStep === 0 ? <><StepLockedNotice locked={stepLocked} /><fieldset className="policy-wizard-fields" disabled={busy || stepLocked}><StrategyAndSourceStep ruleName={ruleName} rulePriority={rulePriority} ruleEnabled={enabled} errors={strategyErrors} onRuleName={updateRuleName} onRulePriority={updateRulePriority} onRuleEnabled={updateEnabled} discovery={discovery} ingress={trafficIngress} subject={subject} terminals={context.terminals} hasIngress={hasIngress} onIngress={toggleIngress} onSubject={updateSubject} /></fieldset></> : null}
    {activeStep === 1 ? <><StepLockedNotice locked={stepLocked} /><fieldset className="policy-wizard-fields" disabled={busy || stepLocked}><TargetStep deviceID={deviceID} targetLists={targetLists} targetListIDs={targetListIDs} onTargetLists={updateTargetLists} onRemoveTarget={(id) => updateTargetLists(targetListIDs.filter((targetID) => targetID !== id))} onPresetPresentation={updatePresetPresentations} onCreateTargetList={setCreatingTargetKind} errors={targetErrors} invalidTargetIDs={invalidTargetIDs} /></fieldset></> : null}
    {activeStep === 2 ? <><StepLockedNotice locked={stepLocked} /><fieldset className="policy-wizard-fields" disabled={busy || stepLocked}><EgressStep draft={draft} discovery={discovery} errors={errors} onDraft={updateDraft} onFamily={updateFamily} readOnly={busy || stepLocked} /></fieldset></> : null}
  </PolicyModal>
  {creatingTargetKind ? <TargetListModal deviceID={deviceID} target={null} initialKind={creatingTargetKind} onClose={() => setCreatingTargetKind(null)} onSaved={async (target) => { addTargetList(target) }} /> : null}
  </>
}

function StepLockedNotice({ locked }: { locked: boolean }) {
  return locked ? <PolicyNotice tone="info" title="此步骤仅供查看">请先完成前面的步骤并点击“下一步”后再编辑。</PolicyNotice> : null
}

function StrategyAndSourceStep({ ruleName, rulePriority, ruleEnabled, errors, onRuleName, onRulePriority, onRuleEnabled, discovery, ingress, subject, terminals, hasIngress, onIngress, onSubject }: { ruleName: string; rulePriority: string; ruleEnabled: boolean; errors: string[]; onRuleName: (name: string) => void; onRulePriority: (priority: string) => void; onRuleEnabled: (enabled: boolean) => void; discovery: PolicyDiscovery | null; ingress: TrafficIngressScope; subject: Subject; terminals: PolicyTerminal[]; hasIngress: boolean; onIngress: (candidate: PolicyDiscovery['trafficIngress'][number]) => void; onSubject: (subject: Subject) => void }) {
  const otherErrors = errors.filter((error) => error !== '规则名称不能为空')
  return <div className="policy-wizard-stage"><section className="policy-section-card"><h4 className="policy-section-card-title">策略基础</h4><PolicyField label="策略名称" htmlFor="routing-rule-name" error={errors.find((error) => error === '规则名称不能为空')}><input id="routing-rule-name" className="settings-input" value={ruleName} onChange={(event) => onRuleName(event.target.value)} placeholder="例如：工作设备走主线路" /></PolicyField><div className="policy-form-grid"><PolicyField label="规则优先级" hint="数字越小越先评估；不用于隐式解决冲突。"><input className="settings-input" type="number" min="0" value={rulePriority} onChange={(event) => onRulePriority(event.target.value)} /></PolicyField><label className="policy-checkbox"><input type="checkbox" checked={ruleEnabled} onChange={(event) => onRuleEnabled(event.target.checked)} /><span>启用此策略</span></label></div></section><SourceStep discovery={discovery} ingress={ingress} subject={subject} terminals={terminals} hasIngress={hasIngress} onIngress={onIngress} onSubject={onSubject} />{otherErrors.length ? <PolicyNotice tone="warn" title="策略与来源还有问题">{otherErrors.join('；')}</PolicyNotice> : null}</div>
}

function EgressStep({ draft, discovery, errors, onDraft, onFamily, readOnly }: { draft: Egress; discovery: PolicyDiscovery | null; errors: string[]; onDraft: (patch: Partial<Egress>) => void; onFamily: (family: EgressFamily) => void; readOnly: boolean }) {
  const updateFamily = (family: 'ipv4' | 'ipv6', patch: Partial<EgressFamily>) => {
    const current = draft.families.find((candidate) => candidate.family === family) ?? defaultFamily(family)
    onFamily({ ...current, ...patch })
  }
  const familyEnabled = (family: 'ipv4' | 'ipv6') => draft.families.some((candidate) => candidate.family === family && candidate.enabled)
  return <div className="policy-wizard-stage">
    <section className="policy-section-card"><h4 className="policy-section-card-title">策略出口</h4><p className="policy-hint">出口按地址族、WAN/下一跳和路由行为配置。保存时会自动生成内部标识并复用等价出口；编辑共享出口时会安全执行 Copy-on-Write。</p>{(['ipv4', 'ipv6'] as const).map((family) => <FamilyEditor key={`${draft.id || 'new'}:${family}`} family={family} value={draft.families.find((candidate) => candidate.family === family) ?? defaultFamily(family)} enabled={familyEnabled(family)} discovery={discovery} readOnly={readOnly} onEnabled={(enabled) => updateFamily(family, { enabled })} onChange={(patch) => updateFamily(family, patch)} />)}</section>
    <details className="settings-disclosure policy-advanced"><summary className="settings-disclosure-summary">高级配置（DNS、断线处理、路由表、NAT 与 RouterOS 本机流量）</summary><div className="settings-disclosure-body policy-advanced-body"><div className="policy-form-grid"><PolicyField label="断线处理"><select className="select-control" value={draft.failureMode} onChange={(event) => onDraft({ failureMode: event.target.value })}><option value="strict">断线阻断</option><option value="fallback">回落 main</option><option value="existing">沿用现有规则</option></select></PolicyField><PolicyField label="DNS 上游"><input className="settings-input" value={draft.dnsUpstream} onChange={(event) => onDraft({ dnsUpstream: event.target.value })} placeholder="1.1.1.1" /></PolicyField><PolicyField label="Fake DNS 别名"><input className="settings-input" value={draft.fakeAlias} onChange={(event) => onDraft({ fakeAlias: event.target.value })} placeholder="自动分配" /></PolicyField></div>{draft.families.filter((family) => family.enabled).map((family) => <div className="policy-form-grid" key={`advanced-${family.family}`}><PolicyField label={`${family.family.toUpperCase()} 路由表`}><input className="settings-input" value={family.routeTable} onChange={(event) => onFamily({ ...family, routeTable: event.target.value })} placeholder="自动创建专用表" /></PolicyField><PolicyField label={`${family.family.toUpperCase()} 断线覆盖`}><select className="select-control" value={family.routeMode} onChange={(event) => onFamily({ ...family, routeMode: event.target.value })}><option value="">跟随全局设置</option><option value="strict">严格绑定</option><option value="fallback">允许回落 main</option></select></PolicyField><PolicyField label={`${family.family.toUpperCase()} NAT 模式`} hint="NAT 只在明确选择时表达；RouterOS 现有的外部 srcnat 规则不会被覆盖。"><select className="select-control" value={family.natMode} onChange={(event) => onFamily({ ...family, natMode: event.target.value })}><option value="">沿用现有 NAT</option><option value="none">不建立 NAT</option><option value="masquerade">动态 masquerade</option></select></PolicyField></div>)}<label className="policy-checkbox"><input type="checkbox" checked={draft.routerOutput} onChange={(event) => onDraft({ routerOutput: event.target.checked })} /><span>包含 RouterOS 本机流量</span></label>{draft.id ? <label className="policy-checkbox"><input type="checkbox" checked={draft.enabled} onChange={(event) => onDraft({ enabled: event.target.checked })} /><span>启用此出口</span></label> : null}</div></details>
    {errors.length ? <PolicyNotice tone="warn" title="完成后才能继续">{errors.join('；')}</PolicyNotice> : null}
  </div>
}

function FamilyEditor({ family, value, enabled, discovery, readOnly, onEnabled, onChange }: { family: 'ipv4' | 'ipv6'; value: EgressFamily; enabled: boolean; discovery: PolicyDiscovery | null; readOnly: boolean; onEnabled: (enabled: boolean) => void; onChange: (patch: Partial<EgressFamily>) => void }) {
  const wans = discovery?.wans ?? []
  const selectedWAN = wans.find((wan) => wan.interface === value.wanInterface)
  const gatewayCandidates = gatewayCandidatesForWAN(selectedWAN, family)
  const suggestedGateway = suggestedGatewayForWAN(selectedWAN, family)
  const pointToPoint = Boolean(selectedWAN?.pointToPoint)
  const nextHop = value.wanSource === 'next-hop'
  const [gatewayManuallyEdited, setGatewayManuallyEdited] = useState(() => Boolean(value.gateway.trim() && !pointToPoint && value.gateway !== suggestedGateway))

  useEffect(() => {
    setGatewayManuallyEdited(Boolean(value.gateway.trim() && !pointToPoint && value.gateway !== suggestedGateway))
  }, [pointToPoint, suggestedGateway, value.gateway, value.wanInterface, value.wanSource])

  useEffect(() => {
    if (readOnly || nextHop || gatewayManuallyEdited || !value.wanInterface.trim()) return
    const gateway = pointToPoint ? '' : suggestedGateway
    if (value.gateway !== gateway) onChange({ gateway })
  }, [gatewayManuallyEdited, nextHop, onChange, pointToPoint, readOnly, suggestedGateway, value.gateway, value.wanInterface])

  const selectInterface = (selected: string) => {
    if (selected === nextHopValue) {
      setGatewayManuallyEdited(false)
      onChange({ wanSource: 'next-hop', wanInterface: '', gateway: '' })
      return
    }
    const nextWAN = wans.find((wan) => wan.interface === selected)
    setGatewayManuallyEdited(false)
    onChange({ wanSource: '', wanInterface: selected, gateway: suggestedGatewayForWAN(nextWAN, family) })
  }
  const selectValue = nextHop ? nextHopValue : value.wanInterface
  const gatewayRequired = nextHop || Boolean(value.wanInterface.trim() && !pointToPoint)
  const gatewayHint = nextHop
    ? '下一跳模式必须显式填写同地址族的网关 IP。'
    : pointToPoint
      ? '点对点接口无需填写网关。'
      : suggestedGateway
        ? '已从 main 表活动默认路由自动填入，可手动修改。'
        : '未发现唯一的下一跳网关，必须手动填写 IP。'

  return <div className="policy-family-block"><label className="policy-family-head checkbox-label"><input type="checkbox" checked={enabled} onChange={(event) => onEnabled(event.target.checked)} /><span>启用 {family.toUpperCase()}</span></label>{enabled ? <div className="policy-form-grid"><PolicyField label="策略 WAN 接口"><select className="select-control" value={selectValue} onChange={(event) => selectInterface(event.target.value)}><option value="">选择已发现接口</option>{!wans.some((wan) => wan.interface === value.wanInterface) && value.wanInterface ? <option value={value.wanInterface}>{value.wanInterface}（当前未发现）</option> : null}{wans.map((wan) => <option key={wan.interface} value={wan.interface}>{wan.interface}（{wan.type || '未知'}{wan.running ? '，运行中' : ''}）</option>)}<option value={nextHopValue}>下一跳网关</option></select></PolicyField><PolicyField label={`下一跳网关（${family.toUpperCase()}）`} hint={gatewayHint}><input className="settings-input" required={gatewayRequired} value={value.gateway} list={`routing-gateway-${family}`} onChange={(event) => { setGatewayManuallyEdited(true); onChange({ gateway: event.target.value }) }} placeholder={gatewayRequired ? '填写网关 IP' : '点对点接口无需填写'} /><datalist id={`routing-gateway-${family}`}>{gatewayCandidates.map((gateway) => <option key={gateway} value={gateway} />)}</datalist>{gatewayManuallyEdited && !nextHop ? <p className="policy-hint"><button type="button" className="link-button" onClick={() => { setGatewayManuallyEdited(false); onChange({ gateway: suggestedGateway }) }}>恢复自动发现</button></p> : null}{gatewayRequired && !value.gateway.trim() ? <p className="policy-field-error">{nextHop ? '下一跳模式必须填写网关 IP。' : '普通接口未发现唯一网关，请填写下一跳 IP。'}</p> : null}</PolicyField></div> : null}</div>
}

function SourceStep({ discovery, ingress, subject, terminals, hasIngress, onIngress, onSubject }: { discovery: PolicyDiscovery | null; ingress: TrafficIngressScope; subject: Subject; terminals: PolicyTerminal[]; hasIngress: boolean; onIngress: (candidate: PolicyDiscovery['trafficIngress'][number]) => void; onSubject: (subject: Subject) => void }) {
  const kindLabel: Record<string, string> = { 'interface-list': '接口列表', bridge: 'Bridge', vlan: 'VLAN', wireguard: 'WireGuard', vpn: 'VPN', tunnel: '隧道', physical: '物理接口' }
  const selectedLists = new Set(ingress.interfaceLists)
  return <div className="policy-wizard-stage"><section className="policy-section-card"><h4 className="policy-section-card-title">策略入口</h4><p className="policy-hint">入口属于当前策略。选择接口列表优先；被列表覆盖的成员不再重复添加。仅选择设备或手动地址时，入口可留空。</p><div className="policy-choice-list">{(discovery?.trafficIngress ?? []).map((candidate) => { const selected = candidate.kind === 'interface-list' ? ingress.interfaceLists.includes(candidate.name) : ingress.interfaces.includes(candidate.name); const covered = candidate.kind !== 'interface-list' && candidate.coveredBy.some((name) => selectedLists.has(name)); return <label key={`${candidate.kind}:${candidate.name}`} className={`policy-choice${selected || covered ? ' active' : ''}${covered ? ' policy-choice-disabled' : ''}`}><input type="checkbox" checked={selected || covered} disabled={covered} onChange={() => onIngress(candidate)} /><span><strong>{candidate.name}</strong><small>{kindLabel[candidate.kind] ?? candidate.kind}{candidate.addresses.length ? ` · ${candidate.addresses.join(', ')}` : ''}{candidate.reason ? ` · ${candidate.reason}` : ''}</small></span></label> })}</div>{!hasIngress && requiresTrafficIngress(subject) ? <PolicyNotice tone="warn">尚未选择入口。全部设备与排除模式必须先选择入口。</PolicyNotice> : null}</section><section className="policy-section-card"><h4 className="policy-section-card-title">来源范围</h4><SubjectSelector terminals={terminals} value={subject} allowExcluded excludedDisabled={!hasIngress} requireObservedAddress onChange={onSubject} /></section></div>
}

function TargetStep({ deviceID, targetLists, targetListIDs, onTargetLists, onRemoveTarget, onPresetPresentation, onCreateTargetList, errors, invalidTargetIDs }: { deviceID: string; targetLists: TargetList[]; targetListIDs: string[]; onTargetLists: (ids: string[]) => void; onRemoveTarget: (id: string) => void; onPresetPresentation: (value: PresetPresentation[]) => void; onCreateTargetList: (kind: 'domain' | 'ip') => void; errors: string[]; invalidTargetIDs: string[] }) {
  return <div className="policy-wizard-stage"><section className="policy-section-card"><h4>访问目标</h4><p className="policy-hint">普通目标列表和应用规则从这里复用。应用规则按域名 / IP 独立准备，Target Library 不会显示 backing rows。</p><TargetSelector deviceID={deviceID} targetLists={targetLists} selectedIDs={targetListIDs} onChange={onTargetLists} onPresetPresentationChange={onPresetPresentation} onCreateTargetList={onCreateTargetList} />{errors.length ? <PolicyNotice tone="warn" title="访问目标校验未通过"><span>{errors.join('；')}</span>{invalidTargetIDs.length ? <span className="policy-invalid-targets">{invalidTargetIDs.map((id) => <button key={id} type="button" className="link-button" onClick={() => onRemoveTarget(id)}>移除失效目标：{id}</button>)}</span> : null}</PolicyNotice> : null}</section></div>
}

function targetNamesForReview(targetListIDs: string[], targetLists: TargetList[], presetPresentations: PresetPresentation[]) {
  const names: string[] = []
  const presetIDs = new Set<string>()
  for (const id of targetListIDs) {
    const match = /^preset:([^:]+):(domain|ip)$/.exec(id)
    if (match) {
      presetIDs.add(match[1])
      continue
    }
    names.push(targetLists.find((target) => target.id === id)?.name ?? '目标列表')
  }
  for (const presentation of presetPresentations) {
    const kinds = presentation.requestedKinds.filter((kind) => targetListIDs.includes(`preset:${presentation.presetId}:${kind}`))
    if (!kinds.length) continue
    const label = kinds.length === 2 ? '域名/IP' : targetKindLabel(kinds[0])
    names.push(`${presentation.name} · ${label}`)
    presetIDs.delete(presentation.presetId)
  }
  for (const presetID of presetIDs) {
    const target = targetLists.find((candidate) => candidate.id.startsWith(`preset:${presetID}:`) && targetListIDs.includes(candidate.id))
    if (target) names.push(target.name)
    else names.push('应用规则')
  }
  return names.join('、')
}

function planSummaryEntries(draft: Egress, discovery: PolicyDiscovery | null, ingress: TrafficIngressScope, subject: Subject, targetListIDs: string[], targetLists: TargetList[], presetPresentations: PresetPresentation[], rulePriority: string, ruleEnabled: boolean): Array<[string, string]> {
  const entries: Array<[string, string]> = [
    ['地址族', draft.families.filter((family) => family.enabled).map((family) => family.family.toUpperCase()).join('/') || '无地址族'],
    ['WAN / 下一跳', draft.families.filter((family) => family.enabled).map((family) => egressFamilySummary(family, draft, discovery)).join('；') || '—'],
    ['TrafficIngress', [...ingress.interfaceLists, ...ingress.interfaces].join('、') || 'source-only / 未设置'],
    ['来源', subject.mode === 'all' ? '全部设备' : subject.mode === 'excluded' ? `入口内排除 ${subject.members.length} 台设备 / ${subject.prefixes.length} 个地址范围` : `${subject.members.length} 台设备 / ${subject.prefixes.length} 个地址范围`],
  ]
  entries.push(['规则', `${ruleEnabled ? '启用' : '停用'} · Priority ${rulePriority}`], ['目标', targetNamesForReview(targetListIDs, targetLists, presetPresentations) || '—'])
  return entries
}
