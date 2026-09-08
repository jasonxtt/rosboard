import assert from 'node:assert/strict'
import test from 'node:test'
import { sameTrafficIngress, shouldIncludeTrafficIngress, sourceIsValid } from '../src/features/policy/source.ts'

const emptyIngress = { interfaceLists: [], interfaces: [] }
const lanIngress = { interfaceLists: ['LAN'], interfaces: [] }

test('source-only device or address selection does not require a TrafficIngress', () => {
  assert.equal(sourceIsValid({ mode: 'selected', members: [{ terminalId: 'device-1', binding: 'fixed', pinnedIpv4: ['192.0.2.10'], pinnedIpv6: [] }], prefixes: [] }, emptyIngress), true)
  assert.equal(sourceIsValid({ mode: 'selected', members: [], prefixes: ['192.0.2.10'] }, emptyIngress), true)
})

test('all and excluded source modes still require a TrafficIngress', () => {
  assert.equal(sourceIsValid({ mode: 'all', members: [], prefixes: [] }, emptyIngress), false)
  assert.equal(sourceIsValid({ mode: 'excluded', members: [{ terminalId: 'device-1', binding: 'fixed', pinnedIpv4: ['192.0.2.10'], pinnedIpv6: [] }], prefixes: [] }, emptyIngress), false)
  assert.equal(sourceIsValid({ mode: 'excluded', members: [{ terminalId: 'device-1', binding: 'fixed', pinnedIpv4: ['192.0.2.10'], pinnedIpv6: [] }], prefixes: [] }, lanIngress), true)
})

test('unchanged shared ingress is omitted from source-only proposals', () => {
  assert.equal(sameTrafficIngress({ interfaceLists: ['LAN'], interfaces: ['ether3'] }, { interfaceLists: ['ether3'], interfaces: ['LAN'] }), false)
  assert.equal(shouldIncludeTrafficIngress({ mode: 'selected', members: [], prefixes: ['192.0.2.10'] }, lanIngress, lanIngress), false)
  assert.equal(shouldIncludeTrafficIngress({ mode: 'selected', members: [], prefixes: ['192.0.2.10'] }, emptyIngress, lanIngress), true)
  assert.equal(shouldIncludeTrafficIngress({ mode: 'all', members: [], prefixes: [] }, emptyIngress, emptyIngress), true)
})
