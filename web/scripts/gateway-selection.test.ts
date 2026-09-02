import assert from 'node:assert/strict'
import test from 'node:test'
import { gatewayCandidatesForWAN, suggestedGatewayForWAN } from '../src/features/policy/gateway.ts'
import type { PolicyDiscovery } from '../src/features/policy/canonical.ts'

function wan(overrides: Partial<PolicyDiscovery['wans'][number]> = {}): PolicyDiscovery['wans'][number] {
  return {
    interface: 'wan-a', type: 'ether', running: true, pointToPoint: false, proven: true,
    routes: [], ...overrides,
  }
}

test('WAN gateway suggestions use only one proven active main route', () => {
  const candidate = wan({ routes: [
    { family: 'ipv4', destination: '0.0.0.0/0', gateway: '192.0.2.1', immediateGateway: '192.0.2.1%wan-a', table: 'main', active: true, proven: true },
    { family: 'ipv4', destination: '0.0.0.0/0', gateway: '198.51.100.1', immediateGateway: '198.51.100.1%wan-a', table: 'backup', active: true, proven: true },
    { family: 'ipv4', destination: '0.0.0.0/0', gateway: '203.0.113.1', immediateGateway: '203.0.113.1%wan-a', table: 'main', active: false, proven: true },
  ] })
  assert.deepEqual(gatewayCandidatesForWAN(candidate, 'ipv4'), ['192.0.2.1'])
  assert.equal(suggestedGatewayForWAN(candidate, 'ipv4'), '192.0.2.1')
  assert.equal(suggestedGatewayForWAN(wan({ interface: 'wan-b', routes: [{ family: 'ipv4', destination: '0.0.0.0/0', gateway: '198.51.100.1', immediateGateway: '198.51.100.1%wan-b', table: 'main', active: true, proven: true }] }), 'ipv4'), '198.51.100.1')
})

test('point-to-point WANs never suggest a carried gateway', () => {
  const ppp = wan({ pointToPoint: true, routes: [{ family: 'ipv4', destination: '0.0.0.0/0', gateway: 'pppoe-out1', immediateGateway: 'pppoe-out1', table: 'main', active: true, proven: true }] })
  assert.deepEqual(gatewayCandidatesForWAN(ppp, 'ipv4'), [])
  assert.equal(suggestedGatewayForWAN(ppp, 'ipv4'), '')
  assert.equal(suggestedGatewayForWAN(ppp, 'ipv6'), '')
})

test('a WAN with multiple main gateways requires an explicit choice', () => {
  const ambiguous = wan({ routes: [
    { family: 'ipv4', destination: '0.0.0.0/0', gateway: '192.0.2.1', immediateGateway: '192.0.2.1%wan-a', table: 'main', active: true, proven: true },
    { family: 'ipv4', destination: '0.0.0.0/0', gateway: '198.51.100.1', immediateGateway: '198.51.100.1%wan-a', table: 'main', active: true, proven: true },
  ] })
  assert.equal(suggestedGatewayForWAN(ambiguous, 'ipv4'), '')
})

test('gateway suggestions keep only the selected address family', () => {
  const candidate = wan({ routes: [
    { family: 'ipv4', destination: '0.0.0.0/0', gateway: '2001:db8::1', immediateGateway: '192.0.2.1%wan-a', table: 'main', active: true, proven: true },
    { family: 'ipv6', destination: '::/0', gateway: '192.0.2.1', immediateGateway: '2001:db8::1%wan-a', table: ' main ', active: true, proven: true },
  ] })
  assert.deepEqual(gatewayCandidatesForWAN(candidate, 'ipv4'), ['192.0.2.1'])
  assert.deepEqual(gatewayCandidatesForWAN(candidate, 'ipv6'), ['2001:db8::1%wan-a'])
})
