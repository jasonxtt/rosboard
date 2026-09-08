import { useState } from 'react'
import { Button } from '../../../ui/Button'
import { Field, Input } from '../../../ui/inputs'
import { Modal } from '../../../ui/Modal'
import { Toggle } from '../../../ui/Toggle'
import type { AccessRule, AccessRuleDraft, PolicyTerminal, Subject, TargetList } from '../canonical'
import { errorMessage } from '../../../lib/api'
import { Notice } from './Notice'
import { SubjectSelector } from './SubjectSelector'
import { TargetListModal } from './TargetListModal'
import { TargetSelector } from './TargetSelector'
import type { PresetPresentation } from './TargetSelector'

type AccessRuleModalProps = {
  deviceID: string
  /** null = 新建 */
  rule: AccessRule | null
  terminals: PolicyTerminal[]
  targetLists: TargetList[]
  saving: boolean
  error?: string | null
  onClose: () => void
  onSave: (rule: AccessRuleDraft) => void | Promise<void>
}

/** 访问控制规则 editor: 名称 + 来源 + 目标范围（整个互联网 / 目标库）. */
export function AccessRuleModal({ deviceID, rule, terminals, targetLists, saving, error, onClose, onSave }: AccessRuleModalProps) {
  const [availableTargetLists, setAvailableTargetLists] = useState<TargetList[]>(() => [...targetLists])
  const [name, setName] = useState(rule?.name ?? '')
  const [subject, setSubject] = useState<Subject>(rule?.subject ?? { mode: 'selected', members: [], prefixes: [] })
  const [targetScope, setTargetScope] = useState<'internet' | 'targets'>(rule?.targetScope ?? 'internet')
  const [targetListIds, setTargetListIds] = useState<string[]>(rule?.targetListIds ?? [])
  const [enabled, setEnabled] = useState(rule?.enabled ?? true)
  const [creatingTargetKind, setCreatingTargetKind] = useState<'domain' | 'ip' | null>(null)
  const [presetPresentations, setPresetPresentations] = useState<PresetPresentation[]>([])
  const [localError, setLocalError] = useState<string | null>(null)

  const subjectValid = subject.mode === 'all' || subject.members.length > 0 || subject.prefixes.length > 0
  const targetsValid = targetScope === 'internet' || targetListIds.length > 0
  const canSave = Boolean(name.trim()) && subjectValid && targetsValid && !saving

  const submit = async () => {
    if (!canSave) return
    setLocalError(null)
    try {
      const selections = presetPresentations.filter((selection) => selection.previewId && selection.requestedKinds.length).map(({ name: _name, ...selection }) => selection)
      await onSave({
        id: rule?.id ?? '',
        name: name.trim(),
        subject,
        targetScope,
        targetListIds: targetScope === 'targets' ? targetListIds : [],
        enabled,
        revision: rule?.revision ?? 0,
        ...(targetScope === 'targets' && selections.length ? { presetSelections: selections } : {}),
      })
    } catch (saveError) {
      setLocalError(errorMessage(saveError, '访问规则保存失败'))
    }
  }

  return (
    <>
      <Modal
        open
        persistent
        onClose={() => {
          if (!saving) onClose()
        }}
        title={rule ? `编辑访问规则：${rule.name}` : '新建访问规则'}
        maxWidth={720}
        footer={
          <>
            <Button disabled={saving} onClick={onClose}>
              取消
            </Button>
            <Button variant="primary" loading={saving} disabled={!canSave} onClick={() => void submit()}>
              保存并应用
            </Button>
          </>
        }
      >
        {error ? <Notice tone="err">{error}</Notice> : null}
        {localError ? <Notice tone="err">{localError}</Notice> : null}
        <div className="pol-form-grid">
          <Field label="规则名称">
            <Input value={name} onChange={setName} placeholder="例如：儿童平板断网" disabled={saving} autoFocus />
          </Field>
          <Field label="启用状态">
            <div className="pol-toggle-row">
              <Toggle checked={enabled} disabled={saving} onChange={setEnabled} label="启用此规则" />
              <span className="muted">{enabled ? '保存后立即启用' : '保存后保持停用'}</span>
            </div>
          </Field>
        </div>
        <Field label="来源 · 受控对象">
          <SubjectSelector terminals={terminals} value={subject} onChange={setSubject} />
        </Field>
        {!subjectValid ? <Notice tone="warn">请选择至少一台终端或填写一个手动地址。</Notice> : null}
        <Field label="目标 · 阻断范围">
          <div className="pol-choice-list" role="radiogroup" aria-label="阻断范围">
            <label className={`pol-choice${targetScope === 'internet' ? ' pol-choice-active' : ''}`}>
              <input type="radio" checked={targetScope === 'internet'} disabled={saving} onChange={() => setTargetScope('internet')} />
              <span>
                <strong>整个互联网</strong>
                <small>阻断互联网访问，局域网通信不受影响</small>
              </span>
            </label>
            <label className={`pol-choice${targetScope === 'targets' ? ' pol-choice-active' : ''}`}>
              <input type="radio" checked={targetScope === 'targets'} disabled={saving} onChange={() => setTargetScope('targets')} />
              <span>
                <strong>指定目标库</strong>
                <small>只阻断目标库中的域名或 IP</small>
              </span>
            </label>
          </div>
        </Field>
        {targetScope === 'targets' ? (
          <>
            <TargetSelector
              deviceID={deviceID}
              targetLists={availableTargetLists}
              selectedIDs={targetListIds}
              onChange={setTargetListIds}
              onPresetPresentationChange={setPresetPresentations}
              onCreateTargetList={setCreatingTargetKind}
            />
            {!targetsValid ? <Notice tone="warn">请选择至少一个目标库或应用预设。</Notice> : null}
          </>
        ) : null}
      </Modal>
      {creatingTargetKind ? (
        <TargetListModal
          deviceID={deviceID}
          target={null}
          initialKind={creatingTargetKind}
          onClose={() => setCreatingTargetKind(null)}
          onSaved={({ targetList }) => {
            setAvailableTargetLists((current) => (current.some((item) => item.id === targetList.id) ? current.map((item) => (item.id === targetList.id ? targetList : item)) : [...current, targetList]))
            setTargetListIds((current) => (current.includes(targetList.id) ? current : [...current, targetList.id]))
            setCreatingTargetKind(null)
          }}
        />
      ) : null}
    </>
  )
}
