import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { ContainerPage } from '../src/features/containers/ContainerPage'
import { apiPost } from '../src/lib/api'
import './preview.css'
const ui =
  new URLSearchParams(location.search).get('ui') === 'compact'
    ? 'compact'
    : 'aurora'
async function start() {
  if (ui === 'compact') {
    await import('../src/compact/features/containers/containers.css')
  } else {
    await import('../src/pages/ContainersPage.css')
  }
  createRoot(document.getElementById('root')!).render(<Preview />)
}
export function Preview() {
  const [device, setDevice] = useState('demo-router'),
    [theme, setTheme] = useState('dark'),
    [nonce, setNonce] = useState(0),
    [error, setError] = useState('')
  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])
  const scenario = async (path: string) => {
    try {
      await apiPost(`/api/containers/${path}?device=${device}`, {})
      setNonce((n) => n + 1)
      setError('')
    } catch (e) {
      setError(String(e))
    }
  }
  return (
    <main className="container-preview">
      <nav>
        <a
          href={`/container-preview.html?ui=${ui === 'compact' ? 'aurora' : 'compact'}`}
        >
          切换至 {ui === 'compact' ? 'Aurora' : 'Compact'}
        </a>
        <label>
          模拟设备{' '}
          <select value={device} onChange={(e) => setDevice(e.target.value)}>
            <option value="demo-router">演示路由器</option>
            <option value="demo-edge">演示边缘设备</option>
          </select>
        </label>
        <button
          onClick={() => setTheme((t) => (t === 'dark' ? 'light' : 'dark'))}
        >
          切换{theme === 'dark' ? '浅色' : '深色'}
        </button>
        <button onClick={() => void scenario('_many')}>载入大量容器</button>
        <button onClick={() => void scenario('_reset')}>
          重置当前模拟设备
        </button>
      </nav>
      {error && <p role="alert">{error}</p>}
      <ContainerPage
        key={`${device}:${nonce}`}
        deviceId={device}
        refreshNonce={nonce}
      />
    </main>
  )
}
void start()
