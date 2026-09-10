import assert from 'node:assert/strict'
import test from 'node:test'
import { sameTrafficIngress, shouldIncludeTrafficIngress, sourceIsValid, typedSourceIsValid } from '../src/features/policy/source.ts'

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

test('typed routing sources validate without a TrafficIngress candidate', () => {
  const noInterface = { interfaces: [], interfaceLists: [], excludePrefixes: [] }
  const allSubject = { mode: 'all', members: [], prefixes: [] } as const
  assert.equal(typedSourceIsValid('device', noInterface, { mode: 'selected', members: [{ terminalId: 'device-1', binding: 'auto', pinnedIpv4: [], pinnedIpv6: [] }], prefixes: [] }), true)
  assert.equal(typedSourceIsValid('ip', noInterface, { mode: 'selected', members: [], prefixes: ['10.0.0.0/24', 'fd86::/64'] }), true)
  assert.equal(typedSourceIsValid('ip', noInterface, { mode: 'selected', members: [], prefixes: ['不是地址'] }), false)
  assert.equal(typedSourceIsValid('interface', { interfaces: ['bridge1'], interfaceLists: [], excludePrefixes: [] }, allSubject), true)
  assert.equal(typedSourceIsValid('interface', { interfaces: [], interfaceLists: ['LAN'], excludePrefixes: ['10.0.0.2'] }, allSubject), true)
  assert.equal(typedSourceIsValid('interface', noInterface, allSubject), false)
  assert.equal(typedSourceIsValid('interface', { interfaces: ['bridge1'], interfaceLists: [], excludePrefixes: ['不是地址'] }, allSubject), false)
  assert.equal(typedSourceIsValid('device', noInterface, allSubject), false)
  // interface-list 并入「指定接口」、all 已移除，两者都不能再新建。
  assert.equal(typedSourceIsValid('interface-list', noInterface, allSubject), false)
  assert.equal(typedSourceIsValid('all', noInterface, allSubject), false)
})
