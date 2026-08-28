import { useEffect, useState } from 'react'
import type {
  PolicyDiscovery,
  PolicyApplyResult,
  PolicyEgressDraft,
  PolicyEgressFamily,
  PolicyOverview,
  PolicyPlanEnvelope,
  PolicySource,
  PolicyTrafficIngressScope,
  PolicyWANRoute,
} from './types'
import { defaultEgressDraft, defaultSourceDraft, egressDraftErrors, egressDraftFromEgress } from './drafts'
import { saveEgress, saveTrafficIngress, saveSource, generatePlan } from './api'
import { ChangePlanView } from './ChangePlanView'
import { SourceEditorModal } from './PolicySourcesPage'
import {
  PolicyErrorDisplay,
  PolicyField,
  PolicyModal,
  PolicyNotice,
  PolicyStatusBadge,
  PolicyWizardSteps,
} from './components'
import {
  policyFamilyLabel,
  policySourceTypeLabel,
} from './format'
import type { PolicyEgress } from './types'

const WIZARD_STEPS = ['出口与地址族', '策略流量入口', '域名列表', '预览并应用']

export function PolicyWizard({
  deviceID,
  overview,
  discovery,
  egress,
  onClose,
  onChanged,
  onApplied,
}: {
  deviceID: string
  overview: PolicyOverview
  discovery: PolicyDiscovery | null
  egress?: PolicyEgress
  onClose: () => void
  onChanged: () => void
  onApplied: (result?: PolicyApplyResult) => void
}) {
  const [step, setStep] = useState(0)
  const [draft, setDraft] = useState<PolicyEgressDraft>(() => egress ? egressDraftFromEgress(egress) : defaultEgressDraft())
  const [trafficIngress, setTrafficIngress] = useState<PolicyTrafficIngressScope>(() => overview.trafficIngress)
  const [trafficIngressDefaulted, setTrafficIngressDefaulted] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [plan, setPlan] = useState<PolicyPlanEnvelope | null>(null)

  const [allDomainOptions, setAllDomainOptions] = useState<PolicySource[]>(() => (
    overview.sources.filter((source) => !source.pendingDeletion)
  ))
  const [selectedDomainIds, setSelectedDomainIds] = useState<string[]>(() => (
    overview.sources
      .filter((source) => source.egressId === egress?.id && !source.pendingDeletion)
      .map((source) => source.id)
  ))
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false)
  const [domainMenuOpen, setDomainMenuOpen] = useState(false)
  const [domainQuery, setDomainQuery] = useState('')

  useEffect(() => {
    const availableSources = overview.sources.filter((source) => !source.pendingDeletion)
    setAllDomainOptions(availableSources)
    setSelectedDomainIds((current) => {
      const availableIDs = new Set(availableSources.map((source) => source.id))
      return current.filter((id) => availableIDs.has(id))
    })
  }, [overview.sources])

  useEffect(() => {
    if (trafficIngressDefaulted || !discovery?.available) return
    setTrafficIngressDefaulted(true)
    const listNames = new Set(discovery.trafficIngress.filter((candidate) => candidate.kind === 'interface-list').map((candidate) => candidate.name))
    const interfaceNames = new Set(discovery.trafficIngress.filter((candidate) => candidate.kind !== 'interface-list').map((candidate) => candidate.name))
    const interfaceLists: string[] = []
    const interfaces: string[] = []
    const classify = (name: string, preferList: boolean) => {
      if (preferList && listNames.has(name)) interfaceLists.push(name)
      else if (!preferList && interfaceNames.has(name)) interfaces.push(name)
      else if (listNames.has(name)) interfaceLists.push(name)
      else if (interfaceNames.has(name)) interfaces.push(name)
    }
    trafficIngress.interfaceLists.forEach((name) => classify(name, true))
    trafficIngress.interfaces.forEach((name) => classify(name, false))
    const normalized = {
      interfaceLists: [...new Set(interfaceLists)],
      interfaces: [...new Set(interfaces)],
    }
    if (!normalized.interfaceLists.length && !normalized.interfaces.length) {
      const defaultList = discovery.trafficIngress.find((candidate) => candidate.kind === 'interface-list' && candidate.default)
      if (defaultList) normalized.interfaceLists = [defaultList.name]
    }
    setTrafficIngress(normalized)
  }, [discovery, trafficIngress, trafficIngressDefaulted])

  const readOnly = !overview || overview.setup.state !== 'ready'
  const errors = egressDraftErrors(draft, discovery)
  const canNext = step === 0
    ? errors.length === 0
    : step === 1
      ? trafficIngress.interfaceLists.length + trafficIngress.interfaces.length > 0
      : true

  const filteredDomainOptions = allDomainOptions.filter((source) => {
    const query = domainQuery.trim().toLocaleLowerCase()
    if (!query) return true
    return source.name.toLocaleLowerCase().includes(query) || source.url.toLocaleLowerCase().includes(query)
  })

  const handleCreateDomainSuccess = (newCreatedItem: PolicySource) => {
    setAllDomainOptions((previous) => [
      newCreatedItem,
      ...previous.filter((source) => source.id !== newCreatedItem.id),
    ])
    setSelectedDomainIds((previous) => previous.includes(newCreatedItem.id) ? previous : [...previous, newCreatedItem.id])
    setIsCreateModalOpen(false)
    setDomainMenuOpen(false)
    setDomainQuery('')
    setError(null)
  }

  const handleToggleDomain = (source: PolicySource) => {
    if (source.egressId && source.egressId !== egress?.id) return
    setError(null)
    setSelectedDomainIds((previous) => previous.includes(source.id)
      ? previous.filter((id) => id !== source.id)
      : [...previous, source.id])
  }

  const handleSaveDraftAndGeneratePlan = async () => {
    setError(null)
    setSaving(true)
    try {
      const savedEgress = await saveEgress(deviceID, draft)
      await saveTrafficIngress(deviceID, trafficIngress)
      for (const source of allDomainOptions) {
        const currentlyBound = source.egressId === savedEgress.id
        const shouldBeBound = selectedDomainIds.includes(source.id)
        if (currentlyBound !== shouldBeBound) {
          await saveSource(deviceID, {
            ...source,
            egressId: shouldBeBound ? savedEgress.id : '',
          }, undefined)
        }
      }
      const envelope = await generatePlan(deviceID, overview.egresses.length ? 'structural' : 'initial')
      setPlan(envelope)
      onChanged()
      setStep(3)
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <PolicyModal
      title={egress ? `编辑策略：${egress.name}` : '新建策略'}
      subtitle="向导依次完成出口、策略流量入口与域名列表草稿（保存在 rosboard，不写入 RouterOS）。"
      wide
      onClose={onClose}
      header={<PolicyWizardSteps steps={WIZARD_STEPS} current={step} onJump={(i) => i <= step && setStep(i)} />}
      footer={step < 3 ? (
        <div className="policy-form-actions policy-wizard-nav policy-wizard-footer">
          <button type="button" className="close-button" onClick={step === 0 ? onClose : () => setStep((s) => Math.max(0, s - 1))} disabled={saving}>
            {step === 0 ? '取消' : '上一步'}
          </button>
          <button type="button" className="primary-button" disabled={!canNext || saving || (step === 2 && readOnly)} onClick={() => {
            if (step === 2 && selectedDomainIds.length === 0) {
              setError('请至少选择或新建一个域名列表')
              return
            }
            if (step === 2) void handleSaveDraftAndGeneratePlan()
            else setStep((s) => s + 1)
          }}>
            {step === 2 && saving ? '保存并预览中…' : '下一步'}
          </button>
        </div>
      ) : null}
    >
      {error ? <PolicyErrorDisplay error={error} /> : null}

      {step === 0 ? (
        <div className="policy-wizard-stage">
          <h4>出口与地址族</h4>
          <EgressDraftForm draft={draft} setDraft={setDraft} discovery={discovery} />
        </div>
      ) : null}

      {step === 1 ? (
        <TrafficIngressForm
          discovery={discovery}
          value={trafficIngress}
          onChange={setTrafficIngress}
        />
      ) : null}

      {step === 2 ? (
        <div className="policy-wizard-stage">
          <h4>域名列表</h4>
          <p className="policy-hint">可同时选择多个域名列表。已绑定到其他策略路由的列表不会被自动改绑。</p>
          <div className="policy-domain-picker">
            <button
              type="button"
              className="policy-domain-picker-trigger"
              aria-expanded={domainMenuOpen}
              aria-haspopup="listbox"
              onClick={() => setDomainMenuOpen((open) => !open)}
            >
              <span>{selectedDomainIds.length ? `已选择 ${selectedDomainIds.length} 个域名列表` : '选择域名列表'}</span>
              <span aria-hidden="true">▾</span>
            </button>
            {domainMenuOpen ? (
              <div className="policy-domain-picker-menu">
                <input
                  className="settings-input"
                  value={domainQuery}
                  onChange={(event) => setDomainQuery(event.target.value)}
                  placeholder="搜索列表名称或 URL"
                  aria-label="搜索域名列表"
                  autoFocus
                />
                <div className="policy-domain-picker-options" role="listbox" aria-label="选择域名列表" aria-multiselectable="true">
                  {filteredDomainOptions.length ? filteredDomainOptions.map((source) => {
                    const selected = selectedDomainIds.includes(source.id)
                    const boundElsewhere = Boolean(source.egressId && source.egressId !== egress?.id)
                    const location = source.type === 'url' ? source.url || '未填写 URL' : '本地上传 YAML'
                    return (
                      <label key={source.id} className={`policy-domain-option${selected ? ' selected' : ''}${boundElsewhere ? ' disabled' : ''}`}>
                        <input
                          type="checkbox"
                          checked={selected}
                          disabled={boundElsewhere}
                          onChange={() => handleToggleDomain(source)}
                        />
                        <span className="policy-domain-option-body">
                          <span className="policy-domain-option-title">
                            <strong title={source.name}>{source.name}</strong>
                            <PolicyStatusBadge tone="info">{policySourceTypeLabel[source.type] ?? source.type}</PolicyStatusBadge>
                            <small className="policy-domain-option-location" title={location}>
                              {location}{boundElsewhere ? ' · 已绑定其他策略路由' : ''}
                            </small>
                          </span>
                        </span>
                      </label>
                    )
                  }) : <p className="policy-hint">暂无已保存列表</p>}
                </div>
                <div className="policy-domain-picker-footer">
                  <button
                    type="button"
                    className="link-button"
                    onClick={() => { setDomainMenuOpen(false); setIsCreateModalOpen(true) }}
                  >
                    ＋ 新建域名列表...
                  </button>
                </div>
              </div>
            ) : null}
          </div>
          {isCreateModalOpen ? (
            <SourceEditorModal
              deviceID={deviceID}
              draft={defaultSourceDraft('')}
              egresses={overview.egresses}
              onClose={() => setIsCreateModalOpen(false)}
              onSuccess={handleCreateDomainSuccess}
            />
          ) : null}
          </div>
      ) : null}

      {step === 3 ? (
        <div className="policy-wizard-apply">
          <h4>预览并应用</h4>
          <p className="policy-hint">草稿已保存，以下为将要应用到 RouterOS 的变更。</p>
          {plan ? (
            <ChangePlanView
              key={plan.planId}
              deviceID={deviceID}
              plan={plan.plan}
              compact
              onApplied={(result) => { setPlan(null); onApplied(result); onClose() }}
              onCancel={() => { setPlan(null); setStep(2) }}
            />
          ) : (
            <PolicyNotice tone="warn">差异预览尚未生成，请返回域名列表重试。</PolicyNotice>
          )}
        </div>
      ) : null}

    </PolicyModal>
  )
}

// ---- Egress draft form ----

function EgressDraftForm({
  draft,
  setDraft,
  discovery,
}: {
  draft: PolicyEgressDraft
  setDraft: (d: PolicyEgressDraft) => void
  discovery: PolicyDiscovery | null
}) {
  const update = (patch: Partial<PolicyEgressDraft>) => setDraft({ ...draft, ...patch })
  const setFamilyEnabled = (family: PolicyEgressFamily['family'], enabled: boolean) => {
    const index = draft.families.findIndex((item) => item.family === family)
    if (index >= 0) {
      const families = draft.families.slice()
      families[index] = { ...families[index], enabled }
      setDraft({ ...draft, families })
    } else if (enabled) {
      setDraft({
        ...draft,
        families: [...draft.families, {
          family,
          enabled: true,
          wanInterface: '',
          gateway: '',
          routeTable: '',
          routeMode: '',
          natMode: '',
          wanSource: '',
        }],
      })
    }
  }
  const isFamilyEnabled = (family: PolicyEgressFamily['family']) => draft.families.some((item) => item.family === family && item.enabled)

  return (
    <div className="policy-form">
      <PolicyField label="出口名称" htmlFor="policy-egress-name">
        <input id="policy-egress-name" className="settings-input" value={draft.name} onChange={(e) => update({ name: e.target.value })} maxLength={128} placeholder="例如 国际出口" />
      </PolicyField>
      <fieldset className="policy-family-editor">
        <legend>地址族与出口</legend>
        {(['ipv4', 'ipv6'] as const).map((family) => {
          const enabled = isFamilyEnabled(family)
          const selected = draft.families.find((item) => item.family === family)
          return (
            <div key={family} className="policy-family-row">
              <label className="policy-family-head checkbox-label">
                <input type="checkbox" checked={enabled} onChange={(event) => setFamilyEnabled(family, event.target.checked)} />
                <span>{policyFamilyLabel[family]}</span>
              </label>
              {enabled && selected ? (
                <WANFamilyEditor
                  family={selected}
                  discovery={discovery}
                  onChange={(updated) => {
                    const families = draft.families.map((item) => item.family === updated.family ? updated : item)
                    setDraft({ ...draft, families })
                  }}
                />
              ) : null}
            </div>
          )
        })}
      </fieldset>
      <label className="checkbox-label policy-checkbox">
        <input type="checkbox" checked={draft.enabled} onChange={(event) => update({ enabled: event.target.checked })} />
        <span>启用该出口策略</span>
      </label>
      <details className="settings-disclosure policy-advanced">
        <summary className="settings-disclosure-summary">高级功能（优先级、路由、DNS、标记列表模式，均有默认值）</summary>
        <div className="settings-disclosure-body policy-advanced-body">
          <div className="policy-form-grid">
            <PolicyField label="优先级" hint="数值越小越先匹配；不同出口的域名冲突会被阻止。">
              <input id="policy-egress-priority" className="settings-input" type="number" min={0} value={draft.priority} onChange={(e) => update({ priority: Number(e.target.value) || 0 })} />
            </PolicyField>
            <PolicyField label="断线处理" hint={draft.failureMode === 'fallback' ? 'WAN 故障时回落 main 路由表，流量可能走非预期出口。' : '断线阻断会在 WAN 故障时丢弃流量，不泄漏到主线。'}>
              <select id="policy-egress-failure" className="settings-select" value={draft.failureMode} onChange={(e) => update({ failureMode: e.target.value })}>
                <option value="strict">断线阻断（drop 流量，不切换出口）（默认）</option>
                <option value="fallback">切主线路（回落 main 路由表）</option>
                <option value="existing">沿用规则（沿用现有故障切换逻辑）</option>
              </select>
            </PolicyField>
            <PolicyField label="DNS 上游" hint="仅支持一个 UDP/TCP 53 的明文 DNS 上游；查询不加密。">
              <input id="policy-egress-dns" className="settings-input" value={draft.dnsUpstream} onChange={(e) => update({ dnsUpstream: e.target.value })} placeholder="1.1.1.1" />
            </PolicyField>
            <PolicyField label="Fake DNS 别名" hint="留空由 rosboard 自动分配经冲突检查的地址。">
              <input id="policy-egress-alias" className="settings-input" value={draft.fakeAlias} onChange={(e) => update({ fakeAlias: e.target.value })} placeholder="自动分配" />
            </PolicyField>
            <PolicyField label="标记列表模式" hint="共享模式下多个域名列表共用一个标记列表和一组业务 mangle；专用模式为每个域名列表独立建表。">
              <select id="policy-egress-listmode" className="settings-select" value={draft.listMode} onChange={(e) => update({ listMode: e.target.value })}>
                <option value="shared">共享标记列表</option>
                <option value="dedicated">每个域名列表专用标记列表（默认）</option>
              </select>
            </PolicyField>
            {draft.listMode === 'dedicated' ? (
              <div className="policy-field">
                <span className="policy-field-label">标记列表名称</span>
                <p className="policy-hint">专用模式下每个域名列表独立生成一个标记列表，名称跟随域名列表并由 rosboard 稳定生成，不在此编辑。当前后端保存契约仍要求出口级兼容名称（将按默认值发送），逐列表自定义命名待后端契约匹配。</p>
              </div>
            ) : (
              <PolicyField label="标记列表名称" hint="RouterOS address-list 名称；共享模式下多个域名列表共用此列表；不加 _v4/_v6 后缀；改名属于结构变更。">
                <input id="policy-egress-listname" className="settings-input" value={draft.listName} onChange={(e) => update({ listName: e.target.value })} placeholder="manual_proxy_lab" />
              </PolicyField>
            )}
          </div>
          {draft.families.filter((family) => family.enabled).map((family) => (
            <EgressFamilyAdvancedFields
              key={family.family}
              family={family}
              onChange={(updated) => {
                const families = draft.families.map((item) => item.family === updated.family ? updated : item)
                setDraft({ ...draft, families })
              }}
            />
          ))}
          <label className="checkbox-label policy-checkbox">
            <input type="checkbox" checked={draft.routerOutput} onChange={(event) => update({ routerOutput: event.target.checked })} />
            <span>包含 RouterOS 本机流量（仅对新连接生效，启停不会清除已有连接）</span>
          </label>
        </div>
      </details>
    </div>
  )
}

function WANFamilyEditor({
  family,
  discovery,
  onChange,
}: {
  family: PolicyEgressFamily
  discovery: PolicyDiscovery | null
  onChange: (f: PolicyEgressFamily) => void
}) {
  const nextHopValue = '__rosboard_next_hop__'
  const nextHop = family.wanSource === 'next-hop'
  const wans = discovery?.wans ?? []
  const interfaces = wans.map((wan) => wan.interface)
  const discoveryAvailable = Boolean(discovery?.available) && interfaces.length > 0
  const [manual, setManual] = useState(false)
  const [gatewayManuallyEdited, setGatewayManuallyEdited] = useState(() => Boolean(family.gateway.trim()))
  const knownInterface = interfaces.includes(family.wanInterface)
  const manualMode = !discoveryAvailable || manual || (family.wanInterface !== '' && !knownInterface)
  const selectedWAN = wans.find((wan) => wan.interface === family.wanInterface)
  const gatewayCandidates = selectedWAN ? selectedWAN.routes
    .filter((route) => route.family === family.family && route.active && route.proven && isMainPolicyRoute(route))
    .map((route) => policyRouteGateway(route, family.family))
    .filter(Boolean)
    .filter((gateway, index, values) => values.indexOf(gateway) === index) : []
  const suggestedGateway = !selectedWAN?.pointToPoint && gatewayCandidates.length === 1 ? gatewayCandidates[0] : ''
  const gatewayRequired = nextHop || Boolean(family.wanInterface.trim() && !selectedWAN?.pointToPoint)

  useEffect(() => {
    if (nextHop || gatewayManuallyEdited || !family.wanInterface.trim() || family.gateway === suggestedGateway) return
    onChange({ ...family, gateway: suggestedGateway })
  }, [family, gatewayManuallyEdited, nextHop, onChange, suggestedGateway])

  const update = (patch: Partial<PolicyEgressFamily>) => onChange({ ...family, ...patch })
  const selectInterface = (value: string) => {
    if (value === nextHopValue) {
      update({ wanSource: 'next-hop', wanInterface: '' })
    } else {
      update({ wanSource: nextHop ? '' : family.wanSource, wanInterface: value })
    }
    setManual(false)
  }

  const gatewayHint = nextHop
    ? '必须填写单个 IP；该模式不绑定真实接口。'
    : selectedWAN?.pointToPoint
      ? '点对点接口无需填写 IP 网关。'
      : suggestedGateway
        ? '已从 main 表活动默认路由或 DHCP 自动填入，可手动修改。'
        : '未发现唯一的下一跳网关，必须手动填写 IP。'

  return (
    <div className="policy-form-grid">
      <PolicyField label="策略接口" htmlFor={`policy-wan-${family.family}-${manualMode ? 'mode' : 'select'}`} hint={discoveryAvailable ? '选择真实 WAN 接口，或选择“下一跳网关”；下一跳模式不绑定接口。' : '无法读取接口候选时可手动输入；普通接口必须同时填写下一跳网关。'}>
        {(discoveryAvailable || nextHop) && !manualMode ? (
          <select
            id={`policy-wan-${family.family}-select`}
            className="settings-select"
            value={nextHop ? nextHopValue : knownInterface ? family.wanInterface : ''}
            onChange={(event) => selectInterface(event.target.value)}
          >
            {!nextHop && !family.wanInterface ? <option value="" disabled>请选择真实接口</option> : null}
            {!knownInterface && family.wanInterface ? <option value={family.wanInterface}>{family.wanInterface}（自定义）</option> : null}
            {wans.map((wan) => <option key={wan.interface} value={wan.interface}>{`${wan.interface}（${wan.type || '未知类型'}${wan.running ? '，运行中' : ''}）`}</option>)}
            <option value={nextHopValue}>下一跳网关</option>
          </select>
        ) : (
          <>
            <select
              id={`policy-wan-${family.family}-mode`}
              className="settings-select"
              value={nextHop ? nextHopValue : 'manual'}
              onChange={(event) => selectInterface(event.target.value)}
            >
              <option value="manual">真实接口（手动输入）</option>
              <option value={nextHopValue}>下一跳网关</option>
            </select>
            {!nextHop ? (
              <input
                id={`policy-wan-${family.family}-input`}
                className="settings-input"
                list={discoveryAvailable ? `policy-wan-${family.family}` : undefined}
                value={family.wanInterface}
                onChange={(event) => update({ wanSource: family.wanSource === 'next-hop' && event.target.value.trim() ? '' : family.wanSource, wanInterface: event.target.value })}
                placeholder="例如 pppoe-out1 / ether1 / wg-cf"
              />
            ) : null}
          </>
        )}
        {discoveryAvailable && manualMode ? <p className="policy-hint"><button type="button" className="link-button" onClick={() => setManual(false)}>从候选接口选择</button></p> : null}
        {discoveryAvailable && !manualMode ? <p className="policy-hint"><button type="button" className="link-button" onClick={() => setManual(true)}>手动输入接口名…</button></p> : null}
        <datalist id={`policy-wan-${family.family}`}>
          {wans.map((wan) => <option key={wan.interface} value={wan.interface}>{`${wan.interface}（${wan.type || '未知类型'}${wan.running ? '，运行中' : ''}）`}</option>)}
        </datalist>
      </PolicyField>
      <PolicyField label={`下一跳网关（${family.family === 'ipv6' ? 'IPv6' : 'IPv4'}）${gatewayRequired ? '（必填）' : ''}`} htmlFor={`policy-wan-gw-${family.family}-input`} hint={gatewayHint}>
        <input
          id={`policy-wan-gw-${family.family}-input`}
          className="settings-input"
          required={gatewayRequired}
          list={`policy-wan-gw-${family.family}`}
          value={family.gateway}
          onChange={(event) => { setGatewayManuallyEdited(true); update({ gateway: event.target.value }) }}
          placeholder={gatewayRequired ? '例如 10.0.2.1（必填）' : '点对点接口无需填写'}
        />
        <datalist id={`policy-wan-gw-${family.family}`}>
          {gatewayCandidates.map((gateway) => <option key={gateway} value={gateway} />)}
        </datalist>
        {gatewayManuallyEdited && !nextHop ? <p className="policy-hint"><button type="button" className="link-button" onClick={() => { setGatewayManuallyEdited(false); update({ gateway: suggestedGateway }) }}>恢复自动发现</button></p> : null}
        {gatewayRequired && !family.gateway.trim() ? <p className="policy-field-error">{nextHop ? '下一跳模式必须填写网关 IP。' : '普通接口未发现唯一网关，请填写下一跳 IP。'}</p> : null}
      </PolicyField>
    </div>
  )
}

function EgressFamilyAdvancedFields({
  family,
  onChange,
}: {
  family: PolicyEgressFamily
  onChange: (f: PolicyEgressFamily) => void
}) {
  const familyName = family.family === 'ipv6' ? 'IPv6' : 'IPv4'
  const update = (patch: Partial<PolicyEgressFamily>) => onChange({ ...family, ...patch })

  return (
    <div className="policy-form-grid">
      <PolicyField label={`路由表（${familyName}）`} hint="留空时由 rosboard 创建专用 fib 表；填 main 表示复用主表。">
        <input id={`policy-table-${family.family}`} className="settings-input" value={family.routeTable} onChange={(event) => update({ routeTable: event.target.value })} placeholder="自动" />
      </PolicyField>
      <PolicyField label={`路由模式（${familyName}）`}>
        <select id={`policy-route-mode-${family.family}`} className="settings-select" value={family.routeMode} onChange={(event) => update({ routeMode: event.target.value })}>
          <option value="">跟随出口断线处理设置</option>
          <option value="strict">严格绑定</option>
          <option value="fallback">允许回落 main</option>
        </select>
      </PolicyField>
      <PolicyField label={`NAT 模式（${familyName}）`}>
        <select id={`policy-nat-mode-${family.family}`} className="settings-select" value={family.natMode} onChange={(event) => update({ natMode: event.target.value })}>
          <option value="">自动（可证明复用，否则窄范围自建）</option>
          <option value="none">不建立 NAT</option>
          <option value="masquerade">动态 masquerade</option>
        </select>
      </PolicyField>
    </div>
  )
}

function isMainPolicyRoute(route: PolicyWANRoute): boolean {
  return !route.table.trim() || route.table.toLowerCase() === 'main'
}

function policyRouteGateway(route: PolicyWANRoute, family: PolicyEgressFamily['family']): string {
  for (const raw of [route.gateway, route.immediateGateway]) {
    const value = raw.trim()
    const address = value.includes('%') ? value.slice(0, value.indexOf('%')) : value
    if (family === 'ipv4' ? address.includes('.') : address.includes(':')) return family === 'ipv6' ? value : address
  }
  return ''
}

// ---- Traffic ingress form ----

function TrafficIngressForm({
  discovery,
  value,
  onChange,
}: {
  discovery: PolicyDiscovery | null
  value: PolicyTrafficIngressScope
  onChange: (value: PolicyTrafficIngressScope) => void
}) {
  const candidates = discovery?.trafficIngress ?? []
  const selectedLists = new Set(value.interfaceLists)

  const toggleCandidate = (name: string, kind: string) => {
    if (kind === 'interface-list') {
      const selecting = !selectedLists.has(name)
      const interfaceLists = selecting
        ? [...value.interfaceLists, name]
        : value.interfaceLists.filter((item) => item !== name)
      const interfaces = selecting
        ? value.interfaces.filter((interfaceName) => !candidates.some((candidate) => candidate.name === interfaceName && candidate.coveredBy.includes(name)))
        : value.interfaces
      onChange({ interfaceLists, interfaces })
      return
    }
    onChange({
      ...value,
      interfaces: value.interfaces.includes(name)
        ? value.interfaces.filter((item) => item !== name)
        : [...value.interfaces, name],
    })
  }

  const kindLabel: Record<string, string> = {
    'interface-list': '接口列表',
    bridge: 'Bridge',
    vlan: 'VLAN',
    wireguard: 'WireGuard',
    vpn: 'VPN',
    tunnel: '隧道',
    physical: '物理接口',
  }

  return (
    <div className="policy-wizard-lan">
      <h4>策略流量入口</h4>
      <p className="policy-hint">
        选择需要进入策略路由的接口列表或三层接口。该设置由本设备全部出口策略共享。
      </p>
      {candidates.length ? (
        <div className="policy-wizard-lan-list">
          {candidates.map((candidate) => {
            const coveredBy = candidate.kind === 'interface-list'
              ? []
              : candidate.coveredBy.filter((listName) => selectedLists.has(listName))
            const covered = coveredBy.length > 0
            const directlySelected = candidate.kind === 'interface-list'
              ? value.interfaceLists.includes(candidate.name)
              : value.interfaces.includes(candidate.name)
            return (
              <label key={`${candidate.kind}:${candidate.name}`} className={`policy-choice ${covered ? 'policy-choice-disabled' : ''}`}>
                <input
                  type="checkbox"
                  checked={directlySelected || covered}
                  onChange={() => toggleCandidate(candidate.name, candidate.kind)}
                  disabled={covered}
                />
                <span>
                  <strong>{candidate.name}</strong>
                  <small>（{kindLabel[candidate.kind] ?? candidate.kind}）</small>
                  {!candidate.running && candidate.kind !== 'interface-list' ? <small> · 当前未运行</small> : null}
                  {candidate.addresses.length ? <small> · {candidate.addresses.join(', ')}</small> : null}
                  {candidate.staticMembers.length ? <small> · 成员：{candidate.staticMembers.join(', ')}</small> : null}
                  {candidate.dynamicMembers ? <small> · 含动态 VPN 成员</small> : null}
                </span>
                <small className="policy-hint">
                  {covered ? `已由 ${coveredBy.join(', ')} 覆盖` : candidate.reason}
                </small>
              </label>
            )
          })}
        </div>
      ) : (
        <PolicyNotice tone="info">
          {discovery?.available ? '未发现可用的策略流量入口' : '设备发现不可用，暂时不能选择策略流量入口'}
        </PolicyNotice>
      )}
      {value.interfaceLists.length + value.interfaces.length ? (
        <div className="policy-lan-selected">
          <strong>已选择：</strong>
          <span>{[...value.interfaceLists, ...value.interfaces].join(', ')}</span>
        </div>
      ) : null}
      <PolicyNotice tone="info">
        WireGuard 可直接选择；动态 L2TP/SSTP/OpenVPN 请先由 RouterOS VPN 配置加入专用接口列表（如 VPN-LAN），再在这里选择该列表。
      </PolicyNotice>
    </div>
  )
}
