import { readUiVariant, UI_VARIANT_KEY, writeLocal } from './uiPreference'

const variant = readUiVariant()
document.documentElement.dataset.ui = variant
writeLocal(UI_VARIANT_KEY, variant)

// No UI CSS here: the two complete interfaces must stay isolated.
// Keep separate awaited statements. A conditional import expression can be
// folded into one Vite preload call with only the other branch's CSS list.
async function boot() {
  if (variant === 'compact') {
    await import('./compact/main')
    return
  }
  await import('./aurora')
}
void boot().catch(() => {
  const root = document.getElementById('root')
  if (!root) return
  const message = document.createElement('p')
  message.textContent = '界面加载失败，请刷新重试，或切换另一套 UI。'
  const retry = document.createElement('button')
  retry.textContent = '刷新重试'
  retry.onclick = () => window.location.reload()
  const link = document.createElement('a')
  const url = new URL(window.location.href)
  url.searchParams.set('ui', variant === 'compact' ? 'aurora' : 'compact')
  link.href = url.href
  link.textContent = '切换另一套 UI'
  root.replaceChildren(message, retry, document.createTextNode(' '), link)
})
