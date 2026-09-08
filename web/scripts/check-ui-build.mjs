import assert from 'node:assert/strict'
import { readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'
const dist = resolve(import.meta.dirname, '../../internal/ui/dist')
const manifest = JSON.parse(readFileSync(resolve(dist, '.vite/manifest.json'), 'utf8'))
function cssOf(key, visited = new Set()) {
  if (visited.has(key)) return new Set()
  visited.add(key)
  const item = manifest[key]
  assert.ok(item, `Missing manifest entry ${key}`)
  const css = new Set(item.css ?? [])
  for (const dependency of item.imports ?? []) for (const file of cssOf(dependency, visited)) css.add(file)
  return css
}
const entry = manifest['index.html']
assert.ok(entry)
assert.equal(cssOf('index.html').size, 0, 'Bootstrap must load no UI CSS')
assert.deepEqual(new Set(entry.dynamicImports), new Set(['src/compact/main.tsx', 'src/aurora.tsx']))
const compact = cssOf('src/compact/main.tsx')
const aurora = cssOf('src/aurora.tsx')
assert.ok(compact.size > 0 && aurora.size > 0)
assert.equal([...compact].filter((file) => aurora.has(file)).length, 0, 'UI root stylesheet graphs must be disjoint')
const html = readFileSync(resolve(dist, 'index.html'), 'utf8')
assert.ok(!/<link[^>]+rel="stylesheet"/.test(html), 'HTML must not preload a UI stylesheet')
for (const item of Object.values(manifest)) {
  for (const file of [item.file, ...(item.css ?? []), ...(item.assets ?? [])]) assert.ok(existsSync(resolve(dist, file)), `Missing built asset ${file}`)
}
console.log(`Dual UI build verified: neutral entry; Compact ${compact.size} CSS graph; Aurora ${aurora.size} CSS graph; all referenced assets exist.`)

// Execute the emitted dispatcher: manifest-only checks miss conditional
// preload rewriting that can attach the OTHER UI's CSS to the chosen import.
const { runInNewContext } = await import('node:vm')
const emitted = readFileSync(resolve(dist, entry.file), 'utf8')
assert.ok(!/\bimport\s*[{"']/.test(emitted), 'Update runtime harness if bootstrap gains static imports')
const runnable = emitted.replace(/import\.meta/g, 'meta').replace(/\bimport\(/g, 'loadModule(').replace(/export\s*\{[^}]*\};?\s*$/, '')
for (const variant of ['compact', 'aurora']) {
  const links = []
  const loaded = []
  const context = {
    URL, URLSearchParams, Promise, Event,
    meta: { url: `http://panel.test/${entry.file}` },
    window: { location: { search: `?ui=${variant}` }, localStorage: { getItem: () => null, setItem() {} }, dispatchEvent() {} },
    document: {
      documentElement: { dataset: {} },
      getElementsByTagName: () => links,
      querySelector: () => null,
      getElementById: () => null,
      createElement: () => ({ relList: { supports: () => true }, addEventListener(event, callback) { if (event === 'load') queueMicrotask(callback) } }),
      head: { appendChild(link) { links.push(link) } },
    },
    loadModule: async (url) => { loaded.push(url); return {} },
  }
  runInNewContext(runnable, context)
  await new Promise((done) => setImmediate(done))
  const key = variant === 'compact' ? 'src/compact/main.tsx' : 'src/aurora.tsx'
  assert.deepEqual(loaded, [`./${manifest[key].file.split('/').pop()}`], `${variant}: wrong runtime entry`)
  const actualCSS = new Set(links.filter((link) => link.rel === 'stylesheet').map((link) => new URL(link.href).pathname.slice(1)))
  assert.deepEqual(actualCSS, cssOf(key), `${variant}: emitted dispatcher preloads wrong stylesheet graph`)
}
console.log('Emitted bootstrap executed for both variants: correct runtime entry and exact matching CSS graph.')
