import { useState } from 'react'
import type { PolicyEgress, PolicySource } from './types'
import { deleteEgress, saveEgress } from './api'
import { egressDraftFromEgress } from './drafts'
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
          description={readOnly ? '当前为只读状态，无法创建策略路由。' : '通过向导创建第一条策略路由：选择 WAN 与地址族，确认路由 / DNS / LAN 范围。'}
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
                  <th>域名列表</th>
                  <th>标记列表</th>
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
  const [toggling, setToggling] = useState(false)
  const status = egressStatus(egress)
  const enabledFamilies = egress.families.filter((f) => f.enabled).map((f) => policyFamilyLabel[f.family] ?? f.family).join(' / ') || '—'
  const egressSources = sources.filter((s) => egress.sources.some((es) => es.id === s.id))
  const sharedListUsers = egress.listMode === 'shared'
    ? egresses.filter((item) => item.id !== egress.id && item.listName === egress.listName && item.enabled)
    : []
  const sourceNames = egressSources.map((s) => s.name).join(', ') || '—'

  const handleToggle = async () => {
    setEditError(null)
    setToggling(true)
    try {
      await saveEgress(deviceID, { ...egressDraftFromEgress(egress), enabled: !egress.enabled })
      onChanged()
    } catch (e) {
      setEditError(e instanceof Error ? e.message : '操作失败')
    } finally {
      setToggling(false)
    }
  }

  const handleDelete = async () => {
    setDeleteError(null)
    try {
      await deleteEgress(deviceID, egress.id, egress.revision)
      setDeleteModal(false)
      onChanged()
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : '删除失败')
    }
  }

  return (
    <>
      <tr className={egress.enabled ? '' : 'disabled-row'}>
        <td><strong>{egress.name}</strong></td>
        <td><span className="policy-cell-stack">{enabledFamilies}</span></td>
        <td>{sourceNames}</td>
        <td><code>{egress.listName || '—'}</code></td>
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
              {egress.pendingDeletion ? <small className="policy-hint">待清理：请点击“应用设置”</small> : (
                <>
                  <button type="button" className="link-button" disabled={toggling} onClick={handleToggle}>
                    {toggling ? '处理中…' : egress.enabled ? '停用' : '启用'}
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
            确认删除出口策略「{egress.name}」？其绑定的域名列表会先解除引用，出口对象将在下一次应用设置时从 RouterOS 清理。
          </p>
          {egressSources.length ? <p className="policy-hint">当前绑定域名列表：{egressSources.map((source) => source.name).join('、')}。</p> : null}
          {sharedListUsers.length ? <PolicyNotice tone="info" title="共享标记列表仍被其他出口使用">标记列表 {egress.listName} 仍被 {sharedListUsers.map((item) => item.name).join('、')} 使用；删除本出口不会删除或停用该共享列表。</PolicyNotice> : null}
          {egress.revision ? <p className="policy-hint">修订版本：{egress.revision}</p> : null}
          <div className="policy-form-actions">
            <button type="button" className="primary-button" onClick={handleDelete}>确认删除</button>
            <button type="button" className="toolbar-button" onClick={() => setDeleteModal(false)}>取消</button>
          </div>
        </PolicyModal>
      ) : null}
    </>
  )
}
