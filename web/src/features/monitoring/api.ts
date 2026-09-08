/**
 * Monitoring feature API layer. All network data is treated as `unknown` and
 * parsed with the shared guards (type-safety spec); pages never fetch directly.
 */

import {
  ApiError,
  apiGet,
  apiPost,
  errorMessage,
  safeArray,
  safeBoolean,
  safeNumber,
  safeObject,
  safeString,
  safeStringArray,
  scoped,
} from '../../lib/api'
import {
  parseInterfaceStatus,
  parseOverview,
  parseRateSample,
  parseTerminal,
  type ChartWindow,
  type InterfaceStatus,
  type Overview,
  type Terminal,
} from '../../lib/types'
import type {
  FleetDevice,
  FleetOverview,
  OnboardingComplete,
  OnboardingPreview,
  OnboardingSession,
  SettingsSummary,
  TrafficHistory,
} from './types'

export type {
  FleetDevice,
  FleetOverview,
  OnboardingComplete,
  OnboardingPreview,
  OnboardingSession,
  SettingsSummary,
  TrafficHistory,
} from './types'

/* ---------- parsers ---------- */

function parseFleetDevice(value: unknown): FleetDevice {
  const o = safeObject(value)
  return {
    id: safeString(o.id),
    name: safeString(o.name),
    state: safeString(o.state) || 'offline',
    alerting: safeBoolean(o.alerting),
    error: safeString(o.error) || undefined,
    routerName: safeString(o.routerName),
    platform: safeString(o.platform),
    boardName: safeString(o.boardName),
    version: safeString(o.version),
    address: safeString(o.address),
    cpuLoadPercent: safeNumber(o.cpuLoadPercent),
    memoryUsedPercent: safeNumber(o.memoryUsedPercent),
    uploadBps: safeNumber(o.uploadBps),
    downloadBps: safeNumber(o.downloadBps),
    terminalCount: safeNumber(o.terminalCount),
    terminalOnline: safeNumber(o.terminalOnline),
    terminalInactive: safeNumber(o.terminalInactive),
    terminalOffline: safeNumber(o.terminalOffline),
    connectionCount: safeNumber(o.connectionCount),
    uptime: safeString(o.uptime),
    updatedAt: safeString(o.updatedAt),
  }
}

function parseFleetOverview(value: unknown): FleetOverview {
  const o = safeObject(value)
  return {
    totalDevices: safeNumber(o.totalDevices),
    onlineDevices: safeNumber(o.onlineDevices),
    offlineDevices: safeNumber(o.offlineDevices),
    alertDevices: safeNumber(o.alertDevices),
    devices: safeArray<unknown>(o.devices).map(parseFleetDevice),
  }
}

function parseTrafficHistory(value: unknown): TrafficHistory {
  const o = safeObject(value)
  const rawWindow = safeString(o.window)
  const window: ChartWindow = rawWindow === '1h' || rawWindow === '6h' || rawWindow === '24h' ? rawWindow : '5m'
  return {
    window,
    samples: safeArray<unknown>(o.samples).map(parseRateSample),
    trafficInterfaces: safeStringArray(o.trafficInterfaces),
  }
}

function parseTerminals(value: unknown): Terminal[] {
  return safeArray<unknown>(safeObject(value).terminals).map(parseTerminal)
}

function parseInterfaces(value: unknown): InterfaceStatus[] {
  return safeArray<unknown>(safeObject(value).interfaces).map(parseInterfaceStatus)
}

function parseOnboardingSession(value: unknown): OnboardingSession {
  const o = safeObject(value)
  const connection = safeObject(o.connection)
  return {
    sessionId: safeString(o.sessionId),
    script: safeString(o.script),
    expiresAt: safeString(o.expiresAt),
    username: safeString(o.username),
    connection: {
      scheme: safeString(connection.scheme) || 'http',
      host: safeString(connection.host),
      port: safeNumber(connection.port),
    },
  }
}

function parseOnboardingPreview(value: unknown): OnboardingPreview {
  const o = safeObject(value)
  const identity = safeObject(o.identity)
  const trafficScope = safeObject(o.trafficScope)
  return {
    verificationToken: safeString(o.verificationToken),
    expiresAt: safeString(o.expiresAt),
    identity: {
      routerName: safeString(identity.routerName),
      version: safeString(identity.version),
      platform: safeString(identity.platform),
      boardName: safeString(identity.boardName),
    },
    trafficInterfaces: safeArray<unknown>(trafficScope.interfaces).map((item) => safeString(safeObject(item).name)).filter(Boolean),
    cidrCandidateCount: safeArray<unknown>(o.cidrCandidates).length,
    warnings: safeArray<unknown>(o.warnings).map((item) => {
      const warning = safeObject(item)
      return { capability: safeString(warning.capability), message: safeString(warning.message) }
    }),
  }
}

function parseOnboardingComplete(value: unknown): OnboardingComplete {
  const o = safeObject(value)
  return { id: safeString(o.id), restarting: safeBoolean(o.restarting) }
}

function parseSettingsSummary(value: unknown): SettingsSummary {
  const o = safeObject(value)
  const collection = safeObject(o.collection)
  const rawInterval = collection.realtimePollIntervalSeconds
  const interval = typeof rawInterval === 'number' && Number.isFinite(rawInterval) && rawInterval > 0 ? rawInterval : null
  return { deviceCount: safeArray<unknown>(o.devices).length, realtimePollIntervalSeconds: interval }
}

function parseSetupComplete(value: unknown): { restarting: boolean } {
  return { restarting: safeBoolean(safeObject(value).restarting) }
}

/* ---------- monitoring reads ---------- */

export function fetchFleetOverview(): Promise<FleetOverview> {
  return apiGet('/api/fleet-overview', parseFleetOverview)
}

export function fetchRealtime(deviceId: string): Promise<Overview> {
  return apiGet(scoped('/api/realtime', deviceId), parseOverview)
}

export function fetchTrafficHistory(deviceId: string, window: ChartWindow): Promise<TrafficHistory> {
  return apiGet(scoped(`/api/traffic-history?window=${window}`, deviceId), parseTrafficHistory)
}

export function fetchTerminals(deviceId: string): Promise<Terminal[]> {
  return apiGet(scoped('/api/terminals', deviceId), parseTerminals)
}

export function fetchInterfaces(deviceId: string): Promise<InterfaceStatus[]> {
  return apiGet(scoped('/api/interfaces', deviceId), parseInterfaces)
}

export function fetchSettingsSummary(): Promise<SettingsSummary> {
  return apiGet('/api/settings', parseSettingsSummary)
}

/* ---------- bootstrap-phase writes (setup / login) ---------- */

export function createAdminAccount(input: { username: string; password: string; passwordConfirmation: string }): Promise<unknown> {
  return apiPost('/api/setup/admin', {
    username: input.username.trim(),
    password: input.password,
    passwordConfirmation: input.passwordConfirmation,
  })
}

export type LoginResult = { phase: string; username: string }

/**
 * Login needs the raw response: HTTP 429 carries a Retry-After header that the
 * shared request helper does not surface, so this performs its own fetch and
 * rethrows ApiError with `details.retryAfterSeconds`.
 */
export async function login(username: string, password: string): Promise<LoginResult> {
  let response: Response
  try {
    response = await fetch('/api/auth/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: username.trim(), password }),
    })
  } catch {
    throw new ApiError('网络请求失败，请检查面板连接', 0, 'network_error')
  }
  const text = await response.text()
  let payload: unknown = null
  if (text.trim()) {
    try {
      payload = JSON.parse(text)
    } catch {
      throw new ApiError('服务返回了无法解析的响应', response.status, 'invalid_response')
    }
  }
  const envelope = safeObject(payload)
  if (!response.ok) {
    const retryAfter = Number(response.headers.get('Retry-After'))
    const details = response.status === 429 && Number.isFinite(retryAfter) && retryAfter > 0 ? { retryAfterSeconds: retryAfter } : undefined
    throw new ApiError(
      safeString(envelope.error) || `请求失败（HTTP ${response.status}）`,
      response.status,
      safeString(envelope.code) || undefined,
      details,
    )
  }
  return { phase: safeString(envelope.phase), username: safeString(envelope.username) }
}

/** 暂不添加 RouterOS，直接完成初始化（或已有设备时完成并重启采集）。 */
export function completeSetup(skipRouterOS: boolean): Promise<{ restarting: boolean }> {
  return apiPost('/api/setup/complete', { skipRouterOS }, parseSetupComplete)
}

/* ---------- quick onboarding session flow ---------- */

export function createOnboardingSession(input: { name: string; host: string; scheme: string; port: number }): Promise<OnboardingSession> {
  return apiPost('/api/device-onboarding/sessions', input, parseOnboardingSession)
}

export function previewOnboardingSession(sessionId: string): Promise<OnboardingPreview> {
  return apiPost(
    `/api/device-onboarding/sessions/${encodeURIComponent(sessionId)}/preview`,
    { trafficScope: { mode: 'auto' }, terminalScope: { mode: 'auto' } },
    parseOnboardingPreview,
  )
}

export function completeOnboardingSession(sessionId: string, verificationToken: string): Promise<OnboardingComplete> {
  return apiPost(
    `/api/device-onboarding/sessions/${encodeURIComponent(sessionId)}/complete`,
    {
      verificationToken,
      trafficScope: { mode: 'auto' },
      terminalScope: { mode: 'auto' },
      completeOnboarding: true,
    },
    parseOnboardingComplete,
  )
}

/** Bootstrap-phase health probe used by the post-restart wait loop. */
export function probeBootstrap(): Promise<unknown> {
  return apiGet('/api/bootstrap')
}

/* ---------- error wording ---------- */

/**
 * Chinese display message for known API error codes; falls back to the server
 * message (already Chinese for RouterOS probe failures) or a generic text.
 */
export function friendlyError(error: unknown, fallback = '操作失败，请稍后重试'): string {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'admin_exists':
        return '管理员账号已存在，请直接登录'
      case 'invalid_credentials':
        return '用户名或密码不正确'
      case 'login_rate_limited':
        return '登录尝试过于频繁，请稍后再试'
      case 'setup_required':
        return '请先创建管理员账号'
      case 'routeros_required':
        return '请先保存一台 RouterOS 设备，或选择暂不添加'
      case 'provisioning_expired':
        return '接入脚本已过期，请重新生成'
      case 'verification_required':
        return '连接检测已失效，请重新执行「我已执行脚本」'
      case 'invalid_admin':
        return '管理员信息不符合要求，请检查用户名与密码'
      case 'network_error':
        return '网络请求失败，请检查面板连接'
    }
  }
  return errorMessage(error, fallback)
}

/** Retry-After seconds carried by a 429 ApiError (default 30s). */
export function retryAfterSeconds(error: unknown): number {
  if (error instanceof ApiError && error.status === 429) {
    const details = safeObject(error.details)
    const seconds = safeNumber(details.retryAfterSeconds)
    if (seconds > 0) return Math.ceil(seconds)
  }
  return 30
}
