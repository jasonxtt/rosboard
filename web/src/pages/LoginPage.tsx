import { useEffect, useState, type FormEvent } from 'react'
import { ApiError } from '../lib/api'
import { Button, Field, Input } from '../ui'
import { friendlyError, login, retryAfterSeconds } from '../features/monitoring/api'
import './setup.css'

export default function LoginPage({ onComplete }: { onComplete: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [limitedUntil, setLimitedUntil] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())

  const remainingSeconds = limitedUntil === null ? 0 : Math.max(0, Math.ceil((limitedUntil - now) / 1000))

  useEffect(() => {
    if (limitedUntil === null) return
    const timer = window.setInterval(() => setNow(Date.now()), 500)
    return () => window.clearInterval(timer)
  }, [limitedUntil])

  useEffect(() => {
    if (limitedUntil !== null && remainingSeconds === 0) setLimitedUntil(null)
  }, [limitedUntil, remainingSeconds])

  const canSubmit = username.trim().length > 0 && password.length > 0 && !saving && remainingSeconds === 0

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!canSubmit) return
    setSaving(true)
    setError(null)
    try {
      await login(username, password)
      onComplete()
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 429) {
        setLimitedUntil(Date.now() + retryAfterSeconds(cause) * 1000)
        setError('登录尝试过于频繁，请按倒计时稍后再试')
      } else {
        setError(friendlyError(cause, '登录失败'))
      }
      setSaving(false)
    }
  }

  return (
    <main className="startup-shell">
      <section className="glass setup-card">
        <div className="setup-brand">
          <span className="logo" aria-hidden="true">
            R
          </span>
          <div>
            <h1>登录 rosboard</h1>
            <p className="muted">使用管理员账号登录后进入监控面板。</p>
          </div>
        </div>
        {error ? <p className="startup-error">{error}</p> : null}
        <form className="setup-form" onSubmit={(event) => void submit(event)}>
          <Field label="用户名">
            <Input value={username} onChange={setUsername} required autoFocus autoComplete="username" name="username" />
          </Field>
          <Field label="密码">
            <Input value={password} onChange={setPassword} type="password" required autoComplete="current-password" name="current-password" />
          </Field>
          <Button variant="primary" type="submit" disabled={!canSubmit} loading={saving}>
            {remainingSeconds > 0 ? `请等待 ${remainingSeconds} 秒后重试` : '登录'}
          </Button>
        </form>
      </section>
    </main>
  )
}
