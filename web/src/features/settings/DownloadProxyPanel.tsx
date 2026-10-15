import { useEffect, useState } from 'react'
import { apiGet, apiPost, apiPut, errorMessage, safeBoolean, safeObject, safeString } from '../../lib/api'

type Status = { displayURL: string; revision: string; passwordSet: boolean }
function parse(value: unknown): Status { const o = safeObject(value); return { displayURL: safeString(o.displayURL), revision: safeString(o.revision), passwordSet: safeBoolean(o.passwordSet) } }
export function DownloadProxyPanel({ buttonClass, inputClass, disabled = false }: { buttonClass: string; inputClass: string; disabled?: boolean }) {
  const [saved, setSaved] = useState<Status | null>(null)
  const [value, setValue] = useState('')
  const [edited, setEdited] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  useEffect(() => { const c = new AbortController(); void apiGet('/api/settings/download-proxy', parse, c.signal).then(s => { setSaved(s); setValue(s.displayURL) }).catch(e => { if (!c.signal.aborted) setMessage(errorMessage(e, '无法读取代理设置')) }); return () => c.abort() }, [])
  const act = async (test: boolean) => {
    if (!saved || busy) return
    setBusy(true); setMessage('')
    const payload = !edited ? { useSaved: true, revision: saved.revision } : { url: value }
    try {
      if (test) {
        const result = await apiPost('/api/settings/download-proxy/test', payload, v => { const o = safeObject(v); return { updates: safeBoolean(o.updates), registry: safeBoolean(o.registry), authentication: safeBoolean(o.authentication) } })
        setMessage(`版本更新：${result.updates ? '连接通过' : '连接失败'}；镜像仓库：${result.registry ? '连接通过' : '连接失败'}；镜像认证：${result.authentication ? '连接通过' : '连接失败'}。测试未保存设置。`)
      } else {
        const s = await apiPut('/api/settings/download-proxy', payload, parse)
        setSaved(s); setValue(s.displayURL); setEdited(false); setMessage(s.displayURL ? '代理已保存，新下载任务使用此设置。' : '已保存直连设置。')
      }
    } catch (e) { setMessage(errorMessage(e, '代理操作失败')) } finally { setBusy(false) }
  }
  return <div className="download-proxy">
    <p>用于 rosboard 版本检查、更新包和容器镜像下载。支持 HTTP(S)、SOCKS5 和 URL 账号密码；留空后确认切回直连。</p>
    <div className="download-proxy-row"><div className="download-proxy-input">
      <input className={inputClass} aria-label="下载代理 URL" type="text" autoComplete="off" spellCheck={false} placeholder="socks5://proxy.example:7890" disabled={disabled || busy || !saved} value={value} onChange={e => { setValue(e.target.value); setEdited(true); setMessage('') }} />
      <button type="button" className={buttonClass} aria-label="测试当前代理 URL" title="测试当前输入，不保存" disabled={disabled || busy || !saved} onClick={() => void act(true)}>{busy ? '…' : '↗'}</button>
    </div><button type="button" className={buttonClass} disabled={disabled || busy || !saved} onClick={() => void act(false)}>确认</button></div>
    {saved?.passwordSet ? <small>已保存的凭据仅显示脱敏值；修改 URL 时请重新填写完整凭据。保留当前值可直接测试。</small> : null}
    {message ? <p role="status">{message}</p> : null}
  </div>
}
