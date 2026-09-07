import { Badge, Button, Modal } from '../../ui'
import type { VerificationResult } from './api'
import type { ScopeOverrideDraft } from './drafts'
import { ScopeEditor } from './ScopeEditor'

type VerificationDialogProps = {
  result: VerificationResult
  scopeDraft: ScopeOverrideDraft
  onScopeChange: (field: keyof ScopeOverrideDraft, value: string) => void
  scopePreviewing: boolean
  busy: boolean
  error: string | null
  confirmLabel: string
  busyLabel: string
  onCancel: () => void
  onConfirm: () => void
}

/**
 * Connection-verification result dialog (old VerificationDialog:2141):
 * identity card, WAN 上网线路, LAN 本地终端 + prefixes, raw detection
 * details, and the advanced scope override editors with live preview.
 */
export function VerificationDialog({
  result,
  scopeDraft,
  onScopeChange,
  scopePreviewing,
  busy,
  error,
  confirmLabel,
  busyLabel,
  onCancel,
  onConfirm,
}: VerificationDialogProps) {
  const wanInterfaces = result.trafficScope.interfaces
  const lanInterfaces = result.terminalScope.interfaces.filter((item) => item.role === 'lan')
  const prefixes = result.terminalScope.prefixes
  const warnings = Array.from(
    new Set(
      [
        ...result.warnings.map((item) => item.message),
        ...result.trafficScope.warnings,
        ...result.terminalScope.warnings,
      ].filter(Boolean),
    ),
  )
  const identity = result.identity
  const identityName = identity.routerName || identity.boardName || 'RouterOS 设备'
  const identityMeta = [identity.platform, identity.version].filter(Boolean).join(' · ')

  return (
    <Modal open onClose={busy ? () => undefined : onCancel} title="连接验证结果" maxWidth={720}>
      <div className="verify-dialog">
        <p className="faint">确认检测到的 WAN / LAN 范围后保存设备。</p>

        <div className="verify-identity">
          <div className="verify-identity-card">
            <span className="verify-label">RouterOS</span>
            <strong>{identityName}</strong>
            <small className="faint">{identityMeta || '身份信息不可用'}</small>
          </div>
          <div className="verify-identity-card">
            <span className="verify-label">识别结果</span>
            <strong className="num">
              {wanInterfaces.length} 条 WAN · {lanInterfaces.length} 个 LAN
            </strong>
            <small className="faint num">{prefixes.length} 个本地网段</small>
          </div>
        </div>

        <div className="verify-scope-grid">
          <section className="verify-scope-card" aria-label="WAN 上网线路">
            <div className="verify-scope-head">
              <h4>WAN / 上网线路</h4>
              <Badge tone={wanInterfaces.length ? 'accent' : 'warn'}>{wanInterfaces.length} 条</Badge>
            </div>
            {wanInterfaces.length ? (
              <div className="verify-scope-list">
                {wanInterfaces.map((item) => (
                  <div className="verify-scope-row" key={item.name}>
                    <span>
                      <strong>{item.name}</strong>
                      <small className="faint">
                        {item.kind} · {item.disabled ? '已禁用' : item.running ? '运行中' : '当前断开'}
                      </small>
                    </span>
                    {item.reasons.length ? <small className="verify-reason">{item.reasons.join('、')}</small> : null}
                  </div>
                ))}
              </div>
            ) : (
              <p className="verify-empty">未识别到 WAN；可在高级设置中强制纳入采集接口。</p>
            )}
          </section>

          <section className="verify-scope-card" aria-label="LAN 本地终端">
            <div className="verify-scope-head">
              <h4>LAN / 本地终端</h4>
              <Badge tone={lanInterfaces.length ? 'accent' : 'warn'}>{lanInterfaces.length} 个接口</Badge>
            </div>
            {lanInterfaces.length ? (
              <div className="verify-scope-list">
                {lanInterfaces.map((item) => (
                  <div className="verify-scope-row" key={item.name}>
                    <span>
                      <strong>{item.name}</strong>
                      <small className="faint">置信度：{item.confidence || '-'}</small>
                    </span>
                    {item.reasons.length ? <small className="verify-reason">{item.reasons.join('、')}</small> : null}
                  </div>
                ))}
              </div>
            ) : (
              <p className="verify-empty">未识别到 LAN 接口；可在高级设置中强制纳入。</p>
            )}
          </section>
        </div>

        {prefixes.length ? (
          <div className="verify-prefixes" aria-label="本地网段">
            {prefixes.map((prefix) => (
              <span className="verify-prefix" key={`${prefix.family}-${prefix.cidr}`}>
                <Badge tone={prefix.family === 'ipv6' ? 'accent' : 'neutral'}>{prefix.family === 'ipv6' ? 'IPv6' : 'IPv4'}</Badge>
                <span className="num">{prefix.cidr}</span>
                {prefix.interface ? <small className="faint">{prefix.interface}</small> : null}
              </span>
            ))}
          </div>
        ) : null}

        {result.interfaces.length || result.cidrCandidates.length ? (
          <details className="verify-details">
            <summary>查看原始检测结果</summary>
            {result.interfaces.length ? (
              <div className="verify-detail-block">
                <strong>RouterOS 接口</strong>
                <div className="verify-detail-list">
                  {result.interfaces.map((item) => (
                    <span key={item.name}>
                      {item.name} · {item.type}
                      {item.addresses.length ? ` · ${item.addresses.join(', ')}` : ''}
                    </span>
                  ))}
                </div>
              </div>
            ) : null}
            {result.cidrCandidates.length ? (
              <div className="verify-detail-block">
                <strong>CIDR 候选</strong>
                <div className="verify-detail-list">
                  {result.cidrCandidates.map((item) => (
                    <span key={`${item.family}-${item.cidr}-${item.interface}`}>
                      {item.cidr} · {item.interface || '未关联接口'}
                    </span>
                  ))}
                </div>
              </div>
            ) : null}
          </details>
        ) : null}

        {warnings.length ? (
          <div className="verify-warnings" role="status">
            {warnings.map((warning) => (
              <p key={warning}>⚠ {warning}</p>
            ))}
          </div>
        ) : null}

        <details className="verify-details verify-advanced">
          <summary>
            <span>
              <strong>高级设置</strong>
              <small className="faint">自动识别不符合实际拓扑时，添加或排除接口、CIDR</small>
            </span>
            <small className="verify-preview-state">{scopePreviewing ? '正在更新预览…' : '修改后自动预览'}</small>
          </summary>
          <ScopeEditor value={scopeDraft} onChange={onScopeChange} disabled={busy} />
        </details>

        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}

        <div className="verify-actions">
          <Button disabled={busy} onClick={onCancel}>
            返回修改
          </Button>
          <Button variant="primary" disabled={busy || scopePreviewing} loading={busy} onClick={onConfirm}>
            {busy ? busyLabel : confirmLabel}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
