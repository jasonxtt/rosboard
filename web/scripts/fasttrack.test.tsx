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

test('keyword domain precedence stays in the warning without a second confirmation flow', async () => {
 const previous = globalThis.fetch
 globalThis.fetch = async () => new Response(JSON.stringify({ plan: {
  planID: 'keyword-plan', planHash: 'keyword-hash', blockers: [], familyBlockers: [], warnings: [{ code: 'routing_keyword_regexp_precedence', status: 'warning', reason: 'RouterOS 会先匹配 regexp，再匹配普通域名；命中关键字时可能优先于普通策略路由或访问控制域名规则。若不希望启用，请返回“高级设置”关闭“启用关键字域名规则”。' }], acknowledgements: [], operations: [], executionGroups: [], summary: {}, keywordImpact: { enabled: true, availableCount: 1, projectedCount: 1, keywords: ['video'], introducedKeywords: ['video'], requiresConfirmation: false, precedenceMode: 'routeros-regexp-first' },
 } }), { status: 200, headers: { 'Content-Type': 'application/json' } })
 try {
  const envelope = await generatePolicyPlan('device', 'structural')
  for (const view of [<PlanReviewBody deviceID="device" envelope={envelope} onApplied={() => {}} />, <PolicyPlanPreview deviceID="device" envelope={envelope} onApplied={() => {}} onBack={() => {}} />]) {
   const html = renderToStaticMarkup(view)
   assert.match(html, /RouterOS 会先匹配 regexp/)
   assert.match(html, /返回“高级设置”关闭/)
   assert.doesNotMatch(html, /此策略将启用关键字域名规则/)
   assert.doesNotMatch(html, /应用前确认/)
   assert.doesNotMatch(html, /type="checkbox"/)
  }
 } finally { globalThis.fetch = previous }
})
