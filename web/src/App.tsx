import { useCallback, useEffect, useState } from 'react'
import { apiGet, AUTH_REQUIRED_EVENT, errorMessage } from './lib/api'
import { parseBootstrap, type BootstrapResponse } from './lib/types'
import { ShellApp } from './shell/ShellApp'
import { Button } from './ui/Button'
import AdminSetupPage from './pages/AdminSetupPage'
import LoginPage from './pages/LoginPage'
import RouterOSSetupPage from './pages/RouterOSSetupPage'

const BOOTSTRAP_POLL_MS = 60_000

function StartupLoading({ error, onRetry }: { error: string | null; onRetry: () => void }) {
  return (
    <main className="startup-shell">
      <section className="glass startup-card">
        <div className="setup-brand">
          <span className="logo" aria-hidden="true">
            R
          </span>
          <div>
            <h1>rosboard</h1>
            <p className="muted">正在读取初始化状态…</p>
          </div>
        </div>
        {error ? (
          <>
            <p className="startup-error">{error}</p>
            <Button variant="ghost" onClick={onRetry}>
              重试
            </Button>
          </>
        ) : null}
      </section>
    </main>
  )
}

/**
 * Bootstrap gate (component-guidelines): /api/bootstrap decides the root
 * phase — never infer setup completion from dashboard availability or
 * browser storage. Re-polls every 60s and on authentication-required.
 */
export default function App() {
  const [bootstrap, setBootstrap] = useState<BootstrapResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const result = await apiGet('/api/bootstrap', parseBootstrap)
      setBootstrap(result)
      setError(null)
    } catch (loadError) {
      setError(errorMessage(loadError, '初始化状态读取失败'))
    }
  }, [])

  useEffect(() => {
    void refresh()
    const onAuthenticationRequired = () => void refresh()
    window.addEventListener(AUTH_REQUIRED_EVENT, onAuthenticationRequired)
    const timer = window.setInterval(() => void refresh(), BOOTSTRAP_POLL_MS)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener(AUTH_REQUIRED_EVENT, onAuthenticationRequired)
    }
  }, [refresh])

  if (!bootstrap) return <StartupLoading error={error} onRetry={() => void refresh()} />
  switch (bootstrap.phase) {
    case 'needs_admin':
      return <AdminSetupPage onComplete={() => void refresh()} />
    case 'needs_login':
      return <LoginPage onComplete={() => void refresh()} />
    case 'needs_routeros':
      return <RouterOSSetupPage onComplete={() => void refresh()} />
    case 'ready':
      return <ShellApp />
  }
}
