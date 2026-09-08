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
  /** Empty custom name restores automatic naming. */
  currentCustomName: string
  /** shown as the input placeholder */
  placeholder: string
  /** extra hint line, e.g. the auto-detected name behind a custom one */
  hint?: string
  onSaved: () => void
}

/** Hover pencil → small bubble popover that edits the local terminal display name. */
export function QuickEdit({ scopedPath, terminalId, currentCustomName, placeholder, hint, onSaved }: QuickEditProps) {
  const [value, setValue] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  return (
    <Popover
      portal
      align="left"
      width={280}
      ariaLabel="编辑名称"
      closeOnContentClick={false}
      trigger={(_open, toggle) => (
        <button
          type="button"
          className="icon-btn quick-edit-trigger"
          aria-label="编辑名称"
          title="编辑名称"
          onClick={(event) => {
            event.stopPropagation()
            setValue(currentCustomName)
            setSaving(false)
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
              customName: value.trim(),
            })
            toast(value.trim() ? '名称已更新' : '已恢复自动名称')
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
            <Input value={value} onChange={setValue} placeholder={placeholder} maxLength={100} autoFocus disabled={saving} ariaLabel="编辑名称" />
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
