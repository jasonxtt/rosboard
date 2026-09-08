/** All 15 top-level views. State-based switching — no router library. */
export type View =
  | 'fleet'
  | 'overview'
  | 'interfaces'
  | 'terminals'
  | 'protocols'
  | 'policies'
  | 'dhcp'
  | 'routes'
  | 'resource'
  | 'load'
  | 'target-library'
  | 'policy-routing'
  | 'access-control'
  | 'recognition'
  | 'settings'

export const VIEW_TITLES: Record<View, string> = {
  fleet: '仪表台',
  overview: '系统概览',
  interfaces: '接口监控',
  terminals: '终端监控',
  protocols: '协议统计',
  policies: '策略统计',
  dhcp: 'DHCP',
  routes: '路由 / 分流',
  resource: '资源监控',
  load: '负载历史',
  'target-library': '目标库',
  'policy-routing': '策略路由',
  'access-control': '访问控制',
  recognition: '识别设置',
  settings: '面板设置',
}

/** Top-level pill nav (§6): direct entries + two grouped flyout menus. */
export const TOP_LEVEL_NAV: View[] = ['fleet', 'overview']

export type NavGroup = 'monitor' | 'host'

export const NAV_GROUPS: Array<{ key: NavGroup; label: string; items: View[] }> = [
  {
    key: 'monitor',
    label: '状态监控',
    items: ['interfaces', 'terminals', 'protocols', 'policies', 'dhcp', 'routes', 'resource', 'load'],
  },
  {
    key: 'host',
    label: '主机设置',
    items: ['target-library', 'policy-routing', 'access-control', 'recognition'],
  },
]

/** Flyout item metadata: icon + one-line description (mega popover / mobile drawer). */
export const NAV_ITEM_META: Partial<Record<View, { icon: string; desc: string }>> = {
  interfaces: { icon: '⇄', desc: '链路状态、实时速率与趋势' },
  terminals: { icon: '⛁', desc: '在线终端、连接与流量明细' },
  protocols: { icon: '∿', desc: '按应用与协议看流量构成' },
  policies: { icon: '▤', desc: 'RouterOS 队列与标记计数' },
  dhcp: { icon: '⌗', desc: '地址池用量与租约' },
  routes: { icon: '⤢', desc: '路由表与分流命中情况' },
  resource: { icon: '✚', desc: 'CPU、内存、硬件与 IRQ' },
  load: { icon: '⧗', desc: '多时间窗的系统负载曲线' },
  'target-library': { icon: '◎', desc: '域名 / IP 列表，供规则引用' },
  'policy-routing': { icon: '⑂', desc: '让指定流量走指定出口线路' },
  'access-control': { icon: '⊘', desc: '断网时段与目标屏蔽' },
  recognition: { icon: '✦', desc: '应用识别与 MosDNS 数据源' },
}

/** Group that contains a view, if any (drives parent-pill active state). */
export function navGroupOf(view: View): NavGroup | null {
  for (const group of NAV_GROUPS) if (group.items.includes(view)) return group.key
  return null
}

/** Views whose data belongs to the selected device; they remount on switch. */
export const DEVICE_SCOPED_VIEWS: ReadonlySet<View> = new Set([
  'overview',
  'interfaces',
  'terminals',
  'protocols',
  'policies',
  'dhcp',
  'routes',
  'resource',
  'load',
  'target-library',
  'policy-routing',
  'access-control',
  'recognition',
])

const ALL_VIEWS = Object.keys(VIEW_TITLES) as View[]

/**
 * Hash routing (`#/<view>`, terminal detail `#/terminals/<id>`): parse the
 * view segment; invalid or empty hashes return null (caller falls back to
 * the landing view). Sub-paths (e.g. the terminal id) are owned by the page.
 */
export function viewFromHash(hash: string): View | null {
  const head = hash.replace(/^#\/?/, '').split('/')[0]
  return (ALL_VIEWS as string[]).includes(head) ? (head as View) : null
}

export function hashForView(view: View): string {
  return `#/${view}`
}
