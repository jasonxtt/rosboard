import { useEffect, useRef, useState } from 'react'
import { Button, CopyButton, Field, Input, Select, Skeleton } from '../../ui'
import { ApiError, errorMessage } from '../../lib/api'
import {
  completeOnboardingSession,
  createOnboardingSession,
  previewOnboardingSession,
  previewScope,
  type MutationResult,
  type ProvisioningSession,
  type VerificationResult,
} from './api'
import { emptyScopeOverrideDraft, scopeConfigsFromOverrides, type ScopeOverrideDraft } from './drafts'
import { VerificationDialog } from './VerificationDialog'

type QuickOnboardingWizardProps = {
  /** Parent restart gate: receives the complete() mutation and runs the §9.4 wait. */
  onSaved: (action: () => Promise<MutationResult>) => Promise<void>
  busy: boolean
}

/**
 * 快速接入 wizard: name+host (+高级 scheme/port) → provisioning session →
 * script steps → 「我已执行脚本」 preview → VerificationDialog → complete.
 */
export function QuickOnboardingWizard({ onSaved, busy }: QuickOnboardingWizardProps) {
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [scheme, setScheme] = useState<'http' | 'https'>('http')
  const [port, setPort] = useState(80)
  const [generating, setGenerating] = useState(false)
  const [session, setSession] = useState<ProvisioningSession | null>(null)
  const [scriptVisible, setScriptVisible] = useState(false)
  const [checking, setChecking] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dialogError, setDialogError] = useState<string | null>(null)
  const [verification, setVerification] = useState<VerificationResult | null>(null)
  const [scopeDraft, setScopeDraft] = useState<ScopeOverrideDraft>(emptyScopeOverrideDraft)
  const [scopePreviewing, setScopePreviewing] = useState(false)
  const previewTimer = useRef<number | null>(null)
  const previewSequence = useRef(0)

  useEffect(
    () => () => {
      if (previewTimer.current !== null) window.clearTimeout(previewTimer.current)
    },
    [],
  )

  const changeScheme = (next: 'http' | 'https') => {
    setPort((current) => (current === 80 || current === 443 ? (next === 'https' ? 443 : 80) : current))
    setScheme(next)
  }

  const resetFlow = () => {
    setSession(null)
    setScriptVisible(false)
    setVerification(null)
    setScopeDraft(emptyScopeOverrideDraft)
    setDialogError(null)
  }

  const handleExpired = () => {
    resetFlow()
    setError('接入脚本已过期，请重新生成。')
  }

  const generate = async () => {
    if (!name.trim() || !host.trim()) return
    setGenerating(true)
    setError(null)
    resetFlow()
    try {
      setSession(await createOnboardingSession({ name: name.trim(), host: host.trim(), scheme, port }))
    } catch (generateError) {
      setError(errorMessage(generateError, '生成接入脚本失败'))
    } finally {
      setGenerating(false)
    }
  }

  const preview = async () => {
    if (!session) return
    setChecking(true)
    setError(null)
    setDialogError(null)
    try {
      const scopes = scopeConfigsFromOverrides(scopeDraft)
      const result = await previewOnboardingSession(session.sessionId, scopes)
      setVerification(result)
    } catch (previewError) {
      if (previewError instanceof ApiError && previewError.code === 'provisioning_expired') {
        handleExpired()
      } else {
        setError(errorMessage(previewError, 'RouterOS 连接检测失败'))
      }
    } finally {
      setChecking(false)
    }
  }

  const handleScopeChange = (field: keyof ScopeOverrideDraft, value: string) => {
    const next = { ...scopeDraft, [field]: value }
    setScopeDraft(next)
    const token = verification?.verificationToken
    if (!token) return
    if (previewTimer.current !== null) window.clearTimeout(previewTimer.current)
    const sequence = previewSequence.current + 1
    previewSequence.current = sequence
    setScopePreviewing(true)
    previewTimer.current = window.setTimeout(() => {
      void (async () => {
        try {
          const scopes = scopeConfigsFromOverrides(next)
          const previewResult = await previewScope({ verificationToken: token, ...scopes })
          if (previewSequence.current !== sequence) return
          setVerification((current) =>
            current ? { ...current, trafficScope: previewResult.trafficScope, terminalScope: previewResult.terminalScope } : current,
          )
          setDialogError(null)
        } catch (previewError) {
          if (previewSequence.current === sequence) setDialogError(errorMessage(previewError, '范围预览失败'))
        } finally {
          if (previewSequence.current === sequence) setScopePreviewing(false)
        }
      })()
    }, 350)
  }

  const confirm = async () => {
    if (!session || !verification?.verificationToken) {
      setDialogError('请先完成连接检测，检测结果有效后才能保存。')
      return
    }
    setSaving(true)
    setDialogError(null)
    const scopes = scopeConfigsFromOverrides(scopeDraft)
    const token = verification.verificationToken
    try {
      await onSaved(() =>
        completeOnboardingSession(session.sessionId, {
          verificationToken: token,
          trafficScope: scopes.trafficScope,
          terminalScope: scopes.terminalScope,
          completeOnboarding: false,
          deferRestart: false,
        }),
      )
      // Success path restarts the panel and reloads the page.
    } catch (saveError) {
      if (saveError instanceof ApiError && saveError.code === 'provisioning_expired') {
        handleExpired()
      } else if (saveError instanceof ApiError && saveError.code === 'verification_required') {
        setDialogError('检测结果已过期，请返回重新检测。')
      } else {
        setDialogError(errorMessage(saveError, '接入失败'))
      }
      setSaving(false)
    }
  }

  if (!session) {
    return (
      <form
        className="quick-form"
        onSubmit={(event) => {
          event.preventDefault()
          void generate()
        }}
      >
        <div className="form-grid">
          <Field label="设备名称">
            <Input value={name} onChange={setName} placeholder="例如：主路由" required maxLength={64} />
          </Field>
          <Field label="RouterOS IP / 主机名">
            <Input value={host} onChange={setHost} placeholder="10.0.0.1" required />
          </Field>
        </div>
        <details className="verify-details">
          <summary>
            <span>
              <strong>高级设置</strong>
              <small className="faint">协议和端口</small>
            </span>
          </summary>
          <div className="form-grid">
            <Field label="协议">
              <Select value={scheme} onChange={(value) => changeScheme(value === 'https' ? 'https' : 'http')} options={[{ value: 'http', label: 'HTTP' }, { value: 'https', label: 'HTTPS' }]} ariaLabel="协议" />
            </Field>
            <Field label="REST 端口">
              <Input type="number" min={1} max={65535} value={String(port)} onChange={(value) => setPort(Number(value) || 0)} />
            </Field>
          </div>
        </details>
        <p className="form-hint">默认通过可信局域网内的 HTTP 连接 RouterOS。HTTP 会明文传输登录凭据；如需 HTTPS，请在高级设置中修改。</p>
        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="form-actions">
          <Button type="submit" variant="primary" disabled={generating || busy || !name.trim() || !host.trim()} loading={generating}>
            {generating ? '正在生成…' : '生成接入脚本'}
          </Button>
        </div>
      </form>
    )
  }

  return (
    <div className="quick-steps">
      <div className="quick-step">
        <div className="quick-step-head">
          <span className="quick-step-index">1</span>
          <strong>复制脚本</strong>
        </div>
        <p className="faint">直接复制完整脚本，无需查看内容；需要核对时可展开。</p>
        <div className="quick-step-actions">
          <Button size="sm" onClick={() => setScriptVisible((visible) => !visible)} title={scriptVisible ? '隐藏脚本' : '查看脚本'}>
            {scriptVisible ? '隐藏脚本' : '查看脚本'}
          </Button>
          <CopyButton text={session.script} label="复制接入脚本" />
          <Button size="sm" disabled={busy || checking} onClick={resetFlow}>
            重新生成脚本
          </Button>
        </div>
        {scriptVisible ? (
          <textarea className="textarea script-area" readOnly value={session.script} rows={12} spellCheck={false} aria-label="RouterOS 接入脚本" />
        ) : null}
      </div>

      <div className="quick-step">
        <div className="quick-step-head">
          <span className="quick-step-index">2</span>
          <strong>在 RouterOS 执行</strong>
        </div>
        <p className="faint">打开 WinBox / WebFig / SSH 中的 Terminal，以管理员账号登录，把整段脚本粘贴并执行。看到 “rosboard account ready” 后再返回本页。</p>
      </div>

      <div className="quick-step">
        <div className="quick-step-head">
          <span className="quick-step-index">3</span>
          <strong>检测并确认</strong>
        </div>
        <p className="faint">
          脚本将在 <span className="num">{new Date(session.expiresAt).toLocaleString('zh-CN')}</span> 过期。检测完成后会展示识别到的 WAN/LAN 范围。
        </p>
        {checking ? <Skeleton lines={2} height={12} /> : null}
        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="form-actions">
          <Button variant="primary" disabled={checking || busy || saving} loading={checking} onClick={() => void preview()}>
            {checking ? '正在检测 RouterOS…' : '我已执行脚本'}
          </Button>
        </div>
      </div>

      {verification ? (
        <VerificationDialog
          result={verification}
          scopeDraft={scopeDraft}
          onScopeChange={handleScopeChange}
          scopePreviewing={scopePreviewing}
          busy={saving || busy}
          error={dialogError}
          confirmLabel="确认保存并启动采集"
          busyLabel="正在保存并启动…"
          onCancel={() => {
            if (!saving && !busy) {
              setVerification(null)
              setDialogError(null)
            }
          }}
          onConfirm={() => void confirm()}
        />
      ) : null}
    </div>
  )
}
