import { VIEW_TITLES, type View } from '../../shell/views'
import { UI_OPTIONS, isUiVariant, switchUi, savePanelRecord, type UiVariant } from '../../uiPreference'
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
  const [uiDraft, setUiDraft] = useState<UiVariant>('aurora')
  const { theme, refreshMs, setRefreshMs } = useShell()
  const [prefs, setPrefs] = useState(loadPanelPreferences)
  const [themeChoice, setThemeChoice] = useState<ThemeChoice>(() => readThemeChoice())
  const [refreshChoice, setRefreshChoice] = useState(refreshMs)

  const save = () => {
    applyThemeChoice(themeChoice)
    setRefreshMs(refreshChoice)
    savePanelPreferences(prefs)
    if (uiDraft !== 'aurora') {
      const effective = document.documentElement.dataset.theme
      savePanelRecord({ theme: effective === 'dark' ? 'dark' : 'light' })
      switchUi(uiDraft)
    } else toast('界面设置已保存')
  }

  return (
    <form
      className="ui-prefs-form"
      onSubmit={(event) => {
        event.preventDefault()
        save()
      }}
    >
      <Field label="UI 风格" hint="切换将重新加载完整界面，请先保存其他编辑。选择仅对当前浏览器生效，设备与业务数据保持共享。">
        <Select value={uiDraft} onChange={(value) => { if (isUiVariant(value)) setUiDraft(value) }} options={[...UI_OPTIONS]} ariaLabel="UI 风格" />
      </Field>
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
            options={(Object.keys(VIEW_TITLES) as View[]).map((value) => ({ value, label: VIEW_TITLES[value] }))}
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
          {uiDraft === 'aurora' ? '保存界面设置' : '保存并切换 UI'}
        </Button>
      </div>
    </form>
  )
}
