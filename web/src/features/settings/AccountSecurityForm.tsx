import { useEffect, useState } from 'react'
import { Button, Field, Input, Skeleton } from '../../ui'
import { apiGet, errorMessage } from '../../lib/api'
import { parseBootstrap } from '../../lib/types'
import { logout, updateAccount } from './api'

/**
 * 账号安全: change admin username/password (PUT /api/account forces
 * re-login) and a separate logout section. Single-column, bounded width —
 * never requests the old password (component-guidelines).
 */
export function AccountSecurityForm() {
  const [initialLoading, setInitialLoading] = useState(true)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [saving, setSaving] = useState(false)
  const [loggingOut, setLoggingOut] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    apiGet('/api/bootstrap', parseBootstrap)
      .then((bootstrap) => {
        if (!cancelled) setUsername(bootstrap.username ?? '')
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setInitialLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const mismatch = confirmation.length > 0 && password !== confirmation

  const save = async () => {
    if (password !== confirmation) return
    setSaving(true)
    setError(null)
    setMessage(null)
    try {
      await updateAccount({ username: username.trim(), password, passwordConfirmation: confirmation })
      setMessage('已更新，请重新登录')
      // The session cookie was cleared server-side; reload into the bootstrap
      // gate, which will route to the login page.
      window.setTimeout(() => window.location.reload(), 1200)
    } catch (saveError) {
      setError(errorMessage(saveError, '账号保存失败'))
      setSaving(false)
    }
  }

  const signOut = async () => {
    setLoggingOut(true)
    try {
      await logout()
    } catch {
      // Even when the request fails, reloading re-runs the bootstrap gate.
    }
    window.location.reload()
  }

  if (initialLoading) {
    return <Skeleton lines={4} height={14} />
  }

  return (
    <div className="account-security">
      <form
        className="account-security-form"
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        <Field label="管理员用户名">
          <Input value={username} onChange={setUsername} required maxLength={64} autoComplete="username" />
        </Field>
        <Field label="新密码（至少 4 个字符）">
          <Input type="password" value={password} onChange={setPassword} required minLength={4} maxLength={128} autoComplete="new-password" />
        </Field>
        <Field label="再次输入新密码" hint={mismatch ? '两次输入的密码不一致' : undefined}>
          <Input type="password" value={confirmation} onChange={setConfirmation} required minLength={4} maxLength={128} autoComplete="new-password" />
        </Field>
        {message ? (
          <p className="form-message" role="status">
            {message}
          </p>
        ) : null}
        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="form-actions">
          <Button type="submit" variant="primary" disabled={saving || !username.trim() || password.length < 4 || password !== confirmation} loading={saving}>
            {saving ? '正在保存…' : '保存账号和密码'}
          </Button>
        </div>
      </form>

      <hr className="divider" />

      <div className="logout-row">
        <div>
          <strong>退出登录</strong>
          <p className="faint">仅退出当前浏览器，不会修改管理员账号和密码。</p>
        </div>
        <Button disabled={loggingOut} loading={loggingOut} onClick={() => void signOut()}>
          退出登录
        </Button>
      </div>
    </div>
  )
}
