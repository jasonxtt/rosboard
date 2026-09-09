import type { ReactNode } from 'react'
import { EmptyState } from './EmptyState'
import { Skeleton } from './Skeleton'

export type TableColumn<T> = {
  key: string
  title: ReactNode
  /** numeric columns right-align with tabular figures (§7) */
  numeric?: boolean
  width?: string
  /** extra class on both th and td, e.g. responsive column hiding */
  className?: string
  render: (row: T, index: number) => ReactNode
}

function cellClassName<T>(column: TableColumn<T>): string | undefined {
  const classes = [column.numeric ? 'num' : '', column.className ?? ''].filter(Boolean).join(' ')
  return classes || undefined
}

type DataTableProps<T> = {
  columns: Array<TableColumn<T>>
  rows: T[]
  rowKey: (row: T) => string
  loading?: boolean
  emptyTitle?: string
  emptyDescription?: string
  /** extra row class, e.g. dim disabled rows */
  rowClassName?: (row: T) => string | undefined
  onRowClick?: (row: T) => void
  ariaLabel?: string
}

/** Typed table with sticky header, row hover and designed empty/loading states (§7). */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  loading = false,
  emptyTitle = '暂时没有数据',
  emptyDescription,
  rowClassName,
  onRowClick,
  ariaLabel,
}: DataTableProps<T>) {
  return (
    <div className="table-scroll">
      <table className="table" aria-label={ariaLabel}>
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column.key} className={cellClassName(column)} style={column.width ? { width: column.width } : undefined}>
                {column.title}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loading
            ? Array.from({ length: 4 }, (_, rowIndex) => (
                <tr key={`skeleton-${rowIndex}`}>
                  {columns.map((column) => (
                    <td key={column.key} className={cellClassName(column)}>
                      <Skeleton height={12} width={column.numeric ? '60%' : '82%'} />
                    </td>
                  ))}
                </tr>
              ))
            : rows.map((row, rowIndex) => (
                <tr
                  key={rowKey(row)}
                  className={rowClassName?.(row)}
                  onClick={onRowClick ? () => onRowClick(row) : undefined}
                  style={onRowClick ? { cursor: 'pointer' } : undefined}
                >
                  {columns.map((column) => (
                    <td key={column.key} className={cellClassName(column)}>
                      {column.render(row, rowIndex)}
                    </td>
                  ))}
                </tr>
              ))}
        </tbody>
      </table>
      {!loading && rows.length === 0 ? <EmptyState title={emptyTitle} description={emptyDescription} /> : null}
    </div>
  )
}
