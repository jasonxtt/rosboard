import { useId } from 'react'
import { formatPercent } from '../lib/format'

type GaugeRingProps = {
  /** 0..100 */
  percent: number
  label?: string
  size?: number
  strokeWidth?: number
  /** gradient stops as CSS values; default to the brand line gradient tokens */
  from?: string
  to?: string
}

/** SVG dual-ring gauge: track --stroke, value arc gradient, round caps (§7 gauge). */
export function GaugeRing({
  percent,
  label,
  size = 84,
  strokeWidth = 8,
  from = 'var(--grad-line-from)',
  to = 'var(--grad-line-to)',
}: GaugeRingProps) {
  const gradientId = useId()
  const clamped = Math.min(100, Math.max(0, Number.isFinite(percent) ? percent : 0))
  const radius = (size - strokeWidth) / 2
  const center = size / 2
  const circumference = 2 * Math.PI * radius
  return (
    <span className="gauge-ring">
      <span className="gauge-figure" style={{ width: size, height: size }}>
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label={label ? `${label} ${formatPercent(clamped)}` : formatPercent(clamped)}>
          <circle cx={center} cy={center} r={radius} fill="none" strokeWidth={strokeWidth} style={{ stroke: 'var(--stroke)' }} />
          <circle
            cx={center}
            cy={center}
            r={radius}
            fill="none"
            strokeWidth={strokeWidth}
            strokeLinecap="round"
            strokeDasharray={circumference}
            strokeDashoffset={circumference * (1 - clamped / 100)}
            transform={`rotate(-90 ${center} ${center})`}
            style={{ stroke: `url(#${gradientId})` }}
          />
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="1" y2="1">
              <stop offset="0" style={{ stopColor: from }} />
              <stop offset="1" style={{ stopColor: to }} />
            </linearGradient>
          </defs>
        </svg>
        <span className="gauge-value" aria-hidden="true">
          {formatPercent(clamped)}
        </span>
      </span>
      {label ? <span className="gauge-label">{label}</span> : null}
    </span>
  )
}
