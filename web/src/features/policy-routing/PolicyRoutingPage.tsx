import { useCallback, useEffect, useState } from 'react'
import type { PolicyApplyResult, PolicyEgress, PolicyPlanEnvelope, PolicySection } from './types'
import { generatePlan } from './api'
import { usePolicyDiscovery, usePolicyOverview } from './hooks'
import { PolicyAccessCard } from './PolicyAccessCard'
import { PolicyAuditDialog } from './PolicyAuditDialog'
import { PolicyDriftPanel, PolicyTakeoverModal } from './PolicyReview'
import { PolicyEgressTable } from './PolicyEgresses'
import { PolicySourcesPage } from './PolicySourcesPage'
import { PolicyWizard } from './PolicyWizard'
import { ChangePlanView } from './ChangePlanView'
import {
  PolicyErrorDisplay,
  PolicyModal,
  PolicyMoreMenu,
  PolicyNotice,
  PolicyPreparing,
} from './components'
import { policySetupStateLabel } from './format'

const policyRoutingViewKey = 'rosboard:policy-routing-view'

export function PolicyRoutingPage({
  deviceID,
  refreshNonce,
  section,
}: {
  deviceID: string
  refreshNonce: number
  section: PolicySection
}) {
  const { overview, loading, error, reload } = usePolicyOverview(deviceID, refreshNonce)
  const discoveryEnabled = overview?.access.enabled === true && overview?.setup.state === 'ready'
  const { discovery } = usePolicyDiscovery(deviceID, discoveryEnabled)

  const [wizardEgress, setWizardEgress] = useState<PolicyEgress | null | undefined>(undefined)
  const [auditOpen, setAuditOpen] = useState(false)
  const [takeoverOpen, setTakeoverOpen] = useState(false)
  const [applyPlan, setApplyPlan] = useState<PolicyPlanEnvelope | null>(null)
  const [statusMessage, setStatusMessage] = useState<string | null>(null)
  const [generatingPlan, setGeneratingPlan] = useState(false)
  const [planError, setPlanError] = useState<string | null>(null)

  // Persist section
  useEffect(() => {
    window.sessionStorage.setItem(policyRoutingViewKey, JSON.stringify({ view: 'policy-routing', section }))
  }, [section])

  const handleApplied = useCallback((result?: PolicyApplyResult) => {
    setApplyPlan(null)
    setWizardEgress(undefined)
    setTakeoverOpen(false)
    const jobID = result?.jobId ?? result?.job?.id
    setStatusMessage(jobID
      ? `变更已提交，后台任务正在处理（任务 ${jobID.slice(0, 8)}）。RouterOS 写入是异步的，刷新可查看草稿状态。`
      : '变更已提交，后台任务正在处理。RouterOS 写入是异步的，刷新可查看草稿状态。')
    void reload()
  }, [reload])

  const handleGeneratePlan = async () => {
    setPlanError(null)
    setGeneratingPlan(true)
    try {
      const envelope = await generatePlan(deviceID, overview?.egresses.length ? 'structural' : 'initial')
      setApplyPlan(envelope)
    } catch (e) {
      setPlanError(e instanceof Error ? e.message : '生成应用计划失败')
    } finally {
      setGeneratingPlan(false)
    }
  }

  if (loading && !overview) {
    return <PolicyPreparing text="正在读取策略状态…" />
  }

  if (error && !overview) {
    return <PolicyErrorDisplay error={error} />
  }

  if (!overview) {
    return <PolicyPreparing text="正在读取策略状态…" />
  }

  const readOnly = !overview.device.enabled || Boolean(overview.device.archived) || !overview.access.enabled || overview.setup.state !== 'ready'
  const setupState = overview.setup.state

  return (
    <div className="policy-page">
      {statusMessage ? <PolicyNotice tone="good">{statusMessage}</PolicyNotice> : null}
      {error ? <PolicyErrorDisplay error={error} /> : null}
      {planError ? <PolicyErrorDisplay error={planError} /> : null}

      {setupState === 'manager_unavailable' ? (
        <PolicyNotice tone="warn" title="策略管理器不可用">
          策略管理器未运行，当前仅展示已保存在面板的草稿与历史状态；扫描、计划与应用不可用。
        </PolicyNotice>
      ) : setupState === 'runtime_unavailable' ? (
        <PolicyNotice tone="warn" title="RouterOS 策略运行时不可用">
          {overview.capability?.reason || '无法连接 RouterOS 策略运行时。'}草稿仍可编辑，预览与应用暂不可用。
        </PolicyNotice>
      ) : setupState === 'access_required' ? (
        <PolicyNotice tone="warn" title="策略运行时未就绪">
          {policySetupStateLabel[setupState] ?? setupState} — 请在下方配置策略管理账号。
        </PolicyNotice>
      ) : null}

      {readOnly && setupState === 'ready' ? (
        <PolicyNotice tone="info" title="当前为只读状态">
          {overview.access.enabled
            ? '设备已停用或策略运行时未就绪，草稿编辑、预览与应用暂不可用；下方仍可更换策略管理账号。'
            : '完成策略管理账号接入后，才能编辑草稿、生成预览与应用变更。'}
        </PolicyNotice>
      ) : null}

      {overview.health?.mutationPaused ? (
        <PolicyNotice tone="warn" title="写入已暂停">
          {overview.health.pauseReason || '策略写入已暂停，请检查漂移状态并生成恢复计划'}
        </PolicyNotice>
      ) : null}

      {section === 'settings' ? (
        <>
          {overview.activeJobs?.length ? (
            <PolicyNotice tone="info">
              {overview.activeJobs.length} 个作业正在执行中…
            </PolicyNotice>
          ) : null}

          <PolicyDriftPanel
            deviceID={deviceID}
            health={overview.health}
            drift={overview.drift}
            onPlan={(envelope) => setApplyPlan(envelope)}
          />

          <PolicyEgressTable
            deviceID={deviceID}
            egresses={overview.egresses}
            sources={overview.sources}
            readOnly={readOnly}
            onChanged={reload}
            onCreate={() => setWizardEgress(null)}
            onEdit={(egress) => setWizardEgress(egress)}
          />

          <PolicyAccessCard
            deviceID={deviceID}
            access={overview.access}
            onChanged={reload}
          />

          {!readOnly ? (
            <div className="policy-bottom-actions">
              <button type="button" className="pill pill--pad-sm" onClick={() => setTakeoverOpen(true)}>
                接管现有配置
              </button>
              <PolicyMoreMenu label="更多操作" items={[{ label: '审计记录', onClick: () => setAuditOpen(true) }]} />
              <button type="button" className="primary-button" disabled={generatingPlan || overview.egresses.length === 0} onClick={() => { void handleGeneratePlan() }}>
                {generatingPlan ? '生成计划中…' : '应用设置'}
              </button>
            </div>
          ) : null}
        </>
      ) : (
        <PolicySourcesPage
          deviceID={deviceID}
          overview={overview}
          sources={overview.sources}
          egresses={overview.egresses}
          readOnly={readOnly}
          onChanged={reload}
        />
      )}

      {wizardEgress !== undefined ? (
        <PolicyWizard
          deviceID={deviceID}
          overview={overview}
          discovery={discovery}
          egress={wizardEgress ?? undefined}
          onClose={() => setWizardEgress(undefined)}
          onChanged={reload}
          onApplied={handleApplied}
        />
      ) : null}

      {auditOpen ? (
        <PolicyAuditDialog deviceID={deviceID} onClose={() => setAuditOpen(false)} />
      ) : null}

      {takeoverOpen && discovery ? (
        <PolicyTakeoverModal
          deviceID={deviceID}
          discovery={discovery}
          onClose={() => setTakeoverOpen(false)}
          onApplied={handleApplied}
        />
      ) : null}

      {takeoverOpen && !discovery ? (
        <PolicyModal title="接管现有配置" onClose={() => setTakeoverOpen(false)}>
          <PolicyPreparing text="正在执行只读发现…" />
        </PolicyModal>
      ) : null}

      {applyPlan ? (
        <ApplyPlanModal
          deviceID={deviceID}
          envelope={applyPlan}
          onClose={() => setApplyPlan(null)}
          onApplied={handleApplied}
        />
      ) : null}
    </div>
  )
}

function ApplyPlanModal({
  deviceID,
  envelope,
  onClose,
  onApplied,
}: {
  deviceID: string
  envelope: PolicyPlanEnvelope
  onClose: () => void
  onApplied: (result?: PolicyApplyResult) => void
}) {
  return (
    <div className="policy-modal-backdrop" onClick={(e) => { if (e.target === e.currentTarget) onClose() }}>
    <div className="policy-modal policy-modal--wide">
        <div className="policy-modal-head">
          <div>
            <h3>应用设置</h3>
            <p className="policy-modal-subtitle">预览并确认 RouterOS 差异；计划在实际状态指纹变化或过期后失效，需要重新生成。</p>
          </div>
          <button type="button" className="policy-modal-close" onClick={onClose}>×</button>
        </div>
        <div className="policy-modal-body">
          <ChangePlanView
            deviceID={deviceID}
            plan={envelope.plan}
            onApplied={onApplied}
            onCancel={onClose}
          />
        </div>
      </div>
    </div>
  )
}
