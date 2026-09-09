import { useState } from 'react'
import { UpdatePanel } from '../update/UpdatePanel'
import { Button, Card, Modal, toast } from '../../ui'
import { errorMessage } from '../../lib/api'
import { restartPanel, type SettingsResponse } from './api'
import type { RestartingActionState } from './hooks'
import { resetPanelPreferences } from './prefs'
import { ArchivedSection } from './ArchivedSection'
import { DangerZone } from './DangerZone'

type MaintenanceSectionProps = {
  settings: SettingsResponse
  restartGate: RestartingActionState
  onChanged: () => void
}

/**
 * Deep-clone the settings response and mask every password-like field.
 * The current backend response carries no RouterOS password at all; the mask
 * is defence-in-depth so the export can never leak one in the future
 * (component-guidelines: never download the raw response object).
 */
function sanitizeSettings(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sanitizeSettings)
  if (typeof value !== 'object' || value === null) return value
  const output: Record<string, unknown> = {}
  for (const [key, entry] of Object.entries(value as Record<string, unknown>)) {
    if (/password/i.test(key) && !/passwordset/i.test(key)) {
      output[key] = '********'
    } else {
      output[key] = sanitizeSettings(entry)
    }
  }
  return output
}

/** 维护设置: sanitized export, UI-preference reset, panel restart, 危险区. */
export function MaintenanceSection({ settings, restartGate, onChanged }: MaintenanceSectionProps) {
  const [restartOpen, setRestartOpen] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const exportSettings = () => {
    const payload = JSON.stringify(sanitizeSettings(settings), null, 2)
    const url = URL.createObjectURL(new Blob([payload], { type: 'application/json' }))
    const link = document.createElement('a')
    link.href = url
    link.download = 'rosboard-settings.json'
    link.click()
    URL.revokeObjectURL(url)
    toast('已导出脱敏设置')
  }

  const confirmRestart = async () => {
    setRestarting(true)
    setError(null)
    try {
      await restartGate.run(() => restartPanel())
      // Restarting path reloads the page.
    } catch (restartError) {
      setError(errorMessage(restartError, '重启面板失败'))
      setRestarting(false)
    }
  }

  const archived = settings.devices.filter((device) => device.archived)

  return (
    <div className="maintenance-section">
 <Card title="版本与更新"><UpdatePanel buttonClass="btn btn-ghost" primaryClass="btn btn-primary" disabled={restartGate.waiting} /></Card>
      <Card title="维护操作" sub="导出、偏好与面板服务">
        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="maintenance-actions">
          <Button disabled={restartGate.waiting} onClick={exportSettings}>
            导出脱敏配置
          </Button>
          <Button
            disabled={restartGate.waiting}
            onClick={() => {
              resetPanelPreferences()
              toast('界面偏好已重置')
            }}
          >
            重置界面偏好
          </Button>
          <Button disabled={restartGate.waiting} onClick={() => setRestartOpen(true)}>
            重启面板服务
          </Button>
        </div>
      </Card>

      <Card title="危险区" sub="以下操作不可撤销，请确认后再执行" className="danger-zone-card">
        <ArchivedSection devices={archived} restartGate={restartGate} onChanged={onChanged} />
        <DangerZone />
      </Card>

      <Modal open={restartOpen} onClose={restarting ? () => undefined : () => setRestartOpen(false)} title="重启面板服务">
        <p>重启期间面板会有数秒不可用，采集在恢复后自动继续。确定现在重启吗？</p>
        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="verify-actions">
          <Button disabled={restarting} onClick={() => setRestartOpen(false)}>
            取消
          </Button>
          <Button variant="primary" loading={restarting} onClick={() => void confirmRestart()}>
            {restarting ? '正在重启…' : '确认重启'}
          </Button>
        </div>
      </Modal>
    </div>
  )
}
