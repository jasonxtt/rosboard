/**
 * Pure terminal presentation helpers ported from the previous UI
 * (state text/tone, family-scoped metrics, numeric IP sorting).
 */

import type { Terminal, TerminalFamily, TerminalState } from '../../lib/types'

export function terminalStateText(state: TerminalState): string {
  if (state === 'online') return '在线'
  if (state === 'inactive') return '空闲'
  return '离线'
}

export function terminalStateTone(state: TerminalState): 'ok' | 'warn' | 'err' {
  if (state === 'online') return 'ok'
  if (state === 'inactive') return 'warn'
  return 'err'
}

/** Family-scoped metrics: `all` uses the combined terminal counters. */
export function terminalMetrics(terminal: Terminal, family: TerminalFamily) {
  if (family === 'all' || !terminal.familyStats?.[family]) return terminal
  const stats = terminal.familyStats[family]
  return {
    connectionCount: stats.connectionCount,
    currentUploadBps: stats.currentUploadBps,
    currentDownloadBps: stats.currentDownloadBps,
    totalUploadBytes: stats.activeUploadBytes,
    totalDownloadBytes: stats.activeDownloadBytes,
  }
}

export function terminalPrimaryAddress(terminal: Terminal, family: TerminalFamily): string {
  if (family === 'ipv4') return terminal.primaryIpv4
  if (family === 'ipv6') return terminal.primaryIpv6
  return terminal.primaryIpv4 || terminal.primaryIpv6
}

export function compareIp(left: string, right: string): number {
  if (left && !right) return -1
  if (!left && right) return 1
  if (!left && !right) return 0
  const leftV4 = left.split('.').map(Number)
  const rightV4 = right.split('.').map(Number)
  if (leftV4.length === 4 && rightV4.length === 4 && [...leftV4, ...rightV4].every((part) => Number.isFinite(part))) {
    for (let index = 0; index < 4; index += 1) {
      if (leftV4[index] !== rightV4[index]) return leftV4[index] - rightV4[index]
    }
    return 0
  }
  return left.localeCompare(right, 'en', { numeric: true })
}

export type TerminalSortKey = 'address' | 'device' | 'connections' | 'upload' | 'download' | 'totalUpload' | 'totalDownload' | 'online' | 'remark'

export function compareTerminal(left: Terminal, right: Terminal, key: TerminalSortKey, family: TerminalFamily): number {
  const text = (a: string, b: string) => a.localeCompare(b, 'zh-CN', { numeric: true, sensitivity: 'base' })
  const leftMetrics = terminalMetrics(left, family)
  const rightMetrics = terminalMetrics(right, family)
  switch (key) {
    case 'address':
      if (family === 'ipv4') return compareIp(left.primaryIpv4, right.primaryIpv4) || text(left.macAddress, right.macAddress)
      if (family === 'ipv6') return compareIp(left.primaryIpv6, right.primaryIpv6) || text(left.macAddress, right.macAddress)
      return compareIp(left.primaryIpv4, right.primaryIpv4) || compareIp(left.primaryIpv6, right.primaryIpv6) || text(left.macAddress, right.macAddress)
    case 'device':
      return text(left.displayName, right.displayName)
    case 'connections':
      return leftMetrics.connectionCount - rightMetrics.connectionCount
    case 'upload':
      return leftMetrics.currentUploadBps - rightMetrics.currentUploadBps
    case 'download':
      return leftMetrics.currentDownloadBps - rightMetrics.currentDownloadBps
    case 'totalUpload':
      return leftMetrics.totalUploadBytes - rightMetrics.totalUploadBytes
    case 'totalDownload':
      return leftMetrics.totalDownloadBytes - rightMetrics.totalDownloadBytes
    case 'online':
      return new Date(left.onlineSince || 0).getTime() - new Date(right.onlineSince || 0).getTime()
    case 'remark':
      return text(left.remark, right.remark)
  }
}

/** seconds since `iso` formatted with the shared duration formatter. */
export function onlineDurationSeconds(iso: string): number {
  const timestamp = new Date(iso).getTime()
  if (!iso || !Number.isFinite(timestamp)) return 0
  return Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
}
