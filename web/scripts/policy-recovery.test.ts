import assert from 'node:assert/strict'
import test from 'node:test'
import { CanonicalPolicyError, type Egress, type RoutingRule } from '../src/features/policy/canonical.ts'
import { internetEgressCandidatesOf, mutationPartialStateOf, PolicyApiError } from '../src/features/policy/api.ts'
import { routingRuleStatus } from '../src/features/policy/ui/ruleStatus.ts'

const candidatesPayload = {
  code: 'plan_blocked',
  error: 'blocked',
  internetEgressCandidates: {
    ipv4: [{ interface: 'wan1', type: 'ether', running: true, reason: 'r' }],
    ipv6: [],
  },
}

test('internetEgressCandidatesOf parses canonical errors carrying details (P1-1)', () => {
  const error = new CanonicalPolicyError('blocked', 422, 'plan_blocked', candidatesPayload)
  assert.deepEqual(internetEgressCandidatesOf(error), {
    ipv4: [{ interface: 'wan1', type: 'ether', running: true, reason: 'r' }],
  })
})

test('internetEgressCandidatesOf keeps supporting PolicyApiError and rejects noise', () => {
  assert.deepEqual(internetEgressCandidatesOf(new PolicyApiError('blocked', 422, 'plan_blocked', candidatesPayload))?.ipv4?.length, 1)
  assert.equal(internetEgressCandidatesOf(new CanonicalPolicyError('other', 500, 'x', { error: 'x' })), null)
  assert.equal(internetEgressCandidatesOf(new Error('plain')), null)
  assert.equal(internetEgressCandidatesOf('nope'), null)
})

test('mutationPartialStateOf reads partial-success markers (P1-1/P1-2)', () => {
  assert.deepEqual(mutationPartialStateOf(new CanonicalPolicyError('x', 422, 'plan_blocked', { deleted: true, desiredSaved: true })), {
    desiredSaved: true,
    deleted: true,
  })
  assert.deepEqual(mutationPartialStateOf(new CanonicalPolicyError('x', 409, 'apply_failed', { desiredSaved: true })), {
    desiredSaved: true,
    deleted: false,
  })
  assert.deepEqual(mutationPartialStateOf(new CanonicalPolicyError('x', 422, 'invalid_rule', { code: 'invalid_rule' })), {
    desiredSaved: false,
    deleted: false,
  })
  assert.deepEqual(mutationPartialStateOf(new Error('plain')), { desiredSaved: false, deleted: false })
})

const egress: Egress = {
  id: 'wan',
  name: 'WAN',
  priority: 10,
  listMode: 'shared',
  listName: 'wan',
  dnsUpstream: '',
  fakeAlias: '',
  failureMode: 'strict',
  routerOutput: false,
  enabled: true,
  revision: 1,
  pendingDeletion: false,
  applied: true,
  families: [],
}

const rule: RoutingRule = {
  id: 'r1',
  name: 'rule',
  subject: { mode: 'all', members: [], prefixes: [] },
  ingress: { interfaceLists: ['LAN'], interfaces: [] },
  targetListIds: [],
  egressId: 'wan',
  priority: 10,
  enabled: true,
  revision: 1,
}

test('routingRuleStatus hides 已应用 while desired ≠ RouterOS (P1-2)', () => {
  assert.deepEqual(routingRuleStatus(rule, egress, false), { tone: 'ok', label: '已应用' })
  // Global desired/applied mismatch must downgrade an otherwise-applied rule.
  assert.deepEqual(routingRuleStatus(rule, egress, true), { tone: 'warn', label: '待应用' })
  // The other states are unaffected by the mismatch.
  assert.deepEqual(routingRuleStatus({ ...rule, enabled: false }, egress, true), { tone: 'neutral', label: '已停用' })
  assert.deepEqual(routingRuleStatus(rule, undefined, true), { tone: 'err', label: '出口缺失' })
  assert.deepEqual(routingRuleStatus(rule, { ...egress, applied: false }, false), { tone: 'warn', label: '待应用' })
  assert.deepEqual(routingRuleStatus(rule, { ...egress, pendingDeletion: true }, false), { tone: 'warn', label: '出口待删除' })
})
