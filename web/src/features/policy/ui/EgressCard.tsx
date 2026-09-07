import { Badge } from '../../../ui/Badge'
import type { BadgeTone } from '../../../ui/Badge'
import { StatusDot } from '../../../ui/StatusDot'
import { formatBitRate } from '../../../lib/format'
import type { Egress } from '../canonical'
import type { InterfaceRate } from '../api'
import { failureModeLabel, natModeLabel } from './labels'

type EgressCardProps = {
  egress: Egress
  /** live rates keyed by WAN interface name, when resolvable */
  rates?: Map<string, InterfaceRate>
  busy?: boolean
  onEdit: () => void
  onDelete: () => void
  onToggle: (enabled: boolean) => void
}

function egressBadge(egress: Egress): { tone: BadgeTone; dot: 'ok' | 'warn' | 'err' | 'neutral'; label: string } {
  if (egress.pendingDeletion) return { tone: 'warn', dot: 'warn', label: '待删除' }
  if (!egress.enabled) return { tone: 'neutral', dot: 'neutral', label: '已停用' }
  if (!egress.applied) return { tone: 'warn', dot: 'warn', label: '待应用' }
  return { tone: 'ok', dot: 'ok', label: '已应用' }
}

function familyLabel(family: string): string {
  return family === 'ipv6' ? 'IPv6' : 'IPv4'
}

/** 出口卡 (§9.2): WAN 图标 + 名称 + 状态徽章 + kv 行（接口/协议族/网关/故障策略/当前负载）. */
export function EgressCard({ egress, rates, busy = false, onEdit, onDelete, onToggle }: EgressCardProps) {
  const badge = egressBadge(egress)
  const enabledFamilies = egress.families.filter((family) => family.enabled)
  const interfaceText =
    enabledFamilies.map((family) => `${familyLabel(family.family)} ${family.wanInterface || (family.wanSource === 'next-hop' ? '下一跳' : '未配置')}`).join(' · ') || '—'
  const gatewayText = enabledFamilies
    .map((family) => `${familyLabel(family.family)} ${family.gateway || (family.wanInterface ? '自动' : '—')}`)
    .join(' · ')
  const tableText = enabledFamilies.map((family) => `${familyLabel(family.family)} ${family.routeTable || '自动'}`).join(' · ')
  const natText = enabledFamilies.map((family) => `${familyLabel(family.family)} ${natModeLabel(family.natMode)}`).join(' · ')

  let download = 0
  let upload = 0
  let resolved = false
  if (rates) {
    for (const family of enabledFamilies) {
      const rate = rates.get(family.wanInterface)
      if (rate) {
        download += rate.downloadBps
        upload += rate.uploadBps
        resolved = true
      }
    }
  }

  return (
    <div className={`glass pol-egr-card${egress.enabled ? '' : ' pol-egr-disabled'}`}>
      <div className="pol-egr-head">
        <span className="pol-egr-wan" aria-hidden="true">
          🌐
        </span>
        <strong className="pol-egr-name">{egress.name || '未命名出口'}</strong>
        <span className="pol-egr-head-right">
          <StatusDot tone={badge.dot} />
          <Badge tone={badge.tone}>{badge.label}</Badge>
        </span>
      </div>
      <div className="pol-kv-list">
        <div className="pol-kv">
          <span>接口</span>
          <b>{interfaceText}</b>
        </div>
        <div className="pol-kv">
          <span>网关</span>
          <b>{gatewayText || '—'}</b>
        </div>
        <div className="pol-kv">
          <span>路由表</span>
          <b>{tableText || '—'}</b>
        </div>
        <div className="pol-kv">
          <span>NAT</span>
          <b>{natText || '—'}</b>
        </div>
        <div className="pol-kv">
          <span>故障策略</span>
          <b>{failureModeLabel(egress.failureMode)}</b>
        </div>
        <div className="pol-kv">
          <span>当前负载</span>
          <b className={resolved ? 'pol-egr-rate' : undefined}>{resolved ? `↓${formatBitRate(download)} ↑${formatBitRate(upload)}` : '—'}</b>
        </div>
      </div>
      <div className="pol-egr-actions">
        <button type="button" className="link-button" disabled={busy || egress.pendingDeletion} onClick={() => onToggle(!egress.enabled)}>
          {egress.enabled ? '停用' : '启用'}
        </button>
        <button type="button" className="link-button" disabled={busy || egress.pendingDeletion} onClick={onEdit}>
          编辑
        </button>
        <button type="button" className="link-button link-danger" disabled={busy || egress.pendingDeletion} onClick={onDelete}>
          删除
        </button>
      </div>
    </div>
  )
}
