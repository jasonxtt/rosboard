import type { ActiveView, Terminal, TerminalFamily, TerminalSortKey } from './types'

export function viewTitle(view: ActiveView) {
  const titles: Record<ActiveView, string> = {
    fleet: '仪表台',
    overview: '系统概览',
    interfaces: '接口监控',
    terminals: '终端监控',
    load: '负载历史',
    resource: '资源监控',
    protocols: '协议统计',
    policies: '策略统计',
    dhcp: 'DHCP',
    routes: '路由 / 分流',
    settings: '面板设置',
    'target-library': '目标库',
    'policy-routing': '策略路由',
    'access-control': '访问控制',
    recognition: '识别设置',
  }
  return titles[view]
}

export function terminalStateText(value: Terminal['state']) {
  if (value === 'online') return '在线'
  if (value === 'inactive') return '近期未活跃'
  return '离线'
}

export function formatBits(value: number) {
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let output = value / 8
  let index = 0
  while (output >= 1024 && index < units.length - 1) {
    output /= 1024
    index += 1
  }
  return `${output.toFixed(output >= 100 ? 0 : output >= 10 ? 1 : 2)} ${units[index]}`
}

export function formatBitRate(value: number) {
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps']
  let output = Math.max(0, Number.isFinite(value) ? value : 0)
  let index = 0
  while (output >= 1000 && index < units.length - 1) {
    output /= 1000
    index += 1
  }
  return `${output.toFixed(output >= 100 ? 0 : output >= 10 ? 1 : 2)} ${units[index]}`
}

export function formatBytes(value: number) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let output = value
  let index = 0
  while (output >= 1024 && index < units.length - 1) {
    output /= 1024
    index += 1
  }
  return `${output.toFixed(output >= 100 ? 0 : output >= 10 ? 1 : 2)} ${units[index]}`
}

export function formatDateTime(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
  })
}

export function formatShortTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export function formatSeconds(seconds: number) {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remainSeconds = seconds % 60
  if (days > 0) return `${days}天${hours}小时${minutes}分`
  if (hours > 0) return `${hours}小时${minutes}分${remainSeconds}秒`
  if (minutes > 0) return `${minutes}分${remainSeconds}秒`
  return `${remainSeconds}秒`
}

export function formatOnlineDuration(trackingSince: string) {
  if (!trackingSince) return '-'
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(trackingSince).getTime()) / 1000))
  return formatSeconds(seconds)
}

export function terminalPrimaryAddress(terminal: Terminal, family: TerminalFamily) {
  if (family === 'ipv4') return terminal.primaryIpv4 || '-'
  if (family === 'ipv6') return terminal.primaryIpv6 || '-'
  return terminal.primaryIpv4 || terminal.primaryIpv6 || '-'
}

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

export function compareTerminal(left: Terminal, right: Terminal, key: TerminalSortKey, family: TerminalFamily) {
  const text = (a: string, b: string) => a.localeCompare(b, 'zh-CN', { numeric: true, sensitivity: 'base' })
  const leftMetrics = terminalMetrics(left, family)
  const rightMetrics = terminalMetrics(right, family)
  switch (key) {
    case 'address':
      if (family === 'ipv4') return compareIp(left.primaryIpv4, right.primaryIpv4) || text(left.macAddress, right.macAddress)
      if (family === 'ipv6') return compareIp(left.primaryIpv6, right.primaryIpv6) || text(left.macAddress, right.macAddress)
      return compareIp(left.primaryIpv4, right.primaryIpv4) || compareIp(left.primaryIpv6, right.primaryIpv6) || text(left.macAddress, right.macAddress)
    case 'connections': return leftMetrics.connectionCount - rightMetrics.connectionCount
    case 'upload': return leftMetrics.currentUploadBps - rightMetrics.currentUploadBps
    case 'download': return leftMetrics.currentDownloadBps - rightMetrics.currentDownloadBps
    case 'totalUpload': return leftMetrics.totalUploadBytes - rightMetrics.totalUploadBytes
    case 'totalDownload': return leftMetrics.totalDownloadBytes - rightMetrics.totalDownloadBytes
    case 'online': return new Date(left.onlineSince || 0).getTime() - new Date(right.onlineSince || 0).getTime()
    case 'device': return text(left.displayName, right.displayName)
    case 'remark': return text(left.remark, right.remark)
  }
}

function compareIp(left: string, right: string) {
  if (left && !right) return -1
  if (!left && right) return 1
  if (!left && !right) return 0
  const leftV4 = left.split('.').map(Number)
  const rightV4 = right.split('.').map(Number)
  if (leftV4.length === 4 && rightV4.length === 4) {
    for (let index = 0; index < 4; index += 1) {
      if (leftV4[index] !== rightV4[index]) return leftV4[index] - rightV4[index]
    }
    return 0
  }
  return left.localeCompare(right, 'en', { numeric: true })
}
