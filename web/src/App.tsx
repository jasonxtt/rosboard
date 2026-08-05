import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import rosboardMark from './assets/rosboard-mark.svg'
import {
  compareTerminal,
  formatBitRate,
  formatBits,
  formatBytes,
  formatDateTime,
  formatOnlineDuration,
  formatSeconds,
  formatShortTime,
  terminalMetrics,
  terminalPrimaryAddress,
  terminalStateText,
  viewTitle,
} from './lib/format'
import type {
  ActiveView,
  BootstrapResponse,
  ConnectionFamily,
  DashboardResponse,
  DeviceStatus,
  DHCPStat,
  FleetDevice,
  FleetOverview,
  InterfaceDetail,
  InterfaceStatus,
  LoadSample,
  Overview,
  PolicyStat,
  ProtocolHistorySample,
  ProtocolStat,
  RouteStat,
  RateSample,
  SettingsResponse,
  SettingsDevice,
  SystemResource,
  Terminal,
  TerminalConnection,
  TerminalDetail,
  TerminalFamily,
  TerminalScopeSummary,
  TerminalScope,
  TrafficScope,
  TerminalSortKey,
  TerminalTab,
  VerificationResponse,
  ProvisioningSessionResponse,
  ProvisioningCompleteResponse,
  RouterOSCleanupResponse,
} from './lib/types'

type IconName = 'overview' | 'status' | 'network' | 'terminal' | 'traffic' | 'policy' | 'runtime' | 'route' | 'settings' | 'refresh' | 'cpu' | 'memory' | 'connections' | 'shield' | 'router' | 'storage' | 'alert' | 'info' | 'check' | 'search' | 'clear' | 'eye' | 'eyeOff' | 'palette'
type SettingsSection = 'connection' | 'collection' | 'recognition' | 'ui' | 'account' | 'maintenance'
type PanelTheme = 'light' | 'dark' | 'glass'
type PanelPreferences = { refreshMs: number; landingView: ActiveView; terminalFamily: TerminalFamily; theme: PanelTheme }
type ConnectionDraft = { scheme: 'http' | 'https'; host: string; port: number; username: string; password: string }
type CollectionDraft = {
  pollIntervalSeconds: number
  realtimePollIntervalSeconds: number
  terminalPollIntervalSeconds: number
  sampleRetentionHours: number
}
type RecognitionDraft = {
  mosdns: { enabled: boolean; baseUrl: string; syncIntervalMinutes: number }
  featureLibrary: { enabled: boolean; sourceUrl: string; refreshIntervalHours: number; matchWindowMinutes: number }
}

const RealtimeTrafficChart = lazy(() => import('./components/RealtimeTrafficChart').then((module) => ({ default: module.RealtimeTrafficChart })))

const panelPreferenceKey = 'rosboard:panel-preferences'
const selectedDeviceKey = 'rosboard:selected-device'
const trafficWindowKey = 'rosboard:traffic-window'
const pendingRouterOSCleanupKey = 'rosboard:pending-routeros-cleanup'
const defaultPanelPreferences: PanelPreferences = { refreshMs: 1000, landingView: 'fleet', terminalFamily: 'all', theme: 'light' }
const restartPollIntervalMs = 750
const restartTimeoutMs = 90_000
const panelThemeOptions: Array<{ value: PanelTheme; label: string; description: string }> = [
  { value: 'light', label: '明亮', description: '清爽的蓝白界面' },
  { value: 'dark', label: '深色', description: '适合夜间使用' },
  { value: 'glass', label: '玻璃渐变', description: '柔和的毛玻璃拟态' },
]

function delay(milliseconds: number) {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds))
}


function fallbackCopyText(value: string) {
  const activeElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const textarea = document.createElement('textarea')
  textarea.value = value
  textarea.readOnly = true
  textarea.setAttribute('aria-hidden', 'true')
  textarea.style.position = 'fixed'
  textarea.style.inset = '0 auto auto 0'
  textarea.style.width = '1px'
  textarea.style.height = '1px'
  textarea.style.padding = '0'
  textarea.style.border = '0'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'
  document.body.appendChild(textarea)
  try {
    textarea.focus({ preventScroll: true })
    textarea.select()
    textarea.setSelectionRange(0, value.length)
    if (!document.execCommand('copy')) throw new Error('copy command was rejected')
  } finally {
    textarea.remove()
    activeElement?.focus({ preventScroll: true })
  }
}

async function copyText(value: string) {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return
    } catch {
      // HTTP deployments and restricted browser permissions use the fallback below.
    }
  }
  fallbackCopyText(value)
}

function pendingRouterOSCleanup(): RouterOSCleanupResponse | null {
  try {
    const raw = window.sessionStorage.getItem(pendingRouterOSCleanupKey)
    if (!raw) return null
    const value = JSON.parse(raw) as Partial<RouterOSCleanupResponse>
    if (!value.deviceId || !value.name || !value.username || !value.groupName || !value.script) return null
    return value as RouterOSCleanupResponse
  } catch {
    return null
  }
}

function storePendingRouterOSCleanup(cleanup: RouterOSCleanupResponse) {
  window.sessionStorage.setItem(pendingRouterOSCleanupKey, JSON.stringify(cleanup))
}

function consumePendingRouterOSCleanup() {
  const cleanup = pendingRouterOSCleanup()
  window.sessionStorage.removeItem(pendingRouterOSCleanupKey)
  return cleanup
}

async function panelAssetsReady() {
  const assetURLs = Array.from(document.querySelectorAll<HTMLScriptElement | HTMLLinkElement>('script[src], link[rel="stylesheet"][href]'))
    .map((element) => element instanceof HTMLScriptElement ? element.src : element.href)
    .filter(Boolean)
  const responses = await Promise.all(assetURLs.map((url) => fetch(url, { cache: 'no-store' })))
  return responses.every((response) => response.ok)
}

async function waitForPanelRestart(onOffline: () => void) {
  const started = Date.now()
  const deadline = Date.now() + restartTimeoutMs
  let observedOffline = false

  await delay(restartPollIntervalMs)
  while (Date.now() < deadline) {
    try {
      const response = await fetch('/api/health', { cache: 'no-store' })
      if ((observedOffline || Date.now() - started > 4000) && response.ok && await panelAssetsReady()) {
        await delay(restartPollIntervalMs)
        window.location.reload()
        return
      }
      if (!response.ok) {
        if (!observedOffline) onOffline()
        observedOffline = true
      }
    } catch {
      if (!observedOffline) onOffline()
      observedOffline = true
    }
    await delay(restartPollIntervalMs)
  }
  throw new Error('面板重启超时，请稍后手动刷新页面')
}

async function postJSON(path: string, body?: unknown) {
  return requestJSON(path, 'POST', body)
}

class APIRequestError extends Error {
  status: number
  code?: string
  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'APIRequestError'
    this.status = status
    this.code = code
  }
}

async function requestJSON(path: string, method: string, body?: unknown) {
  const response = await fetch(path, { method, headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  if (response.status === 401) window.dispatchEvent(new Event('rosboard:authentication-required'))
  const failure = response.ok ? null : await response.json().catch(() => null) as { error?: string; code?: string } | null
  if (!response.ok) throw new APIRequestError(failure?.error || `HTTP ${response.status}`, response.status, failure?.code)
  return response
}
const landingViews: ActiveView[] = ['fleet', 'overview', 'interfaces', 'terminals', 'load', 'resource', 'protocols', 'policies', 'dhcp', 'routes', 'settings']

function loadPanelPreferences(): PanelPreferences {
  try {
    const raw = window.localStorage.getItem(panelPreferenceKey)
    if (!raw) return defaultPanelPreferences
    const parsed = JSON.parse(raw) as Partial<PanelPreferences>
    return {
      refreshMs: [0, 1000, 3000, 5000, 10000].includes(Number(parsed.refreshMs)) ? Number(parsed.refreshMs) : defaultPanelPreferences.refreshMs,
      landingView: parsed.landingView && landingViews.includes(parsed.landingView) ? parsed.landingView : defaultPanelPreferences.landingView,
      terminalFamily: parsed.terminalFamily === 'ipv4' || parsed.terminalFamily === 'ipv6' || parsed.terminalFamily === 'all' ? parsed.terminalFamily : defaultPanelPreferences.terminalFamily,
      theme: parsed.theme === 'dark' ? 'dark' : parsed.theme === 'glass' ? 'glass' : 'light',
    }
  } catch {
    return defaultPanelPreferences
  }
}

function savePanelPreferences(preferences: PanelPreferences) {
  window.localStorage.setItem(panelPreferenceKey, JSON.stringify(preferences))
}

function scopedURL(path: string, deviceID: string) {
  if (!deviceID) return path
  const separator = path.includes('?') ? '&' : '?'
  return `${path}${separator}device=${encodeURIComponent(deviceID)}`
}

function collectionDraftFromSettings(settings: SettingsResponse): CollectionDraft {
  return {
    pollIntervalSeconds: settings.collection.pollIntervalSeconds,
    realtimePollIntervalSeconds: settings.collection.realtimePollIntervalSeconds,
    terminalPollIntervalSeconds: settings.collection.terminalPollIntervalSeconds,
    sampleRetentionHours: settings.collection.sampleRetentionHours,
  }
}

function recognitionDraftFromSettings(settings: SettingsResponse): RecognitionDraft {
  return {
    mosdns: {
      enabled: settings.mosdns.enabled,
      baseUrl: settings.mosdns.baseUrl,
      syncIntervalMinutes: settings.mosdns.syncIntervalMinutes,
    },
    featureLibrary: {
      enabled: settings.featureLibrary.enabled,
      sourceUrl: settings.featureLibrary.sourceUrl,
      refreshIntervalHours: settings.featureLibrary.refreshIntervalHours,
      matchWindowMinutes: settings.featureLibrary.matchWindowMinutes,
    },
  }
}

function parseSettingList(value: string) {
  return value.split(/[\n,]/).map((item) => item.trim()).filter(Boolean)
}

const emptyTerminalScopeSummary: TerminalScopeSummary = {
  deviceCount: 0,
  connectionCount: 0,
  currentUploadBps: 0,
  currentDownloadBps: 0,
  activeUploadBytes: 0,
  activeDownloadBytes: 0,
}

const emptySystemResource: SystemResource = {
  architectureName: '',
  boardName: '',
  badBlocks: '',
  buildTime: '',
  cpu: '',
  cpuCount: '',
  cpuFrequency: '',
  cpuLoad: '',
  factorySoftware: '',
  freeMemory: '',
  freeHddSpace: '',
  platform: '',
  totalMemory: '',
  totalHddSpace: '',
  uptime: '',
  version: '',
  writeSectSinceReboot: '',
  writeSectTotal: '',
  cpuCores: [],
  irqs: [],
  hardware: [],
}

function normalizeOverview(overview: Overview): Overview {
  const systemResource = overview.systemResource ?? emptySystemResource
  return {
    ...overview,
    systemResource: {
      ...emptySystemResource,
      ...systemResource,
      cpuCores: systemResource.cpuCores ?? [],
      irqs: systemResource.irqs ?? [],
      hardware: systemResource.hardware ?? [],
    },
    trafficInterfaces: overview.trafficInterfaces ?? [],
    chartSamples: overview.chartSamples ?? [],
  }
}

function normalizeTerminal(terminal: Terminal): Terminal {
  return {
    ...terminal,
    ipv4: terminal.ipv4 ?? [],
    ipv6: terminal.ipv6 ?? [],
    familyStats: terminal.familyStats ?? {} as Terminal['familyStats'],
  }
}

function normalizeTerminalDetail(detail: TerminalDetail): TerminalDetail {
  detail.terminal = normalizeTerminal(detail.terminal)
  detail.connections ??= []
  detail.connections.forEach((connection) => {
    connection.routeInterfaces ??= []
    connection.egressInterfaces ??= []
  })
  detail.flowCategories ??= []
  detail.history ??= []
  detail.capabilities ??= []
  detail.familySummaries ??= {} as TerminalDetail['familySummaries']
  for (const family of ['ipv4', 'ipv6'] as const) {
    const summary = detail.familySummaries[family]
    if (summary) detail.familySummaries[family] = normalizeTerminal(summary)
  }
  detail.familyFlows ??= {} as TerminalDetail['familyFlows']
  detail.familyFlows.ipv4 ??= []
  detail.familyFlows.ipv6 ??= []
  return detail
}

function normalizeInterface(item: InterfaceStatus): InterfaceStatus {
  const type = item.type?.trim().toLowerCase()
  return { ...item, category: item.category || (type === 'loopback' ? 'system' : type === 'ether' ? 'physical' : 'logical'), relations: item.relations ?? [] }
}

type MonitorSummaryItem = [string, string | number]

function MonitorSummaryBar(props: { items: MonitorSummaryItem[]; ariaLabel: string }) {
  return (
    <div className="monitor-scope-summary" aria-label={props.ariaLabel}>
      {props.items.map(([label, value]) => (
        <span key={label}>
          <small>{label}</small>
          <strong>{value}</strong>
        </span>
      ))}
    </div>
  )
}

function TerminalScopeSummaryBar({ summary }: { summary: TerminalScopeSummary }) {
  const items: MonitorSummaryItem[] = [
    ['设备', summary.deviceCount],
    ['连接', summary.connectionCount],
    ['↑', formatBits(summary.currentUploadBps)],
    ['↓', formatBits(summary.currentDownloadBps)],
    ['活动累计↑', formatBytes(summary.activeUploadBytes)],
    ['活动累计↓', formatBytes(summary.activeDownloadBytes)],
  ]
  return <MonitorSummaryBar items={items} ariaLabel="终端概览" />
}

function InterfaceScopeSummaryBar({ interfaces }: { interfaces: InterfaceStatus[] }) {
  const physical = interfaces.filter((item) => item.category === 'physical')
  const logical = interfaces.filter((item) => item.category === 'logical')
  const active = interfaces.filter((item) => item.running && !item.disabled).length
  const currentTxBps = interfaces.reduce((sum, item) => sum + item.currentTxBps, 0)
  const currentRxBps = interfaces.reduce((sum, item) => sum + item.currentRxBps, 0)
  const items: MonitorSummaryItem[] = [
    ['接口', interfaces.length],
    ['物理', physical.length],
    ['逻辑', logical.length],
    ['活动', `${active}/${interfaces.length}`],
    ['↑', formatBits(currentTxBps)],
    ['↓', formatBits(currentRxBps)],
  ]
  return <MonitorSummaryBar items={items} ariaLabel="接口概览" />
}

type MonitorTabOption = { value: string; label: string }
type MonitorTabConfig = { value: string; options: MonitorTabOption[]; ariaLabel: string; onChange: (value: string) => void }
type InterfaceCategory = 'physical' | 'logical'

function MonitorPageTabs(props: MonitorTabConfig) {
  return (
    <div className="monitor-page-tabs" role="tablist" aria-label={props.ariaLabel}>
      {props.options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="tab"
          aria-selected={props.value === option.value}
          className={props.value === option.value ? 'active' : ''}
          onClick={() => props.onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

function Icon(props: { name: IconName }) {
  const paths: Record<IconName, React.ReactNode> = {
    overview: <><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></>,
    status: <><circle cx="12" cy="12" r="9"/><path d="m8 12 2.5 2.5L16 9"/></>,
    network: <><circle cx="5" cy="12" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="19" cy="18" r="2"/><path d="m7 11 10-4M7 13l10 4"/></>,
    terminal: <><rect x="3" y="4" width="18" height="15" rx="2"/><path d="M8 22h8M9 9l2 2-2 2m4 0h3"/></>,
    traffic: <><path d="M5 20V10m5 10V4m5 16v-7m5 7V7"/></>,
    policy: <><path d="M12 3 4 7v5c0 5 3.4 8 8 9 4.6-1 8-4 8-9V7l-8-4Z"/><path d="m9 12 2 2 4-4"/></>,
    runtime: <><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m6 14 3-3 3 2 4-5 2 3"/></>,
    route: <><circle cx="6" cy="18" r="2"/><circle cx="18" cy="6" r="2"/><path d="M8 18h3a4 4 0 0 0 4-4v-3a3 3 0 0 1 3-3"/></>,
    settings: <><path d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.5 1.7 1.7 0 0 0-1.9.3l-.1.1A2 2 0 1 1 4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.6-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.9l-.1-.1A2 2 0 1 1 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3H9a1.7 1.7 0 0 0 1-1.6V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.3l.1-.1A2 2 0 1 1 19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9v.1a1.7 1.7 0 0 0 1.6 1h.1a2 2 0 1 1 0 4H21a1.7 1.7 0 0 0-1.6 1Z"/></>,
    refresh: <><path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/></>,
    cpu: <><rect x="7" y="7" width="10" height="10" rx="1"/><path d="M9 1v3m6-3v3M9 20v3m6-3v3M20 9h3m-3 6h3M1 9h3m-3 6h3M10 10h4v4h-4z"/></>,
    memory: <><path d="M4 7h16v10H4zM7 4v3m4-3v3m4-3v3m3-3v3M7 17v3m4-3v3m4-3v3"/></>,
    connections: <><circle cx="6" cy="7" r="3"/><circle cx="18" cy="7" r="3"/><circle cx="12" cy="18" r="3"/><path d="m8.5 9 2 6m5-6-2 6M9 7h6"/></>,
    shield: <><path d="M12 3 4 7v5c0 5 3.4 8 8 9 4.6-1 8-4 8-9V7l-8-4Z"/><path d="m9 12 2 2 4-4"/></>,
    router: <><rect x="3" y="7" width="18" height="11" rx="2"/><path d="M7 12h.01M11 12h.01M15 12h3M8 7V4m8 3V4"/></>,
    storage: <><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v7c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12v7c0 1.7 3.6 3 8 3s8-1.3 8-3v-7"/></>,
    alert: <><path d="M12 3 2.5 20h19L12 3Z"/><path d="M12 9v5m0 3h.01"/></>,
    info: <><circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10h.01"/></>,
    check: <><circle cx="12" cy="12" r="9"/><path d="m8 12 2.5 2.5L16 9"/></>,
    search: <><circle cx="11" cy="11" r="7"/><path d="m16 16 5 5"/></>,
    clear: <><path d="m15 3-7.5 11.5"/><path d="M6 13l5 3-3 5H3l-1-2 4-6Z"/><path d="M4 17h5"/></>,
    eye: <><path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/></>,
    eyeOff: <><path d="m3 3 18 18"/><path d="M10.6 6.2A10.8 10.8 0 0 1 12 6c6.5 0 10 6 10 6a18 18 0 0 1-2.1 2.8M6.5 6.5C3.5 8.3 2 12 2 12s3.5 6 10 6c1.8 0 3.3-.5 4.6-1.2"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/></>,
    palette: <><path d="M12 3a9 9 0 1 0 0 18h1.5a2 2 0 0 0 0-4H12a2 2 0 0 1 0-4h4a5 5 0 0 0 5-5 9 9 0 0 0-9-5Z"/><circle cx="7.5" cy="10" r=".75" fill="currentColor" stroke="none"/><circle cx="10" cy="6.8" r=".75" fill="currentColor" stroke="none"/><circle cx="14" cy="6.8" r=".75" fill="currentColor" stroke="none"/><circle cx="17" cy="10" r=".75" fill="currentColor" stroke="none"/></>,
  }
  return <svg className="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[props.name]}</svg>
}

function NavLabel(props: { icon: IconName; label: string }) { return <span className="nav-label"><Icon name={props.icon} /><span>{props.label}</span></span> }

function relativeUpdateTime(value: string) {
  const timestamp = new Date(value).getTime()
  if (!Number.isFinite(timestamp)) return '尚未成功采集'
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000))
  if (seconds < 10) return '刚刚'
  if (seconds < 60) return `${seconds} 秒前`
  return `${Math.floor(seconds / 60)} 分钟前`
}

function App() {
	const [bootstrap, setBootstrap] = useState<BootstrapResponse | null>(null)
	const [error, setError] = useState<string | null>(null)
	const refresh = async () => {
		try {
			const response = await fetch('/api/bootstrap', { cache: 'no-store' })
			if (!response.ok) throw new Error(`HTTP ${response.status}`)
			setBootstrap(await response.json() as BootstrapResponse)
			setError(null)
		} catch (loadError) { setError(loadError instanceof Error ? loadError.message : '初始化状态读取失败') }
	}
	useEffect(() => {
		void refresh()
		const authenticationRequired = () => void refresh()
		window.addEventListener('rosboard:authentication-required', authenticationRequired)
		const timer = window.setInterval(() => void refresh(), 60_000)
		return () => { window.clearInterval(timer); window.removeEventListener('rosboard:authentication-required', authenticationRequired) }
	}, [])
	if (!bootstrap) return <StartupCard title="Rosboard" description="正在读取初始化状态..." error={error} />
	if (bootstrap.phase === 'needs_admin') return <AdminSetupPage onComplete={() => void refresh()} />
	if (bootstrap.phase === 'needs_login') return <LoginPage onComplete={() => void refresh()} />
	if (bootstrap.phase === 'needs_routeros') return <RouterOSSetupPage onComplete={() => void refresh()} />
	return <PanelApp username={bootstrap.username ?? ''} onAuthenticationChanged={() => void refresh()} />
}

function StartupCard(props: { title: string; description: string; error?: string | null; children?: React.ReactNode; wide?: boolean }) {
	return <main className="setup-shell"><section className={`panel setup-panel auth-panel${props.wide ? ' setup-panel-wide' : ''}`}>
		<div className="setup-brand"><img className="brand-mark" src={rosboardMark} alt="" /><div><h1>{props.title}</h1><p>{props.description}</p></div></div>
		{props.error ? <div className="global-error">{props.error}</div> : null}
		{props.children}
	</section></main>
}

function AdminSetupPage(props: { onComplete: () => void }) {
	const [username, setUsername] = useState('admin')
	const [password, setPassword] = useState('')
	const [confirmation, setConfirmation] = useState('')
	const [error, setError] = useState<string | null>(null)
	const [saving, setSaving] = useState(false)
	return <StartupCard title="创建管理员" description="第一步：设置用于持续登录 Rosboard 的唯一管理员账号。密码至少 4 个字符。" error={error}>
		<form className="settings-form auth-form admin-setup-form" onSubmit={async (event) => { event.preventDefault(); setSaving(true); setError(null); try { await postJSON('/api/setup/admin', { username, password, passwordConfirmation: confirmation }); props.onComplete() } catch (submitError) { setError(submitError instanceof Error ? submitError.message : '管理员创建失败') } finally { setSaving(false) } }}>
			<label className="wide"><span>管理员用户名</span><input required maxLength={64} autoFocus autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} /></label>
			<label><span>密码</span><input required minLength={4} maxLength={128} type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} /></label>
			<label><span>确认密码</span><input required minLength={4} maxLength={128} type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></label>
			<div className="settings-actions wide"><button className="primary-button" disabled={saving || password !== confirmation} type="submit">{saving ? '正在创建...' : '创建管理员并继续'}</button></div>
		</form>
	</StartupCard>
}

function LoginPage(props: { onComplete: () => void }) {
	const [username, setUsername] = useState('')
	const [password, setPassword] = useState('')
	const [error, setError] = useState<string | null>(null)
	const [saving, setSaving] = useState(false)
	return <StartupCard title="登录 Rosboard" description="使用管理员账号继续。" error={error}>
		<form className="settings-form auth-form" onSubmit={async (event) => { event.preventDefault(); setSaving(true); setError(null); try { await postJSON('/api/auth/login', { username, password }); props.onComplete() } catch (submitError) { setError(submitError instanceof Error ? submitError.message : '登录失败') } finally { setSaving(false) } }}>
			<label className="wide"><span>用户名</span><input required autoFocus autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} /></label>
			<label className="wide"><span>密码</span><input required type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} /></label>
			<div className="settings-actions wide"><button className="primary-button" disabled={saving} type="submit">{saving ? '正在登录...' : '登录'}</button></div>
		</form>
	</StartupCard>
}

function RouterOSSetupPage(props: { onComplete: () => void }) {
	const [settings, setSettings] = useState<SettingsResponse | null>(null)
	const [error, setError] = useState<string | null>(null)
	const [stage, setStage] = useState<'choice' | 'editor'>('choice')
	const [editorDeviceID, setEditorDeviceID] = useState('')
	const [editorVersion, setEditorVersion] = useState(0)
	const [message, setMessage] = useState<string | null>(null)
	const [finishing, setFinishing] = useState(false)
	const loadSettings = async () => {
		const response = await fetch('/api/settings', { cache: 'no-store' })
		if (!response.ok) throw new Error(`HTTP ${response.status}`)
		const result = await response.json() as SettingsResponse
		setSettings(result)
		return result
	}
	const onRestartingAction = async (action: () => Promise<void>, onOffline: () => void) => {
		await action()
		try { await waitForPanelRestart(onOffline) } finally { props.onComplete() }
	}
	const finishSetup = async () => {
		setFinishing(true)
		setError(null)
		try {
			await onRestartingAction(() => postJSON('/api/setup/complete', { skipRouterOS: false }).then(() => undefined), () => setError('面板正在启动，恢复后将自动进入面板...'))
		} catch (finishError) {
			setError(finishError instanceof Error ? finishError.message : '完成设置失败')
			setFinishing(false)
		}
	}
	useEffect(() => { void loadSettings().catch((loadError) => setError(loadError instanceof Error ? loadError.message : '设置读取失败')) }, [])
	if (stage === 'choice') return <StartupCard title="开始使用 Rosboard" description="管理员已创建。现在可以添加第一台 RouterOS，也可以稍后再配置。" error={error}>
		<div className="onboarding-choice">
			<button type="button" className="onboarding-choice-card primary-choice" onClick={() => { setEditorDeviceID(''); setEditorVersion((value) => value + 1); setStage('editor') }}><Icon name="router" /><span><strong>添加 ROS 设备</strong><small>测试连接并设置采集接口与本地 CIDR</small></span></button>
			<button type="button" className="onboarding-choice-card" onClick={async () => { try { const hasDevices = Boolean(settings?.devices.length); if (hasDevices) await onRestartingAction(() => postJSON('/api/setup/complete', { skipRouterOS: false }).then(() => undefined), () => setError('面板正在启动，恢复后将自动进入面板...')); else { await postJSON('/api/setup/complete', { skipRouterOS: true }); props.onComplete() } } catch (skipError) { setError(skipError instanceof Error ? skipError.message : '进入面板失败') } }}><Icon name="overview" /><span><strong>{settings?.devices.length ? '进入面板' : '跳过并进入面板'}</strong><small>{settings?.devices.length ? '重启采集服务并进入监控面板' : '稍后可在设备管理中添加 RouterOS'}</small></span></button>
		</div>
	</StartupCard>
	return <StartupCard wide title="添加 RouterOS" description="填写连接信息并测试成功后，再选择采集接口和本地 CIDR。" error={error}>
		{message ? <div className="settings-message" role="status">{message}</div> : null}
		{settings ? <DeviceSettingsPanel key={editorVersion} onboarding initialDeviceID={editorDeviceID} settings={settings} selectedDeviceID="" interfaces={[]} onSaved={async () => { await loadSettings(); setEditorDeviceID(''); setEditorVersion((value) => value + 1); setMessage('设备已保存，面板未重启。可继续添加设备，全部确认后再完成设置。') }} onRestartingAction={onRestartingAction} /> : null}
		<div className="setup-back"><button type="button" className="toolbar-button" onClick={() => setStage('choice')}>返回上一步</button>{settings?.devices.some((device) => !device.archived) ? <button type="button" className="complete-setup-button" disabled={finishing} onClick={() => void finishSetup()}>{finishing ? '正在完成...' : '完成设置并进入面板'}</button> : null}</div>
	</StartupCard>
}

function PanelApp(props: { username: string; onAuthenticationChanged: () => void }) {
  const [panelPreferences, setPanelPreferences] = useState<PanelPreferences>(() => loadPanelPreferences())
  const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
  const [activeView, setActiveView] = useState<ActiveView>(() => pendingRouterOSCleanup() ? 'settings' : panelPreferences.landingView)
  const initialActiveView = useRef(activeView)
  const [query, setQuery] = useState('')
  const [fleetQuery, setFleetQuery] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [settings, setSettings] = useState<SettingsResponse | null>(null)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const [deviceChangesPending, setDeviceChangesPending] = useState(false)
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('connection')
  const [collectionSaving, setCollectionSaving] = useState(false)
  const [collectionMessage, setCollectionMessage] = useState<string | null>(null)
  const [recognitionSaving, setRecognitionSaving] = useState(false)
  const [recognitionMessage, setRecognitionMessage] = useState<string | null>(null)
  const [restartSaving, setRestartSaving] = useState(false)
  const [restartMessage, setRestartMessage] = useState<string | null>(null)
  const [restartPending, setRestartPending] = useState(false)
  const [selectedTerminalID, setSelectedTerminalID] = useState<string | null>(null)
  const [terminalDetail, setTerminalDetail] = useState<TerminalDetail | null>(null)
  const [terminalTab, setTerminalTab] = useState<TerminalTab>('basic')
  const [connectionFamily, setConnectionFamily] = useState<ConnectionFamily>('all')
  const [detailScope, setDetailScope] = useState<TerminalFamily>('all')
  const [editingTerminalID, setEditingTerminalID] = useState<string | null>(null)
  const [customNameDraft, setCustomNameDraft] = useState('')
  const [remarkDraft, setRemarkDraft] = useState('')
  const [savingRemark, setSavingRemark] = useState(false)
  const [terminalFamily, setTerminalFamily] = useState<TerminalFamily>(() => panelPreferences.terminalFamily)
  const [interfaceCategory, setInterfaceCategory] = useState<InterfaceCategory>('physical')
  const [dashboardRefreshMs, setDashboardRefreshMs] = useState(() => panelPreferences.refreshMs)
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [loadWindow, setLoadWindow] = useState('1h')
  const [loadSamples, setLoadSamples] = useState<LoadSample[]>([])
  const [fleetOverview, setFleetOverview] = useState<FleetOverview | null>(null)
  const [devices, setDevices] = useState<DeviceStatus[]>([])
	const [devicesLoaded, setDevicesLoaded] = useState(false)
  const [selectedDeviceID, setSelectedDeviceID] = useState(() => window.localStorage.getItem(selectedDeviceKey) ?? '')
  const [trafficWindow, setTrafficWindow] = useState(() => window.sessionStorage.getItem(trafficWindowKey) ?? '5m')
  const [trafficSamples, setTrafficSamples] = useState<RateSample[]>([])
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [statusExpanded, setStatusExpanded] = useState(false)
  const [settingsExpanded, setSettingsExpanded] = useState(false)
  const [warningsExpanded, setWarningsExpanded] = useState(false)
  const [themeMenuOpen, setThemeMenuOpen] = useState(false)

  const updatePanelPreferences = (next: PanelPreferences) => {
    setPanelPreferences(next)
    savePanelPreferences(next)
  }
  const hasDashboard = dashboard !== null

	const refreshSettingsAfterDeviceSave = async () => {
		const response = await fetch('/api/settings', { cache: 'no-store' })
		if (!response.ok) throw new Error(`HTTP ${response.status}`)
		setSettings(await response.json() as SettingsResponse)
		setSettingsError(null)
		setDeviceChangesPending(true)
	}

  useEffect(() => {
    document.documentElement.dataset.theme = panelPreferences.theme
    document.documentElement.style.colorScheme = panelPreferences.theme === 'dark' ? 'dark' : 'light'
  }, [panelPreferences.theme])

  useEffect(() => {
    if (!themeMenuOpen) return
    const closeOnOutsideClick = (event: MouseEvent) => {
      if (event.target instanceof HTMLElement && !event.target.closest('.theme-control')) setThemeMenuOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setThemeMenuOpen(false)
    }
    document.addEventListener('mousedown', closeOnOutsideClick)
    document.addEventListener('keydown', closeOnEscape)
    return () => {
      document.removeEventListener('mousedown', closeOnOutsideClick)
      document.removeEventListener('keydown', closeOnEscape)
    }
  }, [themeMenuOpen])

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const response = await fetch('/api/devices')
        if (!response.ok) return
        const payload = (await response.json()) as { devices: DeviceStatus[] }
        if (cancelled) return
        const available = (payload.devices ?? []).filter((device) => device.enabled && !device.archived)
        setDevices(payload.devices ?? [])
		setDevicesLoaded(true)
        const selectedAvailable = available.some((device) => device.id === selectedDeviceID)
        if (!selectedAvailable && available[0]) setSelectedDeviceID(available[0].id)
      } catch {
        return
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), 10000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [restartPending, selectedDeviceID])

  useEffect(() => {
    if (activeView !== 'fleet') return
    let cancelled = false
    let refreshing = false
    const refresh = async () => {
      if (refreshing) return
      refreshing = true
      try {
        const response = await fetch('/api/fleet-overview')
        if (!response.ok) throw new Error(`HTTP ${response.status}`)
        const payload = (await response.json()) as FleetOverview
        if (!cancelled) {
          payload.devices ??= []
          setFleetOverview(payload)
          setError(null)
        }
      } catch (refreshError) {
        if (!cancelled && !restartPending) setError(refreshError instanceof Error ? refreshError.message : '设备概览读取失败')
      } finally {
        refreshing = false
      }
    }
    void refresh()
    const timer = dashboardRefreshMs > 0 ? window.setInterval(() => void refresh(), dashboardRefreshMs) : 0
    return () => {
      cancelled = true
      if (timer) window.clearInterval(timer)
    }
  }, [activeView, dashboardRefreshMs, refreshNonce, restartPending])

  useEffect(() => {
    if (!selectedDeviceID) return
    window.localStorage.setItem(selectedDeviceKey, selectedDeviceID)
    setDashboard(null)
    setError(null)
    setSelectedTerminalID(null)
    setTerminalDetail(null)
    setRefreshNonce((value) => value + 1)
  }, [selectedDeviceID])

  const saveCollectionSettings = async (draft: CollectionDraft) => {
    setCollectionSaving(true)
    setCollectionMessage(null)
    setRestartPending(false)
    try {
      await postJSON('/api/settings/collection', draft)
      setCollectionMessage('已保存，面板正在重启并应用新的采集参数，请保持此页面打开...')
      setRestartPending(true)
      await waitForPanelRestart(() => setCollectionMessage('面板正在启动，恢复后将自动刷新...'))
    } catch (saveError) {
      setRestartPending(false)
      setCollectionMessage(saveError instanceof Error ? saveError.message : '采集设置保存失败')
    } finally {
      setCollectionSaving(false)
    }
  }

  const saveRecognitionSettings = async (draft: RecognitionDraft) => {
    setRecognitionSaving(true)
    setRecognitionMessage(null)
    setRestartPending(false)
    try {
      await postJSON('/api/settings/recognition', draft)
      setRecognitionMessage('已保存，面板正在重启并应用识别设置，请保持此页面打开...')
      setRestartPending(true)
      await waitForPanelRestart(() => setRecognitionMessage('面板正在启动，恢复后将自动刷新...'))
    } catch (saveError) {
      setRestartPending(false)
      setRecognitionMessage(saveError instanceof Error ? saveError.message : '识别设置保存失败')
    } finally {
      setRecognitionSaving(false)
    }
  }

  const restartPanel = async () => {
    setRestartSaving(true)
    setRestartMessage(null)
    setRestartPending(false)
    try {
      await postJSON('/api/settings/restart')
      setRestartMessage('面板正在重启，请保持此页面打开...')
      setRestartPending(true)
      await waitForPanelRestart(() => setRestartMessage('面板正在启动，恢复后将自动刷新...'))
    } catch (restartError) {
      setRestartPending(false)
      setRestartMessage(restartError instanceof Error ? restartError.message : '面板重启失败')
      setRestartSaving(false)
    }
  }

  useEffect(() => {
    const heartbeat = () => {
      if (document.visibilityState !== 'visible') return
      void fetch('/api/viewer-heartbeat', { method: 'POST' }).catch(() => undefined)
    }
    const handleVisibilityChange = () => heartbeat()

    heartbeat()
    const timer = window.setInterval(heartbeat, 10000)
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [])

  useEffect(() => {
    if (activeView === initialActiveView.current || activeView === 'fleet' || activeView === 'overview' || activeView === 'settings') return
    setStatusExpanded(true)
  }, [activeView])

  useEffect(() => {
    if (activeView === 'fleet') return
    let cancelled = false
    let refreshing = false

    const refresh = async () => {
      if (refreshing) return
      refreshing = true
      try {
        const response = await fetch(scopedURL('/api/dashboard', selectedDeviceID))
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`)
        }
        const payload = (await response.json()) as DashboardResponse
        payload.overview = normalizeOverview(payload.overview)
        payload.interfaces = (payload.interfaces ?? []).map(normalizeInterface)
        payload.terminals = (payload.terminals ?? []).map(normalizeTerminal)
        payload.capabilities ??= []
        payload.protocols ??= []
        payload.policies ??= []
        payload.routes ??= []
        payload.dhcp ??= { servers: [], pools: [], leases: [] }
        payload.alerts ??= []
        payload.warnings ??= []
        payload.terminalScopeSummaries ??= {} as DashboardResponse['terminalScopeSummaries']
		payload.terminalScope ??= { mode: 'auto', legacy: false, interfaces: [], prefixes: [], warnings: [], overridesApplied: false }
		payload.terminalScope.interfaces ??= []
		payload.terminalScope.prefixes ??= []
		payload.terminalScope.warnings ??= []
		payload.trafficScope ??= { mode: 'auto', legacy: false, interfaces: [], warnings: [], overridesApplied: false }
		payload.trafficScope.interfaces ??= []
		payload.trafficScope.warnings ??= []
        payload.overview.trafficInterfaces ??= []
        payload.overview.chartSamples ??= []
        if (!cancelled) {
          setDashboard((current) => {
            if (!current || new Date(payload.overview.updatedAt).getTime() >= new Date(current.overview.updatedAt).getTime()) return payload
            return { ...payload, overview: current.overview }
          })
          setError(null)
        }
      } catch (refreshError) {
        if (!cancelled && !restartPending) {
          setError(refreshError instanceof Error ? refreshError.message : '读取失败')
        }
      } finally {
        refreshing = false
      }
    }

    refresh()
    const timer = dashboardRefreshMs > 0 ? window.setInterval(refresh, dashboardRefreshMs) : 0
    return () => {
      cancelled = true
      if (timer) window.clearInterval(timer)
    }
  }, [activeView, dashboardRefreshMs, refreshNonce, restartPending, selectedDeviceID])

  useEffect(() => {
    if (activeView !== 'overview' && activeView !== 'resource' || dashboardRefreshMs <= 0) return
    let cancelled = false
    let refreshing = false

    const refreshRealtime = async () => {
      if (refreshing) return
      refreshing = true
      try {
        const response = await fetch(scopedURL('/api/realtime', selectedDeviceID))
        if (!response.ok) throw new Error(`HTTP ${response.status}`)
        const overview = normalizeOverview((await response.json()) as Overview)
        if (!cancelled) {
          setDashboard((current) => current ? { ...current, overview } : current)
          setError(null)
        }
      } catch (refreshError) {
        if (!cancelled && !restartPending) setError(refreshError instanceof Error ? refreshError.message : '读取失败')
      } finally {
        refreshing = false
      }
    }

    refreshRealtime()
    const timer = window.setInterval(refreshRealtime, dashboardRefreshMs)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [activeView, dashboardRefreshMs, refreshNonce, restartPending, selectedDeviceID])

  useEffect(() => {
    if (activeView !== 'terminals') return
    const heartbeat = () => {
      if (document.visibilityState !== 'visible') return
      void fetch(scopedURL('/api/terminal-viewer-heartbeat', selectedDeviceID), { method: 'POST' }).catch(() => undefined)
    }
    heartbeat()
    const timer = window.setInterval(heartbeat, 10_000)
    document.addEventListener('visibilitychange', heartbeat)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', heartbeat)
    }
  }, [activeView, selectedDeviceID])

  useEffect(() => {
    if (!selectedTerminalID) {
      setTerminalDetail(null)
      return
    }

    let cancelled = false
    let refreshing = false
    const load = async () => {
      if (refreshing) return
      refreshing = true
      try {
        const response = await fetch(scopedURL(`/api/terminals/${encodeURIComponent(selectedTerminalID)}`, selectedDeviceID))
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`)
        }
        const payload = normalizeTerminalDetail((await response.json()) as TerminalDetail)
        if (!cancelled) {
          setTerminalDetail(payload)
        }
      } finally {
        refreshing = false
      }
    }

    const handleError = (detailError: unknown) => {
      if (!cancelled && !restartPending) {
        setError(detailError instanceof Error ? detailError.message : '终端详情读取失败')
      }
    }
    load().catch(handleError)
    const timer = dashboardRefreshMs > 0 ? window.setInterval(() => load().catch(handleError), 1000) : 0

    return () => {
      cancelled = true
      if (timer) window.clearInterval(timer)
    }
  }, [dashboardRefreshMs, restartPending, selectedTerminalID, selectedDeviceID])

  useEffect(() => {
    if (activeView !== 'load' && activeView !== 'overview') return
    let cancelled = false
    const load = async () => {
      const response = await fetch(scopedURL(`/api/load?window=${activeView === 'overview' ? trafficWindow : loadWindow}`, selectedDeviceID))
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as { samples: LoadSample[] }
      if (!cancelled) setLoadSamples(payload.samples ?? [])
    }
    load().catch((loadError) => { if (!restartPending) setError(loadError instanceof Error ? loadError.message : '负载历史读取失败') })
    const timer = window.setInterval(() => load().catch(() => undefined), activeView === 'overview' && trafficWindow === '5m' ? 3000 : 30000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [activeView, loadWindow, trafficWindow, selectedDeviceID, refreshNonce, restartPending])

  useEffect(() => {
    if (activeView !== 'overview') return
    let cancelled = false
    const load = async () => {
      const response = await fetch(scopedURL(`/api/traffic-history?window=${trafficWindow}`, selectedDeviceID))
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as { samples: RateSample[] }
      if (!cancelled) setTrafficSamples(payload.samples ?? [])
    }
    window.sessionStorage.setItem(trafficWindowKey, trafficWindow)
    void load().catch((historyError) => { if (!restartPending) setError(historyError instanceof Error ? historyError.message : '流量历史读取失败') })
    const timer = dashboardRefreshMs > 0 ? window.setInterval(() => void load().catch(() => undefined), dashboardRefreshMs) : 0
    return () => {
      cancelled = true
      if (timer) window.clearInterval(timer)
    }
  }, [activeView, dashboardRefreshMs, trafficWindow, selectedDeviceID, refreshNonce, restartPending])

  useEffect(() => {
    if (activeView !== 'settings' && hasDashboard) return
    let cancelled = false
    const load = async () => {
      const response = await fetch('/api/settings')
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as SettingsResponse
      if (!cancelled) {
        setSettings(payload)
        setSettingsError(null)
      }
    }
    load().catch((settingsLoadError) => {
      if (!cancelled && !restartPending) setSettingsError(settingsLoadError instanceof Error ? settingsLoadError.message : '设置读取失败')
    })
    return () => { cancelled = true }
  }, [activeView, hasDashboard, refreshNonce, restartPending])

  const filteredTerminals = useMemo(() => {
    if (!dashboard) {
      return []
    }
    const keyword = query.trim().toLowerCase()
    return dashboard.terminals.filter((terminal) => {
      if (terminalFamily === 'ipv4' && terminal.ipv4.length === 0) return false
      if (terminalFamily === 'ipv6' && terminal.ipv6.length === 0) return false
      if (!keyword) return true
      return (
      [
        terminal.displayName,
        terminal.remark,
        terminal.macAddress,
        terminal.primaryInterface,
        ...terminal.ipv4,
        ...terminal.ipv6,
      ]
        .join(' ')
        .toLowerCase()
        .includes(keyword)
      )
    })
  }, [dashboard, query, terminalFamily])

  const editingTerminal = useMemo(() => {
    if (!dashboard || !editingTerminalID) {
      return null
    }
    return dashboard.terminals.find((terminal) => terminal.id === editingTerminalID) ?? null
  }, [dashboard, editingTerminalID])


  const currentDevice = devices.find((device) => device.id === selectedDeviceID)
  const globalWarnings = Array.from(new Set((dashboard?.warnings ?? []).map((warning) => warning.trim()).filter(Boolean)))
  const alertCount = Math.max(dashboard?.alerts?.length ?? 0, globalWarnings.length)

  if (activeView === 'fleet' && fleetOverview && fleetOverview.totalDevices === 0 && settings) {
	return <EmptyDevicePanel settings={settings} username={props.username} onAuthenticationChanged={props.onAuthenticationChanged} onDeviceSaved={refreshSettingsAfterDeviceSave} />
  }

  if (!dashboard && !(activeView === 'fleet' && fleetOverview)) {
	if (devicesLoaded && (deviceChangesPending || devices.filter((device) => device.enabled && !device.archived).length === 0) && settings) return <EmptyDevicePanel settings={settings} username={props.username} onAuthenticationChanged={props.onAuthenticationChanged} onDeviceSaved={refreshSettingsAfterDeviceSave} />
    return (
      <main className="shell loading-shell">
        <div className="loading-card">
          <img className="brand-mark" src={rosboardMark} alt="" />
          <h1>Rosboard</h1>
          <p>正在读取 RouterOS 数据。</p>
          {error ? <p className="error-text">{error}</p> : null}
        </div>
      </main>
    )
  }

  const detailMode = activeView === 'terminals' && selectedTerminalID && terminalDetail
  const connectionDetailMode = Boolean(detailMode && terminalTab === 'connections')
  const terminalListMode = Boolean(activeView === 'terminals' && !detailMode)
  const statusActive = activeView === 'interfaces' || activeView === 'terminals' || activeView === 'protocols' || activeView === 'policies' || activeView === 'load' || activeView === 'resource' || activeView === 'routes' || activeView === 'dhcp'
  const hideTopbarHeading = activeView === 'interfaces' || activeView === 'settings'
  const monitorTabs: MonitorTabConfig | null = activeView === 'interfaces'
    ? {
        value: interfaceCategory,
        ariaLabel: '接口类型',
        options: [{ value: 'physical', label: '物理接口' }, { value: 'logical', label: '逻辑接口' }],
        onChange: (value) => setInterfaceCategory(value as InterfaceCategory),
      }
    : activeView === 'terminals'
      ? {
          value: terminalFamily,
          ariaLabel: '终端地址族群',
          options: [{ value: 'all', label: '全部' }, { value: 'ipv4', label: 'IPv4' }, { value: 'ipv6', label: 'IPv6' }],
          onChange: (value) => setTerminalFamily(value as TerminalFamily),
        }
      : activeView === 'protocols' || activeView === 'policies'
        ? {
            value: activeView,
            ariaLabel: '流量监控页面',
            options: [{ value: 'protocols', label: '协议统计' }, { value: 'policies', label: '策略统计' }],
            onChange: (value) => { setActiveView(value as ActiveView); setSelectedTerminalID(null) },
          }
        : activeView === 'dhcp' || activeView === 'routes'
          ? {
              value: activeView,
              ariaLabel: '网络服务页面',
              options: [{ value: 'dhcp', label: 'DHCP' }, { value: 'routes', label: '路由 / 分流' }],
              onChange: (value) => { setActiveView(value as ActiveView); setSelectedTerminalID(null) },
            }
          : activeView === 'resource' || activeView === 'load'
            ? {
                value: activeView,
                ariaLabel: '系统运行页面',
                options: [{ value: 'resource', label: '资源监控' }, { value: 'load', label: '负载历史' }],
                onChange: (value) => { setActiveView(value as ActiveView); setSelectedTerminalID(null) },
              }
            : null
  const topbarClassName = detailMode
    ? 'topbar detail-topbar'
    : activeView === 'overview'
      ? 'topbar overview-topbar'
      : activeView === 'fleet'
        ? 'topbar fleet-topbar'
        : monitorTabs
          ? activeView === 'terminals' ? 'topbar terminal-topbar monitor-topbar' : 'topbar monitor-topbar'
          : hideTopbarHeading ? 'topbar headingless-topbar' : 'topbar'
  const settingsSections: Array<{ key: SettingsSection; label: string; icon: IconName }> = [
    { key: 'connection', label: '设备管理', icon: 'router' },
    { key: 'collection', label: '采集设置', icon: 'refresh' },
    { key: 'recognition', label: '识别设置', icon: 'shield' },
    { key: 'ui', label: '界面设置', icon: 'overview' },
    { key: 'account', label: '账号安全', icon: 'shield' },
    { key: 'maintenance', label: '维护设置', icon: 'storage' },
  ]

  return (
    <main className={`${sidebarOpen ? 'shell sidebar-open' : 'shell'}${connectionDetailMode ? ' connection-detail-shell' : ''}`}>
      <button
        type="button"
        className="sidebar-backdrop"
        aria-label="关闭导航"
        onClick={() => setSidebarOpen(false)}
      />
      <aside className="sidebar">
        <div className="brand">
          <img className="brand-mark" src={rosboardMark} alt="" />
          <div className="brand-copy">
            <h1>Rosboard</h1>
            <p>{dashboard?.overview.version || currentDevice?.version || '多设备监控'}</p>
          </div>
        </div>

        <nav className="menu">
          <button
            type="button"
            className={activeView === 'fleet' ? 'menu-item active' : 'menu-item'}
            onClick={() => {
              setActiveView('fleet')
              setSelectedTerminalID(null)
              setSidebarOpen(false)
            }}
          >
            <NavLabel icon="overview" label="仪表台" />
          </button>
          <button
            type="button"
            className={activeView === 'overview' ? 'menu-item active' : 'menu-item'}
            onClick={() => {
              setActiveView('overview')
              setSelectedTerminalID(null)
              setSidebarOpen(false)
            }}
          >
            <NavLabel icon="overview" label="系统概览" />
          </button>

          <div className="menu-group">
            <button
              type="button"
              className={
                statusActive
                  ? 'menu-item active'
                  : 'menu-item'
              }
              aria-expanded={statusExpanded}
              aria-controls="status-monitor-menu"
              onClick={() => setStatusExpanded((value) => !value)}
            >
              <NavLabel icon="status" label="状态监控" />
            </button>
            {statusExpanded ? <div className="submenu" id="status-monitor-menu">
              <button
                type="button"
                className={activeView === 'interfaces' ? 'submenu-item active' : 'submenu-item'}
                onClick={() => {
                  setActiveView('interfaces')
                  setInterfaceCategory('physical')
                  setSelectedTerminalID(null)
                  setSidebarOpen(false)
                }}
              >
                <NavLabel icon="network" label="接口监控" />
              </button>
              <button
                type="button"
                className={activeView === 'terminals' ? 'submenu-item active' : 'submenu-item'}
                onClick={() => {
                  setActiveView('terminals')
                  setTerminalFamily('all')
                  setSelectedTerminalID(null)
                  setSidebarOpen(false)
                }}
              >
                <NavLabel icon="terminal" label="终端监控" />
              </button>
              <button type="button" className={activeView === 'protocols' || activeView === 'policies' ? 'submenu-item active' : 'submenu-item'} onClick={() => { setActiveView('protocols'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="traffic" label="流量监控" /></button>
              <button type="button" className={activeView === 'dhcp' || activeView === 'routes' ? 'submenu-item active' : 'submenu-item'} onClick={() => { setActiveView('dhcp'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="network" label="网络服务" /></button>
              <button type="button" className={activeView === 'resource' || activeView === 'load' ? 'submenu-item active' : 'submenu-item'} onClick={() => { setActiveView('resource'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="runtime" label="系统运行" /></button>
            </div> : null}
          </div>

          <div className="menu-group">
            <button
              type="button"
              className={activeView === 'settings' ? 'menu-item active' : 'menu-item'}
              aria-expanded={settingsExpanded}
              aria-controls="panel-settings-menu"
              onClick={() => {
                const alreadyInSettings = activeView === 'settings'
                setSettingsExpanded((value) => alreadyInSettings ? !value : true)
                setActiveView('settings')
                if (!alreadyInSettings) setSettingsSection('connection')
                setSelectedTerminalID(null)
                setSidebarOpen(false)
              }}
            >
              <NavLabel icon="settings" label="面板设置" />
            </button>
            {settingsExpanded ? <div className="submenu" id="panel-settings-menu">
              {settingsSections.map((section) => (
                <button
                  key={section.key}
                  type="button"
                  className={activeView === 'settings' && settingsSection === section.key ? 'submenu-item active' : 'submenu-item'}
                  onClick={() => {
                    setActiveView('settings')
                    setSettingsSection(section.key)
                    setSelectedTerminalID(null)
                    setSidebarOpen(false)
                  }}
                >
                  <NavLabel icon={section.icon} label={section.label} />
                </button>
              ))}
            </div> : null}
          </div>
        </nav>
        <div className="sidebar-device-card">
          <label htmlFor="global-device-selector">当前设备</label>
          <select id="global-device-selector" value={selectedDeviceID} onChange={(event) => setSelectedDeviceID(event.target.value)}>
            {devices.filter((device) => device.enabled && !device.archived).map((device) => <option key={device.id} value={device.id}>{device.name}</option>)}
          </select>
          <dl>
            <div><dt>连接状态</dt><dd>{currentDevice?.healthy ? '采集正常' : currentDevice?.error ? '连接异常' : '等待采集'}</dd></div>
            <div><dt>ROS版本</dt><dd>{dashboard?.overview.version || currentDevice?.version || '-'}</dd></div>
            <div><dt>运行时间</dt><dd>{dashboard?.overview.uptime || '-'}</dd></div>
          </dl>
        </div>
      </aside>

      <section className={connectionDetailMode ? 'content connection-detail-content' : terminalListMode ? 'content terminal-list-content' : 'content'}>
        <header className={topbarClassName}>
          <div className="topbar-title">
            <button
              type="button"
              className="mobile-menu-button"
              aria-label="打开导航"
              aria-expanded={sidebarOpen}
              onClick={() => setSidebarOpen(true)}
            >
              <span />
            </button>
            {monitorTabs && !detailMode ? (
              <MonitorPageTabs {...monitorTabs} />
            ) : hideTopbarHeading ? null : (
              <div>
                <h2>{detailMode ? '终端详情' : viewTitle(activeView)}</h2>
                <p className="topbar-subtitle">
                  {detailMode
                    ? `状态监控 > 终端监控 > ${detailScope === 'all' ? '全部终端' : detailScope.toUpperCase()}`
                    : activeView === 'fleet'
                      ? '多设备运行概览'
                      : `系统正常 · 更新于 ${formatDateTime(dashboard?.overview.updatedAt ?? '')}`}
                </p>
              </div>
            )}
          </div>
          <div className="topbar-controls">
            {activeView === 'interfaces' && !detailMode && dashboard ? <InterfaceScopeSummaryBar interfaces={dashboard.interfaces ?? []} /> : null}
            {activeView === 'terminals' && !detailMode && dashboard ? (
              <TerminalScopeSummaryBar summary={dashboard.terminalScopeSummaries?.[terminalFamily] ?? emptyTerminalScopeSummary} />
            ) : null}
            {activeView === 'overview' && !detailMode ? (
              <OverviewRangePills value={trafficWindow} onChange={setTrafficWindow} />
            ) : null}
            {activeView !== 'fleet' && globalWarnings.length ? (
              <button type="button" className="system-ok system-alerting global-warning-toggle" aria-expanded={warningsExpanded} aria-controls="global-warning-list" onClick={() => setWarningsExpanded((value) => !value)}><i />{alertCount} 项告警</button>
            ) : activeView !== 'fleet' ? (
              <span className={dashboard?.alerts?.length ? 'system-ok system-alerting' : 'system-ok'}><i />{dashboard?.alerts?.length ? `${dashboard.alerts.length} 项告警` : '系统正常'}</span>
            ) : null}
            {activeView !== 'fleet' ? <span className="last-updated">最后更新 {relativeUpdateTime(dashboard?.overview.updatedAt ?? '')}</span> : null}
            {activeView === 'terminals' && !detailMode ? <input className="search-input terminal-topbar-search-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="备注 / 名称 / IP / MAC" aria-label="搜索终端" /> : null}
            {activeView === 'fleet' ? <input className="search-input fleet-topbar-search-input" value={fleetQuery} onChange={(event) => setFleetQuery(event.target.value)} placeholder="搜索设备名称、型号、版本或 IP" aria-label="搜索设备" /> : null}
            <div className="theme-control">
              <button
                type="button"
                className="theme-button"
                aria-label={`修改主题，当前为${panelThemeOptions.find((option) => option.value === panelPreferences.theme)?.label || '明亮'}`}
                aria-haspopup="menu"
                aria-expanded={themeMenuOpen}
                onClick={() => setThemeMenuOpen((value) => !value)}
              >
                <Icon name="palette" />
                <span>主题</span>
              </button>
              {themeMenuOpen ? (
                <div className="theme-menu" role="menu" aria-label="主题外观">
                  <div className="theme-menu-head"><strong>主题外观</strong><small>即时应用并保存到当前浏览器</small></div>
                  {panelThemeOptions.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      className={panelPreferences.theme === option.value ? 'theme-menu-option active' : 'theme-menu-option'}
                      role="menuitemradio"
                      aria-checked={panelPreferences.theme === option.value}
                      onClick={() => {
                        updatePanelPreferences({ ...panelPreferences, theme: option.value })
                        setThemeMenuOpen(false)
                      }}
                    >
                      <span className={`theme-preview theme-preview-${option.value}`} aria-hidden="true"><i /><i /><i /></span>
                      <span><strong>{option.label}</strong><small>{option.description}</small></span>
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
            <button type="button" className="icon-button" aria-label="立即刷新" onClick={() => setRefreshNonce((value) => value + 1)}><Icon name="refresh" /></button>
            <select value={dashboardRefreshMs} onChange={(event) => setDashboardRefreshMs(Number(event.target.value))} aria-label="全局自动刷新">
              <option value={0}>停止刷新</option><option value={1000}>自动刷新（1 秒）</option><option value={3000}>自动刷新（3 秒）</option><option value={5000}>自动刷新（5 秒）</option><option value={10000}>自动刷新（10 秒）</option>
            </select>
          </div>
        </header>

        {globalWarnings.length && warningsExpanded ? (
          <section className="global-warning-list" id="global-warning-list" aria-label="全局告警详情">
            <div className="global-warning-list-head"><strong>当前告警</strong><button type="button" onClick={() => setWarningsExpanded(false)}>收起</button></div>
            <ul>
              {globalWarnings.map((warning) => <li key={warning}>{warning}</li>)}
            </ul>
          </section>
        ) : null}

        {error ? <div className="global-error">最近一次刷新失败: {error}</div> : null}

        {activeView === 'fleet' && fleetOverview ? (
          <FleetDashboardPage
            overview={fleetOverview}
            query={fleetQuery}
            onOpenDevice={(deviceID, view) => {
              setSelectedDeviceID(deviceID)
              if (view === 'terminals') setTerminalFamily('all')
              setSelectedTerminalID(null)
              setActiveView(view)
              setSidebarOpen(false)
            }}
          />
        ) : null}

        {activeView === 'overview' && dashboard ? (
          <OverviewPage dashboard={dashboard} loadSamples={loadSamples} trafficSamples={trafficSamples} />
        ) : null}

        {activeView === 'interfaces' && dashboard ? (
          <InterfacesPage interfaces={dashboard.interfaces} deviceID={selectedDeviceID} category={interfaceCategory} />
        ) : null}

        {activeView === 'load' && dashboard ? <LoadPage samples={loadSamples} window={loadWindow} onWindowChange={setLoadWindow} /> : null}
        {activeView === 'resource' && dashboard ? <ResourcePage overview={dashboard.overview} /> : null}
        {activeView === 'protocols' && dashboard ? <ProtocolPage protocols={dashboard.protocols ?? []} deviceID={selectedDeviceID} /> : null}
        {activeView === 'policies' && dashboard ? <PolicyPage policies={dashboard.policies ?? []} /> : null}
        {activeView === 'dhcp' && dashboard ? <DHCPPage dhcp={dashboard.dhcp ?? { servers: [], pools: [] }} /> : null}
        {activeView === 'routes' && dashboard ? <RoutesPage routes={dashboard.routes ?? []} /> : null}
        {activeView === 'settings' && dashboard ? (
          <SettingsPage
            settings={settings}
            error={settingsError}
            activeSection={settingsSection}
            preferences={panelPreferences}
            dashboard={dashboard}
            selectedDeviceID={selectedDeviceID}
            collectionSaving={collectionSaving}
            collectionMessage={collectionMessage}
            recognitionSaving={recognitionSaving}
            recognitionMessage={recognitionMessage}
            restartSaving={restartSaving}
            restartMessage={restartMessage}
            onSaveCollection={saveCollectionSettings}
            onSaveRecognition={saveRecognitionSettings}
			onDeviceSaved={refreshSettingsAfterDeviceSave}
			username={props.username}
			onAuthenticationChanged={props.onAuthenticationChanged}
            onSavePreferences={(preferences) => {
              updatePanelPreferences(preferences)
              setDashboardRefreshMs(preferences.refreshMs)
              setTerminalFamily(preferences.terminalFamily)
            }}
            onResetPreferences={() => {
              window.localStorage.removeItem(panelPreferenceKey)
              setPanelPreferences(defaultPanelPreferences)
              setDashboardRefreshMs(defaultPanelPreferences.refreshMs)
              setTerminalFamily(defaultPanelPreferences.terminalFamily)
            }}
            onRestart={restartPanel}
            onRestartingAction={async (action, onOffline) => {
              try {
                setRestartPending(true)
                await action()
                await waitForPanelRestart(onOffline)
              } catch (error) {
                setRestartPending(false)
                throw error
              }
            }}
          />
        ) : null}

        {activeView === 'terminals' && !detailMode && dashboard ? (
          <TerminalsPage
            terminals={filteredTerminals}
            family={terminalFamily}
            query={query}
            refreshMs={dashboardRefreshMs}
            onRefreshMsChange={setDashboardRefreshMs}
            onRefresh={() => setRefreshNonce((value) => value + 1)}
            onOpenDetail={(terminalID) => {
              setSelectedTerminalID(terminalID)
              setTerminalTab('basic')
              setDetailScope(terminalFamily)
              setConnectionFamily(terminalFamily)
            }}
            onOpenRemark={(terminal) => {
              setEditingTerminalID(terminal.id)
              setCustomNameDraft(terminal.customName ?? '')
              setRemarkDraft(terminal.remark ?? '')
            }}
          />
        ) : null}

        {detailMode ? (
          <TerminalDetailPage
            detail={terminalDetail}
            activeTab={terminalTab}
            connectionFamily={connectionFamily}
            scope={detailScope}
            onBack={() => {
              setSelectedTerminalID(null)
              setTerminalDetail(null)
            }}
            onTabChange={setTerminalTab}
            onConnectionFamilyChange={setConnectionFamily}
          />
        ) : null}
      </section>

      {editingTerminal ? (
        <TerminalMetadataModal
          terminal={editingTerminal}
          customName={customNameDraft}
          remark={remarkDraft}
          saving={savingRemark}
          onCustomNameChange={setCustomNameDraft}
          onRemarkChange={setRemarkDraft}
          onClose={() => setEditingTerminalID(null)}
          onSave={async () => {
            setSavingRemark(true)
            try {
              const response = await fetch(
                scopedURL(`/api/terminals/${encodeURIComponent(editingTerminal.id)}/metadata`, selectedDeviceID),
                {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({ customName: customNameDraft, remark: remarkDraft }),
                },
              )
              if (!response.ok) {
                const failure = await response.json().catch(() => null) as { error?: string } | null
                throw new Error(failure?.error || `HTTP ${response.status}`)
              }
              const payload = normalizeTerminalDetail((await response.json()) as TerminalDetail)
              setTerminalDetail(payload)
              setDashboard((previous) =>
                previous
                  ? {
                      ...previous,
                      terminals: previous.terminals.map((terminal) =>
                        terminal.id === payload.terminal.id ? payload.terminal : terminal,
                      ),
                    }
                  : previous,
              )
              setEditingTerminalID(null)
              setError(null)
            } catch (saveError) {
              setError(saveError instanceof Error ? saveError.message : '设备信息保存失败')
            } finally {
              setSavingRemark(false)
            }
          }}
        />
      ) : null}
    </main>
  )
}

function EmptyDevicePanel(props: { settings: SettingsResponse; username: string; onAuthenticationChanged: () => void; onDeviceSaved: (deviceID: string) => Promise<void> }) {
	const [section, setSection] = useState<'overview' | 'interfaces' | 'terminals' | 'devices' | 'account' | 'maintenance'>('overview')
	const [sidebarOpen, setSidebarOpen] = useState(false)
	const label = section === 'overview' ? '系统概览' : section === 'interfaces' ? '接口监控' : section === 'terminals' ? '终端监控' : section === 'devices' ? '设备管理' : section === 'account' ? '账号安全' : '维护设置'
	const hideTopbarHeading = section === 'interfaces'
	const choose = (value: typeof section) => { setSection(value); setSidebarOpen(false) }
	return <main className={sidebarOpen ? 'shell empty-device-shell sidebar-open' : 'shell empty-device-shell'}>
		<button type="button" className="sidebar-backdrop" aria-label="关闭导航" onClick={() => setSidebarOpen(false)} />
		<aside className="sidebar">
			<div className="brand"><img className="brand-mark" src={rosboardMark} alt="" /><div className="brand-copy"><h1>Rosboard</h1><p>尚未连接设备</p></div></div>
			<nav className="menu">
				<button className={section === 'overview' ? 'menu-item active' : 'menu-item'} onClick={() => choose('overview')}><NavLabel icon="overview" label="系统概览" /></button>
				<button className={section === 'interfaces' ? 'menu-item active' : 'menu-item'} onClick={() => choose('interfaces')}><NavLabel icon="network" label="接口监控" /></button>
				<button className={section === 'terminals' ? 'menu-item active' : 'menu-item'} onClick={() => choose('terminals')}><NavLabel icon="terminal" label="终端监控" /></button>
				<button className={section === 'devices' ? 'menu-item active' : 'menu-item'} onClick={() => choose('devices')}><NavLabel icon="router" label="设备管理" /></button>
				<button className={section === 'account' ? 'menu-item active' : 'menu-item'} onClick={() => choose('account')}><NavLabel icon="shield" label="账号安全" /></button>
				<button className={section === 'maintenance' ? 'menu-item active' : 'menu-item'} onClick={() => choose('maintenance')}><NavLabel icon="storage" label="维护设置" /></button>
			</nav>
			<div className="sidebar-device-card"><label>当前设备</label><p>尚未添加 RouterOS</p></div>
		</aside>
		<section className="content"><header className={hideTopbarHeading ? 'topbar headingless-topbar' : 'topbar'}><div className="topbar-title"><button type="button" className="mobile-menu-button" aria-label="打开导航" onClick={() => setSidebarOpen(true)}><span /></button>{hideTopbarHeading ? null : <div><h2>{label}</h2><p className="topbar-subtitle">可随时添加第一台 RouterOS，账号与维护设置始终可用。</p></div>}</div></header>
			{section === 'devices' ? <section className="panel settings-panel"><div className="empty-device-callout"><Icon name="router" /><div><h3>还没有 RouterOS 设备</h3><p>可连续添加设备，全部保存后再统一应用并启动采集。</p></div></div><DeviceSettingsPanel settings={props.settings} selectedDeviceID="" interfaces={[]} onSaved={props.onDeviceSaved} onRestartingAction={async (action, onOffline) => { await action(); await waitForPanelRestart(onOffline) }} /></section> : section === 'account' ? <AccountSettings username={props.username} onAuthenticationChanged={props.onAuthenticationChanged} /> : section === 'maintenance' ? <section className="panel settings-panel"><div className="panel-head"><h3>维护设置</h3></div><FullResetZone onRestartingAction={async (action, onOffline) => { await action(); await waitForPanelRestart(onOffline) }} /></section> : <section className="panel settings-panel empty-monitor-state"><Icon name="router" /><h3>尚未添加设备</h3><p>{label}需要 RouterOS 数据。添加设备后，这里会自动开始显示监控内容。</p><button type="button" className="primary-button" onClick={() => setSection('devices')}>添加 RouterOS 设备</button></section>}
		</section>
	</main>
}

function FullResetZone(props: { onRestartingAction: (action: () => Promise<void>, onOffline: () => void) => Promise<void> }) {
	const [resetting, setResetting] = useState(false)
	const [message, setMessage] = useState<string | null>(null)
	const reset = async () => {
		if (!window.confirm('完全重新初始化会删除管理员、全部设备配置和所有采集历史，且无法撤销。确定继续吗？')) return
		setResetting(true)
		setMessage(null)
		try {
			await props.onRestartingAction(async () => {
				await requestJSON('/api/settings/full-reset', 'POST', { confirmed: true })
				window.localStorage.removeItem(panelPreferenceKey)
				window.localStorage.removeItem(selectedDeviceKey)
				window.sessionStorage.removeItem(trafficWindowKey)
			}, () => setMessage('正在进入全新初始化页面...'))
		} catch (resetError) {
			setResetting(false)
			setMessage(resetError instanceof Error ? resetError.message : '完全重新初始化失败')
		}
	}
	return <>
		<div className="full-reset-zone">
			<div><strong>完全重新初始化</strong><p>删除管理员、所有会话、全部 RouterOS 配置和采集历史，并重新进入首次初始化页面。此操作与“重置界面偏好”不同，且无法撤销。</p></div>
			<button type="button" className="full-reset-button" disabled={resetting} onClick={() => void reset()}>{resetting ? '正在完全重置...' : '完全重新初始化'}</button>
		</div>
		{message ? <div className="settings-message">{message}</div> : null}
	</>
}

function SettingsPage(props: {
  settings: SettingsResponse | null
  error: string | null
  activeSection: SettingsSection
  preferences: PanelPreferences
  dashboard: DashboardResponse
  selectedDeviceID: string
  collectionSaving: boolean
  collectionMessage: string | null
  recognitionSaving: boolean
  recognitionMessage: string | null
  restartSaving: boolean
  restartMessage: string | null
  onSaveCollection: (draft: CollectionDraft) => Promise<void>
  onSaveRecognition: (draft: RecognitionDraft) => Promise<void>
  onDeviceSaved: (deviceID: string) => Promise<void>
  onSavePreferences: (preferences: PanelPreferences) => void
  onResetPreferences: () => void
  onRestart: () => Promise<void>
  onRestartingAction: (action: () => Promise<void>, onOffline: () => void) => Promise<void>
	username: string
	onAuthenticationChanged: () => void
}) {
  const [preferenceDraft, setPreferenceDraft] = useState(props.preferences)
  const [preferenceMessage, setPreferenceMessage] = useState<string | null>(null)
  const [maintenanceMessage, setMaintenanceMessage] = useState<string | null>(null)

  useEffect(() => setPreferenceDraft(props.preferences), [props.preferences])
  useEffect(() => {
    document.documentElement.dataset.theme = preferenceDraft.theme
    document.documentElement.style.colorScheme = preferenceDraft.theme === 'dark' ? 'dark' : 'light'
  }, [preferenceDraft.theme])

  const exportSettings = () => {
    if (!props.settings) return
    const payload = JSON.stringify({
      ...props.settings,
    }, null, 2)
    const url = URL.createObjectURL(new Blob([payload], { type: 'application/json' }))
    const link = document.createElement('a')
    link.href = url
    link.download = 'rosboard-settings.json'
    link.click()
    URL.revokeObjectURL(url)
    setMaintenanceMessage('已导出脱敏设置')
  }

  return (
    <div className="settings-page">
      {props.error ? <div className="global-error">设置读取失败: {props.error}</div> : null}
      {!props.settings && !props.error ? <section className="panel settings-panel">正在读取设置...</section> : null}

      {props.settings && props.activeSection === 'connection' ? (
        <section className="panel settings-panel">
          <div className="panel-head"><h3>设备管理</h3></div>
          <DeviceSettingsPanel settings={props.settings} selectedDeviceID={props.selectedDeviceID} interfaces={props.dashboard.interfaces ?? []} terminalScope={props.dashboard.terminalScope} trafficScope={props.dashboard.trafficScope} onSaved={props.onDeviceSaved} onRestartingAction={props.onRestartingAction} />
          <div className="settings-grid connection-runtime-grid">
            <SettingItem label="当前面板 API 路径" value={props.settings.connection.apiBasePath || '/api'} />
            <SettingItem label="服务监听地址" value={props.settings.connection.listenAddress || '-'} />
            <SettingItem label="API 允许来源" value={formatSettingList(props.settings.connection.allowedCidrs)} />
          </div>
        </section>
      ) : null}

      {props.settings && props.activeSection === 'collection' ? (
        <section className="panel settings-panel">
          <div className="panel-head"><h3>采集设置</h3></div>
          <CollectionSettingsForm settings={props.settings} saving={props.collectionSaving} message={props.collectionMessage} onSave={props.onSaveCollection} />
        </section>
      ) : null}

      {props.settings && props.activeSection === 'recognition' ? (
        <section className="panel settings-panel">
          <div className="panel-head"><h3>识别设置</h3><span>只读读取 MosDNS，不修改 RouterOS 或 MosDNS</span></div>
          <RecognitionSettingsForm settings={props.settings} saving={props.recognitionSaving} message={props.recognitionMessage} onSave={props.onSaveRecognition} />
        </section>
      ) : null}

      {props.activeSection === 'ui' ? (
        <section className="panel settings-panel">
          <div className="panel-head"><h3>界面设置</h3><span>仅保存在当前浏览器</span></div>
          <form className="settings-form interface-settings-form" onSubmit={(event) => {
            event.preventDefault()
            props.onSavePreferences(preferenceDraft)
            setPreferenceMessage('界面设置已保存')
          }}>
            <label>
              <span>默认自动刷新</span>
              <select
                value={preferenceDraft.refreshMs}
                onChange={(event) => setPreferenceDraft((current) => ({ ...current, refreshMs: Number(event.target.value) }))}
              >
                <option value={0}>停止刷新</option><option value={1000}>1 秒刷新</option><option value={3000}>3 秒刷新</option><option value={5000}>5 秒刷新</option><option value={10000}>10 秒刷新</option>
              </select>
            </label>
            <label>
              <span>默认打开页面</span>
              <select
                value={preferenceDraft.landingView}
                onChange={(event) => setPreferenceDraft((current) => ({ ...current, landingView: event.target.value as ActiveView }))}
              >
                {landingViews.map((view) => <option key={view} value={view}>{viewTitle(view)}</option>)}
              </select>
            </label>
            <label>
              <span>默认终端范围</span>
              <select
                value={preferenceDraft.terminalFamily}
                onChange={(event) => setPreferenceDraft((current) => ({ ...current, terminalFamily: event.target.value as TerminalFamily }))}
              >
                <option value="all">全部终端</option><option value="ipv4">IPv4</option><option value="ipv6">IPv6</option>
              </select>
            </label>
            <fieldset className="theme-picker wide">
              <legend>主题</legend>
              {panelThemeOptions.map((option) => (
                <label key={option.value} className={preferenceDraft.theme === option.value ? 'theme-option active' : 'theme-option'}>
                  <input type="radio" name="panel-theme" value={option.value} checked={preferenceDraft.theme === option.value} onChange={() => setPreferenceDraft((current) => ({ ...current, theme: option.value }))} />
                  <span className={`theme-preview theme-preview-${option.value}`} aria-hidden="true"><i /><i /><i /></span>
                  <span><strong>{option.label}</strong><small>{option.description}</small></span>
                </label>
              ))}
            </fieldset>
            <div className="settings-actions wide">
              <button type="submit" className="primary-button">保存界面设置</button>
            </div>
            {preferenceMessage ? <div className="settings-message wide">{preferenceMessage}</div> : null}
          </form>
        </section>
      ) : null}

      {props.settings && props.activeSection === 'maintenance' ? (
        <section className="panel settings-panel">
          <div className="panel-head"><h3>维护设置</h3></div>
          <div className="settings-actions">
            <button type="button" className="toolbar-button" onClick={exportSettings}><Icon name="storage" />导出全部设备脱敏设置</button>
            <button type="button" className="toolbar-button" onClick={() => { props.onResetPreferences(); setPreferenceMessage(null); setMaintenanceMessage('界面偏好已重置') }}><Icon name="clear" />重置界面偏好</button>
            <button type="button" className="toolbar-button" disabled={props.restartSaving} onClick={() => void props.onRestart()}><Icon name="refresh" />{props.restartSaving ? '正在重启...' : '重启面板服务'}</button>
          </div>
          <ArchivedDevices settings={props.settings} onRestartingAction={props.onRestartingAction} />
          {props.restartMessage || maintenanceMessage ? <div className="settings-message">{props.restartMessage || maintenanceMessage}</div> : null}
		  <FullResetZone onRestartingAction={props.onRestartingAction} />
        </section>
      ) : null}

		{props.settings && props.activeSection === 'account' ? <AccountSettings username={props.username} onAuthenticationChanged={props.onAuthenticationChanged} /> : null}
    </div>
  )
}

function AccountSettings(props: { username: string; onAuthenticationChanged: () => void }) {
	const [username, setUsername] = useState(props.username)
	const [password, setPassword] = useState('')
	const [confirmation, setConfirmation] = useState('')
	const [message, setMessage] = useState<string | null>(null)
	const [saving, setSaving] = useState(false)
	const updateCredentials = async (event: React.FormEvent) => {
		event.preventDefault(); setMessage(null); setSaving(true)
		try { await requestJSON('/api/account', 'PUT', { username, password, passwordConfirmation: confirmation }); props.onAuthenticationChanged() }
		catch (error) { setMessage(error instanceof Error ? error.message : '账号保存失败'); setSaving(false) }
	}
	return <section className="panel settings-panel">
		<div className="panel-head"><h3>账号安全</h3></div>
		<form className="settings-form account-credentials-form" onSubmit={updateCredentials}>
			<label><span>管理员用户名</span><input required maxLength={64} value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" /></label>
			<label><span>密码（至少 4 个字符）</span><input required minLength={4} maxLength={128} type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" /></label>
			<label><span>再次输入密码</span><input required minLength={4} maxLength={128} type="password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="new-password" /></label>
			<div className="settings-actions"><button className="primary-button" disabled={saving || password !== confirmation} type="submit">{saving ? '正在保存...' : '保存账号和密码'}</button></div>
		</form>
		{message ? <div className="settings-message" role="status">{message}</div> : null}
		<div className="account-logout-zone"><div><strong>退出登录</strong><p>仅退出当前浏览器，不会修改管理员账号和密码。</p></div><button type="button" className="toolbar-button" onClick={async () => { await postJSON('/api/auth/logout'); props.onAuthenticationChanged() }}>退出登录</button></div>
	</section>
}

function SettingItem(props: { label: string; value: string; wide?: boolean }) {
  return <div className={props.wide ? 'setting-item wide' : 'setting-item'}><span>{props.label}</span><strong>{props.value}</strong></div>
}


function RouterOSCleanupCard(props: { cleanup: RouterOSCleanupResponse; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const copy = async () => {
    setError(null)
    try {
      await copyText(props.cleanup.script)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      setError('复制失败，请手动选择脚本复制。')
    }
  }
  return (
    <section className="routeros-cleanup-card" aria-labelledby="routeros-cleanup-title">
      <div className="routeros-cleanup-head">
        <div>
          <strong id="routeros-cleanup-title">清理 RouterOS 专用账号</strong>
          <p>{props.cleanup.name} · 用户 {props.cleanup.username} · 组 {props.cleanup.groupName}</p>
        </div>
        <button type="button" className="close-button" onClick={props.onClose}>关闭</button>
      </div>
      <p className="routeros-cleanup-warning">只有确定不再恢复此设备时才执行。脚本会删除 rosboard 创建的专用用户；仅当专用组没有其他用户时才删除该组。</p>
      <textarea readOnly value={props.cleanup.script} rows={12} spellCheck={false} aria-label="RouterOS 账号清理脚本" />
      <div className="settings-actions">
        <button type="button" className="toolbar-button" onClick={() => void copy()}>{copied ? '已复制' : '复制清理脚本'}</button>
      </div>
      {error ? <div className="settings-message" role="alert">{error}</div> : null}
    </section>
  )
}

type DeviceDraft = ConnectionDraft & { id: string; name: string; enabled: boolean; trafficInterfaces: string; trafficMode: '' | 'auto'; trafficIncludeInterfaces: string; trafficExcludeInterfaces: string; terminalCidrs: string; includeInterfaces: string; excludeInterfaces: string; includeCidrs: string; excludeCidrs: string }

function deviceDraft(device?: SettingsDevice): DeviceDraft {
  return {
    id: device?.id ?? '', name: device?.name ?? '', enabled: device?.enabled ?? true,
    scheme: device?.scheme === 'https' ? 'https' : 'http', host: device?.host || '10.0.0.1',
    port: device?.port || 80, username: device?.username ?? '', password: '',
    trafficInterfaces: device?.trafficInterfaces.join('\n') ?? '', trafficMode: device?.trafficScope?.mode === 'auto' || !device ? 'auto' : '', trafficIncludeInterfaces: device?.trafficScope?.include_interfaces?.join('\n') ?? '', trafficExcludeInterfaces: device?.trafficScope?.exclude_interfaces?.join('\n') ?? '', terminalCidrs: device?.terminalCidrs.join('\n') ?? '',
    includeInterfaces: device?.terminalScope?.include_interfaces?.join('\n') ?? '', excludeInterfaces: device?.terminalScope?.exclude_interfaces?.join('\n') ?? '', includeCidrs: device?.terminalScope?.include_cidrs?.join('\n') ?? '', excludeCidrs: device?.terminalScope?.exclude_cidrs?.join('\n') ?? '',
  }
}

function DeviceSettingsPanel(props: { settings: SettingsResponse; selectedDeviceID: string; interfaces: InterfaceStatus[]; terminalScope?: TerminalScope; trafficScope?: TrafficScope; onboarding?: boolean; initialDeviceID?: string; onSaved?: (deviceID: string) => Promise<void>; onRestartingAction: (action: () => Promise<void>, onOffline: () => void) => Promise<void> }) {
  const { settings } = props
  const available = settings.devices.filter((device) => !device.archived)
  const [draft, setDraft] = useState<DeviceDraft>(() => deviceDraft(props.initialDeviceID === undefined ? available[0] : available.find((device) => device.id === props.initialDeviceID)))
  const [passwordVisible, setPasswordVisible] = useState(false)
  const [savingAction, setSavingAction] = useState<'save' | null>(null)
  const [message, setMessage] = useState<string | null>(null)
	const [verification, setVerification] = useState<VerificationResponse | null>(null)
	const [scopedDashboard, setScopedDashboard] = useState<Pick<DashboardResponse, 'trafficScope' | 'terminalScope'> | null>(null)
	const [scopeLoading, setScopeLoading] = useState(false)
	const [scopeError, setScopeError] = useState<string | null>(null)
	const [testing, setTesting] = useState(false)
	const saving = savingAction !== null
	// Quick provisioning state
	const [provisioningMode, setProvisioningMode] = useState<'quick' | 'manual'>('quick')
	const [provisioningSession, setProvisioningSession] = useState<ProvisioningSessionResponse | null>(null)
	const [provisioningScriptVisible, setProvisioningScriptVisible] = useState(false)
	const [quickName, setQuickName] = useState('')
	const [quickHost, setQuickHost] = useState('10.0.0.1')
	const [quickScheme, setQuickScheme] = useState<'http' | 'https'>('http')
	const [quickPort, setQuickPort] = useState(80)
	const [quickGenerating, setQuickGenerating] = useState(false)
	const [quickCompleting, setQuickCompleting] = useState(false)
	const [quickCopied, setQuickCopied] = useState(false)
	const [quickError, setQuickError] = useState<string | null>(null)
	const [quickMessage, setQuickMessage] = useState<string | null>(null)
	const [pendingDeviceChanges, setPendingDeviceChanges] = useState(false)
	const [applyingDeviceChanges, setApplyingDeviceChanges] = useState(false)
	const [cleanup, setCleanup] = useState<RouterOSCleanupResponse | null>(() => consumePendingRouterOSCleanup())
	const original = available.find((device) => device.id === draft.id)
	const connectionChanged = !original || original.scheme !== draft.scheme || original.host !== draft.host.trim() || original.port !== draft.port || original.username !== draft.username.trim() || draft.password !== ''
	const verificationRequired = connectionChanged && !verification
  const trafficInterfaces = parseSettingList(draft.trafficInterfaces)
	const trafficScope = verification?.trafficScope ?? scopedDashboard?.trafficScope
	const terminalScope = verification?.terminalScope ?? scopedDashboard?.terminalScope
	const trafficScopeInterfaces = trafficScope?.interfaces ?? []
	const trafficScopeWarnings = trafficScope?.warnings ?? []
	const scopeInterfaces = terminalScope?.interfaces ?? []
	const scopePrefixes = terminalScope?.prefixes ?? []
	const scopeWarnings = terminalScope?.warnings ?? []
	useEffect(() => {
		if (!draft.id) {
			setScopedDashboard(null)
			setScopeLoading(false)
			setScopeError(null)
			return
		}
		let cancelled = false
		setScopedDashboard(null)
		setScopeLoading(true)
		setScopeError(null)
		fetch(`/api/dashboard?device=${encodeURIComponent(draft.id)}`, { cache: 'no-store' })
			.then(async (response) => {
				if (!response.ok) throw new Error(`HTTP ${response.status}`)
				return await response.json() as Pick<DashboardResponse, 'trafficScope' | 'terminalScope'>
			})
			.then((dashboard) => {
				if (!cancelled) setScopedDashboard(dashboard)
			})
			.catch((error) => {
				if (!cancelled) setScopeError(error instanceof Error ? error.message : '读取设备自动识别范围失败')
			})
			.finally(() => {
				if (!cancelled) setScopeLoading(false)
			})
		return () => { cancelled = true }
	}, [draft.id])
  const choose = (device?: SettingsDevice) => {
	setDraft(deviceDraft(device)); setPasswordVisible(false); setMessage(null);
	setVerification(null); setScopedDashboard(null); setScopeError(null);
	setProvisioningSession(null); setProvisioningScriptVisible(false); setQuickError(null); setQuickMessage(null); setQuickCopied(false);
	setProvisioningMode('quick')
	if (!device) {
	  setQuickName('')
	  setQuickHost('10.0.0.1')
	  setQuickScheme('http')
	  setQuickPort(80)
	}
  }
	const testConnection = async () => {
		setTesting(true); setMessage(null); setVerification(null)
		try {
			const response = await requestJSON('/api/devices/test-connection', 'POST', { deviceId: draft.id, scheme: draft.scheme, host: draft.host, port: draft.port, username: draft.username, password: draft.password, trafficScope: { mode: draft.trafficMode === 'auto' ? 'auto' : undefined, include_interfaces: parseSettingList(draft.trafficIncludeInterfaces), exclude_interfaces: parseSettingList(draft.trafficExcludeInterfaces) }, terminalScope: { mode: 'auto', include_interfaces: parseSettingList(draft.includeInterfaces), exclude_interfaces: parseSettingList(draft.excludeInterfaces), include_cidrs: parseSettingList(draft.includeCidrs), exclude_cidrs: parseSettingList(draft.excludeCidrs) } })
			const result = await response.json() as VerificationResponse
			setVerification(result)
			setMessage(result.warnings?.length ? `连接成功，但有 ${result.warnings.length} 项可选能力不可用。` : `连接成功：${result.identity.routerName || result.identity.boardName} ${result.identity.version}`)
		} catch (error) {
			setMessage(error instanceof Error ? error.message : 'RouterOS 连接测试失败')
		} finally { setTesting(false) }
	}
  const request = async (path: string, method: string, body?: unknown) => {
	setSavingAction('save'); setMessage(null)
    try {
	  if (props.onboarding || !draft.id) {
		const response = await requestJSON(path, method, body)
		const result = await response.json() as { id?: string }
		await props.onSaved?.(result.id || draft.id)
		if (!props.onboarding) {
			setDraft(deviceDraft())
			setVerification(null)
			setPendingDeviceChanges(true)
			setMessage('设备已保存，尚未重启采集。可继续添加设备，全部确认后再统一应用。')
		}
		setSavingAction(null)
		return
	  }
      setMessage('已保存，面板正在重启，请保持此页面打开...')
      await props.onRestartingAction(() => requestJSON(path, method, body).then(() => undefined), () => setMessage('面板正在启动，恢复后将自动刷新...'))
    } catch (error) { setMessage(error instanceof Error ? error.message : '设备设置保存失败'); setSavingAction(null) }
  }
	const saveDevice = () => request(draft.id ? `/api/devices/${encodeURIComponent(draft.id)}` : '/api/devices', draft.id ? 'PUT' : 'POST', { ...draft, completeOnboarding: false, deferRestart: props.onboarding || !draft.id, verificationToken: verification?.verificationToken || '', trafficInterfaces: draft.trafficMode === 'auto' ? [] : trafficInterfaces, trafficScope: { mode: draft.trafficMode === 'auto' ? 'auto' : undefined, include_interfaces: parseSettingList(draft.trafficIncludeInterfaces), exclude_interfaces: parseSettingList(draft.trafficExcludeInterfaces) }, terminalCidrs: parseSettingList(draft.terminalCidrs), terminalScope: { mode: 'auto', include_interfaces: parseSettingList(draft.includeInterfaces), exclude_interfaces: parseSettingList(draft.excludeInterfaces), include_cidrs: parseSettingList(draft.includeCidrs), exclude_cidrs: parseSettingList(draft.excludeCidrs) } })
  const isAddingNew = !draft.id
  const showQuick = isAddingNew && provisioningMode === 'quick'
  const showManual = !isAddingNew || provisioningMode === 'manual'

  const changeQuickScheme = (newScheme: 'http' | 'https') => {
    setQuickPort((currentPort) => {
      const previousDefault = quickScheme === 'https' ? 443 : 80
      return currentPort === previousDefault ? (newScheme === 'https' ? 443 : 80) : currentPort
    })
    setQuickScheme(newScheme)
  }

  const generateQuickScript = async () => {
    if (!quickName.trim() || !quickHost.trim()) return
    setQuickGenerating(true)
    setQuickError(null)
	setQuickMessage(null)
    setProvisioningSession(null)
	setProvisioningScriptVisible(false)
    setQuickCopied(false)
    try {
      const response = await requestJSON('/api/device-onboarding/sessions', 'POST', {
        name: quickName.trim(),
        host: quickHost.trim(),
        scheme: quickScheme,
        port: quickPort,
      })
      const result = await response.json() as ProvisioningSessionResponse
      setProvisioningSession(result)
    } catch (error) {
      setQuickError(error instanceof Error ? error.message : '生成接入脚本失败')
    } finally {
      setQuickGenerating(false)
    }
  }

  const completeQuickProvisioning = async () => {
    if (!provisioningSession) return
    setQuickCompleting(true)
    setQuickError(null)
	setQuickMessage(null)
    try {
      const path = `/api/device-onboarding/sessions/${encodeURIComponent(provisioningSession.sessionId)}/complete`
	  const response = await requestJSON(path, 'POST', { completeOnboarding: false, deferRestart: true })
	  const result = await response.json() as ProvisioningCompleteResponse
	  await props.onSaved?.(result.id)
	  setProvisioningSession(null)
	  setProvisioningScriptVisible(false)
	  setQuickCopied(false)
	  setQuickName('')
	  setQuickHost('10.0.0.1')
	  setQuickScheme('http')
	  setQuickPort(80)
	  if (!props.onboarding) {
		setPendingDeviceChanges(true)
		setQuickMessage('设备已保存，尚未重启采集。可继续添加设备，全部确认后再统一应用。')
	  }
    } catch (error) {
      if (error instanceof APIRequestError && error.code === 'provisioning_expired') {
        setProvisioningSession(null)
        setQuickCopied(false)
      }
      setQuickError(error instanceof Error ? error.message : '接入失败')
    } finally {
      setQuickCompleting(false)
    }
  }

	const applyDeviceChanges = async () => {
		setApplyingDeviceChanges(true)
		setQuickError(null)
		setMessage('正在重启面板并应用全部设备配置...')
		setQuickMessage('正在重启面板并应用全部设备配置...')
		try {
			await props.onRestartingAction(() => postJSON('/api/settings/restart').then(() => undefined), () => {
				setMessage('面板正在启动，恢复后将自动刷新...')
				setQuickMessage('面板正在启动，恢复后将自动刷新...')
			})
		} catch (error) {
			const failure = error instanceof Error ? error.message : '应用设备配置失败'
			setMessage(failure)
			setQuickMessage(failure)
			setApplyingDeviceChanges(false)
		}
	}

  const copyScript = async () => {
    if (!provisioningSession) return
    setQuickError(null)
    try {
      await copyText(provisioningSession.script)
      setQuickCopied(true)
      window.setTimeout(() => setQuickCopied(false), 2000)
    } catch {
      setQuickError('复制失败，请手动选择并复制脚本')
    }
  }

  const archiveDevice = async () => {
    if (!draft.id || !window.confirm(`归档设备“${draft.name}”？历史数据将保留。`)) return
    setSavingAction('save')
    setMessage('正在归档设备，面板随后会重启...')
    try {
      await props.onRestartingAction(async () => {
        const response = await requestJSON(`/api/devices/${encodeURIComponent(draft.id)}`, 'DELETE')
        const result = await response.json() as { cleanup?: RouterOSCleanupResponse }
        if (result.cleanup) storePendingRouterOSCleanup(result.cleanup)
      }, () => setMessage('设备已归档，面板正在启动...'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '归档设备失败')
      setSavingAction(null)
    }
  }

  return <div className="device-settings-workspace">
    <div className="device-settings-list">
      <div className="device-settings-list-head"><strong>设备</strong><button type="button" className="icon-button" aria-label="添加设备" title="添加设备" onClick={() => choose()}><span aria-hidden="true">+</span></button></div>
      {available.map((device) => <button key={device.id} type="button" className={draft.id === device.id ? 'device-row active' : 'device-row'} onClick={() => choose(device)}><span><strong>{device.name}</strong><small>{device.host}:{device.port}</small></span><i className={device.enabled ? 'online' : ''} /></button>)}
      {!available.length ? <p className="settings-empty">尚未添加设备</p> : null}
    </div>
    <div className="device-settings-editor">
    {cleanup ? <RouterOSCleanupCard cleanup={cleanup} onClose={() => setCleanup(null)} /> : null}
    {isAddingNew ? (
      <div className="provisioning-mode-toggle" role="tablist" aria-label="接入方式">
        <button type="button" role="tab" aria-selected={provisioningMode === 'quick'} className={provisioningMode === 'quick' ? 'active' : ''} onClick={() => { setProvisioningMode('quick'); setProvisioningSession(null); setProvisioningScriptVisible(false); setQuickError(null); setQuickMessage(null); setQuickCopied(false) }}>快速接入（推荐）</button>
        <button type="button" role="tab" aria-selected={provisioningMode === 'manual'} className={provisioningMode === 'manual' ? 'active' : ''} onClick={() => { setProvisioningMode('manual'); setProvisioningSession(null); setProvisioningScriptVisible(false); setQuickError(null); setQuickMessage(null); setQuickCopied(false) }}>手动添加</button>
      </div>
    ) : null}
    {showQuick ? (
      <div className="provisioning-flow">
        {!provisioningSession ? (
          <form className="settings-form provisioning-form" onSubmit={(event) => { event.preventDefault(); void generateQuickScript() }}>
            <label><span>设备名称</span><input required value={quickName} onChange={(event) => setQuickName(event.target.value)} placeholder="例如：主路由" /></label>
            <label><span>RouterOS IP / 主机名</span><input required value={quickHost} onChange={(event) => setQuickHost(event.target.value)} placeholder="10.0.0.1" /></label>
            <details className="settings-disclosure provisioning-advanced">
              <summary><span><strong>高级设置</strong><small>协议和端口</small></span></summary>
              <div className="settings-disclosure-body">
                <label><span>协议</span>
                  <select value={quickScheme} onChange={(event) => changeQuickScheme(event.target.value === 'https' ? 'https' : 'http')}>
                    <option value="http">HTTP</option>
                    <option value="https">HTTPS</option>
                  </select>
                </label>
                <label><span>REST 端口</span>
                  <input type="number" min={1} max={65535} value={quickPort} onChange={(event) => setQuickPort(Number(event.target.value))} />
                </label>
              </div>
            </details>
            <p className="provisioning-http-notice">默认通过可信局域网内的 HTTP 连接 RouterOS。HTTP 会明文传输登录凭据；如需 HTTPS，请在高级设置中修改。</p>
            <div className="settings-actions">
              <button type="submit" className="primary-button" disabled={quickGenerating || !quickName.trim() || !quickHost.trim()}>{quickGenerating ? '正在生成...' : '生成接入脚本'}</button>
            </div>
			{quickMessage ? <div className="settings-message" role="status">{quickMessage}</div> : null}
            {quickError ? <div className="settings-message" role="alert">{quickError}</div> : null}
          </form>
        ) : (
          <div className="provisioning-step">
            <div className="provisioning-step-card">
			  <div className="provisioning-step-head"><strong>步骤 1：复制脚本</strong><button type="button" className="toolbar-button provisioning-script-toggle" aria-expanded={provisioningScriptVisible} onClick={() => setProvisioningScriptVisible((visible) => !visible)}>{provisioningScriptVisible ? '隐藏脚本' : '查看脚本'}</button></div>
			  <p>直接复制完整脚本，无需查看内容；需要核对时可展开。</p>
			  {provisioningScriptVisible ? <div className="provisioning-script-area">
				<textarea readOnly value={provisioningSession.script} rows={14} spellCheck={false} aria-label="RouterOS 接入脚本" />
			  </div> : null}
			  <button type="button" className="toolbar-button" onClick={() => void copyScript()}>{quickCopied ? '已复制 ✓' : '复制脚本'}</button>
            </div>
            <div className="provisioning-step-card">
              <strong>步骤 2：在 RouterOS 执行</strong>
              <p>打开 WinBox/WebFig/SSH 中的 Terminal，以管理员账号登录，把整段脚本粘贴并执行。看到 "rosboard account ready" 后再返回本页。</p>
            </div>
            <div className="provisioning-step-card">
              <strong>步骤 3：完成接入</strong>
              <p className="provisioning-expiry">脚本将在 {new Date(provisioningSession.expiresAt).toLocaleString('zh-CN')} 过期，请在此之前完成。</p>
              <div className="settings-actions">
				<button type="button" className="primary-button" disabled={quickCompleting} onClick={() => void completeQuickProvisioning()}>{quickCompleting ? '正在验证 RouterOS...' : '验证并保存设备'}</button>
				<button type="button" className="toolbar-button" disabled={quickGenerating} onClick={() => { setProvisioningSession(null); setProvisioningScriptVisible(false); setQuickError(null); setQuickMessage(null); setQuickCopied(false) }}>重新生成脚本</button>
              </div>
            </div>
            {quickError ? <div className="settings-message" role="alert">{quickError}</div> : null}
          </div>
        )}
      </div>
    ) : null}
    {showManual ? (
    <form className="settings-form device-editor" onSubmit={(event) => { event.preventDefault(); void saveDevice() }}>
      <label><span>设备名称</span><input required value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} /></label>
      <label><span>协议</span><select value={draft.scheme} onChange={(event) => { setDraft((current) => ({ ...current, scheme: event.target.value === 'https' ? 'https' : 'http', port: current.port === 80 || current.port === 443 ? (event.target.value === 'https' ? 443 : 80) : current.port })); setVerification(null) }}><option value="http">HTTP</option><option value="https">HTTPS</option></select></label>
      <label><span>IP / 主机名</span><input required value={draft.host} onChange={(event) => { setDraft((current) => ({ ...current, host: event.target.value })); setVerification(null) }} /></label>
      <label><span>REST 端口</span><input type="number" min={1} max={65535} value={draft.port} onChange={(event) => { setDraft((current) => ({ ...current, port: Number(event.target.value) })); setVerification(null) }} /></label>
      <label><span>用户名</span><input required autoComplete="username" value={draft.username} onChange={(event) => { setDraft((current) => ({ ...current, username: event.target.value })); setVerification(null) }} /></label>
      <div className="settings-field"><label htmlFor="device-password">密码</label><span className="password-input"><input id="device-password" required={!draft.id} placeholder={draft.id && original?.passwordSet ? '留空则保持现有密码' : ''} type={passwordVisible ? 'text' : 'password'} autoComplete="current-password" value={draft.password} onChange={(event) => { setDraft((current) => ({ ...current, password: event.target.value })); setVerification(null) }} /><button type="button" className="password-toggle" aria-label={passwordVisible ? '隐藏密码' : '显示密码'} onClick={() => setPasswordVisible((value) => !value)}><Icon name={passwordVisible ? 'eyeOff' : 'eye'} /></button></span></div>
      <div className="settings-actions span-2"><button type="button" className="toolbar-button" disabled={testing || !draft.host.trim() || !draft.username.trim() || (!draft.id && !draft.password)} onClick={() => void testConnection()}>{testing ? '正在测试...' : verification ? '重新测试连接' : '测试 RouterOS 连接'}</button><span className="settings-inline-note">连接成功后将自动识别上网线路和本地终端范围。</span></div>
      {verification ? <div className="verification-summary span-2"><strong>{verification.identity.routerName || verification.identity.boardName} · RouterOS {verification.identity.version || '版本未知'}</strong>{verification.warnings?.map((warning) => <p key={warning.capability}>{warning.message}</p>)}</div> : null}
      <details className="settings-disclosure wide auto-scope-settings">
        <summary>
          <span><strong>自动识别范围</strong><small>系统根据 RouterOS 拓扑自动判断</small></span>
          <small className="settings-disclosure-summary">{scopeLoading ? '正在读取…' : scopeError ? '范围读取失败' : `${trafficScopeInterfaces.length} 条上网线路 · ${scopeInterfaces.filter((item) => item.role === 'lan').length} 个 LAN 接口 · ${scopePrefixes.length} 个网段`}</small>
        </summary>
        <div className="settings-disclosure-body">
          {scopeLoading ? <p className="settings-message">正在读取当前设备的自动识别范围…</p> : null}
          {scopeError ? <p className="settings-message">无法读取当前设备的自动识别范围：{scopeError}</p> : null}
          <div className="scope-overview-grid">
            <section className="scope-result-section" aria-labelledby="traffic-scope-title">
              <div className="scope-result-head"><h4 id="traffic-scope-title">上网流量</h4><small>{trafficScopeInterfaces.length} 条线路</small></div>
              {trafficScope?.legacy ? <p className="scope-legacy-note">当前设备使用旧版手动采集接口配置。</p> : null}
              <div className="scope-result-list">
                {trafficScopeInterfaces.map((item) => <div className="scope-result-row" key={item.name}><span><strong>{item.name}</strong><small>{item.kind} · {item.disabled ? '已禁用' : item.running ? '运行中' : '当前断开，仍作为备用线路保留'}</small></span><small className="scope-result-reason">{(item.reasons ?? []).join('、')}</small></div>)}
                {!trafficScopeInterfaces.length ? <p className="scope-empty">尚未识别上网线路；可在高级覆盖设置中强制纳入。</p> : null}
              </div>
              {trafficScopeWarnings.map((warning) => <p key={warning} className="settings-message">{warning}</p>)}
              {trafficScope?.legacy ? <button type="button" className="toolbar-button" onClick={() => setDraft((current) => ({ ...current, trafficMode: 'auto', trafficInterfaces: '' }))}>恢复自动识别</button> : null}
            </section>
            <section className="scope-result-section" aria-labelledby="terminal-scope-title">
              <div className="scope-result-head"><h4 id="terminal-scope-title">本地终端</h4><small>{scopeInterfaces.filter((item) => item.role === 'lan').length} 个接口 · {scopePrefixes.length} 个网段</small></div>
              {terminalScope?.legacy ? <p className="scope-legacy-note">当前设备使用旧版手动终端网段配置。保存高级覆盖设置后可迁移为自动识别加覆盖模式。</p> : null}
              <div className="terminal-scope-groups">
                <div className="terminal-scope-group"><span>LAN 接口</span>{scopeInterfaces.filter((item) => item.role === 'lan').map((item) => <p key={item.name}><strong>{item.name}</strong><small>{(item.reasons ?? []).join('、')}</small></p>)}{!scopeInterfaces.some((item) => item.role === 'lan') ? <small>尚未识别</small> : null}</div>
                <div className="terminal-scope-group"><span>网段</span>{scopePrefixes.map((item) => <p key={`${item.family}-${item.cidr}`}><strong><i className={`ip-family-badge scope-family ${item.family}`}>{item.family === 'ipv6' ? 'IPv6' : 'IPv4'}</i>{item.cidr}</strong><small>{item.interface || '手动'} · {item.source}</small></p>)}{!scopePrefixes.length ? <small>尚未识别</small> : null}</div>
              </div>
              {scopeWarnings.map((warning) => <p key={warning} className="settings-message">{warning}</p>)}
            </section>
          </div>
        </div>
      </details>
      <details className="settings-disclosure wide advanced-scope-settings">
        <summary>
          <span><strong>高级覆盖设置</strong><small>仅用于特殊网络拓扑</small></span>
          <small className="settings-disclosure-summary">留空即使用自动识别</small>
        </summary>
        <div className="settings-disclosure-body scope-override-body">
          <p className="scope-override-help">仅在自动识别结果不符合实际拓扑时填写；每行一项。</p>
          <section className="scope-override-section" aria-labelledby="traffic-override-title">
            <h4 id="traffic-override-title">流量采集覆盖</h4>
            <div className="scope-override-grid">
              <label><span>强制纳入采集接口</span><textarea rows={2} value={draft.trafficIncludeInterfaces} onChange={(event) => setDraft((current) => ({ ...current, trafficIncludeInterfaces: event.target.value }))} /></label>
              <label><span>强制排除采集接口</span><textarea rows={2} value={draft.trafficExcludeInterfaces} onChange={(event) => setDraft((current) => ({ ...current, trafficExcludeInterfaces: event.target.value }))} /></label>
            </div>
          </section>
          <section className="scope-override-section" aria-labelledby="terminal-override-title">
            <h4 id="terminal-override-title">终端范围覆盖</h4>
            <div className="scope-override-grid">
              <label><span>强制纳入接口</span><textarea rows={2} value={draft.includeInterfaces} onChange={(event) => setDraft((current) => ({ ...current, includeInterfaces: event.target.value }))} /></label>
              <label><span>强制排除接口</span><textarea rows={2} value={draft.excludeInterfaces} onChange={(event) => setDraft((current) => ({ ...current, excludeInterfaces: event.target.value }))} /></label>
              <label><span>额外纳入 CIDR</span><textarea rows={2} value={draft.includeCidrs} placeholder="10.0.0.0/24" onChange={(event) => setDraft((current) => ({ ...current, includeCidrs: event.target.value }))} /></label>
              <label><span>排除 CIDR</span><textarea rows={2} value={draft.excludeCidrs} onChange={(event) => setDraft((current) => ({ ...current, excludeCidrs: event.target.value }))} /></label>
            </div>
          </section>
        </div>
      </details>
      <label className="checkbox-field"><input type="checkbox" checked={draft.enabled} onChange={(event) => setDraft((current) => ({ ...current, enabled: event.target.checked }))} /><span>启用后台采集</span></label>
      <div className="settings-actions span-2"><button type="submit" className="primary-button" disabled={saving || verificationRequired}>{savingAction === 'save' ? '保存中...' : verificationRequired ? '请先测试连接' : props.onboarding ? '保存设备' : draft.id ? '保存设备' : '添加设备'}</button>{draft.id && !props.onboarding ? <button type="button" className="danger-button" disabled={saving} onClick={() => void archiveDevice()}>{saving ? '处理中...' : '归档设备'}</button> : null}</div>
      {message ? <div className="settings-message span-2" role="status">{message}</div> : null}
    </form>
    ) : null}
	{pendingDeviceChanges && !props.onboarding ? <div className="pending-device-actions" role="status"><span>设备配置已保存但尚未应用，可继续添加其他设备。</span><button type="button" className="complete-setup-button" disabled={applyingDeviceChanges} onClick={() => void applyDeviceChanges()}>{applyingDeviceChanges ? '正在应用...' : '应用全部设备并重启'}</button></div> : null}
    </div>
  </div>
}

function ArchivedDevices({ settings, onRestartingAction }: { settings: SettingsResponse; onRestartingAction: (action: () => Promise<void>, onOffline: () => void) => Promise<void> }) {
  const archived = settings.devices.filter((device) => device.archived)
  const [cleanup, setCleanup] = useState<RouterOSCleanupResponse | null>(null)
  const [cleanupLoadingID, setCleanupLoadingID] = useState('')
  const [cleanupError, setCleanupError] = useState<string | null>(null)
  if (!archived.length) return null
  const act = async (device: SettingsDevice, purge: boolean) => {
    const confirmation = purge ? window.prompt(`输入设备名称“${device.name}”以永久清除全部历史数据`) : null
    if (purge && confirmation !== device.name) return
    await onRestartingAction(
      () => requestJSON(`/api/devices/${encodeURIComponent(device.id)}/${purge ? 'data' : 'restore'}`, purge ? 'DELETE' : 'POST', purge ? { confirmation } : undefined).then(() => undefined),
      () => undefined,
    )
  }
  const loadCleanup = async (device: SettingsDevice) => {
    setCleanupLoadingID(device.id)
    setCleanupError(null)
    try {
      const response = await requestJSON(`/api/devices/${encodeURIComponent(device.id)}/cleanup-script`, 'GET')
      setCleanup(await response.json() as RouterOSCleanupResponse)
    } catch (error) {
      setCleanupError(error instanceof Error ? error.message : '读取 RouterOS 清理脚本失败')
    } finally {
      setCleanupLoadingID('')
    }
  }
  return <div className="archived-devices">
    <strong>已归档设备</strong>
    {archived.map((device) => <div className="archived-device-row" key={device.id}>
      <span>{device.name}</span>
      {device.cleanupAvailable ? <button type="button" className="toolbar-button" disabled={cleanupLoadingID === device.id} onClick={() => void loadCleanup(device)}>{cleanupLoadingID === device.id ? '正在生成...' : 'RouterOS 清理脚本'}</button> : null}
      <button type="button" className="toolbar-button" onClick={() => void act(device, false)}>恢复</button>
      <button type="button" className="danger-button" onClick={() => void act(device, true)}>永久清除</button>
    </div>)}
    {cleanup ? <RouterOSCleanupCard cleanup={cleanup} onClose={() => setCleanup(null)} /> : null}
    {cleanupError ? <div className="settings-message" role="alert">{cleanupError}</div> : null}
  </div>
}

function CollectionSettingsForm(props: { settings: SettingsResponse; saving: boolean; message: string | null; onSave: (draft: CollectionDraft) => Promise<void> }) {
  const [draft, setDraft] = useState<CollectionDraft>(() => collectionDraftFromSettings(props.settings))
  useEffect(() => setDraft(collectionDraftFromSettings(props.settings)), [props.settings])
  const numberField = (key: keyof Pick<CollectionDraft, 'pollIntervalSeconds' | 'realtimePollIntervalSeconds' | 'terminalPollIntervalSeconds' | 'sampleRetentionHours'>, label: string, unit: string) => (
    <label>
      <span>{label}</span>
      <span className="number-input"><input type="number" min={1} required value={draft[key]} onChange={(event) => setDraft((current) => ({ ...current, [key]: Number(event.target.value) }))} /><small>{unit}</small></span>
    </label>
  )
  return <form className="settings-form collection-settings-form" onSubmit={(event) => { event.preventDefault(); void props.onSave(draft) }}>
    {numberField('pollIntervalSeconds', '完整采集间隔', '秒')}
    {numberField('realtimePollIntervalSeconds', '实时采集间隔', '秒')}
    {numberField('terminalPollIntervalSeconds', '终端采集间隔', '秒')}
    {numberField('sampleRetentionHours', '采样保留时间', '小时')}
    <div className="settings-actions wide">
      <button type="submit" className="primary-button" disabled={props.saving}>{props.saving ? '保存中...' : '保存并重启采集'}</button>
    </div>
    {props.message ? <div className="settings-message wide">{props.message}</div> : null}
  </form>
}

function RecognitionSettingsForm(props: { settings: SettingsResponse; saving: boolean; message: string | null; onSave: (draft: RecognitionDraft) => Promise<void> }) {
  const [draft, setDraft] = useState<RecognitionDraft>(() => recognitionDraftFromSettings(props.settings))
  useEffect(() => setDraft(recognitionDraftFromSettings(props.settings)), [props.settings])
  return <form className="settings-form recognition-settings-form" onSubmit={(event) => { event.preventDefault(); void props.onSave(draft) }}>
    <fieldset className="settings-fieldset wide">
      <legend>MosDNS DNS 日志对接</legend>
      <label className="checkbox-label"><input type="checkbox" checked={draft.mosdns.enabled} onChange={(event) => setDraft((current) => ({ ...current, mosdns: { ...current.mosdns, enabled: event.target.checked } }))} /><span>启用 MosDNS 解析日志同步</span></label>
      <label><span>MosDNS 地址</span><input type="url" required={draft.mosdns.enabled} value={draft.mosdns.baseUrl} onChange={(event) => setDraft((current) => ({ ...current, mosdns: { ...current.mosdns, baseUrl: event.target.value } }))} placeholder="http://10.0.0.3" /></label>
      <label><span>同步周期</span><span className="number-input"><input type="number" min={1} required value={draft.mosdns.syncIntervalMinutes} onChange={(event) => setDraft((current) => ({ ...current, mosdns: { ...current.mosdns, syncIntervalMinutes: Number(event.target.value) } }))} /><small>分钟</small></span></label>
      <div className="settings-grid connection-runtime-grid">
        <SettingItem label="最近导入" value={`${props.settings.mosdns.lastImported} 条`} />
        <SettingItem label="最近去重" value={`${props.settings.mosdns.lastDuplicates} 条`} />
        <SettingItem label="长期 IP 特征" value={`${props.settings.mosdns.learnedFeatureCount} 条`} />
        <SettingItem label="最近学习" value={props.settings.mosdns.learnedFeatureLastSeen ? formatDateTime(props.settings.mosdns.learnedFeatureLastSeen) : '-'} />
        <SettingItem label="当前水位" value={props.settings.mosdns.watermark ? formatDateTime(props.settings.mosdns.watermark) : '-'} />
        <SettingItem label="运行状态" value={props.settings.mosdns.lastError ? `异常：${props.settings.mosdns.lastError}` : props.settings.mosdns.enabled ? '已启用' : '已关闭'} wide />
      </div>
    </fieldset>
    <fieldset className="settings-fieldset wide">
      <legend>协议特征库</legend>
      <label className="checkbox-label"><input type="checkbox" checked={draft.featureLibrary.enabled} onChange={(event) => setDraft((current) => ({ ...current, featureLibrary: { ...current.featureLibrary, enabled: event.target.checked } }))} /><span>启用域名/IP 应用识别</span></label>
      <label><span>特征库地址</span><input type="url" required={draft.featureLibrary.enabled} value={draft.featureLibrary.sourceUrl} onChange={(event) => setDraft((current) => ({ ...current, featureLibrary: { ...current.featureLibrary, sourceUrl: event.target.value } }))} /></label>
      <label><span>刷新周期</span><span className="number-input"><input type="number" min={1} required value={draft.featureLibrary.refreshIntervalHours} onChange={(event) => setDraft((current) => ({ ...current, featureLibrary: { ...current.featureLibrary, refreshIntervalHours: Number(event.target.value) } }))} /><small>小时</small></span></label>
      <label><span>DNS 匹配窗口</span><span className="number-input"><input type="number" min={1} required value={draft.featureLibrary.matchWindowMinutes} onChange={(event) => setDraft((current) => ({ ...current, featureLibrary: { ...current.featureLibrary, matchWindowMinutes: Number(event.target.value) } }))} /><small>分钟</small></span></label>
      <div className="settings-grid connection-runtime-grid">
        <SettingItem label="已加载规则" value={`${props.settings.featureLibrary.ruleCount} 条`} />
        <SettingItem label="最近成功" value={props.settings.featureLibrary.lastSuccess ? formatDateTime(props.settings.featureLibrary.lastSuccess) : '-'} />
        <SettingItem label="运行状态" value={props.settings.featureLibrary.lastError ? `异常：${props.settings.featureLibrary.lastError}` : props.settings.featureLibrary.enabled ? '已启用' : '已关闭'} wide />
      </div>
    </fieldset>
    <div className="settings-actions wide"><button type="submit" className="primary-button" disabled={props.saving}>{props.saving ? '保存中...' : '保存并重启识别服务'}</button></div>
    {props.message ? <div className="settings-message wide" role="status">{props.message}</div> : null}
  </form>
}

function formatSettingList(values: string[]) {
  return values.length ? values.join(' / ') : '-'
}

function OverviewRangePills(props: { value: string; onChange: (value: string) => void }) {
  return <span className="range-pills topbar-range-pills" aria-label="首页时间范围">{['5m', '1h', '6h', '24h'].map((value) => <button key={value} type="button" className={props.value === value ? 'active' : ''} onClick={() => props.onChange(value)}>{value === '5m' ? '5min' : value}</button>)}</span>
}

function FleetDashboardPage(props: { overview: FleetOverview; query: string; onOpenDevice: (deviceID: string, view: ActiveView) => void }) {
  const [page, setPage] = useState(1)
  const pageSize = 10
  const filtered = useMemo(() => {
    const keyword = props.query.trim().toLowerCase()
    return props.overview.devices.filter((device) => !keyword || [device.name, device.routerName, device.boardName, device.version, device.address].join(' ').toLowerCase().includes(keyword))
  }, [props.overview.devices, props.query])
  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize))
  const currentPage = Math.min(page, pageCount)
  const devices = filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  useEffect(() => setPage(1), [props.query])

  const summaries: Array<{ label: string; value: number; tone: string; icon: IconName }> = [
    { label: '全部设备', value: props.overview.totalDevices, tone: 'blue', icon: 'router' },
    { label: '在线设备', value: props.overview.onlineDevices, tone: 'green', icon: 'check' },
    { label: '离线设备', value: props.overview.offlineDevices, tone: 'red', icon: 'network' },
    { label: '告警设备', value: props.overview.alertDevices, tone: 'amber', icon: 'alert' },
  ]

  return <div className="fleet-dashboard">
    <section className="fleet-summary-grid">
      {summaries.map((summary) => <article className={`fleet-summary fleet-summary-${summary.tone}`} key={summary.label}><span><Icon name={summary.icon} /></span><div><small>{summary.label}</small><strong>{summary.value}<em>台</em></strong></div></article>)}
    </section>

    <FleetOverviewList devices={devices} onOpenDevice={props.onOpenDevice} />

    <div className="fleet-pagination">
      <span>共 {filtered.length} 台</span>
      <span className="toolbar-spacer" />
      <button type="button" disabled={currentPage <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}>上一页</button>
      <strong>{currentPage} / {pageCount}</strong>
      <button type="button" disabled={currentPage >= pageCount} onClick={() => setPage((value) => Math.min(pageCount, value + 1))}>下一页</button>
    </div>
  </div>
}

function FleetOverviewList(props: { devices: FleetDevice[]; onOpenDevice: (deviceID: string, view: ActiveView) => void }) {
  return <section className="fleet-overview-list" aria-label="设备运行列表">
    <div className="fleet-list-head" aria-hidden="true">
      <span>设备信息</span><span>CPU</span><span>内存</span><span>流量速率</span><span>终端数量</span><span>连接数量</span><span>运行时间</span><span />
    </div>
    {props.devices.length ? props.devices.map((device) => <DeviceOverviewRow key={device.id} device={device} onOpen={(view) => props.onOpenDevice(device.id, view)} />) : <div className="empty-row">没有符合条件的设备</div>}
  </section>
}

function DeviceOverviewRow(props: { device: FleetDevice; onOpen: (view: ActiveView) => void }) {
  const { device } = props
  const online = device.state === 'online'
  const status = device.alerting ? 'alerting' : online ? 'online' : 'offline'
  const statusText = device.alerting && online ? '告警' : online ? '正常' : '离线'
  const identity = [device.boardName || device.routerName, device.version].filter(Boolean).join(' · ') || 'RouterOS 设备'
  const updatedAt = device.updatedAt ? relativeUpdateTime(device.updatedAt) : '尚未成功采集'

  return <article className={`device-overview-row ${status}`}>
    <button type="button" className="device-overview-identity device-overview-action" onClick={() => props.onOpen('overview')}>
      <strong>{device.name}</strong>
      <small>{identity}</small>
      <span>
        <em className={status}>{statusText}</em>
        <small>{device.address || '未设置地址'}</small>
      </span>
    </button>

    {online ? <>
      <FleetPercent label="CPU" value={device.cpuLoadPercent} onClick={() => props.onOpen('resource')} ariaLabel={`打开 ${device.name} 的资源监控（CPU）`} />
      <FleetPercent label="内存" value={device.memoryUsedPercent} onClick={() => props.onOpen('resource')} ariaLabel={`打开 ${device.name} 的资源监控（内存）`} />

      <button type="button" className="device-overview-traffic device-overview-action" onClick={() => props.onOpen('overview')} aria-label={`打开 ${device.name} 的系统概览`}>
        <small>流量速率</small>
        <strong className="upload">↑ <span>上传</span><b>{formatBitRate(device.uploadBps)}</b></strong>
        <strong className="download">↓ <span>下载</span><b>{formatBitRate(device.downloadBps)}</b></strong>
      </button>

      <FleetDistribution label="终端数量" total={device.terminalCount} entries={[
        ['在线', device.terminalOnline, 'online'],
        ['未活跃', device.terminalInactive, 'inactive'],
        ['离线', device.terminalOffline, 'offline'],
      ]} onClick={() => props.onOpen('terminals')} ariaLabel={`打开 ${device.name} 的全部终端`} />

      <FleetDistribution label="连接数量" total={device.connectionCount} entries={[
        ['TCP', device.connectionTCP, 'tcp'],
        ['UDP', device.connectionUDP, 'udp'],
        ['其他', device.connectionOther, 'other'],
      ]} onClick={() => props.onOpen('terminals')} ariaLabel={`打开 ${device.name} 的全部终端`} />

      <button type="button" className="device-overview-uptime device-overview-action" onClick={() => props.onOpen('overview')} aria-label={`打开 ${device.name} 的系统概览`}>
        <small>运行时间</small>
        <strong>{device.uptime || '-'}</strong>
        <span>更新于 {updatedAt}</span>
      </button>
    </> : <>
      <span className="device-overview-offline">
        <Icon name="alert" />
        <span><strong>{device.error || '设备离线'}</strong><small>设备暂不可用</small></span>
      </span>
      <button type="button" className="device-overview-uptime device-overview-action" onClick={() => props.onOpen('overview')} aria-label={`打开 ${device.name} 的系统概览`}>
        <small>离线时间</small>
        <strong>{updatedAt}</strong>
        <span>最后采集：{updatedAt}</span>
      </button>
    </>}
    <button type="button" className="device-overview-chevron device-overview-action" onClick={() => props.onOpen('overview')} aria-label={`打开 ${device.name} 的系统概览`}>›</button>
  </article>
}

type FleetDistributionEntry = [label: string, value: number, tone: string]

function FleetDistribution(props: { label: string; total: number; entries: FleetDistributionEntry[]; onClick: () => void; ariaLabel: string }) {
  const total = Math.max(props.total, props.entries.reduce((sum, entry) => sum + entry[1], 0))
  const colors: Record<string, string> = { online: '#16a34a', inactive: '#f59e0b', offline: '#94a3b8', tcp: '#2563eb', udp: '#16a34a', other: '#f59e0b' }
  let offset = 0
  const segments = props.entries.map((entry) => {
    const next = total ? offset + entry[1] / total * 100 : offset
    const segment = `${colors[entry[2]]} ${offset}% ${next}%`
    offset = next
    return segment
  })
  const ring = total ? `conic-gradient(${segments.join(', ')})` : '#e7edf4'
  return <button type="button" className="fleet-distribution device-overview-action" onClick={props.onClick} aria-label={props.ariaLabel}><small>{props.label}</small><span className="fleet-distribution-body"><i style={{ '--fleet-ring': ring } as React.CSSProperties}><b>{total}</b></i><span>{props.entries.map(([label, value, tone]) => <small key={label} className={tone}>● {label}<b>{value}</b></small>)}</span></span></button>
}

function FleetPercent(props: { label: string; value: number; onClick: () => void; ariaLabel: string }) {
  const value = Math.max(0, Math.min(100, props.value))
  return <button type="button" className="fleet-percent device-overview-action" style={{ '--fleet-percent': `${value}%` } as React.CSSProperties} onClick={props.onClick} aria-label={props.ariaLabel}><i>{Math.round(value)}%</i><small>{props.label}</small></button>
}

function OverviewPage(props: { dashboard: DashboardResponse; loadSamples: LoadSample[]; trafficSamples: RateSample[] }) {
  const { overview } = props.dashboard
  const interfaces = props.dashboard.interfaces ?? []
  const alerts = props.dashboard.alerts ?? []
  const samples = props.loadSamples ?? []
  const cpuSamples = samples.length ? samples.map((item) => ({ timestamp: item.timestamp, value: item.cpuLoadPercent })) : [{ timestamp: overview.updatedAt, value: overview.cpuLoadPercent }]
  const memorySamples = samples.length ? samples.map((item) => ({ timestamp: item.timestamp, value: item.memoryUsedPercent })) : [{ timestamp: overview.updatedAt, value: overview.memoryUsedPercent }]
  const terminalSamples = samples.length ? samples.map((item) => ({ timestamp: item.timestamp, value: item.onlineTerminalCount })) : [{ timestamp: overview.updatedAt, value: overview.connectedDeviceCount }]
  const connectionHistory = samples.filter((item) => item.connectionCount >= 0)
  const connectionSamples = connectionHistory.length ? connectionHistory.map((item) => ({ timestamp: item.timestamp, value: item.connectionCount })) : [{ timestamp: overview.updatedAt, value: overview.connectionCount }]
  const cpuValues = cpuSamples.map((item) => item.value)
  const memoryValues = memorySamples.map((item) => item.value)
  const terminalValues = terminalSamples.map((item) => item.value)
  const connectionValues = connectionSamples.map((item) => item.value)
  const terminalStates = overview.terminalStateCounts ?? { online: overview.connectedDeviceCount, inactive: 0, offline: 0 }
  const connectionProtocols = overview.connectionProtocolCounts ?? { tcp: 0, udp: 0, other: overview.connectionCount }
  const interfaceRows = [...interfaces]
    .sort((left, right) => Number(right.running && !right.disabled) - Number(left.running && !left.disabled))
    .slice(0, 7)

  return (
    <div className="overview-dashboard">
      <section className="reference-metric-grid">
        <MetricCard title="CPU 使用率" value={`${overview.cpuLoadPercent}%`} detail="当前负载" icon="cpu" tone="blue" samples={cpuSamples} formatSample={(value) => `${value.toFixed(1)}%`} footerLeft={`平均 ${average(cpuValues).toFixed(0)}%`} footerRight={`峰值 ${maximum(cpuValues).toFixed(0)}%`} progress={overview.cpuLoadPercent} />
        <MetricCard title="内存使用率" value={`${overview.memoryUsedPercent.toFixed(1)}%`} icon="memory" tone="green" samples={memorySamples} formatSample={(value) => `${value.toFixed(1)}%`} footerLeft={`平均 ${average(memoryValues).toFixed(1)}%`} footerRight={`峰值 ${maximum(memoryValues).toFixed(1)}%`} progress={overview.memoryUsedPercent} />
        <MetricCard title="在线终端" value={`${overview.connectedDeviceCount}`} icon="terminal" tone="purple" samples={terminalSamples} formatSample={(value) => `${Math.round(value)} 台`} composition={[{ label: '在线', value: terminalStates.online }, { label: '未活跃', value: terminalStates.inactive }, { label: '离线', value: terminalStates.offline }]} footerLeft={`平均 ${average(terminalValues).toFixed(0)}`} footerRight={`峰值 ${maximum(terminalValues).toFixed(0)}`} />
        <MetricCard title="活动连接" value={overview.connectionCount.toLocaleString()} icon="connections" tone="orange" samples={connectionSamples} formatSample={(value) => Math.round(value).toLocaleString()} composition={[{ label: 'TCP', value: connectionProtocols.tcp }, { label: 'UDP', value: connectionProtocols.udp }, { label: '其他', value: connectionProtocols.other }]} footerLeft={`平均 ${average(connectionValues).toFixed(0)}`} footerRight={`峰值 ${maximum(connectionValues).toFixed(0)}`} />
      </section>

      <section className="overview-main-grid">
        <section className="panel reference-panel traffic-panel">
          <div className="panel-head reference-panel-head">
            <div className="traffic-heading-block"><h3>实时流量</h3><div className="traffic-live-values" aria-live="polite"><span className="download-key">下载（{formatBitRate(overview.downloadBps)}）</span><span className="upload-key">上传（{formatBitRate(overview.uploadBps)}）</span></div></div>
          </div>
          {props.trafficSamples.length ? <Suspense fallback={<div className="realtime-traffic-chart chart-loading">正在加载图表...</div>}><RealtimeTrafficChart samples={props.trafficSamples} /></Suspense> : <div className="empty-chart">暂无速率采样</div>}
        </section>

        <section className="panel reference-panel status-panel">
          <div className="panel-head reference-panel-head"><h3>系统状态</h3></div>
          <SystemStatusList dashboard={props.dashboard} />
        </section>
      </section>

      <section className="overview-bottom-grid">
        <section className="panel reference-panel interface-summary-panel">
          <div className="panel-head reference-panel-head"><h3>接口状态</h3><span>{interfaces.length} 个接口</span></div>
          <div className="table-scroll">
            <table className="overview-interface-table">
              <thead><tr><th>接口名称</th><th>类型</th><th>状态</th><th>链路</th><th>接收速率</th><th>发送速率</th><th>接收流量</th><th>发送流量</th></tr></thead>
              <tbody>{interfaceRows.map((item) => <tr key={item.name}><td><strong>{item.name}</strong></td><td>{item.type || '-'}</td><td><StatusText ok={item.running && !item.disabled} trueText="已连接" falseText={item.disabled ? '已禁用' : '未连接'} /></td><td>{item.linkRate || (item.running ? '运行中' : '-')}</td><td>{formatBits(item.currentRxBps)}</td><td>{formatBits(item.currentTxBps)}</td><td>{formatBytes(item.rxBytes)}</td><td>{formatBytes(item.txBytes)}</td></tr>)}</tbody>
            </table>
          </div>
        </section>

        <section className="panel reference-panel events-panel">
          <div className="panel-head reference-panel-head"><h3>当前告警</h3><span>{alerts.length ? `${alerts.length} 项需要关注` : '全部正常'}</span></div>
          <div className="event-list">{alerts.length ? alerts.slice(0, 5).map((item) => <div className={`event-row event-${item.level === 'error' ? 'danger' : 'warning'}`} key={item.id}><span className="event-icon"><Icon name={item.level === 'error' ? 'alert' : 'info'} /></span><span><strong>{item.source}</strong> · {item.message}</span><small>{formatShortTime(item.timestamp)}</small></div>) : <div className="event-empty"><Icon name="check" /><span>当前没有采集告警</span></div>}</div>
          <div className="event-summary"><span className="danger-dot">严重 {alerts.filter((item) => item.level === 'error').length}</span><span className="warning-dot">警告 {alerts.filter((item) => item.level === 'warning').length}</span></div>
        </section>
      </section>
    </div>
  )
}

type MetricSample = { timestamp: string; value: number }
type MetricCompositionItem = { label: string; value: number }

function MetricCard(props: { title: string; value: string; detail?: string | string[]; icon: IconName; tone: string; samples: MetricSample[]; formatSample: (value: number) => string; composition?: MetricCompositionItem[]; footerLeft: string; footerRight: string; progress?: number }) {
  const detailLines = props.detail ? (Array.isArray(props.detail) ? props.detail : [props.detail]) : []
  return <article className={`metric-card metric-${props.tone}`}>
    <div className="metric-card-heading"><p>{props.title}</p>{props.composition ? <MetricLegend items={props.composition} /> : null}</div>
    <div className="metric-card-main"><div className="metric-value-row"><span className="metric-icon"><Icon name={props.icon} /></span><div className="metric-value"><strong>{props.value}</strong>{detailLines.length ? <small>{detailLines.map((line) => <span key={line}>{line}</span>)}</small> : null}</div></div><div className="metric-card-chart"><MiniSparkline title={props.title} samples={props.samples} format={props.formatSample} /></div></div>
    {typeof props.progress === 'number' ? <div className="metric-progress" aria-label={`${props.title} ${Math.min(100, Math.max(0, props.progress)).toFixed(1)}%`}><i style={{ width: `${Math.min(100, Math.max(0, props.progress))}%` }} /></div> : props.composition ? <MetricComposition items={props.composition} /> : null}
    <footer><span>{props.footerLeft}</span><span>{props.footerRight}</span></footer>
  </article>
}

function MetricLegend(props: { items: MetricCompositionItem[] }) {
  return <span className="metric-legend" aria-label={props.items.map((item) => item.label).join('、')}>{props.items.map((item, index) => <span key={item.label} className={`metric-part-${index}`}><i />{item.label}</span>)}</span>
}

function MetricComposition(props: { items: MetricCompositionItem[] }) {
  const total = props.items.reduce((sum, item) => sum + Math.max(0, item.value), 0)
  const label = total ? props.items.map((item) => `${item.label} ${item.value}`).join('，') : '暂无构成数据'
  return <div className={`metric-composition${total ? '' : ' empty'}`} role="img" aria-label={label}>
    {total ? props.items.map((item, index) => {
      const value = Math.max(0, item.value)
      if (!value) return null
      const percent = value / total * 100
      return <span key={item.label} className={`metric-composition-part metric-part-${index}`} style={{ width: `${percent}%` }} tabIndex={0} aria-label={`${item.label} ${value.toLocaleString()}，占比 ${percent.toFixed(1)}%`} data-tooltip={`${item.label}：${value.toLocaleString()}（${percent.toFixed(1)}%）`} />
    }) : null}
  </div>
}

function metricSampleTime(timestamp: string) {
  const date = new Date(timestamp)
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function MiniSparkline(props: { title: string; samples: MetricSample[]; format: (value: number) => string }) {
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const width = 116; const height = 34; const values = props.samples.map((sample) => sample.value); const max = Math.max(1, ...values); const min = Math.min(...values); const range = Math.max(1, max - min)
  const coordinates = values.map((value, index) => ({ x: index * width / Math.max(1, values.length - 1), y: height - 3 - (value - min) / range * (height - 6) }))
  const points = coordinates.map((point) => `${point.x},${point.y}`).join(' ')
  const active = activeIndex === null ? null : coordinates[activeIndex]
  const sample = activeIndex === null ? null : props.samples[activeIndex]
  return <div className="mini-sparkline-wrap" role="img" aria-label={`${props.title}历史趋势`} onPointerMove={(event) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    const ratio = bounds.width ? (event.clientX - bounds.left) / bounds.width : 0
    setActiveIndex(Math.max(0, Math.min(props.samples.length - 1, Math.round(ratio * Math.max(0, props.samples.length - 1)))))
  }} onPointerLeave={() => setActiveIndex(null)}>
    <svg className="mini-sparkline" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" aria-hidden="true"><polyline points={points} />{active ? <line className="mini-sparkline-pointer" x1={active.x} x2={active.x} y1={1} y2={height - 1} /> : null}</svg>
    {active && sample ? <><i className="mini-sparkline-point" style={{ left: `${active.x / width * 100}%`, top: `${active.y / height * 100}%` }} /><span className={`metric-tooltip${active.x > width / 2 ? ' align-right' : ''}`} style={{ left: `${active.x / width * 100}%` }}><small>时间：{metricSampleTime(sample.timestamp)}</small><strong>{props.title}：{props.format(sample.value)}</strong></span></> : null}
  </div>
}

function SystemStatusList(props: { dashboard: DashboardResponse }) {
  const { overview } = props.dashboard
  const interfaces = props.dashboard.interfaces ?? []
  const activeInterfaces = interfaces.filter((item) => item.running && !item.disabled).length
  const updatedAt = new Date(overview.updatedAt)
  const freshnessSeconds = Math.max(0, (Date.now() - updatedAt.getTime()) / 1000)
  const fresh = Number.isFinite(freshnessSeconds) && freshnessSeconds <= 30
  const rows = [
    { icon: 'runtime' as IconName, label: '运行时间', value: overview.uptime || '-', ok: Boolean(overview.uptime) },
    { icon: 'router' as IconName, label: 'RouterOS 版本', value: overview.version || '-', ok: Boolean(overview.version) },
    { icon: 'refresh' as IconName, label: '最后成功采集', value: Number.isNaN(updatedAt.getTime()) ? '-' : formatDateTime(overview.updatedAt), ok: fresh },
    { icon: 'network' as IconName, label: '活动接口', value: `${activeInterfaces} / ${interfaces.length}`, ok: activeInterfaces > 0 },
    { icon: 'storage' as IconName, label: '存储使用率', value: overview.storageTotalBytes ? `${overview.storageUsedPercent.toFixed(1)}%` : '-', ok: !overview.storageTotalBytes || overview.storageUsedPercent < 85 },
    { icon: 'shield' as IconName, label: '数据新鲜度', value: Number.isFinite(freshnessSeconds) ? relativeUpdateTime(overview.updatedAt) : '-', ok: fresh },
  ]
  return <div className="system-status-list">{rows.map((row) => <div className="system-status-row" key={row.label}><span className="status-row-icon"><Icon name={row.icon} /></span><span>{row.label}</span><strong>{row.value}</strong><StatusText ok={row.ok} trueText="正常" falseText="注意" /></div>)}</div>
}

function StatusText(props: { ok: boolean; trueText: string; falseText: string }) { return <span className={props.ok ? 'status-text status-good' : 'status-text status-bad'}><i />{props.ok ? props.trueText : props.falseText}</span> }

function average(values: number[]) { return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : 0 }
function maximum(values: number[]) { return values.length ? Math.max(...values) : 0 }

function InterfacesPage(props: { interfaces: InterfaceStatus[]; deviceID: string; category: InterfaceCategory }) {
  const [selected, setSelected] = useState<string | null>(null)
  const [detail, setDetail] = useState<InterfaceDetail | null>(null)
  useEffect(() => {
    if (!selected) { setDetail(null); return }
    let cancelled = false
    const load = async () => {
      const response = await fetch(scopedURL(`/api/interfaces/${encodeURIComponent(selected)}`, props.deviceID))
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as InterfaceDetail
      payload.interface = normalizeInterface(payload.interface)
      if (!cancelled) setDetail(payload)
    }
    load().catch(() => undefined)
    const timer = window.setInterval(() => load().catch(() => undefined), 5000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [selected, props.deviceID])
  useEffect(() => setSelected(null), [props.category])
  const physical = props.interfaces.filter((item) => item.category === 'physical').sort((left, right) => {
    const rank = (item: InterfaceStatus) => item.disabled ? 2 : item.running ? 1 : 0
    return rank(left) - rank(right) || left.name.localeCompare(right.name, 'zh-CN', { numeric: true })
  })
  const logical = props.interfaces.filter((item) => item.category === 'logical').sort((left, right) => left.name.localeCompare(right.name, 'zh-CN', { numeric: true }))
  const system = props.interfaces.filter((item) => item.category === 'system').sort((left, right) => left.name.localeCompare(right.name, 'zh-CN', { numeric: true }))
  const selectedItems = props.category === 'physical' ? physical : logical
  const selectedLabel = props.category === 'physical' ? '物理接口' : '逻辑接口'
  const selectedActiveCount = selectedItems.filter((item) => item.running && !item.disabled).length
  const interfaceState = (item: InterfaceStatus) => item.disabled ? '已禁用' : item.running ? (item.category === 'physical' ? '在线' : '运行中') : 'Down'
  const relationLabel = (kind: InterfaceStatus['relations'][number]['kind']) => ({ carrier: '承载接口', parent: '父接口', bridge: '所属 Bridge', member: '成员接口' })[kind] ?? kind
  const table = (items: InterfaceStatus[]) => <div className="table-scroll">
    <table className="data-table interface-table">
      <thead><tr><th>接口</th><th>类型</th><th>关联接口</th><th>地址 / MAC</th><th>状态</th><th>上行速率</th><th>下行速率</th><th>累计上行</th><th>累计下行</th><th>MTU</th><th>掉线次数</th><th>错误 / 丢包</th></tr></thead>
      <tbody>{items.length ? items.map((item) => <tr key={item.name} className={!item.disabled && !item.running ? 'interface-row-down' : ''}>
        <td><button type="button" className="link-button" onClick={() => setSelected(item.name)}>{item.name}</button></td>
        <td>{item.type}</td>
        <td>{item.relations?.length ? <div className="interface-relations">{item.relations.map((relation) => <span key={`${relation.kind}-${relation.interface}`}>{relationLabel(relation.kind)}：{relation.interface}</span>)}</div> : '-'}</td>
        <td><div className="address-stack"><span>{item.addresses?.join(' / ') || '-'}</span><span className="muted-text">{item.macAddress || '-'}</span></div></td>
        <td><span className={`interface-state interface-state-${item.disabled ? 'disabled' : item.running ? 'online' : 'down'}`}>{interfaceState(item)}</span></td>
        <td>{formatBits(item.currentTxBps)}</td><td>{formatBits(item.currentRxBps)}</td><td>{formatBytes(item.txBytes)}</td><td>{formatBytes(item.rxBytes)}</td>
        <td>{item.actualMtu || '-'}</td><td>{item.linkDowns}</td><td>{item.rxErrors + item.txErrors} / {item.rxDrops + item.txDrops}</td>
      </tr>) : <tr><td colSpan={12} className="empty-row">暂无接口</td></tr>}</tbody>
    </table>
  </div>
  return (
    <div className="page-grid monitor-page">
    {detail ? <section className="panel interface-detail"><div className="panel-head"><h3>{detail.interface.name} 接口详情</h3><button type="button" className="close-button" onClick={() => setSelected(null)}>关闭</button></div><div className="detail-summary"><DetailSummary label="状态" value={interfaceState(detail.interface)} /><DetailSummary label="地址" value={detail.interface.addresses?.join(' / ') || '-'} /><DetailSummary label="MAC" value={detail.interface.macAddress || '-'} /><DetailSummary label="协商速率" value={detail.interface.linkRate ? `${detail.interface.linkRate}${detail.interface.fullDuplex ? ' / 全双工' : ''}` : '-'} /><DetailSummary label="当前上行" value={formatBits(detail.interface.currentTxBps)} /><DetailSummary label="当前下行" value={formatBits(detail.interface.currentRxBps)} /><DetailSummary label="收 / 发包" value={`${detail.interface.rxPackets} / ${detail.interface.txPackets}`} /><DetailSummary label="错误 / 丢包" value={`${detail.interface.rxErrors + detail.interface.txErrors} / ${detail.interface.rxDrops + detail.interface.txDrops}`} /></div>{detail.samples.length ? <Suspense fallback={<div className="realtime-traffic-chart chart-loading">正在加载图表...</div>}><RealtimeTrafficChart samples={detail.samples} ariaLabel={`${detail.interface.name} 接口上传和下载速率趋势`} /></Suspense> : <div className="empty-chart">暂无速率采样</div>}</section> : null}
    <section className="panel compact-panel interface-section"><div className="data-toolbar interface-toolbar"><strong>{selectedLabel}</strong><span className="result-count">{selectedItems.length} 个 · 活动 {selectedActiveCount}</span><span className="toolbar-spacer" /><span>共 {props.interfaces.length} 个接口</span></div>{table(selectedItems)}</section>
    {system.length ? <details className="panel compact-panel interface-system-section"><summary>系统接口 <span>{system.length} 个</span></summary>{table(system)}</details> : null}
    </div>
  )
}

type ResourceUsage = { total: number | null; free: number | null; used: number | null; percent: number | null }
type ResourceHealth = 'normal' | 'warning' | 'critical' | 'unavailable'

function parseResourceNumber(value: string) {
  const parsed = Number.parseInt(value, 10)
  return value.trim() && Number.isFinite(parsed) ? parsed : null
}

function resourceUsage(totalValue: string, freeValue: string): ResourceUsage {
  const total = parseResourceNumber(totalValue)
  const free = parseResourceNumber(freeValue)
  if (total === null || free === null || total <= 0 || free < 0 || free > total) return { total, free, used: null, percent: null }
  const used = total - free
  return { total, free, used, percent: used / total * 100 }
}

function formatResourceBytes(value: number | null) {
  return value === null || value < 0 ? '-' : formatBytes(value)
}

function formatResourcePercent(value: string) {
  const parsed = parseResourceNumber(value)
  return parsed === null ? '-' : `${parsed}%`
}

function formatResourceCount(value: string) {
  const parsed = parseResourceNumber(value)
  return parsed === null ? '-' : parsed.toLocaleString('zh-CN')
}

function resourceHealth(percent: number | null): ResourceHealth {
  if (percent === null) return 'unavailable'
  if (percent >= 95) return 'critical'
  if (percent >= 85) return 'warning'
  return 'normal'
}

function resourceHealthText(value: ResourceHealth) {
  if (value === 'critical') return '严重'
  if (value === 'warning') return '注意'
  if (value === 'normal') return '正常'
  return '不可用'
}

function ResourceDetails(props: { items: Array<[string, string]> }) {
  return <div className="resource-details">{props.items.map(([label, value]) => <div key={label}><span>{label}</span><strong>{value}</strong></div>)}</div>
}

function ResourceCard(props: { title: string; description: string; icon: IconName; tone: string; health: ResourceHealth; className?: string; children: React.ReactNode }) {
  return <section className={`panel resource-card resource-${props.tone}${props.className ? ` ${props.className}` : ''}`}>
    <div className="panel-head resource-card-head"><div><h3><Icon name={props.icon} />{props.title}</h3><span>{props.description}</span></div><span className={`resource-health resource-health-${props.health}`}>{resourceHealthText(props.health)}</span></div>
    {props.children}
  </section>
}

function ResourceUsageMeter(props: { label: string; percent: number | null; health: ResourceHealth }) {
  const width = props.percent === null ? 0 : Math.min(100, Math.max(0, props.percent))
  return <div className="resource-usage">
    <div><span>{props.label}</span><strong>{props.percent === null ? '-' : `${props.percent.toFixed(1)}%`}</strong></div>
    <div className={`resource-meter resource-meter-${props.health}`} aria-label={`${props.label} ${props.percent === null ? '不可用' : `${props.percent.toFixed(1)}%`}`}><i style={{ width: `${width}%` }} /></div>
  </div>
}

function ResourceCPUList(props: { items: SystemResource['cpuCores'] }) {
  return <div className="resource-table-wrap">
    <table className="resource-table">
      <thead><tr><th>核心</th><th>总占用</th><th>IRQ</th><th>磁盘</th></tr></thead>
      <tbody>{props.items.length ? props.items.map((item, index) => <tr key={`${item.cpu}-${index}`}><td>CPU {item.cpu || index}</td><td>{formatResourcePercent(item.load)}</td><td>{formatResourcePercent(item.irq)}</td><td>{formatResourcePercent(item.disk)}</td></tr>) : <tr><td colSpan={4} className="resource-empty">暂无逐核数据</td></tr>}</tbody>
    </table>
  </div>
}

function ResourceIRQList(props: { items: SystemResource['irqs'] }) {
  return <div className="resource-table-wrap">
    <table className="resource-table">
      <thead><tr><th>IRQ</th><th>CPU</th><th>活动 CPU</th><th>次数</th><th>用户</th></tr></thead>
      <tbody>{props.items.length ? props.items.map((item, index) => <tr key={`${item.irq}-${index}`}><td>{item.irq || '-'}</td><td>{item.cpu || '-'}</td><td>{item.activeCpu || '-'}</td><td>{formatResourceCount(item.count)}</td><td title={item.users || undefined}>{item.users || '-'}</td></tr>) : <tr><td colSpan={5} className="resource-empty">暂无 IRQ 数据</td></tr>}</tbody>
    </table>
  </div>
}

function ResourceHardwareList(props: { items: SystemResource['hardware'] }) {
  return <div className="resource-table-wrap resource-hardware-table-wrap">
    <table className="resource-table resource-hardware-table">
      <thead><tr><th>名称</th><th>类型</th><th>厂商</th><th>位置</th><th>父设备</th><th>速度</th><th>端口</th><th>USB</th><th>序列号</th><th>厂商 ID</th><th>设备 ID</th><th>所有者</th><th>类别</th><th>IRQ</th><th>设备路径</th></tr></thead>
      <tbody>{props.items.length ? props.items.map((item, index) => <tr key={`${item.name}-${item.devicePath}-${index}`}><td>{item.name || '-'}</td><td>{item.type || '-'}</td><td>{item.vendor || '-'}</td><td>{item.location || '-'}</td><td>{item.parent || '-'}</td><td>{item.speed || '-'}</td><td>{item.ports || '-'}</td><td>{item.usbVersion || '-'}</td><td>{item.serialNumber || '-'}</td><td>{item.vendorId || '-'}</td><td>{item.deviceId || '-'}</td><td>{item.owner || '-'}</td><td>{item.category || '-'}</td><td>{item.irq || '-'}</td><td title={item.devicePath || undefined}>{item.devicePath || '-'}</td></tr>) : <tr><td colSpan={15} className="resource-empty">暂无硬件数据</td></tr>}</tbody>
    </table>
  </div>
}

function ResourcePage(props: { overview: Overview }) {
  const resource = props.overview.systemResource ?? emptySystemResource
  const cpuLoad = parseResourceNumber(resource.cpuLoad)
  const memory = resourceUsage(resource.totalMemory, resource.freeMemory)
  const storage = resourceUsage(resource.totalHddSpace, resource.freeHddSpace)
  const cpuHealth = resourceHealth(cpuLoad)
  const memoryHealth = resourceHealth(memory.percent)
  const storageHealth = resourceHealth(storage.percent)

  return <div className="resource-page">
    <section className="resource-grid">
      <ResourceCard title="CPU" description="总负载与逐核占用" icon="cpu" tone="blue" health={cpuHealth} className="resource-card-cpu">
        <div className="resource-primary"><strong>{cpuLoad === null ? '-' : `${cpuLoad}%`}</strong><span>当前总负载</span></div>
        <ResourceDetails
          items={[
            ["CPU 型号", resource.cpu || '-'],
            ["CPU 核心数", resource.cpuCount || '-'],
            ["CPU 频率", resource.cpuFrequency || '-'],
          ]}
        />
        <ResourceCPUList items={resource.cpuCores} />
      </ResourceCard>
      <ResourceCard title="内存" description="RouterOS system resource" icon="memory" tone="green" health={memoryHealth} className="resource-card-memory">
        <ResourceUsageMeter label="内存使用率" percent={memory.percent} health={memoryHealth} />
        <ResourceDetails items={[["总内存", formatResourceBytes(memory.total)], ["已用内存", formatResourceBytes(memory.used)], ["空闲内存", formatResourceBytes(memory.free)]]} />
      </ResourceCard>
      <ResourceCard title="存储" description="RouterOS system resource" icon="storage" tone="orange" health={storageHealth} className="resource-card-storage">
        <ResourceUsageMeter label="存储使用率" percent={storage.percent} health={storageHealth} />
        <ResourceDetails items={[["总硬盘空间", formatResourceBytes(storage.total)], ["已用硬盘空间", formatResourceBytes(storage.used)], ["空闲硬盘空间", formatResourceBytes(storage.free)], ["坏块", resource.badBlocks || '-'], ["重启后写入", resource.writeSectSinceReboot || '-'], ["累计写入", resource.writeSectTotal || '-']]} />
      </ResourceCard>
      <ResourceCard title="系统信息" description="RouterOS /system/resource" icon="settings" tone="blue" health={resource.version ? 'normal' : 'unavailable'} className="resource-card-system">
        <ResourceDetails items={[["平台", resource.platform || '-'], ["架构", resource.architectureName || '-'], ["主板", resource.boardName || '-'], ["RouterOS 版本", resource.version || '-'], ["编译时间", resource.buildTime || '-'], ["出厂软件", resource.factorySoftware || '-'], ["运行时间", resource.uptime || '-']]} />
      </ResourceCard>
      <ResourceCard title="硬件" description="RouterOS hardware 只读信息" icon="settings" tone="green" health={resourceHealth(resource.hardware.length ? 0 : null)} className="resource-card-hardware">
        <ResourceHardwareList items={resource.hardware} />
      </ResourceCard>
      <ResourceCard title="IRQ" description="系统中断分布" icon="status" tone="orange" health={resourceHealth(resource.irqs.length ? 0 : null)} className="resource-card-irq">
        <ResourceIRQList items={resource.irqs} />
      </ResourceCard>
    </section>
    <div className="resource-updated">最后更新 {formatDateTime(props.overview.updatedAt)}</div>
  </div>
}

function LoadPage(props: { samples: LoadSample[]; window: string; onWindowChange: (value: string) => void }) {
  const latest = props.samples[props.samples.length - 1]
  return (
    <div className="page-grid">
      <div className="data-toolbar panel load-toolbar">
        <strong>历史范围</strong>
        {['1h', '1d', '1w', '1m'].map((value) => <button key={value} type="button" className={props.window === value ? 'toolbar-button active' : 'toolbar-button'} onClick={() => props.onWindowChange(value)}>{value === '1h' ? '1 小时' : value === '1d' ? '1 天' : value === '1w' ? '1 周' : '1 月'}</button>)}
        <span className="toolbar-spacer" /><span className="result-count">每分钟聚合，保留 35 天</span>
      </div>
      <section className="load-grid">
        <MetricHistory title="CPU 使用率" samples={props.samples} value={(sample) => sample.cpuLoadPercent} format={(value) => `${value.toFixed(1)}%`} />
        <MetricHistory title="内存使用率" samples={props.samples} value={(sample) => sample.memoryUsedPercent} format={(value) => `${value.toFixed(1)}%`} />
        <MetricHistory title="存储使用率" samples={props.samples} value={(sample) => sample.storageUsedPercent} format={(value) => `${value.toFixed(1)}%`} />
        <MetricHistory title="在线终端" samples={props.samples} value={(sample) => sample.onlineTerminalCount} format={(value) => `${Math.round(value)} 台`} />
        <MetricHistory title="总吞吐" samples={props.samples} value={(sample) => sample.uploadBps + sample.downloadBps} format={formatBits} />
      </section>
      {latest ? <div className="load-current">最新采样：CPU {latest.cpuLoadPercent.toFixed(1)}% · 内存 {latest.memoryUsedPercent.toFixed(1)}% · 存储 {latest.storageUsedPercent.toFixed(1)}% · 在线 {latest.onlineTerminalCount} 台 · 上行 {formatBits(latest.uploadBps)} · 下行 {formatBits(latest.downloadBps)}</div> : null}
    </div>
  )
}

function MetricHistory(props: { title: string; samples: LoadSample[]; value: (sample: LoadSample) => number; format: (value: number) => string }) {
  const values = props.samples.map(props.value)
  const maximum = Math.max(1, ...values)
  const width = 520
  const height = 170
  const points = values.map((value, index) => `${values.length <= 1 ? width / 2 : 18 + index * (width - 36) / (values.length - 1)},${height - 24 - value / maximum * (height - 46)}`).join(' ')
  return <section className="panel metric-history"><div className="panel-head"><h3>{props.title}</h3><span>当前 {values.length ? props.format(values[values.length - 1]) : '-'} · 最大 {props.format(Math.max(0, ...values))}</span></div>{values.length ? <><svg viewBox={`0 0 ${width} ${height}`}><line x1="18" x2={width - 18} y1={height - 24} y2={height - 24} className="grid-line" /><polyline points={points} className="metric-line" /></svg><div className="chart-time"><span>{formatShortTime(props.samples[0].timestamp)}</span><span>{formatShortTime(props.samples[props.samples.length - 1].timestamp)}</span></div></> : <div className="empty-chart">等待历史采样</div>}</section>
}

function ProtocolPage(props: { protocols: ProtocolStat[]; deviceID: string }) {
  const [history, setHistory] = useState<ProtocolHistorySample[]>([])
  useEffect(() => {
    let cancelled = false
    const load = async () => {
      const response = await fetch(scopedURL('/api/protocols?window=30m', props.deviceID))
      if (!response.ok) return
      const payload = (await response.json()) as { history: ProtocolHistorySample[] }
      if (!cancelled) setHistory(payload.history)
    }
    load().catch(() => undefined)
    const timer = window.setInterval(() => load().catch(() => undefined), 30000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [props.deviceID])
  const totalBytes = props.protocols.reduce((sum, item) => sum + item.uploadBytes + item.downloadBytes, 0)
  const historyBytes = new Map<string, number>()
  history.forEach((sample) => historyBytes.set(sample.name, (historyBytes.get(sample.name) ?? 0) + (sample.uploadBps + sample.downloadBps) * 60 / 8))
  const historyTotal = Array.from(historyBytes.values()).reduce((sum, value) => sum + value, 0)
  return <section className="panel compact-panel"><div className="data-toolbar"><strong>当前连接协议分布</strong><span className="result-count">基于 RouterOS 连接跟踪、DNS 和域名特征库，不是 DPI</span><span className="toolbar-spacer" /><span>近 30 分钟 {history.length} 条采样</span></div><div className="table-scroll"><table className="data-table"><thead><tr><th>应用分类</th><th>传输协议</th><th>连接数</th><th>当前上行</th><th>当前下行</th><th>活动连接累计</th><th>当前占比</th><th>近30分钟占比</th><th>识别方式</th></tr></thead><tbody>{props.protocols.length ? props.protocols.map((item) => { const bytes = item.uploadBytes + item.downloadBytes; const recent = historyBytes.get(item.name) ?? 0; return <tr key={`${item.name}-${item.kind}`}><td>{item.name}</td><td>{item.kind}</td><td>{item.connections}</td><td>{formatBits(item.uploadBps)}</td><td>{formatBits(item.downloadBps)}</td><td>{formatBytes(bytes)}</td><td>{totalBytes ? `${(bytes / totalBytes * 100).toFixed(1)}%` : '-'}</td><td>{historyTotal ? `${(recent / historyTotal * 100).toFixed(1)}%` : '-'}</td><td>{item.source === 'dns' ? 'MosDNS + 特征库' : item.source === 'mixed' ? 'DNS + 端口混合' : item.estimated ? '端口估算' : 'RouterOS 原生'}</td></tr> }) : <tr><td colSpan={9} className="empty-row">当前没有可统计的活动连接</td></tr>}</tbody></table></div></section>
}

function PolicyPage(props: { policies: PolicyStat[] }) {
  return <section className="panel compact-panel"><div className="data-toolbar"><strong>现有 RouterOS 策略计数器</strong><span className="result-count">只读展示，不创建或修改规则</span><span className="toolbar-spacer" /><span>共 {props.policies.length} 条</span></div><div className="table-scroll"><table className="data-table"><thead><tr><th>来源</th><th>名称</th><th>目标 / 动作</th><th>标记</th><th>当前速率</th><th>累计流量</th><th>包数</th><th>状态</th></tr></thead><tbody>{props.policies.length ? props.policies.map((item, index) => <tr key={`${item.kind}-${item.name}-${index}`}><td>{item.kind}</td><td>{item.name}</td><td>{item.target || '-'}</td><td>{item.mark || '-'}</td><td>{item.rate || '-'}</td><td>{formatBytes(item.bytes)}</td><td>{item.packets}</td><td>{item.disabled ? '已禁用' : '生效中'}</td></tr>) : <tr><td colSpan={8} className="empty-row">当前 RouterOS 没有可展示的队列、队列树或带计数/标记的 mangle 策略</td></tr>}</tbody></table></div></section>
}

function DHCPPage(props: { dhcp: DHCPStat }) {
  const [query, setQuery] = useState('')
  const trimmed = query.trim().toLowerCase()
  const leases = trimmed ? props.dhcp.leases.filter((item) => [item.address, item.macAddress, item.hostName, item.comment].some((value) => value.toLowerCase().includes(trimmed))) : props.dhcp.leases
  const poolsByName = new Map(props.dhcp.pools.map((pool) => [pool.name, pool]))
  if (!props.dhcp.servers.length && !props.dhcp.leases.length) {
    return <section className="panel compact-panel"><div className="empty-row dhcp-empty">该设备未启用 DHCP Server 或接口无权限</div></section>
  }
  const leaseStatusText = (item: DHCPStat['leases'][number]) => item.blocked ? '已阻止' : item.disabled ? '已禁用' : item.status === 'bound' ? '已绑定' : item.status === 'waiting' ? '等待中' : item.status || '-'
  const leaseStatusClass = (item: DHCPStat['leases'][number]) => item.blocked || item.disabled ? 'lease-status blocked' : item.status === 'bound' ? 'lease-status bound' : 'lease-status waiting'
  return (
    <div className="page-grid">
      <section className="panel compact-panel dhcp-server-list">
        <div className="data-toolbar"><strong>DHCP Server</strong><span className="toolbar-spacer" /><span>{props.dhcp.servers.length} 个</span></div>
        <div className="dhcp-server-grid" role="list" aria-label="DHCP Server 列表">
          {props.dhcp.servers.length ? props.dhcp.servers.map((item) => {
            const pool = item.addressPool ? poolsByName.get(item.addressPool) : undefined
            const usagePercent = pool && Number.isFinite(pool.usedPercent) ? Math.max(0, Math.min(100, pool.usedPercent)) : 0
            return <article key={item.name} className="dhcp-server-card" role="listitem">
              <div className="dhcp-server-name"><span>Server</span><strong>{item.name}</strong></div>
              <div className="dhcp-server-field dhcp-server-interface"><span>接口</span><strong>{item.interface || '-'}</strong></div>
              <div className="dhcp-server-field dhcp-server-pool"><span>地址池</span><strong>{item.addressPool || '-'}</strong></div>
              <div className="dhcp-server-field dhcp-server-range"><span>IP 地址范围</span><strong>{pool?.ranges || '-'}</strong></div>
              <div className="dhcp-server-field dhcp-server-lease"><span>Lease 时长</span><strong>{item.leaseTime || '-'}</strong></div>
              <div className="dhcp-server-usage"><span>地址使用</span><strong>{pool ? `${pool.used} / ${pool.total || '-'}` : '-'}</strong>{pool?.total ? <><div className="dhcp-usage-bar" aria-label={`地址池已使用 ${pool.usedPercent.toFixed(1)}%`}><i style={{ width: `${usagePercent}%` }} /></div><small>{pool.usedPercent.toFixed(1)}% 已用</small></> : null}</div>
              <div className="dhcp-server-field dhcp-server-status"><span>状态</span><strong className={item.disabled || item.invalid ? 'server-bad' : 'server-ok'}>{item.disabled ? '已禁用' : item.invalid ? '配置无效' : '运行中'}</strong></div>
            </article>
          }) : <div className="dhcp-server-card dhcp-server-empty"><span>没有 DHCP Server 配置</span></div>}
        </div>
      </section>
      <section className="panel compact-panel">
        <div className="data-toolbar">
          <strong>DHCP 租约</strong>
          <input className="search-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="地址 / MAC / 主机名 / 备注" />
          <span className="toolbar-spacer" />
          <span>显示 {leases.length} / {props.dhcp.leases.length} 条</span>
        </div>
        <div className="table-scroll"><table className="data-table"><thead><tr><th>地址</th><th>MAC</th><th>主机名</th><th>备注</th><th>Server</th><th>状态</th><th>剩余到期</th><th>类型</th></tr></thead><tbody>
          {leases.length ? leases.map((item, index) => (
            <tr key={item.id || `${item.address}-${index}`}>
              <td>{item.address || '-'}</td>
              <td>{item.macAddress || '-'}</td>
              <td>{item.hostName || '-'}</td>
              <td>{item.comment || '-'}</td>
              <td>{item.server || '-'}</td>
              <td><span className={leaseStatusClass(item)}>{leaseStatusText(item)}</span></td>
              <td>{item.expiresAfter > 0 ? formatSeconds(item.expiresAfter) : '-'}</td>
              <td>{item.dynamic ? '动态' : '静态'}</td>
            </tr>
          )) : <tr><td colSpan={8} className="empty-row">{props.dhcp.leases.length ? '没有匹配搜索条件的租约' : '当前没有租约'}</td></tr>}
        </tbody></table></div>
      </section>
    </div>
  )
}

function RoutesPage(props: { routes: RouteStat[] }) {
  const [hideDisabled, setHideDisabled] = useState(true)
  const disabledCount = props.routes.filter((item) => item.disabled).length
  const visibleRoutes = hideDisabled ? props.routes.filter((item) => !item.disabled) : props.routes
  const rules = visibleRoutes.filter((item) => item.kind === 'rule')
  const routeItems = visibleRoutes.filter((item) => item.kind !== 'rule')
  const tables = Array.from(new Set(routeItems.map((item) => item.table || 'main'))).sort((left, right) => left === 'main' ? -1 : right === 'main' ? 1 : left.localeCompare(right))
  const isDefaultRoute = (item: RouteStat) => item.destination === '0.0.0.0/0' || item.destination === '::/0'
  const protocolText = (value: string) => value === 'static' ? '静态' : value === 'connected' ? '直连' : value === 'dynamic' ? '动态' : value || '-'
  const routeRow = (item: RouteStat, index: number) => (
    <tr key={item.id || `${item.kind}-${item.destination}-${item.table}-${index}`} title={item.scope || item.targetScope ? `scope: ${item.scope || '-'} / target-scope: ${item.targetScope || '-'}` : undefined}>
      <td>{item.family === 'ipv6' ? 'IPv6' : 'IPv4'}</td>
      <td>{item.destination || '-'}</td>
      <td>{item.gateway || '-'}</td>
      <td>{item.prefSrc || '-'}</td>
      <td>{protocolText(item.protocol)}</td>
      <td>{item.distance}</td>
      <td>{item.currentMatches}</td>
      <td>{item.comment || '-'}</td>
      <td>{item.disabled ? '已禁用' : item.active ? '活动' : '非活动'}</td>
    </tr>
  )
  return (
    <div className="page-grid">
      <div className="data-toolbar panel">
        <strong>现有路由与分流状态</strong><span className="result-count">匹配数为当前 conntrack 快照推算</span><span className="toolbar-spacer" />
        <label className="toolbar-toggle"><input type="checkbox" checked={hideDisabled} onChange={(event) => setHideDisabled(event.target.checked)} /><span>隐藏已禁用</span></label>
        <span>显示 {visibleRoutes.length} / {props.routes.length} 条{disabledCount ? `，已禁用 ${disabledCount}` : ''}</span>
      </div>
      {rules.length ? (
        <section className="panel compact-panel">
          <div className="data-toolbar"><strong>Routing Rules（分流入口）</strong><span className="toolbar-spacer" /><span>{rules.length} 条 · 命中连接 {rules.reduce((sum, item) => sum + item.currentMatches, 0)}</span></div>
          <div className="table-scroll"><table className="data-table"><thead><tr><th>IP</th><th>源地址 / 接口</th><th>目标网段</th><th>路由表</th><th>动作</th><th>命中连接</th><th>备注</th><th>状态</th></tr></thead><tbody>
            {rules.map((item, index) => (
              <tr key={item.id || `rule-${index}`}>
                <td>{item.family === 'ipv6' ? 'IPv6' : 'IPv4'}</td>
                <td>{item.source || '-'}</td>
                <td>{item.destination || '-'}</td>
                <td>{item.table || 'main'}</td>
                <td>{item.action || '-'}</td>
                <td>{item.currentMatches}</td>
                <td>{item.comment || '-'}</td>
                <td>{item.disabled ? '已禁用' : '生效中'}</td>
              </tr>
            ))}
          </tbody></table></div>
        </section>
      ) : null}
      {tables.map((table) => {
        const group = routeItems.filter((item) => (item.table || 'main') === table)
        const defaultRoute = group.find((item) => isDefaultRoute(item) && !item.disabled)
        const matchTotal = group.reduce((sum, item) => sum + item.currentMatches, 0)
        return (
          <section className="panel compact-panel" key={table}>
            <div className="data-toolbar"><strong>路由表 {table}</strong><span className="result-count">{group.length} 条 · 命中连接 {matchTotal} · {defaultRoute ? `默认路由 ${defaultRoute.active ? '活动' : '非活动'}（${defaultRoute.gateway || '-'}）` : '无默认路由'}</span></div>
            <div className="table-scroll"><table className="data-table"><thead><tr><th>IP</th><th>目标网段</th><th>网关</th><th>pref-src</th><th>来源</th><th>距离</th><th>命中连接</th><th>备注</th><th>状态</th></tr></thead><tbody>{group.map(routeRow)}</tbody></table></div>
          </section>
        )
      })}
      {!rules.length && !routeItems.length ? <section className="panel compact-panel"><div className="empty-row dhcp-empty">{props.routes.length ? '已隐藏全部禁用路由与分流规则' : '当前没有可读取的路由或分流状态'}</div></section> : null}
    </div>
  )
}

type TerminalVisibilityFilter = 'online' | 'all' | 'offline'

function TerminalsPage(props: {
  terminals: Terminal[]
  family: TerminalFamily
  query: string
  refreshMs: number
  onRefreshMsChange: (value: number) => void
  onRefresh: () => void
  onOpenDetail: (terminalID: string) => void
  onOpenRemark: (terminal: Terminal) => void
}) {
  const [sortKey, setSortKey] = useState<TerminalSortKey>('address')
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('asc')
  const [visibilityFilter, setVisibilityFilter] = useState<TerminalVisibilityFilter>('online')
  const [visibilityFilterOpen, setVisibilityFilterOpen] = useState(false)
  const [visibilityFilterPosition, setVisibilityFilterPosition] = useState({ left: 8, top: 64 })
  const visibilityFilterButtonRef = useRef<HTMLButtonElement>(null)
  const [pageSize, setPageSize] = useState(20)
  const [page, setPage] = useState(1)
  const sorted = useMemo(() => {
    const visible = props.terminals.filter((terminal) => visibilityFilter === 'all' || (visibilityFilter === 'online' ? terminal.state === 'online' : terminal.state !== 'online'))
    return [...visible].sort((left, right) => {
      const comparison = compareTerminal(left, right, sortKey, props.family)
      return sortDirection === 'asc' ? comparison : -comparison
    })
  }, [props.terminals, props.family, visibilityFilter, sortKey, sortDirection])
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const currentPage = Math.min(page, totalPages)
  const rows = sorted.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  useEffect(() => setPage(1), [props.query, visibilityFilter, pageSize])

  useEffect(() => {
    if (!visibilityFilterOpen) return undefined
    const closeOnOutsidePointer = (event: MouseEvent) => {
      const target = event.target
      if (target instanceof Element && target.closest('.terminal-visibility-filter, .terminal-visibility-filter-panel')) return
      setVisibilityFilterOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setVisibilityFilterOpen(false)
    }
    document.addEventListener('mousedown', closeOnOutsidePointer)
    document.addEventListener('keydown', closeOnEscape)
    return () => {
      document.removeEventListener('mousedown', closeOnOutsidePointer)
      document.removeEventListener('keydown', closeOnEscape)
    }
  }, [visibilityFilterOpen])

  const openVisibilityFilter = (anchor: HTMLElement) => {
    const shell = anchor.closest<HTMLElement>('.terminal-list-panel')
    if (shell) {
      const shellRect = shell.getBoundingClientRect()
      const anchorRect = anchor.getBoundingClientRect()
      const panelWidth = 220
      setVisibilityFilterPosition({
        left: Math.max(8, Math.min(anchorRect.left - shellRect.left, shellRect.width - panelWidth - 8)),
        top: anchorRect.bottom - shellRect.top + 4,
      })
    }
    setVisibilityFilterOpen((value) => !value)
  }

  const chooseVisibilityFilter = (value: string) => {
    setVisibilityFilter(value as TerminalVisibilityFilter)
    setVisibilityFilterOpen(false)
  }

  const changeSort = (key: TerminalSortKey) => {
    if (sortKey === key) setSortDirection((value) => value === 'asc' ? 'desc' : 'asc')
    else {
      setSortKey(key)
      setSortDirection('asc')
    }
  }

  return (
    <section className="panel compact-panel terminal-list-panel">
      <div className="data-toolbar terminal-toolbar">
        <span className="toolbar-spacer" />
        <span className="result-count">共 {sorted.length} 台</span>
        <select className="terminal-refresh-select" value={props.refreshMs} onChange={(event) => props.onRefreshMsChange(Number(event.target.value))} aria-label="自动刷新">
          <option value={0}>停止刷新</option><option value={1000}>1 秒刷新</option><option value={3000}>3 秒刷新</option><option value={5000}>5 秒刷新</option><option value={10000}>10 秒刷新</option>
        </select>
        <button type="button" className="toolbar-button terminal-refresh-button" onClick={props.onRefresh}>刷新</button>
      </div>

      <div className="table-scroll terminal-table-scroll">
        <table className="data-table terminal-table">
          <thead><tr>
            <SortHeader label="设备名称" sortKey="device" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="IP" sortKey="address" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="连接数" sortKey="connections" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="上行速率" sortKey="upload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="下行速率" sortKey="download" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label={props.family === 'all' ? '累计上行' : '活动累计上行'} sortKey="totalUpload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label={props.family === 'all' ? '累计下行' : '活动累计下行'} sortKey="totalDownload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <th>
              <div className="connection-header-controls terminal-status-header">
                <span>在线状态</span>
                <span className="terminal-visibility-filter">
                  <button
                    ref={visibilityFilterButtonRef}
                    type="button"
                    className={visibilityFilter !== 'online' ? 'column-filter-button active' : 'column-filter-button'}
                    aria-label="筛选在线状态"
                    aria-pressed={visibilityFilter !== 'online'}
                    aria-expanded={visibilityFilterOpen}
                    aria-controls={visibilityFilterOpen ? 'terminal-visibility-filter' : undefined}
                    onClick={(event) => openVisibilityFilter(event.currentTarget)}
                  >
                    <span aria-hidden="true">▾</span>
                  </button>
                </span>
              </div>
            </th>
            <SortHeader label="在线时长" sortKey="online" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="备注" sortKey="remark" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <th><span>操作</span></th>
          </tr></thead>
          <tbody>
            {rows.map((terminal) => {
              const metrics = terminalMetrics(terminal, props.family)
              const addressCount = props.family === 'ipv4' ? terminal.ipv4.length : props.family === 'ipv6' ? terminal.ipv6.length : terminal.ipv4.length + terminal.ipv6.length
              const shownAddressCount = props.family === 'all' ? Number(Boolean(terminal.primaryIpv4)) + Number(Boolean(terminal.primaryIpv6)) : Number(Boolean(terminalPrimaryAddress(terminal, props.family)))
              const extraAddressCount = Math.max(0, addressCount - shownAddressCount)
              return <tr key={terminal.id}>
                <td><button type="button" className="link-button terminal-link" onClick={() => props.onOpenDetail(terminal.id)}><strong>{terminal.displayName}</strong><span className="muted-text">{terminal.macAddress || 'MAC 未知'}</span></button></td>
                <td><button type="button" className="link-button terminal-link" onClick={() => props.onOpenDetail(terminal.id)}>
                  {props.family === 'all' ? <><strong>{terminal.primaryIpv4 || terminal.primaryIpv6 || '-'}</strong>{terminal.primaryIpv4 && terminal.primaryIpv6 ? <span className="muted-text">{terminal.primaryIpv6}{extraAddressCount ? `  +${extraAddressCount}` : ''}</span> : extraAddressCount ? <span className="muted-text">+{extraAddressCount}</span> : null}</> : <><strong>{terminalPrimaryAddress(terminal, props.family) || '-'}</strong>{extraAddressCount ? <span className="muted-text">+{extraAddressCount}</span> : null}</>}
                </button></td>
                <td>{metrics.connectionCount}</td><td>{formatBits(metrics.currentUploadBps)}</td><td>{formatBits(metrics.currentDownloadBps)}</td>
                <td>{formatBytes(metrics.totalUploadBytes)}</td><td>{formatBytes(metrics.totalDownloadBytes)}</td>
                <td><span className={`terminal-state-badge ${terminal.state}`}><span className={`state-dot state-${terminal.state}`} />{terminalStateText(terminal.state)}</span></td>
                <td>{terminal.state === 'online' ? formatOnlineDuration(terminal.onlineSince) : '-'}</td>
                <td>{terminal.remark || '-'}</td>
                <td><div className="action-links"><button type="button" className="link-button" onClick={() => props.onOpenDetail(terminal.id)}>详情</button><button type="button" className="link-button" onClick={() => props.onOpenRemark(terminal)}>编辑</button></div></td>
              </tr>
            })}
          </tbody>
        </table>
      </div>
      {visibilityFilterOpen ? (
        <div id="terminal-visibility-filter" className="connection-filter-panel terminal-visibility-filter-panel" role="dialog" aria-label="终端显示范围" style={{ left: visibilityFilterPosition.left, top: visibilityFilterPosition.top }}>
          <div className="connection-filter-panel-head"><strong>终端显示范围</strong><button type="button" className="link-button" onClick={() => setVisibilityFilterOpen(false)}>关闭</button></div>
          <ConnectionFilterOptions
            value={visibilityFilter}
            options={[{ value: 'online', label: '在线设备' }, { value: 'all', label: '全部设备' }, { value: 'offline', label: '显示离线设备（含未活跃）' }]}
            onChange={chooseVisibilityFilter}
          />
        </div>
      ) : null}
      <div className="pagination">
        <span>每页</span><select value={pageSize} onChange={(event) => setPageSize(Number(event.target.value))}><option value={10}>10</option><option value={20}>20</option><option value={50}>50</option></select>
        <button type="button" disabled={currentPage <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}>上一页</button>
        <span>{currentPage} / {totalPages}</span>
        <button type="button" disabled={currentPage >= totalPages} onClick={() => setPage((value) => Math.min(totalPages, value + 1))}>下一页</button>
      </div>
    </section>
  )
}

function SortHeader(props: { label: string; sortKey: TerminalSortKey; activeKey: TerminalSortKey; direction: 'asc' | 'desc'; onSort: (key: TerminalSortKey) => void }) {
  return <th><button type="button" className="sort-button" onClick={() => props.onSort(props.sortKey)}>{props.label}<span>{props.activeKey === props.sortKey ? (props.direction === 'asc' ? '↑' : '↓') : '↕'}</span></button></th>
}

type ConnectionFilterKey = 'family' | 'application' | 'protocol' | 'sourceAddress' | 'sourcePort' | 'destination' | 'routeTable' | 'gateway' | 'egress' | 'status'
type ConnectionSortKey = 'family' | 'application' | 'protocol' | 'sourceAddress' | 'sourcePort' | 'destination' | 'destinationPort' | 'upload' | 'download' | 'uploadBytes' | 'downloadBytes' | 'routeTable' | 'gateway' | 'egress' | 'status'

const unavailableRouteValue = '无法判断'

function connectionRouteTable(connection: TerminalConnection) {
  return connection.routeTable || unavailableRouteValue
}

function connectionGateway(connection: TerminalConnection) {
  return connection.routeGateways?.length ? connection.routeGateways.join(' / ') : unavailableRouteValue
}

function connectionEgress(connection: TerminalConnection) {
  return connection.egressInterfaces?.length ? connection.egressInterfaces.join(' / ') : '-'
}

function ConnectionColumnHeader(props: {
  label: string
  sortKey: ConnectionSortKey
  activeSort: ConnectionSortKey | null
  sortDirection: 'asc' | 'desc'
  filterKey?: ConnectionFilterKey
  filterActive?: boolean
  filterOpen?: boolean
  onSort: (key: ConnectionSortKey) => void
  onOpenFilter: (key: ConnectionFilterKey, anchor: HTMLElement) => void
}) {
  const sorting = props.activeSort === props.sortKey
  const filterKey = props.filterKey
  return <th aria-sort={sorting ? (props.sortDirection === 'asc' ? 'ascending' : 'descending') : 'none'}><div className="connection-header-controls">
    <button type="button" className={sorting ? 'connection-sort-button active' : 'connection-sort-button'} aria-label={`${props.label}${sorting ? `，当前${props.sortDirection === 'asc' ? '升序' : '降序'}` : ''}，点击排序`} onClick={() => props.onSort(props.sortKey)}><span>{props.label}</span>{sorting ? <span className="connection-sort-indicator" aria-hidden="true">{props.sortDirection === 'asc' ? '↑' : '↓'}</span> : null}</button>
    {filterKey ? <button type="button" className={props.filterActive ? 'column-filter-button active' : 'column-filter-button'} aria-label={`筛选${props.label}`} aria-pressed={Boolean(props.filterActive)} aria-expanded={Boolean(props.filterOpen)} aria-controls={props.filterOpen ? 'connection-filter-panel' : undefined} onClick={(event) => props.onOpenFilter(filterKey, event.currentTarget)}><span aria-hidden="true">▾</span></button> : null}
  </div></th>
}

function ConnectionFilterOptions(props: {
  value: string
  options: Array<{ value: string; label: string }>
  onChange: (value: string) => void
}) {
  return <div className="connection-filter-options">
    {props.options.map((option) => <button key={option.value} type="button" className={props.value === option.value ? 'active' : ''} aria-pressed={props.value === option.value} onClick={() => props.onChange(option.value)}>{option.label}</button>)}
  </div>
}

function compareConnection(left: TerminalConnection, right: TerminalConnection, key: ConnectionSortKey) {
  const text = (a: string, b: string) => a.localeCompare(b, 'zh-CN', { numeric: true, sensitivity: 'base' })
  switch (key) {
    case 'family': return text(left.family, right.family)
    case 'application': return text(left.application, right.application)
    case 'protocol': return text(left.protocol, right.protocol)
    case 'sourceAddress': return text(left.sourceAddress, right.sourceAddress)
    case 'sourcePort': return text(left.sourcePort, right.sourcePort)
    case 'destination': return text(left.destinationAddress, right.destinationAddress)
    case 'destinationPort': return text(left.destinationPort, right.destinationPort)
    case 'upload': return left.uploadBps - right.uploadBps
    case 'download': return left.downloadBps - right.downloadBps
    case 'uploadBytes': return left.uploadBytes - right.uploadBytes
    case 'downloadBytes': return left.downloadBytes - right.downloadBytes
    case 'routeTable': return text(connectionRouteTable(left), connectionRouteTable(right))
    case 'gateway': return text(connectionGateway(left), connectionGateway(right))
    case 'egress': return text(connectionEgress(left), connectionEgress(right))
    case 'status': return text(left.status, right.status)
  }
}

function useConnectionTableState(props: {
  connections: TerminalConnection[]
  scope: ConnectionFamily
  family: ConnectionFamily
  onFamilyChange: (value: ConnectionFamily) => void
  showStatus: boolean
}) {
  const [applicationQuery, setApplicationQuery] = useState('')
  const [protocolFilter, setProtocolFilter] = useState('all')
  const [sourceAddressQuery, setSourceAddressQuery] = useState('')
  const [sourcePortQuery, setSourcePortQuery] = useState('')
  const [destinationQuery, setDestinationQuery] = useState('')
  const [routeTableFilter, setRouteTableFilter] = useState('all')
  const [gatewayFilter, setGatewayFilter] = useState('all')
  const [egressFilter, setEgressFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [activeConnectionFilter, setActiveConnectionFilter] = useState<ConnectionFilterKey | null>(null)
  const [connectionSortKey, setConnectionSortKey] = useState<ConnectionSortKey | null>(null)
  const [connectionSortDirection, setConnectionSortDirection] = useState<'asc' | 'desc'>('asc')
  const [filterPanelLeft, setFilterPanelLeft] = useState(7)
  const [filterPanelTop, setFilterPanelTop] = useState(96)
  const scopedConnectionRows = props.scope === 'all' ? props.connections : props.connections.filter((item) => item.family === props.scope)
  const selectedFamily = props.scope === 'all' ? props.family : props.scope
  const familyConnections = selectedFamily === 'all' ? scopedConnectionRows : scopedConnectionRows.filter((item) => item.family === selectedFamily)
  const ipv4Connections = scopedConnectionRows.filter((item) => item.family === 'ipv4')
  const ipv6Connections = scopedConnectionRows.filter((item) => item.family === 'ipv6')
  const applications = Array.from(new Set(familyConnections.map((item) => item.application).filter(Boolean))).sort()
  const protocols = Array.from(new Set(familyConnections.map((item) => item.protocol))).sort()
  const routeTables = Array.from(new Set(familyConnections.map(connectionRouteTable))).sort()
  const gateways = Array.from(new Set(familyConnections.flatMap((item) => item.routeGateways?.length ? item.routeGateways : [unavailableRouteValue]))).sort()
  const egresses = Array.from(new Set(familyConnections.flatMap((item) => item.egressInterfaces?.length ? item.egressInterfaces : ['-']))).sort()
  const statuses = Array.from(new Set(familyConnections.map((item) => item.status).filter(Boolean))).sort()
  const filteredConnections = familyConnections.filter((connection) =>
    (protocolFilter === 'all' || connection.protocol === protocolFilter) &&
      (routeTableFilter === 'all' || connectionRouteTable(connection) === routeTableFilter) &&
      (gatewayFilter === 'all' || (gatewayFilter === unavailableRouteValue ? !connection.routeGateways?.length : connection.routeGateways?.includes(gatewayFilter))) &&
      (egressFilter === 'all' || (egressFilter === '-' ? !connection.egressInterfaces?.length : connection.egressInterfaces?.includes(egressFilter))) &&
      (!props.showStatus || statusFilter === 'all' || connection.status === statusFilter) &&
      (!applicationQuery || connection.application === applicationQuery) &&
      connection.sourceAddress.toLowerCase().includes(sourceAddressQuery.trim().toLowerCase()) &&
      connection.sourcePort.toLowerCase().includes(sourcePortQuery.trim().toLowerCase()) &&
      [connection.destinationAddress, connection.destinationPort].join(' ').toLowerCase().includes(destinationQuery.trim().toLowerCase())
  )
  const visibleConnections = connectionSortKey ? [...filteredConnections].sort((left, right) => {
    const comparison = compareConnection(left, right, connectionSortKey)
    return connectionSortDirection === 'asc' ? comparison : -comparison
  }) : filteredConnections
  const filterActive: Record<ConnectionFilterKey, boolean> = {
    family: props.scope === 'all' && props.family !== 'all',
    application: Boolean(applicationQuery),
    protocol: protocolFilter !== 'all',
    sourceAddress: Boolean(sourceAddressQuery),
    sourcePort: Boolean(sourcePortQuery),
    destination: Boolean(destinationQuery),
    routeTable: routeTableFilter !== 'all',
    gateway: gatewayFilter !== 'all',
    egress: egressFilter !== 'all',
    status: props.showStatus && statusFilter !== 'all',
  }
  const openConnectionFilter = (key: ConnectionFilterKey, anchor: HTMLElement) => {
    const shell = anchor.closest('.connection-table-shell')
    if (shell) {
      const shellRect = shell.getBoundingClientRect()
      const anchorRect = anchor.getBoundingClientRect()
      const preferredWidth = shellRect.width < 600 ? 240 : 300
      const panelWidth = Math.min(preferredWidth, Math.max(220, shellRect.width - 14))
      setFilterPanelLeft(Math.max(7, Math.min(anchorRect.left - shellRect.left, shellRect.width - panelWidth - 7)))
      setFilterPanelTop(anchorRect.bottom - shellRect.top + 4)
    }
    setActiveConnectionFilter((value) => value === key ? null : key)
  }
  const changeConnectionSort = (key: ConnectionSortKey) => {
    if (connectionSortKey === key) setConnectionSortDirection((value) => value === 'asc' ? 'desc' : 'asc')
    else {
      setConnectionSortKey(key)
      setConnectionSortDirection('asc')
    }
  }
  const chooseConnectionFilter = (apply: () => void) => {
    apply()
    setActiveConnectionFilter(null)
  }
  return {
    activeConnectionFilter, activeSort: connectionSortKey, applications, changeConnectionSort, chooseConnectionFilter,
    egresses, familyFilterable: props.scope === 'all', filterActive, filterPanelLeft, filterPanelTop, gateways,
    ipv4Connections, ipv6Connections, onFamilyChange: props.onFamilyChange, openConnectionFilter, protocols, routeTables, scopedConnectionRows, selectedFamily,
    setActiveConnectionFilter, setApplicationQuery, setDestinationQuery, setEgressFilter, setGatewayFilter,
    setProtocolFilter, setRouteTableFilter, setSourceAddressQuery, setSourcePortQuery, setStatusFilter,
    sortDirection: connectionSortDirection, sourceAddressQuery, sourcePortQuery, applicationQuery, destinationQuery, gatewayFilter,
    egressFilter, protocolFilter, routeTableFilter, statusFilter, statuses, visibleConnections,
  }
}

type ConnectionTableState = ReturnType<typeof useConnectionTableState>

function ConnectionTable(props: { state: ConnectionTableState; showStatus: boolean; emptyLabel: string }) {
  const state = props.state
  const filterPanel = state.activeConnectionFilter ? <div id="connection-filter-panel" className="connection-filter-panel" role="dialog" aria-label="连接筛选" style={{ left: state.filterPanelLeft, top: state.filterPanelTop }}>
    <div className="connection-filter-panel-head"><strong>筛选连接</strong><button type="button" className="link-button" onClick={() => state.setActiveConnectionFilter(null)}>关闭</button></div>
    {state.activeConnectionFilter === 'family' ? <ConnectionFilterOptions value={state.selectedFamily} options={[{ value: 'all', label: `全部 (${state.scopedConnectionRows.length})` }, { value: 'ipv4', label: `IPv4 (${state.ipv4Connections.length})` }, { value: 'ipv6', label: `IPv6 (${state.ipv6Connections.length})` }]} onChange={(value) => state.chooseConnectionFilter(() => state.onFamilyChange(value as ConnectionFamily))} /> : null}
    {state.activeConnectionFilter === 'application' ? <ConnectionFilterOptions value={state.applicationQuery} options={[{ value: '', label: '全部应用' }, ...state.applications.map((application) => ({ value: application, label: application }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setApplicationQuery(value))} /> : null}
    {state.activeConnectionFilter === 'protocol' ? <ConnectionFilterOptions value={state.protocolFilter} options={[{ value: 'all', label: '全部协议' }, ...state.protocols.map((protocol) => ({ value: protocol, label: protocol }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setProtocolFilter(value))} /> : null}
    {state.activeConnectionFilter === 'sourceAddress' ? <><ConnectionFilterOptions value={state.sourceAddressQuery ? 'filtered' : 'all'} options={[{ value: 'all', label: '全部来源 IP' }]} onChange={() => state.chooseConnectionFilter(() => state.setSourceAddressQuery(''))} /><input value={state.sourceAddressQuery} onChange={(event) => state.setSourceAddressQuery(event.target.value)} placeholder="来源 IP" aria-label="来源 IP 筛选" /></> : null}
    {state.activeConnectionFilter === 'sourcePort' ? <><ConnectionFilterOptions value={state.sourcePortQuery ? 'filtered' : 'all'} options={[{ value: 'all', label: '全部来源端口' }]} onChange={() => state.chooseConnectionFilter(() => state.setSourcePortQuery(''))} /><input value={state.sourcePortQuery} onChange={(event) => state.setSourcePortQuery(event.target.value)} placeholder="来源端口" aria-label="来源端口筛选" /></> : null}
    {state.activeConnectionFilter === 'destination' ? <><ConnectionFilterOptions value={state.destinationQuery ? 'filtered' : 'all'} options={[{ value: 'all', label: '全部目的地址' }]} onChange={() => state.chooseConnectionFilter(() => state.setDestinationQuery(''))} /><input value={state.destinationQuery} onChange={(event) => state.setDestinationQuery(event.target.value)} placeholder="目的 IP 或端口" aria-label="目的地址筛选" /></> : null}
    {state.activeConnectionFilter === 'routeTable' ? <ConnectionFilterOptions value={state.routeTableFilter} options={[{ value: 'all', label: '全部路由表' }, ...state.routeTables.map((table) => ({ value: table, label: table }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setRouteTableFilter(value))} /> : null}
    {state.activeConnectionFilter === 'gateway' ? <ConnectionFilterOptions value={state.gatewayFilter} options={[{ value: 'all', label: '全部网关' }, ...state.gateways.map((gateway) => ({ value: gateway, label: gateway }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setGatewayFilter(value))} /> : null}
    {state.activeConnectionFilter === 'egress' ? <ConnectionFilterOptions value={state.egressFilter} options={[{ value: 'all', label: '全部出接口' }, ...state.egresses.map((egress) => ({ value: egress, label: egress }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setEgressFilter(value))} /> : null}
    {state.activeConnectionFilter === 'status' ? <select value={state.statusFilter} onChange={(event) => state.setStatusFilter(event.target.value)} aria-label="连接状态筛选"><option value="all">全部状态</option>{state.statuses.map((status) => <option key={status} value={status}>{status}</option>)}</select> : null}
  </div> : null
  const columnCount = 14 + Number(props.showStatus)
  return <div className="connection-table-shell" onKeyDown={(event) => { if (event.key === 'Escape') state.setActiveConnectionFilter(null) }}>
    {filterPanel}
    <div className="connection-table-viewport" role="region" aria-label="终端连接明细" tabIndex={0}><table className="data-table connection-table"><thead><tr>
      <ConnectionColumnHeader label="IP版本" sortKey="family" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey={state.familyFilterable ? 'family' : undefined} filterActive={state.filterActive.family} filterOpen={state.activeConnectionFilter === 'family'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="应用" sortKey="application" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="application" filterActive={state.filterActive.application} filterOpen={state.activeConnectionFilter === 'application'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="协议" sortKey="protocol" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="protocol" filterActive={state.filterActive.protocol} filterOpen={state.activeConnectionFilter === 'protocol'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="来源 IP" sortKey="sourceAddress" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="sourceAddress" filterActive={state.filterActive.sourceAddress} filterOpen={state.activeConnectionFilter === 'sourceAddress'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="来源端口" sortKey="sourcePort" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="sourcePort" filterActive={state.filterActive.sourcePort} filterOpen={state.activeConnectionFilter === 'sourcePort'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="目的地址" sortKey="destination" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="destination" filterActive={state.filterActive.destination} filterOpen={state.activeConnectionFilter === 'destination'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="目的端口" sortKey="destinationPort" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="当前上行" sortKey="upload" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="当前下行" sortKey="download" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="累计上行" sortKey="uploadBytes" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="累计下行" sortKey="downloadBytes" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="路由表" sortKey="routeTable" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="routeTable" filterActive={state.filterActive.routeTable} filterOpen={state.activeConnectionFilter === 'routeTable'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="下一跳网关" sortKey="gateway" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="gateway" filterActive={state.filterActive.gateway} filterOpen={state.activeConnectionFilter === 'gateway'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="出接口" sortKey="egress" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="egress" filterActive={state.filterActive.egress} filterOpen={state.activeConnectionFilter === 'egress'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      {props.showStatus ? <ConnectionColumnHeader label="连接状态" sortKey="status" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="status" filterActive={state.filterActive.status} filterOpen={state.activeConnectionFilter === 'status'} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} /> : null}
    </tr></thead><tbody>{state.visibleConnections.length ? state.visibleConnections.map((connection) => (
      <tr key={connection.key}>
        <td><span className={`ip-family-badge ${connection.family}`}>{connection.family === 'ipv4' ? 'IPv4' : 'IPv6'}</span></td>
        <td title={connection.matchedDomain || undefined}>{connection.application}{connection.matchedDomain ? <small className="connection-domain">{connection.matchedDomain}</small> : null}</td><td>{connection.protocol}</td><td>{connection.sourceAddress || '-'}</td><td>{connection.sourcePort || '-'}</td>
        <td>{connection.destinationAddress || '-'}</td><td>{connection.destinationPort || '-'}</td>
        <td>{formatBits(connection.uploadBps)}</td><td>{formatBits(connection.downloadBps)}</td>
        <td>{formatBytes(connection.uploadBytes)}</td><td>{formatBytes(connection.downloadBytes)}</td>
        <td>{connectionRouteTable(connection)}</td><td>{connectionGateway(connection)}</td><td>{connectionEgress(connection)}</td>{props.showStatus ? <td>{connection.status}</td> : null}
      </tr>
    )) : <tr><td colSpan={columnCount} className="empty-row">{props.emptyLabel}</td></tr>}</tbody></table></div>
  </div>
}

function TerminalDetailPage(props: {
  detail: TerminalDetail
  activeTab: TerminalTab
  connectionFamily: ConnectionFamily
  scope: TerminalFamily
  onBack: () => void
  onTabChange: (value: TerminalTab) => void
  onConnectionFamilyChange: (value: ConnectionFamily) => void
}) {
  const isRouterConntrack = props.detail.terminal.id === 'routeros:self'
  const scopedConnections = props.scope === 'all' ? props.detail.connections : props.detail.connections.filter((item) => item.family === props.scope)
  const scopedEgressInterfaces = Array.from(new Set(scopedConnections.flatMap((item) => item.egressInterfaces ?? []))).sort((left, right) => left.localeCompare(right, 'zh-CN', { numeric: true }))
  const repliedConnections = scopedConnections.filter((item) => item.seenReply).length
  const unrepliedConnections = scopedConnections.length - repliedConnections
  const summary = props.scope === 'all' ? props.detail.terminal : (props.detail.familySummaries?.[props.scope] ?? props.detail.terminal)
  const visibleFlows = props.scope === 'all' ? props.detail.flowCategories : (props.detail.familyFlows?.[props.scope] ?? [])
  const connectionTable = useConnectionTableState({ connections: props.detail.connections, scope: props.scope, family: props.connectionFamily, onFamilyChange: props.onConnectionFamilyChange, showStatus: isRouterConntrack })

  return (
    <section className={props.activeTab === 'connections' ? 'detail-page detail-page-connections' : 'detail-page'}>
      <div className="detail-page-head">
        <div className="detail-identity">
          <h3>{summary.displayName}</h3>
          <div className="detail-identity-meta">
            <span>IP {terminalPrimaryAddress(summary, props.scope) || '-'}</span>
            <span>MAC {summary.macAddress || '-'}</span>
            <span className={`identity-state ${summary.state}`}>{terminalStateText(summary.state)}</span>
            <span>{isRouterConntrack ? '跟踪条目' : '连接'} {summary.connectionCount}</span>
            {isRouterConntrack ? <span>已回包 {repliedConnections} / 未回包 {unrepliedConnections}</span> : null}
            <span>↑ {formatBits(summary.currentUploadBps)}</span>
            <span>↓ {formatBits(summary.currentDownloadBps)}</span>
            {props.detail.ratesUpdatedAt ? <span>速率更新 {relativeUpdateTime(props.detail.ratesUpdatedAt)}</span> : null}
          </div>
        </div>
        <div className="detail-head-actions">
          <button type="button" className="close-button" onClick={props.onBack}>
            返回
          </button>
        </div>
      </div>

      <div className={`tab-row detail-tabs${props.scope === 'all' ? ' has-history' : ''}`}>
        <TabButton label="基础信息" active={props.activeTab === 'basic'} onClick={() => props.onTabChange('basic')} />
        <TabButton label={isRouterConntrack ? '跟踪详情' : '连接详情'} active={props.activeTab === 'connections'} onClick={() => props.onTabChange('connections')} />
        <TabButton label="流量分布" active={props.activeTab === 'flows'} onClick={() => props.onTabChange('flows')} />
        {props.scope === 'all' ? <TabButton label="历史记录" active={props.activeTab === 'history'} onClick={() => props.onTabChange('history')} /> : null}
      </div>

      <section className={props.activeTab === 'connections' ? 'panel detail-panel detail-panel-connections' : 'panel detail-panel'}>
        {props.activeTab === 'basic' ? (
          <div className="detail-grid">
            <DetailItem label="设备名称" value={summary.displayName} />
            <DetailItem label="自动识别名称" value={summary.autoName || '暂未识别'} />
            {props.scope !== 'ipv6' ? <DetailItem label="IPv4 地址" value={summary.ipv4.join(' / ') || '-'} /> : null}
            {props.scope !== 'ipv4' ? <DetailItem label="IPv6 地址" value={summary.ipv6.join(' / ') || '-'} /> : null}
            <DetailItem label="MAC 地址" value={summary.macAddress || '-'} />
            <DetailItem label="接入接口" value={summary.primaryInterface || '-'} />
            <DetailItem label="出接口" value={scopedEgressInterfaces.join(' / ') || '-'} />
            <DetailItem label={isRouterConntrack ? (props.scope === 'all' ? 'conntrack 条目（IPv4+IPv6）' : `${props.scope.toUpperCase()} conntrack 条目`) : (props.scope === 'all' ? '连接数（IPv4+IPv6）' : `${props.scope.toUpperCase()} 连接数`)} value={`${summary.connectionCount}`} />
            {isRouterConntrack ? <DetailItem label="已见回包（S）" value={`${repliedConnections}`} /> : null}
            {isRouterConntrack ? <DetailItem label="未见回包" value={`${unrepliedConnections}`} /> : null}
            <DetailItem label="本次在线时长" value={summary.state === 'online' ? formatOnlineDuration(summary.onlineSince) : '-'} />
            <DetailItem label="当前上行速率" value={formatBits(summary.currentUploadBps)} />
            <DetailItem label="当前下行速率" value={formatBits(summary.currentDownloadBps)} />
            <DetailItem label={props.scope === 'all' ? '累计上行' : '活动连接累计上行'} value={formatBytes(summary.totalUploadBytes)} />
            <DetailItem label={props.scope === 'all' ? '累计下行' : '活动连接累计下行'} value={formatBytes(summary.totalDownloadBytes)} />
            <DetailItem label="备注" value={summary.remark || '-'} />
            <DetailItem label="面板开始统计" value={formatDateTime(summary.trackingSince)} />
            <DetailItem label="最后活动时间" value={formatDateTime(summary.lastSeen)} />
          </div>
        ) : null}

        {props.activeTab === 'connections' ? <ConnectionTable state={connectionTable} showStatus={isRouterConntrack} emptyLabel={`当前筛选范围没有 ${connectionTable.selectedFamily === 'all' ? '活动' : connectionTable.selectedFamily.toUpperCase()} ${isRouterConntrack ? '跟踪条目' : '连接详情'}`} /> : null}

        {props.activeTab === 'flows' ? (
          <div>
            <p className="table-note">按当前活动连接的协议和端口估算，不等同于 DPI 应用识别。</p>
            <div className="table-scroll">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>应用</th>
                    <th>上行速率</th>
                    <th>累计上行及占比</th>
                    <th>下行速率</th>
                    <th>累计下行及占比</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleFlows.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="empty-row">
                        当前暂无足够连接用于估算流量分布
                      </td>
                    </tr>
                  ) : (
                    visibleFlows.map((flow) => (
                      <tr key={flow.name}>
                        <td>{flow.name}</td>
                        <td>{formatBits(flow.currentUploadBps)}</td>
                        <td>{formatBytes(flow.totalUploadBytes)} / {flow.uploadPercent.toFixed(2)}%</td>
                        <td>{formatBits(flow.currentDownloadBps)}</td>
                        <td>{formatBytes(flow.totalDownloadBytes)} / {flow.downloadPercent.toFixed(2)}%</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}

        {props.activeTab === 'history' ? (
          <div>
            <p className="table-note">每分钟保存一条面板本地累计快照。</p>
            <div className="table-scroll">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>日期/时间</th>
                    <th>在线时长</th>
                    <th>累计上行</th>
                    <th>累计下行</th>
                  </tr>
                </thead>
                <tbody>
                  {props.detail.history.length === 0 ? (
                    <tr>
                      <td colSpan={4} className="empty-row">
                        暂无历史记录
                      </td>
                    </tr>
                  ) : (
                    props.detail.history.map((entry) => (
                      <tr key={entry.timestamp}>
                        <td>{formatDateTime(entry.timestamp)}</td>
                        <td>{formatSeconds(entry.onlineSeconds)}</td>
                        <td>{formatBytes(entry.totalUploadBytes)}</td>
                        <td>{formatBytes(entry.totalDownloadBytes)}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}
      </section>
    </section>
  )
}

function TerminalMetadataModal(props: {
  terminal: Terminal
  customName: string
  remark: string
  saving: boolean
  onCustomNameChange: (value: string) => void
  onRemarkChange: (value: string) => void
  onClose: () => void
  onSave: () => void
}) {
  return (
    <div className="dialog-backdrop" role="dialog" aria-modal="true">
      <div className="remark-modal">
        <div className="dialog-head">
          <div>
            <h3>编辑终端</h3>
            <p className="muted-text">设备名称和备注只保存到面板本地，不写回 RouterOS。</p>
          </div>
          <button type="button" className="close-button" onClick={props.onClose}>
            关闭
          </button>
        </div>
        <div className="remark-modal-body">
          <label className="metadata-field">
            <span>设备名称</span>
            <input value={props.customName} onChange={(event) => props.onCustomNameChange(event.target.value)} maxLength={100} placeholder={props.terminal.displayName} />
            <small>自动识别：{props.terminal.autoName || '暂未识别'}；清空后恢复自动名称。</small>
          </label>
          <label className="metadata-field">
            <span>备注</span>
          <textarea
            value={props.remark}
            onChange={(event) => props.onRemarkChange(event.target.value)}
            rows={5}
            maxLength={500}
            className="remark-textarea"
          />
          </label>
          <div className="remark-modal-actions">
            <button type="button" className="close-button" onClick={props.onClose}>
              取消
            </button>
            <button type="button" className="primary-button" onClick={props.onSave} disabled={props.saving}>
              {props.saving ? '保存中...' : '保存'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function TabButton(props: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      className={props.active ? 'tab-button active' : 'tab-button'}
      onClick={props.onClick}
    >
      {props.label}
    </button>
  )
}

function DetailItem(props: { label: string; value: string }) {
  return (
    <div className="detail-item">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </div>
  )
}

function DetailSummary(props: { label: string; value: string }) {
  return (
    <div className="detail-summary-item">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </div>
  )
}

export default App
