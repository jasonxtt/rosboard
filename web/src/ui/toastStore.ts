import type { ToastTone } from './toastTypes'

export type ToastItem = {
  id: number
  message: string
  tone: ToastTone
  leaving: boolean
}

const TOAST_DURATION_MS = 4000
const TOAST_EXIT_MS = 160

let nextId = 1
const listeners = new Set<() => void>()
let toasts: ToastItem[] = []

function notify() {
  for (const listener of listeners) listener()
}

/** Fire a toast from anywhere: `toast('已保存')` / `toast('失败', { tone: 'err' })`. */
export function toast(message: string, options?: { tone?: ToastTone }) {
  const item: ToastItem = { id: nextId++, message, tone: options?.tone ?? 'ok', leaving: false }
  toasts = [...toasts, item].slice(-5)
  notify()
  window.setTimeout(() => {
    toasts = toasts.map((entry) => (entry.id === item.id ? { ...entry, leaving: true } : entry))
    notify()
    window.setTimeout(() => {
      toasts = toasts.filter((entry) => entry.id !== item.id)
      notify()
    }, TOAST_EXIT_MS)
  }, TOAST_DURATION_MS)
}

export function snapshotToasts(): ToastItem[] {
  return toasts
}

export function subscribeToasts(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
