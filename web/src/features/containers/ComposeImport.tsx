import { useEffect, useId, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { parseCompose } from './api'
import type { ComposeNotice, ComposeResult, ComposeService } from './compose'

function Notices({ items }: { items: ComposeNotice[] }) {
  return items.length ? <ul>{items.map((n, i) =>
    <li key={i}><code>{n.field}</code>：{n.message}</li>,
  )}</ul> : <p>无</p>
}

export function ComposeReport({ service, skipped }: {
  service: ComposeService
  skipped: ComposeNotice[]
}) {
  return <div className="ct-compose-report">
    <h4>可导入字段</h4>
    <p>{service.imported.join('、') || '无'}</p>
    <h4>已跳过字段</h4>
    <Notices items={[...skipped, ...service.skipped]} />
    <h4>需要补填的信息</h4>
    <Notices items={service.needsInput} />
    {service.fields.mounts.length > 0 && <>
      <h4>挂载来源参考</h4>
      <ul>{service.fields.mounts.map((m, i) => <li key={i}>
        {service.mountHints[i] || '匿名卷'} → {m.target}（{m.readOnly ? '只读' : '读写'}）；请选择 ROS 来源目录
      </li>)}</ul>
    </>}
  </div>
}

export function ComposeImport({ deviceId, onApply, onClose }: {
  deviceId: string
  onApply: (service: ComposeService, skipped: ComposeNotice[]) => void
  onClose: () => void
}) {
  const [source, setSource] = useState('')
  const [result, setResult] = useState<ComposeResult | null>(null)
  const [selected, setSelected] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const dialog = useRef<HTMLDialogElement>(null)
  const lifetime = useRef({ sequence: 0, pending: null as AbortController | null })
  const headingId = useId()
  useEffect(() => {
    const element = dialog.current
    const state = lifetime.current
    element?.showModal()
    return () => {
      state.sequence++
      state.pending?.abort()
      element?.close()
    }
  }, [deviceId])
  const service = result?.services.find((s) => s.name === selected)
  const parse = async () => {
    const state = lifetime.current
    state.pending?.abort()
    const controller = new AbortController()
    state.pending = controller
    const current = ++state.sequence
    setBusy(true)
    setError('')
    setResult(null)
    setSelected('')
    try {
      const next = await parseCompose(deviceId, source, controller.signal)
      if (current !== state.sequence || controller.signal.aborted) return
      setResult(next)
      setSelected(next.services.length === 1 ? next.services[0].name : '')
    } catch (e) {
      if (current === state.sequence && !controller.signal.aborted) setError(errorMessage(e))
    } finally {
      if (current === state.sequence) setBusy(false)
    }
  }
  return <dialog ref={dialog} className="ct-dialog ct-compose-dialog" aria-labelledby={headingId}
    onCancel={(e) => { e.preventDefault(); onClose() }}>
    <h3 id={headingId}>从 Compose 导入</h3>
    <p>粘贴整份 Compose，选择一个服务填入面板。解析不会创建容器。</p>
    <label className="ct-field">
      <span>Compose 内容</span>
      <textarea autoFocus rows={10} value={source} spellCheck={false}
        placeholder={'services:\n  msm:\n    image: msmbox/msm:1.3.0'}
        onChange={(e) => {
          lifetime.current.sequence++
          lifetime.current.pending?.abort()
          setSource(e.target.value)
          setResult(null)
          setSelected('')
          setBusy(false)
          setError('')
        }} />
    </label>
    {error && <p className="ct-error" role="alert">{error}</p>}
    {result && <>
      <label className="ct-field">
        <span>选择服务</span>
        <select value={selected} onChange={(e) => setSelected(e.target.value)}>
          <option value="">请选择一个服务</option>
          {result.services.map((s) => <option key={s.name} value={s.name}>{s.name}</option>)}
        </select>
      </label>
      {result.services.length > 1 && <p>仅导入所选服务；其他服务、依赖顺序和服务名解析不会创建。</p>}
      {service && <ComposeReport service={service} skipped={result.skipped} />}
    </>}
    <div className="ct-compose-actions">
      <button type="button" onClick={onClose}>取消</button>
      <button type="button" disabled={busy || !source.trim()} onClick={() => void parse()}>
        {busy ? '解析中…' : '解析'}
      </button>
      <button type="button" className="primary" disabled={!service || busy}
        onClick={() => { if (service && result) onApply(service, result.skipped) }}>填入面板</button>
    </div>
  </dialog>
}
