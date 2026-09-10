import assert from 'node:assert/strict'
import test from 'node:test'
import { downloadDiagnostics, fetchDeepDiagnostics, parseDeepDiagnostics, parseDiagnostics } from '../src/features/diagnostics/api'

test('diagnostics parser keeps finding status and overall impact separate', () => {
  const report = parseDiagnostics({
    generatedAt: '2026-09-10T00:00:00Z',
    mode: 'quick',
    deviceId: 'router-a',
    overall: 'healthy',
    findings: [
      { id: 'mosdns', group: 'recognition', status: 'error', title: 'MosDNS', summary: 'failed', affectsOverall: false, evidence: { lastError: 'timeout' } },
      { id: 'disabled', group: 'update', status: 'disabled', title: 'Update', summary: 'off', affectsOverall: true },
    ],
  })
  assert.equal(report.overall, 'healthy')
  assert.equal(report.findings[0]?.status, 'error')
  assert.equal(report.findings[0]?.affectsOverall, false)
  assert.equal(report.findings[0]?.evidence.lastError, 'timeout')
  assert.equal(report.findings[1]?.status, 'disabled')
})

test('diagnostics parser rejects unknown status into a visible error state', () => {
  const report = parseDiagnostics({ findings: [{ id: 'unknown', status: 'future-status' }] })
  assert.equal(report.findings[0]?.status, 'error')
})

test('deep diagnostics parser keeps snapshot and ingress trace evidence', () => {
  const report = parseDeepDiagnostics({
    mode: 'deep',
    snapshot: { capturedAt: '2026-09-10T00:00:00Z', fingerprint: 'abc', endpoints: [{ endpoint: '/interface', objectCount: 3, required: true }] },
    ingressTrace: [{ interface: 'bridge1', result: 'rejected', reasonCode: 'ingress.bridge_slave', reason: 'bridge slave', evidence: { bridge: 'bridge0' } }],
  })
  assert.equal(report.mode, 'deep')
  assert.equal(report.snapshot.endpoints[0]?.endpoint, '/interface')
  assert.equal(report.snapshot.endpoints[0]?.objectCount, 3)
  assert.equal(report.ingressTrace[0]?.reasonCode, 'ingress.bridge_slave')
  assert.equal(report.ingressTrace[0]?.evidence.bridge, 'bridge0')
})

test('deep diagnostics API is a device-scoped POST', async () => {
  const originalFetch = globalThis.fetch
  let request: { path: string; method: string | undefined } | null = null
  globalThis.fetch = (async (path, init) => {
    request = { path: String(path), method: init?.method }
    return new Response(JSON.stringify({ mode: 'deep', snapshot: { endpoints: [] }, ingressTrace: [], findings: [] }), { status: 200 })
  }) as typeof fetch
  try {
    await fetchDeepDiagnostics('router/a')
  } finally {
    globalThis.fetch = originalFetch
  }
  assert.deepEqual(request, { path: '/api/diagnostics/deep?device=router%2Fa', method: 'POST' })
})

test('diagnostic export API downloads a device-scoped ZIP', async () => {
  const originalFetch = globalThis.fetch
  let request: { path: string; method: string | undefined } | null = null
  globalThis.fetch = (async (path, init) => {
    request = { path: String(path), method: init?.method }
    return new Response(new Blob(['zip-fixture']), {
      status: 200,
      headers: {
        'Content-Type': 'application/zip',
        'Content-Disposition': 'attachment; filename="rosboard-diagnostics-fixture.zip"',
      },
    })
  }) as typeof fetch
  try {
    const result = await downloadDiagnostics('router/a')
    assert.equal(result.filename, 'rosboard-diagnostics-fixture.zip')
    assert.equal(await result.blob.text(), 'zip-fixture')
  } finally {
    globalThis.fetch = originalFetch
  }
  assert.deepEqual(request, { path: '/api/diagnostics/export?device=router%2Fa', method: 'POST' })
})
