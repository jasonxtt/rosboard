import { useState } from 'react'
import type {
  PolicyDiscovery,
  PolicyApplyResult,
  PolicyEgressDraft,
  PolicyEgressFamily,
  PolicyOverview,
  PolicyPlanEnvelope,
  PolicyPreview,
} from './types'
import { defaultEgressDraft, egressDraftErrors, egressDraftFromEgress } from './drafts'
import { saveEgress, saveLANScope, saveSource, generatePlan } from './api'
import { ChangePlanView } from './ChangePlanView'
import {
  PolicyErrorDisplay,
  PolicyField,
  PolicyModal,
  PolicyNotice,
  PolicyWizardSteps,
} from './components'
import {
  policyFailureModeLabel,
  policyFamilyLabel,
  policyListModeLabel,
  scheduleOptions,
} from './format'
import type { PolicyEgress } from './types'

const WIZARD_STEPS = ['出口与地址族', 'LAN 范围', '域名列表', '解析预览', '确认草稿', '差异与应用']

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
  const [lanInterfaces, setLANInterfaces] = useState<string[]>(() => {
    const scope = overview.lanScope
    const value = scope.interfaces ?? scope.interfaceList ?? scope.listName
    if (Array.isArray(value)) return value.filter((item): item is string => typeof item === 'string')
    return typeof value === 'string' && value ? [value] : []
  })
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [plan, setPlan] = useState<PolicyPlanEnvelope | null>(null)
  const [adminPassword, setAdminPassword] = useState('')

  // Source creation within wizard
  const [newSourceName, setNewSourceName] = useState('')
  const [newSourceType, setNewSourceType] = useState<'url' | 'upload'>('url')
  const [newSourceURL, setNewSourceURL] = useState('')
  const [newSourceFile, setNewSourceFile] = useState<File | null>(null)
  const [newSourceSchedule, setNewSourceSchedule] = useState('24h')
  const [preview, setPreview] = useState<PolicyPreview | null>(null)
  const [selectedSourceIDs, setSelectedSourceIDs] = useState<Set<string>>(() => new Set(
    overview.sources.filter((source) => source.egressId === egress?.id && !source.pendingDeletion).map((source) => source.id),
  ))

  const readOnly = !overview || !overview.access.enabled || overview.setup.state !== 'ready'
  const errors = egressDraftErrors(draft)
  const sourceInputValid = !newSourceName.trim() || (newSourceType === 'url' ? Boolean(newSourceURL.trim()) : Boolean(newSourceFile))
  const sourcePreviewValid = !newSourceName.trim() || Boolean(preview?.previewId)
  const canNext = step === 0
    ? errors.length === 0
    : step === 1
      ? lanInterfaces.length > 0
      : step === 2
        ? sourceInputValid
        : step === 3
          ? sourcePreviewValid
          : true

  const handleSaveDraft = async () => {
    setError(null)
    setSaving(true)
    try {
      const savedEgress = await saveEgress(deviceID, draft)
      const scope = { ...overview.lanScope, interfaces: lanInterfaces }
      await saveLANScope(deviceID, scope, adminPassword)
      if (newSourceName && !preview) {
        throw new Error('请先解析新域名列表，再保存出口草稿')
      }
      if (newSourceName && preview && preview.validRules <= 0) {
        throw new Error('解析结果没有有效规则，无法继续保存该列表')
      }
      for (const source of overview.sources) {
        const currentlyBound = source.egressId === savedEgress.id
        const shouldBeBound = selectedSourceIDs.has(source.id)
        if (currentlyBound !== shouldBeBound) {
          await saveSource(deviceID, {
            ...source,
            egressId: shouldBeBound ? savedEgress.id : '',
          }, undefined)
        }
      }
      if (newSourceName && preview?.previewId) {
        await saveSource(deviceID, {
          id: '',
          egressId: savedEgress.id,
          type: newSourceType,
          name: newSourceName,
          url: newSourceURL,
          schedule: newSourceSchedule,
          enabled: true,
          revision: 0,
        }, { previewId: preview.previewId })
      }
      onChanged()
      setStep(5)
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleGeneratePlan = async () => {
    setError(null)
    setSaving(true)
    try {
      const envelope = await generatePlan(deviceID, 'structural')
      setPlan(envelope)
    } catch (e) {
      setError(e instanceof Error ? e.message : '生成计划失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <PolicyModal
      title={egress ? `编辑策略：${egress.name}` : '新建策略'}
      subtitle="向导依次完成出口、LAN 范围与域名列表草稿（保存在 rosboard，不写入 RouterOS）；管理员密码只在最终应用设置时输入。"
      wide
      onClose={onClose}
    >
      <PolicyWizardSteps steps={WIZARD_STEPS} current={step} onJump={(i) => i <= step && setStep(i)} />
      {error ? <PolicyErrorDisplay error={error} /> : null}

      {step === 0 ? (
        <div className="policy-wizard-stage">
          <h4>出口与地址族</h4>
          <EgressDraftForm draft={draft} setDraft={setDraft} discovery={discovery} />
        </div>
      ) : null}

      {step === 1 ? (
        <LANScopeForm
          discovery={discovery}
          lanInterfaces={lanInterfaces}
          setLANInterfaces={setLANInterfaces}
        />
      ) : null}

      {step === 2 ? (
        <div className="policy-wizard-new-list">
          <h4>域名列表</h4>
          <p className="policy-hint">选择已有域名列表，或在此步骤添加新的来源。已绑定到其他出口的列表不会被自动改绑。</p>
          {overview.sources.length ? (
            <div className="policy-wizard-source-list">
              {overview.sources.filter((source) => !source.pendingDeletion).map((source) => {
                const boundElsewhere = Boolean(source.egressId && source.egressId !== egress?.id)
                return (
                  <label key={source.id} className={`policy-choice ${boundElsewhere ? 'policy-choice-disabled' : ''}`}>
                    <input
                      type="checkbox"
                      checked={selectedSourceIDs.has(source.id)}
                      disabled={boundElsewhere}
                      onChange={() => setSelectedSourceIDs((current) => {
                        const next = new Set(current)
                        if (next.has(source.id)) next.delete(source.id)
                        else next.add(source.id)
                        return next
                      })}
                    />
                    <span><strong>{source.name}</strong><small> · {source.type === 'url' ? '远程 URL' : '本地上传'}{boundElsewhere ? ' · 已绑定其他出口' : ''}</small></span>
                  </label>
                )
              })}
            </div>
          ) : null}
          <div className="policy-wizard-new-list-form">
            <PolicyField label="新列表名称">
              <input className="settings-input" value={newSourceName} onChange={(e) => { setNewSourceName(e.target.value); setPreview(null) }} placeholder="可选" />
            </PolicyField>
            <PolicyField label="类型">
              <div className="policy-choice-list">
                <label className="policy-choice"><input type="radio" checked={newSourceType === 'url'} onChange={() => setNewSourceType('url')} /><span>远程 URL</span></label>
                <label className="policy-choice"><input type="radio" checked={newSourceType === 'upload'} onChange={() => setNewSourceType('upload')} /><span>本地上传</span></label>
              </div>
            </PolicyField>
            {newSourceType === 'url' ? (
              <PolicyField label="URL">
                <input className="settings-input" value={newSourceURL} onChange={(e) => { setNewSourceURL(e.target.value); setPreview(null) }} placeholder="https://…" />
              </PolicyField>
            ) : (
              <PolicyField label="文件">
                <input type="file" className="policy-source-file" accept=".yaml,.yml,.txt" onChange={(e) => { setNewSourceFile(e.target.files?.[0] ?? null); setPreview(null) }} />
              </PolicyField>
            )}
            <PolicyField label="更新计划">
              <select className="select-control" value={newSourceSchedule} onChange={(e) => setNewSourceSchedule(e.target.value)}>
                {scheduleOptions.map((opt) => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
              </select>
            </PolicyField>
          </div>
        </div>
      ) : null}

      {step === 3 ? (
        <div className="policy-preview">
          {preview ? (
            <>
              <div className="policy-preview-stats">
                <span>有效规则：{preview.validRules}</span>
                {preview.sha256 ? <span>SHA-256：{preview.sha256.slice(0, 16)}…</span> : null}
                {preview.notModified ? <PolicyNotice tone="info">未修改</PolicyNotice> : null}
              </div>
              {preview.ignored && Object.keys(preview.ignored).length ? (
                <div className="policy-preview-errors">
                  <h4>忽略统计</h4>
                  {Object.entries(preview.ignored).map(([k, v]) => <span key={k}>{k}: {v}</span>)}
                </div>
              ) : null}
              {preview.errorSamples?.length ? (
                <div className="policy-preview-errors">
                  <h4>错误样本</h4>
                  <ul>{preview.errorSamples.slice(0, 5).map((s, i) => <li key={i}>{s}</li>)}</ul>
                </div>
              ) : null}
              {preview.rules.length ? (
                <div className="policy-preview-rules">
                  <code>{preview.rules.slice(0, 50).map((r) => `${r.type}: ${r.domain}`).join('\n')}</code>
                </div>
              ) : null}
            </>
          ) : (
            <PolicyNotice tone="info">{newSourceName ? '点击「解析预览」按钮预览域名列表' : '未提供域名列表，可跳过此步骤'}</PolicyNotice>
          )}
        </div>
      ) : null}

      {step === 4 ? (
        <div className="policy-draft-confirm">
          <h4>草稿确认</h4>
          <dl className="policy-meta">
            <div><dt>名称</dt><dd>{draft.name}</dd></div>
            <div><dt>优先级</dt><dd>{draft.priority}</dd></div>
            <div><dt>列表模式</dt><dd>{policyListModeLabel[draft.listMode] ?? draft.listMode}</dd></div>
            <div><dt>列表名称</dt><dd>{draft.listName || '—'}</dd></div>
            <div><dt>DNS 上游</dt><dd>{draft.dnsUpstream || '—'}</dd></div>
            <div><dt>断线处理</dt><dd>{policyFailureModeLabel[draft.failureMode] ?? draft.failureMode}</dd></div>
            <div><dt>地址族</dt><dd>{draft.families.filter((f) => f.enabled).map((f) => policyFamilyLabel[f.family]).join(' / ')}</dd></div>
            <div><dt>LAN 接口</dt><dd>{lanInterfaces.join(', ') || '—'}</dd></div>
            {newSourceName ? <div><dt>新域名列表</dt><dd>{newSourceName} ({newSourceType})</dd></div> : null}
          </dl>
          {plan?.readOnly ? <PolicyNotice tone="warn">此计划为只读</PolicyNotice> : null}
        </div>
      ) : null}

      {step === 5 ? (
        <div className="policy-wizard-apply">
          <p className="policy-hint">点击「生成计划」预览将要应用到 RouterOS 的变更，然后应用。</p>
          {plan ? (
            <ChangePlanView
              deviceID={deviceID}
              plan={plan.plan}
              adminPassword={adminPassword}
              onAdminPasswordChange={setAdminPassword}
              onApplied={(result) => { setPlan(null); onApplied(result); onClose() }}
              onCancel={() => setPlan(null)}
            />
          ) : (
            <button type="button" className="primary-button" disabled={saving} onClick={handleGeneratePlan}>
              {saving ? '生成中…' : '生成计划'}
            </button>
          )}
        </div>
      ) : null}

      {step < 5 ? (
        <div className="policy-form-actions policy-wizard-nav">
          <button type="button" className="close-button" onClick={step === 0 ? onClose : () => setStep((s) => Math.max(0, s - 1))} disabled={saving}>
            {step === 0 ? '取消' : '上一步'}
          </button>
          <button type="button" className="primary-button" disabled={!canNext || saving || (step === 4 && readOnly)} onClick={() => {
            if (step === 4) void handleSaveDraft()
            else setStep((s) => s + 1)
          }}>
            {step === 4 ? (saving ? '保存中…' : '保存草稿并继续') : '下一步'}
          </button>
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
                <option value="shared">共享标记列表（默认）</option>
                <option value="dedicated">每个域名列表专用标记列表</option>
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
              discovery={discovery}
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
  const knownInterface = interfaces.includes(family.wanInterface)
  const manualMode = !discoveryAvailable || manual || (family.wanInterface !== '' && !knownInterface)

  if ((discoveryAvailable || nextHop) && !manualMode) {
    return (
      <PolicyField label="策略接口" htmlFor={`policy-wan-${family.family}-select`} hint="选择真实 WAN 网卡，或选择“下一跳网关”改用主路由表可达的显式网关；特殊选项不会作为 wanInterface 发送。">
        <select
          id={`policy-wan-${family.family}-select`}
          className="settings-select"
          value={nextHop ? nextHopValue : knownInterface ? family.wanInterface : ''}
          onChange={(event) => {
            if (event.target.value === nextHopValue) onChange({ ...family, wanSource: 'next-hop', wanInterface: '' })
            else onChange({ ...family, wanSource: nextHop ? '' : family.wanSource, wanInterface: event.target.value })
            setManual(false)
          }}
        >
          {!nextHop && !family.wanInterface ? <option value="" disabled>请选择真实接口</option> : null}
          {!knownInterface && family.wanInterface ? <option value={family.wanInterface}>{family.wanInterface}（自定义）</option> : null}
          {wans.map((wan) => <option key={wan.interface} value={wan.interface}>{`${wan.interface}（${wan.type || '未知类型'}${wan.running ? '，运行中' : ''}）`}</option>)}
          <option value={nextHopValue}>下一跳网关</option>
        </select>
        <p className="policy-hint"><button type="button" className="link-button" onClick={() => setManual(true)}>手动输入接口名…</button></p>
      </PolicyField>
    )
  }

  return (
    <PolicyField label="策略接口" htmlFor={`policy-wan-${family.family}-mode`} hint={discoveryAvailable ? '可手动输入真实接口，也可直接选择“下一跳网关”；特殊选项不会作为 wanInterface 发送。' : '发现不可用时仍可选择“下一跳网关”；如使用真实接口，请手动填写接口名。'}>
      <select
        id={`policy-wan-${family.family}-mode`}
        className="settings-select"
        value={nextHop ? nextHopValue : 'manual'}
        onChange={(event) => {
          if (event.target.value === nextHopValue) onChange({ ...family, wanSource: 'next-hop', wanInterface: '' })
          else onChange({ ...family, wanSource: nextHop ? '' : family.wanSource })
        }}
      >
        <option value="manual">真实接口（手动输入）</option>
        <option value={nextHopValue}>下一跳网关</option>
      </select>
      {nextHop ? (
        <p className="policy-hint">下一跳模式不填写策略接口；请在高级设置填写必填的下一跳网关 IP。</p>
      ) : (
        <>
          <input
            id={`policy-wan-${family.family}-input`}
            className="settings-input"
            list={discoveryAvailable ? `policy-wan-${family.family}` : undefined}
            value={family.wanInterface}
            onChange={(event) => onChange({ ...family, wanSource: family.wanSource === 'next-hop' && event.target.value.trim() ? '' : family.wanSource, wanInterface: event.target.value })}
            placeholder="例如 pppoe-out1 / ether1 / wg-cf"
          />
          <datalist id={`policy-wan-${family.family}`}>
            {wans.map((wan) => <option key={wan.interface} value={wan.interface}>{`${wan.interface}（${wan.type || '未知类型'}${wan.running ? '，运行中' : ''}）`}</option>)}
          </datalist>
        </>
      )}
      {discoveryAvailable ? <p className="policy-hint"><button type="button" className="link-button" onClick={() => setManual(false)}>从候选接口选择</button></p> : null}
      {nextHop && family.wanInterface.trim() ? <p className="policy-field-error">旧数据同时带有下一跳模式和接口：请重新选择“下一跳网关”，或选择真实接口以清除该模式。</p> : null}
    </PolicyField>
  )
}

function EgressFamilyAdvancedFields({
  family,
  discovery,
  onChange,
}: {
  family: PolicyEgressFamily
  discovery: PolicyDiscovery | null
  onChange: (f: PolicyEgressFamily) => void
}) {
  const familyName = family.family === 'ipv6' ? 'IPv6' : 'IPv4'
  const routeSuggestions = discovery
    ? (discovery.wans.find((wan) => wan.interface === family.wanInterface)?.routes ?? [])
      .filter((route) => route.family === family.family)
      .map((route) => route.gateway || route.immediateGateway)
      .filter(Boolean)
    : []
  const nextHop = family.wanSource === 'next-hop'
  const update = (patch: Partial<PolicyEgressFamily>) => onChange({ ...family, ...patch })

  return (
    <div className="policy-form-grid">
      <PolicyField label={`下一跳网关（${familyName}）${nextHop ? '（必填）' : ''}`} hint={nextHop ? '必须填写单个 IP；网关必须能经 RouterOS 主路由表到达，旁路由回程/环路由请自行保证。' : '留空：沿用自动行为（点对点接口用接口路由，普通 WAN 用发现到的默认路径）；填写：显式下一跳 IP。'}>
        <input id={`policy-wan-gw-${family.family}-input`} className="settings-input" required={nextHop} list={`policy-wan-gw-${family.family}`} value={family.gateway} onChange={(event) => update({ gateway: event.target.value })} placeholder={nextHop ? '例如 192.0.2.1（必填）' : '留空表示使用接口路由'} />
        <datalist id={`policy-wan-gw-${family.family}`}>
          {routeSuggestions.map((gateway) => <option key={gateway} value={gateway} />)}
        </datalist>
      </PolicyField>
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
      {family.gateway.trim() ? <PolicyNotice tone="warn" title="显式下一跳（可能转发给局域网旁路由）"><span>策略将使用显式网关生成该地址族的默认路由。rosboard 不检查旁路由回程与策略环路，请自行确认网络拓扑，否则可能回环或断网；只对域名列表命中的流量生效，不会接管全部 LAN 流量。</span></PolicyNotice> : null}
      {nextHop && !family.gateway.trim() ? <PolicyNotice tone="warn" title="下一跳网关未填写">请选择与地址族匹配的单个 IP；保存和预览会被阻止，直到填写完成。</PolicyNotice> : null}
    </div>
  )
}

// ---- LAN scope form ----

function LANScopeForm({
  discovery,
  lanInterfaces,
  setLANInterfaces,
}: {
  discovery: PolicyDiscovery | null
  lanInterfaces: string[]
  setLANInterfaces: (v: string[]) => void
}) {
  const lanCandidates = discovery?.lan ?? []
  const [manualName, setManualName] = useState('')

  const toggleInterface = (name: string) => {
    setLANInterfaces(lanInterfaces.includes(name)
      ? lanInterfaces.filter((i) => i !== name)
      : [...lanInterfaces, name])
  }

  const addManualName = () => {
    const value = manualName.trim()
    if (!value || lanInterfaces.includes(value)) return
    setLANInterfaces([...lanInterfaces, value])
    setManualName('')
  }

  return (
    <div className="policy-wizard-lan">
      <h4>LAN 范围</h4>
      <p className="policy-hint">
        选择需要纳入策略路由的 LAN 接口。这些接口将用于识别本地终端并生成 mangle 规则。
      </p>
      {lanCandidates.length ? (
        <div className="policy-wizard-lan-list">
          {lanCandidates.map((candidate) => (
            <label key={candidate.name} className={`policy-choice ${candidate.frozen ? 'policy-choice-disabled' : ''}`}>
              <input
                type="checkbox"
                checked={lanInterfaces.includes(candidate.name)}
                onChange={() => toggleInterface(candidate.name)}
                disabled={candidate.frozen}
              />
              <span>
                <strong>{candidate.name}</strong>
                {candidate.kind ? <small> ({candidate.kind})</small> : null}
                {candidate.addresses?.length ? <small> {candidate.addresses.join(', ')}</small> : null}
              </span>
              {candidate.reason ? <small className="policy-hint">{candidate.reason}</small> : null}
            </label>
          ))}
        </div>
      ) : (
        <PolicyNotice tone="info">
          {discovery?.available ? '未发现 LAN 接口' : '设备发现不可用，可手动输入接口名称'}
        </PolicyNotice>
      )}
      {lanInterfaces.length ? (
        <div className="policy-lan-selected">
          <strong>已选接口：</strong>
          <span>{lanInterfaces.join(', ')}</span>
        </div>
      ) : null}
      <div className="policy-form-actions">
        <input
          className="settings-input"
          value={manualName}
          onChange={(event) => setManualName(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); addManualName() } }}
          placeholder="手动输入列表名…"
        />
        <button type="button" className="toolbar-button" disabled={!manualName.trim()} onClick={addManualName}>添加</button>
      </div>
    </div>
  )
}
