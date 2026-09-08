/**
 * Display formatters. Rates and byte amounts use the two-part
 * 「数值 + 单位」form (UX standard §4): `split*` helpers return both parts so
 * metric heroes can style the unit separately; `format*` helpers join them.
 */

export type UnitValue = { value: string; unit: string }

function trimNumber(value: number): string {
  return value.toFixed(value >= 100 ? 0 : value >= 10 ? 1 : 2)
}

/** bits per second → bps/Kbps/Mbps/Gbps/Tbps (never divided by eight). */
export function splitBitRate(bps: number): UnitValue {
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps']
  let output = Math.max(0, Number.isFinite(bps) ? bps : 0)
  let index = 0
  while (output >= 1000 && index < units.length - 1) {
    output /= 1000
    index += 1
  }
  return { value: trimNumber(output), unit: units[index] }
}

export function formatBitRate(bps: number): string {
  const { value, unit } = splitBitRate(bps)
  return `${value} ${unit}`
}

/** bytes → B/KB/MB/GB/TB (1024-based). */
export function splitBytes(bytes: number): UnitValue {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let output = Math.max(0, Number.isFinite(bytes) ? bytes : 0)
  let index = 0
  while (output >= 1024 && index < units.length - 1) {
    output /= 1024
    index += 1
  }
  return { value: trimNumber(output), unit: units[index] }
}

export function formatBytes(bytes: number): string {
  const { value, unit } = splitBytes(bytes)
  return `${value} ${unit}`
}

/** bytes per second → 「数值 + B/s|KB/s|…」 */
export function formatByteRate(bytesPerSecond: number): string {
  const { value, unit } = splitBytes(bytesPerSecond)
  return `${value} ${unit}/s`
}

/** 0..100 → 「34%」; non-finite input renders as 0%. */
export function formatPercent(percent: number): string {
  const safe = Number.isFinite(percent) ? Math.min(100, Math.max(0, percent)) : 0
  return `${Math.round(safe)}%`
}

/** integer with thousands separators → 「1,482」 */
export function formatCount(value: number): string {
  return Math.round(Number.isFinite(value) ? value : 0).toLocaleString('zh-CN')
}

/** seconds → 「23 天 4 小时」/「4 小时 12 分」/「42 秒」 */
export function formatDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(Number.isFinite(totalSeconds) ? totalSeconds : 0))
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remain = seconds % 60
  if (days > 0) return hours > 0 ? `${days} 天 ${hours} 小时` : `${days} 天`
  if (hours > 0) return minutes > 0 ? `${hours} 小时 ${minutes} 分` : `${hours} 小时`
  if (minutes > 0) return `${minutes} 分 ${remain} 秒`
  return `${remain} 秒`
}

/** ISO timestamp → 「刚刚 / N 秒前 / N 分钟前 / N 小时前 / N 天前」 */
export function formatRelativeTime(iso: string): string {
  const timestamp = new Date(iso).getTime()
  if (!iso || !Number.isFinite(timestamp)) return '未知时间'
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000))
  if (seconds < 10) return '刚刚'
  if (seconds < 60) return `${seconds} 秒前`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} 分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days} 天前`
  return formatDateTime(iso)
}

/** ISO timestamp → 「MM-DD HH:mm:ss」 */
export function formatDateTime(iso: string): string {
  if (!iso) return '-'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

/** ISO timestamp → 「HH:mm:ss」 */
export function formatClock(iso: string): string {
  if (!iso) return '-'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

/** RouterOS uptime text (「1w2d」/「4:05:06」) → readable Chinese, passes through unknown shapes. */
export function formatUptime(uptime: string): string {
  const text = uptime.trim()
  if (!text) return '-'
  const match = /^(?:(\d+)w)?(?:(\d+)d)?([\d:]+)?$/.exec(text)
  if (!match) return text
  const weeks = Number(match[1] ?? 0)
  const days = Number(match[2] ?? 0)
  const totalDays = weeks * 7 + days
  const hms = match[3] ?? ''
  const hours = Number(hms.split(':')[0] ?? 0)
  if (totalDays > 0) return hours > 0 ? `${totalDays} 天 ${hours} 小时` : `${totalDays} 天`
  if (hms) {
    const parts = hms.split(':').map(Number)
    if (parts.length === 3) return `${parts[0]} 小时 ${parts[1]} 分`
    if (parts.length === 2) return `${parts[0]} 分 ${parts[1]} 秒`
  }
  return text
}
