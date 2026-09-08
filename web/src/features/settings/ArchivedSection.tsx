import { useState } from 'react'
import { Button, Field, Input, Modal } from '../../ui'
import { errorMessage } from '../../lib/api'
import { fetchCleanupScript, purgeDeviceData, restoreDevice, type RouterOSCleanup, type SettingsDevice } from './api'
import type { RestartingActionState } from './hooks'
import { CleanupCard } from './CleanupCard'

type ArchivedSectionProps = {
  devices: SettingsDevice[]
  restartGate: RestartingActionState
  onChanged: () => void
}

/**
 * 危险区 · 已归档设备: restore, view/download the RouterOS cleanup script,
 * and purge history data (typed device-name confirmation).
 */
export function ArchivedSection({ devices, restartGate, onChanged }: ArchivedSectionProps) {
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState('')
  const [cleanup, setCleanup] = useState<RouterOSCleanup | null>(null)
  const [cleanupLoadingId, setCleanupLoadingId] = useState('')
  const [purgeTarget, setPurgeTarget] = useState<SettingsDevice | null>(null)
  const [purgeInput, setPurgeInput] = useState('')
  const [purging, setPurging] = useState(false)

  if (!devices.length) return null

  const restore = async (device: SettingsDevice) => {
    setError(null)
    setBusyId(device.id)
    try {
      await restartGate.run(() => restoreDevice(device.id), onChanged)
      setBusyId('')
    } catch (restoreError) {
      setError(errorMessage(restoreError, '恢复设备失败'))
      setBusyId('')
    }
  }

  const loadCleanup = async (device: SettingsDevice) => {
    setError(null)
    setCleanupLoadingId(device.id)
    try {
      const result = await fetchCleanupScript(device.id)
      if (result) setCleanup(result)
      else setError('该设备没有可用的清理脚本。')
    } catch (loadError) {
      setError(errorMessage(loadError, '读取 RouterOS 清理脚本失败'))
    } finally {
      setCleanupLoadingId('')
    }
  }

  const purge = async () => {
    if (!purgeTarget || purgeInput.trim() !== purgeTarget.name) return
    setPurging(true)
    setError(null)
    try {
      await restartGate.run(() => purgeDeviceData(purgeTarget.id, purgeInput.trim()), onChanged)
      setPurgeTarget(null)
      setPurgeInput('')
      setPurging(false)
    } catch (purgeError) {
      setError(errorMessage(purgeError, '清除设备数据失败'))
      setPurging(false)
    }
  }

  return (
    <div className="archived-section">
      <h4>已归档设备</h4>
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
      <div className="archived-rows">
        {devices.map((device) => (
          <div className="archived-row" key={device.id}>
            <div className="archived-row-main">
              <strong>{device.name}</strong>
              <small className="faint mono">
                {device.host}:{device.port}
              </small>
            </div>
            <div className="archived-row-actions">
              {device.cleanupAvailable ? (
                <Button size="sm" disabled={restartGate.waiting || cleanupLoadingId === device.id} loading={cleanupLoadingId === device.id} onClick={() => void loadCleanup(device)}>
                  清理脚本
                </Button>
              ) : null}
              <Button size="sm" disabled={restartGate.waiting || busyId === device.id} loading={busyId === device.id} onClick={() => void restore(device)}>
                恢复
              </Button>
              <Button
                size="sm"
                variant="danger"
                disabled={restartGate.waiting}
                onClick={() => {
                  setPurgeTarget(device)
                  setPurgeInput('')
                  setError(null)
                }}
              >
                永久清除
              </Button>
            </div>
          </div>
        ))}
      </div>

      <Modal open={cleanup !== null} onClose={() => setCleanup(null)} title="RouterOS 清理脚本" maxWidth={640}>
        {cleanup ? <CleanupCard cleanup={cleanup} onClose={() => setCleanup(null)} /> : null}
      </Modal>

      <Modal open={purgeTarget !== null} persistent onClose={purging ? () => undefined : () => setPurgeTarget(null)} title="永久清除设备数据">
        {purgeTarget ? (
          <div className="purge-confirm">
            <p>
              将删除「<strong>{purgeTarget.name}</strong>」的全部采集历史并从设备列表移除，此操作无法撤销。
            </p>
            <Field label={`输入设备名称「${purgeTarget.name}」以确认`}>
              <Input value={purgeInput} onChange={setPurgeInput} placeholder={purgeTarget.name} autoFocus />
            </Field>
            <div className="verify-actions">
              <Button disabled={purging} onClick={() => setPurgeTarget(null)}>
                取消
              </Button>
              <Button variant="danger" disabled={purgeInput.trim() !== purgeTarget.name || restartGate.waiting} loading={purging} onClick={() => void purge()}>
                {purging ? '正在清除…' : '永久清除数据'}
              </Button>
            </div>
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
