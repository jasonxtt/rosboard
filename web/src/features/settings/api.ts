/**
 * Settings feature API layer. Every endpoint used by the settings and
 * recognition pages lives here; payloads arrive as `unknown` and are parsed
 * with the shared guards (.trellis/spec/frontend/type-safety.md).
 *
 * Contract gotchas (backend truth: internal/api/server.go, verification.go,
 * provisioning.go):
 * - `trafficScope` / `terminalScope` *config* payloads use snake_case inner
 *   keys (`include_interfaces`, `include_cidrs`, …).
 * - Verification/preview *results* use the camelCase model shapes
 *   (`interfaces`, `prefixes`, `overridesApplied`, …).
 * - Device/collection/recognition mutations answer `{ restarting: boolean }`;
 *   when true the panel restarts and the page must poll /api/health.
 */

import {
  apiDelete,
  apiGet,
  apiPost,
  apiPut,
  AUTH_REQUIRED_EVENT,
  ApiError,
  safeArray,
  safeBoolean,
  safeNumber,
  safeObject,
  safeString,
  safeStringArray,
} from '../../lib/api'

/**
 * The shared apiDelete helper has no body support, but
 * `DELETE /api/devices/{id}/data` requires the typed confirmation in a JSON
 * body. Kept inside the feature API layer so components still never fetch.
 */
async function apiDeleteWithBody<T>(path: string, body: unknown, parse?: (value: unknown) => T): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
  } catch {
    throw new ApiError('网络请求失败，请检查面板连接', 0, 'network_error')
  }
  if (response.status === 401) window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT))
  const text = await response.text()
  let payload: unknown = null
  if (text.trim()) {
    try {
      payload = JSON.parse(text)
    } catch {
      throw new ApiError('服务返回了无法解析的响应', response.status, 'invalid_response')
    }
  }
  if (!response.ok) {
    const envelope = safeObject(payload)
    const message = safeString(envelope.error) || `请求失败（HTTP ${response.status}）`
    throw new ApiError(message, response.status, safeString(envelope.code) || undefined, envelope.details)
  }
  return parse ? parse(payload) : (payload as T)
}

/* ---------- scope config (snake_case, persisted form) ---------- */

export type TrafficScopeConfig = {
  mode?: string
  include_interfaces: string[]
  exclude_interfaces: string[]
}

export type TerminalScopeConfig = {
  mode?: string
  include_interfaces: string[]
  exclude_interfaces: string[]
  include_cidrs: string[]
  exclude_cidrs: string[]
}

/* ---------- scope preview (camelCase, runtime/verification form) ---------- */

export type TrafficScopeInterface = {
  name: string
  kind: string
  reasons: string[]
  automatic: boolean
  running: boolean
  disabled: boolean
}

export type TrafficScopePreview = {
  mode: string
  legacy: boolean
  interfaces: TrafficScopeInterface[]
  warnings: string[]
  overridesApplied: boolean
}

export type TerminalScopeInterface = {
  name: string
  role: string
  confidence: string
  reasons: string[]
}

export type TerminalScopePrefix = {
  cidr: string
  family: string
  interface: string
  source: string
  automatic: boolean
}

export type TerminalScopePreview = {
  mode: string
  legacy: boolean
  interfaces: TerminalScopeInterface[]
  prefixes: TerminalScopePrefix[]
  warnings: string[]
  overridesApplied: boolean
}

/* ---------- settings response ---------- */

export type SettingsDeviceMosDNS = {
  enabled: boolean
  baseUrl: string
  syncIntervalMinutes: number
  matchWindowMinutes: number
}

export type SettingsDevice = {
  id: string
  name: string
  enabled: boolean
  archived: boolean
  scheme: 'http' | 'https'
  host: string
  port: number
  username: string
  passwordSet: boolean
  cleanupAvailable: boolean
  trafficInterfaces: string[]
  trafficScope: TrafficScopeConfig
  terminalCidrs: string[]
  terminalScope: TerminalScopeConfig
  protocolAnalysis: boolean
  mosdns: SettingsDeviceMosDNS
}

export type CollectionSettings = {
  pollIntervalSeconds: number
  realtimePollIntervalSeconds: number
  terminalPollIntervalSeconds: number
  sampleRetentionHours: number
}

export type SettingsResponse = {
  collection: CollectionSettings
  devices: SettingsDevice[]
}

/* ---------- verification / provisioning ---------- */

export type VerificationIdentity = {
  routerName: string
  version: string
  platform: string
  boardName: string
}

export type VerificationInterface = {
  name: string
  type: string
  running: boolean
  disabled: boolean
  addresses: string[]
}

export type VerificationCIDR = { cidr: string; interface: string; family: string }

export type VerificationWarning = { capability: string; message: string }

export type VerificationResult = {
  verificationToken: string
  expiresAt: string
  identity: VerificationIdentity
  interfaces: VerificationInterface[]
  cidrCandidates: VerificationCIDR[]
  trafficScope: TrafficScopePreview
  terminalScope: TerminalScopePreview
  warnings: VerificationWarning[]
}

export type ProvisioningSession = {
  sessionId: string
  script: string
  expiresAt: string
  username: string
  connection: { scheme: 'http' | 'https'; host: string; port: number }
}

/* ---------- account / cleanup / recognition ---------- */

export type DeviceAccountStatus = {
  username: string
  group: string
  policies: string[]
  permission: 'write' | 'read_only' | 'unknown'
  writeAccess: boolean
  error?: string
}

export type RouterOSCleanup = {
  deviceId: string
  name: string
  username: string
  groupName: string
  script: string
}

export type MosDNSStatus = {
  enabled: boolean
  baseUrl: string
  syncIntervalMinutes: number
  matchWindowMinutes: number
  lastAttempt?: string
  lastSuccess?: string
  lastImported: number
  lastDuplicates: number
  lastSkipped: number
  watermark?: string
  learnedFeatureCount: number
  learnedFeatureLastSeen?: string
  lastError?: string
}

export type RecognitionStatus = {
  protocolAnalysis: boolean
  mosdns: MosDNSStatus
}

export type MutationResult = { restarting: boolean }

/* ---------- parsers ---------- */

function parseTrafficScopeConfig(value: unknown): TrafficScopeConfig {
  const o = safeObject(value)
  return {
    mode: safeString(o.mode) || undefined,
    include_interfaces: safeStringArray(o.include_interfaces),
    exclude_interfaces: safeStringArray(o.exclude_interfaces),
  }
}

function parseTerminalScopeConfig(value: unknown): TerminalScopeConfig {
  const o = safeObject(value)
  return {
    mode: safeString(o.mode) || undefined,
    include_interfaces: safeStringArray(o.include_interfaces),
    exclude_interfaces: safeStringArray(o.exclude_interfaces),
    include_cidrs: safeStringArray(o.include_cidrs),
    exclude_cidrs: safeStringArray(o.exclude_cidrs),
  }
}

function parseTrafficScopePreview(value: unknown): TrafficScopePreview {
  const o = safeObject(value)
  return {
    mode: safeString(o.mode),
    legacy: safeBoolean(o.legacy),
    interfaces: safeArray<unknown>(o.interfaces).map((raw) => {
      const item = safeObject(raw)
      return {
        name: safeString(item.name),
        kind: safeString(item.kind),
        reasons: safeStringArray(item.reasons),
        automatic: safeBoolean(item.automatic),
        running: safeBoolean(item.running),
        disabled: safeBoolean(item.disabled),
      }
    }),
    warnings: safeStringArray(o.warnings),
    overridesApplied: safeBoolean(o.overridesApplied),
  }
}

function parseTerminalScopePreview(value: unknown): TerminalScopePreview {
  const o = safeObject(value)
  return {
    mode: safeString(o.mode),
    legacy: safeBoolean(o.legacy),
    interfaces: safeArray<unknown>(o.interfaces).map((raw) => {
      const item = safeObject(raw)
      return {
        name: safeString(item.name),
        role: safeString(item.role),
        confidence: safeString(item.confidence),
        reasons: safeStringArray(item.reasons),
      }
    }),
    prefixes: safeArray<unknown>(o.prefixes).map((raw) => {
      const item = safeObject(raw)
      return {
        cidr: safeString(item.cidr),
        family: safeString(item.family),
        interface: safeString(item.interface),
        source: safeString(item.source),
        automatic: safeBoolean(item.automatic),
      }
    }),
    warnings: safeStringArray(o.warnings),
    overridesApplied: safeBoolean(o.overridesApplied),
  }
}

export function parseSettingsDevice(value: unknown): SettingsDevice {
  const o = safeObject(value)
  const mosdns = safeObject(o.mosdns)
  return {
    id: safeString(o.id),
    name: safeString(o.name),
    enabled: safeBoolean(o.enabled),
    archived: safeBoolean(o.archived),
    scheme: safeString(o.scheme) === 'https' ? 'https' : 'http',
    host: safeString(o.host),
    port: safeNumber(o.port),
    username: safeString(o.username),
    passwordSet: safeBoolean(o.passwordSet),
    cleanupAvailable: safeBoolean(o.cleanupAvailable),
    trafficInterfaces: safeStringArray(o.trafficInterfaces),
    trafficScope: parseTrafficScopeConfig(o.trafficScope),
    terminalCidrs: safeStringArray(o.terminalCidrs),
    terminalScope: parseTerminalScopeConfig(o.terminalScope),
    protocolAnalysis: safeBoolean(o.protocolAnalysis),
    mosdns: {
      enabled: safeBoolean(mosdns.enabled),
      baseUrl: safeString(mosdns.baseUrl),
      syncIntervalMinutes: safeNumber(mosdns.syncIntervalMinutes),
      matchWindowMinutes: safeNumber(mosdns.matchWindowMinutes),
    },
  }
}

export function parseSettings(value: unknown): SettingsResponse {
  const o = safeObject(value)
  const collection = safeObject(o.collection)
  return {
    collection: {
      pollIntervalSeconds: safeNumber(collection.pollIntervalSeconds),
      realtimePollIntervalSeconds: safeNumber(collection.realtimePollIntervalSeconds),
      terminalPollIntervalSeconds: safeNumber(collection.terminalPollIntervalSeconds),
      sampleRetentionHours: safeNumber(collection.sampleRetentionHours),
    },
    devices: safeArray<unknown>(o.devices).map(parseSettingsDevice),
  }
}

export function parseVerificationResult(value: unknown): VerificationResult {
  const o = safeObject(value)
  const identity = safeObject(o.identity)
  return {
    verificationToken: safeString(o.verificationToken),
    expiresAt: safeString(o.expiresAt),
    identity: {
      routerName: safeString(identity.routerName),
      version: safeString(identity.version),
      platform: safeString(identity.platform),
      boardName: safeString(identity.boardName),
    },
    interfaces: safeArray<unknown>(o.interfaces).map((raw) => {
      const item = safeObject(raw)
      return {
        name: safeString(item.name),
        type: safeString(item.type),
        running: safeBoolean(item.running),
        disabled: safeBoolean(item.disabled),
        addresses: safeStringArray(item.addresses),
      }
    }),
    cidrCandidates: safeArray<unknown>(o.cidrCandidates).map((raw) => {
      const item = safeObject(raw)
      return { cidr: safeString(item.cidr), interface: safeString(item.interface), family: safeString(item.family) }
    }),
    trafficScope: parseTrafficScopePreview(o.trafficScope),
    terminalScope: parseTerminalScopePreview(o.terminalScope),
    warnings: safeArray<unknown>(o.warnings).map((raw) => {
      const item = safeObject(raw)
      return { capability: safeString(item.capability), message: safeString(item.message) }
    }),
  }
}

export function parseProvisioningSession(value: unknown): ProvisioningSession {
  const o = safeObject(value)
  const connection = safeObject(o.connection)
  return {
    sessionId: safeString(o.sessionId),
    script: safeString(o.script),
    expiresAt: safeString(o.expiresAt),
    username: safeString(o.username),
    connection: {
      scheme: safeString(connection.scheme) === 'https' ? 'https' : 'http',
      host: safeString(connection.host),
      port: safeNumber(connection.port),
    },
  }
}

export function parseDeviceAccountStatus(value: unknown): DeviceAccountStatus {
  const o = safeObject(value)
  const rawPermission = safeString(o.permission)
  return {
    username: safeString(o.username),
    group: safeString(o.group),
    policies: safeStringArray(o.policies),
    permission: rawPermission === 'write' || rawPermission === 'read_only' ? rawPermission : 'unknown',
    writeAccess: safeBoolean(o.writeAccess),
    error: safeString(o.error) || undefined,
  }
}

export function parseRouterOSCleanup(value: unknown): RouterOSCleanup | null {
  const o = safeObject(value)
  const deviceId = safeString(o.deviceId)
  const script = safeString(o.script)
  if (!deviceId || !script) return null
  return {
    deviceId,
    name: safeString(o.name),
    username: safeString(o.username),
    groupName: safeString(o.groupName),
    script,
  }
}

export function parseMosDNSStatus(value: unknown): MosDNSStatus {
  const o = safeObject(value)
  return {
    enabled: safeBoolean(o.enabled),
    baseUrl: safeString(o.baseUrl),
    syncIntervalMinutes: safeNumber(o.syncIntervalMinutes),
    matchWindowMinutes: safeNumber(o.matchWindowMinutes),
    lastAttempt: safeString(o.lastAttempt) || undefined,
    lastSuccess: safeString(o.lastSuccess) || undefined,
    lastImported: safeNumber(o.lastImported),
    lastDuplicates: safeNumber(o.lastDuplicates),
    lastSkipped: safeNumber(o.lastSkipped),
    watermark: safeString(o.watermark) || undefined,
    learnedFeatureCount: safeNumber(o.learnedFeatureCount),
    learnedFeatureLastSeen: safeString(o.learnedFeatureLastSeen) || undefined,
    lastError: safeString(o.lastError) || undefined,
  }
}

export function parseRecognitionStatus(value: unknown): RecognitionStatus {
  const o = safeObject(value)
  return {
    protocolAnalysis: safeBoolean(o.protocolAnalysis),
    mosdns: parseMosDNSStatus(o.mosdns),
  }
}

function parseMutationResult(value: unknown): MutationResult {
  return { restarting: safeBoolean(safeObject(value).restarting) }
}

/* ---------- reads ---------- */

export function fetchSettings(): Promise<SettingsResponse> {
  return apiGet('/api/settings', parseSettings)
}

export function fetchRecognition(deviceId: string): Promise<RecognitionStatus> {
  return apiGet(`/api/recognition?deviceId=${encodeURIComponent(deviceId)}`, parseRecognitionStatus)
}

export function fetchMosDNS(deviceId: string): Promise<MosDNSStatus> {
  return apiGet(`/api/mosdns?deviceId=${encodeURIComponent(deviceId)}`, parseMosDNSStatus)
}

export function fetchDeviceAccount(deviceId: string): Promise<DeviceAccountStatus> {
  return apiGet(`/api/devices/${encodeURIComponent(deviceId)}/account`, parseDeviceAccountStatus)
}

export function fetchCleanupScript(deviceId: string): Promise<RouterOSCleanup | null> {
  return apiGet(`/api/devices/${encodeURIComponent(deviceId)}/cleanup-script`, parseRouterOSCleanup)
}

/** Scoped dashboard slice used by the device editor's topology readout. */
export type DeviceScopeSnapshot = {
  trafficScope: TrafficScopePreview
  terminalScope: TerminalScopePreview
}

export function fetchDeviceScope(deviceId: string): Promise<DeviceScopeSnapshot> {
  return apiGet(`/api/dashboard?device=${encodeURIComponent(deviceId)}`, (value): DeviceScopeSnapshot => {
    const o = safeObject(value)
    return {
      trafficScope: parseTrafficScopePreview(o.trafficScope),
      terminalScope: parseTerminalScopePreview(o.terminalScope),
    }
  })
}

/* ---------- device mutations ---------- */

export type DeviceSavePayload = {
  name: string
  enabled: boolean
  scheme: 'http' | 'https'
  host: string
  port: number
  username: string
  password: string
  trafficInterfaces: string[]
  trafficScope: TrafficScopeConfig
  terminalCidrs: string[]
  terminalScope: TerminalScopeConfig
  verificationToken?: string
  completeOnboarding?: boolean
  deferRestart?: boolean
}

export function createDevice(payload: DeviceSavePayload): Promise<MutationResult> {
  return apiPost('/api/devices', payload, parseMutationResult)
}

export function updateDevice(deviceId: string, payload: DeviceSavePayload): Promise<MutationResult> {
  return apiPut(`/api/devices/${encodeURIComponent(deviceId)}`, payload, parseMutationResult)
}

export function testConnection(payload: {
  deviceId?: string
  scheme: 'http' | 'https'
  host: string
  port: number
  username: string
  password: string
  trafficScope: TrafficScopeConfig
  terminalScope: TerminalScopeConfig
}): Promise<VerificationResult> {
  return apiPost('/api/devices/test-connection', payload, parseVerificationResult)
}

export function previewScope(payload: {
  verificationToken: string
  trafficScope: TrafficScopeConfig
  terminalScope: TerminalScopeConfig
}): Promise<DeviceScopeSnapshot> {
  return apiPost('/api/devices/preview-scope', payload, (value): DeviceScopeSnapshot => {
    const o = safeObject(value)
    return {
      trafficScope: parseTrafficScopePreview(o.trafficScope),
      terminalScope: parseTerminalScopePreview(o.terminalScope),
    }
  })
}

export function reorderDevices(deviceIds: string[]): Promise<MutationResult> {
  return apiPut('/api/devices/reorder', { deviceIds }, parseMutationResult)
}

export function archiveDevice(deviceId: string): Promise<MutationResult & { cleanup: RouterOSCleanup | null }> {
  return apiDelete(`/api/devices/${encodeURIComponent(deviceId)}`, (value: unknown) => {
    const o = safeObject(value)
    return { restarting: safeBoolean(o.restarting), cleanup: parseRouterOSCleanup(o.cleanup) }
  })
}

export function restoreDevice(deviceId: string): Promise<MutationResult> {
  return apiPost(`/api/devices/${encodeURIComponent(deviceId)}/restore`, undefined, parseMutationResult)
}

export function purgeDeviceData(deviceId: string, confirmation: string): Promise<MutationResult> {
  return apiDeleteWithBody(`/api/devices/${encodeURIComponent(deviceId)}/data`, { confirmation }, parseMutationResult)
}

export function deleteDeviceAccount(deviceId: string): Promise<MutationResult> {
  return apiDelete(`/api/devices/${encodeURIComponent(deviceId)}/account`, parseMutationResult)
}

/* ---------- quick onboarding (provisioning sessions) ---------- */

export function createOnboardingSession(payload: {
  deviceId?: string
  name?: string
  host?: string
  scheme?: 'http' | 'https'
  port?: number
}): Promise<ProvisioningSession> {
  return apiPost('/api/device-onboarding/sessions', payload, parseProvisioningSession)
}

export function previewOnboardingSession(
  sessionId: string,
  payload: { trafficScope: TrafficScopeConfig; terminalScope: TerminalScopeConfig },
): Promise<VerificationResult> {
  return apiPost(`/api/device-onboarding/sessions/${encodeURIComponent(sessionId)}/preview`, payload, parseVerificationResult)
}

export function completeOnboardingSession(
  sessionId: string,
  payload: {
    verificationToken?: string
    trafficScope: TrafficScopeConfig
    terminalScope: TerminalScopeConfig
    completeOnboarding: boolean
    deferRestart: boolean
  },
): Promise<MutationResult> {
  return apiPost(`/api/device-onboarding/sessions/${encodeURIComponent(sessionId)}/complete`, payload, parseMutationResult)
}

/* ---------- panel-level mutations ---------- */

export function saveCollectionSettings(payload: CollectionSettings): Promise<MutationResult> {
  return apiPost('/api/settings/collection', payload, parseMutationResult)
}

export type RecognitionDevicePayload = {
  id: string
  protocolAnalysis: boolean
  mosdns: {
    enabled: boolean
    baseUrl: string
    syncIntervalMinutes: number
    matchWindowMinutes: number
  }
}

export function saveRecognitionSettings(devices: RecognitionDevicePayload[]): Promise<MutationResult> {
  return apiPost('/api/settings/recognition', { devices }, parseMutationResult)
}

export function updateAccount(payload: { username: string; password: string; passwordConfirmation: string }): Promise<void> {
  return apiPut('/api/account', payload, () => undefined)
}

export function logout(): Promise<void> {
  return apiPost('/api/auth/logout', undefined, () => undefined)
}

export function restartPanel(): Promise<MutationResult> {
  return apiPost('/api/settings/restart', undefined, parseMutationResult)
}

export function fullReset(): Promise<void> {
  return apiPost('/api/settings/full-reset', { confirmed: true }, () => undefined)
}

/* ---------- restart waiting ---------- */

const RESTART_POLL_MS = 750
const RESTART_TIMEOUT_MS = 90_000

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds))
}

async function panelAssetsReady(): Promise<boolean> {
  const assetURLs = Array.from(
    document.querySelectorAll<HTMLScriptElement | HTMLLinkElement>('script[src], link[rel="stylesheet"][href]'),
  )
    .map((element) => (element instanceof HTMLScriptElement ? element.src : element.href))
    .filter(Boolean)
  if (!assetURLs.length) return true
  try {
    const responses = await Promise.all(assetURLs.map((url) => fetch(url, { cache: 'no-store' })))
    return responses.every((response) => response.ok)
  } catch {
    return false
  }
}

/**
 * Poll /api/health until the panel comes back after a scheduled restart,
 * then reload the page so the fresh assets and bootstrap state load.
 * `onOffline` fires once when the panel first goes away. Throws on timeout.
 */
export async function waitForPanelRestart(onOffline: () => void): Promise<void> {
  const started = Date.now()
  const deadline = started + RESTART_TIMEOUT_MS
  let observedOffline = false

  await delay(RESTART_POLL_MS)
  while (Date.now() < deadline) {
    try {
      const response = await fetch('/api/health', { cache: 'no-store' })
      if ((observedOffline || Date.now() - started > 4000) && response.ok && (await panelAssetsReady())) {
        await delay(RESTART_POLL_MS)
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
    await delay(RESTART_POLL_MS)
  }
  throw new Error('面板重启超时，请稍后手动刷新页面')
}
