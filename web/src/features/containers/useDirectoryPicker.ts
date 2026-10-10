import { useEffect, useRef, useState } from 'react'
import { ApiError, errorMessage } from '../../lib/api'
import { fetchDirectories, mutateDirectory } from './api'
import { directoryPath, parentDirectory } from './directoryPaths'
import type {
  DirectoryListing,
  DirectoryMutation,
  DirectoryRequest,
} from './types'

export function useDirectoryPicker(
  deviceId: string,
  initialPath: string,
  onMutation?: (result: DirectoryMutation) => void,
) {
  const [path, setPath] = useState(() => directoryPath(initialPath) || '/'),
    [listing, setListing] = useState<DirectoryListing | null>(null),
    [loading, setLoading] = useState(true),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [notice, setNotice] = useState(() =>
      directoryPath(initialPath)
        ? ''
        : '输入路径无效，已打开根目录；原输入保留。',
    ),
    [revision, setRevision] = useState(0),
    [pending, setPending] = useState<DirectoryRequest | null>(null)
  const first = useRef(true),
    active = useRef(true),
    running = useRef(false)
  useEffect(() => {
    active.current = true
    return () => {
      active.current = false
    }
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    let current = true
    setLoading(true)
    void fetchDirectories(deviceId, path, controller.signal)
      .then((value) => {
        if (!current) return
        setListing(value)
        if (value.pending)
          setPending({ action: 'recover', requestId: value.pending.requestId })
        first.current = false
      })
      .catch((e) => {
        if (!current) return
        if (first.current && path !== '/') {
          first.current = false
          setNotice('无法定位输入路径，已打开根目录；原输入保留。')
          setPath('/')
        } else if (
          e instanceof ApiError &&
          e.code === 'directory_not_found' &&
          path !== '/'
        ) {
          setNotice('当前目录已不存在，已返回上级目录。')
          setPath(parentDirectory(path))
        } else setError(errorMessage(e))
      })
      .finally(() => {
        if (current) setLoading(false)
      })
    return () => {
      current = false
      controller.abort()
    }
  }, [deviceId, path, revision])
  const navigate = (target: string) => {
    if (running.current) return
    setError('')
    setNotice('')
    setPath(target)
    if (target === path) setRevision((r) => r + 1)
  }
  const refresh = () => {
    setError('')
    setRevision((r) => r + 1)
  }
  const mutate = async (request: DirectoryRequest): Promise<boolean> => {
    if (running.current) return false
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
      if (result.action === 'rename')
        setPath((p) =>
          p === result.previousPath || p.startsWith(result.previousPath + '/')
            ? result.path + p.slice(result.previousPath.length)
            : p,
        )
      if (result.action === 'delete')
        setPath((p) =>
          p === result.path || p.startsWith(result.path + '/')
            ? parentDirectory(result.path)
            : p,
        )
      setNotice(
        result.action === 'delete'
          ? '文件夹及其中全部内容已删除。'
          : '目录操作已完成。',
      )
      setRevision((r) => r + 1)
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
      setRevision((r) => r + 1)
      return false
    } finally {
      running.current = false
      if (active.current) setBusy(false)
    }
  }
  return {
    path,
    listing: listing?.path === path ? listing : null,
    loading,
    busy,
    error,
    notice,
    pending,
    navigate,
    refresh,
    mutate,
    setError,
  }
}
