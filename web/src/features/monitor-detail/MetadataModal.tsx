import { useEffect, useState } from 'react'
import { errorMessage } from '../../lib/api'
import type { Terminal } from '../../lib/types'
import { Button, Field, Input, Modal, Textarea, toast } from '../../ui'
import { saveTerminalMetadata, type ScopedPath, type TerminalDetail } from './api'

type MetadataModalProps = {
  open: boolean
  terminal: Terminal | null
  scopedPath: ScopedPath
  onClose: () => void
  onSaved: (detail: TerminalDetail) => void
}

const CUSTOM_NAME_MAX = 100
const REMARK_MAX = 500

/** Edit 设备名称 / 备注 — stored panel-locally, never written back to RouterOS. */
export function MetadataModal({ open, terminal, scopedPath, onClose, onSaved }: MetadataModalProps) {
  const [customName, setCustomName] = useState('')
  const [remark, setRemark] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open || !terminal) return
    setCustomName(terminal.customName)
    setRemark(terminal.remark)
  }, [open, terminal])

  const save = async () => {
    if (!terminal || saving) return
    setSaving(true)
    try {
      const detail = await saveTerminalMetadata(scopedPath, terminal.id, { customName: customName.trim(), remark: remark.trim() })
      toast('终端信息已保存')
      onSaved(detail)
      onClose()
    } catch (error) {
      toast(errorMessage(error, '保存失败，请稍后重试'), { tone: 'err' })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="编辑终端"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            取消
          </Button>
          <Button variant="primary" onClick={() => void save()} loading={saving}>
            保存
          </Button>
        </>
      }
    >
      <div className="metadata-form">
        <p className="faint metadata-note">设备名称和备注只保存到面板本地，不写回 RouterOS。</p>
        <Field label="设备名称" hint={`自动识别：${terminal?.autoName || '暂未识别'}；清空后恢复自动名称。`}>
          <Input
            value={customName}
            onChange={setCustomName}
            maxLength={CUSTOM_NAME_MAX}
            placeholder={terminal?.autoName || terminal?.displayName || '设备名称'}
            ariaLabel="设备名称"
          />
        </Field>
        <Field label="备注" hint={`已输入 ${remark.length} / ${REMARK_MAX} 字。`}>
          <Textarea value={remark} onChange={(value) => setRemark(value.slice(0, REMARK_MAX))} rows={4} placeholder="例如：书房台式机，只允许走电信线路" ariaLabel="备注" />
        </Field>
      </div>
    </Modal>
  )
}
