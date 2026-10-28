import { ContainerButton } from './ContainerUI'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MutableRefObject,
} from 'react'
import { newRequestId } from './drafts'
import { InlineDirectoryName } from './InlineDirectoryName'
import { childDirectory, directoryNameError, directoryPath, parentDirectory } from './directoryPaths'
import { useDirectoryTree } from './useDirectoryTree'
import type { DirectoryEntry, DirectoryMutation } from './types'

type InlineState =
  | { kind: 'create-child'; parent: string }
  | { kind: 'rename'; entry: DirectoryEntry }
  | { kind: 'delete'; entry: DirectoryEntry }
  | null

/**
 * Lightweight directory tree for the path combobox: no header, toolbar or
 * footer chrome — only folders, indented by depth. Clicking a row selects it
 * (live onChange) and toggles its expansion; typing a path in the bound input
 * reveals its ancestors and offers a create-and-select action row when the
 * leaf folder does not exist yet. New/rename/delete happen inline in rows.
 */
export function DirectoryPicker({
  deviceId,
  id,
  writable,
  purpose,
  initialPath = '',
  allowFiles = false,
  value = '',
  createRef,
  onSelect,
  onClose,
  onMutation,
}: {
  deviceId: string
  id?: string
  writable: boolean
  purpose: string
  initialPath?: string
  /** retained for the field contract; the tree only ever shows directories */
  allowFiles?: boolean
  /** current input text: reveals/highlights the matching node */
  value?: string
  /** set while a create-and-select action is available; the bound input's
   * Enter key triggers it */
  createRef?: MutableRefObject<(() => void) | null>
  onSelect: (path: string) => void
  onClose: () => void
  onMutation?: (result: DirectoryMutation) => void
}) {
  void allowFiles
  const tree = useDirectoryTree(deviceId, initialPath, onMutation)
  const [inline, setInline] = useState<InlineState>(null)
  const [menu, setMenu] = useState<{ entry: DirectoryEntry; top: number; left: number } | null>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const { pending, busy } = tree
  const blocked = busy || !!pending || !!inline

  // reveal the ancestor chain of the typed value as it changes
  const normalizedValue = directoryPath(value) || ''
  useEffect(() => {
    tree.reveal(value)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value])

  // walk the loaded tree along the typed path: the last existing node
  const lastExisting = useMemo(() => {
    if (!normalizedValue) return '/'
    let current = '/'
    for (const segment of normalizedValue.slice(1).split('/')) {
      const next = childDirectory(current, segment)
      if (tree.missing.has(next)) break
      if (!(tree.listings.get(current)?.entries.some((e) => e.directory && e.path === next) ?? false))
        break
      current = next
    }
    return current
  }, [normalizedValue, tree.listings, tree.missing])

  // create-and-select candidate: typed leaf missing under its loaded parent
  const createCandidate = useMemo(() => {
    if (!normalizedValue) return null
    const parent = parentDirectory(normalizedValue)
    const name = normalizedValue.slice(parent === '/' ? 1 : parent.length + 1)
    if (directoryNameError(name)) return null
    // the parent must be the deepest node we could actually walk to
    if (lastExisting !== parent) return null
    const parentListing = tree.listings.get(parent)
    if (!parentListing) return null
    if (parentListing.entries.some((e) => e.path === normalizedValue)) return null
    return { parent, name }
  }, [normalizedValue, lastExisting, tree.listings])

  const createAndSelect = useCallback(
    async (parent: string, name: string) => {
      const created = await tree.mutate({
        action: 'mkdir',
        requestId: newRequestId(),
        parent,
        expectedId: tree.listings.get(parent)?.id,
        name,
      })
      if (created) {
        const path = childDirectory(parent, name)
        tree.expand(path)
        onSelect(path)
        onClose()
      }
      return created
    },
    [tree, onSelect, onClose],
  )

  // expose the create action to the bound input's Enter key
  useEffect(() => {
    if (!createRef) return
    createRef.current = createCandidate
      ? () => void createAndSelect(createCandidate.parent, createCandidate.name)
      : null
    return () => {
      createRef.current = null
    }
  }, [createRef, createCandidate, createAndSelect])

  const closeMenu = useCallback(() => setMenu(null), [])
  useEffect(() => {
    if (!menu) return
    const onPointerDown = (event: PointerEvent) => {
      if (event.target instanceof window.Node && !menuRef.current?.contains(event.target))
        closeMenu()
    }
    document.addEventListener('pointerdown', onPointerDown)
    return () => document.removeEventListener('pointerdown', onPointerDown)
  }, [menu, closeMenu])

  const rowActivate = (path: string) => {
    if (blocked) return
    setMenu(null)
    setInline(null)
    if (path !== '/') onSelect(path)
    tree.toggle(path)
  }

  const startCreateChild = (parent: string) => {
    setMenu(null)
    setInline({ kind: 'create-child', parent })
    tree.expand(parent)
  }

  const defaultChildName = (parent: string) => {
    const names = new Set(
      (tree.listings.get(parent)?.entries || []).map((e) => e.name),
    )
    let name = '新建文件夹'
    for (let i = 2; names.has(name); i++) name = `新建文件夹 ${i}`
    return name
  }

  const saveCreateChild = async (parent: string, name: string) => {
    const created = await tree.mutate({
      action: 'mkdir',
      requestId: newRequestId(),
      parent,
      expectedId: tree.listings.get(parent)?.id,
      name,
    })
    if (created) {
      const path = childDirectory(parent, name)
      tree.expand(path)
      onSelect(path)
      setInline(null)
    }
    return created
  }

  const saveRename = async (entry: DirectoryEntry, name: string) => {
    if (name === entry.name) {
      setInline(null)
      return true
    }
    const renamed = await tree.mutate({
      action: 'rename',
      requestId: newRequestId(),
      path: entry.path,
      expectedId: entry.id,
      name,
    })
    if (renamed) setInline(null)
    return renamed
  }

  const confirmDelete = (entry: DirectoryEntry) => {
    void tree
      .mutate({
        action: 'delete',
        requestId: newRequestId(),
        path: entry.path,
        expectedId: entry.id,
        confirmPath: entry.path,
      })
      .then(() => setInline(null))
  }

  const openMenu = (entry: DirectoryEntry, button: HTMLButtonElement) => {
    if (blocked) return
    const rect = button.getBoundingClientRect()
    setMenu((previous) =>
      previous?.entry.path === entry.path
        ? null
        : {
            entry,
            top: Math.min(rect.bottom + 4, window.innerHeight - 150),
            left: Math.max(8, Math.min(rect.right - 148, window.innerWidth - 156)),
          },
    )
  }

  const nodes = (path: string, name: string, depth: number, entry?: DirectoryEntry) => {
    const isExpanded = tree.expanded.has(path)
    const listing = tree.listings.get(path)
    const selected = normalizedValue === path && path !== '/'
    const inlineHere =
      inline && ((inline.kind === 'rename' && inline.entry.path === path) ||
        (inline.kind === 'delete' && inline.entry.path === path))
        ? inline
        : null
    return (
      <li className="ct-tree-node" key={path}>
        <div
          className={`ct-tree-row${selected ? ' ct-tree-selected' : ''}`}
          style={{ paddingLeft: depth * 18 }}
          onClick={() => rowActivate(path)}
        >
          <span className="ct-tree-icon" aria-hidden="true">📁</span>
          {inlineHere?.kind === 'rename' ? (
            <InlineDirectoryName
              label="重命名文件夹"
              initial={inlineHere.entry.name}
              busy={busy || !!pending}
              onSave={(name) => saveRename(inlineHere.entry, name)}
              onCancel={() => setInline(null)}
            />
          ) : inlineHere?.kind === 'delete' ? (
            <div className="ct-tree-confirm" role="alertdialog" aria-label="删除文件夹确认">
              <strong>确定删除“{inlineHere.entry.name}”？</strong>
              <code>{inlineHere.entry.path}</code>
              <p>将删除此文件夹及其中全部文件和子目录，此操作无法撤销。</p>
              <div>
                <ContainerButton
                  type="button"
                  disabled={busy}
                  onClick={() => setInline(null)}
                >
                  取消
                </ContainerButton>
                <ContainerButton
                  type="button"
                  className="ct-danger"
                  disabled={busy || !!pending}
                  onClick={() => confirmDelete(inlineHere.entry)}
                >
                  删除全部内容
                </ContainerButton>
              </div>
            </div>
          ) : (
            <button
              type="button"
              className="ct-tree-name"
              aria-selected={selected}
              aria-expanded={isExpanded}
              aria-level={depth + 1}
              disabled={blocked}
              title={path}
              onClick={(event) => {
                event.stopPropagation()
                rowActivate(path)
              }}
            >
              {name}
            </button>
          )}
          {!inlineHere && writable && entry && entry.id && (
            <button
              type="button"
              className={`ct-tree-more${menu?.entry.path === entry.path ? ' ct-tree-more-open' : ''}`}
              aria-label={`文件夹 ${entry.name} 操作`}
              aria-expanded={menu?.entry.path === entry.path}
              disabled={blocked}
              title={entry.protected || '重命名或删除'}
              onClick={(event) => {
                event.stopPropagation()
                openMenu(entry, event.currentTarget)
              }}
            >
              {entry.protected ? '🔒' : '⋯'}
            </button>
          )}
        </div>
        {inline?.kind === 'create-child' && inline.parent === path && (
          <div className="ct-tree-row ct-tree-inline-row" style={{ paddingLeft: (depth + 1) * 18 }}>
            <InlineDirectoryName
              label="新文件夹名称"
              initial={defaultChildName(path)}
              busy={busy || !!pending}
              onSave={(name) => saveCreateChild(path, name)}
              onCancel={() => setInline(null)}
            />
          </div>
        )}
        {isExpanded && (
          <ul className="ct-tree-children" role="group">
            {createCandidate && createCandidate.parent === path && (
              <li className="ct-tree-node">
                <button
                  type="button"
                  className="ct-tree-row ct-tree-create"
                  style={{ paddingLeft: (depth + 1) * 18 }}
                  disabled={blocked}
                  onClick={(event) => {
                    event.stopPropagation()
                    void createAndSelect(createCandidate.parent, createCandidate.name)
                  }}
                >
                  <span aria-hidden="true">➕</span>
                  <span className="ct-tree-name">
                    创建并选择文件夹“{createCandidate.name}”（按 Enter 确认）
                  </span>
                </button>
              </li>
            )}
            {(listing?.entries || [])
              .filter((e) => e.directory)
              .sort((a, b) => a.name.localeCompare(b.name, 'zh-Hans-CN'))
              .map((child) => nodes(child.path, child.name, depth + 1, child))}
            {tree.loading.has(path) && !listing && (
              <li className="ct-tree-loading" style={{ paddingLeft: (depth + 1) * 18 + 24 }}>
                正在读取…
              </li>
            )}
            {listing && listing.entries.filter((e) => e.directory).length === 0 && !createCandidate && (
              <li className="ct-tree-loading" style={{ paddingLeft: (depth + 1) * 18 + 24 }}>
                空文件夹
              </li>
            )}
          </ul>
        )}
      </li>
    )
  }

  return (
    <div
      id={id}
      className="ct-directory-picker ct-tree"
      role="tree"
      aria-label={`选择${purpose}`}
      aria-busy={busy}
    >
      {tree.error && (
        <p role="alert" className="ct-tree-note ct-tree-error">{tree.error}</p>
      )}
      {tree.notice && (
        <p role="status" className="ct-tree-note">{tree.notice}</p>
      )}
      {pending && (
        <div className="ct-tree-pending" role="status">
          <span>操作结果尚未确认，系统不会重复执行写入。</span>
          <ContainerButton
            type="button"
            disabled={busy}
            onClick={() =>
              void tree.mutate({ action: 'recover', requestId: pending.requestId })
            }
          >
            刷新确认结果
          </ContainerButton>
        </div>
      )}
      <ul className="ct-tree-list">
        {nodes('/', '/', 0)}
        {tree.loading.has('/') && !tree.listings.has('/') && (
          <li className="ct-tree-loading" style={{ paddingLeft: 24 }}>正在读取…</li>
        )}
      </ul>
      {menu && (
        <div
          ref={menuRef}
          className="ct-tree-menu"
          role="menu"
          aria-label={`文件夹 ${menu.entry.name} 操作`}
          style={{ top: menu.top, left: menu.left }}
        >
          <button
            type="button"
            role="menuitem"
            disabled={busy || !!pending}
            onClick={() => startCreateChild(menu.entry.path)}
          >
            新建子文件夹
          </button>
          <button
            type="button"
            role="menuitem"
            disabled={!!menu.entry.protected}
            title={menu.entry.protected || undefined}
            onClick={() => {
              setInline({ kind: 'rename', entry: menu.entry })
              setMenu(null)
            }}
          >
            重命名
          </button>
          <button
            type="button"
            role="menuitem"
            className="ct-danger"
            disabled={!!menu.entry.protected}
            title={menu.entry.protected || undefined}
            onClick={() => {
              setInline({ kind: 'delete', entry: menu.entry })
              setMenu(null)
            }}
          >
            删除
          </button>
          {menu.entry.protected && (
            <p className="ct-tree-menu-note">{menu.entry.protected}</p>
          )}
        </div>
      )}
    </div>
  )
}
