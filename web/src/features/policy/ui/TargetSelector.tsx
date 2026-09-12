import { useCallback, useMemo, useState } from 'react'
import { Badge } from '../../../ui/Badge'
import { Button } from '../../../ui/Button'
import { SearchInput } from '../../../ui/SearchInput'
import { Select } from '../../../ui/inputs'
import { useApplicationPresets } from '../applicationPresets'
import {
  previewApplicationPreset,
  type ApplicationPreset,
  type ApplicationPresetSelection,
  type PresetPreview,
  type TargetList,
} from '../canonical'
import { formatCount } from '../../../lib/format'
import { Notice } from './Notice'

type PresetKind = 'domain' | 'ip'
type PresetChoice = '' | 'domain' | 'ip' | 'both'

export type PresetPresentation = ApplicationPresetSelection & { name: string }

function presetChoiceLabel(choice: PresetChoice) {
  if (choice === 'both') return '域名/IP'
  return choice === 'ip' ? 'IP' : '域名'
}

function selectedKindsForPreset(selectedIDs: string[], presetID: string): PresetKind[] {
  return (['domain', 'ip'] as const).filter((kind) => selectedIDs.includes(`preset:${presetID}:${kind}`))
}

function presetPresentationsFor(presets: ApplicationPreset[], selectedIDs: string[], previews: Record<string, PresetPreview>): PresetPresentation[] {
  return presets
    .filter((preset) => selectedKindsForPreset(selectedIDs, preset.id).length > 0)
    .map((preset) => ({
      presetId: preset.id,
      name: preset.name,
      previewId: previews[preset.id]?.previewId ?? '',
      requestedKinds: selectedKindsForPreset(selectedIDs, preset.id),
    }))
}

type TargetSelectorProps = {
  deviceID: string
  targetLists: TargetList[]
  selectedIDs: string[]
  onChange: (ids: string[]) => void
  onPresetPresentationChange?: (value: PresetPresentation[]) => void
  onCreateTargetList?: (kind: PresetKind) => void
}

/**
 * 访问目标 picker: existing target lists multi-pick + application preset
 * catalog (category browse, search, preview, domain/ip kind pick) + inline create.
 */
export function TargetSelector({ deviceID, targetLists, selectedIDs, onChange, onPresetPresentationChange, onCreateTargetList }: TargetSelectorProps) {
  const { presets, loading: presetsLoading, error: catalogError, reload: reloadPresets } = useApplicationPresets()
  const [previews, setPreviews] = useState<Record<string, PresetPreview>>({})
  const [category, setCategory] = useState('')
  const [query, setQuery] = useState('')
  const [presetLoading, setPresetLoading] = useState<string | null>(null)
  const [presetError, setPresetError] = useState<string | null>(null)
  const [showPresets, setShowPresets] = useState(false)
  const [openPresetID, setOpenPresetID] = useState<string | null>(null)

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

  const choiceForPreset = useCallback(
    (presetID: string): PresetChoice => {
      const kinds = selectedKindsForPreset(selectedIDs, presetID)
      if (kinds.length === 2) return 'both'
      return kinds[0] ?? ''
    },
    [selectedIDs],
  )
  const selectedPresets = useMemo(() => presets.filter((preset) => choiceForPreset(preset.id)), [choiceForPreset, presets])
  const publishPresetPresentations = useCallback(
    (nextSelectedIDs: string[], nextPreviews: Record<string, PresetPreview>) => {
      onPresetPresentationChange?.(presetPresentationsFor(presets, nextSelectedIDs, nextPreviews))
    },
    [onPresetPresentationChange, presets],
  )

  const loadPreview = useCallback(
    async (preset: ApplicationPreset) => {
      const cached = previews[preset.id]
      if (cached) return cached
      const preview = await previewApplicationPreset(deviceID, preset.id)
      setPreviews((current) => ({ ...current, [preset.id]: preview }))
      return preview
    },
    [deviceID, previews],
  )

  const availableKinds = (preview: PresetPreview): PresetKind[] => [
    ...(preview.domain.validRules > 0 ? (['domain'] as const) : []),
    ...(preview.ip.validRules > 0 ? (['ip'] as const) : []),
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
    setPresetError(null)
    try {
      const preview = await loadPreview(preset)
      const kinds = requestedKinds.filter((kind) => availableKinds(preview).includes(kind))
      if (!kinds.length) throw new Error('所选应用预设没有可用的域名或 IP 规则')
      const nextSelectedIDs = [...selectedIDs.filter((id) => !id.startsWith(presetPrefix)), ...kinds.map((kind) => `preset:${preset.id}:${kind}`)]
      const nextPreviews = previews[preset.id] ? previews : { ...previews, [preset.id]: preview }
      onChange(nextSelectedIDs)
      publishPresetPresentations(nextSelectedIDs, nextPreviews)
    } catch (presetError) {
      setPresetError(presetError instanceof Error ? presetError.message : '应用预设预览失败')
    } finally {
      setPresetLoading(null)
    }
  }

  const togglePresetBody = async (preset: ApplicationPreset) => {
    if (choiceForPreset(preset.id)) {
      await changePresetKinds(preset, [])
      return
    }
    setPresetLoading(preset.id)
    setPresetError(null)
    try {
      const preview = await loadPreview(preset)
      const kinds = availableKinds(preview)
      if (!kinds.length) throw new Error('所选应用预设没有可用的域名或 IP 规则')
      await changePresetKinds(preset, [kinds[0]])
    } catch (presetError) {
      setPresetError(presetError instanceof Error ? presetError.message : '应用预设预览失败')
      setPresetLoading(null)
    }
  }

  const togglePresetKind = async (preset: ApplicationPreset, kind: PresetKind) => {
    const current = selectedKindsForPreset(selectedIDs, preset.id)
    const next = current.includes(kind) ? current.filter((value) => value !== kind) : [...current, kind]
    await changePresetKinds(preset, next)
  }

  const renderTargetSection = (title: string, kind: PresetKind, targets: TargetList[]) => (
    <section className="pol-target-section">
      <div className="pol-target-section-head">
        <h5>{title}</h5>
        {onCreateTargetList ? (
          <button type="button" className="link-button" onClick={() => onCreateTargetList(kind)}>
            ＋ 新建{kind === 'ip' ? ' IP ' : '域名'}目标库
          </button>
        ) : null}
      </div>
      <div className="pol-choice-list">
        {targets.map((target) => (
          <label key={target.id} className={`pol-choice${selectedIDs.includes(target.id) ? ' pol-choice-active' : ''}`}>
            <input type="checkbox" checked={selectedIDs.includes(target.id)} onChange={() => toggle(target.id)} />
            <span>
              <strong>{target.name}</strong>
              <small>
                {target.kind === 'ip' ? 'IP' : '域名'} · {formatCount(target.counts.valid ?? 0)} 条
                {target.kind !== 'ip' && (target.counts['DOMAIN-KEYWORD'] ?? 0) > 0 ? ` · 关键字 ${formatCount(target.counts['DOMAIN-KEYWORD'])} 条` : ''}
              </small>
            </span>
          </label>
        ))}
        {!targets.length ? <p className="pol-hint">暂无目标库</p> : null}
      </div>
    </section>
  )

  return (
    <div className="pol-selector">
      <div className="pol-target-list">
        {renderTargetSection('我的域名目标库', 'domain', domainTargets)}
        {renderTargetSection('我的 IP 目标库', 'ip', ipTargets)}
      </div>
      {selectedPresets.length ? (
        <div className="pol-selected-presets">
          <span className="field-label">已选择的应用预设</span>
          <div className="pol-preset-chips">
            {selectedPresets.map((preset) => (
              <button key={preset.id} type="button" className="pol-preset-chip" onClick={() => void changePresetKinds(preset, [])} title="点击移除">
                {preset.name} · {presetChoiceLabel(choiceForPreset(preset.id))} ✕
              </button>
            ))}
          </div>
        </div>
      ) : null}
      <Button onClick={() => setShowPresets((value) => !value)}>{showPresets ? '收起应用预设目录' : '选择应用预设'}</Button>
      {showPresets ? (
        <div className="pol-preset-picker">
          <div className="pol-preset-toolbar">
            <Select
              value={category}
              onChange={setCategory}
              options={[{ value: '', label: '全部分类' }, ...categories.map((item) => ({ value: item, label: item }))]}
              ariaLabel="预设分类"
            />
            <SearchInput value={query} onChange={setQuery} placeholder="搜索名称、ID、别名或分类" ariaLabel="搜索应用预设" />
          </div>
          {presetsLoading && !presets.length ? <Notice>正在读取应用预设目录…</Notice> : null}
          {catalogError ? <Notice tone="err" action={<button type="button" className="link-button" onClick={reloadPresets} disabled={presetsLoading}>{presetsLoading ? '重试中…' : '重试'}</button>}>{catalogError}</Notice> : null}
          {presetError ? <Notice tone="err">{presetError}</Notice> : null}
          <div className="pol-preset-grid">
            {visiblePresets.map((preset) => {
              const preview = previews[preset.id]
              const selectedKinds = selectedKindsForPreset(selectedIDs, preset.id)
              const choice = choiceForPreset(preset.id)
              const menuOpen = openPresetID === preset.id
              return (
                <div key={preset.id} className={`pol-preset-card${choice ? ' pol-preset-selected' : ''}`}>
                  <button type="button" className="pol-preset-body" disabled={Boolean(presetLoading)} onClick={() => void togglePresetBody(preset)}>
                    <strong>{preset.name}</strong>
                    <small>
                      {preset.category || '其他'} · {preset.id}
                    </small>
                    <small>{choice ? '已选择，可调整规则类型' : '点击选择此应用'}</small>
                  </button>
                  <div className="pol-preset-tail">
                    <button
                      type="button"
                      className="pol-preset-kind-trigger"
                      aria-expanded={menuOpen}
                      disabled={Boolean(presetLoading)}
                      onClick={() => {
                        const opening = openPresetID !== preset.id
                        setOpenPresetID(opening ? preset.id : null)
                        if (opening && !previews[preset.id]) {
                          setPresetLoading(preset.id)
                          setPresetError(null)
                          loadPreview(preset)
                            .catch((previewError) => {
                              setOpenPresetID(null)
                              setPresetError(previewError instanceof Error ? previewError.message : '应用预设预览失败')
                            })
                            .finally(() => setPresetLoading(null))
                        }
                      }}
                    >
                      {presetLoading === preset.id ? '读取中…' : choice ? presetChoiceLabel(choice) : '选择类型'} <span aria-hidden="true">▾</span>
                    </button>
                    {menuOpen ? (
                      <div className="pol-preset-kind-menu" role="menu">
                        <label>
                          <input
                            type="checkbox"
                            checked={selectedKinds.includes('domain')}
                            disabled={Boolean(preview && preview.domain.validRules === 0) || Boolean(presetLoading)}
                            onChange={() => void togglePresetKind(preset, 'domain')}
                          />
                          域名{preview ? `（${formatCount(preview.domain.validRules)} 条）` : ''}
                        </label>
                        <label>
                          <input
                            type="checkbox"
                            checked={selectedKinds.includes('ip')}
                            disabled={Boolean(preview && preview.ip.validRules === 0) || Boolean(presetLoading)}
                            onChange={() => void togglePresetKind(preset, 'ip')}
                          />
                          IP{preview ? `（${formatCount(preview.ip.validRules)} 条）` : ''}
                        </label>
                      </div>
                    ) : null}
                  </div>
                  {preview ? (
                    <div className="pol-preset-preview-line">
                      <Badge tone="neutral">域名 {formatCount(preview.domain.validRules)}</Badge>
                      <Badge tone="neutral">IP {formatCount(preview.ip.validRules)}</Badge>
                      {preview.existingTargetListIds.length ? <Badge tone="ok">已建好，将复用</Badge> : null}
                    </div>
                  ) : null}
                </div>
              )
            })}
            {!catalogError && !presetsLoading && !visiblePresets.length ? <p className="pol-hint">没有匹配的应用预设。</p> : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}
