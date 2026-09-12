import assert from 'node:assert/strict'
import test from 'node:test'
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { JSDOM } from 'jsdom'
import { applicationPresetCatalogErrorMessage, fetchApplicationPresets, type TargetList } from '../src/features/policy/canonical.ts'
import { useApplicationPresets } from '../src/features/policy/applicationPresets.ts'
import { TargetSelector as AuroraTargetSelector } from '../src/features/policy/ui/TargetSelector.tsx'
import { TargetSelector as CompactTargetSelector } from '../src/compact/features/policy/Selectors.tsx'

Object.assign(globalThis, { React, IS_REACT_ACT_ENVIRONMENT: true })

const ordinaryTarget: TargetList = {
  id: 'ordinary', name: '办公域名', kind: 'domain', sourceType: 'manual', schedule: '', enabled: true,
  activeVersionId: '', revision: 1, pendingDeletion: false, counts: { valid: 1 },
  usage: { routingRuleCount: 0, accessRuleCount: 0 }, versions: [],
}

const catalogResponse = () => new Response(JSON.stringify({ presets: [{ id: 'youtube', name: 'YouTube', category: '视频', aliases: ['YT'], ruleURL: 'https://example.test/youtube.yaml' }] }), { status: 200, headers: { 'Content-Type': 'application/json' } })

function installDOM() {
  const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', { url: 'http://localhost/' })
  const previous = { window: globalThis.window, document: globalThis.document, HTMLElement: globalThis.HTMLElement, Node: globalThis.Node, Event: globalThis.Event }
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, HTMLElement: dom.window.HTMLElement, Node: dom.window.Node, Event: dom.window.Event })
  return { dom, restore: () => Object.assign(globalThis, previous) }
}

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

function findButton(root: HTMLElement, pattern: RegExp): HTMLButtonElement {
  const button = Array.from(root.querySelectorAll('button')).find((candidate) => pattern.test(candidate.textContent ?? ''))
  assert.ok(button, `button matching ${pattern} was not rendered`)
  return button as HTMLButtonElement
}

function CatalogProbe() {
  const { presets, error, reload } = useApplicationPresets()
  return <div><span data-testid="catalog">{presets.map((preset) => preset.name).join(',')}</span><span data-testid="error">{error ?? ''}</span><button type="button" onClick={reload}>重载</button></div>
}

async function mountSelector(Component: typeof AuroraTargetSelector, fetcher: typeof fetch) {
  const environment = installDOM()
  globalThis.fetch = fetcher
  const rootElement = environment.dom.window.document.getElementById('root') as HTMLElement
  const root: Root = createRoot(rootElement)
  await act(async () => {
    root.render(<Component deviceID="edge" targetLists={[ordinaryTarget]} selectedIDs={[]} onChange={() => {}} />)
    await Promise.resolve()
  })
  await settle()
  return { ...environment, root, rootElement }
}

test('application preset catalog request is device-independent and uses the authenticated same-origin session', async () => {
  let requestedURL = ''
  let requestedInit: RequestInit | undefined
  const previousFetch = globalThis.fetch
  globalThis.fetch = async (input, init) => {
    requestedURL = String(input)
    requestedInit = init
    return catalogResponse()
  }
  try {
    const presets = await fetchApplicationPresets()
    assert.equal(requestedURL, '/api/application-presets')
    assert.equal(requestedInit?.credentials, 'same-origin')
    assert.equal(requestedInit?.cache, 'no-store')
    assert.equal(presets[0]?.id, 'youtube')
  } finally {
    globalThis.fetch = previousFetch
  }
})

test('catalog endpoint failures preserve auth, server, and malformed-response causes', async () => {
  const cases: Array<{ label: string; response: Response; expected: RegExp }> = [
    { label: '401', response: new Response(JSON.stringify({ code: 'authentication_required', error: 'unauthorized' }), { status: 401 }), expected: /登录状态已失效/ },
    { label: '403', response: new Response(JSON.stringify({ code: 'forbidden', error: 'forbidden' }), { status: 403 }), expected: /无权读取/ },
    { label: '404', response: new Response(JSON.stringify({ code: 'not_found', error: 'missing' }), { status: 404 }), expected: /不支持/ },
    { label: '409', response: new Response(JSON.stringify({ code: 'onboarding_required', error: 'setup required' }), { status: 409 }), expected: /完成设备设置/ },
    { label: '503', response: new Response(JSON.stringify({ code: 'catalog_failed', error: 'temporary failure' }), { status: 503 }), expected: /服务暂时不可用/ },
    { label: 'malformed JSON', response: new Response('{', { status: 200 }), expected: /格式无效/ },
    { label: 'wrong catalog shape', response: new Response(JSON.stringify({ wrong: 'shape' }), { status: 200 }), expected: /格式无效/ },
  ]
  const previousFetch = globalThis.fetch
  try {
    for (const current of cases) {
      globalThis.fetch = async () => current.response.clone()
      await assert.rejects(fetchApplicationPresets(), (error: unknown) => {
        assert.match(applicationPresetCatalogErrorMessage(error), current.expected, current.label)
        return true
      })
    }
  } finally {
    globalThis.fetch = previousFetch
  }
})

test('Aurora and Compact selectors keep ordinary targets usable and retry the optional catalog', async () => {
  const previousFetch = globalThis.fetch
  try {
    for (const Component of [AuroraTargetSelector, CompactTargetSelector]) {
      let attempts = 0
      const mounted = await mountSelector(Component, async (input) => {
        attempts++
        if (String(input) === '/api/application-presets' && attempts === 1) return new Response(JSON.stringify({ code: 'catalog_failed', error: 'temporary failure' }), { status: 503 })
        return catalogResponse()
      })
      try {
        assert.match(mounted.rootElement.textContent ?? '', /办公域名/)
        const openButton = findButton(mounted.rootElement, /选择应用/)
        await act(async () => { openButton.click() })
        await settle()
        assert.match(mounted.rootElement.textContent ?? '', /应用预设目录服务暂时不可用/)
        assert.match(mounted.rootElement.textContent ?? '', /重试/)

        const retryButton = findButton(mounted.rootElement, /重试/)
        await act(async () => { retryButton.click() })
        await settle()
        assert.match(mounted.rootElement.textContent ?? '', /YouTube/)
        assert.match(mounted.rootElement.textContent ?? '', /办公域名/)
      } finally {
        await act(async () => { mounted.root.unmount() })
        mounted.restore()
      }
    }
  } finally {
    globalThis.fetch = previousFetch
  }
})

test('Aurora and Compact defer preview until selection and preserve the catalog after preview failure', async () => {
  const previousFetch = globalThis.fetch
  try {
    for (const Component of [AuroraTargetSelector, CompactTargetSelector]) {
      const requests: string[] = []
      const mounted = await mountSelector(Component, async (input) => {
        const url = String(input)
        requests.push(url)
        if (url === '/api/application-presets') return catalogResponse()
        return new Response(JSON.stringify({ code: 'fetch_failed', error: '源规则暂时不可用' }), { status: 502 })
      })
      try {
        assert.deepEqual(requests, ['/api/application-presets'])
        await act(async () => { findButton(mounted.rootElement, /选择应用/).click() })
        await settle()
        assert.deepEqual(requests, ['/api/application-presets'])
        await act(async () => { findButton(mounted.rootElement, /YouTube/).click() })
        await settle()
        assert.equal(requests[1], '/api/application-presets/youtube/preview?device=edge')
        assert.match(mounted.rootElement.textContent ?? '', /YouTube/)
        assert.match(mounted.rootElement.textContent ?? '', /源规则暂时不可用/)
        assert.match(mounted.rootElement.textContent ?? '', /办公域名/)
      } finally {
        await act(async () => { mounted.root.unmount() })
        mounted.restore()
      }
    }
  } finally {
    globalThis.fetch = previousFetch
  }
})

test('catalog retry aborts the stale request and keeps the newer response authoritative', async () => {
  const previousFetch = globalThis.fetch
  let callCount = 0
  let firstSignal: AbortSignal | undefined
  let resolveFirst: ((response: Response) => void) | undefined
  try {
    const environment = installDOM()
    globalThis.fetch = async (_input, init) => {
      callCount++
      if (callCount === 1) {
        firstSignal = init?.signal
        return new Promise<Response>((resolve) => { resolveFirst = resolve })
      }
      return catalogResponse()
    }
    const rootElement = environment.dom.window.document.getElementById('root') as HTMLElement
    const root: Root = createRoot(rootElement)
    await act(async () => {
      root.render(<CatalogProbe />)
      await Promise.resolve()
    })
    assert.ok(firstSignal)
    assert.equal(firstSignal?.aborted, false)
    await act(async () => { findButton(rootElement, /重载/).click() })
    await settle()
    assert.equal(firstSignal?.aborted, true)
    assert.match(rootElement.textContent ?? '', /YouTube/)

    resolveFirst?.(new Response(JSON.stringify({ presets: [{ id: 'stale', name: 'Stale', ruleURL: 'https://example.test/stale.yaml' }] }), { status: 200 }))
    await settle()
    assert.match(rootElement.textContent ?? '', /YouTube/)
    assert.doesNotMatch(rootElement.textContent ?? '', /Stale/)
    await act(async () => { root.unmount() })
    environment.restore()

    const unmountEnvironment = installDOM()
    let unmountSignal: AbortSignal | undefined
    globalThis.fetch = async (_input, init) => {
      unmountSignal = init?.signal
      return new Promise<Response>(() => {})
    }
    const unmountRootElement = unmountEnvironment.dom.window.document.getElementById('root') as HTMLElement
    const unmountRoot: Root = createRoot(unmountRootElement)
    await act(async () => {
      unmountRoot.render(<CatalogProbe />)
      await Promise.resolve()
    })
    await act(async () => { unmountRoot.unmount() })
    assert.equal(unmountSignal?.aborted, true)
    unmountEnvironment.restore()
  } finally {
    globalThis.fetch = previousFetch
  }
})
