import { useState } from 'react'
import { useShell } from './useShell'

/** Amber warning strip under the topnav when dashboard warnings are non-empty (§6). */
export function WarningBar() {
  const { warnings } = useShell()
  const [expanded, setExpanded] = useState(false)
  if (warnings.length === 0) return null
  return (
    <button type="button" className="warnbar" onClick={() => setExpanded((value) => !value)} aria-expanded={expanded}>
      <span className="warnbar-icon" aria-hidden="true">⚠</span>
      <span className="warnbar-text">
        {expanded ? warnings.map((warning) => <span key={warning} className="warnbar-line">{warning}</span>) : warnings[0]}
      </span>
      {warnings.length > 1 ? <span className="warnbar-more">{expanded ? '收起' : `还有 ${warnings.length - 1} 条`}</span> : null}
    </button>
  )
}
