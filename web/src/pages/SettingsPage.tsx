import { useEffect, useState } from 'react'
import { Card, Skeleton } from '../ui'
import {
  AccountSecurityForm,
  CollectionForm,
  DevicesSection,
  MaintenanceSection,
  RestartingBanner,
  UiPrefsForm,
  useRestartingAction,
  useSettings,
} from '../features/settings'
import './settings.css'

type SettingsSectionKey = 'devices' | 'collection' | 'ui' | 'account' | 'maintenance'

const SECTIONS: Array<{ key: SettingsSectionKey; label: string; icon: string }> = [
  { key: 'devices', label: '设备管理', icon: '📡' },
  { key: 'collection', label: '采集设置', icon: '⏱' },
  { key: 'ui', label: '界面设置', icon: '🎨' },
  { key: 'account', label: '账号安全', icon: '🛡' },
  { key: 'maintenance', label: '维护设置', icon: '🧰' },
]

const SETTINGS_HASH_RE = /^#\/settings(?:\/([a-z-]+))?$/

/** Hash-routed section (`#/settings/<key>`); unknown or missing key → devices. */
function sectionFromHash(): SettingsSectionKey {
  const key = SETTINGS_HASH_RE.exec(window.location.hash)?.[1]
  return SECTIONS.some((item) => item.key === key) ? (key as SettingsSectionKey) : 'devices'
}

/**
 * 面板设置 (§9.4): left glass sub-nav + right content; the sub-nav becomes
 * horizontal scroll pills below 768px. Device mutations flow through the
 * shared restart gate (saved → 等待面板重启 → reload).
 */
export default function SettingsPage() {
  const [section, setSection] = useState<SettingsSectionKey>(() => sectionFromHash())
  const { settings, loading, error, reload } = useSettings()
  const restartGate = useRestartingAction()

  // Browser back/forward and shell-written hashes drive the section too.
  useEffect(() => {
    const sync = () => setSection(sectionFromHash())
    window.addEventListener('popstate', sync)
    window.addEventListener('hashchange', sync)
    return () => {
      window.removeEventListener('popstate', sync)
      window.removeEventListener('hashchange', sync)
    }
  }, [])

  const selectSection = (next: SettingsSectionKey) => {
    setSection(next)
    window.history.pushState(null, '', `#/settings/${next}`)
  }

  const activeLabel = SECTIONS.find((item) => item.key === section)?.label ?? ''

  return (
    <div className="page settings-page">
      <header className="page-head">
        <h1>面板设置</h1>
        <span className="page-sub">{activeLabel}</span>
      </header>

      <div className="settings-layout">
        <nav className="settings-nav glass" aria-label="面板设置目录">
          {SECTIONS.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`settings-nav-item${section === item.key ? ' settings-nav-active' : ''}`}
              aria-current={section === item.key ? 'page' : undefined}
              onClick={() => selectSection(item.key)}
            >
              <span className="settings-nav-icon" aria-hidden="true">
                {item.icon}
              </span>
              <span>{item.label}</span>
            </button>
          ))}
        </nav>

        <div className="settings-content">
          <RestartingBanner gate={restartGate} />
          {error ? (
            <Card>
              <p className="form-error" role="alert">
                设置读取失败：{error}
              </p>
            </Card>
          ) : null}

          {loading && !settings ? (
            <Card>
              <Skeleton lines={5} height={14} />
            </Card>
          ) : null}

          {settings && section === 'devices' ? <DevicesSection settings={settings} restartGate={restartGate} onChanged={() => void reload()} /> : null}

          {settings && section === 'collection' ? (
            <Card title="采集设置" sub="修改后自动重启采集服务">
              <CollectionForm settings={settings.collection} restartGate={restartGate} />
            </Card>
          ) : null}

          {section === 'ui' ? (
            <Card title="界面设置" sub="仅影响当前浏览器">
              <UiPrefsForm />
            </Card>
          ) : null}

          {section === 'account' ? (
            <Card title="账号安全" sub="面板管理员账号">
              <AccountSecurityForm />
            </Card>
          ) : null}

          {settings && section === 'maintenance' ? <MaintenanceSection settings={settings} restartGate={restartGate} onChanged={() => void reload()} /> : null}
        </div>
      </div>
    </div>
  )
}
