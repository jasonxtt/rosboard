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
import { DirectoryPicker } from '../src/features/containers/DirectoryPicker.tsx'
import {
  directoryNameError,
  directoryPath,
  replaceDirectory,
} from '../src/features/containers/directoryPaths.ts'
import type {
  Draft,
  DirectoryEntry,
  DirectoryMutation,
  DirectoryRequest,
} from '../src/features/containers/types.ts'

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
      directoryWrites: writes,
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
  const f = [...document.querySelectorAll<HTMLElement>('.ct-field')]
    .find((f) =>
      (
        f.querySelector('span')?.textContent ||
        f.querySelector('label')?.textContent
      )?.startsWith(label),
    )
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
        '环境变量',
        '启动配置',
        '资源限制',
        '健康检查',
      ],
    )
    assert.equal(document.querySelector('[role="tablist"]'), null)
    assert.equal(document.querySelector('details'), null)
    assert.doesNotMatch(document.body.textContent!, /下一步|端口映射|dst-nat/)
    assert.match(document.body.textContent!, /通过容器 IP:应用端口访问/)
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
    assert.equal(document.querySelector('.ct-startup-advanced'), null)
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[aria-label="启动高级设置"]')!
        .click(),
    )
    assert.equal(field('命令 CMD').value, 'serve --exact')
    assert.equal(field('容器运行目录').value, '/sata1/existing')
    assert.equal(field('变量值').value, item.config.env[0].value)
    assert.equal(field('自动重启策略').value, 'on-failure')
    assert.equal(document.querySelectorAll('.ct-section').length, 7)
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
      assert.equal(document.querySelectorAll('thead th').length, 10)
      assert.doesNotMatch(
        document.querySelector('thead')!.textContent!,
        /端口映射/,
      )
      assert.match(document.querySelector('thead')!.textContent!, /IP \/ VETH/)
      await act(async () => button('配置预览').click())
      assert.equal(document.querySelectorAll('.ct-section').length, 7)
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
    const image = new File(['data'], 'image.tar', {
      type: 'application/x-tar',
    })
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
    await act(async () => button('选择此目录').click())
    assert.equal(field('主机源目录').value, '/sata1')
    assert.equal(field('容器目标目录').value, '/etc/app')
    assert.equal(field('容器运行目录').value, '')
    assert.equal(
      (document.querySelector('.ct-check input') as HTMLInputElement).checked,
      true,
    )
    assert.equal(document.querySelectorAll('.ct-section').length, 7)
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('advanced network starts collapsed, preserves values and reveals invalid hidden fields on submit', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  const draft = newDraft()
  draft.network.address6 = 'fd00::2/64'
  draft.network.gateway6 = 'fd00::1'
  draft.network.mac = 'invalid'
  let submitted: Draft | undefined
  globalThis.fetch = async (_url, init) => {
    const d = JSON.parse(String(init?.body)) as Draft
    return response({
      effective: d,
      errors:
        d.network.mac === 'invalid' ? { 'network.mac': 'MAC 格式无效' } : {},
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
          onSubmit={async (d) => {
            submitted = d
          }}
        />,
      ),
    )
    const toggle = button('高级设置')
    assert.equal(toggle.getAttribute('aria-expanded'), 'false')
    assert.equal(document.querySelector('.ct-network-advanced'), null)
    await act(async () => toggle.click())
    assert.equal(field('IPv6 / 掩码').value, draft.network.address6)
    assert.equal(field('自定义 MAC').value, draft.network.mac)
    await act(async () => toggle.click())
    await act(async () => button('校验配置').click())
    assert.equal(submitted, undefined)
    assert.equal(toggle.getAttribute('aria-expanded'), 'true')
    assert.match(document.body.textContent!, /MAC 格式无效/)
    await act(async () => {
      const input = field('自定义 MAC') as HTMLInputElement
      Object.getOwnPropertyDescriptor(
        env.dom.window.HTMLInputElement.prototype,
        'value',
      )!.set!.call(input, '02:00:00:00:00:02')
      input.dispatchEvent(new env.dom.window.Event('input', { bubbles: true }))
    })
    await act(async () => toggle.click())
    await act(async () => button('校验配置').click())
    assert.equal(submitted?.network.address6, draft.network.address6)
    assert.equal(submitted?.network.gateway6, draft.network.gateway6)
    assert.equal(submitted?.network.mac, '02:00:00:00:00:02')
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('runtime input supports typing and inline Files browsing without changing mounts', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  const draft = newDraft()
  draft.mounts = [
    { source: '/sata1/data/config', target: '/etc/app', readOnly: true },
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
            : path === '/sata1'
              ? [{ name: 'nginx', path: '/sata1/nginx', directory: true }]
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
    const input = field('容器运行目录') as HTMLInputElement
    assert.equal(
      [...document.querySelectorAll('button')].some((b) =>
        b.textContent?.includes('浏览运行目录'),
      ),
      false,
    )
    await act(async () => input.click())
    await settle()
    const picker = document.getElementById(
      input.getAttribute('aria-controls')!,
    )!
    assert.ok(picker?.classList.contains('ct-directory-picker'))
    assert.equal(input.closest('.ct-field')!.nextElementSibling, picker)
    await act(async () => {
      Object.getOwnPropertyDescriptor(
        env.dom.window.HTMLInputElement.prototype,
        'value',
      )!.set!.call(input, '/sata1/manual')
      input.dispatchEvent(new env.dom.window.Event('input', { bubbles: true }))
    })
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[aria-label="关闭目录浏览"]')!
        .click(),
    )
    assert.equal(input.value, '/sata1/manual')
    await act(async () =>
      input.dispatchEvent(
        new env.dom.window.KeyboardEvent('keydown', {
          key: 'ArrowDown',
          bubbles: true,
        }),
      ),
    )
    await settle()
    await act(async () => button('sata1').click())
    await settle()
    await act(async () => button('nginx').click())
    await settle()
    await act(async () => button('选择此目录').click())
    assert.equal(input.value, '/sata1/nginx')
    assert.equal(document.querySelector('.ct-directory-picker'), null)
    assert.equal(field('主机源目录').value, draft.mounts[0].source)
    assert.equal(field('容器目标目录').value, draft.mounts[0].target)
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('inline environment delete removes only its own row and retains verbatim values', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  const draft = newDraft()
  draft.env = [
    { key: 'FIRST', value: 'remove' },
    { key: 'SECOND', value: ' spaces "$;\n中文 ' },
  ]
  let submitted: Draft | undefined
  globalThis.fetch = async (_url, init) =>
    response({
      effective: JSON.parse(String(init?.body)),
      errors: {},
      defaults: [],
    })
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
          onSubmit={async (d) => {
            submitted = d
          }}
        />,
      ),
    )
    const remove = document.querySelector<HTMLButtonElement>(
      '[aria-label="删除变量 1"]',
    )!
    assert.ok(remove.closest('.ct-env-row'))
    assert.match(remove.textContent!, /🗑/)
    await act(async () => remove.click())
    assert.equal(field('变量名').value, 'SECOND')
    assert.equal(field('变量值').value, draft.env[1].value)
    assert.equal(document.querySelectorAll('.ct-env-row').length, 1)
    await act(async () => button('校验配置').click())
    assert.deepEqual(submitted?.env, [draft.env[1]])
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('startup controls precede collapsed advanced fields and preserve all overrides through submit', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  const draft = newDraft()
  Object.assign(draft, {
    command: 'serve --original',
    entrypoint: '/app/bin/server',
    user: '1000:1000',
    workdir: '/app/data',
  })
  let submitted: Draft | undefined
  globalThis.fetch = async (_url, init) =>
    response({
      effective: JSON.parse(String(init?.body)),
      errors: {},
      defaults: [],
    })
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
          onSubmit={async (d) => {
            submitted = d
          }}
        />,
      ),
    )
    const toggle = document.querySelector<HTMLButtonElement>(
      '[aria-label="启动高级设置"]',
    )!
    const section = toggle.closest('section')!
    assert.equal(toggle.getAttribute('aria-expanded'), 'false')
    assert.equal(section.querySelector('.ct-startup-advanced'), null)
    assert.equal(section.querySelectorAll('.ct-check input').length, 3)
    assert.equal(
      section.querySelector('.ct-section-body')!.lastElementChild,
      toggle,
    )
    await act(async () => toggle.click())
    assert.equal(field('命令 CMD').value, draft.command)
    assert.equal(field('入口 ENTRYPOINT').value, draft.entrypoint)
    assert.equal(field('用户').value, draft.user)
    assert.equal(field('工作目录').value, draft.workdir)
    await act(async () => {
      const input = field('命令 CMD') as HTMLInputElement
      Object.getOwnPropertyDescriptor(
        env.dom.window.HTMLInputElement.prototype,
        'value',
      )!.set!.call(input, 'serve --changed')
      input.dispatchEvent(new env.dom.window.Event('input', { bubbles: true }))
    })
    await act(async () => toggle.click())
    await act(async () => button('校验配置').click())
    assert.equal(submitted?.command, 'serve --changed')
    assert.equal(submitted?.entrypoint, draft.entrypoint)
    assert.equal(submitted?.user, draft.user)
    assert.equal(submitted?.workdir, draft.workdir)
    await act(async () => toggle.click())
    assert.equal(field('命令 CMD').value, 'serve --changed')
    assert.equal(
      document
        .querySelector('[aria-label="网络高级设置"]')!
        .getAttribute('aria-expanded'),
      'false',
    )
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('health check choices gate custom settings and retain the command when switching back to image defaults', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!)
  const draft = newDraft()
  let submitted: Draft | undefined
  globalThis.fetch = async (_url, init) => {
    const d = JSON.parse(String(init?.body)) as Draft
    return response({
      effective: d,
      defaults: [],
      errors:
        d.health.mode === 'override' && !d.health.command
          ? { 'health.command': '自定义检查时请填写在容器内执行的命令' }
          : {},
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
          onSubmit={async (d) => {
            submitted = d
          }}
        />,
      ),
    )
    const mode = field('检查方式') as HTMLSelectElement
    assert.equal(mode.value, 'inherit')
    assert.doesNotMatch(
      document.body.textContent!,
      /自定义检查需填写命令|检查间隔|启动准备时间/,
    )
    assert.match(document.body.textContent!, /镜像未提供检查时，默认不会检查/)
    assert.doesNotMatch(
      document.body.textContent!,
      /CMD-SHELL|启动宽限期|覆盖镜像检查/,
    )
    await act(async () => {
      mode.value = 'override'
      mode.dispatchEvent(new env.dom.window.Event('change', { bubbles: true }))
    })
    assert.ok(
      field('检查命令').closest('label')!.querySelector('[aria-label="必填"]'),
    )
    for (const label of [
      '检查命令',
      '检查间隔',
      '单次检查超时',
      '连续失败次数',
      '启动准备时间',
    ])
      assert.equal(field(label).disabled, false)
    await act(async () => button('校验配置').click())
    assert.equal(submitted, undefined)
    assert.match(document.body.textContent!, /请填写在容器内执行的命令/)
    await act(async () => {
      const input = field('检查命令') as HTMLInputElement
      Object.getOwnPropertyDescriptor(
        env.dom.window.HTMLInputElement.prototype,
        'value',
      )!.set!.call(input, 'curl -f http://127.0.0.1:80/')
      input.dispatchEvent(new env.dom.window.Event('input', { bubbles: true }))
    })
    await act(async () => button('校验配置').click())
    assert.equal(submitted?.health.mode, 'override')
    assert.equal(submitted?.health.command, 'curl -f http://127.0.0.1:80/')
    await act(async () => {
      mode.value = 'inherit'
      mode.dispatchEvent(new env.dom.window.Event('change', { bubbles: true }))
    })
    assert.doesNotMatch(
      document.body.textContent!,
      /自定义检查需填写命令|检查间隔|启动准备时间/,
    )
    await act(async () => button('校验配置').click())
    assert.equal(submitted?.health.mode, 'inherit')
    assert.equal(submitted?.health.command, 'curl -f http://127.0.0.1:80/')
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

function typeInput(input: HTMLInputElement, value: string, dom: JSDOM) {
  Object.getOwnPropertyDescriptor(
    dom.window.HTMLInputElement.prototype,
    'value',
  )!.set!.call(input, value)
  input.dispatchEvent(new dom.window.Event('input', { bubbles: true }))
}
function keyInput(input: HTMLInputElement, key: string, dom: JSDOM) {
  input.dispatchEvent(
    new dom.window.KeyboardEvent('keydown', { key, bubbles: true }),
  )
}
function inlineName(label = '新文件夹名称') {
  const input = document.querySelector<HTMLInputElement>(
    `[aria-label="${label}"]`,
  )
  assert.ok(input)
  return input
}
function directoryFetchFixture() {
  let counter = 16
  const entries: DirectoryEntry[] = [
    {
      id: '*1',
      name: 'sata1',
      path: '/sata1',
      directory: true,
      bytes: 0,
      protected: '磁盘挂载点',
    },
    {
      id: '*2',
      name: 'docker',
      path: '/sata1/docker',
      directory: true,
      bytes: 0,
      protected: '',
    },
    {
      id: '*3',
      name: 'data',
      path: '/sata1/docker/data',
      directory: true,
      bytes: 0,
      protected: '',
    },
    {
      id: '*4',
      name: 'config.yaml',
      path: '/sata1/docker/config.yaml',
      directory: false,
      bytes: 1024,
      protected: '',
    },
    {
      id: '*5',
      name: 'child',
      path: '/sata1/docker/data/child',
      directory: true,
      bytes: 0,
      protected: '',
    },
  ]
  const requests: DirectoryRequest[] = [],
    reads: string[] = []
  const fetcher: typeof fetch = async (url, init) => {
    if (init?.method === 'POST') {
      const req = JSON.parse(String(init.body)) as DirectoryRequest
      requests.push(req)
      const target =
        req.action === 'mkdir'
          ? req.parent + '/' + req.name
          : req.action === 'rename'
            ? req.path!.slice(0, req.path!.lastIndexOf('/')) + '/' + req.name
            : req.path!
      if (req.action === 'mkdir')
        entries.push({
          id: '*' + (++counter).toString(16),
          name: req.name!,
          path: target,
          directory: true,
          protected: '',
          bytes: 0,
        })
      if (req.action === 'rename')
        for (const entry of entries) {
          if (
            entry.path === req.path ||
            entry.path.startsWith(req.path + '/')
          ) {
            entry.path = target + entry.path.slice(req.path!.length)
            entry.name = entry.path.slice(entry.path.lastIndexOf('/') + 1)
            entry.id = '*' + (++counter).toString(16)
          }
        }
      if (req.action === 'delete')
        for (let i = entries.length - 1; i >= 0; i--)
          if (
            entries[i].path === target ||
            entries[i].path.startsWith(target + '/')
          )
            entries.splice(i, 1)
      return response(
        {
          action: req.action,
          requestId: req.requestId,
          path: target,
          previousPath: req.path || '',
          state: 'succeeded',
        },
        req.action === 'mkdir' ? 201 : 200,
      )
    }
    const path = new URL(String(url), 'http://localhost').searchParams.get(
      'path',
    )!
    reads.push(path)
    if (path !== '/' && !entries.some((e) => e.path === path && e.directory))
      return response({ code: 'directory_not_found', error: '目录不存在' }, 404)
    return response({
      path,
      id: entries.find((e) => e.path === path)?.id || '',
      canCreate: path !== '/',
      entries: entries
        .filter(
          (e) => (e.path.slice(0, e.path.lastIndexOf('/')) || '/') === path,
        )
        .sort(
          (a, b) =>
            Number(b.directory) - Number(a.directory) ||
            a.name.localeCompare(b.name),
        ),
    })
  }
  return { entries, requests, reads, fetcher }
}
async function renderPicker(initialPath = '/sata1/docker', allowFiles = false) {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!),
    fixture = directoryFetchFixture()
  const values = {
    selected: '',
    closed: false,
    mutations: [] as DirectoryMutation[],
  }
  globalThis.fetch = fixture.fetcher
  await act(async () =>
    root.render(
      <DirectoryPicker
        deviceId="test"
        writable
        initialPath={initialPath}
        purpose="容器运行目录"
        allowFiles={allowFiles}
        onSelect={(p) => {
          values.selected = p
        }}
        onClose={() => {
          values.closed = true
        }}
        onMutation={(r) => values.mutations.push(r)}
      />,
    ),
  )
  await settle()
  return {
    ...env,
    root,
    fixture,
    values,
    cleanup: async () => {
      await act(async () => root.unmount())
      env.restore()
    },
  }
}

test('directory paths reject traversal and preserve Unicode and spaces when synchronizing inputs', () => {
  assert.equal(directoryNameError('配置 文件夹'), '')
  for (const name of ['', '.', '..', 'a/b', 'a\\b', 'bad\tname'])
    assert.ok(directoryNameError(name))
  assert.equal(
    directoryPath('sata1/docker/配置 文件夹'),
    '/sata1/docker/配置 文件夹',
  )
  assert.equal(directoryPath('/sata1/../escape'), null)
  assert.equal(
    replaceDirectory(
      '/sata1/docker/data/config',
      '/sata1/docker/data',
      '/sata1/docker/renamed',
    ),
    '/sata1/docker/renamed/config',
  )
  assert.equal(
    replaceDirectory('/sata1/docker/database', '/sata1/docker/data', ''),
    '/sata1/docker/database',
  )
})

test('bound picker opens at current path, browses ancestors and root, and keeps files unselectable', async () => {
  const env = await renderPicker('sata1/docker')
  try {
    assert.equal(env.fixture.reads[0], '/sata1/docker')
    assert.ok(document.body.textContent?.includes('1.0 KiB'))
    assert.equal(
      document.querySelector('.ct-directory-picker header strong'),
      null,
    )
    assert.equal(
      document.querySelectorAll('.ct-directory-file button').length,
      0,
    )
    assert.ok(
      document.querySelector(
        '.ct-directory-row:first-child .ct-directory-folder',
      ),
    )
    await act(async () => button('data').click())
    await settle()
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('nav [aria-current="location"]')!
        .click(),
    )
    await settle()
    await act(async () => button('docker').click())
    await settle()
    assert.equal(env.fixture.reads.at(-1), '/sata1/docker')
    await act(async () =>
      document.querySelector<HTMLButtonElement>('nav button')!.click(),
    )
    await settle()
    assert.equal(button('选择此目录').disabled, true)
    assert.equal(button('新建文件夹').disabled, true)
    assert.equal(
      document.querySelector<HTMLButtonElement>(
        '[aria-label="文件夹 sata1 操作"]',
      )!.disabled,
      true,
    )
    await act(async () => button('sata1').click())
    await settle()
    await act(async () => button('选择此目录').click())
    assert.equal(env.values.selected, '/sata1')
  } finally {
    await env.cleanup()
  }
})

test('inline creation focuses/selects a draft row and Enter saves once while remaining in parent', async () => {
  const env = await renderPicker()
  try {
    await act(async () => button('新建文件夹').click())
    const input = inlineName()
    assert.equal(document.activeElement, input)
    assert.equal(input.selectionStart, 0)
    assert.equal(input.selectionEnd, input.value.length)
    assert.equal(env.fixture.requests.length, 0)
    await act(async () => typeInput(input, '配置 文件夹', env.dom))
    await act(async () => {
      keyInput(input, 'Enter', env.dom)
      keyInput(input, 'Enter', env.dom)
    })
    await settle()
    assert.equal(env.fixture.requests.length, 1)
    assert.equal(env.fixture.requests[0].action, 'mkdir')
    assert.equal(env.fixture.requests[0].expectedId, '*2')
    assert.equal(env.fixture.requests[0].name, '配置 文件夹')
    assert.equal(env.fixture.reads.at(-1), '/sata1/docker')
    await act(async () => button('配置 文件夹').click())
    await settle()
    await act(async () => button('选择此目录').click())
    assert.equal(env.values.selected, '/sata1/docker/配置 文件夹')
  } finally {
    await env.cleanup()
  }
})

test('inline creation Escape cancels, invalid blur stays local, and valid blur commits', async () => {
  const env = await renderPicker()
  try {
    await act(async () => button('新建文件夹').click())
    await act(async () => keyInput(inlineName(), 'Escape', env.dom))
    assert.equal(env.fixture.requests.length, 0)
    assert.equal(document.querySelector('[aria-label="新文件夹名称"]'), null)
    await act(async () => button('新建文件夹').click())
    const input = inlineName()
    await act(async () => typeInput(input, '../bad', env.dom))
    await act(async () => {
      input.blur()
      keyInput(input, 'Enter', env.dom)
    })
    assert.equal(env.fixture.requests.length, 0)
    assert.ok(input.getAttribute('aria-invalid'))
    await act(async () => {
      input.focus()
      typeInput(input, 'blur-saved', env.dom)
    })
    await act(async () => input.blur())
    await settle()
    assert.equal(env.fixture.requests.length, 1)
    assert.equal(env.fixture.requests[0].name, 'blur-saved')
  } finally {
    await env.cleanup()
  }
})

test('inline rename Escape retains name, successful rename retains children, and failure restores original', async () => {
  const env = await renderPicker()
  const actions = () =>
    document.querySelector<HTMLButtonElement>(
      '[aria-label="文件夹 data 操作"]',
    )!
  try {
    await act(async () => actions().click())
    await act(async () => button('重命名').click())
    const input = inlineName('重命名文件夹')
    assert.equal(document.activeElement, input)
    assert.equal(input.selectionEnd, 4)
    await act(async () => keyInput(input, 'Escape', env.dom))
    assert.equal(env.fixture.requests.length, 0)
    await act(async () => actions().click())
    await act(async () => button('重命名').click())
    globalThis.fetch = async (url, init) =>
      init?.method === 'POST'
        ? response(
            { code: 'directory_permission_denied', error: '目录权限不足' },
            403,
          )
        : env.fixture.fetcher(url, init)
    await act(async () =>
      typeInput(inlineName('重命名文件夹'), 'rejected', env.dom),
    )
    await act(async () =>
      keyInput(inlineName('重命名文件夹'), 'Enter', env.dom),
    )
    await settle()
    assert.equal(document.querySelector('[aria-label="重命名文件夹"]'), null)
    assert.ok(actions())
    assert.match(document.body.textContent!, /目录权限不足/)
    globalThis.fetch = env.fixture.fetcher
    await act(async () => actions().click())
    await act(async () => button('重命名').click())
    await act(async () =>
      typeInput(inlineName('重命名文件夹'), 'renamed', env.dom),
    )
    await act(async () =>
      keyInput(inlineName('重命名文件夹'), 'Enter', env.dom),
    )
    await settle()
    assert.ok(
      env.fixture.entries.some((e) => e.path === '/sata1/docker/renamed/child'),
    )
    assert.equal(env.values.mutations[0].previousPath, '/sata1/docker/data')
  } finally {
    await env.cleanup()
  }
})

test('nonempty directory deletion requires exact name/path warning and explicit confirmation', async () => {
  const env = await renderPicker()
  try {
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[aria-label="文件夹 data 操作"]')!
        .click(),
    )
    await act(async () => button('删除').click())
    const confirm = document.querySelector('[role="alertdialog"]')!
    assert.match(confirm.textContent!, /data/)
    assert.match(confirm.textContent!, /\/sata1\/docker\/data/)
    assert.match(confirm.textContent!, /全部文件和子目录/)
    assert.equal(env.fixture.requests.length, 0)
    await act(async () => button('取消').click())
    assert.equal(env.fixture.requests.length, 0)
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[aria-label="文件夹 data 操作"]')!
        .click(),
    )
    await act(async () => button('删除').click())
    await act(async () => button('删除全部内容').click())
    await settle()
    assert.equal(env.fixture.requests[0].confirmPath, '/sata1/docker/data')
    assert.equal(
      env.fixture.entries.some((e) => e.path.startsWith('/sata1/docker/data')),
      false,
    )
  } finally {
    await env.cleanup()
  }
})

test('invalid initial directory falls back to root without selecting or replacing a form value', async () => {
  for (const initial of ['/sata1/absent', '/sata1/../bad']) {
    const env = await renderPicker(initial)
    try {
      await settle()
      assert.equal(env.fixture.reads.at(-1), '/')
      assert.equal(env.values.selected, '')
      assert.match(document.body.textContent!, /原输入保留/)
      assert.equal(button('选择此目录').disabled, true)
    } finally {
      await env.cleanup()
    }
  }
})

test('unknown directory outcome disables further writes and recovers only by request ID', async () => {
  const env = await renderPicker()
  try {
    let original: DirectoryRequest | undefined
    globalThis.fetch = async (url, init) => {
      if (init?.method !== 'POST') return env.fixture.fetcher(url, init)
      const req = JSON.parse(String(init.body)) as DirectoryRequest
      if (req.action !== 'recover') {
        original = req
        await env.fixture.fetcher(url, init)
        return response(
          { code: 'directory_outcome_unknown', error: '尚未确认' },
          409,
        )
      }
      assert.equal(req.requestId, original?.requestId)
      return response({
        action: 'mkdir',
        requestId: req.requestId,
        path: '/sata1/docker/pending',
        state: 'succeeded',
      })
    }
    await act(async () => button('新建文件夹').click())
    await act(async () => typeInput(inlineName(), 'pending', env.dom))
    await act(async () => keyInput(inlineName(), 'Enter', env.dom))
    await settle()
    assert.equal(button('新建文件夹').disabled, true)
    assert.equal(button('选择此目录').disabled, true)
    assert.equal(button('刷新确认结果').disabled, false)
    await act(async () => button('刷新确认结果').click())
    await settle()
    assert.equal(env.fixture.requests.length, 1)
    assert.equal(button('新建文件夹').disabled, false)
  } finally {
    await env.cleanup()
  }
})

test('resource switch starts off, preserves hidden values, submits inheritance off, and retains existing limits on edit', async () => {
  for (const editing of [false, true]) {
    const env = installDOM(),
      root = createRoot(document.getElementById('root')!)
    const draft = editing ? editDraft(existing()) : newDraft()
    if (editing) {
      draft.memoryHigh = '128M'
      draft.memoryMax = '256M'
      draft.cpuList = '0,1'
    }
    let submitted: Draft | undefined
    globalThis.fetch = async (_url, init) =>
      response({
        effective: JSON.parse(String(init?.body)),
        errors: {},
        defaults: [],
      })
    try {
      await act(async () =>
        root.render(
          <ContainerEditor
            deviceId="test"
            snapshot={snapshot()}
            initial={draft}
            item={editing ? existing() : null}
            busy={false}
            onClose={() => {}}
            onSubmit={async (d) => {
              submitted = d
            }}
          />,
        ),
      )
      const toggle = document.querySelector<HTMLInputElement>(
        '.ct-resource-toggle input',
      )!
      assert.equal(toggle.checked, editing)
      if (!editing) {
        assert.doesNotMatch(
          document.body.textContent!,
          /内存 high|CPU 编号列表/,
        )
        await act(async () => toggle.click())
        await act(async () =>
          typeInput(field('内存 max') as HTMLInputElement, '256M', env.dom),
        )
      }
      assert.equal(field('内存 max').value, '256M')
      await act(async () => toggle.click())
      await act(async () => button('校验配置').click())
      assert.equal(submitted?.memoryHigh, '')
      assert.equal(submitted?.memoryMax, '')
      assert.equal(submitted?.cpuList, '')
      await act(async () => toggle.click())
      assert.equal(field('内存 max').value, '256M')
      await act(async () => button('校验配置').click())
      assert.equal(submitted?.memoryMax, '256M')
      if (editing) assert.equal(submitted?.cpuList, '0,1')
    } finally {
      await act(async () => root.unmount())
      env.restore()
    }
  }
})

test('real directory capability updates bound root and mount inputs after rename and preserves them on failed delete', async () => {
  const env = installDOM(),
    root = createRoot(document.getElementById('root')!),
    fixture = directoryFetchFixture()
  const draft = newDraft(),
    data = snapshot()
  draft.rootDir = '/sata1/docker/data'
  draft.mounts = [
    {
      source: '/sata1/docker/data/child',
      target: '/etc/config',
      readOnly: true,
    },
  ]
  data.capabilities.directoryWrites = true
  globalThis.fetch = async (url, init) =>
    String(url).includes('/directories')
      ? fixture.fetcher(url, init)
      : response({
          effective: JSON.parse(String(init?.body)),
          errors: {},
          defaults: [],
        })
  try {
    await act(async () =>
      root.render(
        <ContainerEditor
          deviceId="test"
          snapshot={data}
          initial={draft}
          item={null}
          busy={false}
          onClose={() => {}}
          onSubmit={async () => {}}
        />,
      ),
    )
    await act(async () => (field('容器运行目录') as HTMLInputElement).click())
    await settle()
    assert.equal(fixture.reads[0], draft.rootDir)
    await act(async () => button('docker').click())
    await settle()
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[aria-label="文件夹 data 操作"]')!
        .click(),
    )
    await act(async () => button('重命名').click())
    await act(async () =>
      typeInput(inlineName('重命名文件夹'), 'renamed', env.dom),
    )
    await act(async () =>
      keyInput(inlineName('重命名文件夹'), 'Enter', env.dom),
    )
    await settle()
    assert.equal(field('容器运行目录').value, '/sata1/docker/renamed')
    assert.equal(field('主机源目录').value, '/sata1/docker/renamed/child')
    assert.equal(field('容器目标目录').value, '/etc/config')
    const directoryFetch = globalThis.fetch
    globalThis.fetch = async (url, init) =>
      init?.method === 'POST' && String(url).includes('/directories')
        ? response(
            { code: 'directory_permission_denied', error: '删除权限不足' },
            403,
          )
        : directoryFetch(url, init)
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[aria-label="文件夹 renamed 操作"]')!
        .click(),
    )
    await act(async () => button('删除').click())
    await act(async () => button('删除全部内容').click())
    await settle()
    assert.match(document.body.textContent!, /删除权限不足/)
    assert.equal(field('容器运行目录').value, '/sata1/docker/renamed')
    assert.equal(field('主机源目录').value, '/sata1/docker/renamed/child')
    globalThis.fetch = directoryFetch
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[aria-label="文件夹 renamed 操作"]')!
        .click(),
    )
    await act(async () => button('删除').click())
    await act(async () => button('删除全部内容').click())
    await settle()
    assert.equal(field('容器运行目录').value, '')
    assert.equal(field('主机源目录').value, '')
    assert.equal(field('容器目标目录').value, '/etc/config')
    assert.equal(data.capabilities.writes, false)
  } finally {
    await act(async () => root.unmount())
    env.restore()
  }
})

test('mount source picker retains file selection while runtime picker excludes file actions', async () => {
  const env = await renderPicker('/sata1/docker', true)
  try {
    await act(async () => button('选用文件').click())
    assert.equal(env.values.selected, '/sata1/docker/config.yaml')
    assert.equal(
      document.querySelectorAll('.ct-directory-file [aria-label*="操作"]')
        .length,
      0,
    )
  } finally {
    await env.cleanup()
  }
})
