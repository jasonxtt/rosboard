import { useState } from 'react'
import { toast } from './toastStore'

function fallbackCopyText(value: string) {
  const activeElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const textarea = document.createElement('textarea')
  textarea.value = value
  textarea.readOnly = true
  textarea.setAttribute('aria-hidden', 'true')
  textarea.style.position = 'fixed'
  textarea.style.inset = '0 auto auto 0'
  textarea.style.width = '1px'
  textarea.style.height = '1px'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  try {
    textarea.focus({ preventScroll: true })
    textarea.select()
    textarea.setSelectionRange(0, value.length)
    if (!document.execCommand('copy')) throw new Error('copy command was rejected')
  } finally {
    textarea.remove()
    activeElement?.focus({ preventScroll: true })
  }
}

async function copyText(value: string) {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return
    } catch {
      // HTTP deployments fall through to the textarea path.
    }
  }
  fallbackCopyText(value)
}

type CopyButtonProps = {
  text: string
  label?: string
  className?: string
  /** render the label next to the icon (for prominent, text-labeled buttons) */
  showText?: boolean
}

/** Copy-to-clipboard button with toast feedback. */
export function CopyButton({ text, label = '复制', className = '', showText = false }: CopyButtonProps) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      className={`icon-btn ${className}`.trim()}
      aria-label={label}
      title={label}
      onClick={async () => {
        try {
          await copyText(text)
          setCopied(true)
          toast('已复制到剪贴板')
          window.setTimeout(() => setCopied(false), 1200)
        } catch {
          toast('复制失败，请手动选择文本复制', { tone: 'err' })
        }
      }}
    >
      {showText ? (copied ? '已复制' : label) : copied ? '✓' : '⧉'}
    </button>
  )
}
