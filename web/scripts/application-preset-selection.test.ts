import assert from 'node:assert/strict'
import test from 'node:test'
import { applicationPresetTargetListPlan, type PresetPreview, type TargetListPreview } from '../src/features/policy/canonical.ts'

function content(validRules: number): TargetListPreview {
  return { validRules, ignored: {}, errorSamples: [], rules: [] }
}

function preview(domainRules: number, ipRules: number, existingTargetListIds: string[]): PresetPreview {
  return {
    previewId: 'preview', id: 'demo', name: 'Demo', existingTargetListIds,
    domain: content(domainRules), ip: content(ipRules),
  }
}

test('application preset reuse only selects current required target lists', () => {
  assert.deepEqual(applicationPresetTargetListPlan(preview(1, 0, ['preset:demo:domain'])), { requiredIDs: ['preset:demo:domain'], reuseExisting: true })
  assert.deepEqual(applicationPresetTargetListPlan(preview(1, 0, ['preset:demo:domain', 'preset:demo:ip'])), { requiredIDs: ['preset:demo:domain'], reuseExisting: true })
  assert.deepEqual(applicationPresetTargetListPlan(preview(1, 1, ['preset:demo:domain', 'preset:demo:ip'])), { requiredIDs: ['preset:demo:domain'], reuseExisting: true })
  assert.deepEqual(applicationPresetTargetListPlan(preview(1, 1, ['preset:demo:domain'])), { requiredIDs: ['preset:demo:domain'], reuseExisting: true })
  assert.deepEqual(applicationPresetTargetListPlan(preview(1, 1, ['preset:demo:ip'])), { requiredIDs: ['preset:demo:domain'], reuseExisting: false })
})
