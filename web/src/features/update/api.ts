import { apiGet, apiPost, safeBoolean, safeNumber, safeObject, safeString } from '../../lib/api'

export type UpdateJob = { id: string; from: string; to: string; stage: string; startedAt: string; finishedAt: string; message: string; downloaded: number; total: number }
export type UpdateStatus = {
  current: { version: string; os: string; arch: string }
  latest: { version: string; publishedAt: string; notes: string; url: string } | null
  checkedAt: string
  checkError: string
  canInstall: boolean
  reason: string
  job: UpdateJob | null
}
export function parseUpdateStatus(value: unknown): UpdateStatus {
  const o = safeObject(value), current = safeObject(o.current), latest = safeObject(o.latest), job = safeObject(o.job)
  const url = safeString(latest.url)
  return {
    current: { version: safeString(current.version), os: safeString(current.os), arch: safeString(current.arch) },
    latest: o.latest && safeString(latest.version) ? { version: safeString(latest.version), publishedAt: safeString(latest.publishedAt), notes: safeString(latest.notes), url: /^https:\/\/github\.com\/jasonxtt\/rosboard\/releases\/tag\/v?[0-9]+\.[0-9]+\.[0-9]+$/.test(url) ? url : 'https://github.com/jasonxtt/rosboard/releases' } : null,
    checkedAt: safeString(o.checkedAt), checkError: safeString(o.checkError), canInstall: safeBoolean(o.canInstall), reason: safeString(o.reason),
    job: o.job && safeString(job.id) ? { id: safeString(job.id), from: safeString(job.from), to: safeString(job.to), stage: safeString(job.stage), startedAt: safeString(job.startedAt), finishedAt: safeString(job.finishedAt), message: safeString(job.message), downloaded: safeNumber(job.downloaded), total: safeNumber(job.total) } : null,
  }
}
export const fetchUpdate = (signal?: AbortSignal) => apiGet('/api/settings/update', parseUpdateStatus, signal)
export const checkUpdate = () => apiPost('/api/settings/update/check', undefined, parseUpdateStatus)
export const installUpdate = (version: string) => apiPost('/api/settings/update/install', { version }, parseUpdateStatus)
export const activeUpdate = (job: UpdateJob | null | undefined) => !!job && !['succeeded', 'failed', 'rolled_back'].includes(job.stage)
