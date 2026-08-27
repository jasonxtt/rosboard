import { useState } from 'react'
import type { PolicyAccess, PolicyProvisionSession } from './types'
import { completeProvisionSession, createProvisionSession, fetchCleanupInfo, saveAccess } from './api'
import {
  PolicyCopyButton,
  PolicyErrorDisplay,
  PolicyField,
  PolicyIcon,
  PolicyMetadata,
  PolicyNotice,
  PolicyPasswordInput,
  PolicyStatusBadge,
  type StatusTone,
} from './components'

type Mode = 'overview' | 'script' | 'manual' | 'remove'

export function PolicyAccessCard({
  deviceID,
  access,
  onChanged,
}: {
  deviceID: string
  access: PolicyAccess
  onChanged: () => void
}) {
  const [mode, setMode] = useState<Mode>('overview')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)

  // script mode state
  const [session, setSession] = useState<PolicyProvisionSession | null>(null)
  const [cleanupScript, setCleanupScript] = useState('')

  // manual mode state
  const [manualUsername, setManualUsername] = useState('')
  const [manualPassword, setManualPassword] = useState('')
  const [adminPassword, setAdminPassword] = useState('')

  // remove mode state
  const [removeConfirm, setRemoveConfirm] = useState(false)
  const [removeAdminPassword, setRemoveAdminPassword] = useState('')

  const handleScriptMode = async () => {
    setMode('script')
    setError(null)
    setSaving(true)
    try {
      const s = await createProvisionSession(deviceID)
      setSession(s)
    } catch (e) {
      setError(e instanceof Error ? e.message : '生成脚本失败')
    } finally {
      setSaving(false)
    }
  }

  const handleCompleteSession = async () => {
    if (!session) return
    setError(null)
    setSaving(true)
    try {
      await completeProvisionSession(deviceID, session.sessionId, adminPassword)
      setMessage('策略访问账号已验证，面板正在重启以加载策略运行时。')
      setMode('overview')
      onChanged()
    } catch (e) {
      setError(e instanceof Error ? e.message : '验证失败')
    } finally {
      setSaving(false)
    }
  }

  const handleManualSave = async () => {
    setError(null)
    setSaving(true)
    try {
      await saveAccess(deviceID, { enabled: true, username: manualUsername, password: manualPassword, adminPassword })
      setMessage('策略访问账号已保存，面板正在重启以加载策略运行时。')
      setMode('overview')
      onChanged()
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleRemoveMode = async () => {
    setMode('remove')
    setError(null)
    setRemoveConfirm(false)
    setRemoveAdminPassword('')
    try {
      const info = await fetchCleanupInfo(deviceID)
      setCleanupScript(info.script)
    } catch (e) {
      setError(e instanceof Error ? e.message : '获取清理脚本失败')
    }
  }

  const handleRemoveAccess = async () => {
    setError(null)
    setSaving(true)
    try {
      await saveAccess(deviceID, { enabled: false, username: '', password: '', adminPassword: removeAdminPassword })
      setMessage('策略访问已关闭。')
      setMode('overview')
      onChanged()
    } catch (e) {
      setError(e instanceof Error ? e.message : '关闭失败')
    } finally {
      setSaving(false)
    }
  }

  const accessTone: StatusTone = access.enabled ? 'good' : 'warn'

  return (
    <section className="panel policy-panel" aria-label="策略管理账号接入">
      <div className="policy-panel-head">
        <h3>策略管理账号</h3>
        <PolicyStatusBadge tone={accessTone}>{access.enabled ? '已启用' : '未配置'}</PolicyStatusBadge>
      </div>
      <div className="policy-panel-body">
        {message ? <PolicyNotice tone="good">{message}</PolicyNotice> : null}
        {error ? <PolicyErrorDisplay error={error} /> : null}

        {mode === 'overview' ? (
          <div className="policy-access-overview">
            {access.enabled ? (
              <div className="policy-access-info">
                <dl className="policy-meta">
                  <div><dt>账号</dt><dd><span className="mono">{access.username || '-'}</span></dd></div>
                  <div><dt>来源</dt><dd>{access.managed ? 'rosboard 托管（最小权限）' : '用户提供的现有账号'}</dd></div>
                  <div><dt>密码</dt><dd>{access.passwordSet ? '已保存（不回显）' : '未保存'}</dd></div>
                </dl>
                <p className="policy-hint">策略管理账号用于让 rosboard 管理 RouterOS 中的策略路由，需要具备相应的写入权限（仅 read,write,test,api,rest-api），与只读监控账号相互独立。凭据保存在权限 0600 的本地配置中，不通过 API 回显、不落日志、不写入浏览器存储。</p>
                <div className="policy-form-actions">
                  <button type="button" className="pill pill--pad-sm" onClick={() => { setManualUsername(access.username); setManualPassword(''); setAdminPassword(''); setMode('manual') }}>更换账号</button>
                  <button type="button" className="link-button link-button--danger" onClick={handleRemoveMode}>删除账号</button>
                </div>
              </div>
            ) : (
              <div className="policy-access-setup">
                <p className="policy-hint">策略路由需要一个独立的 RouterOS 账号来执行写入操作。该账号不能与面板监控账号相同。</p>
                <div className="policy-form-actions">
                  <button type="button" className="primary-button" disabled={saving} onClick={handleScriptMode}>
                    {saving ? '生成中…' : '生成初始化脚本'}
                  </button>
                  <button type="button" className="toolbar-button" onClick={() => { setManualUsername(access.username); setManualPassword(''); setAdminPassword(''); setMode('manual') }}>
                    新建账号（使用现有账号）
                  </button>
                </div>
              </div>
            )}
          </div>
        ) : null}

        {mode === 'script' ? (
          <div className="policy-provision-session">
            {session ? (
              <>
                <PolicyMetadata entries={[
                  ['用户名', session.username],
                  ['过期时间', session.expiresAt],
                ]} />
              </>
            ) : <PolicyIcon name="refresh" />}
            {session ? (
              <>
                <div className="policy-script">
                  <pre>{session.script}</pre>
                  <PolicyCopyButton text={session.script} label="复制脚本" />
                </div>
                <p className="policy-hint">请在 RouterOS 中执行上述脚本以创建托管账号；完成后输入 RouterOS 管理员密码进行验证。脚本中的账号密码不会回传到面板。</p>
                <div className="policy-form-grid">
                  <PolicyField label="管理员密码">
                    <PolicyPasswordInput value={adminPassword} onChange={setAdminPassword} className="policy-manual-admin-password" placeholder="确认操作" />
                  </PolicyField>
                </div>
                <div className="policy-form-actions">
                  <button type="button" className="primary-button" disabled={saving || !adminPassword} onClick={handleCompleteSession}>
                    {saving ? '验证中…' : '验证并启用'}
                  </button>
                  <button type="button" className="toolbar-button" onClick={() => { setMode('overview'); setSession(null) }}>取消</button>
                </div>
              </>
            ) : null}
          </div>
        ) : null}

        {mode === 'manual' ? (
          <div className="policy-access-manual">
              <p className="policy-hint">输入一个已有的 RouterOS 账号。该账号需要写入策略路由相关配置的权限；管理员密码只用于验证本次变更。</p>
            <div className="policy-form-grid">
              <PolicyField label="用户名">
                <input className="settings-input" value={manualUsername} onChange={(e) => setManualUsername(e.target.value)} placeholder="策略账号用户名" />
              </PolicyField>
              <PolicyField label="密码">
                <PolicyPasswordInput value={manualPassword} onChange={setManualPassword} className="policy-manual-password" placeholder="策略账号密码" />
              </PolicyField>
              <PolicyField label="管理员密码">
                <PolicyPasswordInput value={adminPassword} onChange={setAdminPassword} className="policy-manual-admin-password" placeholder="确认操作" />
              </PolicyField>
            </div>
            <div className="policy-form-actions">
              <button type="button" className="primary-button" disabled={saving || !manualUsername || !manualPassword || !adminPassword} onClick={handleManualSave}>
                {saving ? '保存中…' : '保存'}
              </button>
              <button type="button" className="toolbar-button" onClick={() => setMode('overview')}>取消</button>
            </div>
          </div>
        ) : null}

        {mode === 'remove' ? (
          <div className="policy-access-remove">
            {cleanupScript ? (
              <>
                <div className="policy-script">
                  <pre>{cleanupScript}</pre>
                  <PolicyCopyButton text={cleanupScript} label="复制清理脚本" />
                </div>
                <label className="policy-checkbox">
                  <input type="checkbox" checked={removeConfirm} onChange={(e) => setRemoveConfirm(e.target.checked)} />
                  <span>我已在 RouterOS 手工执行上述清理脚本，并确认删除策略账号</span>
                </label>
              </>
            ) : (
              <PolicyNotice tone="info">未返回清理脚本；关闭账号后，现有 RouterOS 策略对象不会自动删除。</PolicyNotice>
            )}
            <PolicyField label="管理员密码">
              <PolicyPasswordInput value={removeAdminPassword} onChange={setRemoveAdminPassword} className="policy-remove-admin-password" placeholder="确认操作" />
            </PolicyField>
            <div className="policy-form-actions">
              <button type="button" className="primary-button" disabled={saving || (Boolean(cleanupScript) && !removeConfirm) || !removeAdminPassword} onClick={handleRemoveAccess}>
                {saving ? '删除中…' : '确认删除账号'}
              </button>
              <button type="button" className="toolbar-button" onClick={() => setMode('overview')}>取消</button>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  )
}
