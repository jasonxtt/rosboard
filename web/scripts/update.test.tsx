import assert from 'node:assert/strict'
import { after, beforeEach, test } from 'node:test'
import { JSDOM } from 'jsdom'
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { parseUpdateStatus } from '../src/features/update/api.ts'
import { UpdatePanel } from '../src/features/update/UpdatePanel.tsx'
const dom = new JSDOM('<html><body><div id="root"></div></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
Object.assign(globalThis, { React, window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true })
dom.window.HTMLDialogElement.prototype.showModal = function () { this.open = true }
dom.window.HTMLDialogElement.prototype.close = function () { this.open = false }
const root = createRoot(document.getElementById('root')!)
const originalFetch = globalThis.fetch
const calls: { path: string; body?: string }[] = []
let response: Record<string, unknown>
beforeEach(async () => {
 await act(async () => root.render(null))
 calls.length = 0
 response = { current: {version:'0.2.0',os:'linux',arch:'arm64'}, latest: {version:'0.2.1', notes:'<script>alert(1)</script>',url:'https://github.com/jasonxtt/rosboard/releases/tag/v0.2.1'},canInstall:true,reason:'',job:null }
 globalThis.fetch = (async (path, init) => { calls.push({path:String(path),body:init?.body as string|undefined}); return new Response(JSON.stringify(response),{status:200}) }) as typeof fetch
})
after(async () => {await act(async () => root.unmount());globalThis.fetch=originalFetch;dom.window.close()})
test('boundary normalizes missing fields and refuses unsafe release links', () => {
 const p=parseUpdateStatus({latest:{version:'0.2.1',url:'javascript:alert(1)'},job:null})
 assert.equal(p.current.version,'');assert.equal(p.canInstall,false);assert.equal(p.latest?.url,'https://github.com/jasonxtt/rosboard/releases');assert.equal(p.job,null)
})
for (const style of [['toolbar-button','primary-button'],['btn btn-ghost','btn btn-primary']]) test(`two actions, escaped notes and pinned confirmation: ${style[0]}`,async () => {
 await act(async () => root.render(<UpdatePanel buttonClass={style[0]} primaryClass={style[1]} />))
 const buttons=[...document.querySelectorAll<HTMLButtonElement>('.version-update > .version-update-actions > button')]
 assert.deepEqual(buttons.map(b=>b.textContent),['检查更新','立即更新'])
 assert.equal(document.querySelector('script'),null)
 await act(async () => buttons[1].click())
 assert.equal(document.querySelector('dialog')?.open,true)
 assert.equal(calls.filter(c=>c.path.endsWith('/install')).length,0)
 response={...response,job:{id:'one',from:'0.2.0',to:'0.2.1',stage:'downloading',total:100,downloaded:25},canInstall:false}
 await act(async () => document.querySelector<HTMLButtonElement>('dialog button:last-child')!.click())
 assert.equal(calls.filter(c=>c.path.endsWith('/install')).length,1)
 assert.equal(calls.at(-1)?.body,'{"version":"0.2.1"}')
 assert.equal(document.querySelector<HTMLProgressElement>('progress')?.value,25)
 assert.equal(buttons[1].disabled,true)
 assert.equal(document.querySelector('dialog')?.open,false)
})
test('check failure stays visible and cannot be reported as latest', async () => {
 response={...response,checkError:'无法连接 GitHub',canInstall:false,reason:'检查失败，请重新检查更新'}
 await act(async () => root.render(<UpdatePanel buttonClass="toolbar-button" primaryClass="primary-button" />))
 assert.match(document.querySelector('[role="alert"]')!.textContent!,/无法连接 GitHub/)
 assert.equal(document.querySelector<HTMLButtonElement>('.version-update-actions > button:last-child')!.disabled,true)
 assert.doesNotMatch(document.body.textContent!,/已是最新版本/)
})
test('only the last result is shown after rollback, with no history controls', async () => {
 response={...response,job:{id:'last',from:'0.2.0',to:'0.2.1',stage:'rolled_back',finishedAt:'2026-09-09T10:00:00Z',message:'已恢复原版本和数据'}}
 await act(async () => root.render(<UpdatePanel buttonClass="toolbar-button" primaryClass="primary-button" />))
 assert.match(document.querySelector('[role="status"]')!.textContent!,/最近更新：v0.2.0 → v0.2.1 · 已恢复原版本/)
 assert.doesNotMatch(document.body.textContent!,/更多操作|重新安装|历史记录/)
})
