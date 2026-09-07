import { Button, CopyButton } from '../../ui'
import type { RouterOSCleanup } from './api'

type CleanupCardProps = {
  cleanup: RouterOSCleanup
  onClose: () => void
}

/**
 * RouterOS managed-account cleanup script card (old RouterOSCleanupCard:2010).
 * Shown after archiving a device whose account was provisioned by rosboard,
 * and from the archived-devices section on demand.
 */
export function CleanupCard({ cleanup, onClose }: CleanupCardProps) {
  const download = () => {
    const url = URL.createObjectURL(new Blob([cleanup.script], { type: 'text/plain' }))
    const link = document.createElement('a')
    link.href = url
    link.download = `rosboard-cleanup-${cleanup.name || cleanup.deviceId}.rsc`
    link.click()
    URL.revokeObjectURL(url)
  }

  return (
    <section className="cleanup-card" aria-label="清理 RouterOS 专用账号">
      <div className="cleanup-card-head">
        <div>
          <strong>清理 RouterOS 专用账号</strong>
          <p className="faint">
            {cleanup.name} · 用户 <span className="mono">{cleanup.username}</span> · 组 <span className="mono">{cleanup.groupName}</span>
          </p>
        </div>
        <Button size="sm" onClick={onClose}>
          关闭
        </Button>
      </div>
      <p className="cleanup-card-warning">只有确定不再恢复此设备时才执行。脚本会删除 rosboard 创建的专用用户；仅当专用组没有其他用户时才删除该组。</p>
      <textarea className="textarea script-area" readOnly value={cleanup.script} rows={10} spellCheck={false} aria-label="RouterOS 账号清理脚本" />
      <div className="cleanup-card-actions">
        <Button size="sm" onClick={download}>
          下载脚本
        </Button>
        <CopyButton text={cleanup.script} label="复制清理脚本" />
      </div>
    </section>
  )
}
