import { Button } from './Button'

type PaginationProps = {
  page: number
  pageSize: number
  total: number
  onChange: (page: number) => void
}

export function Pagination({ page, pageSize, total, onChange }: PaginationProps) {
  const pages = Math.max(1, Math.ceil(total / pageSize))
  if (pages <= 1) return null
  const clamped = Math.min(Math.max(1, page), pages)
  return (
    <nav className="pagination" aria-label="分页">
      <Button size="sm" disabled={clamped <= 1} onClick={() => onChange(clamped - 1)}>
        上一页
      </Button>
      <span className="pagination-info">
        第 {clamped} / {pages} 页 · 共 {total} 条
      </span>
      <Button size="sm" disabled={clamped >= pages} onClick={() => onChange(clamped + 1)}>
        下一页
      </Button>
    </nav>
  )
}
