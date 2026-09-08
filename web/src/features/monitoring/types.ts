/**
 * Monitoring feature contracts. Field names mirror the backend payloads
 * (internal/service/manager.go fleet, internal/api/provisioning.go
 * onboarding); every value is parsed from `unknown` in ./api.ts.
 */

import type { ChartWindow, RateSample } from '../../lib/types'

/** One device row of GET /api/fleet-overview. */
export type FleetDevice = {
  id: string
  name: string
  state: 'online' | 'offline' | string
  alerting: boolean
  error?: string
  routerName: string
  platform: string
  boardName: string
  version: string
  address: string
  cpuLoadPercent: number
  memoryUsedPercent: number
  uploadBps: number
  downloadBps: number
  terminalCount: number
  terminalOnline: number
  terminalInactive: number
  terminalOffline: number
  connectionCount: number
  uptime: string
  updatedAt: string
}

export type FleetOverview = {
  totalDevices: number
  onlineDevices: number
  offlineDevices: number
  alertDevices: number
  devices: FleetDevice[]
}

/** GET /api/traffic-history?window= */
export type TrafficHistory = {
  window: ChartWindow
  samples: RateSample[]
  trafficInterfaces: string[]
}

/* ---------- quick onboarding (POST /api/device-onboarding/sessions…) ---------- */

export type OnboardingConnection = { scheme: string; host: string; port: number }

export type OnboardingSession = {
  sessionId: string
  script: string
  expiresAt: string
  username: string
  connection: OnboardingConnection
}

export type OnboardingIdentity = { routerName: string; version: string; platform: string; boardName: string }

export type OnboardingWarning = { capability: string; message: string }

export type OnboardingPreview = {
  verificationToken: string
  expiresAt: string
  identity: OnboardingIdentity
  /** names of the automatically detected traffic-collect interfaces */
  trafficInterfaces: string[]
  /** count of automatically detected terminal prefixes */
  cidrCandidateCount: number
  warnings: OnboardingWarning[]
}

export type OnboardingComplete = { id: string; restarting: boolean }

/** Subset of GET /api/settings used by the setup/overview pages. */
export type SettingsSummary = {
  deviceCount: number
  realtimePollIntervalSeconds: number | null
}
