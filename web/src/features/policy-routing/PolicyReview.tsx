import { useState } from 'react'
import type { PolicyApplyResult, PolicyDiscovery, PolicyDrift, PolicyHealth, PolicyPlanEnvelope } from './types'
import { generateAdoptionPlan, generateDriftPlan } from './api'
import { ChangePlanView } from './ChangePlanView'
import {
  PolicyErrorDisplay,
  PolicyModal,
  PolicyNotice,
  PolicyStatusBadge,
} from './components'
import { policyOwnershipLabel } from './format'

export function PolicyDriftPanel({
  deviceID,
  health,
  drift,
  onPlan,
}: {
  deviceID: string
  health?: PolicyHealth
  drift?: PolicyDrift
  onPlan: (envelope: PolicyPlanEnvelope) => void
}) {
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!health && !drift) return null
  const hasDrift = drift?.state === 'drifted' && (drift.items?.length ?? 0) > 0
  const paused = health?.mutationPaused || health?.manualInterventionRequired
  if (!hasDrift && !paused) return null

  const handleGenerate = async () => {
    setError(null)
    setGenerating(true)
    try {
      const envelope = await generateDriftPlan(deviceID)
      onPlan(envelope)
    } catch (e) {
      setError(e instanceof Error ? e.message : '生成恢复计划失败')
    } finally {
      setGenerating(false)
    }
  }

  return (
    <section className="policy-panel policy-drift-panel">
      <div className="policy-panel-head">
        <h3>漂移检测</h3>
        <PolicyStatusBadge tone={hasDrift ? 'bad' : 'warn'}>
          {hasDrift ? '检测到漂移' : paused ? '写入已暂停' : '正常'}
        </PolicyStatusBadge>
      </div>
      <div className="policy-panel-body">
        {error ? <PolicyErrorDisplay error={error} /> : null}
        {health?.pauseReason ? (
          <PolicyNotice tone="warn" title="写入已暂停">{health.pauseReason}</PolicyNotice>
        ) : null}
        {hasDrift && drift?.items?.length ? (
          <div className="policy-issue-block policy-issue-warn">
            <h4>漂移项 ({drift.items.length})</h4>
            <ul>
              {drift.items.slice(0, 10).map((item, i) => (
                <li key={i}>
                  {item.reason}
                  {item.logicalID ? <code className="policy-issue-code">{item.logicalID}</code> : null}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        <div className="policy-form-actions">
          <button type="button" className="primary-button" disabled={generating} onClick={handleGenerate}>
            {generating ? '生成中…' : '生成恢复计划'}
          </button>
        </div>
      </div>
    </section>
  )
}

export function PolicyTakeoverModal({
  deviceID,
  discovery,
  onClose,
  onApplied,
}: {
  deviceID: string
  discovery: PolicyDiscovery
  onClose: () => void
  onApplied: (result?: PolicyApplyResult) => void
}) {
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirmed, setConfirmed] = useState<Set<string>>(new Set())
  const [generating, setGenerating] = useState(false)
  const [plan, setPlan] = useState<PolicyPlanEnvelope | null>(null)
  const [error, setError] = useState<string | null>(null)

  const allCandidates = [
    ...(discovery.adoptionCandidates ?? []),
    ...(discovery.existingPolicy ?? []).filter((item) => item.ownership === 'manual_candidate'),
  ]
  const adoptable = Array.from(new Map(allCandidates.map((item) => [item.logicalId, item])).values())
  const foreign = discovery.existingPolicy?.filter((p) => p.foreignManager || p.ownership === 'foreign') ?? []
  const readOnly = discovery.existingPolicy?.filter((p) => !p.foreignManager && p.ownership !== 'manual_candidate' && p.ownership !== 'foreign') ?? []

  const toggleSelect = (logicalId: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(logicalId)) {
        next.delete(logicalId)
        setConfirmed((current) => {
          const nextConfirmed = new Set(current)
          nextConfirmed.delete(logicalId)
          return nextConfirmed
        })
      } else next.add(logicalId)
      return next
    })
  }

  const toggleConfirmation = (logicalId: string) => {
    setConfirmed((prev) => {
      const next = new Set(prev)
      if (next.has(logicalId)) next.delete(logicalId)
      else next.add(logicalId)
      return next
    })
  }

  const handleGenerate = async () => {
    setError(null)
    setGenerating(true)
    try {
      const items = Array.from(selected).map((id) => {
        const item = adoptable.find((candidate) => candidate.logicalId === id)
        return {
          logicalId: id,
          routerId: item?.routerId,
          selected: true,
          userSelected: true,
          compatibilityComplete: confirmed.has(id),
          force: false,
        }
      })
      const envelope = await generateAdoptionPlan(deviceID, items)
      setPlan(envelope)
    } catch (e) {
      setError(e instanceof Error ? e.message : '生成接管计划失败')
    } finally {
      setGenerating(false)
    }
  }

  if (plan) {
    return (
      <PolicyModal title="接管计划" wide onClose={onClose}>
        <ChangePlanView
          deviceID={deviceID}
          plan={plan.plan}
          onApplied={(result) => { setPlan(null); onApplied(result) }}
          onCancel={() => setPlan(null)}
        />
      </PolicyModal>
    )
  }

  return (
    <PolicyModal title="接管发现" wide onClose={onClose}>
      {error ? <PolicyErrorDisplay error={error} /> : null}
      {adoptable.length ? (
        <div className="policy-ownership-list">
          <h4>可接管对象 ({adoptable.length})</h4>
          {adoptable.map((item) => (
            <div key={item.logicalId} className="policy-ownership-item">
              <label className="policy-ownership-confirm">
                <input
                  type="checkbox"
                  checked={selected.has(item.logicalId)}
                  onChange={() => toggleSelect(item.logicalId)}
                />
                <span>{item.logicalId}</span>
              </label>
              <span className="policy-ownership-label">{policyOwnershipLabel[item.ownership] ?? item.ownership}</span>
              {item.reason ? <small className="policy-ownership-reason">{item.reason}</small> : null}
              {selected.has(item.logicalId) ? (
                <label className="policy-ownership-confirm">
                  <input type="checkbox" checked={confirmed.has(item.logicalId)} onChange={() => toggleConfirmation(item.logicalId)} />
                  <span>确认兼容性检查和所有权变更</span>
                </label>
              ) : null}
            </div>
          ))}
        </div>
      ) : (
        <PolicyNotice tone="info">没有可接管的对象</PolicyNotice>
      )}

      {foreign.length ? (
        <div className="policy-ownership-list">
          <h4>其他实例所有（不可接管）</h4>
          {foreign.map((item) => (
            <div key={item.logicalId} className="policy-ownership-item">
              <span>{item.logicalId}</span>
              <span className="policy-ownership-label">{policyOwnershipLabel[item.ownership] ?? item.ownership}</span>
            </div>
          ))}
        </div>
      ) : null}

      {readOnly.length ? (
        <div className="policy-ownership-list">
          <h4>已存在对象（只读）</h4>
          {readOnly.map((item) => (
            <div key={item.logicalId} className="policy-ownership-item">
              <span>{item.logicalId}</span>
              <span className="policy-ownership-label">{policyOwnershipLabel[item.ownership] ?? item.ownership}</span>
              {item.reason ? <small className="policy-ownership-reason">{item.reason}</small> : null}
            </div>
          ))}
        </div>
      ) : null}

      {selected.size > 0 ? (
        <PolicyNotice tone={confirmed.size === selected.size ? 'info' : 'warn'}>
          {confirmed.size === selected.size ? '已确认全部所选对象的兼容性。' : '请逐项确认所选对象的兼容性后再生成接管计划。'}
        </PolicyNotice>
      ) : null}

      <div className="policy-form-actions">
        <button type="button" className="primary-button" disabled={generating || selected.size === 0 || confirmed.size !== selected.size} onClick={handleGenerate}>
          {generating ? '生成中…' : '生成接管计划'}
        </button>
        <button type="button" className="toolbar-button" onClick={onClose}>取消</button>
      </div>
    </PolicyModal>
  )
}
