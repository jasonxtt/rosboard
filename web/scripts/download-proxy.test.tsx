import assert from 'node:assert/strict'
import { after, beforeEach, test } from 'node:test'
import { JSDOM } from 'jsdom'
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { DownloadProxyPanel } from '../src/features/settings/DownloadProxyPanel.tsx'
const dom = new JSDOM('<html><body><div id="root"></div></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
Object.assign(globalThis, { React, window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true })
const root = createRoot(document.getElementById('root')!)
const originalFetch = globalThis.fetch
const calls: { path: string; method?: string; body?: Record<string, unknown> }[] = []
beforeEach(async () => {
 await act(async () => root.render(null)); calls.length = 0
 globalThis.fetch = (async (path, init) => {
  calls.push({ path: String(path), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
  const result = String(path).endsWith('/test') ? { updates: true, registry: true, authentication: true } : { displayURL: 'http://user:xxxxx@proxy.example:7890', passwordSet: true, revision: 'saved-revision' }
  return new Response(JSON.stringify(result), { status: 200 })
 }) as typeof fetch
})
after(async () => { await act(async () => root.unmount()); globalThis.fetch = originalFetch; dom.window.close() })
for (const buttonClass of ['toolbar-button', 'btn btn-ghost']) test(`single proxy URL, inline test and external confirm in ${buttonClass}`, async () => {
 await act(async () => root.render(<DownloadProxyPanel buttonClass={buttonClass} inputClass="input" />))
 assert.equal(document.querySelectorAll('input').length, 1)
 const testButton = document.querySelector<HTMLButtonElement>('button[aria-label="测试当前代理 URL"]')!
 assert.equal(testButton.parentElement, document.querySelector('input')!.parentElement)
 const confirm = [...document.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent === '确认')!
 assert.notEqual(confirm.parentElement, testButton.parentElement)
 await act(async () => testButton.click())
 assert.deepEqual(calls.at(-1)?.body, { useSaved: true, revision: 'saved-revision' })
 assert.equal(calls.at(-1)?.path, '/api/settings/download-proxy/test')
 assert.equal(calls.filter(c => c.method === 'PUT').length, 0)
 assert.match(document.querySelector('[role="status"]')!.textContent!, /测试未保存/)
 await act(async () => confirm.click())
 assert.equal(calls.at(-1)?.method, 'PUT')
 assert.equal(calls.at(-1)?.body?.url, undefined)
 assert.equal(window.localStorage.length, 0)
})
