import { useState } from 'react'
import { Button, Field, SegTabs, Select, toast } from '../../ui'
import { useShell } from '../../shell/useShell'
import { REFRESH_OPTIONS } from '../../shell/shellContext'
import {
  applyThemeChoice,
  loadPanelPreferences,
  readThemeChoice,
  savePanelPreferences,
  type DefaultTerminalFamily,
  type LandingView,
  type ThemeChoice,
} from './prefs'

/**
 * 界面设置: theme (shell-owned), default refresh interval (shell-owned),
 * landing view and default terminal family (rosboard:panel-preferences).
 * Nothing here touches RouterOS credentials or server state.
 */
export function UiPrefsForm() {
  const { theme, refreshMs, setRefreshMs } = useShell()
  const [prefs, setPrefs] = useState(loadPanelPreferences)
  const [themeChoice, setThemeChoice] = useState<ThemeChoice>(() => readThemeChoice())
  const [refreshChoice, setRefreshChoice] = useState(refreshMs)

  const save = () => {
    applyThemeChoice(themeChoice)
    setRefreshMs(refreshChoice)
    savePanelPreferences(prefs)
    toast('界面设置已保存')
  }

  return (
    <form
      className="ui-prefs-form"
      onSubmit={(event) => {
        event.preventDefault()
        save()
      }}
    >
      <div className="form-grid form-grid-three">
        <Field label="主题" hint={`当前生效：${theme === 'dark' ? '深色' : '浅色'}`}>
          <SegTabs<ThemeChoice>
            ariaLabel="主题"
            value={themeChoice}
            onChange={(value) => {
              setThemeChoice(value)
              // Live preview — persisted only through 保存界面设置.
              applyThemeChoice(value)
            }}
            options={[
              { value: 'system', label: '跟随系统' },
              { value: 'dark', label: '深色' },
              { value: 'light', label: '浅色' },
            ]}
          />
        </Field>
        <Field label="默认自动刷新" hint="监控页面的数据轮询周期">
          <Select
            value={String(refreshChoice)}
            onChange={(value) => {
              const next = Number(value)
              setRefreshChoice(next)
              setRefreshMs(next)
            }}
            options={REFRESH_OPTIONS.map((option) => ({ value: String(option.value), label: option.label }))}
            ariaLabel="默认自动刷新"
          />
        </Field>
        <Field label="默认打开页面" hint="进入面板时首先显示的页面">
          <Select
            value={prefs.landingView}
            onChange={(value) => setPrefs((current) => ({ ...current, landingView: value as LandingView }))}
            options={[
              { value: 'overview', label: '系统概览' },
              { value: 'fleet', label: '仪表台' },
            ]}
            ariaLabel="默认打开页面"
          />
        </Field>
        <Field label="默认终端范围" hint="终端监控默认展示的协议族">
          <Select
            value={prefs.terminalFamily}
            onChange={(value) => setPrefs((current) => ({ ...current, terminalFamily: value as DefaultTerminalFamily }))}
            options={[
              { value: 'all', label: '全部终端' },
              { value: 'ipv4', label: 'IPv4' },
              { value: 'ipv6', label: 'IPv6' },
            ]}
            ariaLabel="默认终端范围"
          />
        </Field>
      </div>
      <p className="form-hint">主题与刷新间隔即时生效；默认打开页面与终端范围将在下次进入对应页面时生效。</p>
      <div className="form-actions">
        <Button type="submit" variant="primary">
          保存界面设置
        </Button>
      </div>
    </form>
  )
}
