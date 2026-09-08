import { useState } from 'react'
import { toast } from '../../ui/toastStore'
import { Button } from '../../ui/Button'
import { Input } from '../../ui/inputs'
import { Popover } from '../../ui/Popover'
import { errorMessage } from '../../lib/api'
import { saveTerminalMetadata } from './api'
import type { ScopedPath } from './api'

type QuickEditProps = {
  scopedPath: ScopedPath
  terminalId: string
  field: 'customName' | 'remark'
  /** current stored values — the untouched field must be re-sent verbatim */
  currentCustomName: string
  currentRemark: string
  /** shown as the input placeholder */
  placeholder: string
  /** extra hint line, e.g. the auto-detected name behind a custom one */
  hint?: string
  onSaved: () => void
}

const FIELD_META = {
  customName: { label: '编辑名称', maxLength: 100 },
  remark: { label: '编辑备注', maxLength: 500 },
} as const

/** Hover pencil → small bubble popover that edits one metadata field inline. */
export function QuickEdit({ scopedPath, terminalId, field, currentCustomName, currentRemark, placeholder, hint, onSaved }: QuickEditProps) {
  const meta = FIELD_META[field]
  const [value, setValue] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  return (
    <Popover
      portal
      align="left"
      width={280}
      ariaLabel={meta.label}
      closeOnContentClick={false}
      trigger={(_open, toggle) => (
        <button
          type="button"
          className="icon-btn quick-edit-trigger"
          aria-label={meta.label}
          title={meta.label}
          onClick={(event) => {
            event.stopPropagation()
            setValue(field === 'customName' ? currentCustomName : currentRemark)
            setError(null)
            toggle()
          }}
        >
          ✏️
        </button>
      )}
    >
      {(close) => {
        const save = async () => {
          if (saving) return
          setSaving(true)
          setError(null)
          try {
            await saveTerminalMetadata(scopedPath, terminalId, {
              customName: field === 'customName' ? value.trim() : currentCustomName,
              remark: field === 'remark' ? value.trim() : currentRemark,
            })
            toast(field === 'customName' ? '名称已更新' : '备注已更新')
            onSaved()
            close()
          } catch (saveError) {
            setError(errorMessage(saveError, '保存失败'))
            setSaving(false)
          }
        }
        return (
          <form
            className="quick-edit"
            onClick={(event) => event.stopPropagation()}
            onSubmit={(event) => {
              event.preventDefault()
              void save()
            }}
          >
            <Input value={value} onChange={setValue} placeholder={placeholder} maxLength={meta.maxLength} autoFocus disabled={saving} ariaLabel={meta.label} />
            {hint ? <small className="faint">{hint}</small> : null}
            {error ? <small className="form-error">{error}</small> : null}
            <div className="quick-edit-actions">
              <Button size="sm" disabled={saving} onClick={close}>
                取消
              </Button>
              <Button size="sm" variant="primary" loading={saving} type="submit">
                确认
              </Button>
            </div>
          </form>
        )
      }}
    </Popover>
  )
}
