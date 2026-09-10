import type { RoutingSourceKind, Subject, TrafficIngressScope } from './canonical'

/** 指定接口来源的编辑状态：接口与接口列表合并多选，排除地址按行输入。 */
export type RoutingInterfaceSource = { interfaces: string[]; interfaceLists: string[]; excludePrefixes: string[] }

export const emptyRoutingInterfaceSource: RoutingInterfaceSource = { interfaces: [], interfaceLists: [], excludePrefixes: [] }

// 轻量预检：IPv4/IPv6 字面量加可选 CIDR 前缀长度；严格校验仍由后端执行。
const sourcePrefixPattern = /^[0-9a-fA-F:.]+(\/\d{1,3})?$/

export function sourcePrefixesAreValid(prefixes: string[]): boolean {
  return prefixes.every((prefix) => sourcePrefixPattern.test(prefix))
}

/** 按行拆分用户输入的 IP/CIDR 列表。 */
export function parseSourcePrefixLines(text: string): string[] {
  return text.split(/\r?\n/).map((item) => item.trim()).filter(Boolean)
}

export function hasTrafficIngress(scope: TrafficIngressScope): boolean {
  return scope.interfaceLists.length + scope.interfaces.length > 0
}

export function requiresTrafficIngress(subject: Subject): boolean {
  return subject.mode === 'all' || subject.mode === 'excluded'
}

export function sourceIsValid(subject: Subject, ingress: TrafficIngressScope): boolean {
  if (subject.mode === 'all') return hasTrafficIngress(ingress)
  if (subject.mode === 'excluded') return hasTrafficIngress(ingress) && (subject.members.length > 0 || subject.prefixes.length > 0)
  return subject.members.length > 0 || subject.prefixes.length > 0
}

export function typedSourceIsValid(kind: RoutingSourceKind, interfaceSource: RoutingInterfaceSource, subject: Subject): boolean {
  if (kind === 'device') return subject.mode === 'selected' && subject.members.length > 0 && subject.prefixes.length === 0
  if (kind === 'ip') return subject.mode === 'selected' && subject.members.length === 0 && subject.prefixes.length > 0 && sourcePrefixesAreValid(subject.prefixes)
  if (kind === 'interface') {
    return (interfaceSource.interfaces.length > 0 || interfaceSource.interfaceLists.length > 0) && sourcePrefixesAreValid(interfaceSource.excludePrefixes)
  }
  // interface-list 已并入「指定接口」，all 已全部移除；两者都不再可新建。
  return false
}

function normalizedNames(values: string[]): string[] {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean))).sort()
}

export function sameTrafficIngress(left: TrafficIngressScope, right: TrafficIngressScope): boolean {
  return JSON.stringify({ interfaceLists: normalizedNames(left.interfaceLists), interfaces: normalizedNames(left.interfaces) })
    === JSON.stringify({ interfaceLists: normalizedNames(right.interfaceLists), interfaces: normalizedNames(right.interfaces) })
}

export function shouldIncludeTrafficIngress(subject: Subject, current: TrafficIngressScope, initial: TrafficIngressScope): boolean {
  return requiresTrafficIngress(subject) || !sameTrafficIngress(current, initial)
}
