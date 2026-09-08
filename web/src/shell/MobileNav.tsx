import { useState } from 'react'
import { Modal } from '../ui/Modal'
import { useShell } from './useShell'
import { NAV_GROUPS, NAV_ITEM_META, TOP_LEVEL_NAV, VIEW_TITLES, navGroupOf, type NavGroup } from './views'

/** Mobile (<768px) bottom floating pill: 仪表台/系统概览 + group drawers (§11). */
export function MobileNav() {
  const { view, navigate } = useShell()
  const [openGroup, setOpenGroup] = useState<NavGroup | null>(null)
  const group = NAV_GROUPS.find((candidate) => candidate.key === openGroup)

  return (
    <>
      <nav className="bottomnav glass" aria-label="移动端主导航">
        {TOP_LEVEL_NAV.map((item) => (
          <button key={item} type="button" className={view === item ? 'on' : undefined} onClick={() => navigate(item)}>
            {VIEW_TITLES[item]}
          </button>
        ))}
        {NAV_GROUPS.map((candidate) => (
          <button
            key={candidate.key}
            type="button"
            className={navGroupOf(view) === candidate.key || openGroup === candidate.key ? 'on' : undefined}
            onClick={() => setOpenGroup(candidate.key)}
            aria-haspopup="dialog"
          >
            {candidate.label}
          </button>
        ))}
      </nav>
      <Modal open={group != null} onClose={() => setOpenGroup(null)} title={group?.label ?? ''}>
        <div className="mobile-more nav-mega">
          {group?.items.map((item) => {
            const meta = NAV_ITEM_META[item]
            return (
              <button
                key={item}
                type="button"
                className={view === item ? 'mega-item mega-item-on' : 'mega-item'}
                onClick={() => {
                  setOpenGroup(null)
                  navigate(item)
                }}
              >
                <span className="mega-item-ic" aria-hidden="true">{meta?.icon ?? '·'}</span>
                <span className="mega-item-text">
                  <b>{VIEW_TITLES[item]}</b>
                  {meta ? <small>{meta.desc}</small> : null}
                </span>
              </button>
            )
          })}
        </div>
      </Modal>
    </>
  )
}
