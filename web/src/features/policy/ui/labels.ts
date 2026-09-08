/** Shared Chinese display labels (UX standard §13 terminology). */
import type { PolicyJob } from '../api'
import type { TargetList } from '../canonical'
import { formatCount } from '../../../lib/format'

export function kindLabel(kind: string): string {
  return kind === 'ip' ? 'IP' : '域名'
}

export function sourceTypeLabel(sourceType: string): string {
  switch (sourceType) {
    case 'url':
      return 'URL'
    case 'upload':
      return '上传'
    case 'preset':
      return '预设'
    default:
      return '手动'
  }
}

export function scheduleLabel(schedule: string): string {
  switch (schedule) {
    case '1h':
      return '每 1 小时'
    case '6h':
      return '每 6 小时'
    case '12h':
      return '每 12 小时'
    case '24h':
      return '每天'
    case '7d':
      return '每 7 天'
    case '30d':
      return '每 30 天'
    default:
      return '手动刷新'
  }
}

export function failureModeLabel(mode: string): string {
  switch (mode) {
    case 'strict':
      return '断线阻断'
    case 'fallback':
      return '回落 main 表'
    case 'existing':
      return '沿用现有规则'
    default:
      return '断线阻断'
  }
}

export function natModeLabel(mode: string): string {
  switch (mode) {
    case 'none':
      return '不建立 NAT'
    case 'masquerade':
      return 'masquerade'
    default:
      return '沿用现有'
  }
}

export function jobPhaseLabel(job: PolicyJob): string {
  const labels: Record<string, string> = {
    queued: '排队中',
    staging: '写入暂存',
    verifying: '校验变更',
    dns: '更新 DNS',
    cleanup: '清理旧对象',
    activation: '激活变更',
    follow_up: '收尾检查',
    committed: '已应用',
    failed: '失败',
  }
  return labels[job.phase] ?? labels[job.state] ?? job.phase ?? job.state ?? '进行中'
}

export function planKindLabel(kind: string): string {
  const labels: Record<string, string> = {
    initial: '初始化配置',
    structural: '结构变更',
    'egress-state': '出口状态',
    'egress-delete': '删除出口',
    'routing-rule-save': '保存分流规则',
    'routing-rule-delete': '删除分流规则',
    'access-rule-save': '保存访问规则',
    'access-rule-delete': '删除访问规则',
    'access-control-sync': '访问控制同步',
    'target-list-save': '保存目标库',
    'target-list-delete': '删除目标库',
    'target-list-refresh': '刷新目标库',
  }
  return labels[kind] ?? kind ?? '变更'
}

export function planDomainLabel(domain?: string): string {
  switch (domain) {
    case 'routing':
      return '策略路由'
    case 'access':
      return '访问控制'
    case 'combined':
      return '策略路由 + 访问控制'
    default:
      return '—'
  }
}

export const PLAN_ACTION_LABELS: Record<string, string> = {
  create: '新建',
  patch: '修改',
  delete: '删除',
  move: '移动',
  disable: '停用',
  enable: '启用',
  reuse: '复用',
  adopt: '接管',
  reference_add: '建立引用',
  reference_remove: '解除引用',
}

export function planMenuLabel(menu?: string): string {
  const labels: Record<string, string> = {
    'routing/table': '策略路由表',
    'ip/route': 'IPv4 路由',
    'ipv6/route': 'IPv6 路由',
    'routing/rule': '路由规则',
    'ip/firewall/mangle': 'IPv4 流量标记',
    'ipv6/firewall/mangle': 'IPv6 流量标记',
    'ip/firewall/address-list': 'IPv4 地址列表',
    'ipv6/firewall/address-list': 'IPv6 地址列表',
    'ip/firewall/filter': 'IPv4 访问控制',
    'ipv6/firewall/filter': 'IPv6 访问控制',
    'ip/firewall/nat': 'IPv4 NAT',
    'ipv6/firewall/nat': 'IPv6 NAT',
    'ip/dns': 'DNS',
    'ip/dns/static': 'DNS 静态记录',
  }
  return labels[menu ?? ''] ?? (menu || 'RouterOS 对象')
}

/** Target list version/apply state for badges (待命/已应用/待应用/清理中). */
export function targetState(target: Pick<TargetList, 'pendingDeletion' | 'pendingVersionId' | 'activeVersionId'>): { tone: 'ok' | 'warn' | 'neutral'; label: string } {
  if (target.pendingDeletion) return { tone: 'warn', label: '清理中' }
  if (target.pendingVersionId) return { tone: 'warn', label: '待应用' }
  if (target.activeVersionId) return { tone: 'ok', label: '已应用' }
  return { tone: 'neutral', label: '待命' }
}

export function targetCountCap(target: TargetList): string {
  return `${formatCount(target.counts.valid ?? 0)} 条`
}
