import { useEffect, useRef, useState } from 'react'
import { Badge, Button, Field, Input, Select, Skeleton } from '../../ui'
import { ApiError, errorMessage } from '../../lib/api'
import {
  createDevice,
  fetchDeviceScope,
  previewScope,
  testConnection,
  updateDevice,
  type DeviceScopeSnapshot,
  type MutationResult,
  type SettingsDevice,
  type VerificationResult,
} from './api'
import {
  deviceFormDraft,
  emptyScopeOverrideDraft,
  scopeConfigsFromOverrides,
  scopeOverrideDraftFromDevice,
  type DeviceFormDraft,
  type ScopeOverrideDraft,
} from './drafts'
import { AccountCard } from './AccountCard'
import { ScopeEditor } from './ScopeEditor'
import { VerificationDialog } from './VerificationDialog'

type ManualAddWizardProps = {
  /** Present in edit mode; absent for 手动添加. */
  device?: SettingsDevice
  /** Parent restart gate: receives the save mutation and runs the §9.4 wait. */
  onSaved: (action: () => Promise<MutationResult>) => Promise<void>
  /** Edit mode: close the editor without saving. */
  onCancelEdit?: () => void
  /** Edit mode: archive request (parent owns the confirm + mutation). */
  onArchive?: (device: SettingsDevice) => void
  busy: boolean
}

type ProbeFeedback = { tone: 'pending' | 'success' | 'error'; message: string }

/**
 * 手动添加 wizard and device editor: connection form → test-connection →
 * VerificationDialog (identity, WAN/LAN, advanced overrides + live preview)
 * → save POST/PUT /api/devices. Edit mode adds the topology/scope readout,
 * the account permission card and the archive action.
 */
export function ManualAddWizard({ device, onSaved, onCancelEdit, onArchive, busy }: ManualAddWizardProps) {
  const editing = Boolean(device)
  const [form, setForm] = useState<DeviceFormDraft>(() => deviceFormDraft(device))
  const [scopeDraft, setScopeDraft] = useState<ScopeOverrideDraft>(() => (device ? scopeOverrideDraftFromDevice(device) : emptyScopeOverrideDraft))
  const [passwordVisible, setPasswordVisible] = useState(false)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)
  const [probe, setProbe] = useState<ProbeFeedback | null>(null)
  const [verification, setVerification] = useState<VerificationResult | null>(null)
  const [dialogError, setDialogError] = useState<string | null>(null)
  const [scopePreviewing, setScopePreviewing] = useState(false)
  const [scopeSnapshot, setScopeSnapshot] = useState<DeviceScopeSnapshot | null>(null)
  const [scopeLoading, setScopeLoading] = useState(false)
  const [scopeError, setScopeError] = useState<string | null>(null)
  const previewTimer = useRef<number | null>(null)
  const previewSequence = useRef(0)

  useEffect(
    () => () => {
      if (previewTimer.current !== null) window.clearTimeout(previewTimer.current)
    },
    [],
  )

  // Edit mode: load the runtime auto-detected scope for the readout.
  useEffect(() => {
    if (!device) return
    let cancelled = false
    setScopeLoading(true)
    setScopeError(null)
    fetchDeviceScope(device.id)
      .then((snapshot) => {
        if (!cancelled) setScopeSnapshot(snapshot)
      })
      .catch((loadError) => {
        if (!cancelled) setScopeError(errorMessage(loadError, '读取设备自动识别范围失败'))
      })
      .finally(() => {
        if (!cancelled) setScopeLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [device])

  const setField = (field: keyof DeviceFormDraft, value: string | number) => {
    setForm((current) => ({ ...current, [field]: value }))
    // Any connection change invalidates the previous verification result.
    setVerification(null)
    setDialogError(null)
    setProbe(null)
  }

  const changeScheme = (next: 'http' | 'https') => {
    setForm((current) => ({
      ...current,
      scheme: next,
      port: current.port === 80 || current.port === 443 ? (next === 'https' ? 443 : 80) : current.port,
    }))
    setVerification(null)
    setProbe(null)
  }

  const handleScopeChange = (field: keyof ScopeOverrideDraft, value: string) => {
    const next = { ...scopeDraft, [field]: value }
    setScopeDraft(next)
    const token = verification?.verificationToken
    if (!token) return
    if (previewTimer.current !== null) window.clearTimeout(previewTimer.current)
    const sequence = previewSequence.current + 1
    previewSequence.current = sequence
    setScopePreviewing(true)
    previewTimer.current = window.setTimeout(() => {
      void (async () => {
        try {
          const scopes = scopeConfigsFromOverrides(next)
          const previewResult = await previewScope({ verificationToken: token, ...scopes })
          if (previewSequence.current !== sequence) return
          setVerification((current) =>
            current ? { ...current, trafficScope: previewResult.trafficScope, terminalScope: previewResult.terminalScope } : current,
          )
          setDialogError(null)
        } catch (previewError) {
          if (previewSequence.current === sequence) setDialogError(errorMessage(previewError, '范围预览失败'))
        } finally {
          if (previewSequence.current === sequence) setScopePreviewing(false)
        }
      })()
    }, 350)
  }

  const verify = async () => {
    // 编辑模式凭据不可见：用户名来自已存配置，密码留空时后端回退使用已存密码
    // （resolveTestConnection 的 deviceId fallback）。
    const missing = editing ? !form.name.trim() || !form.host.trim() : !form.name.trim() || !form.host.trim() || !form.username.trim() || !form.password
    if (missing) {
      setProbe({ tone: 'error', message: editing ? '请填写设备名称与 IP 地址或主机名。' : '请填写设备名称、IP 地址或主机名、REST 用户名和密码。' })
      return
    }
    setTesting(true)
    setVerification(null)
    setDialogError(null)
    setProbe({ tone: 'pending', message: '正在检测 RouterOS 连接与拓扑…' })
    try {
      const scopes = scopeConfigsFromOverrides(scopeDraft)
      const result = await testConnection({
        deviceId: device?.id,
        scheme: form.scheme,
        host: form.host.trim(),
        port: form.port,
        username: form.username.trim(),
        password: form.password,
        trafficScope: scopes.trafficScope,
        terminalScope: scopes.terminalScope,
      })
      setVerification(result)
      const identity = [result.identity.routerName || result.identity.boardName || 'RouterOS 设备', result.identity.version].filter(Boolean).join(' ')
      setProbe({ tone: 'success', message: `检测完成：${identity}` })
    } catch (verifyError) {
      setProbe({ tone: 'error', message: `检测失败：${errorMessage(verifyError, 'RouterOS 连接测试失败')}` })
    } finally {
      setTesting(false)
    }
  }

  const confirm = async () => {
    if (!verification?.verificationToken) {
      setDialogError('请先完成连接检测，检测结果有效后才能保存。')
      return
    }
    setSaving(true)
    setDialogError(null)
    const scopes = scopeConfigsFromOverrides(scopeDraft)
    const payload = {
      name: form.name.trim(),
      enabled: device?.enabled ?? true,
      scheme: form.scheme,
      host: form.host.trim(),
      port: form.port,
      username: form.username.trim(),
      password: form.password,
      trafficInterfaces: [],
      trafficScope: scopes.trafficScope,
      terminalCidrs: [],
      terminalScope: scopes.terminalScope,
      verificationToken: verification.verificationToken,
      completeOnboarding: false,
      deferRestart: false,
    }
    try {
      await onSaved(() => (device ? updateDevice(device.id, payload) : createDevice(payload)))
      // Success path restarts the panel and reloads the page.
    } catch (saveError) {
      if (saveError instanceof ApiError && saveError.code === 'verification_required') {
        setDialogError('检测结果已过期，请返回重新检测。')
      } else {
        setDialogError(errorMessage(saveError, '设备设置保存失败'))
      }
      setSaving(false)
    }
  }

  const snapshotTraffic = scopeSnapshot?.trafficScope
  const snapshotTerminal = scopeSnapshot?.terminalScope
  const snapshotLan = snapshotTerminal?.interfaces.filter((item) => item.role === 'lan') ?? []
  const snapshotPrefixes = snapshotTerminal?.prefixes ?? []

  return (
    <div className="manual-wizard">
      <form
        className="manual-form"
        onSubmit={(event) => {
          event.preventDefault()
          void verify()
        }}
      >
        <div className="form-grid form-grid-four">
          <Field label="设备名称">
            <Input value={form.name} onChange={(value) => setField('name', value)} placeholder="例如：主路由" required maxLength={64} />
          </Field>
          <Field label="连接协议">
            <Select
              value={form.scheme}
              onChange={(value) => changeScheme(value === 'https' ? 'https' : 'http')}
              options={[
                { value: 'http', label: 'HTTP' },
                { value: 'https', label: 'HTTPS' },
              ]}
              ariaLabel="连接协议"
            />
          </Field>
          <Field label="IP 地址或主机名">
            <Input value={form.host} onChange={(value) => setField('host', value)} placeholder="10.0.0.1" required />
          </Field>
          <Field label="REST 端口">
            <Input type="number" min={1} max={65535} value={String(form.port)} onChange={(value) => setField('port', Number(value) || 0)} required />
          </Field>
        </div>
        {!editing ? (
          <div className="form-grid">
            <Field label="REST 用户名">
              <Input value={form.username} onChange={(value) => setField('username', value)} autoComplete="username" required />
            </Field>
            <Field label="REST 密码" hint="仅首次接入需要；保存后可在「接入账号设置」中更换。">
              <span className="password-field">
                <Input
                  type={passwordVisible ? 'text' : 'password'}
                  value={form.password}
                  onChange={(value) => setField('password', value)}
                  autoComplete="current-password"
                  required
                />
                <button
                  type="button"
                  className="password-toggle"
                  aria-label={passwordVisible ? '隐藏 RouterOS 密码' : '显示 RouterOS 密码'}
                  aria-pressed={passwordVisible}
                  title={passwordVisible ? '隐藏密码' : '显示密码'}
                  onClick={() => setPasswordVisible((visible) => !visible)}
                >
                  {passwordVisible ? '🙈' : '👁'}
                </button>
              </span>
            </Field>
          </div>
        ) : null}

        {editing && device ? (
          <>
            <section className="scope-readout" aria-label="拓扑与范围">
              <div className="scope-readout-head">
                <div>
                  <h4>拓扑与范围</h4>
                  <p className="faint">系统根据 RouterOS 拓扑自动识别采集线路、LAN 接口与终端网段。</p>
                </div>
                <Badge tone="accent">
                  {scopeLoading
                    ? '正在读取…'
                    : scopeError
                      ? '范围读取失败'
                      : `${snapshotTraffic?.interfaces.length ?? 0} 条上网线路 · ${snapshotLan.length} 个 LAN 接口 · ${snapshotPrefixes.length} 个网段`}
                </Badge>
              </div>
              {scopeLoading ? <Skeleton lines={3} height={12} /> : null}
              {scopeError ? (
                <p className="form-error" role="alert">
                  无法读取当前设备的自动识别范围：{scopeError}
                </p>
              ) : null}
              {!scopeLoading && !scopeError && scopeSnapshot ? (
                <div className="verify-scope-grid">
                  <section className="verify-scope-card" aria-label="上网线路">
                    <div className="verify-scope-head">
                      <h4>上网线路</h4>
                      <small className="faint">{snapshotTraffic?.interfaces.length ?? 0} 条线路</small>
                    </div>
                    {snapshotTraffic?.legacy ? <p className="scope-legacy-note">当前设备使用旧版手动采集接口配置；保存后将迁移为自动识别加覆盖模式。</p> : null}
                    {snapshotTraffic?.interfaces.length ? (
                      <div className="verify-scope-list">
                        {snapshotTraffic.interfaces.map((item) => (
                          <div className="verify-scope-row" key={item.name}>
                            <span>
                              <strong>{item.name}</strong>
                              <small className="faint">
                                {item.kind} · {item.disabled ? '已禁用' : item.running ? '运行中' : '当前断开，仍作为备用线路保留'}
                              </small>
                            </span>
                            {item.reasons.length ? <small className="verify-reason">{item.reasons.join('、')}</small> : null}
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="verify-empty">尚未识别上网线路；可在高级覆盖设置中强制纳入。</p>
                    )}
                    {(snapshotTraffic?.warnings ?? []).map((warning) => (
                      <p key={warning} className="verify-warning-line">
                        ⚠ {warning}
                      </p>
                    ))}
                  </section>
                  <section className="verify-scope-card" aria-label="本地终端">
                    <div className="verify-scope-head">
                      <h4>本地终端</h4>
                      <small className="faint">
                        {snapshotLan.length} 个接口 · {snapshotPrefixes.length} 个网段
                      </small>
                    </div>
                    {snapshotTerminal?.legacy ? <p className="scope-legacy-note">当前设备使用旧版手动终端网段配置。</p> : null}
                    {snapshotLan.length ? (
                      <div className="verify-scope-list">
                        {snapshotLan.map((item) => (
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
                      <p className="verify-empty">尚未识别 LAN 接口。</p>
                    )}
                    {snapshotPrefixes.length ? (
                      <div className="verify-prefixes">
                        {snapshotPrefixes.map((prefix) => (
                          <span className="verify-prefix" key={`${prefix.family}-${prefix.cidr}`}>
                            <Badge tone={prefix.family === 'ipv6' ? 'accent' : 'neutral'}>{prefix.family === 'ipv6' ? 'IPv6' : 'IPv4'}</Badge>
                            <span className="num">{prefix.cidr}</span>
                            <small className="faint">
                              {prefix.interface || '手动'} · {prefix.source}
                            </small>
                          </span>
                        ))}
                      </div>
                    ) : null}
                    {(snapshotTerminal?.warnings ?? []).map((warning) => (
                      <p key={warning} className="verify-warning-line">
                        ⚠ {warning}
                      </p>
                    ))}
                  </section>
                </div>
              ) : null}
            </section>

            <details className="verify-details">
              <summary>
                <span>
                  <strong>高级覆盖设置</strong>
                  <small className="faint">仅用于特殊网络拓扑，留空即使用自动识别</small>
                </span>
              </summary>
              <ScopeEditor value={scopeDraft} onChange={handleScopeChange} disabled={busy || testing || saving} />
            </details>

            <AccountCard device={device} onRestarting={onSaved} disabled={busy || testing || saving} />
          </>
        ) : null}

        {probe ? (
          <p className={`probe-feedback probe-${probe.tone}`} role={probe.tone === 'error' ? 'alert' : 'status'} aria-live="polite">
            {probe.message}
          </p>
        ) : (
          <p className="form-hint">点击「{editing ? '保存设备' : '添加设备'}」后会自动检测连接和 LAN/WAN 范围，确认无误才写入。</p>
        )}

        <div className="form-actions">
          {editing && device && onArchive ? (
            <Button variant="danger" disabled={busy || testing || saving} onClick={() => onArchive(device)}>
              归档设备
            </Button>
          ) : null}
          <span className="form-actions-spacer" />
          {editing && onCancelEdit ? (
            <Button disabled={busy || testing || saving} onClick={onCancelEdit}>
              取消
            </Button>
          ) : null}
          <Button type="submit" variant="primary" disabled={busy || testing || saving} loading={testing}>
            {testing ? '正在检测…' : editing ? '保存设备' : '添加设备'}
          </Button>
        </div>
      </form>

      {verification ? (
        <VerificationDialog
          result={verification}
          scopeDraft={scopeDraft}
          onScopeChange={handleScopeChange}
          scopePreviewing={scopePreviewing}
          busy={saving || busy}
          error={dialogError}
          confirmLabel="确认保存并启动采集"
          busyLabel="正在保存并启动…"
          onCancel={() => {
            if (!saving && !busy) {
              setVerification(null)
              setDialogError(null)
            }
          }}
          onConfirm={() => void confirm()}
        />
      ) : null}
    </div>
  )
}
