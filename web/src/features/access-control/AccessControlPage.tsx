import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  PolicyEmptyState,
  PolicyErrorDisplay,
  PolicyField,
  PolicyModal,
  PolicyNotice,
  PolicyPreparing,
  PolicyStatusBadge,
  type StatusTone,
} from '../policy-routing/components'
import {
  AccessApiError,
  createAccessRule,
  deleteAccessRule,
  loadAccessOverview,
  reapplyAccessControl,
  updateAccessRule,
  waitForAccessJob,
} from './api'
import type { AccessApplyResult, AccessBinding, AccessEgressCandidates, AccessJob, AccessOverview, AccessRule, AccessRuleInput, AccessRuleMemberInput, AccessSource, AccessTargetScope, AccessTerminal } from './types'

const statusPresentation: Record<AccessRule['status'], { tone: StatusTone; label: string }> = {
  applied: { tone: 'bad', label: '阻止中' },
  applying: { tone: 'info', label: '正在应用' },
  pending: { tone: 'warn', label: '待应用' },
  failed: { tone: 'bad', label: '应用失败' },
  degraded: { tone: 'warn', label: '部分降级' },
  disabled: { tone: 'neutral', label: '已停用' },
}

function ruleToInput(rule: AccessRule): AccessRuleInput {
  return {
    id: rule.id, name: rule.name, targetScope: rule.targetScope, sourceIds: [...rule.sourceIds],
    enabled: rule.enabled, revision: rule.revision,
    members: rule.members.map((member) => {
      if (member.binding === 'fixed') {
        return {
          terminalId: member.terminalId, binding: 'fixed' as const,
          pinnedIpv4: [...member.ipv4], pinnedIpv6: [...member.ipv6],
        }
      }
      return { terminalId: member.terminalId, binding: 'auto' as const, pinnedIpv4: [], pinnedIpv6: [] }
    }),
  }
}

function sourceHasPendingVersion(source: AccessSource): boolean {
  return source.versions.some((version) => version.state === 'pending')
}

export function AccessControlPage({ deviceID, refreshNonce }: { deviceID: string; refreshNonce: number }) {
  const [overview, setOverview] = useState<AccessOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
	const [status, setStatus] = useState<string | null>(null)
	const [editing, setEditing] = useState<AccessRule | null | undefined>(undefined)
	const [deleting, setDeleting] = useState<AccessRule | null>(null)
	const [egressChoice, setEgressChoice] = useState<AccessEgressCandidates | null>(null)

  const reload = useCallback(async () => {
    if (!deviceID) return
    try {
      const next = await loadAccessOverview(deviceID)
      next.rules ??= []
      next.sources ??= []
      next.terminals ??= []
      // 后端旧版本可能把空数组序列化成 null；统一归一化，避免 .length / 展开时页面崩溃。
      next.rules = next.rules.map((rule) => ({ ...rule, sourceIds: rule.sourceIds ?? [], members: (rule.members ?? []).map((member) => ({ ...member, ipv4: member.ipv4 ?? [], ipv6: member.ipv6 ?? [] })), issues: rule.issues ?? [] }))
      next.terminals = next.terminals.map((terminal) => ({ ...terminal, ipv4: terminal.ipv4 ?? [], ipv6: terminal.ipv6 ?? [] }))
      next.sources = next.sources.map((source) => ({ ...source, versions: source.versions ?? [] }))
      setOverview(next)
      setError(null)
    } catch (loadError) {
      setError(loadError)
    } finally {
      setLoading(false)
    }
  }, [deviceID])

  useEffect(() => { setLoading(true); void reload() }, [reload, refreshNonce])

  useEffect(() => {
    const timer = window.setInterval(() => void reload(), 5000)
    return () => window.clearInterval(timer)
  }, [reload])

  useEffect(() => {
    if (!status) return
    const timer = window.setTimeout(() => setStatus(null), 5000)
    return () => window.clearTimeout(timer)
  }, [status])

  const runApply = async (action: () => Promise<AccessApplyResult>, savedMessage: string, deferredMessage: string): Promise<boolean> => {
    setStatus(null)
    const result = await action()
    const jobID = result.jobId ?? result.job?.id
    if (!jobID) throw new Error('服务未返回 RouterOS 应用任务，无法确认规则是否已生效')
    let applied = true
    try {
      await waitForAccessJob(deviceID, jobID)
      setStatus(savedMessage)
    } catch (applyError) {
      // 规则已保存，只是 RouterOS 应用暂未成功；不要把用户刚保存的规则当成失败丢弃。
      applied = false
      setStatus(deferredMessage)
      setError(applyError)
    }
    await reload()
    return applied
  }

  const handleReapply = async () => {
    setError(null)
    try {
      await runApply(() => reapplyAccessControl(deviceID), '已重新应用到 RouterOS。', '已重新应用，但 RouterOS 暂未完成。')
    } catch (reapplyError) {
      if (reapplyError instanceof AccessApiError && Object.keys(reapplyError.internetEgressCandidates).length > 0) {
        setEgressChoice(reapplyError.internetEgressCandidates)
        return
      }
      setError(reapplyError)
    }
  }

  if (loading && !overview) return <PolicyPreparing text="正在读取访问控制状态…" />
  if (error && !overview) return <PolicyErrorDisplay error={error} />
  if (!overview) return <PolicyPreparing text="正在读取访问控制状态…" />

  const terminalByID = new Map(overview.terminals.map((terminal) => [terminal.id, terminal]))
  const sourceByID = new Map(overview.sources.map((source) => [source.id, source]))
  const activeJob: AccessJob | null = overview.job?.id && overview.job.state !== 'committed' && overview.job.state !== 'failed' ? overview.job : null
  const needsAttention = overview.rules.some((rule) => rule.status === 'failed' || rule.status === 'pending' || rule.status === 'degraded')

  return (
    <div className="policy-page access-page">
      {status ? <PolicyNotice tone={error ? 'warn' : 'good'}>{status}</PolicyNotice> : null}
      {error ? <PolicyErrorDisplay error={error} /> : null}
      <details className="policy-advanced">
        <summary>访问控制说明（域名解析边界）</summary>
        <div className="policy-advanced-body">
          <PolicyNotice tone="warn" title="能力边界">{overview.boundary}</PolicyNotice>
        </div>
      </details>
      {activeJob ? <PolicyNotice tone="info">正在应用到 RouterOS，阶段 {activeJob.phase || activeJob.state}。</PolicyNotice> : null}

      <section className="panel policy-panel" aria-label="访问规则">
        <div className="policy-panel-head">
          <div>
            <h3>访问规则</h3>
            <p className="policy-hint">设备：{overview.device.name}</p>
          </div>
          <div className="policy-head-actions">
            {needsAttention ? (
              <button type="button" className="toolbar-button" disabled={Boolean(activeJob)} onClick={() => void handleReapply()}>重新应用</button>
            ) : null}
            <button type="button" className="primary-button" disabled={!overview.device.enabled} onClick={() => setEditing(null)}>新增规则</button>
          </div>
        </div>

        {overview.rules.length === 0 ? (
          <PolicyEmptyState
            title="尚未配置访问规则"
            description="一条规则可以选择多台设备，阻断整个互联网或多个网站 / IP 列表，保存后自动应用。"
            action={<button type="button" className="primary-button" disabled={!overview.device.enabled} onClick={() => setEditing(null)}>新增规则</button>}
          />
        ) : (
          <div className="policy-table-scroll">
            <table className="data-table access-policy-table">
              <thead><tr><th>规则</th><th>设备</th><th>访问范围</th><th>生效条件</th><th>状态</th><th>操作</th></tr></thead>
              <tbody>{overview.rules.map((rule) => (
                <AccessRuleRow
                  key={rule.id}
                  rule={rule}
                  terminals={rule.members.map((member) => terminalByID.get(member.terminalId)).filter((terminal): terminal is AccessTerminal => Boolean(terminal))}
                  sources={rule.sourceIds.map((sourceID) => sourceByID.get(sourceID)).filter((source): source is AccessSource => Boolean(source))}
                  busy={Boolean(activeJob)}
                  onEdit={() => setEditing(rule)}
                  onDelete={() => setDeleting(rule)}
                  onToggle={async () => {
                    setError(null)
                    try {
                      await runApply(
                        () => updateAccessRule(deviceID, { ...ruleToInput(rule), enabled: !rule.enabled }),
                        rule.enabled ? '规则已停用。' : '规则已启用并生效。',
                        rule.enabled ? '规则已保存，但暂未应用到 RouterOS。' : '规则已保存，但暂未应用到 RouterOS。',
                      )
                    } catch (toggleError) { setError(toggleError) }
                  }}
                />
              ))}</tbody>
            </table>
          </div>
        )}
      </section>

      {editing !== undefined ? (
        <AccessRuleModal
          rule={editing}
          terminals={overview.terminals}
          sources={overview.sources}
          onClose={() => setEditing(undefined)}
          onSave={async (input) => {
            await runApply(
              () => input.revision ? updateAccessRule(deviceID, input) : createAccessRule(deviceID, input),
              '规则已保存并生效。',
              '规则已保存，但暂未应用到 RouterOS。',
            )
            setEditing(undefined)
          }}
          onEgressSelectionRequired={(candidates) => {
            setEditing(undefined)
            setStatus('规则已保存，但尚未应用；请确认互联网出口。')
            setEgressChoice(candidates)
          }}
        />
      ) : null}

      {egressChoice ? (
        <AccessEgressPickerModal
          candidates={egressChoice}
          onClose={() => setEgressChoice(null)}
          onApply={async (selected) => {
            const applied = await runApply(
              () => reapplyAccessControl(deviceID, selected),
              '已按所选互联网出口应用访问规则。',
              '规则已保存，但 RouterOS 暂未完成应用。',
            )
            if (applied) setEgressChoice(null)
          }}
        />
      ) : null}

      {deleting ? (
        <DeleteAccessRuleModal
          rule={deleting}
          onClose={() => setDeleting(null)}
          onDelete={async () => {
            setError(null)
            try {
              const result = await deleteAccessRule(deviceID, deleting.id, deleting.revision)
              const jobID = result.jobId ?? result.job?.id
              try {
                if (!jobID) throw new Error('服务未返回 RouterOS 清理任务，无法确认规则是否已清理')
                await waitForAccessJob(deviceID, jobID)
                setStatus('规则已删除并从 RouterOS 清理。')
              } catch {
                setStatus('规则已删除，但 RouterOS 上的旧版 rosboard 策略未能自动清理；请登录 RouterOS 手动确认并清理相关策略对象，完成后再回到访问控制重新应用。')
                setDeleting(null)
                await reload()
                return
              }
              setDeleting(null)
              await reload()
            } catch (deleteError) {
              if (deleteError instanceof AccessApiError && deleteError.deleted && deleteError.desiredSaved) {
                setStatus('规则已删除，但 RouterOS 上的旧版 rosboard 策略未能自动清理；请登录 RouterOS 手动确认并清理相关策略对象，完成后再回到访问控制重新应用。')
                setDeleting(null)
                await reload()
                return
              }
              setError(deleteError)
            }
          }}
        />
      ) : null}
    </div>
  )
}

function AccessRuleRow({ rule, terminals, sources, busy, onEdit, onDelete, onToggle }: {
  rule: AccessRule
  terminals: AccessTerminal[]
  sources: AccessSource[]
  busy: boolean
  onEdit: () => void
  onDelete: () => void
  onToggle: () => Promise<void>
}) {
  const [toggling, setToggling] = useState(false)
  const presentation = statusPresentation[rule.status] ?? { tone: 'neutral' as const, label: rule.status }
  const deviceNames = terminals.map((terminal) => terminal.displayName || terminal.id).join('、')
  return (
    <tr className={rule.enabled ? '' : 'disabled-row'}>
      <td><strong>{rule.name}</strong></td>
      <td>
        <strong>{rule.members.length} 台</strong>
        <span className="policy-sub-cell">{deviceNames || '—'}</span>
      </td>
      <td>
        <strong>{rule.targetScope === 'internet' ? '整个互联网' : `${sources.length} 个网站 / IP 列表`}</strong>
        <span className="policy-sub-cell">{rule.targetScope === 'internet' ? '局域网访问不受影响' : sources.map((source) => source.name).join('、') || '—'}</span>
      </td>
      <td>始终</td>
      <td>
        <PolicyStatusBadge tone={presentation.tone}>{presentation.label}</PolicyStatusBadge>
        {rule.issues.length > 0 ? <span className="policy-sub-cell">{rule.issues[0]}</span> : null}
      </td>
      <td><div className="action-links">
        <button type="button" className="link-button" disabled={busy || toggling} onClick={() => { setToggling(true); void onToggle().finally(() => setToggling(false)) }}>{toggling ? '处理中…' : rule.enabled ? '停用' : '启用'}</button>
        <button type="button" className="link-button" disabled={busy} onClick={onEdit}>编辑</button>
        <button type="button" className="link-button link-button--danger" disabled={busy} onClick={onDelete}>删除</button>
      </div></td>
    </tr>
  )
}

function AccessRuleModal({ rule, terminals, sources, onClose, onSave, onEgressSelectionRequired }: {
  rule: AccessRule | null
  terminals: AccessTerminal[]
  sources: AccessSource[]
  onClose: () => void
  onSave: (input: AccessRuleInput) => Promise<void>
  onEgressSelectionRequired: (candidates: AccessEgressCandidates) => void
}) {
  const editing = Boolean(rule)
  const [name, setName] = useState(rule?.name ?? '')
  const [targetScope, setTargetScope] = useState<AccessTargetScope>(rule?.targetScope ?? 'internet')
  const [selectedTerminals, setSelectedTerminals] = useState<string[]>(() => rule ? rule.members.map((member) => member.terminalId) : [])
  const [selectedSources, setSelectedSources] = useState<string[]>(() => rule ? [...rule.sourceIds] : [])
  const [binding, setBinding] = useState<AccessBinding>(() => {
    const bindings = new Set(rule?.members.map((member) => member.binding) ?? [])
    return bindings.size === 1 && bindings.has('fixed') ? 'fixed' : 'auto'
  })
  const [bindingChanged, setBindingChanged] = useState(false)
  const [enabled, setEnabled] = useState(rule?.enabled ?? true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)

  const terminalByID = useMemo(() => new Map(terminals.map((terminal) => [terminal.id, terminal])), [terminals])
  const existingMemberByID = useMemo(() => new Map((rule?.members ?? []).map((member) => [member.terminalId, member])), [rule])
  const availableSources = useMemo(() => {
    const existingIDs = new Set(rule?.sourceIds ?? [])
    return sources.filter((source) => existingIDs.has(source.id) || (
      source.enabled && !source.pendingDeletion &&
      (source.activeVersionId !== '' || sourceHasPendingVersion(source))
    ))
  }, [rule, sources])

  const effectiveBinding = useCallback((terminalID: string): AccessBinding => {
    if (!bindingChanged) return existingMemberByID.get(terminalID)?.binding ?? binding
    return binding
  }, [binding, bindingChanged, existingMemberByID])

  const toggleTerminal = (terminalID: string) => {
    setSelectedTerminals((current) => current.includes(terminalID) ? current.filter((id) => id !== terminalID) : [...current, terminalID])
  }
  const toggleSource = (sourceID: string) => {
    setSelectedSources((current) => current.includes(sourceID) ? current.filter((id) => id !== sourceID) : [...current, sourceID])
  }

  const fixedPins = useMemo(() => {
    if (binding !== 'fixed' && !selectedTerminals.some((terminalID) => effectiveBinding(terminalID) === 'fixed')) return []
    return selectedTerminals.flatMap((terminalID) => {
      const existing = existingMemberByID.get(terminalID)
      if (!bindingChanged && existing?.binding === 'fixed') {
        return [...existing.ipv4, ...existing.ipv6].map((address) => ({ terminalID, address }))
      }
      const terminal = terminalByID.get(terminalID)
      if (effectiveBinding(terminalID) !== 'fixed') return []
      if (!terminal) return []
      return [...terminal.ipv4, ...terminal.ipv6].map((address) => ({ terminalID, address }))
    })
  }, [binding, bindingChanged, effectiveBinding, existingMemberByID, selectedTerminals, terminalByID])

  const saveDisabled = saving || !name.trim() || selectedTerminals.length === 0 || (targetScope === 'sources' && selectedSources.length === 0)

  const submit = async () => {
    setSaving(true)
    setError(null)
    try {
      const members: AccessRuleMemberInput[] = selectedTerminals.map((terminalID) => {
        const memberBinding = effectiveBinding(terminalID)
        const existing = existingMemberByID.get(terminalID)
        if (memberBinding === 'fixed' && !bindingChanged && existing?.binding === 'fixed') {
          return { terminalId: terminalID, binding: 'fixed' as const, pinnedIpv4: [...existing.ipv4], pinnedIpv6: [...existing.ipv6] }
        }
        if (memberBinding === 'fixed') {
          // 固定当前 IP：把每台设备当前观察到的地址固定下来。
          const terminal = terminalByID.get(terminalID)
          return { terminalId: terminalID, binding: 'fixed' as const, pinnedIpv4: terminal ? [...terminal.ipv4] : [], pinnedIpv6: terminal ? [...terminal.ipv6] : [] }
        }
        return { terminalId: terminalID, binding: 'auto' as const, pinnedIpv4: [], pinnedIpv6: [] }
      })
      await onSave({
        id: rule?.id ?? '', name: name.trim(), targetScope, sourceIds: targetScope === 'sources' ? selectedSources : [],
        enabled, revision: rule?.revision ?? 0, members,
      })
    } catch (saveError) {
      if (saveError instanceof AccessApiError && saveError.desiredSaved && Object.keys(saveError.internetEgressCandidates).length > 0) {
        onEgressSelectionRequired(saveError.internetEgressCandidates)
        return
      }
      setError(saveError)
    } finally {
      setSaving(false)
    }
  }

  const eligibleTerminals = useMemo(() => {
    const currentIDs = new Set(terminals.map((terminal) => terminal.id))
    const existingIDs = new Set((rule?.members ?? []).map((member) => member.terminalId))
    const current = terminals.filter((terminal) => existingIDs.has(terminal.id) || terminal.ipv4.length + terminal.ipv6.length > 0)
    const missingExisting = (rule?.members ?? [])
      .filter((member) => !currentIDs.has(member.terminalId))
      .map((member) => ({ id: member.terminalId, displayName: member.terminalId, ipv4: [], ipv6: [], autoEligible: false }))
    return [...current, ...missingExisting]
  }, [binding, rule, terminals])
  const mixedBinding = Boolean(rule && new Set(rule.members.map((member) => member.binding)).size > 1)

  return (
    <PolicyModal
      title={editing ? '编辑访问规则' : '新增访问规则'}
      wide
      onClose={onClose}
      footer={<>
        <button type="button" className="toolbar-button" disabled={saving} onClick={onClose}>取消</button>
        <button type="button" className="primary-button" disabled={saveDisabled} onClick={() => void submit()}>{saving ? '正在保存…' : '保存'}</button>
      </>}
    >
      <div className="policy-form access-rule-form">
        {error ? <PolicyErrorDisplay error={error} /> : null}
        <PolicyField label="规则名称" htmlFor="access-rule-name">
          <input id="access-rule-name" className="settings-input" value={name} placeholder="例如：儿童娱乐限制" onChange={(event) => setName(event.target.value)} />
        </PolicyField>

        <PolicyField label="控制设备" hint={`已选择 ${selectedTerminals.length} 台`}>
          <div className="access-multi-select">
            {eligibleTerminals.map((terminal) => (
              <label key={terminal.id} className={`access-multi-option${selectedTerminals.includes(terminal.id) ? ' selected' : ''}`}>
                <input type="checkbox" checked={selectedTerminals.includes(terminal.id)} onChange={() => toggleTerminal(terminal.id)} />
                <span><strong>{terminal.displayName || terminal.id}</strong><small>{terminal.autoEligible ? '自动跟随可用' : '仅支持固定当前 IP'}</small></span>
              </label>
            ))}
            {eligibleTerminals.length === 0 ? <p className="policy-hint">当前没有可控制的设备：需要可靠终端身份和至少一个观察到的地址。</p> : null}
          </div>
          {selectedTerminals.length > 0 ? (
            <p className="policy-hint">已选择：{selectedTerminals.map((terminalID) => terminalByID.get(terminalID)?.displayName || terminalID).join('、')}</p>
          ) : null}
        </PolicyField>

        <PolicyField label="访问范围">
          <div className="access-scope-options">
            <label className={`access-multi-option${targetScope === 'internet' ? ' selected' : ''}`}>
              <input type="radio" name="access-target-scope" checked={targetScope === 'internet'} onChange={() => setTargetScope('internet')} />
              <span><strong>整个互联网</strong><small>禁止所选设备访问互联网，局域网访问不受影响</small></span>
            </label>
            <label className={`access-multi-option${targetScope === 'sources' ? ' selected' : ''}`}>
              <input type="radio" name="access-target-scope" checked={targetScope === 'sources'} onChange={() => setTargetScope('sources')} />
              <span><strong>指定网站 / IP</strong><small>只禁止所选设备访问选中的列表</small></span>
            </label>
          </div>
        </PolicyField>

        {targetScope === 'sources' ? (
          <PolicyField label="网站 / IP 列表" hint="可多选">
            <div className="access-multi-select">
              {availableSources.map((source) => {
                const pendingOnly = source.activeVersionId === '' && sourceHasPendingVersion(source)
                const selected = selectedSources.includes(source.id)
                return (
                  <label key={source.id} className={`access-multi-option${selected ? ' selected' : ''}${pendingOnly ? ' disabled' : ''}`}>
                    <input
                      type="checkbox"
                      checked={selected}
                      disabled={pendingOnly && !selected}
                      onChange={() => toggleSource(source.id)}
                    />
                    <span>
                      <strong>{source.name}</strong>
                      <small>{source.kind === 'ip' ? 'IP 列表' : '域名列表'}{pendingOnly ? ' · 待应用，请先在策略路由中应用完整计划' : ''}</small>
                    </span>
                  </label>
                )
              })}
              {availableSources.length === 0 ? <p className="policy-hint">暂无可用来源，请先在策略路由中创建网站 / IP 列表。</p> : null}
              {availableSources.some((source) => source.activeVersionId === '' && sourceHasPendingVersion(source)) ? (
                <p className="policy-hint">标记为“待应用”的列表已保存但尚未同步到 RouterOS；请先在策略路由页面生成并应用完整计划，完成后刷新此页面即可选择。</p>
              ) : null}
            </div>
          </PolicyField>
        ) : null}

        <PolicyField label="控制方式">
          <div className="access-mode-list">
            <label className="access-mode-option"><input type="radio" name="access-action" checked readOnly /> <span>禁止访问</span></label>
            <p className="policy-hint">限制访问时间、每日可访问时长将在后续版本提供。</p>
          </div>
        </PolicyField>

        <details className="policy-advanced">
          <summary>高级设置</summary>
          <div className="policy-advanced-body">
            <div className="access-binding-options">
              <label className={`access-multi-option${binding === 'auto' ? ' selected' : ''}`}>
                <input type="radio" name="access-binding" checked={binding === 'auto'} onChange={() => { setBinding('auto'); setBindingChanged(binding !== 'auto') }} />
                <span><strong>自动跟随设备地址（推荐）</strong><small>设备地址变化时自动保持阻断；设备暂时离线时保留最后已知地址。</small></span>
              </label>
              <label className={`access-multi-option${binding === 'fixed' ? ' selected' : ''}`}>
                <input type="radio" name="access-binding" checked={binding === 'fixed'} onChange={() => { setBinding('fixed'); setBindingChanged(binding !== 'fixed') }} />
                <span><strong>固定当前 IP</strong><small>只对固定地址生效，适合没有可靠身份的设备。</small></span>
              </label>
            </div>
            {mixedBinding && !bindingChanged ? <p className="policy-hint">当前规则包含两种控制方式；未选择新的控制方式时，将保留每台设备原有设置。</p> : null}
            {binding === 'fixed' && fixedPins.length > 0 ? (
              <p className="policy-hint mono">将固定：{fixedPins.map((pin) => pin.address).join('、')}</p>
            ) : null}
          </div>
        </details>

        <label className="policy-checkbox"><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} /><span>启用此规则</span></label>
      </div>
    </PolicyModal>
  )
}

function AccessEgressPickerModal({ candidates, onClose, onApply }: {
  candidates: AccessEgressCandidates
  onClose: () => void
  onApply: (selected: Record<string, string[]>) => Promise<void>
}) {
  const families = Object.entries(candidates).filter(([, values]) => values.length > 0)
  const [selected, setSelected] = useState<Record<string, string[]>>({})
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<unknown>(null)

  const toggle = (family: string, interfaceName: string) => {
    setSelected((current) => {
      const values = current[family] ?? []
      return {
        ...current,
        [family]: values.includes(interfaceName) ? values.filter((value) => value !== interfaceName) : [...values, interfaceName],
      }
    })
  }

  const ready = families.length > 0 && families.every(([family]) => (selected[family]?.length ?? 0) > 0)
  const submit = async () => {
    setApplying(true)
    setError(null)
    try {
      await onApply(selected)
    } catch (applyError) {
      setError(applyError)
    } finally {
      setApplying(false)
    }
  }

  return (
    <PolicyModal
      title="确认互联网出口"
      wide
      onClose={onClose}
      footer={<>
        <button type="button" className="toolbar-button" disabled={applying} onClick={onClose}>取消</button>
        <button type="button" className="primary-button" disabled={!ready || applying} onClick={() => void submit()}>{applying ? '正在应用…' : '按所选出口应用'}</button>
      </>}
    >
      {error ? <PolicyErrorDisplay error={error} /> : null}
      <PolicyNotice tone="warn" title="RouterOS 默认路由暂时无法自动确认">
        请按地址族勾选实际连接互联网的接口。勾选多个接口时，rosboard 会把它们放入同一个 interface list，再生成一组双向阻断规则；未勾选的接口不会被用于本次规则。
      </PolicyNotice>
      {families.map(([family, values]) => (
        <PolicyField key={family} label={family === 'ipv6' ? 'IPv6 出口接口' : 'IPv4 出口接口'} hint="至少选择一个；备用线路也可以一并选择。">
          <div className="access-multi-select">
            {values.map((candidate) => (
              <label key={`${family}:${candidate.interface}`} className={`access-multi-option${selected[family]?.includes(candidate.interface) ? ' selected' : ''}`}>
                <input type="checkbox" checked={selected[family]?.includes(candidate.interface) ?? false} onChange={() => toggle(family, candidate.interface)} />
                <span>
                  <strong>{candidate.interface}</strong>
                  <small>{candidate.type || 'RouterOS 接口'} · {candidate.running ? '运行中' : '当前未运行'}{candidate.reason ? ` · ${candidate.reason}` : ''}</small>
                </span>
              </label>
            ))}
          </div>
        </PolicyField>
      ))}
      {families.length === 0 ? <PolicyNotice tone="bad" title="没有可供人工确认的接口">请先检查 RouterOS 账号读取权限和接口配置。</PolicyNotice> : null}
    </PolicyModal>
  )
}

function DeleteAccessRuleModal({ rule, onClose, onDelete }: {
  rule: AccessRule
  onClose: () => void
  onDelete: () => Promise<void>
}) {
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState<unknown>(null)
  return (
    <PolicyModal title="删除访问规则" onClose={onClose} footer={<>
      <button type="button" className="toolbar-button" disabled={deleting} onClick={onClose}>取消</button>
      <button type="button" className="primary-button policy-apply-button" disabled={deleting} onClick={() => { setDeleting(true); setError(null); void onDelete().catch(setError).finally(() => setDeleting(false)) }}>{deleting ? '正在删除…' : '确认删除'}</button>
    </>}>
      {error ? <PolicyErrorDisplay error={error} /> : null}
      <p className="policy-hint">删除“{rule.name}”后，将自动清理这台 RouterOS 上对应的受管阻断规则。</p>
    </PolicyModal>
  )
}
