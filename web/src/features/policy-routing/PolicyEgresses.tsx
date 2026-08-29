import { useState } from 'react'
import type { PolicyEgress, PolicySource } from './types'
import { deleteEgress, setEgressEnabled, waitForPolicyJob } from './api'
import {
  PolicyErrorDisplay,
  PolicyEmptyState,
  PolicyModal,
  PolicyNotice,
  PolicyStatusBadge,
  type StatusTone,
} from './components'
import {
  policyFailureModeLabel,
  policyFamilyLabel,
} from './format'

export function PolicyEgressTable({
  deviceID,
  egresses,
  sources,
  readOnly,
  onCreate,
  onEdit,
  onChanged,
}: {
  deviceID: string
  egresses: PolicyEgress[]
  sources: PolicySource[]
  readOnly: boolean
  onChanged: () => void
  onCreate: () => void
  onEdit: (egress: PolicyEgress) => void
}) {
  return (
    <section className="panel policy-panel" aria-label="出口策略">
      <div className="policy-panel-head">
        <h3>出口策略</h3>
      </div>
      {egresses.length === 0 ? (
        <PolicyEmptyState
          title="尚未配置策略路由"
          description={readOnly ? '当前为只读状态，无法创建策略路由。' : '通过向导创建第一条策略路由：选择 WAN 与地址族，确认路由 / DNS / 策略流量入口。'}
          action={readOnly ? undefined : <button type="button" className="primary-button" onClick={onCreate}>新建策略</button>}
        />
      ) : (
        <>
          <div className="policy-table-scroll">
            <table className="data-table policy-egress-table">
              <thead>
                <tr>
                  <th>名称</th>
                  <th>策略接口</th>
                  <th>下一跳网关</th>
                  <th>域名列表</th>
                  <th>地址族</th>
                  <th>优先级</th>
                  <th>断线处理</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {egresses.map((eg) => (
                  <EgressRow key={eg.id} deviceID={deviceID} egress={eg} egresses={egresses} sources={sources} readOnly={readOnly} onChanged={onChanged} onEdit={onEdit} />
                ))}
              </tbody>
            </table>
          </div>
          {!readOnly ? (
            <div className="policy-panel-add-action">
              <button type="button" className="primary-button" onClick={onCreate}>增加策略</button>
            </div>
          ) : null}
        </>
      )}
    </section>
  )
}

function egressStatus(eg: PolicyEgress): { tone: StatusTone; label: string } {
  return eg.enabled
    ? { tone: 'good', label: '启用' }
    : { tone: 'neutral', label: '停用' }
}

function EgressRow({
  deviceID,
  egress,
  egresses,
  sources,
  readOnly,
  onChanged,
  onEdit,
}: {
  deviceID: string
  egress: PolicyEgress
  egresses: PolicyEgress[]
  sources: PolicySource[]
  readOnly: boolean
  onChanged: () => void
  onEdit: (egress: PolicyEgress) => void
}) {
  const [editError, setEditError] = useState<string | null>(null)
  const [deleteModal, setDeleteModal] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [toggling, setToggling] = useState(false)
  const [retryEnabled, setRetryEnabled] = useState<boolean | null>(null)
  const status = egressStatus(egress)
  const enabledFamilies = egress.families.filter((f) => f.enabled)
  const ipVersions = enabledFamilies.map((f) => policyFamilyLabel[f.family] ?? f.family).join('/') || '—'
  const wanInterfaces = [...new Set(enabledFamilies.map((f) => f.wanInterface.trim()).filter(Boolean))].join(' / ') || '—'
  const gateways = [...new Set(enabledFamilies.map((f) => f.gateway.trim()).filter(Boolean))].join(' / ') || '—'
  const egressSources = sources.filter((s) => egress.sources.some((es) => es.id === s.id))
  const sharedListUsers = egress.listMode === 'shared'
    ? egresses.filter((item) => item.id !== egress.id && item.listName === egress.listName && item.enabled)
    : []
  const sourceNames = egressSources.map((s) => s.name).join(', ') || '—'

  const handleToggle = async () => {
    setEditError(null)
    setToggling(true)
    const enabled = retryEnabled ?? !egress.enabled
    try {
      const result = await setEgressEnabled(deviceID, egress.id, enabled, egress.revision)
      if (result.jobId) await waitForPolicyJob(deviceID, result.jobId)
      setRetryEnabled(null)
      onChanged()
    } catch (e) {
      setRetryEnabled(enabled)
      setEditError(e instanceof Error ? e.message : '操作失败')
    } finally {
      setToggling(false)
    }
  }

  const handleDelete = async () => {
    setDeleteError(null)
    setDeleting(true)
    try {
      const result = await deleteEgress(deviceID, egress.id, egress.revision)
      if (result.jobId) await waitForPolicyJob(deviceID, result.jobId)
      setDeleteModal(false)
      onChanged()
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : '删除失败')
      onChanged()
    } finally {
      setDeleting(false)
    }
  }

  return (
    <>
      <tr className={egress.enabled ? '' : 'disabled-row'}>
        <td><strong>{egress.name}</strong></td>
        <td><span className="policy-cell-stack">{wanInterfaces}</span></td>
        <td><span className="mono">{gateways}</span></td>
        <td>{sourceNames}</td>
        <td>{ipVersions}</td>
        <td>{egress.priority}</td>
        <td>{policyFailureModeLabel[egress.failureMode] ?? egress.failureMode}</td>
        <td>
          <span className="policy-cell-stack">
            <PolicyStatusBadge tone={status.tone}>{status.label}</PolicyStatusBadge>
            {editError ? <small className="policy-field-error">{editError}</small> : null}
          </span>
        </td>
        <td>
          {readOnly ? null : (
            <div className="action-links">
              {egress.pendingDeletion ? <button type="button" className="link-button link-button--danger" disabled={deleting} onClick={() => setDeleteModal(true)}>重试清理</button> : (
                <>
                  <button type="button" className="link-button" disabled={toggling} onClick={handleToggle}>
                    {toggling ? '处理中…' : retryEnabled !== null ? `重试${retryEnabled ? '启用' : '停用'}` : egress.enabled ? '停用' : '启用'}
                  </button>
                  <button type="button" className="link-button" onClick={() => onEdit(egress)}>编辑</button>
                  <button type="button" className="link-button link-button--danger" onClick={() => setDeleteModal(true)}>删除</button>
                </>
              )}
            </div>
          )}
        </td>
      </tr>
      {deleteModal ? (
        <PolicyModal title="删除出口策略" onClose={() => setDeleteModal(false)}>
          {deleteError ? <PolicyErrorDisplay error={deleteError} /> : null}
          <p className="policy-hint">
            {egress.pendingDeletion ? `继续清理出口策略「${egress.name}」在 RouterOS 中的受管对象？` : `确认删除出口策略「${egress.name}」？确认后将立即解除域名列表引用并清理 RouterOS 中属于该策略的对象。`}
          </p>
          {egressSources.length ? <p className="policy-hint">当前绑定域名列表：{egressSources.map((source) => source.name).join('、')}。</p> : null}
          {sharedListUsers.length ? <PolicyNotice tone="info" title="共享标记列表仍被其他出口使用">标记列表 {egress.listName} 仍被 {sharedListUsers.map((item) => item.name).join('、')} 使用；删除本出口不会删除或停用该共享列表。</PolicyNotice> : null}
          {egress.revision ? <p className="policy-hint">修订版本：{egress.revision}</p> : null}
          <div className="policy-form-actions">
            <button type="button" className="primary-button" disabled={deleting} onClick={handleDelete}>{deleting ? '正在清理…' : egress.pendingDeletion ? '重试清理' : '确认删除'}</button>
            <button type="button" className="toolbar-button" disabled={deleting} onClick={() => setDeleteModal(false)}>取消</button>
          </div>
        </PolicyModal>
      ) : null}
    </>
  )
}
