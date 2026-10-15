import assert from 'node:assert/strict'
import { after, beforeEach, test } from 'node:test'
import React, { act } from 'react'
import { JSDOM } from 'jsdom'
import { createRoot } from 'react-dom/client'
import { ImageTransferPanel } from '../src/features/settings/ImageTransferPanel.tsx'

const dom = new JSDOM('<html><body><div id="root"></div></body></html>', { url: 'http://localhost' })
Object.assign(globalThis, { React, window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true })
const root = createRoot(document.getElementById('root')!)
const originalFetch = globalThis.fetch
const calls: Record<string, unknown>[] = []
beforeEach(async () => {
  await act(async () => root.render(null))
  calls.length = 0
  globalThis.fetch = (async (_url, init) => {
    if (init?.body) calls.push(JSON.parse(String(init.body)))
    return new Response(JSON.stringify({ mode: 'connection', username: 'fixture', passwordSet: true, port: 22, hostKey: init?.method === 'PUT' ? 'SHA256:fixture' : '', revision: 'fixture-revision' }), { status: 200 })
  }) as typeof fetch
})
after(async () => { await act(async () => root.unmount()); globalThis.fetch = originalFetch; dom.window.close() })
for (const buttonClass of ['toolbar-button', 'btn btn-ghost']) test(`transfer configuration automatically initializes SSH trust: ${buttonClass}`, async () => {
  await act(async () => root.render(<ImageTransferPanel deviceId="fixture" buttonClass={buttonClass} inputClass="input" />))
  assert.equal(document.querySelectorAll('input[type="text"]').length, 0)
  assert.equal(document.querySelector('input[placeholder="SHA256:…"]'), null)
  assert.match(document.body.textContent!, /无需填写指纹/)
  const save = [...document.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent === '验证并保存')!
  await act(async () => save.click())
  assert.equal(calls.at(-1)?.hostKey, '')
  assert.equal(calls.at(-1)?.mode, 'connection')
  assert.match(document.body.textContent!, /设备连接校验已配置/)
  assert.equal(window.localStorage.length, 0)
})
