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

export type FleetDevice = {
  id: string
  name: string
  state: 'online' | 'offline'
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
  connectionTCP: number
  connectionUDP: number
  connectionOther: number
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
  category: 'physical' | 'logical' | 'system'
  relations: Array<{ kind: 'carrier' | 'parent' | 'bridge' | 'member'; interface: string }>
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
export type RouteStat = { id: string; kind: string; family: string; destination: string; gateway: string; table: string; action: string; source: string; distance: number; active: boolean; disabled: boolean; prefSrc: string; scope: string; targetScope: string; immediateGateway: string; protocol: string; comment: string; currentMatches: number }
export type DHCPServerStat = { name: string; interface: string; addressPool: string; leaseTime: string; disabled: boolean; invalid: boolean }
export type DHCPPoolStat = { name: string; ranges: string; total: number; used: number; free: number; usedPercent: number; servers: string[] }
export type DHCPLeaseStat = { id: string; address: string; macAddress: string; hostName: string; comment: string; server: string; status: string; expiresAfter: number; lastSeen: number; dynamic: boolean; blocked: boolean; disabled: boolean }
export type DHCPStat = { servers: DHCPServerStat[]; pools: DHCPPoolStat[]; leases: DHCPLeaseStat[] }

export type DeviceStatus = { id: string; name: string; enabled: boolean; archived: boolean; healthy: boolean; error?: string; routerName: string; version: string; updatedAt: string }
export type SettingsDevice = {
  id: string; name: string; enabled: boolean; archived: boolean; scheme: 'http' | 'https'; host: string; port: number
  username: string; passwordSet: boolean; cleanupAvailable: boolean; trafficInterfaces: string[]; trafficScope?: TrafficScopeConfig; terminalCidrs: string[]; terminalScope?: TerminalScopeConfig
}
export type TrafficScopeConfig = { mode?: 'auto'; include_interfaces?: string[]; exclude_interfaces?: string[] }
export type TrafficScope = { mode: string; legacy: boolean; interfaces: { name: string; kind: string; reasons: string[]; automatic: boolean; running: boolean; disabled: boolean }[]; warnings: string[]; overridesApplied: boolean }
export type TerminalScopeConfig = { mode?: 'auto'; include_interfaces?: string[]; exclude_interfaces?: string[]; include_cidrs?: string[]; exclude_cidrs?: string[] }
export type TerminalScope = { mode: string; legacy: boolean; interfaces: { name: string; role: 'lan' | 'wan' | 'unknown'; confidence: string; reasons: string[] }[]; prefixes: { cidr: string; family: 'ipv4' | 'ipv6'; interface: string; source: string; automatic: boolean }[]; warnings: string[]; overridesApplied: boolean }

export type BootstrapResponse = {
  phase: 'needs_admin' | 'needs_login' | 'needs_routeros' | 'ready'
  authenticated: boolean
  onboardingComplete: boolean
  username?: string
}

export type VerificationInterface = { name: string; type: string; running: boolean; disabled: boolean; addresses: string[] }
export type VerificationCIDR = { cidr: string; interface: string; family: string }
export type VerificationResponse = {
  verificationToken: string
  expiresAt: string
  identity: { routerName: string; version: string; platform: string; boardName: string }
  interfaces: VerificationInterface[]
  cidrCandidates: VerificationCIDR[]
  trafficScope?: TrafficScope
  terminalScope?: TerminalScope
  warnings: { capability: string; message: string }[]
}

export type DashboardResponse = {
  overview: Overview
  interfaces: InterfaceStatus[]
  terminals: Terminal[]
  terminalScopeSummaries: Record<TerminalFamily, TerminalScopeSummary>
  terminalScope: TerminalScope
  trafficScope: TrafficScope
  capabilities: CapabilityNote[]
  protocols: ProtocolStat[]
  policies: PolicyStat[]
  routes: RouteStat[]
  dhcp: DHCPStat
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


export type RouterOSCleanupResponse = {
  deviceId: string
  name: string
  username: string
  groupName: string
  script: string
}

export type ProvisioningSessionResponse = {
  sessionId: string
  script: string
  expiresAt: string
  username: string
  connection: {
    scheme: 'http' | 'https'
    host: string
    port: number
  }
}

export type ProvisioningCompleteResponse = {
  id: string
  restarting: boolean
  identity?: {
    routerName: string
    version: string
    platform: string
    boardName: string
  }
  warnings?: Array<{
    capability: string
    message: string
  }>
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
  routeInterfaces: string[]
  egressInterfaces: string[]
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
  ratesUpdatedAt: string
  connections: TerminalConnection[]
  flowCategories: TerminalFlowCategory[]
  history: TerminalHistoryEntry[]
  capabilities: TerminalCapability[]
  familySummaries: Record<'ipv4' | 'ipv6', Terminal>
  familyFlows: Record<'ipv4' | 'ipv6', TerminalFlowCategory[]>
}

export type ActiveView = 'fleet' | 'overview' | 'interfaces' | 'terminals' | 'load' | 'protocols' | 'policies' | 'dhcp' | 'routes' | 'settings'
export type TerminalTab = 'basic' | 'connections' | 'flows' | 'history'
export type ConnectionFamily = 'all' | 'ipv4' | 'ipv6'
export type TerminalFamily = 'all' | 'ipv4' | 'ipv6'
export type TerminalSortKey = 'address' | 'connections' | 'upload' | 'download' | 'totalUpload' | 'totalDownload' | 'online' | 'device' | 'remark'
