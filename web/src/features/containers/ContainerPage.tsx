import { useEffect, useMemo, useRef, useState } from 'react'
import { errorMessage } from '../../lib/api'
import {
  fetchContainers,
  fetchJob,
  fetchLogs,
  performAction,
  recoverJob,
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
    } | null>(null),
    [job, setJob] = useState<Job | null>(null),
    [sending, setSending] = useState(false),
    [message, setMessage] = useState('')
  const [confirm, setConfirm] = useState<{ action: Action; item: Item } | null>(
      null,
    ),
    [logItem, setLogItem] = useState<Item | null>(null),
    [logs, setLogs] = useState<ContainerLog[] | null>(null),
    [logError, setLogError] = useState('')
  const [scenario, setScenario] = useState('success')
  const active = useRef(true),
    dialog = useRef<HTMLDialogElement>(null)
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
          setError('')
        }
      } catch (e) {
        if (!cancelled) setError(errorMessage(e))
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
          setMessage('模拟操作完成')
          setEditor(null)
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
  }, [deviceId, job?.id, job?.state])
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
  useEffect(() => {
    const element = dialog.current
    if (confirm) element?.showModal()
    return () => element?.close()
  }, [confirm])
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase()
    const list = (snapshot?.items || []).filter(
      (i) =>
        (status === 'all' || i.status === status) &&
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
      const av =
        sort === 'cpu'
          ? Number.parseFloat(a.cpu) || 0
          : sort === 'memory'
            ? memoryBytes(a.memory)
            : sort === 'status'
              ? a.status
              : sort === 'image'
                ? a.image
                : a.name
      const bv =
        sort === 'cpu'
          ? Number.parseFloat(b.cpu) || 0
          : sort === 'memory'
            ? memoryBytes(b.memory)
            : sort === 'status'
              ? b.status
              : sort === 'image'
                ? b.image
                : b.name
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
    <button
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
    </button>
  )
  if (!deviceId)
    return (
      <section className="ct">
        <div className="ct-card">请先选择 RouterOS 设备。</div>
      </section>
    )
  if (!snapshot)
    return (
      <section className="ct">
        <div className="ct-card" role={error ? 'alert' : 'status'}>
          {loading ? '正在读取容器、网络和存储…' : error || '没有容器数据'}
          {error && (
            <button onClick={() => setNonce((n) => n + 1)}>重新读取</button>
          )}
        </div>
      </section>
    )
  const c = snapshot.capabilities
  return (
    <section className="ct" aria-label="容器管理">
      <header className="ct-heading">
        <div className="ct-brand">▣</div>
        <div>
          <span className="ct-eyebrow">NATIVE CONTAINERS</span>
          <h1>容器管理</h1>
          <p>镜像、网络与运行状态，集中管理。</p>
        </div>
        <span className="ct-mode">
          {c.mode === 'simulation' ? '模拟预览' : '只读模式'}
          {c.version && ` · ROS ${c.version}`}
        </span>
        <button onClick={() => setNonce((n) => n + 1)} disabled={loading}>
          刷新
        </button>
        <button
          className="ct-primary"
          disabled={!c.supported || busy}
          onClick={() => {
            setEditor({ draft: newDraft(), item: null })
            setMessage('')
          }}
        >
          {c.writes ? '＋ 创建容器' : '＋ 配置预览'}
        </button>
      </header>
      <div className="ct-notice">
        {c.mode === 'simulation'
          ? '这里的数据与所有操作均为模拟，可测试创建、失败和恢复。'
          : '当前提供真实只读数据及配置校验，容器写入尚未启用。'}
      </div>
      {c.warnings.map((w) => (
        <p className="ct-notice" key={w}>
          {w}
        </p>
      ))}
      {error && (
        <p className="ct-alert" role="alert">
          {error}
        </p>
      )}
      {message && (
        <p className="ct-notice" role="status">
          {message}
        </p>
      )}
      {c.mode === 'simulation' && (
        <div className="ct-simulation">
          <label>
            下一次模拟操作{' '}
            <select
              aria-label="模拟情景"
              value={scenario}
              onChange={(e) => setScenario(e.target.value)}
              disabled={busy}
            >
              <option value="success">正常完成</option>
              <option value="failure">下载 / 操作失败</option>
              <option value="unknown">超时结果不明 → 回读恢复</option>
            </select>
          </label>
          <small>结果不明时不会重复创建，请使用任务中的回读确认。</small>
        </div>
      )}
      {editor ? (
        <ContainerEditor
          key={editor.draft.draftId || editor.item?.id}
          deviceId={deviceId}
          snapshot={snapshot}
          initial={editor.draft}
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
      ) : (
        <>
          <div className="ct-stats">
            <div className="ct-card">
              <span>全部容器</span>
              <strong>{snapshot.items.length}</strong>
            </div>
            <div className="ct-card">
              <span>运行中</span>
              <strong className="ct-green">
                {snapshot.items.filter((i) => i.status === 'running').length}
              </strong>
            </div>
            <div className="ct-card">
              <span>已停止</span>
              <strong>
                {snapshot.items.filter((i) => i.status === 'stopped').length}
              </strong>
            </div>
            <div className="ct-card">
              <span>rosboard 管理</span>
              <strong>
                {snapshot.items.filter((i) => i.ownership === 'managed').length}
              </strong>
            </div>
          </div>
          <div className="ct-card ct-list">
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
                  {Object.entries(statusLabels).map(([value, label]) => (
                    <option value={value} key={value}>
                      {label}
                    </option>
                  ))}
                </select>
              </label>
              <span>{rows.length} 个结果</span>
            </div>
            <div className="ct-table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>{heading('name', '名称')}</th>
                    <th>{heading('status', '状态')}</th>
                    <th>{heading('image', '镜像')}</th>
                    <th>IP / VETH</th>
                    <th>Bridge</th>
                    <th>{heading('cpu', 'CPU')}</th>
                    <th>{heading('memory', '内存')}</th>
                    <th>端口映射</th>
                    <th>开机启动</th>
                    <th>归属</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.slice(page * 25, (page + 1) * 25).map((item) => (
                    <tr key={item.id}>
                      <td>
                        <button
                          className="ct-name"
                          onClick={() =>
                            setEditor({ draft: editDraft(item), item })
                          }
                        >
                          {item.name || item.id}
                        </button>
                      </td>
                      <td>
                        <span className={`ct-state ct-state-${item.status}`}>
                          {statusLabels[item.status] || item.status}
                        </span>
                      </td>
                      <td className="ct-image" title={item.image}>
                        {item.image || '本地导入镜像'}
                      </td>
                      <td>
                        <span>{item.network.address || '—'}</span>
                        <small>
                          {item.network.veth || '—'}
                          {item.sharedVeth.length > 0 &&
                            ` · 共享 ${item.sharedVeth.length + 1} 个容器`}
                        </small>
                      </td>
                      <td>{item.network.bridge || '—'}</td>
                      <td>
                        {item.cpu ? `${item.cpu.replace('%', '')}%` : '—'}
                      </td>
                      <td>{item.memory || '—'}</td>
                      <td>
                        {item.ports.map((p, i) => (
                          <small key={i}>
                            {p.host} → {p.container}/{p.protocol.toUpperCase()}
                          </small>
                        ))}
                        {item.ports.length === 0 && '—'}
                      </td>
                      <td>{item.startOnBoot ? '开启' : '关闭'}</td>
                      <td>
                        <span className="ct-ownership">
                          {item.ownership === 'managed' ? 'rosboard' : '未接管'}
                        </span>
                      </td>
                      <td>
                        <div className="ct-row-actions">
                          <button
                            title={item.status === 'running' ? '停止' : '启动'}
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
                            {item.status === 'running' ? '■' : '▶'}
                          </button>
                          <button
                            title="重启"
                            aria-label={`重启 ${item.name}`}
                            disabled={
                              !c.writes || busy || item.status !== 'running'
                            }
                            onClick={() => requestAction('restart', item)}
                          >
                            ↻
                          </button>
                          <button
                            title="日志"
                            aria-label={`日志 ${item.name}`}
                            disabled={!c.logs}
                            onClick={() => setLogItem(item)}
                          >
                            ≡
                          </button>
                          <button
                            title="编辑配置"
                            aria-label={`编辑 ${item.name}`}
                            disabled={busy}
                            onClick={() =>
                              setEditor({ draft: editDraft(item), item })
                            }
                          >
                            ✎
                          </button>
                          {item.ownership === 'managed' ? (
                            <>
                              <button
                                title="手动更新镜像"
                                aria-label={`更新 ${item.name}`}
                                disabled={!c.writes || busy}
                                onClick={() => requestAction('update', item)}
                              >
                                ↑
                              </button>
                              <button
                                title="删除"
                                aria-label={`删除 ${item.name}`}
                                disabled={!c.writes || busy}
                                onClick={() => requestAction('delete', item)}
                              >
                                ×
                              </button>
                            </>
                          ) : (
                            <button
                              title="接管已有容器"
                              aria-label={`接管 ${item.name}`}
                              disabled={!c.writes || busy}
                              onClick={() => requestAction('adopt', item)}
                            >
                              接管
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {rows.length === 0 && (
              <div className="ct-empty">
                {c.supported ? '没有匹配的容器' : '当前设备不支持原生容器读取'}
              </div>
            )}
            <footer className="ct-pagination">
              <span>
                每页 25 个 ·{' '}
                {Math.min(page + 1, Math.max(1, Math.ceil(rows.length / 25)))} /{' '}
                {Math.max(1, Math.ceil(rows.length / 25))}
              </span>
              <button
                disabled={page === 0}
                onClick={() => setPage((p) => p - 1)}
              >
                上一页
              </button>
              <button
                disabled={(page + 1) * 25 >= rows.length}
                onClick={() => setPage((p) => p + 1)}
              >
                下一页
              </button>
            </footer>
          </div>
        </>
      )}
      {job && (
        <section className="ct-card ct-job" role="status">
          <div>
            <h3>操作进度 · {job.action}</h3>
            <span>
              {job.phase} · {job.progress}%
            </span>
          </div>
          <progress value={job.progress} max="100" />
          {job.error && <p>{job.error}</p>}
          {job.state === 'unknown' && (
            <button
              className="ct-primary"
              disabled={sending}
              onClick={() => void recover()}
            >
              回读确认结果
            </button>
          )}
          {job.retained.length > 0 && (
            <p>保留对象：{job.retained.join('、')}</p>
          )}
        </section>
      )}
      {logItem && (
        <section className="ct-card ct-logs">
          <header>
            <h3>{logItem.name} · 日志</h3>
            <button onClick={() => setLogItem(null)}>关闭日志</button>
          </header>
          <p>
            CPU {logItem.cpu || '不可用'} · 内存 {logItem.memory || '不可用'} ·{' '}
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
      )}
      {confirm && (
        <dialog
          ref={dialog}
          className="ct-dialog"
          onCancel={(e) => {
            if (sending) e.preventDefault()
            else setConfirm(null)
          }}
        >
          <h2>
            {
              (
                {
                  adopt: '接管容器',
                  delete: '删除容器',
                  update: '手动更新镜像',
                  restart: '重启容器',
                } as Record<string, string>
              )[confirm.action]
            }
          </h2>
          <p>{confirm.item.name}</p>
          <p>
            {confirm.action === 'adopt'
              ? '明确将此容器纳入 rosboard 管理。现有 VETH、环境变量列表和挂载保持原归属，共享对象不自动接管。'
              : confirm.action === 'delete'
                ? '只删除容器及 rosboard 独立创建并记录归属的对象。已有和共享对象、存储数据保持保留。'
                : confirm.action === 'update'
                  ? '模拟重新下载镜像并替换容器，保留配置和存储数据。真实更新的数据保留行为需在独立 RouterOS 上验收。'
                  : '容器运行会暂时中断。'}
            {confirm.item.sharedVeth.length > 0 &&
              ` VETH 共享关系：${confirm.item.sharedVeth.join(', ')}。`}
          </p>
          <div className="ct-actions">
            <button disabled={sending} onClick={() => setConfirm(null)}>
              取消
            </button>
            <button
              className="ct-primary"
              disabled={sending}
              onClick={() => void run(confirm.action, confirm.item.id)}
            >
              {sending ? '正在提交…' : '确认模拟操作'}
            </button>
          </div>
        </dialog>
      )}
    </section>
  )
}
