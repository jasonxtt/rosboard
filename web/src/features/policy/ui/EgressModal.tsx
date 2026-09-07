import { useEffect, useMemo, useState } from 'react'
import { Button } from '../../../ui/Button'
import { Field, Input, Select } from '../../../ui/inputs'
import { Modal } from '../../../ui/Modal'
import { Toggle } from '../../../ui/Toggle'
import { fetchPolicyDiscovery, saveEgress, type Egress, type EgressFamily, type PolicyDiscovery } from '../canonical'
import { gatewayCandidatesForWAN, suggestedGatewayForWAN } from '../gateway'
import { NEXT_HOP_VALUE, defaultEgressDraft, defaultEgressFamily, egressDraftErrors, egressDraftFrom } from './egressDraft'
import { errorMessage } from '../../../lib/api'
import { Notice } from './Notice'

/* ---------- shared form body (also used inline by the rule wizard) ---------- */

type EgressFieldsProps = {
  draft: Egress
  discovery: PolicyDiscovery | null
  onChange: (next: Egress) => void
  readOnly?: boolean
  /** name / priority / listMode editors */
  showIdentity?: boolean
}

export function EgressFields({ draft, discovery, onChange, readOnly = false, showIdentity = true }: EgressFieldsProps) {
  const patch = (partial: Partial<Egress>) => onChange({ ...draft, ...partial })
  const patchFamily = (family: EgressFamily) =>
    onChange({
      ...draft,
      families: draft.families.some((candidate) => candidate.family === family.family)
        ? draft.families.map((candidate) => (candidate.family === family.family ? family : candidate))
        : [...draft.families, family],
    })
  const enabledFamilies = draft.families.filter((family) => family.enabled)

  return (
    <div className="pol-egr-fields">
      {showIdentity ? (
        <div className="pol-form-grid">
          <Field label="出口名称" hint={draft.id ? '名称创建后不可修改' : '例如：电信 · PPPoE'}>
            <Input value={draft.name} onChange={(name) => patch({ name })} placeholder="给这条 WAN 线路起个名字" disabled={readOnly || Boolean(draft.id)} />
          </Field>
        </div>
      ) : null}
      {(['ipv4', 'ipv6'] as const).map((family) => (
        <FamilyEditor
          key={`${draft.id || 'new'}:${family}`}
          family={family}
          value={draft.families.find((candidate) => candidate.family === family) ?? defaultEgressFamily(family)}
          discovery={discovery}
          readOnly={readOnly}
          onChange={(next) => patchFamily(next)}
        />
      ))}
      <details className="pol-disclosure">
        <summary>高级设置（DNS、故障策略、路由表、NAT 与本机流量）</summary>
        <div className="pol-disclosure-body">
          <div className="pol-form-grid">
            <Field label="故障策略">
              <Select
                value={draft.failureMode || 'strict'}
                onChange={(failureMode) => patch({ failureMode })}
                disabled={readOnly}
                options={[
                  { value: 'strict', label: '断线阻断（严格）' },
                  { value: 'fallback', label: '回落 main 路由表' },
                  { value: 'existing', label: '沿用现有故障切换' },
                ]}
              />
            </Field>
            <Field label="DNS 上游" hint="协议族必须与启用的出口协议族一致">
              <Input value={draft.dnsUpstream} onChange={(dnsUpstream) => patch({ dnsUpstream })} placeholder="1.1.1.1" disabled={readOnly} />
            </Field>
            <Field label="Fake DNS 别名" hint="留空自动分配">
              <Input value={draft.fakeAlias} onChange={(fakeAlias) => patch({ fakeAlias })} placeholder="自动分配" disabled={readOnly} />
            </Field>
          </div>
          {enabledFamilies.map((family) => (
            <div className="pol-form-grid" key={`advanced-${family.family}`}>
              <Field label={`${family.family.toUpperCase()} 路由表`}>
                <Input value={family.routeTable} onChange={(routeTable) => patchFamily({ ...family, routeTable })} placeholder="自动创建专用表" disabled={readOnly} />
              </Field>
              <Field label={`${family.family.toUpperCase()} 断线覆盖`}>
                <Select
                  value={family.routeMode}
                  onChange={(routeMode) => patchFamily({ ...family, routeMode })}
                  disabled={readOnly}
                  options={[
                    { value: '', label: '跟随出口故障策略' },
                    { value: 'strict', label: '严格绑定' },
                    { value: 'fallback', label: '允许回落 main' },
                  ]}
                />
              </Field>
              <Field label={`${family.family.toUpperCase()} NAT 模式`} hint="RouterOS 现有外部 NAT 不会被覆盖">
                <Select
                  value={family.natMode}
                  onChange={(natMode) => patchFamily({ ...family, natMode })}
                  disabled={readOnly}
                  options={[
                    { value: '', label: '沿用现有 NAT' },
                    { value: 'none', label: '不建立 NAT' },
                    { value: 'masquerade', label: 'masquerade' },
                  ]}
                />
              </Field>
            </div>
          ))}
          <label className="pol-check">
            <input type="checkbox" checked={draft.routerOutput} disabled={readOnly} onChange={(event) => patch({ routerOutput: event.target.checked })} />
            <span>包含 RouterOS 本机流量</span>
          </label>
          {draft.id ? (
            <label className="pol-check">
              <input type="checkbox" checked={draft.enabled} disabled={readOnly} onChange={(event) => patch({ enabled: event.target.checked })} />
              <span>启用此出口</span>
            </label>
          ) : null}
        </div>
      </details>
    </div>
  )
}

type FamilyEditorProps = {
  family: 'ipv4' | 'ipv6'
  value: EgressFamily
  discovery: PolicyDiscovery | null
  readOnly: boolean
  onChange: (next: EgressFamily) => void
}

function FamilyEditor({ family, value, discovery, readOnly, onChange }: FamilyEditorProps) {
  const wans = discovery?.wans ?? []
  const selectedWAN = wans.find((wan) => wan.interface === value.wanInterface)
  const candidates = gatewayCandidatesForWAN(selectedWAN, family)
  const suggested = suggestedGatewayForWAN(selectedWAN, family)
  const pointToPoint = Boolean(selectedWAN?.pointToPoint)
  const nextHop = value.wanSource === 'next-hop'
  const [gatewayEdited, setGatewayEdited] = useState(() => Boolean(value.gateway.trim() && !pointToPoint && value.gateway !== suggested))

  useEffect(() => {
    setGatewayEdited(Boolean(value.gateway.trim() && !pointToPoint && value.gateway !== suggested))
  }, [pointToPoint, suggested, value.gateway, value.wanInterface, value.wanSource])

  useEffect(() => {
    if (readOnly || nextHop || gatewayEdited || !value.wanInterface.trim()) return
    const gateway = pointToPoint ? '' : suggested
    if (value.gateway !== gateway) onChange({ ...value, gateway })
    // eslint-disable-next-line react-hooks/exhaustive-deps -- derived autofill, mirrors old wizard behavior
  }, [gatewayEdited, nextHop, readOnly, suggested, pointToPoint, value.wanInterface])

  const selectInterface = (selected: string) => {
    if (selected === NEXT_HOP_VALUE) {
      setGatewayEdited(false)
      onChange({ ...value, wanSource: 'next-hop', wanInterface: '', gateway: '' })
      return
    }
    const nextWAN = wans.find((wan) => wan.interface === selected)
    setGatewayEdited(false)
    onChange({ ...value, wanSource: '', wanInterface: selected, gateway: suggestedGatewayForWAN(nextWAN, family) })
  }

  const gatewayRequired = value.enabled && (nextHop || Boolean(value.wanInterface.trim() && !pointToPoint))
  const gatewayHint = nextHop
    ? '下一跳模式必须显式填写同协议族的网关 IP。'
    : pointToPoint
      ? '点对点接口无需填写网关。'
      : suggested
        ? '已从 main 表活动默认路由自动填入，可手动修改。'
        : '未发现唯一的下一跳网关，必须手动填写 IP。'

  const wanOptions = [
    { value: '', label: '选择已发现接口' },
    ...(!wans.some((wan) => wan.interface === value.wanInterface) && value.wanInterface ? [{ value: value.wanInterface, label: `${value.wanInterface}（当前未发现）` }] : []),
    ...wans.map((wan) => ({ value: wan.interface, label: `${wan.interface}（${wan.type || '未知'}${wan.running ? '，运行中' : ''}）` })),
    { value: NEXT_HOP_VALUE, label: '下一跳网关（手动指定）' },
  ]

  return (
    <div className="pol-family">
      <div className="pol-family-head">
        <Toggle checked={value.enabled} disabled={readOnly} onChange={(enabled) => onChange({ ...value, enabled })} label={`启用 ${family.toUpperCase()}`} />
        <strong>{family.toUpperCase()}</strong>
        {pointToPoint && value.enabled ? <span className="faint">点对点</span> : null}
      </div>
      {value.enabled ? (
        <div className="pol-form-grid">
          <Field label="策略 WAN 接口">
            <Select value={nextHop ? NEXT_HOP_VALUE : value.wanInterface} onChange={selectInterface} disabled={readOnly} options={wanOptions} ariaLabel={`${family.toUpperCase()} WAN 接口`} />
          </Field>
          <Field label={`下一跳网关（${family.toUpperCase()}）`} hint={gatewayHint}>
            <input
              className="input"
              value={value.gateway}
              list={`pol-gateway-${family}`}
              disabled={readOnly}
              placeholder={gatewayRequired ? '填写网关 IP' : '点对点接口无需填写'}
              onChange={(event) => {
                setGatewayEdited(true)
                onChange({ ...value, gateway: event.target.value })
              }}
            />
            <datalist id={`pol-gateway-${family}`}>
              {candidates.map((gateway) => (
                <option key={gateway} value={gateway} />
              ))}
            </datalist>
            {gatewayEdited && !nextHop && suggested ? (
              <button
                type="button"
                className="link-button"
                onClick={() => {
                  setGatewayEdited(false)
                  onChange({ ...value, gateway: suggested })
                }}
              >
                恢复自动发现
              </button>
            ) : null}
            {gatewayRequired && !value.gateway.trim() ? <span className="pol-field-error">{nextHop ? '下一跳模式必须填写网关 IP。' : '未发现唯一网关，请填写下一跳 IP。'}</span> : null}
          </Field>
        </div>
      ) : null}
    </div>
  )
}

/* ---------- standalone modal ---------- */

type EgressModalProps = {
  deviceID: string
  /** null = 新建出口 */
  egress: Egress | null
  onClose: () => void
  onSaved: (egress: Egress) => void | Promise<void>
}

export function EgressModal({ deviceID, egress, onClose, onSaved }: EgressModalProps) {
  const [draft, setDraft] = useState<Egress>(() => (egress ? egressDraftFrom(egress) : defaultEgressDraft()))
  const [discovery, setDiscovery] = useState<PolicyDiscovery | null>(null)
  const [discoveryError, setDiscoveryError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    fetchPolicyDiscovery(deviceID)
      .then((value) => {
        if (!cancelled) setDiscovery(value)
      })
      .catch((loadError) => {
        if (!cancelled) setDiscoveryError(errorMessage(loadError, '设备发现读取失败'))
      })
    return () => {
      cancelled = true
    }
  }, [deviceID])

  const errors = useMemo(() => egressDraftErrors(draft, discovery, true), [draft, discovery])
  const submit = async () => {
    if (errors.length || saving) return
    setSaving(true)
    setError(null)
    try {
      const saved = await saveEgress(deviceID, { ...draft, name: draft.name.trim() })
      await onSaved(saved)
    } catch (saveError) {
      setError(errorMessage(saveError, '出口保存失败'))
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      persistent
      onClose={() => {
        if (!saving) onClose()
      }}
      title={egress ? `编辑出口：${egress.name}` : '添加出口'}
      maxWidth={720}
      footer={
        <>
          <Button disabled={saving} onClick={onClose}>
            取消
          </Button>
          <Button variant="primary" loading={saving} disabled={errors.length > 0} onClick={() => void submit()}>
            保存出口
          </Button>
        </>
      }
    >
      {discoveryError ? (
        <Notice tone="warn" title="设备发现不可用">
          {discoveryError}；仍可手动填写接口与网关。
        </Notice>
      ) : null}
      {discovery && !discovery.available ? (
        <Notice tone="warn" title="设备发现不可用">
          {discovery.reason || '无法读取 RouterOS WAN 候选。'}仍可手动填写接口与网关。
        </Notice>
      ) : null}
      {error ? <Notice tone="err">{error}</Notice> : null}
      {errors.length ? (
        <Notice tone="warn" title="完成后才能保存">
          {errors.join('；')}
        </Notice>
      ) : null}
      <EgressFields draft={draft} discovery={discovery} onChange={setDraft} readOnly={saving} />
    </Modal>
  )
}
