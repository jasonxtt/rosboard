/**
 * Shared monitoring types, parsed from `unknown` at the API boundary.
 * Field names mirror the backend payloads (internal/model/types.go); feature
 * pages keep their own contracts beside the feature.
 */

import { safeArray, safeBoolean, safeNumber, safeObject, safeString, safeStringArray } from './api.ts'

export type TerminalFamily = 'all' | 'ipv4' | 'ipv6'
export type ConnectionFamily = 'all' | 'ipv4' | 'ipv6'
export type TerminalState = 'online' | 'inactive' | 'offline'
export type ChartWindow = '5m' | '1h' | '6h' | '24h'

export type BootstrapPhase = 'needs_admin' | 'needs_login' | 'needs_routeros' | 'ready'

export type BootstrapResponse = {
  phase: BootstrapPhase
  authenticated: boolean
  onboardingComplete: boolean
  username?: string
}

export type RateSample = { timestamp: string; uploadBps: number; downloadBps: number }

export type LoadSample = {
  timestamp: string
  cpuLoadPercent: number
  memoryUsedPercent: number
  storageUsedPercent: number
  onlineTerminalCount: number
  connectionCount: number
  uploadBps: number
  downloadBps: number
}

export type Overview = {
  routerName: string
  platform: string
  version: string
  boardName: string
  uptime: string
  cpuLoadPercent: number
  memoryUsedPercent: number
  memoryUsedBytes: number
  memoryTotalBytes: number
  storageUsedPercent: number
  storageUsedBytes: number
  storageTotalBytes: number
  connectedDeviceCount: number
  connectionCount: number
  terminalStateCounts: { online: number; inactive: number; offline: number }
  connectionProtocolCounts: { tcp: number; udp: number; other: number }
  uploadBps: number
  downloadBps: number
  trafficInterfaces: string[]
  healthEnabled: boolean
  updatedAt: string
  chartSamples: RateSample[]
  /** Light pick from the nested systemResource payload (values arrive as strings). */
  system: { architectureName: string; cpu: string; cpuCount: string; cpuFrequency: string }
}

export type InterfaceRelation = { kind: 'carrier' | 'parent' | 'bridge' | 'member' | string; interface: string }
export type InterfaceCategory = 'physical' | 'logical' | 'system'

export type InterfaceStatus = {
  name: string
  type: string
  running: boolean
  disabled: boolean
  macAddress: string
  status: string
  lastLinkUpTime: string
  linkDowns: number
  actualMtu: number
  rxBytes: number
  txBytes: number
  currentRxBps: number
  currentTxBps: number
  addresses: string[]
  rxPackets: number
  txPackets: number
  rxDrops: number
  txDrops: number
  rxErrors: number
  txErrors: number
  linkRate: string
  fullDuplex: boolean
  category: InterfaceCategory
  relations: InterfaceRelation[]
}

export type TerminalFamilyStats = {
  connectionCount: number
  currentUploadBps: number
  currentDownloadBps: number
  activeUploadBytes: number
  activeDownloadBytes: number
}

export type TerminalScopeSummary = {
  deviceCount: number
  connectionCount: number
  currentUploadBps: number
  currentDownloadBps: number
  activeUploadBytes: number
  activeDownloadBytes: number
}

export type Terminal = {
  id: string
  displayName: string
  autoName: string
  customName: string
  macAddress: string
  primaryInterface: string
  ipv4: string[]
  ipv6: string[]
  connectionCount: number
  currentUploadBps: number
  currentDownloadBps: number
  totalUploadBytes: number
  totalDownloadBytes: number
  trackingSince: string
  lastSeen: string
  primaryIpv4: string
  primaryIpv6: string
  state: TerminalState
  onlineSince: string
  familyStats: Record<'ipv4' | 'ipv6', TerminalFamilyStats>
}

export type AlertEvent = { id: string; level: 'warning' | 'error' | string; source: string; message: string; timestamp: string }

export type ProtocolStat = {
  name: string
  applicationId?: string
  service?: string
  kind: string
  connections: number
  uploadBps: number
  downloadBps: number
  uploadBytes: number
  downloadBytes: number
  estimated: boolean
  source?: string
}

export type PolicyStat = { kind: string; name: string; target: string; mark: string; rate: string; bytes: number; packets: number; disabled: boolean }

export type RouteStat = {
  id: string
  kind: string
  family: string
  destination: string
  gateway: string
  table: string
  action: string
  source: string
  distance: number
  active: boolean
  disabled: boolean
  prefSrc: string
  scope: string
  targetScope: string
  immediateGateway: string
  protocol: string
  comment: string
  currentMatches: number
}

export type DHCPServerStat = { name: string; interface: string; addressPool: string; leaseTime: string; disabled: boolean; invalid: boolean }
export type DHCPPoolStat = { name: string; ranges: string; total: number; used: number; free: number; usedPercent: number; servers: string[] }
export type DHCPLeaseStat = {
  id: string
  address: string
  macAddress: string
  hostName: string
  comment: string
  server: string
  status: string
  expiresAfter: number
  lastSeen: number
  dynamic: boolean
  blocked: boolean
  disabled: boolean
}
export type DHCPStat = { servers: DHCPServerStat[]; pools: DHCPPoolStat[]; leases: DHCPLeaseStat[] }

export type DashboardResponse = {
  overview: Overview
  interfaces: InterfaceStatus[]
  terminals: Terminal[]
  protocols: ProtocolStat[]
  policies: PolicyStat[]
  routes: RouteStat[]
  dhcp: DHCPStat
  alerts: AlertEvent[]
  warnings: string[]
}

export type DeviceStatus = {
  id: string
  name: string
  enabled: boolean
  archived: boolean
  healthy: boolean
  error?: string
  routerName: string
  version: string
  updatedAt: string
}

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

/* ---------- parsers (unknown → typed) ---------- */

export function parseBootstrap(value: unknown): BootstrapResponse {
  const o = safeObject(value)
  const rawPhase = safeString(o.phase)
  const phase: BootstrapPhase =
    rawPhase === 'needs_admin' || rawPhase === 'needs_routeros' || rawPhase === 'ready' ? rawPhase : 'needs_login'
  return {
    phase,
    authenticated: safeBoolean(o.authenticated),
    onboardingComplete: safeBoolean(o.onboardingComplete),
    username: safeString(o.username) || undefined,
  }
}

export function parseRateSample(value: unknown): RateSample {
  const o = safeObject(value)
  return { timestamp: safeString(o.timestamp), uploadBps: safeNumber(o.uploadBps), downloadBps: safeNumber(o.downloadBps) }
}

export function parseLoadSample(value: unknown): LoadSample {
  const o = safeObject(value)
  return {
    timestamp: safeString(o.timestamp),
    cpuLoadPercent: safeNumber(o.cpuLoadPercent),
    memoryUsedPercent: safeNumber(o.memoryUsedPercent),
    storageUsedPercent: safeNumber(o.storageUsedPercent),
    onlineTerminalCount: safeNumber(o.onlineTerminalCount),
    connectionCount: safeNumber(o.connectionCount),
    uploadBps: safeNumber(o.uploadBps),
    downloadBps: safeNumber(o.downloadBps),
  }
}

export function parseOverview(value: unknown): Overview {
  const o = safeObject(value)
  const states = safeObject(o.terminalStateCounts)
  const protocols = safeObject(o.connectionProtocolCounts)
  const system = safeObject(o.systemResource)
  return {
    routerName: safeString(o.routerName),
    platform: safeString(o.platform),
    version: safeString(o.version),
    boardName: safeString(o.boardName),
    uptime: safeString(o.uptime),
    cpuLoadPercent: safeNumber(o.cpuLoadPercent),
    memoryUsedPercent: safeNumber(o.memoryUsedPercent),
    memoryUsedBytes: safeNumber(o.memoryUsedBytes),
    memoryTotalBytes: safeNumber(o.memoryTotalBytes),
    storageUsedPercent: safeNumber(o.storageUsedPercent),
    storageUsedBytes: safeNumber(o.storageUsedBytes),
    storageTotalBytes: safeNumber(o.storageTotalBytes),
    connectedDeviceCount: safeNumber(o.connectedDeviceCount),
    connectionCount: safeNumber(o.connectionCount),
    terminalStateCounts: { online: safeNumber(states.online), inactive: safeNumber(states.inactive), offline: safeNumber(states.offline) },
    connectionProtocolCounts: { tcp: safeNumber(protocols.tcp), udp: safeNumber(protocols.udp), other: safeNumber(protocols.other) },
    uploadBps: safeNumber(o.uploadBps),
    downloadBps: safeNumber(o.downloadBps),
    trafficInterfaces: safeStringArray(o.trafficInterfaces),
    healthEnabled: safeBoolean(o.healthEnabled),
    updatedAt: safeString(o.updatedAt),
    chartSamples: safeArray<unknown>(o.chartSamples).map(parseRateSample),
    system: {
      architectureName: safeString(system.architectureName),
      cpu: safeString(system.cpu),
      cpuCount: safeString(system.cpuCount),
      cpuFrequency: safeString(system.cpuFrequency),
    },
  }
}

export function parseInterfaceStatus(value: unknown): InterfaceStatus {
  const o = safeObject(value)
  const type = safeString(o.type).toLowerCase()
  const rawCategory = safeString(o.category)
  const category: InterfaceCategory =
    rawCategory === 'physical' || rawCategory === 'logical' || rawCategory === 'system'
      ? rawCategory
      : type === 'loopback'
        ? 'system'
        : type === 'ether'
          ? 'physical'
          : 'logical'
  return {
    name: safeString(o.name),
    type: safeString(o.type),
    running: safeBoolean(o.running),
    disabled: safeBoolean(o.disabled),
    macAddress: safeString(o.macAddress),
    status: safeString(o.status),
    lastLinkUpTime: safeString(o.lastLinkUpTime),
    linkDowns: safeNumber(o.linkDowns),
    actualMtu: safeNumber(o.actualMtu),
    rxBytes: safeNumber(o.rxBytes),
    txBytes: safeNumber(o.txBytes),
    currentRxBps: safeNumber(o.currentRxBps),
    currentTxBps: safeNumber(o.currentTxBps),
    addresses: safeStringArray(o.addresses),
    rxPackets: safeNumber(o.rxPackets),
    txPackets: safeNumber(o.txPackets),
    rxDrops: safeNumber(o.rxDrops),
    txDrops: safeNumber(o.txDrops),
    rxErrors: safeNumber(o.rxErrors),
    txErrors: safeNumber(o.txErrors),
    linkRate: safeString(o.linkRate),
    fullDuplex: safeBoolean(o.fullDuplex),
    category,
    relations: safeArray<unknown>(o.relations).map((raw) => {
      const relation = safeObject(raw)
      return { kind: safeString(relation.kind), interface: safeString(relation.interface) }
    }),
  }
}

function parseTerminalFamilyStats(value: unknown): TerminalFamilyStats {
  const o = safeObject(value)
  return {
    connectionCount: safeNumber(o.connectionCount),
    currentUploadBps: safeNumber(o.currentUploadBps),
    currentDownloadBps: safeNumber(o.currentDownloadBps),
    activeUploadBytes: safeNumber(o.activeUploadBytes),
    activeDownloadBytes: safeNumber(o.activeDownloadBytes),
  }
}

export function parseTerminal(value: unknown): Terminal {
  const o = safeObject(value)
  const rawState = safeString(o.state)
  const state: TerminalState = rawState === 'online' || rawState === 'inactive' ? rawState : 'offline'
  const familyStats = safeObject(o.familyStats)
  return {
    id: safeString(o.id),
    displayName: safeString(o.displayName),
    autoName: safeString(o.autoName),
    customName: safeString(o.customName),
    macAddress: safeString(o.macAddress),
    primaryInterface: safeString(o.primaryInterface),
    ipv4: safeStringArray(o.ipv4),
    ipv6: safeStringArray(o.ipv6),
    connectionCount: safeNumber(o.connectionCount),
    currentUploadBps: safeNumber(o.currentUploadBps),
    currentDownloadBps: safeNumber(o.currentDownloadBps),
    totalUploadBytes: safeNumber(o.totalUploadBytes),
    totalDownloadBytes: safeNumber(o.totalDownloadBytes),
    trackingSince: safeString(o.trackingSince),
    lastSeen: safeString(o.lastSeen),
    primaryIpv4: safeString(o.primaryIpv4),
    primaryIpv6: safeString(o.primaryIpv6),
    state,
    onlineSince: safeString(o.onlineSince),
    familyStats: {
      ipv4: parseTerminalFamilyStats(familyStats.ipv4),
      ipv6: parseTerminalFamilyStats(familyStats.ipv6),
    },
  }
}

export function parseAlertEvent(value: unknown): AlertEvent {
  const o = safeObject(value)
  return {
    id: safeString(o.id),
    level: safeString(o.level) || 'warning',
    source: safeString(o.source),
    message: safeString(o.message),
    timestamp: safeString(o.timestamp),
  }
}

export function parseProtocolStat(value: unknown): ProtocolStat {
  const o = safeObject(value)
  return {
    name: safeString(o.name),
    applicationId: safeString(o.applicationId) || undefined,
    service: safeString(o.service) || undefined,
    kind: safeString(o.kind),
    connections: safeNumber(o.connections),
    uploadBps: safeNumber(o.uploadBps),
    downloadBps: safeNumber(o.downloadBps),
    uploadBytes: safeNumber(o.uploadBytes),
    downloadBytes: safeNumber(o.downloadBytes),
    estimated: safeBoolean(o.estimated),
    source: safeString(o.source) || undefined,
  }
}

export function parsePolicyStat(value: unknown): PolicyStat {
  const o = safeObject(value)
  return {
    kind: safeString(o.kind),
    name: safeString(o.name),
    target: safeString(o.target),
    mark: safeString(o.mark),
    rate: safeString(o.rate),
    bytes: safeNumber(o.bytes),
    packets: safeNumber(o.packets),
    disabled: safeBoolean(o.disabled),
  }
}

export function parseRouteStat(value: unknown): RouteStat {
  const o = safeObject(value)
  return {
    id: safeString(o.id),
    kind: safeString(o.kind),
    family: safeString(o.family),
    destination: safeString(o.destination),
    gateway: safeString(o.gateway),
    table: safeString(o.table),
    action: safeString(o.action),
    source: safeString(o.source),
    distance: safeNumber(o.distance),
    active: safeBoolean(o.active),
    disabled: safeBoolean(o.disabled),
    prefSrc: safeString(o.prefSrc),
    scope: safeString(o.scope),
    targetScope: safeString(o.targetScope),
    immediateGateway: safeString(o.immediateGateway),
    protocol: safeString(o.protocol),
    comment: safeString(o.comment),
    currentMatches: safeNumber(o.currentMatches),
  }
}

export function parseDHCPStat(value: unknown): DHCPStat {
  const o = safeObject(value)
  return {
    servers: safeArray<unknown>(o.servers).map((raw) => {
      const server = safeObject(raw)
      return {
        name: safeString(server.name),
        interface: safeString(server.interface),
        addressPool: safeString(server.addressPool),
        leaseTime: safeString(server.leaseTime),
        disabled: safeBoolean(server.disabled),
        invalid: safeBoolean(server.invalid),
      }
    }),
    pools: safeArray<unknown>(o.pools).map((raw) => {
      const pool = safeObject(raw)
      return {
        name: safeString(pool.name),
        ranges: safeString(pool.ranges),
        total: safeNumber(pool.total),
        used: safeNumber(pool.used),
        free: safeNumber(pool.free),
        usedPercent: safeNumber(pool.usedPercent),
        servers: safeStringArray(pool.servers),
      }
    }),
    leases: safeArray<unknown>(o.leases).map((raw) => {
      const lease = safeObject(raw)
      return {
        id: safeString(lease.id),
        address: safeString(lease.address),
        macAddress: safeString(lease.macAddress),
        hostName: safeString(lease.hostName),
        comment: safeString(lease.comment),
        server: safeString(lease.server),
        status: safeString(lease.status),
        expiresAfter: safeNumber(lease.expiresAfter),
        lastSeen: safeNumber(lease.lastSeen),
        dynamic: safeBoolean(lease.dynamic),
        blocked: safeBoolean(lease.blocked),
        disabled: safeBoolean(lease.disabled),
      }
    }),
  }
}

export function parseDashboard(value: unknown): DashboardResponse {
  const o = safeObject(value)
  return {
    overview: parseOverview(o.overview),
    interfaces: safeArray<unknown>(o.interfaces).map(parseInterfaceStatus),
    terminals: safeArray<unknown>(o.terminals).map(parseTerminal),
    protocols: safeArray<unknown>(o.protocols).map(parseProtocolStat),
    policies: safeArray<unknown>(o.policies).map(parsePolicyStat),
    routes: safeArray<unknown>(o.routes).map(parseRouteStat),
    dhcp: parseDHCPStat(o.dhcp),
    alerts: safeArray<unknown>(o.alerts).map(parseAlertEvent),
    warnings: safeStringArray(o.warnings),
  }
}

export function parseDeviceStatus(value: unknown): DeviceStatus {
  const o = safeObject(value)
  return {
    id: safeString(o.id),
    name: safeString(o.name),
    enabled: safeBoolean(o.enabled),
    archived: safeBoolean(o.archived),
    healthy: safeBoolean(o.healthy),
    error: safeString(o.error) || undefined,
    routerName: safeString(o.routerName),
    version: safeString(o.version),
    updatedAt: safeString(o.updatedAt),
  }
}

export function parseDevices(value: unknown): DeviceStatus[] {
  return safeArray<unknown>(safeObject(value).devices).map(parseDeviceStatus)
}

export function parseFleetDevice(value: unknown): FleetDevice {
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
    connectionCount: safeNumber(o.connectionCount),
    uptime: safeString(o.uptime),
    updatedAt: safeString(o.updatedAt),
  }
}

export function parseFleetOverview(value: unknown): FleetOverview {
  const o = safeObject(value)
  return {
    totalDevices: safeNumber(o.totalDevices),
    onlineDevices: safeNumber(o.onlineDevices),
    offlineDevices: safeNumber(o.offlineDevices),
    alertDevices: safeNumber(o.alertDevices),
    devices: safeArray<unknown>(o.devices).map(parseFleetDevice),
  }
}
