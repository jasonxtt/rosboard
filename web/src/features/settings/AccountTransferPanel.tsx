import { useEffect, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { fetchAccountTransfer, type AccountTransferStatus } from './api'

export const transferPermissionHelp = '专用账号权限为 read、write、test、api、rest-api、ssh、ftp；ssh/ftp 用于 SFTP 镜像传输，无需启用 FTP 服务。脚本不会开启 SSH、修改防火墙、信任主机密钥或开启容器写入。'

/** Shared behavior only; each shell supplies its own button/input classes. */
export function AccountTransferPanel({ deviceId, buttonClass, inputClass, disabled = false }: { deviceId: string; buttonClass: string; inputClass: string; disabled?: boolean }) {
  const [result, setResult] = useState<AccountTransferStatus | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])

  const check = async () => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setBusy(true)
    setError('')
    setResult(null)
    try {
      const value = await fetchAccountTransfer(deviceId, controller.signal)
      if (!controller.signal.aborted) setResult(value)
    } catch (e) {
      if (!controller.signal.aborted) setError(errorMessage(e, '无法确认传输账号'))
    } finally {
      if (!controller.signal.aborted) setBusy(false)
    }
  }
  return <details>
    <summary>镜像传输权限与旧账号升级</summary>
    <p>{transferPermissionHelp}</p>
    <p>以下检查只读取设备。旧账号需管理员手动执行绑定现场身份的脚本，保留用户名和密码；共享或自定义组不支持升级。</p>
    <button type="button" className={buttonClass} disabled={disabled || busy} onClick={() => void check()}>{busy ? '正在只读检查…' : '检查权限 / 执行后重新验证'}</button>
    {error ? <p role="alert">{error}</p> : null}
    {result ? <>
      <p role="status">{result.state === 'old' ? '需升级旧权限' : '已声明完整传输权限'}。{result.warning}</p>
      <p>请在生成脚本的同一台 RouterOS 上以管理员身份执行。脚本执行前后均检查身份与精确权限；若报告未知结果或权限漂移，请停止并人工检查。RouterOS 不提供原子比较并更新。</p>
      <textarea className={inputClass} aria-label="传输权限升级脚本" readOnly value={result.script} rows={12} spellCheck={false} />
    </> : null}
  </details>
}
