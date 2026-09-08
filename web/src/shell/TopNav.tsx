import { Badge } from '../ui/Badge'
import { StatusDot } from '../ui/StatusDot'
import { formatRelativeTime } from '../lib/format'
import type { AlertEvent } from '../lib/types'
import { useShell } from './useShell'
import { Popover } from './Popover'
import { NAV_GROUPS, NAV_ITEM_META, TOP_LEVEL_NAV, VIEW_TITLES, navGroupOf } from './views'

function DevicePill() {
  const { devices, selectedDeviceId, selectDevice, navigate } = useShell()
  const available = devices.filter((device) => device.enabled && !device.archived)
  const current = available.find((device) => device.id === selectedDeviceId)
  return (
    <Popover ariaLabel="切换设备" width={260} trigger={(open, toggle) => (
      <button
        type="button"
        className="devpill"
        onClick={toggle}
        aria-expanded={open}
        aria-haspopup="menu"
        title={current ? `${current.name}${current.error ? `：${current.error}` : ''}` : '未选择设备'}
      >
        <StatusDot tone={current ? (current.healthy ? 'ok' : 'err') : 'neutral'} />
        <span className="devpill-name">{current ? current.name : '选择设备'}</span>
        <span className="devpill-state">{current ? (current.healthy ? '在线' : '离线') : ''}</span>
        <span aria-hidden="true" className="devpill-chevron">▾</span>
      </button>
    )}>
      {available.map((device) => (
        <button key={device.id} type="button" className="popover-item" role="menuitem" onClick={() => selectDevice(device.id)}>
          <StatusDot tone={device.healthy ? 'ok' : 'err'} />
          <span>
            {device.name}
            {device.id === selectedDeviceId ? ' ✓' : ''}
            <br />
            <small>{[device.routerName, device.version ? `v${device.version}` : ''].filter(Boolean).join(' · ') || device.id}</small>
          </span>
        </button>
      ))}
      {available.length === 0 ? <span className="popover-item faint">暂无可用设备</span> : null}
      <hr className="divider" />
      <button type="button" className="popover-item" role="menuitem" onClick={() => navigate('fleet')}>
        <span aria-hidden="true">▦</span>
        <span>设备总览</span>
      </button>
    </Popover>
  )
}

function AlertsBell() {
  const { alerts, warnings } = useShell()
  const count = Math.max(alerts.length, warnings.length)
  const renderAlert = (alert: AlertEvent) => (
    <span key={alert.id} className="popover-item alert-item" role="menuitem">
      <StatusDot tone={alert.level === 'error' ? 'err' : 'warn'} />
      <span>
        {alert.message}
        <br />
        <small>{alert.source ? `${alert.source} · ` : ''}{formatRelativeTime(alert.timestamp)}</small>
      </span>
    </span>
  )
  return (
    <Popover ariaLabel="告警" width={320} trigger={(open, toggle) => (
      <button type="button" className="icon-btn" onClick={toggle} aria-expanded={open} aria-haspopup="menu" aria-label={`告警，${count} 条未读`}>
        🔔
        {count > 0 ? <span className="icon-btn-count">{count > 99 ? '99+' : count}</span> : null}
      </button>
    )}>
      {alerts.length === 0 && warnings.length === 0 ? (
        <span className="popover-item faint">暂无告警，一切正常</span>
      ) : (
        <>
          {warnings.map((warning) => (
            <span key={warning} className="popover-item alert-item" role="menuitem">
              <StatusDot tone="warn" />
              <span>{warning}</span>
            </span>
          ))}
          {alerts.map(renderAlert)}
        </>
      )}
    </Popover>
  )
}

function RefreshControl() {
  const { refreshMs, setRefreshMs, requestReload } = useShell()
  return (
    <span className="refresh-control">
      <button type="button" className="icon-btn" aria-label="立即刷新" title="立即刷新" onClick={requestReload}>
        ⟳
      </button>
      <select
        className="select refresh-select"
        aria-label="自动刷新间隔"
        value={String(refreshMs)}
        onChange={(event) => setRefreshMs(Number(event.target.value))}
      >
        <option value="0">停止刷新</option>
        <option value="1000">1 秒刷新</option>
        <option value="3000">3 秒刷新</option>
        <option value="5000">5 秒刷新</option>
        <option value="10000">10 秒刷新</option>
      </select>
    </span>
  )
}

/** Top glass bar (§6): logo, center pill nav, device pill, bell, theme, settings. */
export function TopNav() {
  const { view, navigate, theme, toggleTheme } = useShell()
  return (
    <header className="topnav glass">
      <span className="logo" aria-hidden="true">R</span>
      <b className="brand">rosboard</b>
      <nav className="pill-nav" aria-label="主导航">
        {TOP_LEVEL_NAV.map((item) => (
          <button key={item} type="button" className={view === item ? 'on' : undefined} onClick={() => navigate(item)}>
            {VIEW_TITLES[item]}
          </button>
        ))}
        {NAV_GROUPS.map((group) => {
          const active = navGroupOf(view) === group.key
          return (
            <Popover
              key={group.key}
              align="left"
              ariaLabel={group.label}
              width={470}
              trigger={(open, toggle) => (
                <button type="button" className={active || open ? 'on' : undefined} onClick={toggle} aria-expanded={open} aria-haspopup="menu">
                  {group.label} <span aria-hidden="true" className="nav-chevron">▾</span>
                </button>
              )}
            >
              <div className="nav-mega nav-mega-2col">
                {group.items.map((item) => {
                  const meta = NAV_ITEM_META[item]
                  const current = view === item
                  return (
                    <button key={item} type="button" role="menuitem" className={current ? 'mega-item mega-item-on' : 'mega-item'} onClick={() => navigate(item)}>
                      <span className="mega-item-ic" aria-hidden="true">{meta?.icon ?? '·'}</span>
                      <span className="mega-item-text">
                        <b>{VIEW_TITLES[item]}</b>
                        {meta ? <small>{meta.desc}</small> : null}
                      </span>
                      {current ? <Badge tone="accent">当前</Badge> : null}
                    </button>
                  )
                })}
              </div>
            </Popover>
          )
        })}
      </nav>
      <DevicePill />
      <AlertsBell />
      <RefreshControl />
      <button type="button" className="icon-btn" aria-label="切换主题" title={theme === 'dark' ? '切换到浅色主题' : '切换到深色主题'} onClick={() => toggleTheme()}>
        {theme === 'dark' ? '☀️' : '🌙'}
      </button>
      <button
        type="button"
        className="icon-btn"
        aria-label="面板设置"
        title="面板设置"
        aria-pressed={view === 'settings'}
        onClick={() => navigate('settings')}
      >
        ⚙️
      </button>
    </header>
  )
}
