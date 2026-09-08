type SkeletonProps = {
  width?: number | string
  height?: number | string
  lines?: number
  className?: string
}

function size(value: number | string | undefined, fallback: string): string {
  if (value === undefined) return fallback
  return typeof value === 'number' ? `${value}px` : value
}

/** Sweep-gradient placeholder while data loads — never a bare spinner (§7). */
export function Skeleton({ width, height = 14, lines = 1, className = '' }: SkeletonProps) {
  if (lines <= 1) {
    return <div className={`skeleton ${className}`.trim()} style={{ width: size(width, '100%'), height: size(height, '14px') }} />
  }
  return (
    <div className={className} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {Array.from({ length: lines }, (_, index) => (
        <div
          key={index}
          className="skeleton"
          style={{ width: index === lines - 1 && width === undefined ? '62%' : size(width, '100%'), height: size(height, '14px') }}
        />
      ))}
    </div>
  )
}
