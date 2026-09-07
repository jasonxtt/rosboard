import { useCallback, useRef, useState } from 'react'
import { ApiError } from '../lib/api'
import { formatClock } from '../lib/format'
import { Badge, Button, CopyButton, Field, Input, Select, StatusDot, ToastHost } from '../ui'
import {
  completeOnboardingSession,
  completeSetup,
  createOnboardingSession,
  friendlyError,
  previewOnboardingSession,
  probeBootstrap,
  type OnboardingPreview,
  type OnboardingSession,
} from '../features/monitoring/api'
import { useSettingsSummary } from '../features/monitoring/hooks'
import './setup.css'

type Stage = 'choice' | 'form' | 'script' | 'verified' | 'finishing'

const STEPS = ['填写连接', '执行脚本', '完成接入']

const RESTART_POLL_MS = 1500
const RESTART_TIMEOUT_MS = 60_000

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

function defaultPort(scheme: string): number {
  return scheme === 'https' ? 443 : 80
}

function SetupBrand({ title, description }: { title: string; description: string }) {
  return (
    <div className="setup-brand">
      <span className="logo" aria-hidden="true">
        R
      </span>
      <div>
        <h1>{title}</h1>
        <p className="muted">{description}</p>
      </div>
    </div>
  )
}

function StepIndicator({ stage }: { stage: Stage }) {
  const activeIndex = stage === 'form' ? 0 : stage === 'script' ? 1 : 2
  return (
    <ol className="setup-steps" aria-label="接入步骤">
      {STEPS.map((label, index) => (
        <li key={label} className={`setup-step${index === activeIndex ? ' on' : index < activeIndex ? ' done' : ''}`}>
          <span className="setup-step-n" aria-hidden="true">
            {index < activeIndex ? '✓' : index + 1}
          </span>
          {label}
        </li>
      ))}
    </ol>
  )
}

export default function RouterOSSetupPage({ onComplete }: { onComplete: () => void }) {
  const { summary } = useSettingsSummary()
  const [stage, setStage] = useState<Stage>('choice')
  const [error, setError] = useState<string | null>(null)
  const [sessionExpired, setSessionExpired] = useState(false)

  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [scheme, setScheme] = useState('http')
  const [port, setPort] = useState('80')
  const portTouchedRef = useRef(false)

  const [session, setSession] = useState<OnboardingSession | null>(null)
  const [scriptVisible, setScriptVisible] = useState(false)
  const [preview, setPreview] = useState<OnboardingPreview | null>(null)
  const [busy, setBusy] = useState<'skip' | 'create' | 'preview' | 'complete' | null>(null)

  /** Wait out the scheduled backend restart, then hand back to the bootstrap gate. */
  const finishAfterRestart = useCallback(async (): Promise<boolean> => {
    const started = Date.now()
    let observedOffline = false
    await delay(RESTART_POLL_MS)
    while (Date.now() - started < RESTART_TIMEOUT_MS) {
      try {
        await probeBootstrap()
        if (observedOffline || Date.now() - started > 4000) return true
      } catch {
        observedOffline = true
      }
      await delay(RESTART_POLL_MS)
    }
    return false
  }, [])

  const skip = async () => {
    const hasDevices = (summary?.deviceCount ?? 0) > 0
    setBusy('skip')
    setError(null)
    try {
      const result = await completeSetup(!hasDevices)
      if (hasDevices && result.restarting) {
        setStage('finishing')
        const recovered = await finishAfterRestart()
        if (!recovered) {
          setError('面板启动超时，请稍后手动刷新页面')
          setBusy(null)
          return
        }
      }
      onComplete()
    } catch (cause) {
      setError(friendlyError(cause, '进入面板失败'))
      setBusy(null)
    }
  }

  const changeScheme = (next: string) => {
    if (!portTouchedRef.current || port === String(defaultPort(scheme))) {
      setPort(String(defaultPort(next)))
      portTouchedRef.current = false
    }
    setScheme(next)
  }

  const portNumber = Number(port)

  const generate = async () => {
    if (!name.trim() || !host.trim() || !Number.isInteger(portNumber) || portNumber < 1 || portNumber > 65535) return
    setBusy('create')
    setError(null)
    setSessionExpired(false)
    setPreview(null)
    setScriptVisible(false)
    try {
      const created = await createOnboardingSession({ name: name.trim(), host: host.trim(), scheme, port: portNumber })
      setSession(created)
      setStage('script')
    } catch (cause) {
      setError(friendlyError(cause, '生成接入脚本失败'))
    } finally {
      setBusy(null)
    }
  }

  const verify = async () => {
    if (!session) return
    setBusy('preview')
    setError(null)
    setSessionExpired(false)
    try {
      const result = await previewOnboardingSession(session.sessionId)
      setPreview(result)
      setStage('verified')
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 410) setSessionExpired(true)
      setError(friendlyError(cause, 'RouterOS 连接检测失败'))
    } finally {
      setBusy(null)
    }
  }

  const complete = async () => {
    if (!session || !preview) return
    setBusy('complete')
    setError(null)
    try {
      await completeOnboardingSession(session.sessionId, preview.verificationToken)
      setStage('finishing')
      const recovered = await finishAfterRestart()
      if (recovered) {
        onComplete()
      } else {
        setError('设备已保存，但面板启动超时，请稍后手动刷新页面')
        setBusy(null)
      }
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 410) {
        setSessionExpired(true)
        setStage('script')
      }
      setError(friendlyError(cause, '完成接入失败'))
      setBusy(null)
    }
  }

  if (stage === 'finishing') {
    return (
      <main className="startup-shell">
        <section className="glass setup-card">
          <SetupBrand title="正在启动采集" description="设备已保存，面板正在启动并连接 RouterOS，恢复后将自动进入面板。" />
          <p className="setup-waiting">
            <StatusDot tone="ok" pulse />
            正在等待面板恢复…
          </p>
          {error ? (
            <>
              <p className="startup-error">{error}</p>
              <Button variant="ghost" onClick={onComplete}>
                重新检查状态
              </Button>
            </>
          ) : null}
        </section>
      </main>
    )
  }

  if (stage === 'choice') {
    const hasDevices = (summary?.deviceCount ?? 0) > 0
    return (
      <main className="startup-shell">
        <section className="glass setup-card setup-card-choice">
          <SetupBrand title="开始使用 rosboard" description="管理员已就绪。现在可以添加第一台 RouterOS 设备，也可以稍后再配置。" />
          {error ? <p className="startup-error">{error}</p> : null}
          <div className="setup-choice">
            <button type="button" className="setup-choice-card primary" onClick={() => setStage('form')}>
              <span className="choice-icon" aria-hidden="true">
                🌐
              </span>
              <span className="choice-text">
                <strong>添加 RouterOS 设备</strong>
                <small>生成接入脚本，在 RouterOS 终端执行后自动完成连接与采集</small>
              </span>
              <Badge tone="accent" className="choice-badge">
                推荐
              </Badge>
            </button>
            <button type="button" className="setup-choice-card" disabled={busy === 'skip'} onClick={() => void skip()}>
              <span className="choice-icon" aria-hidden="true">
                ⏭️
              </span>
              <span className="choice-text">
                <strong>{busy === 'skip' ? '正在进入…' : hasDevices ? '进入面板' : '暂不添加，直接进入面板'}</strong>
                <small>{hasDevices ? '完成初始化并重启采集服务' : '稍后可在「面板设置 · 设备管理」中添加 RouterOS'}</small>
              </span>
            </button>
          </div>
        </section>
      </main>
    )
  }

  return (
    <main className="startup-shell">
      <section className="glass setup-card setup-card-wide">
        <SetupBrand title="添加 RouterOS" description="三步完成接入：填写连接信息，在 RouterOS 终端执行脚本，确认后开始采集。" />
        <StepIndicator stage={stage} />
        {error ? <p className="startup-error">{error}</p> : null}

        {stage === 'form' ? (
          <form
            className="setup-form"
            onSubmit={(event) => {
              event.preventDefault()
              void generate()
            }}
          >
            <Field label="设备名称" hint="用于在面板中区分多台设备，例如：主路由">
              <Input value={name} onChange={setName} required maxLength={64} placeholder="主路由" autoFocus name="device-name" />
            </Field>
            <div className="setup-form-row">
              <Field label="协议">
                <Select
                  value={scheme}
                  onChange={changeScheme}
                  options={[
                    { value: 'http', label: 'HTTP' },
                    { value: 'https', label: 'HTTPS' },
                  ]}
                  ariaLabel="连接协议"
                />
              </Field>
              <Field label="RouterOS 地址" className="setup-form-grow">
                <Input value={host} onChange={setHost} required placeholder="192.168.88.1" name="device-host" />
              </Field>
              <Field label="端口">
                <Input
                  value={port}
                  onChange={(value) => {
                    portTouchedRef.current = true
                    setPort(value)
                  }}
                  type="number"
                  min={1}
                  max={65535}
                  required
                  name="device-port"
                />
              </Field>
            </div>
            <div className="setup-actions">
              <Button variant="ghost" onClick={() => setStage('choice')}>
                上一步
              </Button>
              <Button
                variant="primary"
                type="submit"
                loading={busy === 'create'}
                disabled={!name.trim() || !host.trim() || !Number.isInteger(portNumber) || portNumber < 1 || portNumber > 65535}
              >
                生成接入脚本
              </Button>
            </div>
          </form>
        ) : null}

        {stage === 'script' && session ? (
          <div className="setup-form">
            <ol className="setup-instructions">
              <li>
                <b>复制接入脚本</b>
                <span>脚本会创建一个仅供 rosboard 使用的 RouterOS 账号与权限组。</span>
              </li>
              <li>
                <b>在 RouterOS 中执行</b>
                <span>用 Winbox 或 WebFig 打开「New Terminal」，粘贴脚本并回车执行。</span>
              </li>
              <li>
                <b>回到这里检测</b>
                <span>点击「我已执行脚本」，面板将连接 {session.connection.host} 验证账号。</span>
              </li>
            </ol>
            <div className="setup-script-tools">
              <CopyButton text={session.script} label="复制接入脚本" />
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                aria-expanded={scriptVisible}
                onClick={() => setScriptVisible((visible) => !visible)}
              >
                {scriptVisible ? '隐藏脚本' : '查看脚本'}
              </button>
              <span className="faint setup-script-meta">
                账号 <code>{session.username}</code> · 有效期至 {formatClock(session.expiresAt)}
              </span>
            </div>
            {scriptVisible ? (
              <pre className="setup-script" tabIndex={0}>
                {session.script}
              </pre>
            ) : null}
            {sessionExpired ? (
              <p className="setup-expired">
                接入脚本已过期。
                <Button size="sm" variant="primary" onClick={() => void generate()} loading={busy === 'create'}>
                  重新生成脚本
                </Button>
              </p>
            ) : null}
            <div className="setup-actions">
              <Button variant="ghost" onClick={() => setStage('form')}>
                上一步
              </Button>
              <Button variant="primary" onClick={() => void verify()} disabled={busy !== null || sessionExpired}>
                {busy === 'preview' ? '正在连接 RouterOS 检测…' : '我已执行脚本'}
              </Button>
            </div>
          </div>
        ) : null}

        {stage === 'verified' && preview ? (
          <div className="setup-form">
            <div className="setup-verify">
              <div className="setup-verify-head">
                <StatusDot tone="ok" />
                <strong>{preview.identity.routerName || session?.connection.host}</strong>
                <Badge tone="ok" dot>
                  连接成功
                </Badge>
              </div>
              <div className="kv">
                <span>RouterOS 版本</span>
                <b>{preview.identity.version ? `v${preview.identity.version}` : '-'}</b>
              </div>
              <div className="kv">
                <span>型号 / 平台</span>
                <b>{[preview.identity.boardName, preview.identity.platform].filter(Boolean).join(' · ') || '-'}</b>
              </div>
              <div className="kv">
                <span>流量采集接口</span>
                <b>{preview.trafficInterfaces.length ? preview.trafficInterfaces.join('、') : '自动识别'}</b>
              </div>
              <div className="kv">
                <span>终端网段</span>
                <b>{preview.cidrCandidateCount > 0 ? `自动识别 ${preview.cidrCandidateCount} 个网段` : '自动识别'}</b>
              </div>
            </div>
            {preview.warnings.length > 0 ? (
              <ul className="setup-warn-list">
                {preview.warnings.map((warning, index) => (
                  <li key={`${warning.capability}-${index}`}>
                    <StatusDot tone="warn" />
                    {warning.message}
                  </li>
                ))}
              </ul>
            ) : null}
            <div className="setup-actions">
              <Button variant="ghost" onClick={() => setStage('script')}>
                上一步
              </Button>
              <Button variant="primary" onClick={() => void complete()} disabled={busy !== null}>
                {busy === 'complete' ? '正在保存并启动采集…' : '完成接入并进入面板'}
              </Button>
            </div>
          </div>
        ) : null}
      </section>
      <ToastHost />
    </main>
  )
}
