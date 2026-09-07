import { useState } from 'react'
import { Button, Field, Input, toast } from '../../ui'
import { errorMessage } from '../../lib/api'
import { saveCollectionSettings, type CollectionSettings } from './api'
import type { RestartingActionState } from './hooks'

type CollectionFormProps = {
  settings: CollectionSettings
  restartGate: RestartingActionState
}

const FIELDS: Array<{ key: keyof CollectionSettings; label: string; unit: string; hint: string }> = [
  { key: 'pollIntervalSeconds', label: '完整采集间隔', unit: '秒', hint: '接口、路由、DHCP 等完整快照的采集周期' },
  { key: 'realtimePollIntervalSeconds', label: '实时采集间隔', unit: '秒', hint: '首页实时流量曲线的采样周期' },
  { key: 'terminalPollIntervalSeconds', label: '终端采集间隔', unit: '秒', hint: '终端在线状态与连接的采集周期' },
  { key: 'sampleRetentionHours', label: '采样保留时间', unit: '小时', hint: '历史负载与流量采样的保留时长' },
]

/** 采集设置: four numeric fields → POST /api/settings/collection → restart wait. */
export function CollectionForm({ settings, restartGate }: CollectionFormProps) {
  const [draft, setDraft] = useState<CollectionSettings>({ ...settings })
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const setField = (key: keyof CollectionSettings, value: string) => {
    setDraft((current) => ({ ...current, [key]: Number(value) || 0 }))
    setError(null)
  }

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      await restartGate.run(() => saveCollectionSettings(draft), () => toast('采集设置已保存'))
      // Restarting path reloads the page.
    } catch (saveError) {
      setError(errorMessage(saveError, '采集设置保存失败'))
      setSaving(false)
    }
  }

  return (
    <form
      className="collection-form"
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
    >
      <div className="form-grid form-grid-four">
        {FIELDS.map((field) => (
          <Field key={field.key} label={field.label} hint={field.hint}>
            <span className="number-field">
              <Input
                type="number"
                min={1}
                required
                value={String(draft[field.key])}
                onChange={(value) => setField(field.key, value)}
                ariaLabel={field.label}
              />
              <small className="number-field-unit">{field.unit}</small>
            </span>
          </Field>
        ))}
      </div>
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
      <div className="form-actions">
        <Button type="submit" variant="primary" disabled={saving || restartGate.waiting} loading={saving}>
          {saving ? '保存中…' : '保存并重启采集'}
        </Button>
      </div>
    </form>
  )
}
