import assert from 'node:assert/strict'
import test from 'node:test'
import { parseDiagnostics } from '../src/features/diagnostics/api'

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
