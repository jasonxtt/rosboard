import { useState } from 'react'
import { newRequestId } from './drafts'
import { InlineDirectoryName } from './InlineDirectoryName'
import { useDirectoryPicker } from './useDirectoryPicker'
import type { DirectoryEntry, DirectoryMutation } from './types'

export function DirectoryPicker({
  deviceId,
  id,
  writable,
  purpose,
  initialPath = '',
  allowFiles = false,
  onSelect,
  onClose,
  onMutation,
}: {
  deviceId: string
  id?: string
  writable: boolean
  purpose: string
  initialPath?: string
  allowFiles?: boolean
  onSelect: (path: string) => void
  onClose: () => void
  onMutation?: (result: DirectoryMutation) => void
}) {
  const state = useDirectoryPicker(deviceId, initialPath, onMutation)
  const [editing, setEditing] = useState<'new' | DirectoryEntry | null>(null),
    [menu, setMenu] = useState<string | null>(null),
    [deleting, setDeleting] = useState<DirectoryEntry | null>(null)
  const { path, listing, busy, loading, pending } = state
  const blocked = busy || loading || !!pending
  const parts = path.split('/').filter(Boolean)
  const names = new Set(listing?.entries.map((e) => e.name))
  let defaultName = '新建文件夹'
  for (let i = 2; names.has(defaultName); i++) defaultName = `新建文件夹 ${i}`
  const save = async (name: string) => {
    if (!editing || !listing) return false
    if (editing !== 'new' && name === editing.name) {
      setEditing(null)
      return true
    }
    const success = await state.mutate(
      editing === 'new'
        ? {
            action: 'mkdir',
            requestId: newRequestId(),
            parent: path,
            expectedId: listing.id,
            name,
          }
        : {
            action: 'rename',
            requestId: newRequestId(),
            path: editing.path,
            expectedId: editing.id,
            name,
          },
    )
    if (success || editing !== 'new') setEditing(null)
    return success
  }
  const nameEditor = (initial: string, label: string) => (
    <InlineDirectoryName
      initial={initial}
      label={label}
      busy={busy || !!pending}
      onSave={save}
      onCancel={() => setEditing(null)}
    />
  )
  return (
    <div
      id={id}
      className="ct-directory-picker"
      role="group"
      aria-label={`选择${purpose}`}
    >
      <header>
        <nav aria-label="目录路径">
          <button
            type="button"
            disabled={blocked}
            onClick={() => {
              setEditing(null)
              setMenu(null)
              state.navigate('/')
            }}
            aria-current={path === '/' ? 'location' : undefined}
          >
            /
          </button>
          {parts.map((part, i) => (
            <button
              type="button"
              key={i}
              disabled={blocked}
              aria-current={i === parts.length - 1 ? 'location' : undefined}
              onClick={() => {
                setEditing(null)
                setMenu(null)
                state.navigate('/' + parts.slice(0, i + 1).join('/'))
              }}
            >
              {part}
              <span aria-hidden="true"> /</span>
            </button>
          ))}
        </nav>
        <button
          type="button"
          className="ct-directory-close"
          aria-label="关闭目录浏览"
          disabled={busy}
          onClick={onClose}
        >
          ×
        </button>
      </header>
      {state.error && (
        <p role="alert" className="ct-error">
          {state.error}
        </p>
      )}
      {state.notice && (
        <p role="status" className="ct-directory-notice">
          {state.notice}
        </p>
      )}
      <div className="ct-directory-toolbar">
        {writable && (
          <button
            type="button"
            disabled={blocked || !listing?.canCreate || !!editing || !!deleting}
            onClick={() => {
              state.setError('')
              setMenu(null)
              setEditing('new')
            }}
          >
            ＋ 新建文件夹
          </button>
        )}
        <button
          type="button"
          disabled={busy || (!!editing && !pending)}
          onClick={() => {
            setMenu(null)
            if (pending) {
              setEditing(null)
              void state.mutate(pending)
            } else state.refresh()
          }}
        >
          {pending ? '刷新确认结果' : '刷新'}
        </button>
        {loading && <small role="status">正在读取目录…</small>}
        {busy && <small role="status">正在保存…</small>}
      </div>
      <ul aria-label="目录内容" aria-busy={loading || busy}>
        {editing === 'new' && (
          <li className="ct-directory-row">
            {nameEditor(defaultName, '新文件夹名称')}
          </li>
        )}
        {listing?.entries.map((entry) => (
          <li
            key={entry.path}
            className={`ct-directory-row${entry.directory ? '' : ' ct-directory-file'}`}
          >
            {editing !== 'new' && editing?.path === entry.path ? (
              nameEditor(entry.name, '重命名文件夹')
            ) : (
              <>
                {entry.directory ? (
                  <button
                    type="button"
                    className="ct-directory-folder"
                    disabled={blocked || !!editing || !!deleting}
                    onClick={() => {
                      setMenu(null)
                      state.navigate(entry.path)
                    }}
                  >
                    <span aria-hidden="true">📁</span>
                    <span>{entry.name}</span>
                    <span aria-hidden="true">›</span>
                  </button>
                ) : (
                  <span className="ct-file-name">
                    <span aria-hidden="true">▤</span>
                    <span>{entry.name}</span>
                    <small>{(entry.bytes / 1024).toFixed(1)} KiB</small>
                  </span>
                )}
                {entry.directory && writable && (
                  <div className="ct-directory-actions">
                    <button
                      type="button"
                      aria-label={`文件夹 ${entry.name} 操作`}
                      aria-expanded={menu === entry.path}
                      disabled={
                        blocked ||
                        !!editing ||
                        !!deleting ||
                        !!entry.protected ||
                        !entry.id
                      }
                      title={entry.protected || '重命名或删除'}
                      onClick={() =>
                        setMenu((p) => (p === entry.path ? null : entry.path))
                      }
                    >
                      ⋯
                    </button>
                    {menu === entry.path && (
                      <div
                        className="ct-directory-menu"
                        role="group"
                        aria-label={`${entry.name} 操作`}
                      >
                        <button
                          type="button"
                          onClick={() => {
                            setMenu(null)
                            setEditing(entry)
                          }}
                        >
                          重命名
                        </button>
                        <button
                          type="button"
                          className="ct-danger"
                          onClick={() => {
                            setMenu(null)
                            setDeleting(entry)
                          }}
                        >
                          删除
                        </button>
                      </div>
                    )}
                  </div>
                )}
                {!entry.directory && allowFiles && (
                  <button
                    type="button"
                    disabled={blocked || !!editing || !!deleting}
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
      {listing?.entries.length === 0 && !editing && (
        <p className="ct-empty-inline">空文件夹</p>
      )}
      {deleting && (
        <div
          className="ct-directory-confirm"
          role="alertdialog"
          aria-label="删除文件夹确认"
        >
          <strong>删除文件夹“{deleting.name}”？</strong>
          <code>{deleting.path}</code>
          <p>将删除此文件夹及其中全部文件和子目录，此操作无法撤销。</p>
          <div>
            <button
              type="button"
              disabled={busy}
              onClick={() => setDeleting(null)}
            >
              取消
            </button>
            <button
              type="button"
              className="ct-danger"
              disabled={blocked}
              onClick={() => {
                void state
                  .mutate({
                    action: 'delete',
                    requestId: newRequestId(),
                    path: deleting.path,
                    expectedId: deleting.id,
                    confirmPath: deleting.path,
                  })
                  .then(() => setDeleting(null))
              }}
            >
              删除全部内容
            </button>
          </div>
        </div>
      )}
      <footer>
        <div>
          <small>当前目录</small>
          <code>{path}</code>
        </div>
        <button
          type="button"
          className="ct-primary"
          disabled={
            blocked || !listing || path === '/' || !!editing || !!deleting
          }
          onClick={() => onSelect(path)}
        >
          选择此目录
        </button>
      </footer>
    </div>
  )
}
