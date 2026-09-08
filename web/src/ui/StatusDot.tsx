type StatusDotProps = {
  tone?: 'ok' | 'warn' | 'err' | 'neutral'
  /** breathing glow — reserved for the live/collecting hero state (§10) */
  pulse?: boolean
}

export function StatusDot({ tone = 'neutral', pulse = false }: StatusDotProps) {
  return <span className={`status-dot status-dot-${tone}${pulse ? ' status-dot-pulse' : ''}`} aria-hidden="true" />
}
