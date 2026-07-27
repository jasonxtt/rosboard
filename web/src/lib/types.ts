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
}

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
}

export type InterfaceDetail = { interface: InterfaceStatus; samples: RateSample[] }

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
  remark: string
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
  state: 'online' | 'inactive' | 'offline'
  onlineSince: string
  familyStats: Record<'ipv4' | 'ipv6', TerminalFamilyStats>
}

export type CapabilityNote = { area: string; item: string; status: string; details: string }
export type ProtocolStat = { name: string; kind: string; connections: number; uploadBps: number; downloadBps: number; uploadBytes: number; downloadBytes: number; estimated: boolean }
export type ProtocolHistorySample = { timestamp: string; name: string; kind: string; connections: number; uploadBps: number; downloadBps: number }
export type PolicyStat = { kind: string; name: string; target: string; mark: string; rate: string; bytes: number; packets: number; disabled: boolean }
export type RouteStat = { id: string; kind: string; family: string; destination: string; gateway: string; table: string; action: string; source: string; distance: number; active: boolean; disabled: boolean; currentMatches: number }

export type DeviceStatus = { id: string; name: string; enabled: boolean; archived: boolean; healthy: boolean; error?: string; routerName: string; version: string; updatedAt: string }
export type SettingsDevice = {
  id: string; name: string; enabled: boolean; archived: boolean; scheme: 'http' | 'https'; host: string; port: number
  username: string; password: string; passwordSet: boolean; trafficInterfaces: string[]; terminalCidrs: string[]
}

export type DashboardResponse = {
  overview: Overview
  interfaces: InterfaceStatus[]
  terminals: Terminal[]
  terminalScopeSummaries: Record<TerminalFamily, TerminalScopeSummary>
  capabilities: CapabilityNote[]
  protocols: ProtocolStat[]
  policies: PolicyStat[]
  routes: RouteStat[]
  alerts: AlertEvent[]
  warnings: string[]
}

export type SettingsResponse = {
  connection: {
    apiBasePath: string
    configured: boolean
    listenAddress: string
    allowedCidrs: string[]
    routerosBaseUrl: string
    routerosScheme: 'http' | 'https'
    routerosHost: string
    routerosPort: number
    routerosUsername: string
    routerosPassword: string
    routerosPasswordSet: boolean
  }
  collection: {
    pollIntervalSeconds: number
    realtimePollIntervalSeconds: number
    terminalPollIntervalSeconds: number
    sampleRetentionHours: number
  }
  diagnostics: {
    routerName: string
    version: string
    updatedAt: string
  }
  devices: SettingsDevice[]
}

export type AlertEvent = { id: string; level: 'warning' | 'error'; source: string; message: string; timestamp: string }

export type TerminalConnection = {
  key: string
  family: string
  application: string
  protocol: string
  line: string
  sourceAddress: string
  sourcePort: string
  destinationAddress: string
  destinationPort: string
  uploadBytes: number
  downloadBytes: number
  uploadBps: number
  downloadBps: number
  status: string
  seenReply: boolean
  assured: boolean
  publicAddress: string
  connectionMark: string
  routingMark: string
  routeTable: string
  matchedRule: string
  matchedRuleId: string
  routeDestination: string
  routeId: string
  routeIds: string[]
  routeGateways: string[]
  routeMatchBasis: string
  routeAttribution: string
  estimated: boolean
}

export type TerminalFlowCategory = {
  name: string
  currentUploadBps: number
  currentDownloadBps: number
  totalUploadBytes: number
  totalDownloadBytes: number
  uploadPercent: number
  downloadPercent: number
  estimated: boolean
}

export type TerminalHistoryEntry = { timestamp: string; onlineSeconds: number; totalUploadBytes: number; totalDownloadBytes: number }
export type TerminalCapability = { tab: string; status: string; details: string }

export type TerminalDetail = {
  terminal: Terminal
  connections: TerminalConnection[]
  flowCategories: TerminalFlowCategory[]
  history: TerminalHistoryEntry[]
  capabilities: TerminalCapability[]
  familySummaries: Record<'ipv4' | 'ipv6', Terminal>
  familyFlows: Record<'ipv4' | 'ipv6', TerminalFlowCategory[]>
}

export type ActiveView = 'overview' | 'interfaces' | 'terminals' | 'load' | 'protocols' | 'policies' | 'routes' | 'settings'
export type TerminalTab = 'basic' | 'connections' | 'flows' | 'history'
export type ConnectionFamily = 'all' | 'ipv4' | 'ipv6'
export type TerminalFamily = 'all' | 'ipv4' | 'ipv6'
export type TerminalSortKey = 'address' | 'connections' | 'upload' | 'download' | 'totalUpload' | 'totalDownload' | 'online' | 'device' | 'remark'
