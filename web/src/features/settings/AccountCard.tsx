import { useCallback, useEffect, useState } from 'react'
import { Badge, Button, CopyButton, Field, Input, Modal, Skeleton } from '../../ui'
import { errorMessage } from '../../lib/api'
import {
  completeOnboardingSession,
  createOnboardingSession,
  deleteDeviceAccount,
  fetchDeviceAccount,
  updateDevice,
  type DeviceAccountStatus,
  type MutationResult,
  type ProvisioningSession,
  type SettingsDevice,
} from './api'
import { emptyScopeOverrideDraft, scopeConfigsFromOverrides } from './drafts'

type AccountCardProps = {
  device: SettingsDevice
  /** Restarting-mutation runner owned by the parent section (§9.4). */
  onRestarting: (action: () => Promise<MutationResult>) => Promise<void>
  disabled?: boolean
}

/**
 * 接入账号设置 card: live permission read from RouterOS
 * (GET /api/devices/{id}/account), 手动更换账号 (direct credential update),
 * 一键更换账号 provisioning flow, and 清除已存账号 (DELETE …/account,
 * device is disabled afterwards).
 */
export function AccountCard({ device, onRestarting, disabled = false }: AccountCardProps) {
  const [account, setAccount] = useState<DeviceAccountStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [session, setSession] = useState<ProvisioningSession | null>(null)
  const [replaceOpen, setReplaceOpen] = useState(false)
  const [replaceBusy, setReplaceBusy] = useState(false)
  const [replaceError, setReplaceError] = useState<string | null>(null)
  const [clearOpen, setClearOpen] = useState(false)
  const [clearBusy, setClearBusy] = useState(false)
  const [clearError, setClearError] = useState<string | null>(null)
  const [manualOpen, setManualOpen] = useState(false)
  const [manualUsername, setManualUsername] = useState('')
  const [manualPassword, setManualPassword] = useState('')
  const [manualVisible, setManualVisible] = useState(false)
  const [manualBusy, setManualBusy] = useState(false)
  const [manualError, setManualError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fetchDeviceAccount(device.id)
      .then((status) => {
        if (!cancelled) setAccount(status)
      })
      .catch((error) => {
        if (!cancelled) {
          setAccount({ username: device.username, group: '', policies: [], permission: 'unknown', writeAccess: false, error: errorMessage(error, '读取权限失败') })
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [device.id, device.username])

  const openReplacement = useCallback(async () => {
    setReplaceOpen(true)
    setReplaceBusy(true)
    setReplaceError(null)
    setSession(null)
    try {
      setSession(await createOnboardingSession({ deviceId: device.id }))
    } catch (error) {
      setReplaceError(errorMessage(error, '生成一键更换脚本失败'))
    } finally {
      setReplaceBusy(false)
    }
  }, [device.id])

  const completeReplacement = async () => {
    if (!session) return
    setReplaceBusy(true)
    setReplaceError(null)
    try {
      const scopes = scopeConfigsFromOverrides(emptyScopeOverrideDraft)
      await onRestarting(() =>
        completeOnboardingSession(session.sessionId, {
          trafficScope: scopes.trafficScope,
          terminalScope: scopes.terminalScope,
          completeOnboarding: false,
          deferRestart: false,
        }),
      )
      // Success path restarts the panel and reloads the page.
    } catch (error) {
      setReplaceError(errorMessage(error, '更换账号失败'))
      setReplaceBusy(false)
    }
  }

  const clearAccount = async () => {
    setClearBusy(true)
    setClearError(null)
    try {
      await onRestarting(() => deleteDeviceAccount(device.id))
      // Success path restarts the panel and reloads the page.
    } catch (error) {
      setClearError(errorMessage(error, '删除设备账号失败'))
      setClearBusy(false)
    }
  }

  const openManual = () => {
    setManualUsername(account?.username || device.username || '')
    setManualPassword('')
    setManualError(null)
    setManualOpen(true)
  }

  const saveManual = async () => {
    if (!manualUsername.trim() || !manualPassword || manualBusy) return
    setManualBusy(true)
    setManualError(null)
    try {
      // 后端会以新凭据实际连接验证（prepareDevice 的 legacy verify path），
      // 保存成功即生效并重启面板。
      await onRestarting(() =>
        updateDevice(device.id, {
          name: device.name,
          enabled: device.enabled,
          scheme: device.scheme,
          host: device.host,
          port: device.port,
          username: manualUsername.trim(),
          password: manualPassword,
          trafficInterfaces: device.trafficInterfaces,
          trafficScope: device.trafficScope,
          terminalCidrs: device.terminalCidrs,
          terminalScope: device.terminalScope,
          deferRestart: false,
        }),
      )
      // Success path restarts the panel and reloads the page.
    } catch (error) {
      setManualError(errorMessage(error, '账号更换失败'))
      setManualBusy(false)
    }
  }

  const permissionBadge = loading ? (
    <Badge tone="neutral">读取中…</Badge>
  ) : account?.permission === 'write' ? (
    <Badge tone="ok" dot>
      可写
    </Badge>
  ) : account?.permission === 'read_only' ? (
    <Badge tone="warn" dot>
      只读
    </Badge>
  ) : (
    <Badge tone="err" dot>
      未知
    </Badge>
  )

  return (
    <section className="account-card" aria-label="接入账号设置">
      <div className="account-card-head">
        <h4>接入账号设置</h4>
        {permissionBadge}
      </div>
      {loading ? (
        <Skeleton lines={2} height={12} />
      ) : (
        <>
          <div className="account-card-grid">
            <div className="kv">
              <span>用户名</span>
              <b className="mono">{account?.username || device.username || '-'}</b>
            </div>
            <div className="kv">
              <span>用户组</span>
              <b className="mono">{account?.group || '-'}</b>
            </div>
            <div className="kv">
              <span>权限策略</span>
              <b className="mono">{account?.policies.length ? account.policies.join(', ') : '-'}</b>
            </div>
            <div className="kv">
              <span>写入权限</span>
              <b>{account?.writeAccess ? '具备（策略路由/访问控制可用）' : '缺失（仅监控可用）'}</b>
            </div>
          </div>
          {account?.error ? (
            <p className="form-error" role="alert">
              无法读取 RouterOS 账号权限：{account.error}
            </p>
          ) : null}
        </>
      )}
      <div className="account-card-actions">
        <Button size="sm" disabled={disabled || manualBusy} onClick={openManual}>
          手动更换账号
        </Button>
        <Button size="sm" disabled={disabled || replaceBusy} loading={replaceBusy && replaceOpen && !session} onClick={() => void openReplacement()}>
          一键更换账号
        </Button>
        <Button size="sm" variant="danger" disabled={disabled} onClick={() => setClearOpen(true)}>
          清除已存账号
        </Button>
      </div>

      <Modal open={manualOpen} persistent onClose={manualBusy ? () => undefined : () => setManualOpen(false)} title="手动更换账号">
        <p className="faint">直接填写 RouterOS 上已存在的账号与密码。保存时后端会实际连接验证，凭据正确才会保存并重启面板；验证失败不会改动现有配置。</p>
        <div className="form-grid">
          <Field label="REST 用户名">
            <Input value={manualUsername} onChange={setManualUsername} autoComplete="username" disabled={manualBusy} required />
          </Field>
          <Field label="REST 密码">
            <span className="password-field">
              <Input
                type={manualVisible ? 'text' : 'password'}
                value={manualPassword}
                onChange={setManualPassword}
                autoComplete="off"
                disabled={manualBusy}
                required
              />
              <button
                type="button"
                className="password-toggle"
                aria-label={manualVisible ? '隐藏密码' : '显示密码'}
                aria-pressed={manualVisible}
                title={manualVisible ? '隐藏密码' : '显示密码'}
                onClick={() => setManualVisible((visible) => !visible)}
              >
                {manualVisible ? '🙈' : '👁'}
              </button>
            </span>
          </Field>
        </div>
        {manualError ? (
          <p className="form-error" role="alert">
            {manualError}
          </p>
        ) : null}
        <div className="verify-actions">
          <Button disabled={manualBusy} onClick={() => setManualOpen(false)}>
            取消
          </Button>
          <Button variant="primary" disabled={!manualUsername.trim() || !manualPassword || manualBusy} loading={manualBusy} onClick={() => void saveManual()}>
            {manualBusy ? '正在验证并保存…' : '保存并重启'}
          </Button>
        </div>
      </Modal>

      <Modal open={replaceOpen} persistent onClose={replaceBusy ? () => undefined : () => setReplaceOpen(false)} title="一键更换 RouterOS 接入账号">
        <div className="replace-account">
          <p className="faint">脚本会创建具备 read、write、test、api、rest-api 权限的新账号，不授予用户管理权限。验证通过后 rosboard 会替换保存的凭据并重启。</p>
          {session ? (
            <>
              <div className="script-block">
                <textarea className="textarea script-area" readOnly value={session.script} rows={10} spellCheck={false} aria-label="更换账号脚本" />
                <div className="script-block-actions">
                  <CopyButton text={session.script} label="复制脚本" showText className="copy-prominent" />
                  <small className="faint">一键复制命令</small>
                  <small className="faint">脚本将在 {new Date(session.expiresAt).toLocaleString('zh-CN')} 过期</small>
                </div>
              </div>
              <p className="faint">在 RouterOS Terminal 以管理员身份执行成功后，点击下方「验证并更换账号」。</p>
            </>
          ) : replaceBusy ? (
            <Skeleton lines={3} height={14} />
          ) : null}
          {replaceError ? (
            <p className="form-error" role="alert">
              {replaceError}
            </p>
          ) : null}
          <div className="verify-actions">
            <Button disabled={replaceBusy} onClick={() => setReplaceOpen(false)}>
              取消
            </Button>
            <Button variant="primary" disabled={!session || replaceBusy} loading={replaceBusy && session !== null} onClick={() => void completeReplacement()}>
              {replaceBusy && session ? '正在验证…' : '验证并更换账号'}
            </Button>
          </div>
        </div>
      </Modal>

      <Modal open={clearOpen} persistent onClose={clearBusy ? () => undefined : () => setClearOpen(false)} title="清除已存账号">
        <p>
          删除 rosboard 保存的设备账号？设备 <strong>{device.name}</strong> 将同时停用，但不会删除 RouterOS 中的用户。
        </p>
        {clearError ? (
          <p className="form-error" role="alert">
            {clearError}
          </p>
        ) : null}
        <div className="verify-actions">
          <Button disabled={clearBusy} onClick={() => setClearOpen(false)}>
            取消
          </Button>
          <Button variant="danger" loading={clearBusy} onClick={() => void clearAccount()}>
            {clearBusy ? '正在删除…' : '确认清除'}
          </Button>
        </div>
      </Modal>
    </section>
  )
}
