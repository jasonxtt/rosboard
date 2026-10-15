import assert from 'node:assert/strict'
import test from 'node:test'
import React, { act } from 'react'
import { JSDOM } from 'jsdom'
import { ComposeImport } from '../src/features/containers/ComposeImport.tsx'
import { ContainerEditor } from '../src/features/containers/ContainerEditor.tsx'
import { applyCompose } from '../src/features/containers/compose.ts'
import { newDraft } from '../src/features/containers/drafts.ts'
import { parseComposeResult, parseSnapshot } from '../src/features/containers/api.ts'

const bootstrap = new JSDOM('<!doctype html><html><body></body></html>')
const saved = { window: globalThis.window, document: globalThis.document }
Object.assign(globalThis, { window: bootstrap.window, document: bootstrap.window.document })
const { createRoot } = await import('react-dom/client')
Object.assign(globalThis, saved, { React, IS_REACT_ACT_ENVIRONMENT: true })
bootstrap.window.close()

function installDOM() {
  const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', { url: 'http://localhost' })
  const previous = { window: globalThis.window, document: globalThis.document, HTMLElement: globalThis.HTMLElement, Event: globalThis.Event, fetch: globalThis.fetch }
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, HTMLElement: dom.window.HTMLElement, Event: dom.window.Event })
  dom.window.HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', '') }
  dom.window.HTMLDialogElement.prototype.close = function () { this.removeAttribute('open') }
  const root = createRoot(document.getElementById('root')!)
  return { dom, root, async close() { await act(async () => root.unmount()); Object.assign(globalThis, previous); dom.window.close() } }
}
const response = (value: unknown) => new Response(JSON.stringify(value))
const tick = () => act(async () => { await new Promise((r) => setTimeout(r, 0)) })
const service = (name: string) => ({ name, fields: { name, image: `example/${name}:v1`, restartPolicy: 'no', env: [{ key: 'EMPTY', value: '' }], mounts: [{ source: '/not-a-ros-path', target: '/config', readOnly: true, mode: 'ro' }] }, imported: ['image', 'environment'], skipped: [{ field: 'ports', message: '不生成 NAT' }], needsInput: [{ field: 'volumes[0].source', message: '请选择 ROS 来源目录' }], mountHints: ['./config'] })
const result = (...names: string[]) => ({ services: names.map(service), skipped: [] })
function button(label: string) {
  const value = [...document.querySelectorAll<HTMLButtonElement>('button')].find((v) => v.textContent === label)
  assert.ok(value, `missing ${label}`)
  return value
}
async function typeSource(dom: JSDOM, value: string) {
  await act(async () => {
    const input = document.querySelector('textarea')!
    Object.getOwnPropertyDescriptor(dom.window.HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new dom.window.Event('input', { bubbles: true }))
  })
}

test('Compose field application retains ROS scope and replaces old imported values', () => {
  const original = newDraft()
  original.network = { ...original.network, bridge: 'bridge1', address: '192.0.2.2/24', gateway: '192.0.2.1', veth: 'dedicated' }
  original.rootDir = '/sata1/my-root/rootfs'
  original.env = [{ key: 'OLD', value: 'old' }]
  original.command = 'old command'
  original.memoryMax = '256M'
  original.startOnBoot = false
  const parsed = parseComposeResult(result('app'))
  const next = applyCompose(original, parsed.services[0].fields)
  assert.equal(next.draftId, original.draftId)
  assert.deepEqual(next.network, original.network)
  assert.equal(next.rootDir, original.rootDir)
  assert.equal(next.startOnBoot, false)
  assert.equal(next.command, '')
  assert.equal(next.memoryMax, '')
  assert.deepEqual(next.env, [{ key: 'EMPTY', value: '' }])
  assert.equal(next.mounts[0].source, '')
  assert.equal(next.mounts[0].readOnly, true)
  const second = applyCompose(next, parseComposeResult(result('db')).services[0].fields)
  assert.equal(second.name, 'db')
  assert.equal(second.env.length, 1)
  assert.equal(original.env[0].key, 'OLD')
  assert.throws(() => parseComposeResult({ services: [] }))
  assert.throws(() => parseComposeResult({ services: [{}] }))
})

test('Compose dialog selects only one service and never creates a container', async () => {
  const env = installDOM()
  const calls: string[] = []
  let applied = ''
  globalThis.fetch = async (input, init) => {
    calls.push(String(input))
    assert.equal(init?.method, 'POST')
    return response(result('msm', 'db'))
  }
  try {
    await act(async () => env.root.render(<ComposeImport deviceId="lab" onApply={(s) => { applied = s.name }} onClose={() => {}} />))
    await typeSource(env.dom, 'services: example')
    await act(async () => button('解析').click())
    await tick()
    assert.equal(button('填入面板').disabled, true)
    const select = document.querySelector('select')!
    await act(async () => { select.value = 'msm'; select.dispatchEvent(new env.dom.window.Event('change', { bubbles: true })) })
    assert.match(document.body.textContent || '', /已跳过字段/)
    assert.match(document.body.textContent || '', /需要补填的信息/)
    assert.match(document.body.textContent || '', /\.\/config/)
    await act(async () => button('填入面板').click())
    assert.equal(applied, 'msm')
    assert.deepEqual(calls, ['/api/containers/compose/parse?device=lab'])
  } finally { await env.close() }
})

test('Compose source edits, cancellation and device changes discard stale responses', async () => {
  const env = installDOM()
  let finish: ((r: Response) => void) | undefined
  let applied = false
  let closed = false
  globalThis.fetch = async () => new Promise<Response>((r) => { finish = r })
  const render = (deviceId: string) => <ComposeImport key={deviceId} deviceId={deviceId} onApply={() => { applied = true }} onClose={() => { closed = true }} />
  try {
    await act(async () => env.root.render(render('a')))
    await typeSource(env.dom, 'first')
    await act(async () => button('解析').click())
    await typeSource(env.dom, 'second')
    await act(async () => finish?.(response(result('old'))))
    await tick()
    assert.equal(button('填入面板').disabled, true)
    await act(async () => button('解析').click())
    await act(async () => env.root.render(render('b')))
    await act(async () => finish?.(response(result('other-device'))))
    await tick()
    assert.equal(document.querySelector('textarea')?.value, '')
    assert.equal(button('填入面板').disabled, true)
    await act(async () => button('取消').click())
    assert.equal(closed, true)
    assert.equal(applied, false)
  } finally { await env.close() }
})

test('Create editor applies a single service, keeps network/root and exposes resource inputs', async () => {
  const env = installDOM()
  const initial = newDraft()
  initial.rootDir = '/sata1/retained/rootfs'
  initial.network.address = '192.0.2.2/24'
  const parsed = result('app')
  Object.assign(parsed.services[0].fields, { memoryMax: '134217728', user: '1000' })
  let mutations = 0
  globalThis.fetch = async (input, init) => {
    if (String(input).includes('/compose/parse')) return response(parsed)
    if (String(input).includes('/resolve')) return response({ effective: JSON.parse(String(init?.body)), errors: {}, defaults: [], containerFields: {} })
    mutations++
    return response({})
  }
  try {
    await act(async () => env.root.render(<ContainerEditor deviceId="lab" snapshot={parseSnapshot({})} initial={initial} item={null} busy={false} onClose={() => {}} onSubmit={async () => { mutations++ }} />))
    await act(async () => button('从 Compose 导入').click())
    await typeSource(env.dom, 'services: example')
    await act(async () => button('解析').click())
    await tick()
    assert.equal(document.querySelector('select')?.value, 'app')
    await act(async () => button('填入面板').click())
    assert.equal(document.querySelector('dialog'), null)
    const inputs = [...document.querySelectorAll<HTMLInputElement>('input')]
    assert.ok(inputs.some((i) => i.value === initial.rootDir))
    assert.ok(inputs.some((i) => i.value === initial.network.address))
    assert.ok(inputs.some((i) => i.value === '128'))
    assert.match(document.body.textContent || '', /已导入服务 app/)
    assert.equal(mutations, 0)
  } finally { await env.close() }
})
