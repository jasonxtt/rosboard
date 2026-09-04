import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  fetchApplicationPresets,
  previewApplicationPreset,
  type ApplicationPresetSelection,
  type ApplicationPreset,
  type PolicyTerminal,
  type PresetPreview,
  type Subject,
  type SubjectBinding,
  type TargetList,
} from './canonical'

const emptySubject: Subject = { mode: 'selected', members: [], prefixes: [] }
type PresetKind = 'domain' | 'ip'
type PresetChoice = '' | 'domain' | 'ip' | 'both'

type PresetPresentation = ApplicationPresetSelection & { name: string }

function presetChoiceLabel(choice: PresetChoice) {
  if (choice === 'both') return '域名/IP'
  return choice === 'ip' ? 'IP' : '域名'
}

function selectedKindsForPreset(selectedIDs: string[], presetID: string): PresetKind[] {
  return (['domain', 'ip'] as const).filter((kind) => selectedIDs.includes(`preset:${presetID}:${kind}`))
}

function presetPresentationsFor(presets: ApplicationPreset[], selectedIDs: string[], previews: Record<string, PresetPreview>): PresetPresentation[] {
  return presets.filter((preset) => selectedKindsForPreset(selectedIDs, preset.id).length > 0).map((preset) => ({
    presetId: preset.id,
    name: preset.name,
    previewId: previews[preset.id]?.previewId ?? '',
    requestedKinds: selectedKindsForPreset(selectedIDs, preset.id),
  }))
}

function terminalAddresses(terminal: PolicyTerminal, routingOnly: boolean) {
  return routingOnly ? { ipv4: terminal.routingIpv4, ipv6: terminal.routingIpv6 } : { ipv4: terminal.ipv4, ipv6: terminal.ipv6 }
}

export function SubjectSelector({
  terminals,
  value = emptySubject,
  onChange,
  allowExcluded = false,
  excludedDisabled = false,
  requireObservedAddress = false,
}: {
  terminals: PolicyTerminal[]
  value?: Subject
  onChange: (value: Subject) => void
  allowExcluded?: boolean
  excludedDisabled?: boolean
  requireObservedAddress?: boolean
}) {
  const [query, setQuery] = useState('')
  const selected = new Map(value.members.map((member) => [member.terminalId, member]))
  const visibleTerminals = useMemo(() => {
    const candidates = terminals.filter((terminal) => {
      const addresses = terminalAddresses(terminal, requireObservedAddress)
      return !requireObservedAddress || addresses.ipv4.length + addresses.ipv6.length > 0
    })
    const keyword = query.trim().toLowerCase()
    if (!keyword) return candidates
    return candidates.filter((terminal) => {
      const addresses = terminalAddresses(terminal, requireObservedAddress)
      return [terminal.displayName, terminal.id, terminal.macAddress, ...addresses.ipv4, ...addresses.ipv6].join(' ').toLowerCase().includes(keyword)
    })
  }, [query, requireObservedAddress, terminals])

  const update = (next: Partial<Subject>) => onChange({ ...value, ...next, members: next.members ?? value.members, prefixes: next.prefixes ?? value.prefixes })
  const toggleTerminal = (terminal: PolicyTerminal) => {
    const members = [...value.members]
    const index = members.findIndex((member) => member.terminalId === terminal.id)
    if (index >= 0) members.splice(index, 1)
    else {
      const addresses = terminalAddresses(terminal, requireObservedAddress)
      const binding = terminal.autoEligible ? 'auto' : 'fixed'
      members.push({ terminalId: terminal.id, binding, pinnedIpv4: binding === 'fixed' ? [...addresses.ipv4] : [], pinnedIpv6: binding === 'fixed' ? [...addresses.ipv6] : [] })
    }
    update({ members })
  }
  const changeBinding = (terminal: PolicyTerminal, binding: SubjectBinding) => {
    const addresses = terminalAddresses(terminal, requireObservedAddress)
    const members = value.members.map((member) => member.terminalId === terminal.id
      ? { ...member, binding, pinnedIpv4: binding === 'fixed' ? [...addresses.ipv4] : [], pinnedIpv6: binding === 'fixed' ? [...addresses.ipv6] : [] }
      : member)
    update({ members })
  }

  return <div className="canonical-selector">
    <div className="policy-choice-list" role="radiogroup" aria-label="受控对象范围">
      <label className={`policy-choice${value.mode === 'all' ? ' active' : ''}`}><input type="radio" checked={value.mode === 'all'} onChange={() => update({ mode: 'all', members: [], prefixes: [] })} /><span><strong>全部设备</strong><small>由策略流量入口限定范围</small></span></label>
      <label className={`policy-choice${value.mode === 'selected' ? ' active' : ''}`}><input type="radio" checked={value.mode === 'selected'} onChange={() => update({ mode: 'selected' })} /><span><strong>仅选择的设备 / 地址</strong><small>只使用下面的终端和手动地址作为来源匹配</small></span></label>
      {allowExcluded ? <label className={`policy-choice${value.mode === 'excluded' ? ' active' : ''}${excludedDisabled ? ' disabled' : ''}`}><input type="radio" checked={value.mode === 'excluded'} disabled={excludedDisabled} onChange={() => update({ mode: 'excluded' })} /><span><strong>入口内排除设备 / 地址</strong><small>{excludedDisabled ? '需要先选择有效的 TrafficIngress' : '先匹配 TrafficIngress，再排除下面的终端和地址'}</small></span></label> : null}
    </div>
    {value.mode === 'selected' || value.mode === 'excluded' ? <>
      <input className="settings-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索名称、IP 或 MAC" aria-label="搜索受控设备" />
      <div className="canonical-terminal-list">
        {visibleTerminals.map((terminal) => {
          const member = selected.get(terminal.id)
          const addresses = terminalAddresses(terminal, requireObservedAddress)
          return <div key={terminal.id} className={`canonical-terminal-option${member ? ' selected' : ''}`}>
            <label><input type="checkbox" checked={Boolean(member)} onChange={() => toggleTerminal(terminal)} /><span><strong>{terminal.displayName || terminal.id}</strong><small>{addresses.ipv4.join(', ') || '无 IPv4'} · {addresses.ipv6.join(', ') || '无 IPv6'} · {terminal.macAddress || '无 MAC'}</small></span></label>
            {member ? <select className="select-control" value={member.binding} onChange={(event) => changeBinding(terminal, event.target.value as SubjectBinding)}><option value="auto" disabled={!terminal.autoEligible}>自动跟随</option><option value="fixed">固定当前地址</option></select> : null}
            {member && requireObservedAddress && !terminal.autoEligible ? <small className="policy-hint">未获取到可靠 MAC，无法自动跟随 IP 变化，已固定使用当前地址。</small> : null}
          </div>
        })}
        {!visibleTerminals.length ? <p className="policy-hint">没有匹配的终端。</p> : null}
      </div>
      <label className="policy-field canonical-prefix-field"><span>{value.mode === 'excluded' ? '排除地址 / CIDR' : '手动地址 / CIDR'}</span><textarea className="settings-input policy-textarea" rows={3} value={value.prefixes.join('\n')} onChange={(event) => update({ prefixes: event.target.value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean) })} placeholder={'192.168.1.50\n192.168.1.0/24\n2001:db8::/64'} /><small>每行一个 IPv4、IPv6、IPv4 CIDR 或 IPv6 CIDR；后端会执行严格校验。</small></label>
    </> : null}
  </div>
}

export function TargetSelector({ deviceID, targetLists, selectedIDs, onChange, onPresetPresentationChange, onCreateTargetList }: { deviceID: string; targetLists: TargetList[]; selectedIDs: string[]; onChange: (ids: string[]) => void; onPresetPresentationChange?: (value: PresetPresentation[]) => void; onCreateTargetList?: (kind: PresetKind) => void }) {
  const [presets, setPresets] = useState<ApplicationPreset[]>([])
  const [previews, setPreviews] = useState<Record<string, PresetPreview>>({})
  const [category, setCategory] = useState('')
  const [query, setQuery] = useState('')
  const [presetLoading, setPresetLoading] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [showPresets, setShowPresets] = useState(false)
  const [openPresetID, setOpenPresetID] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    void fetchApplicationPresets().then((items) => { if (active) setPresets(items) }).catch(() => { if (active) setError('应用规则目录读取失败，请稍后重试') })
    return () => { active = false }
  }, [])

  const ordinaryTargets = useMemo(() => targetLists.filter((target) => target.sourceType !== 'preset' && !target.pendingDeletion), [targetLists])
  const domainTargets = ordinaryTargets.filter((target) => target.kind !== 'ip')
  const ipTargets = ordinaryTargets.filter((target) => target.kind === 'ip')
  const categories = useMemo(() => Array.from(new Set(presets.map((preset) => preset.category).filter(Boolean) as string[])).sort(), [presets])
  const visiblePresets = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    return presets.filter((preset) => {
      if (category && preset.category !== category) return false
      if (!keyword) return true
      return [preset.name, preset.id, preset.category ?? '', ...preset.aliases].join(' ').toLowerCase().includes(keyword)
    })
  }, [category, presets, query])

  const toggle = (id: string) => onChange(selectedIDs.includes(id) ? selectedIDs.filter((value) => value !== id) : [...selectedIDs, id])

  const choiceForPreset = useCallback((presetID: string): PresetChoice => {
    const kinds = selectedKindsForPreset(selectedIDs, presetID)
    if (kinds.length === 2) return 'both'
    return kinds[0] ?? ''
  }, [selectedIDs])
  const selectedPresets = useMemo(() => presets.filter((preset) => choiceForPreset(preset.id)), [choiceForPreset, presets])
  const publishPresetPresentations = useCallback((nextSelectedIDs: string[], nextPreviews: Record<string, PresetPreview>) => {
    onPresetPresentationChange?.(presetPresentationsFor(presets, nextSelectedIDs, nextPreviews))
  }, [onPresetPresentationChange, presets])

  const loadPreview = async (preset: ApplicationPreset) => {
    const cached = previews[preset.id]
    if (cached) return cached
    const preview = await previewApplicationPreset(deviceID, preset.id)
    setPreviews((current) => ({ ...current, [preset.id]: preview }))
    return preview
  }

  const availableKinds = (preview: PresetPreview): PresetKind[] => [
    ...(preview.domain.validRules > 0 ? ['domain' as const] : []),
    ...(preview.ip.validRules > 0 ? ['ip' as const] : []),
  ]

  const changePresetKinds = async (preset: ApplicationPreset, requestedKinds: PresetKind[]) => {
    const presetPrefix = `preset:${preset.id}:`
    if (!requestedKinds.length) {
      const nextSelectedIDs = selectedIDs.filter((id) => !id.startsWith(presetPrefix))
      onChange(nextSelectedIDs)
      publishPresetPresentations(nextSelectedIDs, previews)
      setOpenPresetID(null)
      return
    }
    setPresetLoading(preset.id)
    setError(null)
    try {
      const preview = await loadPreview(preset)
      const kinds = requestedKinds.filter((kind) => availableKinds(preview).includes(kind))
      if (!kinds.length) throw new Error('所选应用规则没有可用的域名或 IP 规则')
      const nextSelectedIDs = [...selectedIDs.filter((id) => !id.startsWith(presetPrefix)), ...kinds.map((kind) => `preset:${preset.id}:${kind}`)]
      const nextPreviews = previews[preset.id] ? previews : { ...previews, [preset.id]: preview }
      onChange(nextSelectedIDs)
      publishPresetPresentations(nextSelectedIDs, nextPreviews)
    } catch (presetError) {
      setError(presetError instanceof Error ? presetError.message : '应用规则预览失败')
    } finally {
      setPresetLoading(null)
    }
  }

  const togglePresetKindMenu = async (preset: ApplicationPreset, menuOpen: boolean) => {
    if (menuOpen) {
      setOpenPresetID(null)
      return
    }
    setOpenPresetID(preset.id)
    if (previews[preset.id]) return
    setPresetLoading(preset.id)
    setError(null)
    try {
      await loadPreview(preset)
    } catch (presetError) {
      setOpenPresetID(null)
      setError(presetError instanceof Error ? presetError.message : '应用规则预览失败')
    } finally {
      setPresetLoading(null)
    }
  }

  const togglePresetBody = async (preset: ApplicationPreset) => {
    const current = choiceForPreset(preset.id)
    if (current) {
      await changePresetKinds(preset, [])
      return
    }
    setPresetLoading(preset.id)
    setError(null)
    try {
      const preview = await loadPreview(preset)
      const kinds = availableKinds(preview)
      if (!kinds.length) throw new Error('所选应用规则没有可用的域名或 IP 规则')
      await changePresetKinds(preset, [kinds[0]])
    } catch (presetError) {
      setError(presetError instanceof Error ? presetError.message : '应用规则预览失败')
      setPresetLoading(null)
    }
  }

  const togglePresetKind = async (preset: ApplicationPreset, kind: PresetKind) => {
    const current = selectedKindsForPreset(selectedIDs, preset.id)
    const next = current.includes(kind) ? current.filter((value) => value !== kind) : [...current, kind]
    await changePresetKinds(preset, next)
  }

  const renderTargetSection = (title: string, kind: PresetKind, targets: TargetList[]) => <section className="canonical-target-section"><div className="canonical-target-section-head"><h5>{title}</h5>{onCreateTargetList ? <button type="button" className="link-button" onClick={() => onCreateTargetList(kind)}>新增{kind === 'ip' ? ' IP' : '域名'}列表</button> : null}</div>{targets.map((target) => <label key={target.id} className={`access-multi-option${selectedIDs.includes(target.id) ? ' selected' : ''}`}><input type="checkbox" checked={selectedIDs.includes(target.id)} onChange={() => toggle(target.id)} /><span><strong>{target.name}</strong><small>{target.kind === 'ip' ? 'IP' : '域名'} · {target.counts.valid ?? 0} 条</small></span></label>)}{!targets.length ? <p className="policy-hint">暂无列表</p> : null}</section>

  return <div className="canonical-selector">
    <div className="canonical-target-list">{renderTargetSection('我的域名列表', 'domain', domainTargets)}{renderTargetSection('我的 IP 列表', 'ip', ipTargets)}</div>
    {selectedPresets.length ? <div className="canonical-selected-presets"><strong>已选择的应用</strong>{selectedPresets.map((preset) => <button key={preset.id} type="button" className="canonical-selected-preset" onClick={() => void changePresetKinds(preset, [])}>{preset.name} · {presetChoiceLabel(choiceForPreset(preset.id))} ×</button>)}</div> : null}
    <button type="button" className="toolbar-button" onClick={() => setShowPresets((value) => !value)}>{showPresets ? '收起应用目录' : '选择应用规则'}</button>
    {showPresets ? <div className="canonical-preset-picker">
      <div className="canonical-preset-toolbar"><select className="select-control" value={category} onChange={(event) => setCategory(event.target.value)}><option value="">全部分类</option>{categories.map((item) => <option key={item} value={item}>{item}</option>)}</select><input className="settings-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索名称、ID、别名或分类" /></div>
      <div className="canonical-preset-grid">
        {visiblePresets.map((preset) => {
          const preview = previews[preset.id]
          const selectedKinds = selectedKindsForPreset(selectedIDs, preset.id)
          const choice = choiceForPreset(preset.id)
          const menuOpen = openPresetID === preset.id
          return <div key={preset.id} className={`canonical-preset-card canonical-preset-card--selectable${choice ? ' selected' : ''}`}>
            <button type="button" className="canonical-preset-card-body" disabled={Boolean(presetLoading)} onClick={() => void togglePresetBody(preset)}><strong>{preset.name}</strong><small>{preset.category || '其他'} · {preset.id}</small><small>{choice ? '已选择，可在尾部调整类型' : '点击选择应用'}</small></button>
            <div className="canonical-preset-tail">
              <button type="button" className="canonical-preset-kind-trigger" aria-expanded={menuOpen} disabled={Boolean(presetLoading)} onClick={() => void togglePresetKindMenu(preset, menuOpen)}>{choice ? presetChoiceLabel(choice) : '选择类型'} <span aria-hidden="true">▾</span></button>
              {menuOpen ? <div className="canonical-preset-kind-menu" role="menu"><label><input type="checkbox" checked={selectedKinds.includes('domain')} disabled={Boolean(preview && preview.domain.validRules === 0) || Boolean(presetLoading)} onChange={() => void togglePresetKind(preset, 'domain')} />域名</label><label><input type="checkbox" checked={selectedKinds.includes('ip')} disabled={Boolean(preview && preview.ip.validRules === 0) || Boolean(presetLoading)} onChange={() => void togglePresetKind(preset, 'ip')} />IP</label></div> : null}
            </div>
            <small className="canonical-preset-warning">IP 范围可能包含共享基础设施并扩大匹配；不需要时只选择域名。</small>
            {presetLoading === preset.id ? <small className="policy-hint">正在预览规则…</small> : null}
          </div>
        })}
      </div>
      {!visiblePresets.length ? <p className="policy-hint">没有匹配的应用规则。</p> : null}
      {error ? <p className="policy-field-error">{error} <button type="button" className="link-button" onClick={() => setError(null)}>重试</button></p> : null}
    </div> : null}
    {selectedIDs.length ? <p className="policy-hint">已选择 {selectedIDs.length} 个目标；生成预览后才会把应用 backing row 纳入待审阅 proposal。</p> : null}
  </div>
}
