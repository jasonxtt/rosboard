import { Card, EmptyState } from '../../ui'

type EmptyDevicePanelProps = {
  /** Navigate to the device add flow (settings 设备管理 / onboarding). */
  onAdd: () => void
}

/**
 * Shown when the panel has no enabled RouterOS devices and the current view
 * is device-scoped (old EmptyDevicePanel:1797). Exported from
 * features/settings so the shell can wire it into ShellApp for device-scoped
 * views; the settings 设备管理 section also renders it for the empty list.
 */
export function EmptyDevicePanel({ onAdd }: EmptyDevicePanelProps) {
  return (
    <Card>
      <EmptyState
        icon="📡"
        title="尚未添加 RouterOS 设备"
        description="添加设备后，这里会自动开始显示监控内容。支持脚本快速接入或手动填写账号，保存前自动检测连接与范围。"
        actionLabel="添加 RouterOS 设备"
        onAction={onAdd}
      />
    </Card>
  )
}
