import assert from 'node:assert/strict'
import test from 'node:test'
import React, { act } from 'react'
import { JSDOM } from 'jsdom'
import { ContainerEditor } from '../src/features/containers/ContainerEditor.tsx'
import { ContainerPage } from '../src/features/containers/ContainerPage.tsx'
import CompactContainers from '../src/compact/features/containers/ContainersPage.tsx'
import {
  editDraft,
  newDraft,
  normalizedImage,
} from '../src/features/containers/drafts.ts'
import {
  parseItem,
  parseSnapshot,
  parseResolution,
  fetchContainers,
  performAction,
} from '../src/features/containers/api.ts'
import type { Draft } from '../src/features/containers/types.ts'

// React detects input-event support at import time. Initialize a DOM before
// loading react-dom so native typing exercises the real controlled-input path.
const bootstrapDOM = new JSDOM('<!doctype html><html><body></body></html>')
const previousBootstrap = {
  window: globalThis.window,
  document: globalThis.document,
}
Object.assign(globalThis, {
  window: bootstrapDOM.window,
  document: bootstrapDOM.window.document,
})
const { createRoot } = await import('react-dom/client')
Object.assign(globalThis, previousBootstrap)
bootstrapDOM.window.close()
Object.assign(globalThis, { React, IS_REACT_ACT_ENVIRONMENT: true })
const existing = () =>
  parseItem({
    id: '*7',
    name: 'dns',
    image: 'dns:v1',
    status: 'stopped',
    ownership: 'unmanaged',
    network: {
      veth: 'shared-veth',
      bridge: 'br-container',
      address: '172.20.0.2/24',
      gateway: '172.20.0.1',
    },
    sharedVeth: ['metrics'],
    envLists: ['shared-env'],
    mountLists: ['shared-mount'],
    config: {
      existingId: '*7',
      name: 'dns',
      image: 'dns:v1',
      rootDir: '/sata1/existing',
      network: {
        veth: 'shared-veth',
        bridge: 'br-container',
        address: '172.20.0.2/24',
        gateway: '172.20.0.1',
      },
      logging: false,
      startOnBoot: false,
      restartPolicy: 'on-failure',
      command: 'serve --exact',
      user: '1000',
      env: [{ key: 'SPECIAL', value: ' \' "$; 中文 ' }],
      mounts: [
        { source: '/sata1/shared', target: '/etc/config', readOnly: true },
      ],
      health: { mode: 'inherit' },
    },
  })
const snapshot = (writes = false) =>
  parseSnapshot({
    items: [existing()],
    options: {
      bridges: ['br-container'],
      disks: [{ name: 'sata1', freeBytes: 4 * 2 ** 30, writable: true }],
      memoryHigh: '256M',
      memoryMax: '512M',
    },
    capabilities: {
      supported: true,
      writes,
      mode: writes ? 'simulation' : 'read-only',
      logs: true,
      version: '7.23.5',
    },
  })
function installDOM() {
  const dom = new JSDOM(
    '<!doctype html><html><body><div id="root"></div></body></html>',
    { url: 'http://localhost/' },
  )
  const previous = {
    window: globalThis.window,
    document: globalThis.document,
    HTMLElement: globalThis.HTMLElement,
    Event: globalThis.Event,
    fetch: globalThis.fetch,
  }
  Object.assign(globalThis, {
    window: dom.window,
    document: dom.window.document,
    HTMLElement: dom.window.HTMLElement,
    Event: dom.window.Event,
  })
  dom.window.HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  dom.window.HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open')
  }
  return {
    dom,
    restore: () => {
      Object.assign(globalThis, previous)
      dom.window.close()
    },
  }
}
const response = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status })
const settle = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0))
  })
function button(text: string) {
  const b = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent?.includes(text),
  )
  assert.ok(b, `missing ${text}`)
  return b
}
function field(label: string) {
  const f = [...document.querySelectorAll<HTMLLabelElement>('.ct-field')]
    .find((f) => f.querySelector('span')?.textContent?.startsWith(label))
    ?.querySelector<
      HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
    >('input,select,textarea')
  assert.ok(f, `missing ${label}`)
  return f
}

test('API parsing fails closed and new defaults never preselect a network', () => {
  const d = newDraft()
  assert.equal(d.image, '')
  assert.equal(d.network.bridge, '')
  assert.equal(d.network.veth, '')
  assert.equal(d.startOnBoot, true)
  assert.equal(d.startAfterCreate, true)
  assert.equal(d.logging, true)
  assert.equal(d.restartPolicy, 'no')
  assert.equal(d.health.mode, 'inherit')
  assert.notEqual(d.draftId, newDraft().draftId)
  assert.equal(
    normalizedImage('registry:5000/path/image'),
    'registry:5000/path/image:latest',
  )
  assert.equal(normalizedImage('image@sha256:abcdef'), 'image@sha256:abcdef')
  const invalid = parseSnapshot({
    capabilities: { writes: 'true', supported: true },
  })
  assert.equal(invalid.capabilities.writes, false)
  assert.deepEqual(invalid.items, [])
  assert.equal(
    parseSnapshot({
      capabilities: { writes: true, supported: true, mode: 'read-only' },
    }).capabilities.writes,
    false,
  )
  const item = existing(),
    draft = editDraft(item)
  assert.equal(draft.logging, false)
  assert.equal(draft.startOnBoot, false)
  assert.equal(draft.command, 'serve --exact')
  assert.equal(draft.restartPolicy, 'on-failure')
  draft.env[0].value = 'changed'
  assert.notEqual(item.config.env[0].value, 'changed')
  assert.deepEqual(
    parseResolution({
      effective: newDraft(),
      errors: { image: 'required', unsafe: 1 },
    }).errors,
    { image: 'required' },
  )
})

test('reads/actions preserve encoded device scope and JSON special characters', async () => {
  const previous = globalThis.fetch
  const calls: { path: string; init?: RequestInit }[] = []
  globalThis.fetch = async (path, init) => {
    calls.push({ path: String(path), init })
    return response(
      init?.method === 'POST'
        ? { id: 'j', deviceId: 'device / 2', state: 'queued' }
        : snapshot(),
    )
  }
  try {
    await fetchContainers('device / 2')
    const d = newDraft()
    d.env = [{ key: 'VALUE', value: ' space " \\ $() ; \n 中文 ' }]
    await performAction('device / 2', 'create', '', 'request-1', d)
    assert.match(calls[0].path, /device=device%20%2F%202/)
    assert.equal(calls[0].init?.credentials, 'same-origin')
    assert.equal(
      JSON.parse(String(calls[1].init?.body)).draft.env[0].value,
      d.env[0].value,
    )
  } finally {
    globalThis.fetch = previous
  }
})

test('flat editor shows every section, keeps required errors inline and retains the draft', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  let submitted = 0,
    lastDraft: Draft | undefined
  globalThis.fetch = async (_path, init) => {
    lastDraft = JSON.parse(String(init?.body)) as Draft
    return response({
      effective: lastDraft,
      errors: {
        image: '请填写镜像',
        'network.veth': '请填写 VETH',
        'network.bridge': '请选择 bridge',
        'network.address': '填写静态 IP',
        'network.gateway': '填写网关',
      },
      defaults: ['内存继承全局'],
      containerFields: {},
    })
  }
  try {
    const d = newDraft()
    d.env = [{ key: 'SPECIAL', value: ' text "$; ' }]
    await act(async () =>
      root.render(
        <ContainerEditor
          deviceId="a"
          snapshot={snapshot()}
          initial={d}
          item={null}
          busy={false}
          onClose={() => {}}
          onSubmit={async () => {
            submitted++
          }}
        />,
      ),
    )
    assert.deepEqual(
      [...document.querySelectorAll('.ct-section h3')].map(
        (e) => e.textContent,
      ),
      [
        '镜像',
        '网络',
        '存储与挂载',
        '端口映射',
        '环境变量',
        '启动配置',
        '资源限制',
        '健康检查',
      ],
    )
    assert.equal(document.querySelector('[role="tablist"]'), null)
    assert.equal(document.querySelector('details'), null)
    assert.doesNotMatch(document.body.textContent!, /下一步/)
    assert.equal(field('已有 bridge').value, '')
    assert.equal(
      document.querySelectorAll<HTMLInputElement>('.ct-check input:checked')
        .length,
      3,
    )
    await act(async () => button('校验配置').click())
    assert.equal(submitted, 0)
    assert.match(document.body.textContent!, /请填写镜像/)
    assert.equal(field('变量值').value, d.env[0].value)
    assert.equal(lastDraft?.env[0].value, d.env[0].value)
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('edit retains values and shared VETH fields are locked without hiding sections', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  const item = existing()
  let received: Draft | undefined
  globalThis.fetch = async (_p, init) => {
    received = JSON.parse(String(init?.body))
    return response({
      effective: received,
      errors: {},
      defaults: [],
      containerFields: {},
    })
  }
  try {
    await act(async () =>
      root.render(
        <ContainerEditor
          deviceId="a"
          snapshot={snapshot()}
          initial={editDraft(item)}
          item={item}
          busy={false}
          onClose={() => {}}
          onSubmit={async (d) => {
            received = d
          }}
        />,
      ),
    )
    assert.equal(field('VETH 名称').disabled, true)
    assert.equal(field('IPv4 网关').disabled, true)
    assert.equal(field('命令 CMD').value, 'serve --exact')
    assert.equal(field('根目录').value, '/sata1/existing')
    assert.equal(field('变量值').value, item.config.env[0].value)
    assert.equal(field('自动重启策略').value, 'on-failure')
    assert.equal(document.querySelectorAll('.ct-section').length, 8)
    await act(async () => button('校验配置').click())
    assert.equal(received?.logging, false)
    assert.equal(received?.startOnBoot, false)
    assert.equal(received?.mounts[0].readOnly, true)
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

for (const [name, Page] of [
  ['Aurora contract', ContainerPage],
  ['Compact', CompactContainers],
] as const)
  test(`${name}: production capabilities disable every RouterOS write while keeping logs and configuration readable`, async () => {
    const env = installDOM(),
      root = createRoot(document.getElementById('root')!)
    globalThis.fetch = async () => response(snapshot())
    try {
      await act(async () => root.render(<Page deviceId="a" />))
      await settle()
      for (const action of ['启动 dns', '重启 dns', '接管 dns'])
        assert.equal(
          document.querySelector<HTMLButtonElement>(`[aria-label="${action}"]`)
            ?.disabled,
          true,
        )
      assert.equal(
        document.querySelector<HTMLButtonElement>('[aria-label="日志 dns"]')
          ?.disabled,
        false,
      )
      await act(async () => button('配置预览').click())
      assert.equal(document.querySelectorAll('.ct-section').length, 8)
    } finally {
      await act(async () => root.unmount())
      env.restore()
    }
  })

test('device remount cancels old reads and resets draft/log/job state', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  let pending: ((value: Response) => void) | undefined
  const signals: (AbortSignal | null | undefined)[] = []
  globalThis.fetch = async (path, init) => {
    signals.push(init?.signal)
    if (String(path).includes('device=a'))
      return await new Promise<Response>((resolve) => {
        pending = resolve
      })
    const s = snapshot()
    s.items[0].name = 'only-b'
    return response(s)
  }
  try {
    await act(async () => root.render(<ContainerPage key="a" deviceId="a" />))
    await act(async () => root.render(<ContainerPage key="b" deviceId="b" />))
    await settle()
    await act(async () => pending?.(response(snapshot())))
    assert.equal(signals[0]?.aborted, true)
    assert.match(document.body.textContent!, /only-b/)
    assert.doesNotMatch(document.body.textContent!, /创建容器|dns:v1.*dns/)
    assert.equal(document.querySelector('.ct-editor'), null)
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('pending unknown task is restored and recovered without another mutation', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  const calls: string[] = []
  const s = snapshot(true)
  s.activeJob = {
    id: 'old',
    deviceId: 'a',
    action: 'create',
    targetId: '*created',
    state: 'unknown',
    phase: '等待回读',
    progress: 85,
    error: '结果不明',
    retained: [],
  }
  globalThis.fetch = async (path, init) => {
    calls.push(`${init?.method} ${path}`)
    if (String(path).includes('/recover')) {
      s.activeJob = {
        ...s.activeJob!,
        state: 'succeeded',
        phase: '回读确认',
        progress: 100,
      }
      return response(s.activeJob)
    }
    return response(s)
  }
  try {
    await act(async () => root.render(<ContainerPage deviceId="a" />))
    await settle()
    assert.equal(button('创建容器').disabled, true)
    assert.match(document.body.textContent!, /等待回读/)
    await act(async () => button('回读确认结果').click())
    await settle()
    assert.ok(calls.some((c) => c.includes('/jobs/old/recover?device=a')))
    assert.ok(calls.every((c) => !c.includes('/actions')))
    assert.equal(button('创建容器').disabled, false)
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('local archive upload uses multipart, keeps device scope and authenticates errors', async () => {
  const previous = globalThis.fetch
  const calls: { path: string; init?: RequestInit }[] = []
  const { uploadImage, parseDraft } = await import(
    '../src/features/containers/api.ts'
  )
  globalThis.fetch = async (path, init) => {
    calls.push({ path: String(path), init })
    return response(
      {
        id: 'archive-a',
        name: 'image.tar',
        reference: 'local/app:v1',
        architecture: 'amd64',
        bytes: 4,
        sha256: 'a'.repeat(64),
        remotePath: '/sata1/images/a.tar',
      },
      201,
    )
  }
  try {
    const image = new File(['data'], 'image.tar', { type: 'application/x-tar' })
    const result = await uploadImage('device / 2', image)
    assert.match(calls[0].path, /images\/upload\?device=device%20%2F%202/)
    assert.ok(calls[0].init?.body instanceof FormData)
    assert.equal(calls[0].init?.headers, undefined)
    assert.equal(calls[0].init?.credentials, 'same-origin')
    assert.equal(result.reference, 'local/app:v1')
    assert.equal(parseDraft({}).imageSource, 'registry')
    assert.equal(
      parseDraft({
        imageSource: 'archive',
        archiveId: 'a',
        archiveFile: 'sata1/a.tar',
      }).archiveFile,
      'sata1/a.tar',
    )
  } finally {
    globalThis.fetch = previous
  }
})

test('directory picker navigates actual names, creates a child and selects it without changing other fields', async () => {
  const { DirectoryPicker } = await import(
    '../src/features/containers/DirectoryPicker.tsx'
  )
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  let selected = '',
    created: unknown
  globalThis.fetch = async (url, init) => {
    if (init?.method === 'POST') {
      created = JSON.parse(String(init.body))
      return response({ path: '/sata1/rootfs' }, 201)
    }
    const path = new URL(String(url), 'http://localhost').searchParams.get(
      'path',
    )!
    return response({
      path,
      entries:
        path === '/'
          ? [{ name: 'sata1', path: '/sata1', directory: true }]
          : path === '/sata1'
            ? [
                {
                  name: 'config.yaml',
                  path: '/sata1/config.yaml',
                  directory: false,
                  bytes: 128,
                },
              ]
            : [],
    })
  }
  try {
    await act(async () =>
      root.render(
        <DirectoryPicker
          deviceId="a"
          writable
          purpose="根目录"
          onClose={() => {}}
          onSelect={(p) => {
            selected = p
          }}
        />,
      ),
    )
    await settle()
    assert.equal(button('选用此目录').disabled, true)
    await act(async () => button('sata1').click())
    await settle()
    assert.ok(document.body.textContent?.includes('config.yaml'))
    assert.equal(
      [...document.querySelectorAll('button')].some((b) =>
        b.textContent?.includes('选用文件'),
      ),
      false,
    )
    await act(async () => {
      const input = field('新文件夹名称') as HTMLInputElement
      // Trigger React's controlled input handler without importing a testing framework.
      Object.getOwnPropertyDescriptor(
        env.dom.window.HTMLInputElement.prototype,
        'value',
      )!.set!.call(input, 'rootfs')
      input.dispatchEvent(new env.dom.window.Event('input', { bubbles: true }))
    })
    await act(async () => button('新建文件夹').click())
    await settle()
    assert.deepEqual(created, { parent: '/sata1', name: 'rootfs' })
    await act(async () => button('选用此目录').click())
    assert.equal(selected, '/sata1/rootfs')
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('readonly local upload stays disabled while directory selection updates only the chosen mount', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  const draft = newDraft()
  draft.mounts = [
    { source: '/sata1/original', target: '/etc/app', readOnly: true },
  ]
  globalThis.fetch = async (url, init) => {
    if (String(url).includes('/directories')) {
      const path = new URL(String(url), 'http://localhost').searchParams.get(
        'path',
      )!
      return response({
        path,
        entries:
          path === '/'
            ? [{ name: 'sata1', path: '/sata1', directory: true }]
            : [],
      })
    }
    return response({
      effective: JSON.parse(String(init?.body)),
      errors: {},
      defaults: [],
    })
  }
  try {
    await act(async () =>
      root.render(
        <ContainerEditor
          deviceId="a"
          snapshot={snapshot()}
          initial={draft}
          item={null}
          busy={false}
          onClose={() => {}}
          onSubmit={async () => {}}
        />,
      ),
    )
    await act(async () => {
      const source = field('镜像来源')
      source.value = 'archive'
      source.dispatchEvent(new Event('change', { bubbles: true }))
    })
    assert.equal((field('本地镜像归档') as HTMLInputElement).disabled, true)
    assert.match(document.body.textContent!, /真实镜像上传尚未启用/)
    await act(async () => button('浏览挂载源').click())
    await settle()
    await act(async () => button('sata1').click())
    await settle()
    assert.equal(
      [...document.querySelectorAll('button')].some(
        (b) => b.textContent === '新建文件夹',
      ),
      false,
    )
    await act(async () => button('选用此目录').click())
    assert.equal(field('主机源目录').value, '/sata1')
    assert.equal(field('容器目标目录').value, '/etc/app')
    assert.equal(field('根目录').value, '')
    assert.equal(
      (document.querySelector('.ct-check input') as HTMLInputElement).checked,
      true,
    )
    assert.equal(document.querySelectorAll('.ct-section').length, 8)
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})
