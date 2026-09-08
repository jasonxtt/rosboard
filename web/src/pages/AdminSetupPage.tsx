import { useState, type FormEvent } from 'react'
import { Button, Field, Input } from '../ui'
import { createAdminAccount, friendlyError } from '../features/monitoring/api'
import './setup.css'

/** 密码规则与后端 internal/auth ValidatePassword 对齐：4–128 个字符。 */
function runeLength(value: string): number {
  return [...value].length
}

export default function AdminSetupPage({ onComplete }: { onComplete: () => void }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const trimmedName = username.trim()
  const passwordLength = runeLength(password)
  const passwordValid = passwordLength >= 4 && passwordLength <= 128
  const matches = password === confirmation
  const canSubmit = trimmedName.length > 0 && trimmedName.length <= 64 && passwordValid && matches && !saving

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!canSubmit) return
    setSaving(true)
    setError(null)
    try {
      await createAdminAccount({ username: trimmedName, password, passwordConfirmation: confirmation })
      onComplete()
    } catch (cause) {
      setError(friendlyError(cause, '管理员创建失败'))
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
            <h1>创建管理员</h1>
            <p className="muted">第一步：设置用于持续登录 rosboard 的唯一管理员账号。</p>
          </div>
        </div>
        {error ? <p className="startup-error">{error}</p> : null}
        <form className="setup-form" onSubmit={(event) => void submit(event)}>
          <Field label="管理员用户名">
            <Input value={username} onChange={setUsername} required maxLength={64} autoFocus autoComplete="username" name="username" />
          </Field>
          <Field label="密码" hint="4–128 个字符">
            <Input
              value={password}
              onChange={setPassword}
              type="password"
              required
              minLength={4}
              maxLength={128}
              autoComplete="new-password"
              name="new-password"
            />
          </Field>
          <Field label="确认密码" hint={confirmation && !matches ? '两次输入的密码不一致' : undefined}>
            <Input
              value={confirmation}
              onChange={setConfirmation}
              type="password"
              required
              minLength={4}
              maxLength={128}
              autoComplete="new-password"
              name="confirm-password"
            />
          </Field>
          <Button variant="primary" type="submit" disabled={!canSubmit} loading={saving}>
            创建管理员并继续
          </Button>
        </form>
      </section>
    </main>
  )
}
