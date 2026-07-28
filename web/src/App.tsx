import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
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
} from './lib/types'

type IconName = 'overview' | 'status' | 'network' | 'terminal' | 'traffic' | 'policy' | 'runtime' | 'route' | 'settings' | 'refresh' | 'cpu' | 'memory' | 'connections' | 'shield' | 'router' | 'storage' | 'alert' | 'info' | 'check' | 'search' | 'clear' | 'eye' | 'eyeOff'
type SettingsSection = 'connection' | 'collection' | 'ui' | 'account' | 'maintenance'
type PanelTheme = 'light' | 'dark'
type PanelPreferences = { refreshMs: number; landingView: ActiveView; terminalFamily: TerminalFamily; theme: PanelTheme }
type ConnectionDraft = { scheme: 'http' | 'https'; host: string; port: number; username: string; password: string }
type CollectionDraft = {
  pollIntervalSeconds: number
  realtimePollIntervalSeconds: number
  terminalPollIntervalSeconds: number
  sampleRetentionHours: number
}

const RealtimeTrafficChart = lazy(() => import('./components/RealtimeTrafficChart').then((module) => ({ default: module.RealtimeTrafficChart })))

const panelPreferenceKey = 'rosboard:panel-preferences'
const selectedDeviceKey = 'rosboard:selected-device'
const trafficWindowKey = 'rosboard:traffic-window'
const defaultPanelPreferences: PanelPreferences = { refreshMs: 1000, landingView: 'overview', terminalFamily: 'all', theme: 'light' }
const restartPollIntervalMs = 750
const restartTimeoutMs = 90_000

function delay(milliseconds: number) {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds))
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

async function requestJSON(path: string, method: string, body?: unknown) {
  const response = await fetch(path, { method, headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  if (response.status === 401) window.dispatchEvent(new Event('rosboard:authentication-required'))
  const failure = response.ok ? null : await response.json().catch(() => null) as { error?: string } | null
  if (!response.ok) throw new Error(failure?.error || `HTTP ${response.status}`)
  return response
}
const landingViews: ActiveView[] = ['overview', 'interfaces', 'terminals', 'load', 'protocols', 'policies', 'routes', 'settings']

function loadPanelPreferences(): PanelPreferences {
  try {
    const raw = window.localStorage.getItem(panelPreferenceKey)
    if (!raw) return defaultPanelPreferences
    const parsed = JSON.parse(raw) as Partial<PanelPreferences>
    return {
      refreshMs: [0, 1000, 3000, 5000, 10000].includes(Number(parsed.refreshMs)) ? Number(parsed.refreshMs) : defaultPanelPreferences.refreshMs,
      landingView: parsed.landingView && landingViews.includes(parsed.landingView) ? parsed.landingView : defaultPanelPreferences.landingView,
      terminalFamily: parsed.terminalFamily === 'ipv4' || parsed.terminalFamily === 'ipv6' || parsed.terminalFamily === 'all' ? parsed.terminalFamily : defaultPanelPreferences.terminalFamily,
      theme: parsed.theme === 'dark' ? 'dark' : 'light',
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

function TerminalScopeSummaryBar({ summary }: { summary: TerminalScopeSummary }) {
  const items = [
    ['设备', summary.deviceCount],
    ['连接', summary.connectionCount],
    ['↑', formatBits(summary.currentUploadBps)],
    ['↓', formatBits(summary.currentDownloadBps)],
    ['活动累计↑', formatBytes(summary.activeUploadBytes)],
    ['活动累计↓', formatBytes(summary.activeDownloadBytes)],
  ]
  return (
    <div className="terminal-scope-summary" aria-label="终端概览">
      {items.map(([label, value]) => (
        <span key={label}>
          <small>{label}</small>
          <strong>{value}</strong>
        </span>
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
  }
  return <svg className="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[props.name]}</svg>
}

function NavLabel(props: { icon: IconName; label: string }) { return <span className="nav-label"><Icon name={props.icon} /><span>{props.label}</span></span> }

function relativeUpdateTime(value: string) {
  const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000))
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

function StartupCard(props: { title: string; description: string; error?: string | null; children?: React.ReactNode }) {
	return <main className="setup-shell"><section className="panel setup-panel auth-panel">
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
	useEffect(() => { void loadSettings().catch((loadError) => setError(loadError instanceof Error ? loadError.message : '设置读取失败')) }, [])
	if (stage === 'choice') return <StartupCard title="开始使用 Rosboard" description="管理员已创建。现在可以添加第一台 RouterOS，也可以稍后再配置。" error={error}>
		<div className="onboarding-choice">
			<button type="button" className="onboarding-choice-card primary-choice" onClick={() => { setEditorDeviceID(''); setEditorVersion((value) => value + 1); setStage('editor') }}><Icon name="router" /><span><strong>添加 ROS 设备</strong><small>测试连接并设置采集接口与本地 CIDR</small></span></button>
			<button type="button" className="onboarding-choice-card" onClick={async () => { try { const hasDevices = Boolean(settings?.devices.length); if (hasDevices) await onRestartingAction(() => postJSON('/api/setup/complete', { skipRouterOS: false }).then(() => undefined), () => setError('面板正在启动，恢复后将自动进入面板...')); else { await postJSON('/api/setup/complete', { skipRouterOS: true }); props.onComplete() } } catch (skipError) { setError(skipError instanceof Error ? skipError.message : '进入面板失败') } }}><Icon name="overview" /><span><strong>{settings?.devices.length ? '进入面板' : '跳过并进入面板'}</strong><small>{settings?.devices.length ? '重启采集服务并进入监控面板' : '稍后可在设备管理中添加 RouterOS'}</small></span></button>
		</div>
	</StartupCard>
	return <StartupCard title="添加 RouterOS" description="填写连接信息并测试成功后，再选择采集接口和本地 CIDR。" error={error}>
		{message ? <div className="settings-message" role="status">{message}</div> : null}
		{settings ? <DeviceSettingsPanel key={editorVersion} onboarding initialDeviceID={editorDeviceID} settings={settings} selectedDeviceID="" interfaces={[]} onSaved={async (deviceID) => { await loadSettings(); setEditorDeviceID(deviceID); setEditorVersion((value) => value + 1); setMessage('设备已保存，面板未重启。可点击“+”继续添加设备，或点击“完成设置”。') }} onRestartingAction={onRestartingAction} /> : null}
		<div className="setup-back"><button type="button" className="toolbar-button" onClick={() => setStage('choice')}>返回上一步</button></div>
	</StartupCard>
}

function PanelApp(props: { username: string; onAuthenticationChanged: () => void }) {
  const [panelPreferences, setPanelPreferences] = useState<PanelPreferences>(() => loadPanelPreferences())
  const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
  const [activeView, setActiveView] = useState<ActiveView>(() => panelPreferences.landingView)
  const [query, setQuery] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [settings, setSettings] = useState<SettingsResponse | null>(null)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('connection')
  const [collectionSaving, setCollectionSaving] = useState(false)
  const [collectionMessage, setCollectionMessage] = useState<string | null>(null)
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
  const [dashboardRefreshMs, setDashboardRefreshMs] = useState(() => panelPreferences.refreshMs)
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [loadWindow, setLoadWindow] = useState('1h')
  const [loadSamples, setLoadSamples] = useState<LoadSample[]>([])
  const [devices, setDevices] = useState<DeviceStatus[]>([])
	const [devicesLoaded, setDevicesLoaded] = useState(false)
  const [selectedDeviceID, setSelectedDeviceID] = useState(() => window.localStorage.getItem(selectedDeviceKey) ?? '')
  const [trafficWindow, setTrafficWindow] = useState(() => window.sessionStorage.getItem(trafficWindowKey) ?? '5m')
  const [trafficSamples, setTrafficSamples] = useState<RateSample[]>([])
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [statusExpanded, setStatusExpanded] = useState(true)
  const [settingsExpanded, setSettingsExpanded] = useState(true)
  const [expandedMonitorGroup, setExpandedMonitorGroup] = useState<'terminals' | 'traffic' | 'runtime' | null>(null)
  const [warningsExpanded, setWarningsExpanded] = useState(false)

  const updatePanelPreferences = (next: PanelPreferences) => {
    setPanelPreferences(next)
    savePanelPreferences(next)
  }
  const hasDashboard = dashboard !== null

  useEffect(() => {
    document.documentElement.dataset.theme = panelPreferences.theme
    document.documentElement.style.colorScheme = panelPreferences.theme
  }, [panelPreferences.theme])

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
    if (activeView === 'overview' || activeView === 'settings') return
    setStatusExpanded(true)
    if (activeView === 'terminals') setExpandedMonitorGroup('terminals')
    if (activeView === 'protocols' || activeView === 'policies') setExpandedMonitorGroup('traffic')
    if (activeView === 'load' || activeView === 'routes') setExpandedMonitorGroup('runtime')
  }, [activeView])

  useEffect(() => {
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
        payload.interfaces ??= []
        payload.terminals ??= []
        payload.capabilities ??= []
        payload.protocols ??= []
        payload.policies ??= []
        payload.routes ??= []
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
    const timer = dashboardRefreshMs > 0 ? window.setInterval(refresh, 3000) : 0
    return () => {
      cancelled = true
      if (timer) window.clearInterval(timer)
    }
  }, [dashboardRefreshMs, refreshNonce, restartPending, selectedDeviceID])

  useEffect(() => {
    if (activeView !== 'overview' || dashboardRefreshMs <= 0) return
    let cancelled = false
    let refreshing = false

    const refreshRealtime = async () => {
      if (refreshing) return
      refreshing = true
      try {
        const response = await fetch(scopedURL('/api/realtime', selectedDeviceID))
        if (!response.ok) throw new Error(`HTTP ${response.status}`)
        const overview = (await response.json()) as Overview
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
    if (!selectedTerminalID) {
      setTerminalDetail(null)
      return
    }

    let cancelled = false
    const load = async () => {
      const response = await fetch(scopedURL(`/api/terminals/${encodeURIComponent(selectedTerminalID)}`, selectedDeviceID))
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`)
      }
      const payload = (await response.json()) as TerminalDetail
      if (!cancelled) {
        setTerminalDetail(payload)
      }
    }

    const handleError = (detailError: unknown) => {
      if (!cancelled && !restartPending) {
        setError(detailError instanceof Error ? detailError.message : '终端详情读取失败')
      }
    }
    load().catch(handleError)
    const timer = window.setInterval(() => load().catch(handleError), 3000)

    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [restartPending, selectedTerminalID, selectedDeviceID])

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
    const timer = window.setInterval(() => void load().catch(() => undefined), trafficWindow === '5m' ? 3000 : 30000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [activeView, trafficWindow, selectedDeviceID, refreshNonce, restartPending])

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

  if (!dashboard) {
	if (devicesLoaded && devices.filter((device) => device.enabled && !device.archived).length === 0 && settings) return <EmptyDevicePanel settings={settings} username={props.username} onAuthenticationChanged={props.onAuthenticationChanged} />
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
  const currentDevice = devices.find((device) => device.id === selectedDeviceID)
  const globalWarnings = Array.from(new Set((dashboard.warnings ?? []).map((warning) => warning.trim()).filter(Boolean)))
  const alertCount = Math.max(dashboard.alerts?.length ?? 0, globalWarnings.length)
  const statusActive = activeView === 'interfaces' || activeView === 'terminals' || activeView === 'protocols' || activeView === 'policies' || activeView === 'load' || activeView === 'routes'
  const settingsSections: Array<{ key: SettingsSection; label: string; icon: IconName }> = [
    { key: 'connection', label: '设备管理', icon: 'router' },
    { key: 'collection', label: '采集设置', icon: 'refresh' },
    { key: 'ui', label: '界面设置', icon: 'overview' },
    { key: 'account', label: '账号安全', icon: 'shield' },
    { key: 'maintenance', label: '维护设置', icon: 'storage' },
  ]

  return (
    <main className={sidebarOpen ? 'shell sidebar-open' : 'shell'}>
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
            <p>{dashboard.overview.version}</p>
          </div>
        </div>

        <nav className="menu">
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
                  setSelectedTerminalID(null)
                  setSidebarOpen(false)
                }}
              >
                <NavLabel icon="network" label="线路监控" />
              </button>
              <button
                type="button"
                className="submenu-section submenu-toggle"
                aria-expanded={expandedMonitorGroup === 'terminals'}
                onClick={() => {
                  setExpandedMonitorGroup('terminals')
                  setActiveView('terminals')
                  setTerminalFamily('all')
                  setSelectedTerminalID(null)
                  setSidebarOpen(false)
                }}
              >
                <NavLabel icon="terminal" label="终端监控" />
              </button>
              {expandedMonitorGroup === 'terminals' ? (['all', 'ipv4', 'ipv6'] as TerminalFamily[]).map((family) => (
                <button
                  key={family}
                  type="button"
                  className={activeView === 'terminals' && terminalFamily === family ? 'submenu-item nested active' : 'submenu-item nested'}
                  onClick={() => {
                    setActiveView('terminals')
                    setTerminalFamily(family)
                    setSelectedTerminalID(null)
                    setSidebarOpen(false)
                  }}
                >
                  <NavLabel icon="terminal" label={family === 'all' ? '全部终端' : family.toUpperCase()} />
                </button>
              )) : null}
              <button type="button" className="submenu-section submenu-toggle" aria-expanded={expandedMonitorGroup === 'traffic'} onClick={() => setExpandedMonitorGroup((value) => value === 'traffic' ? null : 'traffic')}><NavLabel icon="traffic" label="流量监控" /></button>
              {expandedMonitorGroup === 'traffic' ? <>
                <button type="button" className={activeView === 'protocols' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('protocols'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="traffic" label="协议统计" /></button>
                <button type="button" className={activeView === 'policies' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('policies'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="policy" label="策略统计" /></button>
              </> : null}
              <button type="button" className="submenu-section submenu-toggle" aria-expanded={expandedMonitorGroup === 'runtime'} onClick={() => setExpandedMonitorGroup((value) => value === 'runtime' ? null : 'runtime')}><NavLabel icon="runtime" label="运行监控" /></button>
              {expandedMonitorGroup === 'runtime' ? <>
                <button type="button" className={activeView === 'load' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('load'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="runtime" label="负载历史" /></button>
                <button type="button" className={activeView === 'routes' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('routes'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="route" label="路由 / 分流" /></button>
              </> : null}
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
            <div><dt>RouterOS 版本</dt><dd>{dashboard.overview.version || '-'}</dd></div>
            <div><dt>运行时间</dt><dd>{dashboard.overview.uptime || '-'}</dd></div>
          </dl>
        </div>
      </aside>

      <section className="content">
        <header className={detailMode ? 'topbar detail-topbar' : activeView === 'overview' ? 'topbar overview-topbar' : activeView === 'terminals' ? 'topbar terminal-topbar' : 'topbar'}>
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
            <div>
              <h2>{detailMode ? '终端详情' : viewTitle(activeView)}</h2>
              <p className="topbar-subtitle">
                {detailMode
                  ? `状态监控 > 终端监控 > ${detailScope === 'all' ? '全部终端' : detailScope.toUpperCase()}`
                  : `系统正常 · 更新于 ${formatDateTime(dashboard.overview.updatedAt)}`}
              </p>
            </div>
          </div>
          <div className="topbar-controls">
            {activeView === 'terminals' && !detailMode ? (
              <TerminalScopeSummaryBar summary={dashboard.terminalScopeSummaries?.[terminalFamily] ?? emptyTerminalScopeSummary} />
            ) : null}
            {activeView === 'overview' && !detailMode ? (
              <OverviewRangePills value={trafficWindow} onChange={setTrafficWindow} />
            ) : null}
            {globalWarnings.length ? (
              <button type="button" className="system-ok system-alerting global-warning-toggle" aria-expanded={warningsExpanded} aria-controls="global-warning-list" onClick={() => setWarningsExpanded((value) => !value)}><i />{alertCount} 项告警</button>
            ) : (
              <span className={dashboard.alerts?.length ? 'system-ok system-alerting' : 'system-ok'}><i />{dashboard.alerts?.length ? `${dashboard.alerts.length} 项告警` : '系统正常'}</span>
            )}
            <span className="last-updated">最后更新 {relativeUpdateTime(dashboard.overview.updatedAt)}</span>
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

        {activeView === 'overview' ? (
          <OverviewPage dashboard={dashboard} loadSamples={loadSamples} trafficSamples={trafficSamples} />
        ) : null}

        {activeView === 'interfaces' ? (
          <InterfacesPage interfaces={dashboard.interfaces} deviceID={selectedDeviceID} />
        ) : null}

        {activeView === 'load' ? <LoadPage samples={loadSamples} window={loadWindow} onWindowChange={setLoadWindow} /> : null}
        {activeView === 'protocols' ? <ProtocolPage protocols={dashboard.protocols ?? []} deviceID={selectedDeviceID} /> : null}
        {activeView === 'policies' ? <PolicyPage policies={dashboard.policies ?? []} /> : null}
        {activeView === 'routes' ? <RoutesPage routes={dashboard.routes ?? []} /> : null}
        {activeView === 'settings' ? (
          <SettingsPage
            settings={settings}
            error={settingsError}
            activeSection={settingsSection}
            preferences={panelPreferences}
            dashboard={dashboard}
            selectedDeviceID={selectedDeviceID}
            collectionSaving={collectionSaving}
            collectionMessage={collectionMessage}
            restartSaving={restartSaving}
            restartMessage={restartMessage}
            onSaveCollection={saveCollectionSettings}
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

        {activeView === 'terminals' && !detailMode ? (
          <TerminalsPage
            terminals={filteredTerminals}
            family={terminalFamily}
            query={query}
            onQueryChange={setQuery}
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
              const payload = (await response.json()) as TerminalDetail
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
              setError(saveError instanceof Error ? saveError.message : '备注保存失败')
            } finally {
              setSavingRemark(false)
            }
          }}
        />
      ) : null}
    </main>
  )
}

function EmptyDevicePanel(props: { settings: SettingsResponse; username: string; onAuthenticationChanged: () => void }) {
	const [section, setSection] = useState<'overview' | 'interfaces' | 'terminals' | 'devices' | 'account' | 'maintenance'>('overview')
	const [sidebarOpen, setSidebarOpen] = useState(false)
	const label = section === 'overview' ? '系统概览' : section === 'interfaces' ? '线路监控' : section === 'terminals' ? '终端监控' : section === 'devices' ? '设备管理' : section === 'account' ? '账号安全' : '维护设置'
	const choose = (value: typeof section) => { setSection(value); setSidebarOpen(false) }
	return <main className={sidebarOpen ? 'shell empty-device-shell sidebar-open' : 'shell empty-device-shell'}>
		<button type="button" className="sidebar-backdrop" aria-label="关闭导航" onClick={() => setSidebarOpen(false)} />
		<aside className="sidebar">
			<div className="brand"><img className="brand-mark" src={rosboardMark} alt="" /><div className="brand-copy"><h1>Rosboard</h1><p>尚未连接设备</p></div></div>
			<nav className="menu">
				<button className={section === 'overview' ? 'menu-item active' : 'menu-item'} onClick={() => choose('overview')}><NavLabel icon="overview" label="系统概览" /></button>
				<button className={section === 'interfaces' ? 'menu-item active' : 'menu-item'} onClick={() => choose('interfaces')}><NavLabel icon="network" label="线路监控" /></button>
				<button className={section === 'terminals' ? 'menu-item active' : 'menu-item'} onClick={() => choose('terminals')}><NavLabel icon="terminal" label="终端监控" /></button>
				<button className={section === 'devices' ? 'menu-item active' : 'menu-item'} onClick={() => choose('devices')}><NavLabel icon="router" label="设备管理" /></button>
				<button className={section === 'account' ? 'menu-item active' : 'menu-item'} onClick={() => choose('account')}><NavLabel icon="shield" label="账号安全" /></button>
				<button className={section === 'maintenance' ? 'menu-item active' : 'menu-item'} onClick={() => choose('maintenance')}><NavLabel icon="storage" label="维护设置" /></button>
			</nav>
			<div className="sidebar-device-card"><label>当前设备</label><p>尚未添加 RouterOS</p></div>
		</aside>
		<section className="content"><header className="topbar"><div className="topbar-title"><button type="button" className="mobile-menu-button" aria-label="打开导航" onClick={() => setSidebarOpen(true)}><span /></button><div><h2>{label}</h2><p className="topbar-subtitle">可随时添加第一台 RouterOS，账号与维护设置始终可用。</p></div></div></header>
			{section === 'devices' ? <section className="panel settings-panel"><div className="empty-device-callout"><Icon name="router" /><div><h3>还没有 RouterOS 设备</h3><p>测试连接并选择至少一个采集接口和本地 CIDR 后即可开始监控。</p></div></div><DeviceSettingsPanel settings={props.settings} selectedDeviceID="" interfaces={[]} onRestartingAction={async (action, onOffline) => { await action(); await waitForPanelRestart(onOffline) }} /></section> : section === 'account' ? <AccountSettings username={props.username} onAuthenticationChanged={props.onAuthenticationChanged} /> : section === 'maintenance' ? <section className="panel settings-panel"><div className="panel-head"><h3>维护设置</h3></div><FullResetZone onRestartingAction={async (action, onOffline) => { await action(); await waitForPanelRestart(onOffline) }} /></section> : <section className="panel settings-panel empty-monitor-state"><Icon name="router" /><h3>尚未添加设备</h3><p>{label}需要 RouterOS 数据。添加设备后，这里会自动开始显示监控内容。</p><button type="button" className="primary-button" onClick={() => setSection('devices')}>添加 RouterOS 设备</button></section>}
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
  restartSaving: boolean
  restartMessage: string | null
  onSaveCollection: (draft: CollectionDraft) => Promise<void>
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
    document.documentElement.style.colorScheme = preferenceDraft.theme
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
          <DeviceSettingsPanel settings={props.settings} selectedDeviceID={props.selectedDeviceID} interfaces={props.dashboard.interfaces ?? []} terminalScope={props.dashboard.terminalScope} trafficScope={props.dashboard.trafficScope} onRestartingAction={props.onRestartingAction} />
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
              <label className={preferenceDraft.theme === 'light' ? 'theme-option active' : 'theme-option'}>
                <input type="radio" name="panel-theme" value="light" checked={preferenceDraft.theme === 'light'} onChange={() => setPreferenceDraft((current) => ({ ...current, theme: 'light' }))} />
                <span className="theme-preview theme-preview-light" aria-hidden="true"><i /><i /><i /></span>
                <span><strong>明亮</strong><small>适合白天和高亮环境</small></span>
              </label>
              <label className={preferenceDraft.theme === 'dark' ? 'theme-option active' : 'theme-option'}>
                <input type="radio" name="panel-theme" value="dark" checked={preferenceDraft.theme === 'dark'} onChange={() => setPreferenceDraft((current) => ({ ...current, theme: 'dark' }))} />
                <span className="theme-preview theme-preview-dark" aria-hidden="true"><i /><i /><i /></span>
                <span><strong>黑暗</strong><small>适合夜间和低亮环境</small></span>
              </label>
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
  const [savingAction, setSavingAction] = useState<'save' | 'complete' | null>(null)
  const [message, setMessage] = useState<string | null>(null)
	const [verification, setVerification] = useState<VerificationResponse | null>(null)
	const [scopedDashboard, setScopedDashboard] = useState<Pick<DashboardResponse, 'trafficScope' | 'terminalScope'> | null>(null)
	const [scopeLoading, setScopeLoading] = useState(false)
	const [scopeError, setScopeError] = useState<string | null>(null)
	const [testing, setTesting] = useState(false)
	const saving = savingAction !== null
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
  const choose = (device?: SettingsDevice) => { setDraft(deviceDraft(device)); setPasswordVisible(false); setMessage(null); setVerification(null); setScopedDashboard(null); setScopeError(null) }
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
  const request = async (path: string, method: string, body?: unknown, completeOnboarding = false) => {
	setSavingAction(completeOnboarding ? 'complete' : 'save'); setMessage(null)
    try {
	  if (props.onboarding && !completeOnboarding) {
		const response = await requestJSON(path, method, body)
		const result = await response.json() as { id?: string }
		await props.onSaved?.(result.id || draft.id)
		setSavingAction(null)
		return
	  }
      setMessage('已保存，面板正在重启，请保持此页面打开...')
      await props.onRestartingAction(() => requestJSON(path, method, body).then(() => undefined), () => setMessage('面板正在启动，恢复后将自动刷新...'))
    } catch (error) { setMessage(error instanceof Error ? error.message : '设备设置保存失败'); setSavingAction(null) }
  }
	const saveDevice = (completeOnboarding: boolean) => request(draft.id ? `/api/devices/${encodeURIComponent(draft.id)}` : '/api/devices', draft.id ? 'PUT' : 'POST', { ...draft, completeOnboarding, deferRestart: props.onboarding && !completeOnboarding, verificationToken: verification?.verificationToken || '', trafficInterfaces: draft.trafficMode === 'auto' ? [] : trafficInterfaces, trafficScope: { mode: draft.trafficMode === 'auto' ? 'auto' : undefined, include_interfaces: parseSettingList(draft.trafficIncludeInterfaces), exclude_interfaces: parseSettingList(draft.trafficExcludeInterfaces) }, terminalCidrs: parseSettingList(draft.terminalCidrs), terminalScope: { mode: 'auto', include_interfaces: parseSettingList(draft.includeInterfaces), exclude_interfaces: parseSettingList(draft.excludeInterfaces), include_cidrs: parseSettingList(draft.includeCidrs), exclude_cidrs: parseSettingList(draft.excludeCidrs) } }, completeOnboarding)
  return <div className="device-settings-workspace">
	<div className="device-settings-list">
      <div className="device-settings-list-head"><strong>设备</strong><button type="button" className="icon-button" aria-label="添加设备" title="添加设备" onClick={() => choose()}><span aria-hidden="true">+</span></button></div>
      {available.map((device) => <button key={device.id} type="button" className={draft.id === device.id ? 'device-row active' : 'device-row'} onClick={() => choose(device)}><span><strong>{device.name}</strong><small>{device.host}:{device.port}</small></span><i className={device.enabled ? 'online' : ''} /></button>)}
      {!available.length ? <p className="settings-empty">尚未添加设备</p> : null}
    </div>
    <form className="settings-form device-editor" onSubmit={(event) => { event.preventDefault(); void saveDevice(false) }}>
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
      <div className={props.onboarding ? 'settings-actions onboarding-device-actions span-2' : 'settings-actions span-2'}><button type="submit" className="primary-button" disabled={saving || verificationRequired}>{savingAction === 'save' ? '保存中...' : verificationRequired ? '请先测试连接' : props.onboarding ? '保存设备' : draft.id ? '保存设备' : '添加设备'}</button>{props.onboarding ? <button type="button" className="complete-setup-button" disabled={saving || verificationRequired} onClick={() => void saveDevice(true)}>{savingAction === 'complete' ? '正在完成...' : '完成设置'}</button> : null}{draft.id && !props.onboarding ? <button type="button" className="danger-button" disabled={saving} onClick={() => { if (window.confirm(`归档设备“${draft.name}”？历史数据将保留。`)) void request(`/api/devices/${encodeURIComponent(draft.id)}`, 'DELETE') }}>归档设备</button> : null}</div>
      {message ? <div className="settings-message span-2" role="status">{message}</div> : null}
    </form>
  </div>
}

function ArchivedDevices({ settings, onRestartingAction }: { settings: SettingsResponse; onRestartingAction: (action: () => Promise<void>, onOffline: () => void) => Promise<void> }) {
  const archived = settings.devices.filter((device) => device.archived)
  if (!archived.length) return null
  const act = async (device: SettingsDevice, purge: boolean) => {
    const confirmation = purge ? window.prompt(`输入设备名称“${device.name}”以永久清除全部历史数据`) : null
    if (purge && confirmation !== device.name) return
    await onRestartingAction(
      () => requestJSON(`/api/devices/${encodeURIComponent(device.id)}/${purge ? 'data' : 'restore'}`, purge ? 'DELETE' : 'POST', purge ? { confirmation } : undefined).then(() => undefined),
      () => undefined,
    )
  }
  return <div className="archived-devices"><strong>已归档设备</strong>{archived.map((device) => <div key={device.id}><span>{device.name}</span><button type="button" className="toolbar-button" onClick={() => void act(device, false)}>恢复</button><button type="button" className="danger-button" onClick={() => void act(device, true)}>永久清除</button></div>)}</div>
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

function formatSettingList(values: string[]) {
  return values.length ? values.join(' / ') : '-'
}

function OverviewRangePills(props: { value: string; onChange: (value: string) => void }) {
  return <span className="range-pills topbar-range-pills" aria-label="首页时间范围">{['5m', '1h', '6h', '24h'].map((value) => <button key={value} type="button" className={props.value === value ? 'active' : ''} onClick={() => props.onChange(value)}>{value === '5m' ? '5min' : value}</button>)}</span>
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

function InterfacesPage(props: { interfaces: InterfaceStatus[]; deviceID: string }) {
  const [selected, setSelected] = useState<string | null>(null)
  const [detail, setDetail] = useState<InterfaceDetail | null>(null)
  useEffect(() => {
    if (!selected) { setDetail(null); return }
    let cancelled = false
    const load = async () => {
      const response = await fetch(scopedURL(`/api/interfaces/${encodeURIComponent(selected)}`, props.deviceID))
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as InterfaceDetail
      if (!cancelled) setDetail(payload)
    }
    load().catch(() => undefined)
    const timer = window.setInterval(() => load().catch(() => undefined), 5000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [selected, props.deviceID])
  return (
    <div className="page-grid">
    {detail ? <section className="panel interface-detail"><div className="panel-head"><h3>{detail.interface.name} 接口详情</h3><button type="button" className="close-button" onClick={() => setSelected(null)}>关闭</button></div><div className="detail-summary"><DetailSummary label="状态" value={detail.interface.running && !detail.interface.disabled ? '在线' : '离线'} /><DetailSummary label="地址" value={detail.interface.addresses?.join(' / ') || '-'} /><DetailSummary label="MAC" value={detail.interface.macAddress || '-'} /><DetailSummary label="协商速率" value={detail.interface.linkRate ? `${detail.interface.linkRate}${detail.interface.fullDuplex ? ' / 全双工' : ''}` : '-'} /><DetailSummary label="当前上行" value={formatBits(detail.interface.currentTxBps)} /><DetailSummary label="当前下行" value={formatBits(detail.interface.currentRxBps)} /><DetailSummary label="收 / 发包" value={`${detail.interface.rxPackets} / ${detail.interface.txPackets}`} /><DetailSummary label="错误 / 丢包" value={`${detail.interface.rxErrors + detail.interface.txErrors} / ${detail.interface.rxDrops + detail.interface.txDrops}`} /></div>{detail.samples.length ? <Suspense fallback={<div className="realtime-traffic-chart chart-loading">正在加载图表...</div>}><RealtimeTrafficChart samples={detail.samples} ariaLabel={`${detail.interface.name} 接口上传和下载速率趋势`} /></Suspense> : <div className="empty-chart">暂无速率采样</div>}</section> : null}
    <section className="panel compact-panel">
      <div className="data-toolbar"><strong>接口运行状态</strong><span className="result-count">物理与逻辑接口只读监控</span><span className="toolbar-spacer" /><span>共 {props.interfaces.length} 个接口</span></div>
      <div className="table-scroll">
        <table className="data-table interface-table">
          <thead>
            <tr>
              <th>接口</th>
              <th>类型</th>
              <th>地址 / MAC</th>
              <th>状态</th>
              <th>上行速率</th>
              <th>下行速率</th>
              <th>累计上行</th>
              <th>累计下行</th>
              <th>MTU</th>
              <th>掉线次数</th>
              <th>错误 / 丢包</th>
            </tr>
          </thead>
          <tbody>
            {props.interfaces.map((item) => (
              <tr key={item.name}>
                <td><button type="button" className="link-button" onClick={() => setSelected(item.name)}>{item.name}</button></td>
                <td>{item.type}</td>
                <td><div className="address-stack"><span>{item.addresses?.join(' / ') || '-'}</span><span className="muted-text">{item.macAddress || '-'}</span></div></td>
                <td>{item.running && !item.disabled ? '在线' : '离线'}</td>
                <td>{formatBits(item.currentTxBps)}</td>
                <td>{formatBits(item.currentRxBps)}</td>
                <td>{formatBytes(item.txBytes)}</td>
                <td>{formatBytes(item.rxBytes)}</td>
                <td>{item.actualMtu || '-'}</td>
                <td>{item.linkDowns}</td>
                <td>{item.rxErrors + item.txErrors} / {item.rxDrops + item.txDrops}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
    </div>
  )
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
  return <section className="panel compact-panel"><div className="data-toolbar"><strong>当前连接协议分布</strong><span className="result-count">基于 RouterOS 连接跟踪与端口估算，不是 DPI</span><span className="toolbar-spacer" /><span>近 30 分钟 {history.length} 条采样</span></div><div className="table-scroll"><table className="data-table"><thead><tr><th>应用分类</th><th>传输协议</th><th>连接数</th><th>当前上行</th><th>当前下行</th><th>活动连接累计</th><th>当前占比</th><th>近30分钟占比</th><th>识别方式</th></tr></thead><tbody>{props.protocols.length ? props.protocols.map((item) => { const bytes = item.uploadBytes + item.downloadBytes; const recent = historyBytes.get(item.name) ?? 0; return <tr key={`${item.name}-${item.kind}`}><td>{item.name}</td><td>{item.kind}</td><td>{item.connections}</td><td>{formatBits(item.uploadBps)}</td><td>{formatBits(item.downloadBps)}</td><td>{formatBytes(bytes)}</td><td>{totalBytes ? `${(bytes / totalBytes * 100).toFixed(1)}%` : '-'}</td><td>{historyTotal ? `${(recent / historyTotal * 100).toFixed(1)}%` : '-'}</td><td>{item.estimated ? '端口估算' : 'RouterOS 原生'}</td></tr> }) : <tr><td colSpan={9} className="empty-row">当前没有可统计的活动连接</td></tr>}</tbody></table></div></section>
}

function PolicyPage(props: { policies: PolicyStat[] }) {
  return <section className="panel compact-panel"><div className="data-toolbar"><strong>现有 RouterOS 策略计数器</strong><span className="result-count">只读展示，不创建或修改规则</span><span className="toolbar-spacer" /><span>共 {props.policies.length} 条</span></div><div className="table-scroll"><table className="data-table"><thead><tr><th>来源</th><th>名称</th><th>目标 / 动作</th><th>标记</th><th>当前速率</th><th>累计流量</th><th>包数</th><th>状态</th></tr></thead><tbody>{props.policies.length ? props.policies.map((item, index) => <tr key={`${item.kind}-${item.name}-${index}`}><td>{item.kind}</td><td>{item.name}</td><td>{item.target || '-'}</td><td>{item.mark || '-'}</td><td>{item.rate || '-'}</td><td>{formatBytes(item.bytes)}</td><td>{item.packets}</td><td>{item.disabled ? '已禁用' : '生效中'}</td></tr>) : <tr><td colSpan={8} className="empty-row">当前 RouterOS 没有可展示的队列、队列树或带计数/标记的 mangle 策略</td></tr>}</tbody></table></div></section>
}

function RoutesPage(props: { routes: RouteStat[] }) {
  const [hideDisabled, setHideDisabled] = useState(true)
  const disabledCount = props.routes.filter((item) => item.disabled).length
  const visibleRoutes = hideDisabled ? props.routes.filter((item) => !item.disabled) : props.routes
  return <section className="panel compact-panel"><div className="data-toolbar"><strong>现有路由与分流状态</strong><span className="result-count">匹配数为当前 conntrack 快照推算</span><span className="toolbar-spacer" /><label className="toolbar-toggle"><input type="checkbox" checked={hideDisabled} onChange={(event) => setHideDisabled(event.target.checked)} /><span>隐藏已禁用</span></label><span>显示 {visibleRoutes.length} / {props.routes.length} 条{disabledCount ? `，已禁用 ${disabledCount}` : ''}</span></div><div className="table-scroll"><table className="data-table"><thead><tr><th>类型</th><th>IP</th><th>源地址 / 接口</th><th>目标网段</th><th>网关</th><th>路由表</th><th>动作</th><th>距离</th><th>当前匹配连接</th><th>状态</th></tr></thead><tbody>{visibleRoutes.length ? visibleRoutes.map((item, index) => <tr key={item.id || `${item.kind}-${item.destination}-${item.table}-${index}`}><td>{item.kind}</td><td>{item.family === 'ipv6' ? 'IPv6' : 'IPv4'}</td><td>{item.source || '-'}</td><td>{item.destination || '-'}</td><td>{item.gateway || '-'}</td><td>{item.table || 'main'}</td><td>{item.action || '-'}</td><td>{item.distance || '-'}</td><td>{item.currentMatches}</td><td>{item.disabled ? '已禁用' : item.kind === 'route' ? (item.active ? '活动' : '非活动') : '生效中'}</td></tr>) : <tr><td colSpan={10} className="empty-row">{props.routes.length ? '已隐藏全部禁用路由与分流规则' : '当前没有可读取的路由或分流状态'}</td></tr>}</tbody></table></div></section>
}

function TerminalsPage(props: {
  terminals: Terminal[]
  family: TerminalFamily
  query: string
  onQueryChange: (value: string) => void
  refreshMs: number
  onRefreshMsChange: (value: number) => void
  onRefresh: () => void
  onOpenDetail: (terminalID: string) => void
  onOpenRemark: (terminal: Terminal) => void
}) {
  const [sortKey, setSortKey] = useState<TerminalSortKey>('address')
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('asc')
  const [stateFilter, setStateFilter] = useState('online')
  const [interfaceFilter, setInterfaceFilter] = useState('all')
  const [pageSize, setPageSize] = useState(20)
  const [page, setPage] = useState(1)
  const interfaces = useMemo(() => Array.from(new Set(props.terminals.map((item) => item.primaryInterface).filter(Boolean))).sort(), [props.terminals])
  const sorted = useMemo(() => {
    const visible = props.terminals.filter((terminal) =>
      (stateFilter === 'all' || terminal.state === stateFilter) &&
      (interfaceFilter === 'all' || terminal.primaryInterface === interfaceFilter),
    )
    return [...visible].sort((left, right) => {
      const comparison = compareTerminal(left, right, sortKey, props.family)
      return sortDirection === 'asc' ? comparison : -comparison
    })
  }, [props.terminals, props.family, stateFilter, interfaceFilter, sortKey, sortDirection])
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const currentPage = Math.min(page, totalPages)
  const rows = sorted.slice((currentPage - 1) * pageSize, currentPage * pageSize)
  const showingInactive = stateFilter !== 'online'

  useEffect(() => setPage(1), [props.query, stateFilter, interfaceFilter, pageSize])

  const changeSort = (key: TerminalSortKey) => {
    if (sortKey === key) setSortDirection((value) => value === 'asc' ? 'desc' : 'asc')
    else {
      setSortKey(key)
      setSortDirection('asc')
    }
  }

  return (
    <section className="panel compact-panel">
      <div className="data-toolbar terminal-toolbar">
        <input className="search-input" value={props.query} onChange={(event) => props.onQueryChange(event.target.value)} placeholder="备注 / 名称 / IP / MAC" />
        <select className="terminal-state-filter" value={stateFilter} onChange={(event) => setStateFilter(event.target.value)} aria-label="终端状态">
          <option value="all">全部状态</option><option value="online">在线</option><option value="inactive">近期未活跃</option><option value="offline">离线</option>
        </select>
        <select className="terminal-interface-filter" value={interfaceFilter} onChange={(event) => setInterfaceFilter(event.target.value)} aria-label="接入接口">
          <option value="all">全部接口</option>{interfaces.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
        <button
          type="button"
          className={showingInactive ? 'toolbar-button terminal-presence-toggle active' : 'toolbar-button terminal-presence-toggle'}
          aria-pressed={showingInactive}
          onClick={() => setStateFilter(showingInactive ? 'online' : 'all')}
        >
          <span className="presence-label-full">{showingInactive ? '隐藏非在线设备' : '显示非在线设备'}</span>
          <span className="presence-label-mobile">{showingInactive ? '只看在线' : '非在线'}</span>
        </button>
        <span className="toolbar-spacer" />
        <span className="result-count">共 {sorted.length} 台</span>
        <select className="terminal-refresh-select" value={props.refreshMs} onChange={(event) => props.onRefreshMsChange(Number(event.target.value))} aria-label="自动刷新">
          <option value={0}>停止刷新</option><option value={1000}>1 秒刷新</option><option value={3000}>3 秒刷新</option><option value={5000}>5 秒刷新</option><option value={10000}>10 秒刷新</option>
        </select>
        <button type="button" className="toolbar-button terminal-refresh-button" onClick={props.onRefresh}>刷新</button>
      </div>

      <div className="table-scroll">
        <table className="data-table terminal-table">
          <thead><tr>
            <SortHeader label="设备名称" sortKey="device" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="IP" sortKey="address" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="连接数" sortKey="connections" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="上行速率" sortKey="upload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="下行速率" sortKey="download" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label={props.family === 'all' ? '累计上行' : '活动累计上行'} sortKey="totalUpload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label={props.family === 'all' ? '累计下行' : '活动累计下行'} sortKey="totalDownload" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="在线时长" sortKey="online" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <SortHeader label="备注" sortKey="remark" activeKey={sortKey} direction={sortDirection} onSort={changeSort} />
            <th>操作</th>
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
                <td><span className={`state-dot state-${terminal.state}`} />{terminal.state === 'online' ? formatOnlineDuration(terminal.onlineSince) : terminalStateText(terminal.state)}</td>
                <td>{terminal.remark || '-'}</td>
                <td><div className="action-links"><button type="button" className="link-button" onClick={() => props.onOpenDetail(terminal.id)}>详情</button><button type="button" className="link-button" onClick={() => props.onOpenRemark(terminal)}>编辑</button></div></td>
              </tr>
            })}
          </tbody>
        </table>
      </div>
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

type ConnectionFilterKey = 'family' | 'application' | 'protocol' | 'sourceAddress' | 'sourcePort' | 'destination' | 'routeTable' | 'gateway' | 'status' | 'search'
type ConnectionSortKey = 'family' | 'application' | 'protocol' | 'sourceAddress' | 'sourcePort' | 'destination' | 'destinationPort' | 'publicAddress' | 'upload' | 'download' | 'uploadBytes' | 'downloadBytes' | 'routeTable' | 'gateway' | 'status'

const unavailableRouteValue = '无法判断'

function connectionRouteTable(connection: TerminalConnection) {
  return connection.routeTable || unavailableRouteValue
}

function connectionGateway(connection: TerminalConnection) {
  return connection.routeGateways?.length ? connection.routeGateways.join(' / ') : unavailableRouteValue
}

function ConnectionColumnHeader(props: {
  label: string
  sortKey: ConnectionSortKey
  activeSort: ConnectionSortKey | null
  sortDirection: 'asc' | 'desc'
  filterKey?: ConnectionFilterKey
  filterActive?: boolean
  onSort: (key: ConnectionSortKey) => void
  onOpenFilter: (key: ConnectionFilterKey, anchor: HTMLElement) => void
}) {
  const sorting = props.activeSort === props.sortKey
  const filterKey = props.filterKey
  return <th><div className="connection-header-controls">
    <button type="button" className={sorting ? 'connection-sort-button active' : 'connection-sort-button'} aria-label={`${props.label}${sorting ? `，当前${props.sortDirection === 'asc' ? '升序' : '降序'}` : ''}，点击排序`} onClick={() => props.onSort(props.sortKey)}><span>{props.label}</span>{sorting ? <span className="connection-sort-indicator" aria-hidden="true">{props.sortDirection === 'asc' ? '↑' : '↓'}</span> : null}</button>
    {filterKey ? <button type="button" className={props.filterActive ? 'column-filter-button active' : 'column-filter-button'} aria-label={`筛选${props.label}`} aria-pressed={Boolean(props.filterActive)} onClick={(event) => props.onOpenFilter(filterKey, event.currentTarget)}><span aria-hidden="true">▾</span></button> : null}
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
    case 'publicAddress': return text(left.publicAddress, right.publicAddress)
    case 'upload': return left.uploadBps - right.uploadBps
    case 'download': return left.downloadBps - right.downloadBps
    case 'uploadBytes': return left.uploadBytes - right.uploadBytes
    case 'downloadBytes': return left.downloadBytes - right.downloadBytes
    case 'routeTable': return text(connectionRouteTable(left), connectionRouteTable(right))
    case 'gateway': return text(connectionGateway(left), connectionGateway(right))
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
  const [connectionQuery, setConnectionQuery] = useState('')
  const [applicationQuery, setApplicationQuery] = useState('')
  const [protocolFilter, setProtocolFilter] = useState('all')
  const [sourceAddressQuery, setSourceAddressQuery] = useState('')
  const [sourcePortQuery, setSourcePortQuery] = useState('')
  const [destinationQuery, setDestinationQuery] = useState('')
  const [routeTableFilter, setRouteTableFilter] = useState('all')
  const [gatewayFilter, setGatewayFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [activeConnectionFilter, setActiveConnectionFilter] = useState<ConnectionFilterKey | null>(null)
  const [connectionSortKey, setConnectionSortKey] = useState<ConnectionSortKey | null>(null)
  const [connectionSortDirection, setConnectionSortDirection] = useState<'asc' | 'desc'>('asc')
  const [filterPanelLeft, setFilterPanelLeft] = useState(7)
  const scopedConnectionRows = props.scope === 'all' ? props.connections : props.connections.filter((item) => item.family === props.scope)
  const selectedFamily = props.scope === 'all' ? props.family : props.scope
  const familyConnections = selectedFamily === 'all' ? scopedConnectionRows : scopedConnectionRows.filter((item) => item.family === selectedFamily)
  const ipv4Connections = scopedConnectionRows.filter((item) => item.family === 'ipv4')
  const ipv6Connections = scopedConnectionRows.filter((item) => item.family === 'ipv6')
  const applications = Array.from(new Set(familyConnections.map((item) => item.application).filter(Boolean))).sort()
  const protocols = Array.from(new Set(familyConnections.map((item) => item.protocol))).sort()
  const routeTables = Array.from(new Set(familyConnections.map(connectionRouteTable))).sort()
  const gateways = Array.from(new Set(familyConnections.flatMap((item) => item.routeGateways?.length ? item.routeGateways : [unavailableRouteValue]))).sort()
  const statuses = Array.from(new Set(familyConnections.map((item) => item.status).filter(Boolean))).sort()
  const normalizedGlobalQuery = connectionQuery.trim().toLowerCase()
  const filteredConnections = familyConnections.filter((connection) =>
    (protocolFilter === 'all' || connection.protocol === protocolFilter) &&
      (routeTableFilter === 'all' || connectionRouteTable(connection) === routeTableFilter) &&
      (gatewayFilter === 'all' || (gatewayFilter === unavailableRouteValue ? !connection.routeGateways?.length : connection.routeGateways?.includes(gatewayFilter))) &&
      (!props.showStatus || statusFilter === 'all' || connection.status === statusFilter) &&
      (!applicationQuery || connection.application === applicationQuery) &&
      connection.sourceAddress.toLowerCase().includes(sourceAddressQuery.trim().toLowerCase()) &&
      connection.sourcePort.toLowerCase().includes(sourcePortQuery.trim().toLowerCase()) &&
      [connection.destinationAddress, connection.destinationPort].join(' ').toLowerCase().includes(destinationQuery.trim().toLowerCase()) &&
      [connection.application, connection.protocol, connectionRouteTable(connection), connectionGateway(connection), connection.sourceAddress, connection.sourcePort, connection.destinationAddress, connection.destinationPort, connection.publicAddress, connection.connectionMark]
        .join(' ').toLowerCase().includes(normalizedGlobalQuery)
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
    status: props.showStatus && statusFilter !== 'all',
    search: Boolean(connectionQuery),
  }
  const openConnectionFilter = (key: ConnectionFilterKey, anchor: HTMLElement) => {
    const shell = anchor.closest('.connection-table-shell')
    if (shell) {
      const shellRect = shell.getBoundingClientRect()
      const anchorRect = anchor.getBoundingClientRect()
      const preferredWidth = shellRect.width < 600 ? 240 : 300
      const panelWidth = Math.min(preferredWidth, Math.max(220, shellRect.width - 14))
      setFilterPanelLeft(Math.max(7, Math.min(anchorRect.left - shellRect.left, shellRect.width - panelWidth - 7)))
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
  const clearConnectionTableState = () => {
    props.onFamilyChange(props.scope === 'all' ? 'all' : props.scope)
    setApplicationQuery('')
    setProtocolFilter('all')
    setSourceAddressQuery('')
    setSourcePortQuery('')
    setDestinationQuery('')
    setRouteTableFilter('all')
    setGatewayFilter('all')
    setStatusFilter('all')
    setConnectionQuery('')
    setConnectionSortKey(null)
    setConnectionSortDirection('asc')
    setActiveConnectionFilter(null)
  }
  const chooseConnectionFilter = (apply: () => void) => {
    apply()
    setActiveConnectionFilter(null)
  }
  return {
    activeConnectionFilter, activeSort: connectionSortKey, applications, changeConnectionSort, chooseConnectionFilter,
    clearConnectionTableState, connectionQuery, familyFilterable: props.scope === 'all', filterActive, filterPanelLeft, gateways, hasState: connectionSortKey !== null || Object.values(filterActive).some(Boolean),
    ipv4Connections, ipv6Connections, onFamilyChange: props.onFamilyChange, openConnectionFilter, protocols, routeTables, scopedConnectionRows, selectedFamily,
    setActiveConnectionFilter, setApplicationQuery, setConnectionQuery, setDestinationQuery, setFilterPanelLeft, setGatewayFilter,
    setProtocolFilter, setRouteTableFilter, setSourceAddressQuery, setSourcePortQuery, setStatusFilter,
    sortDirection: connectionSortDirection, sourceAddressQuery, sourcePortQuery, applicationQuery, destinationQuery, gatewayFilter,
    protocolFilter, routeTableFilter, statusFilter, statuses, visibleConnections,
  }
}

type ConnectionTableState = ReturnType<typeof useConnectionTableState>

function ConnectionMobileActions(props: { state: ConnectionTableState }) {
  return <div className="connection-mobile-actions">
    <button type="button" className="table-clear-button" aria-label="清除全部筛选和排序" disabled={!props.state.hasState} onClick={props.state.clearConnectionTableState}><Icon name="clear" /></button>
    <button type="button" className={props.state.filterActive.search ? 'table-search-button active' : 'table-search-button'} aria-label="搜索全部连接字段" aria-expanded={props.state.activeConnectionFilter === 'search'} onClick={() => { props.state.setFilterPanelLeft(7); props.state.setActiveConnectionFilter((value) => value === 'search' ? null : 'search') }}><Icon name="search" /></button>
  </div>
}

function ConnectionTable(props: { state: ConnectionTableState; showStatus: boolean; emptyLabel: string }) {
  const state = props.state
  const filterPanel = state.activeConnectionFilter ? <div className="connection-filter-panel" role="dialog" aria-label="连接筛选" style={{ left: state.filterPanelLeft }}>
    <div className="connection-filter-panel-head"><strong>{state.activeConnectionFilter === 'search' ? '搜索全部连接字段' : '筛选连接'}</strong><button type="button" className="link-button" onClick={() => state.setActiveConnectionFilter(null)}>关闭</button></div>
    {state.activeConnectionFilter === 'family' ? <ConnectionFilterOptions value={state.selectedFamily} options={[{ value: 'all', label: `全部 (${state.scopedConnectionRows.length})` }, { value: 'ipv4', label: `IPv4 (${state.ipv4Connections.length})` }, { value: 'ipv6', label: `IPv6 (${state.ipv6Connections.length})` }]} onChange={(value) => state.chooseConnectionFilter(() => state.onFamilyChange(value as ConnectionFamily))} /> : null}
    {state.activeConnectionFilter === 'application' ? <ConnectionFilterOptions value={state.applicationQuery} options={[{ value: '', label: '全部应用' }, ...state.applications.map((application) => ({ value: application, label: application }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setApplicationQuery(value))} /> : null}
    {state.activeConnectionFilter === 'protocol' ? <ConnectionFilterOptions value={state.protocolFilter} options={[{ value: 'all', label: '全部协议' }, ...state.protocols.map((protocol) => ({ value: protocol, label: protocol }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setProtocolFilter(value))} /> : null}
    {state.activeConnectionFilter === 'sourceAddress' ? <input value={state.sourceAddressQuery} onChange={(event) => state.setSourceAddressQuery(event.target.value)} placeholder="来源 IP" aria-label="来源 IP 筛选" /> : null}
    {state.activeConnectionFilter === 'sourcePort' ? <input value={state.sourcePortQuery} onChange={(event) => state.setSourcePortQuery(event.target.value)} placeholder="来源端口" aria-label="来源端口筛选" /> : null}
    {state.activeConnectionFilter === 'destination' ? <input value={state.destinationQuery} onChange={(event) => state.setDestinationQuery(event.target.value)} placeholder="目的 IP 或端口" aria-label="目的地址筛选" /> : null}
    {state.activeConnectionFilter === 'routeTable' ? <ConnectionFilterOptions value={state.routeTableFilter} options={[{ value: 'all', label: '全部路由表' }, ...state.routeTables.map((table) => ({ value: table, label: table }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setRouteTableFilter(value))} /> : null}
    {state.activeConnectionFilter === 'gateway' ? <ConnectionFilterOptions value={state.gatewayFilter} options={[{ value: 'all', label: '全部网关' }, ...state.gateways.map((gateway) => ({ value: gateway, label: gateway }))]} onChange={(value) => state.chooseConnectionFilter(() => state.setGatewayFilter(value))} /> : null}
    {state.activeConnectionFilter === 'status' ? <select value={state.statusFilter} onChange={(event) => state.setStatusFilter(event.target.value)} aria-label="连接状态筛选"><option value="all">全部状态</option>{state.statuses.map((status) => <option key={status} value={status}>{status}</option>)}</select> : null}
    {state.activeConnectionFilter === 'search' ? <input value={state.connectionQuery} onChange={(event) => state.setConnectionQuery(event.target.value)} placeholder="地址 / 端口 / 应用 / 标记" aria-label="搜索全部连接字段" /> : null}
  </div> : null
  const columnCount = 14 + Number(props.showStatus)
  return <div className="connection-table-shell">
    <button type="button" className="table-clear-button" aria-label="清除全部筛选和排序" disabled={!state.hasState} onClick={state.clearConnectionTableState}><Icon name="clear" /></button>
    <button type="button" className={state.filterActive.search ? 'table-search-button active' : 'table-search-button'} aria-label="搜索全部连接字段" aria-expanded={state.activeConnectionFilter === 'search'} onClick={(event) => state.openConnectionFilter('search', event.currentTarget)}><Icon name="search" /></button>
    {filterPanel}
    <div className="table-scroll"><table className="data-table connection-table"><thead><tr>
      <ConnectionColumnHeader label="IP版本" sortKey="family" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey={state.familyFilterable ? 'family' : undefined} filterActive={state.filterActive.family} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="应用" sortKey="application" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="application" filterActive={state.filterActive.application} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="协议" sortKey="protocol" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="protocol" filterActive={state.filterActive.protocol} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="来源 IP" sortKey="sourceAddress" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="sourceAddress" filterActive={state.filterActive.sourceAddress} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="来源端口" sortKey="sourcePort" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="sourcePort" filterActive={state.filterActive.sourcePort} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="目的地址" sortKey="destination" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="destination" filterActive={state.filterActive.destination} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="目的端口" sortKey="destinationPort" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="外网地址" sortKey="publicAddress" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="当前上行" sortKey="upload" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="当前下行" sortKey="download" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="累计上行" sortKey="uploadBytes" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="累计下行" sortKey="downloadBytes" activeSort={state.activeSort} sortDirection={state.sortDirection} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="路由表" sortKey="routeTable" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="routeTable" filterActive={state.filterActive.routeTable} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      <ConnectionColumnHeader label="下一跳网关" sortKey="gateway" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="gateway" filterActive={state.filterActive.gateway} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} />
      {props.showStatus ? <ConnectionColumnHeader label="连接状态" sortKey="status" activeSort={state.activeSort} sortDirection={state.sortDirection} filterKey="status" filterActive={state.filterActive.status} onSort={state.changeConnectionSort} onOpenFilter={state.openConnectionFilter} /> : null}
    </tr></thead><tbody>{state.visibleConnections.length ? state.visibleConnections.map((connection) => (
      <tr key={connection.key}>
        <td><span className={`ip-family-badge ${connection.family}`}>{connection.family === 'ipv4' ? 'IPv4' : 'IPv6'}</span></td>
        <td>{connection.application}</td><td>{connection.protocol}</td><td>{connection.sourceAddress || '-'}</td><td>{connection.sourcePort || '-'}</td>
        <td>{connection.destinationAddress || '-'}</td><td>{connection.destinationPort || '-'}</td><td>{connection.publicAddress || '-'}</td>
        <td>{formatBits(connection.uploadBps)}</td><td>{formatBits(connection.downloadBps)}</td>
        <td>{formatBytes(connection.uploadBytes)}</td><td>{formatBytes(connection.downloadBytes)}</td>
        <td>{connectionRouteTable(connection)}</td><td>{connectionGateway(connection)}</td>{props.showStatus ? <td>{connection.status}</td> : null}
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
  const repliedConnections = scopedConnections.filter((item) => item.seenReply).length
  const unrepliedConnections = scopedConnections.length - repliedConnections
  const summary = props.scope === 'all' ? props.detail.terminal : (props.detail.familySummaries?.[props.scope] ?? props.detail.terminal)
  const visibleFlows = props.scope === 'all' ? props.detail.flowCategories : (props.detail.familyFlows?.[props.scope] ?? [])
  const connectionTable = useConnectionTableState({ connections: props.detail.connections, scope: props.scope, family: props.connectionFamily, onFamilyChange: props.onConnectionFamilyChange, showStatus: isRouterConntrack })

  return (
    <section className="detail-page">
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
          </div>
        </div>
        <div className="detail-head-actions">
          <button type="button" className="close-button" onClick={props.onBack}>
            返回
          </button>
        </div>
      </div>

      <div className={`tab-row detail-tabs${props.scope === 'all' ? ' has-history' : ''}${props.activeTab === 'connections' ? ' with-mobile-actions' : ''}`}>
        <TabButton label="基础信息" active={props.activeTab === 'basic'} onClick={() => props.onTabChange('basic')} />
        <TabButton label={isRouterConntrack ? '跟踪详情' : '连接详情'} active={props.activeTab === 'connections'} onClick={() => props.onTabChange('connections')} />
        <TabButton label="流量分布" active={props.activeTab === 'flows'} onClick={() => props.onTabChange('flows')} />
        {props.scope === 'all' ? <TabButton label="历史记录" active={props.activeTab === 'history'} onClick={() => props.onTabChange('history')} /> : null}
        {props.activeTab === 'connections' ? <ConnectionMobileActions state={connectionTable} /> : null}
      </div>

      <section className="panel detail-panel">
        {props.activeTab === 'basic' ? (
          <div className="detail-grid">
            <DetailItem label="设备名称" value={summary.displayName} />
            <DetailItem label="自动识别名称" value={summary.autoName || '暂未识别'} />
            {props.scope !== 'ipv6' ? <DetailItem label="IPv4 地址" value={summary.ipv4.join(' / ') || '-'} /> : null}
            {props.scope !== 'ipv4' ? <DetailItem label="IPv6 地址" value={summary.ipv6.join(' / ') || '-'} /> : null}
            <DetailItem label="MAC 地址" value={summary.macAddress || '-'} />
            <DetailItem label="接入接口" value={summary.primaryInterface || '-'} />
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
