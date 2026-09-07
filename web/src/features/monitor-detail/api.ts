/**
 * Slice-2 monitor feature API layer: contracts not yet in lib/types
 * (terminal detail, connections, interface detail, protocols envelope,
 * system resource) plus the device-scoped fetchers for the monitor pages.
 * All payloads arrive as `unknown` and are parsed through the lib guards.
 */

import {
  apiGet,
  apiPost,
  safeArray,
  safeBoolean,
  safeNumber,
  safeObject,
  safeString,
  safeStringArray,
} from '../../lib/api'
import {
  parseDHCPStat,
  parseInterfaceStatus,
  parseLoadSample,
  parseOverview,
  parsePolicyStat,
  parseProtocolStat,
  parseRateSample,
  parseRouteStat,
  parseTerminal,
  type DHCPStat,
  type InterfaceStatus,
  type LoadSample,
  type Overview,
  type PolicyStat,
  type ProtocolStat,
  type RateSample,
  type RouteStat,
  type Terminal,
} from '../../lib/types'

/** `(path) => path?device=…` from useShell().scopedPath */
export type ScopedPath = (path: string) => string

export type LoadWindow = '1h' | '1d' | '1w' | '1m'

/* ---------- contracts (mirror internal/model/types.go) ---------- */

export type TerminalConnection = {
  key: string
  family: string
  application: string
  service?: string
  matchedDomain?: string
  applicationSource?: string
  protocol: string
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
  connectionMark: string
  routingMark: string
  routeTable: string
  matchedRule: string
  routeGateways: string[]
  egressInterfaces: string[]
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

export type TerminalHistoryEntry = {
  timestamp: string
  onlineSeconds: number
  totalUploadBytes: number
  totalDownloadBytes: number
}

export type TerminalCapability = { tab: string; status: string; details: string }

export type TerminalDetail = {
  terminal: Terminal
  ratesUpdatedAt: string
  connections: TerminalConnection[]
  flowCategories: TerminalFlowCategory[]
  history: TerminalHistoryEntry[]
  capabilities: TerminalCapability[]
  familySummaries: Record<'ipv4' | 'ipv6', Terminal | null>
  familyFlows: Record<'ipv4' | 'ipv6', TerminalFlowCategory[]>
}

export type InterfaceDetail = { interface: InterfaceStatus; samples: RateSample[] }

export type ProtocolHistorySample = {
  timestamp: string
  name: string
  kind: string
  connections: number
  uploadBps: number
  downloadBps: number
}

export type ProtocolResponse = {
  protocols: ProtocolStat[]
  history: ProtocolHistorySample[]
  enabled: boolean
}

export type SystemResourceCPU = { cpu: string; load: string; irq: string; disk: string }
export type SystemResourceIRQ = { cpu: string; activeCpu: string; count: string; irq: string; users: string }
export type SystemResourceHardware = {
  location: string
  parent: string
  type: string
  vendor: string
  name: string
  serialNumber: string
  speed: string
  ports: string
  owner: string
  category: string
  irq: string
}

export type SystemResource = {
  architectureName: string
  boardName: string
  badBlocks: string
  buildTime: string
  cpu: string
  cpuCount: string
  cpuFrequency: string
  cpuLoad: string
  factorySoftware: string
  freeMemory: string
  freeHddSpace: string
  platform: string
  totalMemory: string
  totalHddSpace: string
  uptime: string
  version: string
  writeSectSinceReboot: string
  writeSectTotal: string
  cpuCores: SystemResourceCPU[]
  irqs: SystemResourceIRQ[]
  hardware: SystemResourceHardware[]
}

export type ResourceOverview = { overview: Overview; resource: SystemResource }

/* ---------- parsers ---------- */

function parseConnection(value: unknown): TerminalConnection {
  const o = safeObject(value)
  return {
    key: safeString(o.key),
    family: safeString(o.family) || 'ipv4',
    application: safeString(o.application),
    service: safeString(o.service) || undefined,
    matchedDomain: safeString(o.matchedDomain) || undefined,
    applicationSource: safeString(o.applicationSource) || undefined,
    protocol: safeString(o.protocol),
    sourceAddress: safeString(o.sourceAddress),
    sourcePort: safeString(o.sourcePort),
    destinationAddress: safeString(o.destinationAddress),
    destinationPort: safeString(o.destinationPort),
    uploadBytes: safeNumber(o.uploadBytes),
    downloadBytes: safeNumber(o.downloadBytes),
    uploadBps: safeNumber(o.uploadBps),
    downloadBps: safeNumber(o.downloadBps),
    status: safeString(o.status),
    seenReply: safeBoolean(o.seenReply),
    assured: safeBoolean(o.assured),
    connectionMark: safeString(o.connectionMark),
    routingMark: safeString(o.routingMark),
    routeTable: safeString(o.routeTable),
    matchedRule: safeString(o.matchedRule),
    routeGateways: safeStringArray(o.routeGateways),
    egressInterfaces: safeStringArray(o.egressInterfaces),
    estimated: safeBoolean(o.estimated),
  }
}

function parseFlowCategory(value: unknown): TerminalFlowCategory {
  const o = safeObject(value)
  return {
    name: safeString(o.name),
    currentUploadBps: safeNumber(o.currentUploadBps),
    currentDownloadBps: safeNumber(o.currentDownloadBps),
    totalUploadBytes: safeNumber(o.totalUploadBytes),
    totalDownloadBytes: safeNumber(o.totalDownloadBytes),
    uploadPercent: safeNumber(o.uploadPercent),
    downloadPercent: safeNumber(o.downloadPercent),
    estimated: safeBoolean(o.estimated),
  }
}

function parseHistoryEntry(value: unknown): TerminalHistoryEntry {
  const o = safeObject(value)
  return {
    timestamp: safeString(o.timestamp),
    onlineSeconds: safeNumber(o.onlineSeconds),
    totalUploadBytes: safeNumber(o.totalUploadBytes),
    totalDownloadBytes: safeNumber(o.totalDownloadBytes),
  }
}

function parseFamilySummary(value: unknown): Terminal | null {
  const o = safeObject(value)
  return safeString(o.id) ? parseTerminal(o) : null
}

export function parseTerminalDetail(value: unknown): TerminalDetail {
  const o = safeObject(value)
  const summaries = safeObject(o.familySummaries)
  const flows = safeObject(o.familyFlows)
  return {
    terminal: parseTerminal(o.terminal),
    ratesUpdatedAt: safeString(o.ratesUpdatedAt),
    connections: safeArray<unknown>(o.connections).map(parseConnection),
    flowCategories: safeArray<unknown>(o.flowCategories).map(parseFlowCategory),
    history: safeArray<unknown>(o.history).map(parseHistoryEntry),
    capabilities: safeArray<unknown>(o.capabilities).map((raw) => {
      const capability = safeObject(raw)
      return { tab: safeString(capability.tab), status: safeString(capability.status), details: safeString(capability.details) }
    }),
    familySummaries: { ipv4: parseFamilySummary(summaries.ipv4), ipv6: parseFamilySummary(summaries.ipv6) },
    familyFlows: {
      ipv4: safeArray<unknown>(flows.ipv4).map(parseFlowCategory),
      ipv6: safeArray<unknown>(flows.ipv6).map(parseFlowCategory),
    },
  }
}

function parseInterfaceDetail(value: unknown): InterfaceDetail {
  const o = safeObject(value)
  return {
    interface: parseInterfaceStatus(o.interface),
    samples: safeArray<unknown>(o.samples).map(parseRateSample),
  }
}

function parseProtocolHistorySample(value: unknown): ProtocolHistorySample {
  const o = safeObject(value)
  return {
    timestamp: safeString(o.timestamp),
    name: safeString(o.name),
    kind: safeString(o.kind),
    connections: safeNumber(o.connections),
    uploadBps: safeNumber(o.uploadBps),
    downloadBps: safeNumber(o.downloadBps),
  }
}

export function parseProtocolResponse(value: unknown): ProtocolResponse {
  const o = safeObject(value)
  return {
    protocols: safeArray<unknown>(o.protocols).map(parseProtocolStat),
    history: safeArray<unknown>(o.history).map(parseProtocolHistorySample),
    enabled: safeBoolean(o.enabled),
  }
}

function parseSystemResource(value: unknown): SystemResource {
  const o = safeObject(value)
  return {
    architectureName: safeString(o.architectureName),
    boardName: safeString(o.boardName),
    badBlocks: safeString(o.badBlocks),
    buildTime: safeString(o.buildTime),
    cpu: safeString(o.cpu),
    cpuCount: safeString(o.cpuCount),
    cpuFrequency: safeString(o.cpuFrequency),
    cpuLoad: safeString(o.cpuLoad),
    factorySoftware: safeString(o.factorySoftware),
    freeMemory: safeString(o.freeMemory),
    freeHddSpace: safeString(o.freeHddSpace),
    platform: safeString(o.platform),
    totalMemory: safeString(o.totalMemory),
    totalHddSpace: safeString(o.totalHddSpace),
    uptime: safeString(o.uptime),
    version: safeString(o.version),
    writeSectSinceReboot: safeString(o.writeSectSinceReboot),
    writeSectTotal: safeString(o.writeSectTotal),
    cpuCores: safeArray<unknown>(o.cpuCores).map((raw) => {
      const core = safeObject(raw)
      return { cpu: safeString(core.cpu), load: safeString(core.load), irq: safeString(core.irq), disk: safeString(core.disk) }
    }),
    irqs: safeArray<unknown>(o.irqs).map((raw) => {
      const irq = safeObject(raw)
      return {
        cpu: safeString(irq.cpu),
        activeCpu: safeString(irq.activeCpu),
        count: safeString(irq.count),
        irq: safeString(irq.irq),
        users: safeString(irq.users),
      }
    }),
    hardware: safeArray<unknown>(o.hardware).map((raw) => {
      const hardware = safeObject(raw)
      return {
        location: safeString(hardware.location),
        parent: safeString(hardware.parent),
        type: safeString(hardware.type),
        vendor: safeString(hardware.vendor),
        name: safeString(hardware.name),
        serialNumber: safeString(hardware.serialNumber),
        speed: safeString(hardware.speed),
        ports: safeString(hardware.ports),
        owner: safeString(hardware.owner),
        category: safeString(hardware.category),
        irq: safeString(hardware.irq),
      }
    }),
  }
}

/* ---------- fetchers (pages/hooks call these; no fetch in components) ---------- */

export function fetchInterfaces(scopedPath: ScopedPath): Promise<InterfaceStatus[]> {
  return apiGet(scopedPath('/api/interfaces'), (value) => safeArray<unknown>(safeObject(value).interfaces).map(parseInterfaceStatus))
}

export function fetchInterfaceDetail(scopedPath: ScopedPath, name: string): Promise<InterfaceDetail> {
  return apiGet(scopedPath(`/api/interfaces/${encodeURIComponent(name)}`), parseInterfaceDetail)
}

export function fetchTerminals(scopedPath: ScopedPath): Promise<Terminal[]> {
  return apiGet(scopedPath('/api/terminals'), (value) => safeArray<unknown>(safeObject(value).terminals).map(parseTerminal))
}

export function fetchTerminalDetail(scopedPath: ScopedPath, id: string): Promise<TerminalDetail> {
  return apiGet(scopedPath(`/api/terminals/${encodeURIComponent(id)}`), parseTerminalDetail)
}

export function saveTerminalMetadata(scopedPath: ScopedPath, id: string, draft: { customName: string; remark: string }): Promise<TerminalDetail> {
  return apiPost(scopedPath(`/api/terminals/${encodeURIComponent(id)}/metadata`), draft, parseTerminalDetail)
}

export function postTerminalViewerHeartbeat(scopedPath: ScopedPath): Promise<unknown> {
  return apiPost(scopedPath('/api/terminal-viewer-heartbeat'))
}

export function fetchProtocols(scopedPath: ScopedPath): Promise<ProtocolResponse> {
  return apiGet(scopedPath('/api/protocols'), parseProtocolResponse)
}

export function fetchPolicies(scopedPath: ScopedPath): Promise<PolicyStat[]> {
  return apiGet(scopedPath('/api/policies'), (value) => safeArray<unknown>(safeObject(value).policies).map(parsePolicyStat))
}

export function fetchRoutes(scopedPath: ScopedPath): Promise<RouteStat[]> {
  return apiGet(scopedPath('/api/routes'), (value) => safeArray<unknown>(safeObject(value).routes).map(parseRouteStat))
}

export function fetchDhcp(scopedPath: ScopedPath): Promise<DHCPStat> {
  return apiGet(scopedPath('/api/dhcp'), parseDHCPStat)
}

export function fetchLoadHistory(scopedPath: ScopedPath, window: LoadWindow): Promise<LoadSample[]> {
  return apiGet(scopedPath(`/api/load?window=${window}`), (value) => safeArray<unknown>(safeObject(value).samples).map(parseLoadSample))
}

export function fetchResourceOverview(scopedPath: ScopedPath): Promise<ResourceOverview> {
  return apiGet(scopedPath('/api/overview'), (value) => ({
    overview: parseOverview(value),
    resource: parseSystemResource(safeObject(value).systemResource),
  }))
}
