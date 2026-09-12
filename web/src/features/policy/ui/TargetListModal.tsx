import { useCallback, useEffect, useState } from 'react'
import { Badge } from '../../../ui/Badge'
import { Button } from '../../../ui/Button'
import { Field, Input, Select, Textarea } from '../../../ui/inputs'
import { Modal } from '../../../ui/Modal'
import { SegTabs } from '../../../ui/SegTabs'
import { Skeleton } from '../../../ui/Skeleton'
import {
  fetchTargetList,
  previewTargetList,
  saveTargetList,
  type TargetList,
  type TargetListPreview,
} from '../canonical'
import { jobIdOf } from '../api'
import { errorMessage } from '../../../lib/api'
import { Notice } from './Notice'
import { formatCount } from '../../../lib/format'

type SourceType = 'manual' | 'url' | 'upload'

const SCHEDULE_OPTIONS = [
  { value: '1h', label: '每 1 小时' },
  { value: '6h', label: '每 6 小时' },
  { value: '12h', label: '每 12 小时' },
  { value: '24h', label: '每天' },
  { value: '7d', label: '每 7 天' },
  { value: '30d', label: '每 30 天' },
]

export type TargetListSavedResult = { targetList: TargetList; jobId: string }

type TargetListModalProps = {
  deviceID: string
  /** null = 新建 */
  target: TargetList | null
  initialKind?: 'domain' | 'ip'
  onClose: () => void
  onSaved: (result: TargetListSavedResult) => void | Promise<void>
}

/**
 * Create/edit a target list. Content changes require a fresh preview
 * (15-min previewId TTL); save stays disabled until a preview succeeds.
 */
export function TargetListModal({ deviceID, target, initialKind = 'domain', onClose, onSaved }: TargetListModalProps) {
  const [name, setName] = useState(target?.name ?? '')
  const [kind, setKind] = useState<'domain' | 'ip'>(target?.kind ?? initialKind)
  const [sourceType, setSourceType] = useState<SourceType>(target?.sourceType === 'url' || target?.sourceType === 'upload' ? target.sourceType : 'manual')
  const [url, setURL] = useState(target?.url ?? '')
  const [text, setText] = useState('')
  const [loadedText, setLoadedText] = useState('')
  const [contentLoading, setContentLoading] = useState(Boolean(target?.sourceType === 'manual'))
  const [contentLoaded, setContentLoaded] = useState(!target || target.sourceType !== 'manual')
  const [contentLoadError, setContentLoadError] = useState<string | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [schedule, setSchedule] = useState(target?.schedule && target.schedule !== 'manual' ? target.schedule : '7d')
  const [preview, setPreview] = useState<TargetListPreview | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadManualContent = useCallback(async () => {
    if (!target || target.sourceType !== 'manual') return
    setContentLoading(true)
    setContentLoaded(false)
    setContentLoadError(null)
    try {
      const detail = await fetchTargetList(deviceID, target.id)
      if (detail.editableContent === undefined) throw new Error('目标库内容不可用')
      setText(detail.editableContent)
      setLoadedText(detail.editableContent)
      setContentLoaded(true)
    } catch (loadError) {
      setContentLoadError(errorMessage(loadError, '目标库内容读取失败'))
    } finally {
      setContentLoading(false)
    }
  }, [deviceID, target])

  useEffect(() => {
    if (target?.sourceType === 'manual') void loadManualContent()
  }, [loadManualContent, target?.sourceType])

  const canPreview = sourceType === 'url' ? Boolean(url.trim()) : sourceType === 'manual' ? Boolean(text.trim()) : Boolean(file)
  const contentDirty = target?.sourceType === 'manual' ? contentLoaded && text !== loadedText : target?.sourceType === 'url' ? url.trim() !== (target.url ?? '').trim() : target?.sourceType === 'upload' ? Boolean(file) : true
  const needsPreview = !target || contentDirty

  const doPreview = async () => {
    if (!canPreview || contentLoading || !contentLoaded || contentLoadError) return
    setPreviewing(true)
    setError(null)
    try {
      const input = sourceType === 'upload' ? (() => { const form = new FormData(); form.append('file', file as File); return form })() : sourceType === 'url' ? url.trim() : text
      setPreview(await previewTargetList(deviceID, kind, sourceType, input))
    } catch (previewError) {
      setPreview(null)
      setError(errorMessage(previewError, '预览失败'))
    } finally {
      setPreviewing(false)
    }
  }

  const submit = async () => {
    if (!name.trim() || contentLoading || !contentLoaded || (needsPreview && !preview?.previewId)) return
    setSaving(true)
    setError(null)
    try {
      const result = await saveTargetList(
        deviceID,
        {
          name: name.trim(),
          kind,
          sourceType,
          url: sourceType === 'url' ? url.trim() : undefined,
          schedule: sourceType === 'url' ? schedule : 'manual',
          enabled: target?.enabled ?? true,
          revision: target?.revision ?? 0,
        },
        target?.id,
        preview?.previewId,
      )
      if (!result.targetList) throw new Error('目标库保存响应无效')
      await onSaved({ targetList: result.targetList, jobId: jobIdOf(result) })
    } catch (saveError) {
      setError(errorMessage(saveError, '目标库保存失败'))
      setSaving(false)
    }
  }

  const ignoredEntries = preview ? Object.entries(preview.ignored).filter(([, count]) => count > 0) : []

  return (
    <Modal
      open
      persistent
      onClose={() => {
        if (!saving) onClose()
      }}
      title={target ? `编辑目标库：${target.name}` : '新建目标库'}
      maxWidth={720}
      footer={
        <>
          <Button disabled={saving} onClick={onClose}>
            取消
          </Button>
          <Button variant="primary" loading={saving} disabled={!name.trim() || contentLoading || !contentLoaded || (needsPreview && !preview?.previewId)} onClick={() => void submit()}>
            {needsPreview && !preview ? '先预览再保存' : '保存目标库'}
          </Button>
        </>
      }
    >
      {error ? <Notice tone="err">{error}</Notice> : null}
      {contentLoadError ? (
        <Notice tone="err" action={
          <button type="button" className="link-button" disabled={contentLoading} onClick={() => void loadManualContent()}>
            重试读取
          </button>
        }>
          {contentLoadError}
        </Notice>
      ) : null}
      <div className="pol-form-grid">
        <Field label="名称">
          <Input value={name} onChange={setName} placeholder="例如：视频站点" disabled={saving} />
        </Field>
        <Field label="类型" hint={target ? '创建后不可修改' : undefined}>
          <Select
            value={kind}
            disabled={Boolean(target) || saving}
            onChange={(value) => {
              setKind(value as 'domain' | 'ip')
              setPreview(null)
            }}
            options={[
              { value: 'domain', label: '域名列表' },
              { value: 'ip', label: 'IP 列表' },
            ]}
          />
        </Field>
      </div>
      <Field label="来源" hint={target ? '创建后不可修改' : undefined}>
        {target ? (
          <div>
            <Badge tone="neutral">{sourceType === 'url' ? 'URL 订阅' : sourceType === 'upload' ? '上传文件' : '手动粘贴'}</Badge>
          </div>
        ) : (
          <SegTabs
            options={[
              { value: 'manual', label: '手动粘贴' },
              { value: 'url', label: 'URL 订阅' },
              { value: 'upload', label: '上传文件' },
            ]}
            value={sourceType}
            onChange={(value) => {
              setSourceType(value)
              setPreview(null)
            }}
            ariaLabel="目标库来源"
          />
        )}
      </Field>
      {sourceType === 'url' ? (
        <div className="pol-form-grid">
          <Field label="订阅 URL">
            <Input
              type="url"
              value={url}
              onChange={(value) => {
                setURL(value)
                setPreview(null)
              }}
              placeholder="https://example.com/list.txt"
              disabled={saving}
            />
          </Field>
          <Field label="更新计划">
            <Select value={schedule} onChange={setSchedule} options={SCHEDULE_OPTIONS} disabled={saving} />
          </Field>
        </div>
      ) : null}
      {sourceType === 'upload' ? (
        <Field label="规则文件" hint="支持 .txt / .list / .yaml / .csv">
          <input
            className="input"
            type="file"
            accept=".txt,.list,.yaml,.yml,.csv"
            disabled={saving}
            onChange={(event) => {
              setFile(event.target.files?.[0] ?? null)
              setPreview(null)
            }}
          />
        </Field>
      ) : null}
      {sourceType === 'manual' ? (
        <Field label="内容" hint={contentLoading ? '正在读取已保存内容…' : kind === 'ip' ? '每行一个 IP 或 CIDR。' : '每行一个域名；支持精确 / 后缀匹配。'}>
          {contentLoading ? (
            <Skeleton lines={4} height={12} />
          ) : (
            <Textarea
              rows={8}
              value={text}
              disabled={Boolean(contentLoadError) || saving}
              onChange={(value) => {
                setText(value)
                setPreview(null)
              }}
              placeholder={kind === 'ip' ? '203.0.113.0/24' : 'example.com'}
              ariaLabel="目标库内容"
            />
          )}
        </Field>
      ) : null}
      <div className="pol-preview-actions">
        <Button disabled={!canPreview || contentLoading || !contentLoaded || Boolean(contentLoadError) || previewing || saving} loading={previewing} onClick={() => void doPreview()}>
          预览并校验
        </Button>
        {needsPreview ? <span className="faint">内容有变化，保存前必须先预览。</span> : <span className="faint">内容未变化，可直接保存。</span>}
      </div>
      {preview ? (
        <div className="pol-preview-result">
          <Notice tone={preview.errorSamples.length ? 'warn' : 'ok'} title="预览结果">
            有效规则 {formatCount(preview.validRules)} 条
            {kind !== 'ip' && (preview.counts['DOMAIN-KEYWORD'] ?? 0) > 0 ? `；DOMAIN-KEYWORD ${formatCount(preview.counts['DOMAIN-KEYWORD'])} 条` : ''}
            {ignoredEntries.length ? `；忽略：${ignoredEntries.map(([key, count]) => `${key} ${count}`).join('、')}` : ''}
            {preview.errorSamples.length ? `；${preview.errorSamples.length} 条错误样例` : ''}。预览凭证 15 分钟内有效。
          </Notice>
          {preview.errorSamples.length ? (
            <div className="pol-preview-errors">
              {preview.errorSamples.slice(0, 5).map((sample, index) => (
                <code key={`${sample}-${index}`}>{sample}</code>
              ))}
            </div>
          ) : null}
          {preview.rules.length ? (
            <div className="pol-preview-rules">
              <div className="pol-preview-rules-head">
                <span className="field-label">前 {Math.min(preview.rules.length, 100)} 条规则</span>
                <Badge tone="neutral">共 {formatCount(preview.validRules)} 条</Badge>
                {kind !== 'ip' && (preview.counts['DOMAIN-KEYWORD'] ?? 0) > 0 ? <Badge tone="neutral">关键字 {formatCount(preview.counts['DOMAIN-KEYWORD'])}</Badge> : null}
              </div>
              <div className="pol-preview-rules-table table-scroll">
                <table className="table">
                  <thead>
                    <tr>
                      <th>类型</th>
                      <th>{kind === 'ip' ? '地址' : '域名'}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.rules.slice(0, 100).map((rule, index) => (
                      <tr key={`${rule.type}:${rule.domain ?? rule.address ?? index}`}>
                        <td>{rule.type}</td>
                        <td>{rule.domain ?? rule.address ?? ''}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
    </Modal>
  )
}
