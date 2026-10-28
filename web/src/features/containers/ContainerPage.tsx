import {
  ContainerButton,
  ContainerDialog,
  ContainerStatus,
  ContainerSurface,
  ContainerTable,
} from './ContainerUI'
import { useEffect, useMemo, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { formatBytes } from '../../lib/format'
import {
  fetchContainers,
  fetchJob,
  fetchLogs,
  performAction,
  recoverJob,
  cancelJob,
} from './api'
import {
  editDraft,
  newDraft,
  statusLabels,
  memoryBytes,
  newRequestId,
} from './drafts'
import { ContainerEditor } from './ContainerEditor'
import type { Action, ContainerLog, Draft, Item, Job, Snapshot } from './types'

const actionLabels: Record<Action, string> = {
  create: '创建容器',
  edit: '编辑配置',
  start: '启动容器',
  stop: '停止容器',
  restart: '重启容器',
  update: '更新镜像',
  delete: '删除容器',
  adopt: '接管容器',
}

function memoryLabel(value: string, unavailable = '—'): string {
  if (!value.trim()) return unavailable
  return /^\d+(?:\.\d+)?$/.test(value.trim())
    ? formatBytes(Number(value))
    : value
}

/** clickable pill that hosts the simulation scenario picker */
function ModePill({
  simulation,
  busy,
  scenario,
  onScenario,
  label,
}: {
  simulation: boolean
  busy: boolean
  scenario: string
  onScenario: (value: string) => void
  label: string
}) {
  const ref = useRef<HTMLDetailsElement>(null)
  useEffect(() => {
    if (!simulation) return
    const dismiss = (event: PointerEvent) => {
      if (event.target instanceof window.Node && !ref.current?.contains(event.target))
        ref.current?.removeAttribute('open')
    }
    document.addEventListener('pointerdown', dismiss)
    return () => document.removeEventListener('pointerdown', dismiss)
  }, [simulation])
  if (!simulation)
    return <span className="ct-mode-pill" aria-label="容器管理模式">{label}</span>
  return (
    <details ref={ref} className="ct-mode-pop">
      <summary className="ct-mode-pill" aria-label="容器管理模式（模拟，点击调整情景）">
        {label} ⌄
      </summary>
      <div className="ct-mode-pop-body">
        <label>
          下一次模拟操作
          <select
            aria-label="模拟情景"
            value={scenario}
            disabled={busy}
            onChange={(e) => onScenario(e.target.value)}
          >
            <option value="success">正常完成</option>
            <option value="failure">下载 / 操作失败</option>
            <option value="unknown">超时结果不明 → 回读恢复</option>
          </select>
        </label>
        <small>这里的数据与所有操作均为模拟。结果不明时不会重复创建，请使用任务中的回读确认。</small>
      </div>
    </details>
  )
}

export function ContainerPage({
  deviceId,
  refreshNonce = 0,
  refreshMs = 0,
}: {
  deviceId: string
  refreshNonce?: number
  refreshMs?: number
}) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null),
    [error, setError] = useState(''),
    [loadError, setLoadError] = useState(''),
    [loading, setLoading] = useState(true),
    [nonce, setNonce] = useState(0)
  const [query, setQuery] = useState(''),
    [status, setStatus] = useState('all'),
    [sort, setSort] = useState('name'),
    [descending, setDescending] = useState(false),
    [page, setPage] = useState(0)
  const [editor, setEditor] = useState<{
    draft: Draft
    item: Item | null
  } | null>(null)
  const [job, setJob] = useState<Job | null>(null),
    [sending, setSending] = useState(false),
    [message, setMessage] = useState('')
  const [confirm, setConfirm] = useState<{ action: Action; item: Item } | null>(
    null,
  )
  const [logItem, setLogItem] = useState<Item | null>(null),
    [logs, setLogs] = useState<ContainerLog[] | null>(null),
    [logError, setLogError] = useState('')
  const [scenario, setScenario] = useState('success')
  const active = useRef(true)
  useEffect(() => {
    active.current = true
    return () => {
      active.current = false
    }
  }, [])
  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const checkVisible = () => {
      if (document.visibilityState === 'visible') void load()
      else timer = setTimeout(checkVisible, Math.max(refreshMs, 5000))
    }
    const load = async () => {
      try {
        const s = await fetchContainers(deviceId, controller.signal)
        if (!cancelled) {
          setSnapshot(s)
          if (s.activeJob?.deviceId === deviceId) setJob(s.activeJob)
          setLoadError('')
        }
      } catch (e) {
        if (!cancelled) setLoadError(errorMessage(e))
      } finally {
        if (!cancelled) {
          setLoading(false)
          if (refreshMs > 0)
            timer = setTimeout(
              () => {
                if (document.visibilityState === 'visible') void load()
                else timer = setTimeout(checkVisible, Math.max(refreshMs, 5000))
              },
              Math.max(refreshMs, 5000),
            )
        }
      }
    }
    if (deviceId) void load()
    else setLoading(false)
    return () => {
      cancelled = true
      controller.abort()
      clearTimeout(timer)
    }
  }, [deviceId, refreshNonce, refreshMs, nonce])
  useEffect(() => {
    setPage(0)
  }, [query, status, sort, descending])
  const busy =
    sending || (!!job && ['queued', 'running', 'unknown'].includes(job.state))
  useEffect(() => {
    if (!job?.id || !['queued', 'running'].includes(job.state)) return
    let cancelled = false
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const next = await fetchJob(deviceId, job.id, controller.signal)
        if (cancelled) return
        setJob(next)
        if (next.state === 'succeeded') {
          setNonce((n) => n + 1)
          setMessage(snapshot?.capabilities.mode === 'simulation' ? '模拟操作完成' : '操作完成，已回读确认')
          setEditor(null)
          if (next.action === 'update' || next.action === 'delete') setLogItem(null)
        } else if (next.state === 'failed') setError(next.error || '操作失败')
        else if (next.state !== 'unknown')
          timer = setTimeout(() => void poll(), 650)
      } catch (e) {
        if (!cancelled) {
          setError(errorMessage(e))
          timer = setTimeout(() => void poll(), 2000)
        }
      }
    }
    timer = setTimeout(() => void poll(), 400)
    return () => {
      cancelled = true
      controller.abort()
      clearTimeout(timer)
    }
  }, [deviceId, job?.id, job?.state, snapshot?.capabilities.mode])
  useEffect(() => {
    if (!logItem) return
    let cancelled = false
    const c = new AbortController()
    setLogs(null)
    setLogError('')
    void fetchLogs(deviceId, logItem.id, c.signal)
      .then((v) => {
        if (!cancelled) setLogs(v)
      })
      .catch((e) => {
        if (!cancelled) setLogError(errorMessage(e))
      })
    return () => {
      cancelled = true
      c.abort()
    }
  }, [deviceId, logItem])
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase()
    const list = (snapshot?.items || []).filter(
      (i) =>
        (status === 'all' ||
          (status === 'other'
            ? !['running', 'stopped'].includes(i.status)
            : i.status === status)) &&
        [
          i.name,
          i.image,
          i.network.veth,
          i.network.address,
          i.network.bridge,
          i.ownership,
        ].some((v) => v.toLowerCase().includes(q)),
    )
    return list.sort((a, b) => {
      const value = (i: Item) =>
        sort === 'memory'
          ? memoryBytes(i.memory)
          : sort === 'status'
            ? i.status
            : i.name
      const av = value(a),
        bv = value(b)
      const n =
        typeof av === 'number' && typeof bv === 'number'
          ? av - bv
          : String(av).localeCompare(String(bv))
      return descending ? -n : n
    })
  }, [snapshot, query, status, sort, descending])
  useEffect(() => {
    setPage((p) => Math.min(p, Math.max(0, Math.ceil(rows.length / 25) - 1)))
  }, [rows.length])
  const run = async (action: Action, targetId: string, draft?: Draft) => {
    if (!snapshot?.capabilities.writes) {
      setMessage('配置校验通过。当前为只读模式，真实写入尚未启用。')
      return
    }
    if (sending || busy) return
    setSending(true)
    setError('')
    setMessage('')
    try {
      const j = await performAction(
        deviceId,
        action,
        targetId,
        newRequestId(),
        draft,
        scenario,
      )
      if (active.current) {
        setJob(j)
        setConfirm(null)
      }
    } catch (e) {
      if (active.current) setError(errorMessage(e))
    } finally {
      if (active.current) setSending(false)
    }
  }
  const cancelPreparation = async () => {
    if (!job) return
    setSending(true)
    try {
      const j = await cancelJob(deviceId, job.id)
      if (active.current) setJob(j)
    } catch (e) {
      if (active.current) setError(errorMessage(e))
    } finally {
      if (active.current) setSending(false)
    }
  }
  const recover = async () => {
    if (!job) return
    setSending(true)
    try {
      const j = await recoverJob(deviceId, job.id)
      if (active.current) {
        setJob(j)
        if (j.state === 'succeeded') setEditor(null)
        setNonce((n) => n + 1)
        setError('')
      }
    } catch (e) {
      if (active.current) setError(errorMessage(e))
    } finally {
      if (active.current) setSending(false)
    }
  }
  const requestAction = (action: Action, item: Item) => {
    if (['delete', 'update', 'adopt', 'restart'].includes(action))
      setConfirm({ action, item })
    else void run(action, item.id)
  }
  const heading = (key: string, label: string) => (
    <ContainerButton
      className="ct-sort"
      type="button"
      onClick={() => {
        if (sort === key) setDescending((d) => !d)
        else {
          setSort(key)
          setDescending(false)
        }
      }}
    >
      {label} {sort === key ? (descending ? '↓' : '↑') : ''}
    </ContainerButton>
  )
  if (!deviceId)
    return (
      <section className="ct">
        <ContainerSurface>请先选择 RouterOS 设备。</ContainerSurface>
      </section>
    )
  if (!snapshot)
    return (
      <section className="ct">
        <ContainerSurface role={error || loadError ? 'alert' : 'status'}>
          {loading ? '正在读取容器、网络和存储…' : error || loadError || '没有容器数据'}
          {(error || loadError) && (
            <ContainerButton onClick={() => setNonce((n) => n + 1)}>重新读取</ContainerButton>
          )}
        </ContainerSurface>
      </section>
    )
  const c = snapshot.capabilities
  const modeLabel = `${c.mode === 'simulation' ? '模拟预览' : c.writes ? '可管理' : '只读'}${c.version ? ` · ROS ${c.version}` : ''}`
  const stats = [
    `共 ${snapshot.items.length}`,
    `运行中 ${snapshot.items.filter((i) => i.status === 'running').length}`,
    `已停止 ${snapshot.items.filter((i) => i.status === 'stopped').length}`,
  ].join(' · ')
  return (
    <section className="ct" aria-label="容器管理">
      <header className="ct-page-head">
        <div className="ct-page-title">
          <h1>容器管理</h1>
          <ModePill
            simulation={c.mode === 'simulation'}
            busy={busy}
            scenario={scenario}
            onScenario={setScenario}
            label={modeLabel}
          />
        </div>
        <div className="ct-page-actions">
          <span className="ct-stats-pill" aria-label="容器统计">{stats}</span>
          <ContainerButton
            className="ct-primary"
            disabled={!c.supported || busy}
            onClick={() => {
              setEditor({ draft: newDraft(), item: null })
              setMessage('')
            }}
          >
            {c.writes ? '＋ 创建容器' : '＋ 配置预览'}
          </ContainerButton>
        </div>
      </header>
      {!(c.mode === 'native' && c.writes) && (
        <div className="ct-notice">
          {c.mode === 'simulation'
            ? '这里的数据与所有操作均为模拟，可测试创建、失败和恢复。'
            : c.directoryWrites
              ? '容器读取与配置校验已接入。目录新建、重命名和删除直接写入所选 RouterOS；容器启停等写入尚未启用。'
              : '当前提供真实只读数据及配置校验，容器写入尚未启用。'}
        </div>
      )}
      {c.warnings.map((w) => (
        <p className="ct-notice" key={w}>
          {w}
        </p>
      ))}
      {loadError && <p className="ct-alert" role="alert">读取失败：{loadError}</p>}
      {error && !editor && !confirm && <p className="ct-alert" role="alert">{error}</p>}
      {message && (
        <p className="ct-notice" role="status">
          {message}
        </p>
      )}
      {editor && (
        <ContainerDialog
          wide
          title={editor.item ? '编辑容器' : '创建容器'}
          onClose={() => setEditor(null)}
          busy={busy}
        >
          <ContainerEditor
            key={editor.draft.draftId || editor.item?.id}
            deviceId={deviceId}
            snapshot={snapshot}
            initial={editor.draft}
            submissionError={error}
            item={editor.item}
            busy={busy}
            onClose={() => setEditor(null)}
            onSubmit={async (d) => {
              if (editor.item?.ownership === 'unmanaged') {
                setMessage('配置校验通过；编辑此容器前请先明确接管。')
                return
              }
              await run(d.existingId ? 'edit' : 'create', d.existingId, d)
            }}
          />
        </ContainerDialog>
      )}
      <ContainerSurface className="ct-list">
        <header className="ct-list-heading">
          <h3>容器列表</h3>
          <div className="ct-toolbar">
            <label className="ct-search">
              <span>搜索容器</span>
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="名称、镜像、IP 或 VETH"
              />
            </label>
            <label>
              状态{' '}
              <select
                value={status}
                onChange={(e) => setStatus(e.target.value)}
              >
                <option value="all">全部状态</option>
                <option value="running">运行中</option>
                <option value="stopped">已停止</option>
                <option value="other">其他状态</option>
              </select>
            </label>
            <div className="ct-toolbar-actions">
              <ContainerButton onClick={() => setNonce((n) => n + 1)} disabled={loading}>刷新</ContainerButton>
            </div>
          </div>
        </header>
        <div className="ct-table-scroll">
          <ContainerTable>
            <thead>
              <tr>
                <th>{heading('name', '名称')}</th>
                <th>{heading('status', '状态')}</th>
                <th className="col-hide-sm">网络</th>
                <th className="col-hide-sm">{heading('memory', '资源')}</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.slice(page * 25, (page + 1) * 25).map((item) => (
                <tr key={item.id} className={item.ownership === 'unmanaged' ? 'ct-row-unmanaged' : undefined}>
                  <td>
                    <div className="ct-name-stack">
                      <ContainerButton
                        className="ct-name"
                        title={item.name || item.id}
                        onClick={() =>
                          setEditor({ draft: editDraft(item), item })
                        }
                      >
                        {item.name || item.id}
                      </ContainerButton>
                      <small className="ct-image" title={item.image}>
                        {item.image || '本地导入镜像'}
                        {item.imageDigest ? ` · ${item.imageDigest.slice(0, 12)}…` : null}
                      </small>
                    </div>
                  </td>
                  <td>
                    <div className="ct-status-stack">
                      <ContainerStatus status={item.status}>
                        {statusLabels[item.status] || item.status}
                      </ContainerStatus>
                      <small className="ct-tags">
                        {item.ownership === 'unmanaged' && <span>未接管</span>}
                        {item.startOnBoot && <span>开机启动</span>}
                        {item.sharedVeth.length > 0 && <span>共享 VETH</span>}
                      </small>
                    </div>
                  </td>
                  <td className="col-hide-sm">
                    <div className="ct-name-stack">
                      <span className="ct-network-address">{item.network.address || '—'}</span>
                      <small>
                        {item.network.veth || '—'}
                        {item.network.bridge && ` · ${item.network.bridge}`}
                      </small>
                    </div>
                  </td>
                  <td className="ct-resource col-hide-sm">
                    <span className="ct-resource-value">
                      {item.cpu ? `${item.cpu.replace('%', '')}%` : '—'} · {memoryLabel(item.memory)}
                    </span>
                  </td>
                  <td>
                    <div className="ct-row-actions">
                      <ContainerButton
                        className="ct-icon"
                        data-tip={item.status === 'running' ? '停止' : '启动'}
                        aria-label={`${item.status === 'running' ? '停止' : '启动'} ${item.name}`}
                        disabled={
                          !c.writes ||
                          busy ||
                          !['running', 'stopped'].includes(item.status)
                        }
                        onClick={() =>
                          requestAction(
                            item.status === 'running' ? 'stop' : 'start',
                            item,
                          )
                        }
                      >
                        <span aria-hidden="true">{item.status === 'running' ? '⏸' : '▶'}</span>
                      </ContainerButton>
                      <ContainerButton
                        className="ct-icon"
                        data-tip="日志"
                        aria-label={`日志 ${item.name}`}
                        disabled={!c.logs}
                        onClick={() => setLogItem(item)}
                      >
                        <span aria-hidden="true">📋</span>
                      </ContainerButton>
                      <ContainerButton
                        className="ct-icon"
                        data-tip="编辑配置"
                        aria-label={`编辑 ${item.name}`}
                        disabled={busy}
                        onClick={() =>
                          setEditor({ draft: editDraft(item), item })
                        }
                      >
                        <span aria-hidden="true">✎</span>
                      </ContainerButton>
                      <ContainerButton
                        className="ct-icon"
                        data-tip="重启"
                        aria-label={`重启 ${item.name}`}
                        disabled={!c.writes || busy || item.status !== 'running'}
                        onClick={() => requestAction('restart', item)}
                      >
                        <span aria-hidden="true">⟳</span>
                      </ContainerButton>
                      {item.ownership === 'managed' ? (
                        <>
                          <ContainerButton
                            className="ct-icon"
                            data-tip="更新镜像"
                            aria-label={`更新 ${item.name}`}
                            disabled={!c.writes || busy}
                            onClick={() => requestAction('update', item)}
                          >
                            <span aria-hidden="true">⬆</span>
                          </ContainerButton>
                          <ContainerButton
                            className="ct-icon ct-danger"
                            data-tip="删除"
                            aria-label={`删除 ${item.name}`}
                            disabled={!c.writes || busy}
                            onClick={() => requestAction('delete', item)}
                          >
                            <span aria-hidden="true">🗑️</span>
                          </ContainerButton>
                        </>
                      ) : (
                        <ContainerButton
                          className="ct-icon"
                          data-tip="接管已有容器"
                          aria-label={`接管 ${item.name}`}
                          disabled={!c.writes || busy}
                          onClick={() => requestAction('adopt', item)}
                        >
                          <span aria-hidden="true">📥</span>
                        </ContainerButton>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </ContainerTable>
        </div>
        {rows.length === 0 && (
          <div className="ct-empty">
            {c.supported ? '没有匹配的容器' : '当前设备不支持原生容器读取'}
          </div>
        )}
        <footer className="ct-pagination">
          <span>
            {rows.length} 个结果 · 每页 25 个 ·{' '}
            {Math.min(page + 1, Math.max(1, Math.ceil(rows.length / 25)))} /{' '}
            {Math.max(1, Math.ceil(rows.length / 25))}
          </span>
          <ContainerButton
            disabled={page === 0}
            onClick={() => setPage((p) => p - 1)}
          >
            上一页
          </ContainerButton>
          <ContainerButton
            disabled={(page + 1) * 25 >= rows.length}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </ContainerButton>
        </footer>
      </ContainerSurface>
      {job &&
        (['queued', 'running', 'unknown'].includes(job.state) ||
          job.cleanupPending ||
          (job.state === 'failed' && c.mode === 'native')) && (
        <ContainerSurface as="section" className="ct-job" role="status">
          <div>
            <h3>操作进度 · {actionLabels[job.action as Action] || '容器操作'}</h3>
            <span>
              {job.phase} · {job.progress}%
            </span>
          </div>
          <progress value={job.progress} max="100" />
          {job.error && <p>{job.error}</p>}
          {job.warnings?.map((warning) => <p key={warning}>{warning}</p>)}
          {job.canCancel ? <ContainerButton type="button" className="ct-button" disabled={sending} onClick={() => void cancelPreparation()}>取消镜像准备</ContainerButton> : null}
          {(job.cleanupPending || job.state === 'unknown' || (job.state === 'failed' && c.mode === 'native')) && (
            <ContainerButton
              className="ct-primary"
              disabled={sending}
              onClick={() => void recover()}
            >
              {job.state === 'failed' ? '继续未完成步骤' : '回读确认结果'}
            </ContainerButton>
          )}
          {job.retained.length > 0 && (
            <p>保留对象：{job.retained.join('、')}</p>
          )}
        </ContainerSurface>
      )}
      {logItem && (
        <ContainerDialog title={`${logItem.name} · 日志`} onClose={() => setLogItem(null)}>
          <section className="ct-logs">
            <p>
              CPU {logItem.cpu || '不可用'} · 内存 {memoryLabel(logItem.memory, '不可用')} ·{' '}
              {logItem.network.veth} / {logItem.network.bridge}
            </p>
            {logError ? (
              <p role="alert">{logError}</p>
            ) : logs === null ? (
              <p>正在读取日志…</p>
            ) : (
              <pre>
                {logs.length
                  ? logs.map((l) => `${l.time} ${l.message}`).join('\n')
                  : logItem.config.logging
                    ? '暂无日志'
                    : '此容器的日志记录已关闭'}
              </pre>
            )}
          </section>
        </ContainerDialog>
      )}
      {confirm && (
        <ContainerDialog title={actionLabels[confirm.action]} busy={sending} onClose={() => setConfirm(null)}>
          <p>{confirm.item.name}</p>
          <p>
            {confirm.action === 'adopt'
              ? '明确将此容器纳入 rosboard 管理。现有 VETH、环境变量列表和挂载保持原归属，共享对象不自动接管。'
              : confirm.action === 'delete'
                ? '只删除容器及 rosboard 独立创建并记录归属的对象。独占运行目录及其内容会一并删除，不再改名备份。共享运行目录或与挂载数据重叠时会阻止删除；独立挂载数据保持保留。'
                : confirm.action === 'update'
                  ? c.mode === 'simulation' ? '模拟重新下载镜像并替换容器，保留配置和存储数据。' : '在新的运行目录重新导入镜像，完成后切换容器。将原运行目录改名保留，挂载数据保留；切换会暂时中断服务。'
                  : '容器运行会暂时中断。'}
            {confirm.item.sharedVeth.length > 0 &&
              ` VETH 共享关系：${confirm.item.sharedVeth.join(', ')}。`}
          </p>
          <div className="ct-actions">
            <ContainerButton disabled={sending} onClick={() => setConfirm(null)}>
              取消
            </ContainerButton>
            <ContainerButton
              className={confirm.action === 'delete' ? 'ct-danger' : 'ct-primary'}
              disabled={sending}
              onClick={() => void run(confirm.action, confirm.item.id)}
            >
              {sending ? '正在提交…' : c.mode === 'simulation' ? '确认模拟操作' : '确认操作'}
            </ContainerButton>
          </div>
        </ContainerDialog>
      )}
    </section>
  )
}
