import type { SortDirection } from './hooks'
import './monitor-detail.css'

type SortHeaderProps<K extends string> = {
  label: string
  sortKey: K
  activeKey: K
  direction: SortDirection
  onSort: (key: K) => void
}

/**
 * Sortable column label for DataTable `title` slots: click selects ascending,
 * second click flips direction. The direction indicator only renders for the
 * active column (component-guidelines).
 */
export function SortHeader<K extends string>({ label, sortKey, activeKey, direction, onSort }: SortHeaderProps<K>) {
  const active = sortKey === activeKey
  return (
    <button
      type="button"
      className={`mon-sort-header${active ? ' mon-sort-active' : ''}`}
      aria-label={`按${label}排序${active ? `，当前${direction === 'asc' ? '升序' : '降序'}` : ''}`}
      onClick={(event) => {
        event.stopPropagation()
        onSort(sortKey)
      }}
    >
      <span>{label}</span>
      {active ? (
        <span className="mon-sort-indicator" aria-hidden="true">
          {direction === 'asc' ? '↑' : '↓'}
        </span>
      ) : null}
    </button>
  )
}
