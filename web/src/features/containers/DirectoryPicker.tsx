import { useEffect, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { createDirectory, fetchDirectories } from './api'
import type { DirectoryListing } from './types'

export function DirectoryPicker({
  deviceId,
  writable,
  purpose,
  allowFiles = false,
  onSelect,
  onClose,
}: {
  deviceId: string
  writable: boolean
  purpose: string
  allowFiles?: boolean
  onSelect: (path: string) => void
  onClose: () => void
}) {
  const [path, setPath] = useState('/'),
    [listing, setListing] = useState<DirectoryListing | null>(null),
    [error, setError] = useState(''),
    [name, setName] = useState(''),
    [busy, setBusy] = useState(false),
    [loading, setLoading] = useState(true)
  const lifetime = useRef<{ active: boolean; mutation?: AbortController }>({
    active: true,
  })
  useEffect(() => {
    const current = lifetime.current
    current.active = true
    return () => {
      current.active = false
      current.mutation?.abort()
    }
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setLoading(true)
    setError('')
    setListing(null)
    void fetchDirectories(deviceId, path, controller.signal)
      .then((value) => {
        if (active) setListing(value)
      })
      .catch((e) => {
        if (active) setError(errorMessage(e))
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
      controller.abort()
    }
  }, [deviceId, path])
  const mkdir = async () => {
    const controller = new AbortController()
    lifetime.current.mutation = controller
    setBusy(true)
    setError('')
    try {
      const created = await createDirectory(
        deviceId,
        path,
        name,
        controller.signal,
      )
      if (lifetime.current.active) {
        setName('')
        setPath(created)
      }
    } catch (e) {
      if (lifetime.current.active) setError(errorMessage(e))
    } finally {
      if (lifetime.current.active) setBusy(false)
    }
  }
  const parts = path.split('/').filter(Boolean)
  return (
    <div
      className="ct-directory-picker"
      role="group"
      aria-label={`选择${purpose}`}
    >
      <header>
        <strong>选择{purpose}</strong>
        <button type="button" onClick={onClose}>
          关闭目录浏览
        </button>
      </header>
      <nav aria-label="目录路径">
        <button type="button" disabled={busy} onClick={() => setPath('/')}>
          Files
        </button>
        {parts.map((part, i) => (
          <button
            type="button"
            key={i}
            disabled={busy}
            onClick={() => setPath('/' + parts.slice(0, i + 1).join('/'))}
          >
            {' '}
            / {part}
          </button>
        ))}
      </nav>
      <p className="ct-muted">
        {writable
          ? '模拟 Files · 新建目录后可直接选用'
          : 'RouterOS Files · 当前只读，可浏览并选择现有目录'}
      </p>
      {error && (
        <p role="alert" className="ct-error">
          {error}
        </p>
      )}
      {loading && <p role="status">正在读取目录…</p>}
      {listing && (
        <ul aria-label="目录内容">
          {listing.entries.map((entry) => (
            <li key={entry.path}>
              {entry.directory ? (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => setPath(entry.path)}
                >
                  <span aria-hidden="true">▰</span> {entry.name}
                  <span aria-hidden="true"> ›</span>
                </button>
              ) : (
                <>
                  <span className="ct-file-name">
                    {entry.name}
                    <small>{(entry.bytes / 1024).toFixed(1)} KiB</small>
                  </span>
                  {allowFiles && (
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => onSelect(entry.path)}
                    >
                      选用文件
                    </button>
                  )}
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {listing && listing.entries.length === 0 && (
        <p className="ct-empty-inline">空文件夹</p>
      )}
      {writable && (
        <div className="ct-directory-create">
          <label className="ct-field">
            <span>新文件夹名称</span>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={busy}
              placeholder="例如 rootfs 或 config"
            />
          </label>
          <button
            type="button"
            disabled={busy || loading || path === '/' || !name.trim()}
            onClick={() => void mkdir()}
          >
            {busy ? '正在创建…' : '新建文件夹'}
          </button>
        </div>
      )}
      <footer>
        <code>{path}</code>
        <button
          type="button"
          className="ct-primary"
          disabled={busy || loading || !listing || path === '/'}
          onClick={() => onSelect(path)}
        >
          选用此目录
        </button>
      </footer>
    </div>
  )
}
