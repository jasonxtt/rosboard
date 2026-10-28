import assert from 'node:assert/strict'
import { after, beforeEach, test } from 'node:test'
import { JSDOM } from 'jsdom'
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { AccountTransferPanel } from '../src/features/settings/AccountTransferPanel.tsx'
import { parseAccountTransfer } from '../src/features/settings/api.ts'

const dom = new JSDOM('<html><body><div id="root"></div></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
Object.assign(globalThis, { React, window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true })
const root = createRoot(document.getElementById('root')!)
const originalFetch = globalThis.fetch
const calls: { path: string; init?: RequestInit }[] = []
let response: unknown
beforeEach(async () => {
  await act(async () => root.render(null))
  calls.length = 0
  response = { state: 'new', script: '<script>text only</script>', readAccess: true, writeVerified: false, warning: '未执行写入，不能据此认定上传成功。' }
  globalThis.fetch = (async (path, init) => {
    calls.push({ path: String(path), init })
    return new Response(JSON.stringify(response), { status: 200 })
  }) as typeof fetch
})
after(async () => { await act(async () => root.unmount()); globalThis.fetch = originalFetch; dom.window.close() })

test('transfer boundary refuses unknown roles and claims of proven writes', () => {
  for (const value of [null, { state: 'full', script: 'x', writeVerified: false }, { state: 'new', script: 'x', writeVerified: true }, { state: 'old', script: '', writeVerified: false }]) {
    assert.throws(() => parseAccountTransfer(value))
  }
})
for (const buttonClass of ['toolbar-button', 'btn btn-ghost btn-sm']) test(`transfer check stays read-only in ${buttonClass}`, async () => {
  await act(async () => root.render(<AccountTransferPanel deviceId="lab" buttonClass={buttonClass} inputClass="script-area" />))
  assert.equal(calls.length, 0)
  await act(async () => document.querySelector<HTMLButtonElement>('button')!.click())
  assert.equal(calls.length, 1)
  assert.equal(calls[0].path, '/api/devices/lab/account-transfer')
  assert.equal(calls[0].init?.method, 'GET')
  assert.equal(calls[0].init?.body, undefined)
  assert.equal(document.querySelector('script'), null)
  assert.equal(document.querySelector('textarea')?.value, '<script>text only</script>')
  assert.match(document.querySelector('[role="status"]')!.textContent!, /不能据此认定上传成功/)
})
test('device switch aborts the old identity check and discards its script', async () => {
  let finish: (response: Response) => void = () => undefined
  let signal: AbortSignal | null | undefined
  globalThis.fetch = (async (_, init) => { signal = init?.signal; return new Promise<Response>((resolve) => { finish = resolve }) }) as typeof fetch
  await act(async () => root.render(<AccountTransferPanel key="a" deviceId="a" buttonClass="btn" inputClass="script-area" />))
  await act(async () => document.querySelector<HTMLButtonElement>('button')!.click())
  await act(async () => root.render(<AccountTransferPanel key="b" deviceId="b" buttonClass="btn" inputClass="script-area" />))
  assert.equal(signal?.aborted, true)
  await act(async () => finish(new Response(JSON.stringify(response), { status: 200 })))
  assert.equal(document.querySelector('textarea'), null)
})
