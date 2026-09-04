import type { Subject, TrafficIngressScope } from './canonical'

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
