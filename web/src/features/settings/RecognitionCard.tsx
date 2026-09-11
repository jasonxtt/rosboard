import { useEffect, useState } from 'react'
import { Badge, Button, Field, Input, Skeleton, Toggle, toast } from '../../ui'
import { errorMessage } from '../../lib/api'
import { formatDateTime, formatRelativeTime } from '../../lib/format'
import { fetchMosDNS, fetchRecognition, saveRecognitionSettings, type MosDNSStatus } from './api'
import type { RestartingActionState } from './hooks'

type RecognitionCardProps = {
  deviceId: string
  deviceName: string
  restartGate: RestartingActionState
}

type RecognitionDraft = {
  protocolAnalysis: boolean
  mosdns: {
    enabled: boolean
    baseUrl: string
    syncIntervalMinutes: number
    matchWindowMinutes: number
  }
}

/** Stored baseUrl may be a full URL; the editor shows host[:port] only. */
function mosDNSAddressFromBaseURL(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  try {
    const parsed = new URL(trimmed.includes('://') ? trimmed : `http://${trimmed}`)
    return `${parsed.hostname}${parsed.port ? `:${parsed.port}` : ''}`
  } catch {
    return trimmed.replace(/^https?:\/\//i, '')
  }
}

function StatItem({ label, value, wide = false, danger = false }: { label: string; value: string; wide?: boolean; danger?: boolean }) {
  return (
    <div className={`stat-item${wide ? ' stat-item-wide' : ''}`}>
      <span>{label}</span>
      <b className={danger ? 'stat-danger' : undefined}>{value}</b>
    </div>
  )
}

/**
 * 识别设置 per-device card: 协议分析 toggle + MosDNS 归因 (enabled, 地址,
 * 同步周期, 实时证据窗口) + runtime stats → POST /api/settings/recognition.
 */
export function RecognitionCard({ deviceId, deviceName, restartGate }: RecognitionCardProps) {
  const [draft, setDraft] = useState<RecognitionDraft | null>(null)
  const [status, setStatus] = useState<MosDNSStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setLoadError(null)
    Promise.all([fetchRecognition(deviceId), fetchMosDNS(deviceId)])
      .then(([recognition, mosdns]) => {
        if (cancelled) return
        setDraft({
          protocolAnalysis: recognition.protocolAnalysis,
          mosdns: {
            enabled: recognition.mosdns.enabled,
            baseUrl: mosDNSAddressFromBaseURL(recognition.mosdns.baseUrl),
            syncIntervalMinutes: recognition.mosdns.syncIntervalMinutes || 5,
            matchWindowMinutes: recognition.mosdns.matchWindowMinutes || 30,
          },
        })
        setStatus(mosdns)
      })
      .catch((error) => {
        if (!cancelled) setLoadError(errorMessage(error, '识别设置读取失败'))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [deviceId])

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setSaveError(null)
    try {
      await restartGate.run(
        () =>
          saveRecognitionSettings([
            {
              id: deviceId,
              protocolAnalysis: draft.protocolAnalysis,
              mosdns: {
                enabled: draft.mosdns.enabled,
                baseUrl: draft.mosdns.baseUrl.trim(),
                syncIntervalMinutes: draft.mosdns.syncIntervalMinutes,
                matchWindowMinutes: draft.mosdns.matchWindowMinutes,
              },
            },
          ]),
        () => toast('识别设置已保存'),
      )
      // Restarting path reloads the page.
    } catch (error) {
      setSaveError(errorMessage(error, '识别设置保存失败'))
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="recognition-card-loading">
        <Skeleton lines={5} height={14} />
      </div>
    )
  }
  if (loadError || !draft) {
    return (
      <p className="form-error" role="alert">
        {loadError ?? '识别设置读取失败'}
      </p>
    )
  }

  const runtime = status

  return (
    <form
      className="recognition-form"
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
    >
      <section className="recognition-section" aria-label={`协议分析 · ${deviceName}`}>
        <div className="recognition-toggle-row">
          <div>
            <strong>协议分析</strong>
            <p className="faint">关闭后保留原始连接采集，但不进行实时应用归因、协议聚合和协议页面统计。</p>
          </div>
          <Toggle checked={draft.protocolAnalysis} disabled={restartGate.waiting || saving} label={`协议分析 · ${deviceName}`} onChange={(checked) => setDraft((current) => current && { ...current, protocolAnalysis: checked })} />
        </div>
      </section>

      <section className="recognition-section" aria-label={`MosDNS 应用归因 · ${deviceName}`}>
        <div className="recognition-toggle-row">
          <div>
            <strong>MosDNS 应用归因</strong>
            <p className="faint">MosDNS 仅提供流量统计中的应用归因证据，不改变目标库与访问规则。</p>
          </div>
          <Toggle
            checked={draft.mosdns.enabled}
            disabled={restartGate.waiting || saving}
            label={`MosDNS · ${deviceName}`}
            onChange={(checked) => setDraft((current) => current && { ...current, mosdns: { ...current.mosdns, enabled: checked } })}
          />
        </div>
        <div className="form-grid form-grid-three">
          <Field label="MosDNS 地址" hint="例如 10.0.0.3 或 10.0.0.3:1053">
            <Input
              value={draft.mosdns.baseUrl}
              onChange={(value) => setDraft((current) => current && { ...current, mosdns: { ...current.mosdns, baseUrl: value } })}
              placeholder="10.0.0.3"
              disabled={!draft.mosdns.enabled}
              required={draft.mosdns.enabled}
              autoComplete="off"
            />
          </Field>
          <Field label="同步周期（分钟）">
            <Input
              type="number"
              min={1}
              required
              disabled={!draft.mosdns.enabled}
              value={String(draft.mosdns.syncIntervalMinutes)}
              onChange={(value) => setDraft((current) => current && { ...current, mosdns: { ...current.mosdns, syncIntervalMinutes: Number(value) || 0 } })}
            />
          </Field>
          <Field label="实时证据窗口（分钟）" hint="窗口内的 DNS 证据标记为「MosDNS 匹配」，更早学习到的特征标记为「特征推断」">
            <Input
              type="number"
              min={1}
              required
              disabled={!draft.mosdns.enabled}
              value={String(draft.mosdns.matchWindowMinutes)}
              onChange={(value) => setDraft((current) => current && { ...current, mosdns: { ...current.mosdns, matchWindowMinutes: Number(value) || 0 } })}
            />
          </Field>
        </div>

        {draft.mosdns.enabled && runtime ? (
          <div className="recognition-stats" aria-label="MosDNS 运行状态">
            <div className="recognition-stats-head">
              <h4>运行状态</h4>
              {runtime.lastError ? (
                <Badge tone="err" dot>
                  异常
                </Badge>
              ) : (
                <Badge tone="ok" dot>
                  运行中
                </Badge>
              )}
            </div>
            <div className="stat-grid">
              <StatItem label="最近成功同步" value={runtime.lastSuccess ? formatRelativeTime(runtime.lastSuccess) : '-'} />
              <StatItem label="最近导入" value={`${runtime.lastImported} 条`} />
              <StatItem label="最近去重" value={`${runtime.lastDuplicates} 条`} />
              <StatItem label="最近跳过" value={`${runtime.lastSkipped} 条`} />
              <StatItem label="长期 IP 特征" value={`${runtime.learnedFeatureCount} 条`} />
              <StatItem label="最近学习" value={runtime.learnedFeatureLastSeen ? formatDateTime(runtime.learnedFeatureLastSeen) : '-'} />
              <StatItem label="当前水位" value={runtime.watermark ? formatDateTime(runtime.watermark) : '-'} />
              <StatItem label="最近错误" value={runtime.lastError || '无'} wide danger={Boolean(runtime.lastError)} />
            </div>
          </div>
        ) : null}
      </section>

      {saveError ? (
        <p className="form-error" role="alert">
          {saveError}
        </p>
      ) : null}
      <div className="form-actions">
        <Button type="submit" variant="primary" disabled={saving || restartGate.waiting} loading={saving}>
          {saving ? '保存中…' : '保存并重启识别服务'}
        </Button>
      </div>
    </form>
  )
}
