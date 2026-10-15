import { useEffect, useState } from 'react'
import { apiGet, apiPut, errorMessage, safeBoolean, safeNumber, safeObject, safeString } from '../../lib/api'
type Status = { mode: string; username: string; passwordSet: boolean; port: number; hostKey: string; revision: string }
function parse(v: unknown): Status { const o = safeObject(v); return { mode: safeString(o.mode), username: safeString(o.username), passwordSet: safeBoolean(o.passwordSet), port: safeNumber(o.port) || 22, hostKey: safeString(o.hostKey), revision: safeString(o.revision) } }
export function ImageTransferPanel({ deviceId, buttonClass, inputClass }: { deviceId: string; buttonClass: string; inputClass: string }) {
  const [saved, setSaved] = useState<Status | null>(null)
  const [draft, setDraft] = useState<Status | null>(null)
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  useEffect(() => { const c = new AbortController(); void apiGet(`/api/devices/${encodeURIComponent(deviceId)}/image-transfer`, parse, c.signal).then(s => { setSaved(s); setDraft(s) }).catch(e => { if (!c.signal.aborted) setMessage(errorMessage(e, '读取失败')) }); return () => c.abort() }, [deviceId])
  const submit = async (testOnly: boolean) => {
    if (!draft || !saved || busy) return
    setBusy(true); setMessage('')
    try {
      const result = await apiPut(`/api/devices/${encodeURIComponent(deviceId)}/image-transfer`, { ...draft, password, useSavedPassword: draft.mode === 'dedicated' && saved.mode === 'dedicated' && saved.username === draft.username && !password, testOnly }, testOnly ? () => null : parse)
      if (result) { setSaved(result); setDraft(result); setPassword('') }
      setMessage(testOnly ? '认证与目录读取通过；未验证写入，未保存设置。' : '传输设置已保存；容器写入仍由服务器独立授权。')
    } catch (e) { setMessage(errorMessage(e, '传输设置失败')) } finally { setBusy(false) }
  }
  return <details><summary>镜像传输账号与 SSH 指纹</summary><p>复用连接账号或配置专用账号。请独立核对主机指纹；检查仅认证和读取目录，不上传测试文件。</p>{draft ? <div className="image-transfer-settings">
    <label>账号来源<select className={inputClass} value={draft.mode} disabled={busy} onChange={e => { setDraft({ ...draft, mode: e.target.value }); setPassword('') }}><option value="connection">复用已保存的连接账号</option><option value="dedicated">专用 SSH 账号</option></select></label>
    {draft.mode === 'dedicated' ? <><label>SSH 用户名<input className={inputClass} autoComplete="off" value={draft.username} disabled={busy} onChange={e => setDraft({ ...draft, username: e.target.value })} /></label><label>SSH 密码<input className={inputClass} type="password" autoComplete="new-password" placeholder={saved?.mode === 'dedicated' && saved.passwordSet ? '留空保留同一账号的密码' : ''} value={password} disabled={busy} onChange={e => setPassword(e.target.value)} /></label></> : null}
    <label>SSH 端口<input className={inputClass} type="number" min={1} max={65535} value={draft.port} disabled={busy} onChange={e => setDraft({ ...draft, port: Number(e.target.value) })} /></label>
    <label>已核验的 SSH 主机指纹<input className={inputClass} placeholder="SHA256:…" value={draft.hostKey} disabled={busy} onChange={e => setDraft({ ...draft, hostKey: e.target.value })} /></label>
    <div><button type="button" className={buttonClass} disabled={busy} onClick={() => void submit(true)}>只读检查</button> <button type="button" className={buttonClass} disabled={busy} onClick={() => void submit(false)}>验证并保存</button></div>
  </div> : null}{message ? <p role="status">{message}</p> : null}</details>
}
