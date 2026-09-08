import type { Terminal } from './types'

type TerminalNameFields = Pick<Terminal, 'autoName' | 'customName' | 'displayName'>

export function terminalNameDraft(terminal: TerminalNameFields) {
  return terminal.customName || ''
}

export function terminalNamePlaceholder(terminal: TerminalNameFields) {
  return terminal.autoName || terminal.displayName || '自动名称'
}

/** Return the normalized value to persist, or null when no metadata changed. */
export function terminalNameSubmission(draft: string, currentCustomName: string) {
  const next = draft.trim()
  return next === currentCustomName.trim() ? null : next
}
