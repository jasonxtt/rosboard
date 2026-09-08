import { useId } from 'react'

type SparklineProps = {
  /** series values; fewer than two points renders an empty track */
  points: number[]
  width?: number
  height?: number
  stroke?: string
  /** fill the area under the line with a fading gradient (default true) */
  area?: boolean
  ariaLabel?: string
}

/**
 * Small SVG sparkline; non-scaling stroke, fills its container width (§7/§8).
 * `stroke` accepts any CSS color — pass token references like 'var(--accent)'.
 */
export function Sparkline({ points, width = 120, height = 34, stroke = 'var(--accent)', area = true, ariaLabel }: SparklineProps) {
  const gradientId = useId()
  if (points.length < 2) {
    return <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" aria-hidden="true" />
  }
  const min = Math.min(...points)
  const max = Math.max(...points)
  const span = max - min || 1
  const inset = 2
  const usable = height - inset * 2
  const step = width / (points.length - 1)
  const coordinates = points.map((point, index) => {
    const x = index * step
    const y = inset + usable - ((point - min) / span) * usable
    return `${x.toFixed(1)},${y.toFixed(1)}`
  })
  const line = `M${coordinates.join(' L')}`
  const fill = `${line} L${width},${height} L0,${height} Z`
  return (
    <svg
      width="100%"
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      role={ariaLabel ? 'img' : undefined}
      aria-label={ariaLabel}
      aria-hidden={ariaLabel ? undefined : true}
    >
      {area ? (
        <>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0" style={{ stopColor: stroke, stopOpacity: 0.28 }} />
              <stop offset="1" style={{ stopColor: stroke, stopOpacity: 0 }} />
            </linearGradient>
          </defs>
          <path d={fill} stroke="none" style={{ fill: `url(#${gradientId})` }} />
        </>
      ) : null}
      <path
        d={line}
        fill="none"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
        style={{ stroke }}
      />
    </svg>
  )
}
