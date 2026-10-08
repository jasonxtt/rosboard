import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { ContainerPage } from '../src/features/containers/ContainerPage'
import {
  apiGet,
  apiPost,
  safeArray,
  safeObject,
  safeString,
} from '../src/lib/api'
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
    [error, setError] = useState(''),
    [devices, setDevices] = useState([
      { id: 'demo-router', name: '演示路由器' },
      { id: 'demo-edge', name: '演示边缘设备' },
    ])
  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])
  useEffect(() => {
    const controller = new AbortController()
    let active = true
    void apiGet(
      '/api/containers/_preview-devices',
      (value) =>
        safeArray<unknown>(value).map((value) => {
          const o = safeObject(value)
          return { id: safeString(o.id), name: safeString(o.name) }
        }),
      controller.signal,
    )
      .then((value) => {
        if (active) setDevices(value)
      })
      .catch((e) => {
        if (active) setError(String(e))
      })
    return () => {
      active = false
      controller.abort()
    }
  }, [])
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
          预览设备{' '}
          <select value={device} onChange={(e) => setDevice(e.target.value)}>
            {devices.map((d) => (
              <option value={d.id} key={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </label>
        <button
          onClick={() => setTheme((t) => (t === 'dark' ? 'light' : 'dark'))}
        >
          切换{theme === 'dark' ? '浅色' : '深色'}
        </button>
        <button
          disabled={device === 'test-router'}
          onClick={() => void scenario('_many')}
        >
          载入大量容器
        </button>
        <button
          disabled={device === 'test-router'}
          onClick={() => void scenario('_reset')}
        >
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
