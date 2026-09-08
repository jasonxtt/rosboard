import { useState } from 'react'
import type { DragEvent } from 'react'
import { Badge, StatusDot, Toggle, Tooltip } from '../../ui'
import type { DeviceStatus } from '../../lib/types'
import type { SettingsDevice } from './api'
import { AccountBadge } from './AccountBadge'

type DeviceRowStatus = { tone: 'ok' | 'warn' | 'err' | 'neutral'; label: string }

function deviceRowStatus(device: SettingsDevice, statuses: DeviceStatus[]): DeviceRowStatus {
  if (!device.enabled) return { tone: 'neutral', label: '已停用' }
  const status = statuses.find((item) => item.id === device.id)
  if (status?.error) return { tone: 'err', label: '连接异常' }
  if (status?.healthy) return { tone: 'ok', label: '在线' }
  return { tone: 'warn', label: '等待采集' }
}

type DeviceListProps = {
  devices: SettingsDevice[]
  statuses: DeviceStatus[]
  busy: boolean
  onEdit: (device: SettingsDevice) => void
  onToggle: (device: SettingsDevice) => void
  onArchive: (device: SettingsDevice) => void
  onReorder: (deviceIds: string[]) => void
}

/**
 * Device rows in display order: drag handle reorder (→ PUT reorder),
 * enabled toggle, online status, host:port mono, edit and archive actions.
 */
export function DeviceList({ devices, statuses, busy, onEdit, onToggle, onArchive, onReorder }: DeviceListProps) {
  const [draggedId, setDraggedId] = useState<string | null>(null)
  const [dragOverId, setDragOverId] = useState<string | null>(null)

  const handleDragStart = (event: DragEvent<HTMLButtonElement>, deviceId: string) => {
    if (busy) {
      event.preventDefault()
      return
    }
    setDraggedId(deviceId)
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', deviceId)
  }

  const handleDragOver = (event: DragEvent<HTMLDivElement>, deviceId: string) => {
    if (!draggedId || draggedId === deviceId || busy) return
    event.preventDefault()
    event.dataTransfer.dropEffect = 'move'
    setDragOverId(deviceId)
  }

  const handleDrop = (event: DragEvent<HTMLDivElement>, targetId: string) => {
    event.preventDefault()
    const deviceId = event.dataTransfer.getData('text/plain') || draggedId
    setDraggedId(null)
    setDragOverId(null)
    if (!deviceId || deviceId === targetId) return
    const index = devices.findIndex((device) => device.id === deviceId)
    const target = devices.findIndex((device) => device.id === targetId)
    if (index < 0 || target < 0) return
    const next = devices.slice()
    const [moved] = next.splice(index, 1)
    next.splice(target, 0, moved)
    onReorder(next.map((device) => device.id))
  }

  return (
    <div className="device-list" role="list" aria-label="设备列表">
      {devices.map((device) => {
        const status = deviceRowStatus(device, statuses)
        const runtime = statuses.find((item) => item.id === device.id)
        const meta = [runtime?.routerName, runtime?.version].filter(Boolean).join(' · ')
        return (
          <div
            key={device.id}
            role="listitem"
            className={`device-row${dragOverId === device.id ? ' device-row-drag-over' : ''}${draggedId === device.id ? ' device-row-dragging' : ''}`}
            onDragOver={(event) => handleDragOver(event, device.id)}
            onDrop={(event) => handleDrop(event, device.id)}
          >
            <button
              type="button"
              className="device-row-handle"
              draggable={!busy}
              aria-label={`拖拽排序设备 ${device.name}`}
              title="按住拖拽排序"
              onDragStart={(event) => handleDragStart(event, device.id)}
              onDragEnd={() => {
                setDraggedId(null)
                setDragOverId(null)
              }}
            >
              ⠿
            </button>
            <div className="device-row-main">
              <div className="device-row-title">
                <strong>{device.name}</strong>
                <Badge tone={status.tone} dot>
                  {status.label}
                </Badge>
                {device.enabled ? <AccountBadge deviceId={device.id} /> : <Badge tone="neutral">不采集</Badge>}
              </div>
              <div className="device-row-meta">
                <StatusDot tone={status.tone === 'neutral' ? 'neutral' : status.tone} />
                <span className="mono num">
                  {device.host}:{device.port}
                </span>
                {meta ? <span className="faint">{meta}</span> : null}
                {runtime?.error ? (
                  <span className="device-row-error" title={runtime.error}>
                    {runtime.error}
                  </span>
                ) : null}
              </div>
            </div>
            <div className="device-row-actions">
              <Tooltip tip={device.enabled ? '停用采集' : '启用采集'}>
                <Toggle
                  checked={device.enabled}
                  disabled={busy}
                  label={`${device.enabled ? '停用' : '启用'}设备 ${device.name}`}
                  onChange={() => onToggle(device)}
                />
              </Tooltip>
              <button type="button" className="icon-btn" aria-label={`编辑设备 ${device.name}`} title="编辑" disabled={busy} onClick={() => onEdit(device)}>
                ✎
              </button>
              <button
                type="button"
                className="icon-btn device-row-archive"
                aria-label={`归档设备 ${device.name}`}
                title="归档"
                disabled={busy}
                onClick={() => onArchive(device)}
              >
                🗄
              </button>
              <span className="device-archive-hint" title="归档后保留数据，可在维护设置中恢复或彻底删除">
                归档保留数据，可恢复
              </span>
            </div>
          </div>
        )
      })}
    </div>
  )
}
