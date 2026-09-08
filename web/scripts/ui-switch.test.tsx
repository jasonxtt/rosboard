import assert from 'node:assert/strict'
import { after, beforeEach, test } from 'node:test'
import { JSDOM } from 'jsdom'
import React, { act, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import {
  PANEL_PREFERENCES_KEY, REFRESH_KEY, THEME_KEY, UI_VARIANT_KEY,
  readPanelRecord, readRefreshPreference, readThemePreference, readSession, writeSession, removeSession, resolveUiVariant,
  resetSharedPanelPreferences, savePanelRecord, switchUi, uiSwitchURL,
} from '../src/uiPreference.ts'

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', { url: 'http://localhost/?ui=compact#/settings/ui' })
const navigations: string[] = []
const location = { href: dom.window.location.href, search: '?ui=compact', assign: (url: string) => navigations.push(url) }
const testWindow = Object.create(dom.window)
Object.defineProperty(testWindow, 'location', { value: location })
Object.defineProperty(testWindow, 'matchMedia', { value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) })
Object.assign(globalThis, { React, window: testWindow, document: dom.window.document, HTMLElement: dom.window.HTMLElement, MutationObserver: dom.window.MutationObserver, getComputedStyle: dom.window.getComputedStyle.bind(dom.window), IS_REACT_ACT_ENVIRONMENT: true })
const { SettingsPage: CompactSettings } = await import('../src/compact/App.tsx')
const { UiPrefsForm } = await import('../src/features/settings/UiPrefsForm.tsx')
const { ShellContext } = await import('../src/shell/shellContext.ts')
const { loadPanelPreferences, savePanelPreferences, applyThemeChoice } = await import('../src/features/settings/prefs.ts')
const root = createRoot(document.getElementById('root')!)

beforeEach(async () => {
  await act(async () => root.render(null))
  dom.window.localStorage.clear()
  navigations.length = 0
  document.documentElement.dataset.theme = 'light'
})
after(async () => { await act(async () => root.unmount()); dom.window.close() })

test('choice has a safe default and URL overrides saved or corrupt preferences', () => {
  for (const value of [null, '', 'legacy', '{}', 'AURORA']) assert.equal(resolveUiVariant('', value), 'aurora')
  assert.equal(resolveUiVariant('', 'aurora'), 'aurora')
  assert.equal(resolveUiVariant('', 'compact'), 'compact')
  assert.equal(resolveUiVariant('?ui=compact', 'aurora'), 'compact')
  assert.equal(resolveUiVariant('?ui=aurora', 'compact'), 'aurora')
  assert.equal(resolveUiVariant('?ui=bad', 'aurora'), 'aurora')
})

test('switch is same-origin, retains other URL parameters and lands in counterpart UI settings', () => {
  const url = uiSwitchURL('https://panel.example/?device=x&ui=compact#/terminals/a', 'aurora')
  assert.equal(url, 'https://panel.example/?device=x&ui=aurora#/settings/ui')
  switchUi('aurora')
  assert.equal(dom.window.localStorage.getItem(UI_VARIANT_KEY), 'aurora')
  assert.match(navigations[0], /ui=aurora#\/settings\/ui$/)
})

test('denied storage does not block switch or default preferences', () => {
  Object.defineProperty(testWindow, 'localStorage', { configurable: true, get() { throw new Error('denied') } })
  Object.defineProperty(testWindow, 'sessionStorage', { configurable: true, get() { throw new Error('denied') } })
  try {
    assert.equal(readSession('test'), null)
    assert.doesNotThrow(() => { writeSession('test', 'x'); removeSession('test') })
    switchUi('aurora')
    assert.equal(navigations.length, 1)
    assert.equal(readRefreshPreference(), 1000)
    assert.deepEqual(readPanelRecord(), {})
    assert.doesNotThrow(() => resetSharedPanelPreferences())
  } finally { delete testWindow.localStorage; delete testWindow.sessionStorage }
})

test('legacy Compact preferences migrate, Aurora saves preserve all compatible fields', () => {
  dom.window.localStorage.setItem(PANEL_PREFERENCES_KEY, JSON.stringify({ refreshMs: 3000, theme: 'dark', landingView: 'policy-routing', terminalFamily: 'ipv6', futureOption: true }))
  assert.equal(readRefreshPreference(), 3000)
  assert.equal(readThemePreference(), 'dark')
  assert.deepEqual(loadPanelPreferences(), { landingView: 'policy-routing', terminalFamily: 'ipv6' })
  savePanelPreferences({ landingView: 'interfaces', terminalFamily: 'all' })
  assert.deepEqual(readPanelRecord(), { refreshMs: 3000, theme: 'dark', landingView: 'interfaces', terminalFamily: 'all', futureOption: true })
  dom.window.localStorage.setItem(REFRESH_KEY, '0')
  dom.window.localStorage.setItem(THEME_KEY, 'light')
  assert.equal(readRefreshPreference(), 0)
  assert.equal(readThemePreference(), 'light')
})

test('missing refresh is 1s, explicit zero stops refresh, malformed storage is rejected', () => {
  assert.equal(readRefreshPreference(), 1000)
  for (const raw of ['null', '[]', '"oops"', '{bad']) {
    dom.window.localStorage.setItem(PANEL_PREFERENCES_KEY, raw)
    assert.deepEqual(readPanelRecord(), {})
  }
  for (const raw of ['', '-1', '7', 'null']) {
    dom.window.localStorage.setItem(REFRESH_KEY, raw)
    assert.equal(readRefreshPreference(), 1000)
  }
})

test('system theme does not resurrect Compact legacy override; reset retains UI choice', () => {
  savePanelRecord({ theme: 'dark' })
  applyThemeChoice('system')
  assert.equal(readThemePreference(), null)
  dom.window.localStorage.setItem(UI_VARIANT_KEY, 'aurora')
  resetSharedPanelPreferences()
  assert.equal(dom.window.localStorage.getItem(UI_VARIANT_KEY), 'aurora')
})

test('Compact settings saves preferences before full Aurora navigation', async () => {
  const preferences = { refreshMs: 5000, landingView: 'overview' as const, terminalFamily: 'ipv6' as const, theme: 'dark' as const }
  const save = (value: typeof preferences) => { savePanelRecord(value) }
  const props = {
    settings: null, deviceStatuses: [], error: null, activeSection: 'ui' as const, preferences,
    dashboard: {} as ComponentProps<typeof CompactSettings>['dashboard'], selectedDeviceID: 'lab',
    collectionSaving: false, collectionMessage: null, restartSaving: false, restartMessage: null,
    onSaveCollection: async () => {}, onSavePreferences: save, onPreviewTheme: () => {}, onResetPreferences: () => {},
    onRestart: async () => {}, onRestartingAction: async () => {}, username: 'test', onAuthenticationChanged: () => {},
  }
  await act(async () => root.render(<CompactSettings {...props} />))
  const select = [...document.querySelectorAll('select')].find((element) => element.closest('label')?.textContent?.includes('UI 风格'))!
  assert.equal(select.value, 'compact')
  await act(async () => { select.value = 'aurora'; select.dispatchEvent(new dom.window.Event('change', { bubbles: true })) })
  assert.equal(navigations.length, 0, 'selection alone must not navigate')
  assert.match(document.body.textContent!, /保存并切换 UI/)
  await act(async () => select.closest('form')!.dispatchEvent(new dom.window.Event('submit', { bubbles: true, cancelable: true })))
  assert.equal(readPanelRecord().refreshMs, 5000)
  assert.match(navigations[0], /ui=aurora#\/settings\/ui$/)
})

test('Aurora settings preserves device and saves preferences before Compact navigation', async () => {
  dom.window.localStorage.setItem('rosboard:selected-device', 'router-b')
  const context: NonNullable<ComponentProps<typeof ShellContext.Provider>['value']> = {
    view: 'settings', navigate() {}, devices: [], devicesLoading: false, selectedDeviceId: 'router-b', selectDevice() {},
    scopedPath: (path) => path, refreshMs: 3000, setRefreshMs: (ms) => dom.window.localStorage.setItem(REFRESH_KEY, String(ms)),
    reloadNonce: 0, requestReload() {}, alerts: [], warnings: [], theme: 'dark', toggleTheme() {},
  }
  await act(async () => root.render(<ShellContext.Provider value={context}><UiPrefsForm /></ShellContext.Provider>))
  const select = document.querySelector<HTMLSelectElement>('select[aria-label="UI 风格"]')!
  assert.equal(select.value, 'aurora')
  await act(async () => { select.value = 'compact'; select.dispatchEvent(new dom.window.Event('change', { bubbles: true })) })
  assert.equal(navigations.length, 0)
  await act(async () => select.closest('form')!.dispatchEvent(new dom.window.Event('submit', { bubbles: true, cancelable: true })))
  assert.match(navigations[0], /ui=compact#\/settings\/ui$/)
  assert.equal(dom.window.localStorage.getItem('rosboard:selected-device'), 'router-b')
  assert.equal(readRefreshPreference(), 3000)
})

test('Aurora terminal API sends only customName and retains device/terminal scope', async () => {
  const { saveTerminalMetadata } = await import('../src/features/monitor-detail/api.ts')
  const originalFetch = globalThis.fetch
  const requests: Array<{ url: string; body: unknown; method: string | undefined }> = []
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), body: JSON.parse(String(init?.body)), method: init?.method })
    return new Response(JSON.stringify({ terminal: {} }), { status: 200 })
  }
  try {
    await saveTerminalMetadata((path) => `${path}?device=router-b`, 'mac:aa/bb', { customName: 'Desk' })
    await saveTerminalMetadata((path) => `${path}?device=router-b`, 'mac:aa/bb', { customName: '' })
    assert.deepEqual(requests.map((request) => request.body), [{ customName: 'Desk' }, { customName: '' }])
    assert.equal(requests[0].url, '/api/terminals/mac%3Aaa%2Fbb/metadata?device=router-b')
    assert.equal(requests[0].method, 'POST')
  } finally { globalThis.fetch = originalFetch }
})
