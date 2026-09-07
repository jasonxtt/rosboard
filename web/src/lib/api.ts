/**
 * Shared fetch layer. Components never call fetch directly; feature api.ts
 * modules use these helpers and parse `unknown` payloads with the guards
 * below (see .trellis/spec/frontend/type-safety.md).
 */

export const AUTH_REQUIRED_EVENT = 'rosboard:authentication-required'

export class ApiError extends Error {
  readonly status: number
  readonly code?: string
  readonly details?: unknown

  constructor(message: string, status: number, code?: string, details?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }
}

/** Append the device scope query parameter required by device-level APIs. */
export function scoped(path: string, deviceId: string): string {
  if (!deviceId) return path
  const separator = path.includes('?') ? '&' : '?'
  return `${path}${separator}device=${encodeURIComponent(deviceId)}`
}

function dispatchAuthenticationRequired(): void {
  window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT))
}

async function readJson(response: Response): Promise<unknown> {
  const text = await response.text()
  if (!text.trim()) return null
  try {
    return JSON.parse(text)
  } catch {
    throw new ApiError('服务返回了无法解析的响应', response.status, 'invalid_response')
  }
}

async function request<T>(path: string, init: RequestInit, parse?: (value: unknown) => T): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, { credentials: 'same-origin', ...init })
  } catch {
    throw new ApiError('网络请求失败，请检查面板连接', 0, 'network_error')
  }
  if (response.status === 401) dispatchAuthenticationRequired()
  const payload = await readJson(response)
  if (!response.ok) {
    const envelope = safeObject(payload)
    const message = safeString(envelope.error) || `请求失败（HTTP ${response.status}）`
    throw new ApiError(message, response.status, safeString(envelope.code) || undefined, envelope.details)
  }
  return parse ? parse(payload) : (payload as T)
}

function jsonInit(method: string, body?: unknown): RequestInit {
  if (body === undefined) return { method }
  return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }
}

export function apiGet<T = unknown>(path: string, parse?: (value: unknown) => T): Promise<T> {
  return request(path, { method: 'GET', cache: 'no-store' }, parse)
}

export function apiPost<T = unknown>(path: string, body?: unknown, parse?: (value: unknown) => T): Promise<T> {
  return request(path, jsonInit('POST', body), parse)
}

export function apiPut<T = unknown>(path: string, body?: unknown, parse?: (value: unknown) => T): Promise<T> {
  return request(path, jsonInit('PUT', body), parse)
}

export function apiDelete<T = unknown>(path: string, body?: unknown, parse?: (value: unknown) => T): Promise<T> {
  return request(path, jsonInit('DELETE', body), parse)
}

/** Display message for any thrown value. */
export function errorMessage(error: unknown, fallback = '操作失败，请稍后重试'): string {
  if (error instanceof Error && error.message) return error.message
  return fallback
}

/* ---------- boundary guards ---------- */

export function safeObject(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : {}
}

export function safeArray<T = unknown>(value: unknown): T[] {
  return Array.isArray(value) ? (value as T[]) : []
}

export function safeString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

export function safeStringOr(value: unknown, fallback: string): string {
  const text = safeString(value)
  return text || fallback
}

export function optionalString(value: unknown): string | undefined {
  const text = safeString(value)
  return text || undefined
}

export function safeNumber(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

export function safeBoolean(value: unknown): boolean {
  return value === true
}

export function safeStringArray(value: unknown): string[] {
  return safeArray<unknown>(value).filter((item): item is string => typeof item === 'string')
}

export function safeNumberArray(value: unknown): number[] {
  return safeArray<unknown>(value).map(safeNumber)
}
