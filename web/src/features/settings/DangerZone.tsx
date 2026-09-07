import { useState } from 'react'
import { Button, Field, Input, Modal } from '../../ui'
import { errorMessage } from '../../lib/api'
import { fullReset, waitForPanelRestart } from './api'
import { clearLocalPanelState } from './prefs'

/**
 * 危险区 · 完全重新初始化: typed 「RESET」 confirmation → POST
 * /api/settings/full-reset → session cleared → bootstrap back to first-run.
 */
export function DangerZone() {
  const [open, setOpen] = useState(false)
  const [confirmation, setConfirmation] = useState('')
  const [resetting, setResetting] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const reset = async () => {
    if (confirmation.trim() !== 'RESET') return
    setResetting(true)
    setError(null)
    setMessage('正在完全重置…')
    try {
      await fullReset()
      clearLocalPanelState()
      setMessage('正在进入全新初始化页面…')
      // The backend restarts itself after wiping config + data; wait for it
      // and reload into the bootstrap gate (needs_admin).
      await waitForPanelRestart(() => setMessage('面板已断开，正在等待恢复…'))
    } catch (resetError) {
      setError(errorMessage(resetError, '完全重新初始化失败'))
      setMessage(null)
      setResetting(false)
    }
  }

  return (
    <div className="full-reset-zone">
      <div>
        <strong>完全重新初始化</strong>
        <p className="faint">删除管理员、所有会话、全部 RouterOS 配置和采集历史，并重新进入首次初始化页面。此操作与「重置界面偏好」不同，且无法撤销。</p>
      </div>
      <Button variant="danger" disabled={resetting} onClick={() => setOpen(true)}>
        完全重新初始化
      </Button>

      <Modal open={open} onClose={resetting ? () => undefined : () => setOpen(false)} title="完全重新初始化">
        <div className="purge-confirm">
          <p>
            此操作会删除<strong>管理员账号、全部设备配置和所有采集历史</strong>，无法撤销。输入「RESET」以确认。
          </p>
          <Field label="输入 RESET 以确认">
            <Input value={confirmation} onChange={setConfirmation} placeholder="RESET" autoFocus autoComplete="off" />
          </Field>
          {message ? (
            <p className="form-message" role="status">
              {message}
            </p>
          ) : null}
          {error ? (
            <p className="form-error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="verify-actions">
            <Button disabled={resetting} onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button variant="danger" disabled={confirmation.trim() !== 'RESET' || resetting} loading={resetting} onClick={() => void reset()}>
              {resetting ? '正在完全重置…' : '完全重新初始化'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
