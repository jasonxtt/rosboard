export type RateSample = { timestamp: string; uploadBps: number; downloadBps: number }

export type LoadSample = {
  timestamp: string
  cpuLoadPercent: number
  memoryUsedPercent: number
  storageUsedPercent: number
  onlineTerminalCount: number
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

export type Terminal = {
  id: string
  displayName: string
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
export type RouteStat = { kind: string; destination: string; gateway: string; table: string; action: string; source: string; distance: number; active: boolean; disabled: boolean }

export type DashboardResponse = {
  overview: Overview
  interfaces: InterfaceStatus[]
  terminals: Terminal[]
  capabilities: CapabilityNote[]
  protocols: ProtocolStat[]
  policies: PolicyStat[]
  routes: RouteStat[]
  alerts: AlertEvent[]
  warnings: string[]
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

export type ActiveView = 'overview' | 'interfaces' | 'terminals' | 'load' | 'protocols' | 'policies' | 'routes'
export type TerminalTab = 'basic' | 'connections' | 'flows' | 'history'
export type ConnectionFamily = 'all' | 'ipv4' | 'ipv6'
export type TerminalFamily = 'all' | 'ipv4' | 'ipv6'
export type TerminalSortKey = 'address' | 'connections' | 'upload' | 'download' | 'totalUpload' | 'totalDownload' | 'online' | 'interface' | 'device' | 'remark'
