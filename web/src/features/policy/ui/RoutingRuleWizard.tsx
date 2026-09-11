import { useEffect, useMemo, useRef, useState } from 'react'
import { Button } from '../../../ui/Button'
import { Field, Input } from '../../../ui/inputs'
import { Modal } from '../../../ui/Modal'
import { Toggle } from '../../../ui/Toggle'
import {
  fetchPolicyDiscoverySnapshot,
  generatePolicyPlan,
  type ApplicationPresetSelection,
  type Egress,
  type PlanEnvelope,
  type PolicyDiscovery,
  type PolicyPlanProposal,
  type PolicyTerminal,
  type RoutingRule,
  type RoutingSourceKind,
  type Subject,
  type TargetList,
  type TrafficIngressScope,
} from '../canonical'
import { hasTrafficIngress, parseSourcePrefixLines, sourceIsValid, typedSourceIsValid, type RoutingInterfaceSource } from '../source'
import { EgressFields } from './EgressFields'
import { defaultEgressDraft, egressDraftErrors, egressDraftFrom } from './egressDraft'
import { errorMessage } from '../../../lib/api'
import { Notice } from './Notice'
import { PlanReviewBody } from './PlanReview'
import type { PlanSummaryEntry } from './PlanReview'
import { RoutingSourcePicker, type RoutingSourceSelectionKind } from './RoutingSourcePicker'
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

const STEPS = ['基础信息与源地址', '访问目标', '出口', '审查并应用']

function targetKindLabel(kind: 'domain' | 'ip') {
  return kind === 'ip' ? 'IP' : '域名'
}

function KeywordDomainSetting({ targetLists, targetListIDs, enabled, onChange }: { targetLists: TargetList[]; targetListIDs: string[]; enabled: boolean; onChange: (value: boolean) => void }) {
  const selectedTargets = targetListIDs.map((id) => targetLists.find((target) => target.id === id)).filter((target): target is TargetList => Boolean(target))
  const keywordCount = selectedTargets.reduce((sum, target) => sum + (target.counts['DOMAIN-KEYWORD'] ?? 0), 0)
  const hasUnmaterializedPreset = targetListIDs.some((id) => id.startsWith('preset:'))
  const countSummary = keywordCount > 0 ? `当前目标列表包含 ${keywordCount} 条 DOMAIN-KEYWORD 规则。` : hasUnmaterializedPreset ? '预设目标的关键字数量将在生成预览时由后端计算。' : '当前目标列表暂无关键字规则。'
  return (
    <details className="settings-disclosure policy-advanced">
      <summary className="settings-disclosure-summary">高级设置</summary>
      <div className="settings-disclosure-body policy-advanced-body">
        <label className="policy-checkbox">
          <input type="checkbox" checked={enabled} onChange={(event) => onChange(event.target.checked)} />
          <span>启用关键字域名规则</span>
        </label>
        <p className="pol-hint">允许使用目标列表中的 DOMAIN-KEYWORD 规则。</p>
        <p className="pol-hint">关键字规则通过 RouterOS 正则表达式匹配。当一个域名同时匹配关键字规则和其他普通域名策略时，关键字规则可能优先于 Priority 更高的普通策略生效。</p>
        <p className="pol-hint">关闭后，本策略不使用关键字域名规则。</p>
        <p className="pol-hint">注意：设备上其他策略启用的关键字规则仍可能影响同时命中的域名。</p>
        <p className="pol-hint">{countSummary}{keywordCount === 0 && !hasUnmaterializedPreset ? ' 如果列表后续更新加入 DOMAIN-KEYWORD，开启状态下这些规则将自动参与策略。' : ''}</p>
      </div>
    </details>
  )
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

function sourceSummary(kind: RoutingSourceSelectionKind, interfaceSource: RoutingInterfaceSource, subject: Subject, ingress: TrafficIngressScope): string {
  if (kind === 'legacy') return [...ingress.interfaceLists, ...ingress.interfaces].join('、') || '旧规则仅来源匹配'
  if (kind === 'interface') {
    const names = [...interfaceSource.interfaceLists, ...interfaceSource.interfaces].join('、') || '未选择'
    return interfaceSource.excludePrefixes.length ? `${names}（排除 ${interfaceSource.excludePrefixes.join('、')}）` : names
  }
  if (kind === 'ip') return subject.prefixes.join('、') || '未填写'
  if (kind === 'device') return `${subject.members.length} 台指定终端`
  return '未选择来源'
}

function initialSourceKind(rule: RoutingRule | null): RoutingSourceSelectionKind {
  const scope = rule?.sourceScope
  if (!scope) return rule ? 'legacy' : 'interface'
  // interface-list 并入「指定接口」；已延后的 all 打开时要求重新选择接口。
  if (scope.kind === 'interface-list' || scope.kind === 'all') return 'interface'
  return scope.kind
}

function initialSourceInterfaces(rule: RoutingRule | null): string[] {
  const scope = rule?.sourceScope
  if (!scope || scope.kind !== 'interface') return []
  return Array.from(new Set([...(scope.interfaces ?? []), ...(scope.name ? [scope.name] : [])]))
}

function initialSourceInterfaceLists(rule: RoutingRule | null): string[] {
  const scope = rule?.sourceScope
  if (!scope) return []
  const lists = [...(scope.interfaceLists ?? [])]
  if (scope.kind === 'interface-list' && scope.name) lists.push(scope.name)
  return Array.from(new Set(lists))
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
  const [includeKeywordDomains, setIncludeKeywordDomains] = useState(rule ? rule.includeKeywordDomains : true)
  const [subject, setSubject] = useState<Subject>(() => rule?.subject ?? { mode: 'selected', members: [], prefixes: [] })
  const [sourceKind, setSourceKind] = useState<RoutingSourceSelectionKind>(() => initialSourceKind(rule))
  const [sourceInterfaces, setSourceInterfaces] = useState<string[]>(() => initialSourceInterfaces(rule))
  const [sourceInterfaceLists, setSourceInterfaceLists] = useState<string[]>(() => initialSourceInterfaceLists(rule))
  const [excludeText, setExcludeText] = useState(() => (rule?.sourceScope?.excludePrefixes ?? []).join('\n'))
  const [ipText, setIpText] = useState(() => (rule?.sourceScope?.kind === 'ip' ? rule.subject.prefixes.join('\n') : ''))
  const [trafficIngress, setTrafficIngress] = useState<TrafficIngressScope>(() => ({
    interfaceLists: [...(rule?.sourceScope ? [] : (rule?.ingress?.interfaceLists ?? context.trafficIngress.interfaceLists))],
    interfaces: [...(rule?.sourceScope ? [] : (rule?.ingress?.interfaces ?? context.trafficIngress.interfaces))],
  }))
  // 出口始终按「新配置」维护：编辑规则时预填当前出口配置，保存时后端按执行签名
  // 自动复用等价出口或安全复制（ResolvePolicyEgress），名称由系统分配。
  const [draft, setDraft] = useState<Egress>(() => (initialEgress ? egressDraftFrom(initialEgress) : defaultEgressDraft()))
  const [discovery, setDiscovery] = useState<PolicyDiscovery | null>(null)
  const [discoveryError, setDiscoveryError] = useState<string | null>(null)
  const [sourceSelectors, setSourceSelectors] = useState<Awaited<ReturnType<typeof fetchPolicyDiscoverySnapshot>>['sourceSelectors'] | null>(null)
  const [sourceSelectorError, setSourceSelectorError] = useState<string | null>(null)
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
    fetchPolicyDiscoverySnapshot(deviceID)
      .then((value) => {
        if (active) {
          setDiscovery(value.discovery)
          setDiscoveryError(null)
          setSourceSelectors(value.sourceSelectors)
          setSourceSelectorError(null)
        }
      })
      .catch((loadError) => {
        if (active) {
          const message = errorMessage(loadError, '设备拓扑读取失败')
          setDiscoveryError(message)
          setSourceSelectorError(message)
        }
      })
    return () => {
      active = false
    }
  }, [deviceID])

  // Default the ingress to the discovery-provided default interface-list once,
  // mirroring the old wizard (only for all/excluded source modes).
  useEffect(() => {
    if (sourceKind !== 'legacy' || ingressDefaulted || !discovery?.available || subject.mode === 'selected') return
    setIngressDefaulted(true)
    if (hasTrafficIngress(trafficIngress)) return
    const defaultList = discovery.trafficIngress.find((candidate) => candidate.kind === 'interface-list' && candidate.default)
    if (defaultList) setTrafficIngress({ interfaceLists: [defaultList.name], interfaces: [] })
  }, [discovery, ingressDefaulted, sourceKind, subject.mode, trafficIngress])

  const markDraftChanged = () => {
    draftRevisionRef.current += 1
    setDraftRevision(draftRevisionRef.current)
    setError(null)
  }

  const strategyErrors = useMemo(() => {
    const errors: string[] = []
    if (!ruleName.trim()) errors.push('规则名称不能为空')
    const interfaceSource: RoutingInterfaceSource = { interfaces: sourceInterfaces, interfaceLists: sourceInterfaceLists, excludePrefixes: parseSourcePrefixLines(excludeText) }
    const typedSubject = sourceKind === 'ip' ? { mode: 'selected' as const, members: [], prefixes: parseSourcePrefixLines(ipText) } : subject
    if (sourceKind === 'legacy' ? !sourceIsValid(subject, trafficIngress) : !typedSourceIsValid(sourceKind, interfaceSource, typedSubject)) errors.push('来源范围配置不完整，请选择有效的来源类型与值')
    return errors
  }, [ruleName, sourceKind, sourceInterfaces, sourceInterfaceLists, excludeText, ipText, subject, trafficIngress])

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

  const changeSourceKind = (next: RoutingSourceSelectionKind) => {
    markDraftChanged()
    setSourceKind(next)
    if (next === 'legacy') return
    if (next === 'device') {
      setSubject({ mode: 'selected', members: sourceKind === 'device' ? subject.members : [], prefixes: [] })
      return
    }
    if (next === 'ip') {
      setSubject({ mode: 'selected', members: [], prefixes: [] })
      return
    }
    setSubject({ mode: 'all', members: [], prefixes: [] })
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
    const excludePrefixes = parseSourcePrefixLines(excludeText)
    const interfaceSource: RoutingInterfaceSource = { interfaces: sourceInterfaces, interfaceLists: sourceInterfaceLists, excludePrefixes }
    // 接口来源的 subject 由后端按 scope 推导（含排除投影），前端始终发送 all 占位。
    const effectiveSubject: Subject = sourceKind === 'ip' ? { mode: 'selected', members: [], prefixes: parseSourcePrefixLines(ipText) } : sourceKind === 'interface' ? { mode: 'all', members: [], prefixes: [] } : subject
    const sourceScope = sourceKind === 'legacy'
      ? undefined
      : sourceKind === 'interface'
        ? {
            kind: 'interface' as RoutingSourceKind,
            ...(sourceInterfaces.length ? { interfaces: [...sourceInterfaces].sort() } : {}),
            ...(sourceInterfaceLists.length ? { interfaceLists: [...sourceInterfaceLists].sort() } : {}),
            ...(excludePrefixes.length ? { excludePrefixes } : {}),
          }
        : { kind: sourceKind as RoutingSourceKind }
    const proposal: PolicyPlanProposal = {
      egress: draft,
      routingRule: {
        id: rule?.id ?? '',
        name: ruleName.trim(),
        subject: effectiveSubject,
        ingress: sourceKind === 'legacy' ? trafficIngress : { interfaceLists: [], interfaces: [] },
        ...(sourceScope ? { sourceScope } : {}),
        targetListIds: targetListIDs,
        egressId: draft.id,
        priority: Number(rulePriority) || 0,
        enabled,
        includeKeywordDomains,
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
        ['来源', sourceSummary(sourceKind, interfaceSource, effectiveSubject, trafficIngress)],
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

  return (
    <>
      <Modal
        open
        persistent
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
            <RoutingSourcePicker
              sourceKind={sourceKind}
              interfaceSource={{ interfaces: sourceInterfaces, interfaceLists: sourceInterfaceLists, excludePrefixes: parseSourcePrefixLines(excludeText) }}
              excludeText={excludeText}
              ipText={ipText}
              sourceSelectors={sourceSelectors}
              sourceSelectorError={sourceSelectorError}
              discovery={discovery}
              trafficIngress={trafficIngress}
              subject={subject}
              terminals={context.terminals}
              busy={busy}
              onKindChange={changeSourceKind}
              onInterfaceSource={(next) => { markDraftChanged(); setSourceInterfaces(next.interfaces); setSourceInterfaceLists(next.interfaceLists) }}
              onExcludeText={(text) => { markDraftChanged(); setExcludeText(text) }}
              onIpText={(text) => { markDraftChanged(); setIpText(text) }}
              onIngress={(candidate) => toggleIngress(candidate)}
              onSubject={(next) => { markDraftChanged(); setSubject(next) }}
            />
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
              <KeywordDomainSetting
                targetLists={targetLists}
                targetListIDs={targetListIDs}
                enabled={includeKeywordDomains}
                onChange={(value) => {
                  markDraftChanged()
                  setIncludeKeywordDomains(value)
                }}
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
              <p className="pol-hint">选择 WAN 接口即可，网关自动发现、名称由系统分配；保存时与现有出口配置相同的会自动复用，不会重复创建。</p>
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
