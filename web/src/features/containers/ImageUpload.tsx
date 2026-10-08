import { useEffect, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { uploadImage } from './api'
import type { ImageArchive } from './types'

export function ImageUpload({
  deviceId,
  enabled,
  archiveFile,
  onUploaded,
  onBusy,
}: {
  deviceId: string
  enabled: boolean
  archiveFile: string
  onUploaded: (archive: ImageArchive) => void
  onBusy: (busy: boolean) => void
}) {
  const [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [archive, setArchive] = useState<ImageArchive | null>(null)
  const lifetime = useRef<{ active: boolean; controller?: AbortController }>({
    active: true,
  })
  const busyCallback = useRef(onBusy)
  busyCallback.current = onBusy
  useEffect(() => {
    const current = lifetime.current
    current.active = true
    return () => {
      current.active = false
      current.controller?.abort()
      busyCallback.current(false)
    }
  }, [])
  const upload = async (file: File) => {
    setError('')
    if (!file.name.toLowerCase().endsWith('.tar') || file.size > 2 ** 30) {
      setError('请选择不超过 1 GiB 的 Docker-save .tar 镜像归档')
      return
    }
    const controller = new AbortController()
    lifetime.current.controller = controller
    setBusy(true)
    onBusy(true)
    try {
      const result = await uploadImage(deviceId, file, controller.signal)
      if (lifetime.current.active) {
        setArchive(result)
        onUploaded(result)
      }
    } catch (e) {
      if (lifetime.current.active) setError(errorMessage(e))
    } finally {
      if (lifetime.current.active) {
        setBusy(false)
        onBusy(false)
      }
    }
  }
  return (
    <div className="ct-image-upload">
      <label className="ct-field">
        <span>本地镜像归档 *</span>
        <input
          type="file"
          accept=".tar"
          disabled={!enabled || busy}
          onChange={(e) => {
            const file = e.target.files?.[0]
            e.target.value = ''
            if (file) void upload(file)
          }}
        />
        <small>
          使用 docker save 或 podman save --format docker-archive 导出单个 Linux
          镜像。支持 .tar，最大 1 GiB；架构需与设备一致。
        </small>
      </label>
      {!enabled && (
        <p className="ct-notice">
          真实镜像上传尚未启用。可在模拟预览中验证上传、校验及创建流程。
        </p>
      )}
      {busy && <p role="status">正在上传到模拟服务并校验归档…</p>}
      {error && (
        <p role="alert" className="ct-error">
          {error}
        </p>
      )}
      {archive && (
        <div className="ct-notice">
          <strong>{archive.reference}</strong>
          <p>
            {archive.architecture} · {(archive.bytes / 2 ** 20).toFixed(1)} MiB
            · {archive.name}
          </p>
          <small>校验完成 · 仅模拟传输至 RouterOS，归档内容不保留</small>
        </div>
      )}
      {archiveFile && (
        <small>
          RouterOS 镜像路径：<code>{archiveFile}</code>
        </small>
      )}
    </div>
  )
}
