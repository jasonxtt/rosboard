import { useEffect, useState } from 'react'
import { Badge, Tooltip } from '../../ui'
import { fetchDeviceAccount, type DeviceAccountStatus } from './api'

/**
 * Session-scoped cache: GET /api/devices/{id}/account performs a live
 * RouterOS call, so each device is probed at most once per page lifetime.
 */
const accountCache = new Map<string, DeviceAccountStatus>()

type AccountBadgeProps = {
  deviceId: string
}

/** Per-row RouterOS account permission badge (可写 / 只读 / 未知). */
export function AccountBadge({ deviceId }: AccountBadgeProps) {
  const [status, setStatus] = useState<DeviceAccountStatus | null>(() => accountCache.get(deviceId) ?? null)

  useEffect(() => {
    let cancelled = false
    fetchDeviceAccount(deviceId)
      .then((result) => {
        accountCache.set(deviceId, result)
        if (!cancelled) setStatus(result)
      })
      .catch(() => {
        if (!cancelled) setStatus({ username: '', group: '', policies: [], permission: 'unknown', writeAccess: false })
      })
    return () => {
      cancelled = true
    }
  }, [deviceId])

  if (!status) {
    return (
      <Badge tone="neutral" dot>
        权限读取中
      </Badge>
    )
  }
  const tip = status.username
    ? `账号 ${status.username}${status.group ? ` · 组 ${status.group}` : ''}${status.error ? ` · ${status.error}` : ''}`
    : status.error || '无法读取 RouterOS 账号权限'
  if (status.permission === 'write') {
    return (
      <Tooltip tip={tip}>
        <Badge tone="ok" dot>
          可写
        </Badge>
      </Tooltip>
    )
  }
  if (status.permission === 'read_only') {
    return (
      <Tooltip tip={tip}>
        <Badge tone="warn" dot>
          只读
        </Badge>
      </Tooltip>
    )
  }
  return (
    <Tooltip tip={tip}>
      <Badge tone="neutral" dot>
        权限未知
      </Badge>
    </Tooltip>
  )
}
