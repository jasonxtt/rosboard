import { useState } from 'react'
import { Modal } from '../ui/Modal'
import { StatusDot } from '../ui/StatusDot'
import { useShell } from './useShell'
import { MORE_NAV, MOBILE_NAV, VIEW_TITLES, type View } from './views'

/** Mobile (<768px) bottom floating pill: 概览/接口/终端/策略 + 更多 drawer (§11). */
export function MobileNav() {
  const { view, navigate, selectedDeviceId, devices } = useShell()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const current = devices.find((device) => device.id === selectedDeviceId)
  const moreActive = MORE_NAV.includes(view)

  const go = (next: View) => {
    setDrawerOpen(false)
    navigate(next)
  }

  return (
    <>
      <nav className="bottomnav glass" aria-label="移动端主导航">
        {MOBILE_NAV.map((item) => (
          <button key={item} type="button" className={view === item ? 'on' : undefined} onClick={() => navigate(item)}>
            {VIEW_TITLES[item]}
          </button>
        ))}
        <button type="button" className={moreActive || drawerOpen ? 'on' : undefined} onClick={() => setDrawerOpen(true)} aria-haspopup="dialog">
          更多
        </button>
      </nav>
      <Modal open={drawerOpen} onClose={() => setDrawerOpen(false)} title="全部页面">
        <div className="mobile-more">
          <button type="button" className="popover-item" onClick={() => go('fleet')}>
            <span aria-hidden="true">▦</span>
            <span>设备总览</span>
          </button>
          {MORE_NAV.filter((item) => item !== 'fleet').map((item) => (
            <button key={item} type="button" className="popover-item" onClick={() => go(item)}>
              <span>{VIEW_TITLES[item]}</span>
            </button>
          ))}
          <button type="button" className="popover-item" onClick={() => go('settings')}>
            <span aria-hidden="true">⚙️</span>
            <span>{VIEW_TITLES.settings}</span>
          </button>
          {current ? (
            <span className="popover-item faint">
              <StatusDot tone={current.healthy ? 'ok' : 'err'} />
              <span>当前设备：{current.name}（在桌面端顶栏切换）</span>
            </span>
          ) : null}
        </div>
      </Modal>
    </>
  )
}
