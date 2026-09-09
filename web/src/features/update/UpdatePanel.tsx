import { useEffect, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { activeUpdate, checkUpdate, fetchUpdate, installUpdate, type UpdateStatus } from './api'

const stages: Record<string, string> = { recovery_required: '更新状态异常，请检查服务日志', downloading: '正在下载', verifying: '正在校验', pending: '正在准备重启', backing_up: '正在备份', installing: '正在安装', verifying_startup: '正在验证启动', succeeded: '更新成功', failed: '更新失败', rolled_back: '已恢复原版本' }
const date = (value: string) => value && !Number.isNaN(Date.parse(value)) ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '尚未检查'
const version = (value: string) => /^\d+\.\d+\.\d+$/.test(value) ? `v${value}` : '开发构建'

/** Shared markup/behavior, styled independently by each UI's own stylesheet. */
export function UpdatePanel({ buttonClass, primaryClass, disabled = false }: { buttonClass: string; primaryClass: string; disabled?: boolean }) {
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [error, setError] = useState('')
  const [action, setAction] = useState('')
  const [cooldown, setCooldown] = useState(0)
  const [confirmVersion, setConfirmVersion] = useState('')
  const dialog = useRef<HTMLDialogElement>(null)
  const mounted = useRef(true)
  const watched = useRef('')
  const inFlight = useRef(false)
  const requestEpoch = useRef(0)
  const active = activeUpdate(status?.job)

  useEffect(() => {
    mounted.current = true
    let timer: ReturnType<typeof setTimeout>
    let cancelled = false
    const controller = new AbortController()
    const poll = async () => {
      if (!document.hidden && !inFlight.current) {
        try {
          const epoch = requestEpoch.current
          const result = await fetchUpdate(controller.signal)
          if (cancelled) return
          if (epoch === requestEpoch.current) {
            if (activeUpdate(result.job)) watched.current = result.job!.id
            if (result.job?.id === watched.current && result.job.stage === 'succeeded') {
              window.location.reload()
              return
            }
            setStatus(result)
            setError('')
          }
        } catch (e) {
          if (!cancelled) setError(watched.current ? '正在等待服务恢复；更新结果将在连接恢复后显示。若长时间未恢复，请检查服务日志。' : errorMessage(e))
        }
      }
      if (!cancelled) timer = setTimeout(() => void poll(), 3000)
    }
    void poll()
    return () => { cancelled = true; mounted.current = false; clearTimeout(timer); controller.abort() }
  }, [])
  useEffect(() => {
    if (!cooldown) return
    const timer = setTimeout(() => setCooldown(Math.max(0, cooldown - 1)), 1000)
    return () => clearTimeout(timer)
  }, [cooldown])
  useEffect(() => { if (confirmVersion) dialog.current?.showModal(); else dialog.current?.close() }, [confirmVersion])

  const run = async (install: boolean) => {
    if (inFlight.current) return
    inFlight.current = true
    requestEpoch.current++
    setAction(install ? 'install' : 'check'); setError('')
    if (!install) setCooldown(30)
    try {
      const result = install ? await installUpdate(confirmVersion) : await checkUpdate()
      if (!mounted.current) return
      if (activeUpdate(result.job)) watched.current = result.job!.id
      setStatus(result)
      if (install) setConfirmVersion('')
    } catch (e) { if (mounted.current) {setError(errorMessage(e)); if (install) setConfirmVersion('')} }
    finally { inFlight.current = false; if (mounted.current) setAction('') }
  }
  const job = status?.job
  const locked = disabled || !!action || active
  return <div className="version-update">
    <dl className="version-update-fields">
      <div><dt>当前版本</dt><dd>{status ? version(status.current.version) : '读取中…'}</dd></div>
      <div><dt>最新版本</dt><dd>{status?.latest ? version(status.latest.version) : '尚未检查'}{status?.canInstall ? <span className="version-update-badge">有新版本</span> : null}</dd></div>
      <div><dt>运行平台</dt><dd>{status ? `${status.current.os} · ${status.current.arch}` : '—'}</dd></div>
      <div><dt>最近检查</dt><dd>{date(status?.checkedAt ?? '')}</dd></div>
    </dl>
    {status?.latest ? <details className="version-update-notes"><summary>更新说明 · {date(status.latest.publishedAt)}</summary><p>{status.latest.notes || '此版本未提供更新说明。'}</p><a href={status.latest.url} target="_blank" rel="noopener noreferrer">查看 GitHub 发布说明 ↗</a></details> : null}
    {status?.checkError || error ? <p className="version-update-error" role="alert">{error || status?.checkError}</p> : null}
    {!status?.canInstall && status?.reason && !active && !status?.checkError ? <p className="version-update-hint">{status.reason}</p> : null}
    <div className="version-update-actions">
      <button type="button" className={buttonClass} disabled={locked || cooldown > 0} onClick={() => void run(false)}>{action === 'check' ? '检查中…' : cooldown > 0 ? `检查更新（${cooldown}s）` : '检查更新'}</button>
      <button type="button" className={primaryClass} disabled={locked || !status?.canInstall} onClick={() => setConfirmVersion(status?.latest?.version ?? '')}>{active || action === 'install' ? '更新中…' : '立即更新'}</button>
    </div>
    {job ? <div className="version-update-result" role="status">
      {active ? <><strong>{stages[job.stage] || '正在更新'}</strong>{job.stage === 'downloading' && job.total > 0 ? <progress aria-label="下载进度" max={job.total} value={Math.min(job.downloaded, job.total)} /> : null}<p>更新由服务器继续执行，关闭页面不会取消。</p></> : <>最近更新：{version(job.from)} → {version(job.to)} · {stages[job.stage] || '状态未知'} · {date(job.finishedAt || job.startedAt)}{job.stage !== 'succeeded' ? <p>{job.message}</p> : null}</>}
    </div> : null}
    <dialog ref={dialog} className="version-update-dialog" onCancel={(event) => { if (action) event.preventDefault(); else setConfirmVersion('') }} onClose={() => { if (!action) setConfirmVersion('') }} aria-labelledby="version-update-confirm-title">
      <h3 id="version-update-confirm-title">更新至 {version(confirmVersion)}</h3>
      <p>下载和校验完成后，面板将备份数据并重启。期间面板、采集及依赖面板的动态功能会短暂中断；更新失败将尝试恢复原版本。</p>
      <div className="version-update-actions"><button type="button" className={buttonClass} disabled={!!action} onClick={() => setConfirmVersion('')}>取消</button><button type="button" className={primaryClass} disabled={!!action || active || disabled} onClick={() => void run(true)}>{action ? '正在提交…' : '确认更新'}</button></div>
    </dialog>
  </div>
}
