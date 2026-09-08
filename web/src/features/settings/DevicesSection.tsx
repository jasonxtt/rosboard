import { useState } from 'react'
import { Button, Card, EmptyState, Modal, toast } from '../../ui'
import { errorMessage } from '../../lib/api'
import { useShell } from '../../shell/useShell'
import { archiveDevice, reorderDevices, updateDevice, type MutationResult, type RouterOSCleanup, type SettingsDevice, type SettingsResponse } from './api'
import type { RestartingActionState } from './hooks'
import { consumePendingCleanup, storePendingCleanup } from './prefs'
import { CleanupCard } from './CleanupCard'
import { DeviceList } from './DeviceList'
import { ManualAddWizard } from './ManualAddWizard'
import { QuickOnboardingWizard } from './QuickOnboardingWizard'

type DevicesSectionProps = {
  settings: SettingsResponse
  restartGate: RestartingActionState
  onChanged: () => void
}

type EditorMode = { kind: 'list' } | { kind: 'choice' } | { kind: 'quick' } | { kind: 'manual' } | { kind: 'edit'; device: SettingsDevice }

/** Restart progress banner (§9.4: 已保存，等待面板重启…). */
export function RestartingBanner({ gate }: { gate: RestartingActionState }) {
  if (!gate.waiting) return null
  return (
    <div className="restart-banner" role="status" aria-live="polite">
      <span className="restart-banner-dot" aria-hidden="true" />
      {gate.offline ? '面板已断开，正在等待恢复…' : '已保存，等待面板重启…'}
    </div>
  )
}

/**
 * 设备管理 section: device rows (drag reorder, enable toggle, edit, archive)
 * plus the two add entries (快速接入 wizard / 手动添加 form).
 */
export function DevicesSection({ settings, restartGate, onChanged }: DevicesSectionProps) {
  const { devices: statuses, requestReload } = useShell()
  const [mode, setMode] = useState<EditorMode>({ kind: 'list' })
  const [cleanup, setCleanup] = useState<RouterOSCleanup | null>(() => consumePendingCleanup())
  const [archiveTarget, setArchiveTarget] = useState<SettingsDevice | null>(null)
  const [archiving, setArchiving] = useState(false)
  const [archiveError, setArchiveError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const active = settings.devices.filter((device) => !device.archived)

  const notifyChanged = () => {
    onChanged()
    requestReload()
  }

  const handleReorder = async (deviceIds: string[]) => {
    setActionError(null)
    try {
      await restartGate.run(() => reorderDevices(deviceIds), notifyChanged)
      toast('设备顺序已更新')
    } catch (error) {
      setActionError(errorMessage(error, '设备排序保存失败'))
    }
  }

  const handleToggle = async (device: SettingsDevice) => {
    setActionError(null)
    const nextEnabled = !device.enabled
    try {
      await restartGate.run(
        () =>
          updateDevice(device.id, {
            name: device.name,
            enabled: nextEnabled,
            scheme: device.scheme,
            host: device.host,
            port: device.port,
            username: device.username,
            // Blank password keeps the stored credential (prepareDevice).
            password: '',
            trafficInterfaces: device.trafficInterfaces,
            // Send the persisted scope config verbatim: a legacy manual
            // selection must survive a simple enable/disable toggle.
            trafficScope: device.trafficScope,
            terminalCidrs: device.terminalCidrs,
            terminalScope: device.terminalScope,
            deferRestart: false,
          }),
        notifyChanged,
      )
    } catch (error) {
      setActionError(errorMessage(error, nextEnabled ? '启用设备失败' : '停用设备失败'))
    }
  }

  const confirmArchive = async () => {
    if (!archiveTarget) return
    setArchiving(true)
    setArchiveError(null)
    try {
      await restartGate.run(async (): Promise<MutationResult> => {
        const result = await archiveDevice(archiveTarget.id)
        if (result.cleanup) storePendingCleanup(result.cleanup)
        return result
      }, notifyChanged)
      // Restarting path reloads the page; the non-restarting fallback closes here.
      setArchiveTarget(null)
      setArchiving(false)
    } catch (error) {
      setArchiveError(errorMessage(error, '归档设备失败'))
      setArchiving(false)
    }
  }

  const savedVia = (action: () => Promise<MutationResult>) => restartGate.run(action, notifyChanged)

  if (mode.kind === 'edit') {
    return (
      <Card
        title={`编辑设备 · ${mode.device.name}`}
        sub={`${mode.device.host}:${mode.device.port}`}
        actions={
          <Button size="sm" disabled={restartGate.waiting} onClick={() => setMode({ kind: 'list' })}>
            返回设备列表
          </Button>
        }
      >
        <ManualAddWizard
          key={mode.device.id}
          device={mode.device}
          busy={restartGate.waiting}
          onSaved={savedVia}
          onCancelEdit={() => setMode({ kind: 'list' })}
          onArchive={(device) => setArchiveTarget(device)}
        />
        {renderArchiveModal()}
      </Card>
    )
  }

  if (mode.kind === 'quick' || mode.kind === 'manual') {
    return (
      <Card
        title={mode.kind === 'quick' ? '快速接入' : '手动添加'}
        sub={mode.kind === 'quick' ? '脚本自动创建受限专用账号' : '使用现有 RouterOS 账号，保存前自动检测'}
        actions={
          <Button size="sm" disabled={restartGate.waiting} onClick={() => setMode(active.length ? { kind: 'list' } : { kind: 'choice' })}>
            返回
          </Button>
        }
      >
        {mode.kind === 'quick' ? <QuickOnboardingWizard busy={restartGate.waiting} onSaved={savedVia} /> : <ManualAddWizard busy={restartGate.waiting} onSaved={savedVia} />}
      </Card>
    )
  }

  if (mode.kind === 'choice') {
    return (
      <Card
        title="添加 RouterOS 设备"
        sub="选择接入方式"
        actions={
          active.length ? (
            <Button size="sm" onClick={() => setMode({ kind: 'list' })}>
              返回设备列表
            </Button>
          ) : undefined
        }
      >
        <div className="add-choice-grid">
          <button type="button" className="add-choice" onClick={() => setMode({ kind: 'quick' })}>
            <span className="add-choice-icon" aria-hidden="true">
              ⚡
            </span>
            <strong>快速接入（推荐）</strong>
            <small>生成一段脚本，在 RouterOS 执行后自动创建受限账号并完成接入，无需手动填写密码。</small>
          </button>
          <button type="button" className="add-choice" onClick={() => setMode({ kind: 'manual' })}>
            <span className="add-choice-icon" aria-hidden="true">
              ✎
            </span>
            <strong>手动添加</strong>
            <small>使用已有的 RouterOS REST 账号；保存前自动检测连接与 LAN/WAN 范围。</small>
          </button>
        </div>
      </Card>
    )
  }

  return (
    <Card
      title="设备管理"
      sub={`${active.length} 台设备`}
      actions={
        <Button size="sm" variant="primary" disabled={restartGate.waiting} onClick={() => setMode({ kind: 'choice' })}>
          ＋ 添加设备
        </Button>
      }
    >
      {cleanup ? <CleanupCard cleanup={cleanup} onClose={() => setCleanup(null)} /> : null}
      {actionError ? (
        <p className="form-error" role="alert">
          {actionError}
        </p>
      ) : null}
      {active.length ? (
        <>
          <p className="faint device-list-hint">拖动 ⠿ 可调整顺序，主页设备列表按此顺序显示。</p>
          <DeviceList
            devices={active}
            statuses={statuses}
            busy={restartGate.waiting}
            onEdit={(device) => setMode({ kind: 'edit', device })}
            onToggle={(device) => void handleToggle(device)}
            onArchive={(device) => setArchiveTarget(device)}
            onReorder={(deviceIds) => void handleReorder(deviceIds)}
          />
        </>
      ) : (
        <EmptyState
          icon="📡"
          title="尚未添加 RouterOS 设备"
          description="支持脚本快速接入或手动填写账号，保存前自动检测连接与范围。"
          actionLabel="添加 RouterOS 设备"
          onAction={() => setMode({ kind: 'choice' })}
        />
      )}
      {renderArchiveModal()}
    </Card>
  )

  function renderArchiveModal() {
    return (
      <Modal open={archiveTarget !== null} onClose={archiving ? () => undefined : () => setArchiveTarget(null)} title="归档设备">
        {archiveTarget ? (
          <div className="archive-confirm">
            <p>
              归档设备「<strong>{archiveTarget.name}</strong>」？历史数据将保留，可随时在 维护设置 → 危险区 恢复。
              {archiveTarget.cleanupAvailable ? '归档后会提供 RouterOS 专用账号的清理脚本。' : ''}
            </p>
            {archiveError ? (
              <p className="form-error" role="alert">
                {archiveError}
              </p>
            ) : null}
            <div className="verify-actions">
              <Button disabled={archiving} onClick={() => setArchiveTarget(null)}>
                取消
              </Button>
              <Button variant="danger" loading={archiving} onClick={() => void confirmArchive()}>
                {archiving ? '正在归档…' : '确认归档'}
              </Button>
            </div>
          </div>
        ) : null}
      </Modal>
    )
  }
}
