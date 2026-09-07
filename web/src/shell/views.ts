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

/** Center pill nav (§6): 概览 / 接口 / 终端 / 策略 + 更多 overflow. */
export const PRIMARY_NAV: View[] = ['overview', 'interfaces', 'terminals', 'policy-routing']

/** Views folded into the 更多 menu. 设备总览 also lives in the device pill. */
export const MORE_NAV: View[] = ['fleet', 'protocols', 'policies', 'dhcp', 'routes', 'resource', 'load', 'target-library', 'access-control', 'recognition']

/** Mobile bottom bar (§11): four entries + 更多 drawer. */
export const MOBILE_NAV: View[] = ['overview', 'interfaces', 'terminals', 'policy-routing']

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
