import type { PolicyPreview, PolicySourceType } from './types'

export type PreviewState = {
  loading: boolean
  error: string | null
  preview: PolicyPreview | null
}

export const emptyPreviewState: PreviewState = {
  loading: false,
  error: null,
  preview: null,
}

export function sourceTypeLabel(type: PolicySourceType | string): string {
  if (type === 'url') return '远程 URL'
  if (type === 'upload') return '本地上传'
  return type
}

export function isUrlSource(type: PolicySourceType | string): boolean {
  return type === 'url'
}

export function isUploadSource(type: PolicySourceType | string): boolean {
  return type === 'upload'
}

export function previewSummaryText(preview: PolicyPreview | null): string {
  if (!preview) return ''
  const parts: string[] = []
  parts.push(`有效规则 ${preview.validRules}`)
  if (preview.size) parts.push(`${preview.size} 字节`)
  if (preview.sha256) parts.push(`SHA-256 ${preview.sha256.slice(0, 16)}…`)
  return parts.join(' · ')
}
