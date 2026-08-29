import type {
  PolicyAddressFamily,
  PolicyProofStatus as PS,
  PolicySection,
  PolicySetupState,
} from './types'

export const policySetupStateLabel: Record<PolicySetupState, string> = {
  write_access_required: 'RouterOS 账号缺少写入权限',
  manager_unavailable: '策略管理器不可用',
  runtime_unavailable: '策略运行时不可用',
  ready: '就绪',
}

export const policyFamilyLabel: Record<PolicyAddressFamily, string> = {
  ipv4: 'IPv4',
  ipv6: 'IPv6',
}

export const policyListModeLabel: Record<string, string> = {
  shared: '共享标记列表',
  dedicated: '每域名列表专用标记列表',
}

export const policyFailureModeLabel: Record<string, string> = {
  strict: '断线阻断',
  fallback: '切主线路',
  existing: '沿用规则',
}

export const policySourceTypeLabel: Record<string, string> = {
  url: '远程 URL',
  upload: '本地上传',
}

export const policyWANSourceLabel: Record<string, string> = {
  '': '接口模式',
  'next-hop': '下一跳网关',
}

export const policyRouteModeLabel: Record<string, string> = {
  '': '跟随全局断线处理',
  strict: '严格绑定',
  fallback: '允许回落 main',
}

export const policyRuleTypeLabel: Record<string, string> = {
  DOMAIN: '精确匹配',
  'DOMAIN-SUFFIX': '后缀匹配',
}

export const policyOwnershipLabel: Record<string, string> = {
  owned: 'rosboard 自有',
  reused: '复用用户对象',
  foreign: '其他实例所有',
  manual_candidate: '手工配置候选',
}

export const policyCapabilityStatusLabel: Record<string, string> = {
  supported: '支持',
  unsupported: '不支持',
  unknown: '未知',
  unavailable: '不可用',
}

export const policyProofStatusLabel: Record<string, string> = {
  safe: '安全',
  warning: '警告',
  blocker: '阻断',
  indeterminate: '待定',
}

export const policyScanIssueKindLabel: Record<string, string> = {
  blocker: '阻断',
  warning: '警告',
  unavailable: '不可用',
}

export const policyPlanKindLabel: Record<string, string> = {
  initial: '初次应用',
  structural: '结构变更',
  domain_delta: '域名增量',
  source_migration: '来源迁移',
  disable_delete: '停用/删除',
  adoption: '接管',
}

export const policyOperationActionLabel: Record<string, string> = {
  create: '创建',
  patch: '修改',
  delete: '删除',
  move: '移动',
  disable: '停用',
  enable: '启用',
  reference_add: '关联引用',
  reference_remove: '解除引用',
  reuse: '复用',
  adopt: '接管',
  dns_cache_flush: '刷新 DNS 缓存',
}

export const policyAckCodeLabel: Record<string, string> = {
  fallback_main_table: '允许失败时回落到 main 路由表',
  main_table_reuse: '复用 main 路由表（继承其 ECMP / 故障切换与未来变化）',
  firewall_high_risk_exception: '接受防火墙不确定性的高风险例外（可能形成开放解析器）',
  reuse_user_list: '复用用户已有标记列表（其现有与未来条目都会受策略影响）',
  adoption: '确认接管手工配置的所有权',
  force_adoption: '确认旧实例已停止，强制接管其对象',
  managed_field_delta: '确认修改共享对象的受管字段（删除时仅撤回 rosboard 增量）',
  large_change: '确认大规模变更（超过 10,000 条规则）',
  source_shrink_review: '确认来源规则缩减超过 50%',
}

export const policyCapabilityKeyLabel: Record<string, string> = {
  named_forwarder: 'DNS 命名转发器',
  dns_static_address_list: 'DNS Static 标记列表',
  dhcp_default_route_tables: 'DHCP default-route-tables',
  ipv6_family: 'IPv6 地址族',
  move_order: '规则排序 (move)',
}

export const policySectionLabel: Record<PolicySection, string> = {
  settings: '策略设置',
  sources: '域名列表',
}

export const policyScheduleLabel: Record<string, string> = {
  manual: '手动更新',
  '1h': '每小时',
  '6h': '每 6 小时',
  '12h': '每 12 小时',
  '24h': '每 24 小时',
  '7d': '每 7 天',
  '30d': '每 30 天',
}

export const scheduleOptions: Array<{ value: string; label: string }> = [
  { value: 'manual', label: '手动更新' },
  { value: '1h', label: '每小时' },
  { value: '6h', label: '每 6 小时' },
  { value: '12h', label: '每 12 小时' },
  { value: '24h', label: '每 24 小时（默认）' },
  { value: '7d', label: '每 7 天' },
  { value: '30d', label: '每 30 天' },
]

export function policyStatusLabel(status: PS | string): string {
  return policyProofStatusLabel[status] ?? status
}

export function policyCapabilityLabel(key: string): string {
  return policyCapabilityKeyLabel[key] ?? key
}

export function policyViewTitle(section: PolicySection | string): string {
  if (section === 'sources') return '域名列表'
  return '策略设置'
}
