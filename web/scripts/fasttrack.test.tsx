import assert from 'node:assert/strict'
import test from 'node:test'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { generatePolicyPlan, applyPolicyPlan, fastTrackSummary } from '../src/features/policy/canonical.ts'
import { PlanReviewBody } from '../src/features/policy/ui/PlanReview.tsx'
import { PolicyPlanPreview } from '../src/compact/features/policy/PolicyPlanPreview.tsx'

Object.assign(globalThis, { React })

test('both plan views expose FastTrack changes and require readable risk confirmation bound to the plan', async () => {
 const previous = globalThis.fetch
 const requests: Array<{ url: string; body: unknown }> = []
 globalThis.fetch = async (input, init) => {
  requests.push({ url: String(input), body: init?.body ? JSON.parse(String(init.body)) : null })
  return new Response(JSON.stringify({ plan: { planID: 'p', planHash: 'reviewed', blockers: [], warnings: [], acknowledgements: [{ code: 'fasttrack_compatibility_unverified', required: true }], fastTrack: { consumers: 1, rules: [{ id: '*A', menu: 'ip/firewall/filter', status: 'auto_fixable', reason: '将添加 connection-mark=no-mark' }] } } }), { status: 200, headers: { 'Content-Type': 'application/json' } })
 }
 try {
  const envelope = await generatePolicyPlan('device', 'structural')
  assert.equal(envelope.plan.fastTrack?.rules[0].id, '*A')
  for (const view of [<PlanReviewBody deviceID="device" envelope={envelope} onApplied={() => {}} />, <PolicyPlanPreview deviceID="device" envelope={envelope} onApplied={() => {}} onBack={() => {}} />]) {
   const html = renderToStaticMarkup(view)
   assert.match(html, /connection-mark=no-mark/)
   assert.match(html, /我已了解 FastTrack/)
   assert.match(html, /type="checkbox"/)
   assert.match(html, /disabled=""/)
  }
  await applyPolicyPlan('device', 'p', ['fasttrack_compatibility_unverified'], 'reviewed')
  assert.deepEqual(requests.at(-1)?.body, { acknowledgements: ['fasttrack_compatibility_unverified'], planHash: 'reviewed' })
  assert.match(fastTrackSummary({ consumers: 1, retainOnly: true, rules: [] }), /不修改或恢复/)
 } finally { globalThis.fetch = previous }
})
