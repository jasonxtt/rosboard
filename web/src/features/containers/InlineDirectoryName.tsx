import { useEffect, useRef, useState } from 'react'
import { directoryNameError } from './directoryPaths'

export function InlineDirectoryName({
  initial,
  label,
  busy,
  onSave,
  onCancel,
}: {
  initial: string
  label: string
  busy: boolean
  onSave: (name: string) => Promise<boolean>
  onCancel: () => void
}) {
  const [name, setName] = useState(initial),
    [error, setError] = useState('')
  const input = useRef<HTMLInputElement>(null),
    finishing = useRef(false)
  useEffect(() => {
    input.current?.focus()
    input.current?.select()
  }, [])
  const save = async (blur = false) => {
    if (finishing.current || busy) return
    const message = directoryNameError(name)
    if (message) {
      if (!blur) setError(message)
      return
    }
    finishing.current = true
    const success = await onSave(name)
    if (!success) finishing.current = false
  }
  return (
    <div className="ct-directory-inline">
      <span aria-hidden="true">📁</span>
      <div>
        <input
          ref={input}
          aria-label={label}
          value={name}
          disabled={busy}
          aria-invalid={!!error}
          onChange={(e) => {
            setName(e.target.value)
            setError('')
          }}
          onBlur={() => void save(true)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              void save()
            }
            if (e.key === 'Escape') {
              e.preventDefault()
              e.stopPropagation()
              finishing.current = true
              onCancel()
            }
          }}
        />
        {error && <small role="alert">{error}</small>}
      </div>
      <button
        type="button"
        aria-label="取消名称编辑"
        disabled={busy}
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => {
          finishing.current = true
          onCancel()
        }}
      >
        ×
      </button>
    </div>
  )
}
