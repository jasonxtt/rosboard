import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, errorMessage } from '../../lib/api'
import { fetchDirectories, mutateDirectory } from './api'
import { childDirectory, directoryPath, parentDirectory } from './directoryPaths'
import type {
  DirectoryListing,
  DirectoryMutation,
  DirectoryRequest,
} from './types'

/**
 * Lazy directory tree for the path combobox. Keeps one listing cache per
 * expanded node; expanding a node (re)fetches its children silently. All
 * mutations go through the same device-scoped API and uncertainty handling
 * as the previous single-path picker: unknown outcomes are only resolved by
 * an explicit recover request with the original request ID — never retried.
 */
export function useDirectoryTree(
  deviceId: string,
  initialPath: string,
  onMutation?: (result: DirectoryMutation) => void,
) {
  const [listings, setListings] = useState(() => new Map<string, DirectoryListing>())
  const [expanded, setExpanded] = useState(() => new Set<string>())
  const [loading, setLoading] = useState(() => new Set<string>())
  const [missing, setMissing] = useState(() => new Set<string>())
  const [error, setError] = useState('')
  const [notice, setNotice] = useState(() =>
    directoryPath(initialPath) ? '' : initialPath.trim() ? '输入路径无效，已打开根目录；原输入保留。' : '',
  )
  const [pending, setPending] = useState<DirectoryRequest | null>(null)
  const [busy, setBusy] = useState(false)
  const inflight = useRef(new Set<string>())
  const running = useRef(false)
  const active = useRef(true)
  useEffect(() => {
    active.current = true
    return () => {
      active.current = false
    }
  }, [])

  /** fetch one node's children into the cache; a 404 marks the node missing */
  const load = useCallback(
    (path: string, opts: { force?: boolean } = {}) => {
      if (inflight.current.has(path)) return
      if (!opts.force && listings.has(path)) return
      inflight.current.add(path)
      setLoading((s) => new Set(s).add(path))
      void fetchDirectories(deviceId, path)
        .then((value) => {
          if (!active.current) return
          setListings((m) => new Map(m).set(path, value))
          if (value.pending) setPending({ action: 'recover', requestId: value.pending.requestId })
        })
        .catch((e) => {
          if (!active.current) return
          if (e instanceof ApiError && e.code === 'directory_not_found') {
            setMissing((s) => (s.has(path) ? s : new Set(s).add(path)))
            setExpanded((s) => {
              if (!s.has(path)) return s
              const next = new Set(s)
              next.delete(path)
              return next
            })
          } else setError(errorMessage(e))
        })
        .finally(() => {
          inflight.current.delete(path)
          if (!active.current) return
          setLoading((s) => {
            const next = new Set(s)
            next.delete(path)
            return next
          })
        })
    },
    [deviceId, listings],
  )

  const expand = useCallback(
    (path: string, opts: { force?: boolean } = {}) => {
      // return the same Set when unchanged so React can bail out of rerenders
      setExpanded((s) => (s.has(path) ? s : new Set(s).add(path)))
      load(path, opts)
    },
    [load],
  )

  const toggle = useCallback(
    (path: string) => {
      const wasExpanded = expanded.has(path)
      setExpanded((s) => {
        const next = new Set(s)
        if (wasExpanded) next.delete(path)
        else next.add(path)
        return next
      })
      if (!wasExpanded) load(path)
    },
    [expanded, load],
  )

  /** load + expand the ancestor chain of a typed path so the tree reveals
   * it; invalid input still reveals the root so the tree is never empty */
  const reveal = useCallback(
    (value: string) => {
      const path = directoryPath(value)
      expand('/')
      if (!path) return
      let current = '/'
      for (const segment of path.slice(1).split('/')) {
        current = childDirectory(current, segment)
        expand(current)
      }
    },
    [expand],
  )

  /** drop cached listings for a subtree (after rename/delete) */
  const dropSubtree = useCallback((path: string) => {
    setListings((m) => {
      const next = new Map(m)
      for (const key of next.keys())
        if (key === path || key.startsWith(path + '/')) next.delete(key)
      return next
    })
    setExpanded((s) => {
      const next = new Set(s)
      for (const key of next)
        if (key === path || key.startsWith(path + '/')) next.delete(key)
      return next
    })
  }, [])

  const remapSubtree = useCallback((oldPath: string, newPath: string) => {
    setListings((m) => {
      const next = new Map(m)
      for (const [key, value] of next)
        if (key === oldPath || key.startsWith(oldPath + '/')) {
          next.delete(key)
          next.set(newPath + key.slice(oldPath.length), value)
        }
      return next
    })
    setExpanded((s) => {
      const next = new Set<string>()
      for (const key of s) next.add(newPath + key.slice(oldPath.length))
      return next
    })
  }, [])

  const mutate = useCallback(
    async (request: DirectoryRequest): Promise<boolean> => {
      // a pending outcome blocks new writes, but recover itself must pass
      if (running.current || (pending && request.action !== 'recover'))
        return false
      running.current = true
      setBusy(true)
      setError('')
      setNotice('')
      try {
        const result = await mutateDirectory(deviceId, request)
        if (!active.current) return false
        if (result.state !== 'succeeded') {
          setPending({ action: 'recover', requestId: result.requestId })
          setError('操作结果尚未确认，请刷新确认结果。系统不会重复执行写入。')
          return false
        }
        setPending(null)
        onMutation?.(result)
        if (request.action === 'mkdir' && request.parent)
          load(request.parent, { force: true })
        if (request.action === 'rename' && request.path) {
          remapSubtree(request.path, result.path)
          load(parentDirectory(request.path), { force: true })
        }
        if (request.action === 'delete' && request.path) {
          dropSubtree(request.path)
          load(parentDirectory(request.path), { force: true })
        }
        if (request.action === 'recover') {
          for (const path of [...listings.keys()]) load(path, { force: true })
        }
        setNotice(
          request.action === 'delete'
            ? '文件夹及其中全部内容已删除。'
            : request.action === 'recover'
              ? '已确认操作结果。'
              : '目录操作已完成。',
        )
        return true
      } catch (e) {
        if (!active.current) return false
        const uncertain =
          !(e instanceof ApiError) ||
          e.status === 0 ||
          e.code === 'directory_outcome_unknown' ||
          e.code === 'invalid_response' ||
          (e.status >= 500 &&
            ![
              'directory_write_failed',
              'directory_read_failed',
              'directory_references_failed',
            ].includes(e.code || ''))
        if (
          uncertain ||
          (e instanceof ApiError &&
            e.code === 'directory_busy' &&
            request.action === 'recover')
        ) {
          setPending({ action: 'recover', requestId: request.requestId })
          setError(`${errorMessage(e)}。请刷新确认结果，系统不会重复执行写入。`)
        } else {
          setPending(null)
          setError(errorMessage(e))
        }
        return false
      } finally {
        running.current = false
        if (active.current) setBusy(false)
      }
    },
    [pending, deviceId, onMutation, load, remapSubtree, dropSubtree, listings],
  )

  // initial mount: load the root and reveal the bound initial path
  useEffect(() => {
    reveal(initialPath)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return {
    listings,
    expanded,
    loading,
    missing,
    error,
    notice,
    pending,
    busy,
    setError,
    setNotice,
    load,
    expand,
    toggle,
    reveal,
    dropSubtree,
    mutate,
  }
}
